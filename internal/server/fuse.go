package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

// FuseModule represents the FUSE module for mounting volumes.
type FuseModule struct {
	server     *Server
	volumeName string
	mountPoint string
	fs         *FS
	conn       *fuse.Conn
}

// FuseModuleConfig represents the configuration needed to initialize a FuseModule
type FuseModuleConfig struct {
	VolumeName string `json:"Volume"`
	MountPoint string `json:"MountPoint"`
}

func NewFuseModule(server *Server, volumeName, mountPoint string) *FuseModule {
	return &FuseModule{
		server:     server,
		volumeName: volumeName,
		mountPoint: mountPoint,
	}
}

func (fm *FuseModule) Start() error {
	volume := fm.server.FindVolumeByName(fm.volumeName)
	if volume == nil {
		return fmt.Errorf("volume %s not found", fm.volumeName)
	}

	fm.fs = NewFS(volume)

	// Mount the filesystem
	conn, err := fuse.Mount(
		fm.mountPoint,
		fuse.FSName("grits-fuse-module"),
		fuse.Subtype("FuseModule"),
	)
	if err != nil {
		return err
	}
	fm.conn = conn

	// Serve the filesystem
	go func() {
		log.Printf("About to serve FUSE fs\n")
		if err := fs.Serve(conn, fm.fs); err != nil {
			log.Printf("Failed to serve FUSE filesystem: %v", err)
		}
		log.Printf("Done with serve FUSE fs\n")
	}()

	return nil
}

func (fm *FuseModule) Stop() error {
	log.Printf("Stopping FUSE fs\n")
	if fm.conn != nil {
		fuse.Unmount(fm.mountPoint)
		fm.conn.Close()
	}
	return nil
}

func (fm *FuseModule) GetModuleName() string {
	return "fuse"
}

// FS represents the filesystem structure.
type FS struct {
	volume Volume

	openFiles     map[string]*os.File // Note, this is only files with unflushed changes
	openFilesLock sync.RWMutex

	uidManager *UIDManager
}

// Initialize FS with openFiles map
func NewFS(volume Volume) *FS {
	return &FS{
		volume:     volume,
		openFiles:  make(map[string]*os.File),
		uidManager: NewUIDManager(),
	}
}

func (fs *FS) GetOpenFile(path string, isCreate bool) (*os.File, error) {
	fs.openFilesLock.RLock()
	file, exists := fs.openFiles[path]
	fs.openFilesLock.RUnlock()
	if exists {
		return file, nil
	}

	tempFile, err := os.CreateTemp("", "fusewrite-")
	if err != nil {
		return nil, fmt.Errorf("creating temp file for write: %w", err)
	}
	defer func() {
		if tempFile != nil {
			os.Remove(tempFile.Name())
		}
	}()

	cf, err := fs.volume.GetNameStore().Lookup(path)
	defer func() {
		if cf != nil {
			fs.volume.GetNameStore().BlobStore.Release(cf)
		}
	}()

	if isCreate {
		// No data to copy, but return error if this already existed
		if cf != nil {
			return nil, fmt.Errorf("file %s already exists", path)
		}
	} else {
		// Copy pre-existing data

		infile, err := os.Open(cf.Path)
		if err != nil {
			return nil, err
		}
		defer infile.Close()

		_, err = io.Copy(tempFile, infile)
		if err != nil {
			return nil, err
		}
	}

	fs.openFilesLock.Lock()
	defer fs.openFilesLock.Unlock()

	file, exists = fs.openFiles[path]
	if exists {
		if isCreate {
			return nil, fmt.Errorf("file %s already exists", path)
		} else {
			// Oops. Clean up the copy we made and never speak of it again.
			return file, nil
		}
	}

	fs.openFiles[path] = tempFile
	tempFile = nil // Disable deletion
	return fs.openFiles[path], nil
}

// Hacky UID manager

type UIDManager struct {
	pathToUID map[string]uint64
	maxUID    uint64
	lock      sync.Mutex
}

func NewUIDManager() *UIDManager {
	return &UIDManager{
		pathToUID: make(map[string]uint64),
	}
}

func (m *UIDManager) GetUID(path string) uint64 {
	m.lock.Lock()
	defer m.lock.Unlock()

	if uid, exists := m.pathToUID[path]; exists {
		return uid
	}
	// Assign a new UID
	m.maxUID++
	m.pathToUID[path] = m.maxUID
	return m.maxUID
}

func (m *UIDManager) RemovePath(path string) {
	m.lock.Lock()
	defer m.lock.Unlock()

	delete(m.pathToUID, path)
}

// Base operations and structures

type Dir struct {
	fs   *FS
	Path string
}

type File struct {
	fs   *FS
	Path string
}

// Main FUSE interface functions

func (fs *FS) Root() (fs.Node, error) {
	return LookupFuseNode(fs, "/")
}

func (d *Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	log.Printf("Fuse module Lookup() for %s in %s\n", name, d.Path)

	// Combine the directory's path with the name to form the full path
	fullPath := path.Join(d.Path, name)
	fullPath = strings.TrimLeft(fullPath, "/")

	return LookupFuseNode(d.fs, fullPath)
}

func LookupFuseNode(fs *FS, fullPath string) (fs.Node, error) {
	log.Printf("  LookupFuseNode() for %s\n", fullPath)

	// Lookup the node using the NameStore
	node, err := fs.volume.GetNameStore().LookupNode(fullPath)
	if err != nil {
		log.Printf("    Error: %v\n", err)
		// Convert the error into a FUSE ENOENT error if the node is not found
		if os.IsNotExist(err) {
			return nil, syscall.ENOENT
		}
		return nil, err
	}
	defer node.Release(fs.volume.GetNameStore().BlobStore)

	// Depending on the type of node, return a Dir or File object
	switch node.(type) {
	case *grits.TreeNode:
		log.Printf("    got tree\n")
		// For TreeNode, return a Dir object
		return &Dir{fs: fs, Path: fullPath}, nil
	case *grits.BlobNode:
		log.Printf("    got blob\n")
		// For BlobNode, return a File object
		return &File{fs: fs, Path: fullPath}, nil
	default:
		log.Print("    Unknown node type", err)
		// If the node type is unknown, return an error
		return nil, fmt.Errorf("unknown node type for path %s", fullPath)
	}
}

func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	log.Printf("Fuse module ReadDirAll() for %s\n", d.Path)

	node, err := d.fs.volume.GetNameStore().LookupNode(d.Path)
	if err != nil {
		return nil, err
	}
	defer node.Release(d.fs.volume.GetNameStore().BlobStore)

	tn, ok := node.(*grits.TreeNode)
	if !ok {
		return nil, fmt.Errorf("%s is no longer directory", d.Path)
	}

	var dirents []fuse.Dirent

	// Assuming d.Node is the TreeNode representing this directory
	for name, child := range tn.ChildrenMap {
		// Determine the type of the child (Dir or File)
		var dtype fuse.DirentType
		switch child.(type) {
		case *grits.TreeNode:
			dtype = fuse.DT_Dir
		case *grits.BlobNode:
			dtype = fuse.DT_File
		default:
			return nil, fmt.Errorf("unknown child type")
		}

		// Construct a fuse.Dirent for each child
		dirent := fuse.Dirent{
			Inode: 1, // FIXME
			Name:  name,
			Type:  dtype,
		}
		log.Printf("  have %s\n", name)
		dirents = append(dirents, dirent)
	}

	return dirents, nil
}

func (f *File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	log.Printf("Fuse module Read() for %s\n", f.Path)

	cf, err := f.fs.volume.GetNameStore().Lookup(f.Path)
	if err != nil {
		return err
	}
	defer f.fs.volume.GetNameStore().BlobStore.Release(cf)

	data, err := cf.Read(int(req.Offset), req.Size)
	if err != nil {
		return err
	}

	resp.Data = data
	return nil
}

// Example implementation for Write and Flush
func (f *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	log.Printf("Fuse module Write() for %s\n", f.Path)

	openFile, err := f.fs.GetOpenFile(f.Path, false)
	if err != nil {
		return err
	}

	// Write to the temporary file
	written, err := openFile.WriteAt(req.Data, req.Offset)
	if err != nil {
		return err
	}

	resp.Size = written
	return nil
}

func (f *File) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	log.Printf("Fuse module Flush() for %s\n", f.Path)

	f.fs.openFilesLock.Lock()
	defer f.fs.openFilesLock.Unlock() // Note: Lock contention under write load

	openFile, exists := f.fs.openFiles[f.Path]
	if !exists {
		// No problem; nothing to flush.
		return nil
	}
	delete(f.fs.openFiles, f.Path)

	defer os.Remove(openFile.Name())

	_, err := openFile.Seek(0, 0)
	if err != nil {
		return err
	}

	cf, err := f.fs.volume.GetNameStore().BlobStore.AddOpenFile(openFile)
	if err != nil {
		return err
	}
	defer f.fs.volume.GetNameStore().BlobStore.Release(cf)

	err = f.fs.volume.GetNameStore().LinkBlob(f.Path, cf.Address)
	if err != nil {
		return err
	}

	return nil
}

func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	log.Printf("Fuse module Open() for %s\n", f.Path)

	return f, nil // Assuming the *File itself can act as a handle
}

func (d *Dir) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	log.Printf("Fuse module Create() for %s %s\n", d.Path, req.Name)

	fullPath := path.Join(d.Path, req.Name)
	fullPath = strings.TrimLeft(fullPath, "/")

	_, err := d.fs.GetOpenFile(fullPath, true)
	if err != nil {
		return nil, nil, err
	}

	// Prepare the FUSE response and new File node
	fileNode := &File{fs: d.fs, Path: fullPath}

	uid := d.fs.uidManager.GetUID(fullPath)
	resp.Node = fuse.NodeID(uid)
	resp.Handle = fuse.HandleID(uid)
	resp.Attr.Inode = uid

	resp.Attr.Mode = req.Mode

	return fileNode, fileNode, nil
}

func FuseAttr(cf *grits.CachedFile, attr *fuse.Attr, mode os.FileMode) error {
	fileInfo, err := os.Stat(cf.Path)
	if err != nil {
		log.Printf("  Oh no %v\n", err)
		return err
	}

	stat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		log.Printf("  Oh nooooo\n")
		return fmt.Errorf("failed to get inode number for %s", cf.Path)
	}

	attr.Inode = 1 // FIXME
	attr.Mode = mode
	attr.Size = uint64(fileInfo.Size())
	attr.Atime = time.Unix(stat.Atim.Sec, stat.Atim.Nsec) // Access time
	attr.Mtime = time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec) // Modification time
	attr.Ctime = time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec) // Change time

	return nil
}

func (f *File) Attr(ctx context.Context, attr *fuse.Attr) error {
	log.Printf("Fuse module Attr() for %s\n", f.Path)

	cf, err := f.fs.volume.GetNameStore().Lookup(f.Path)
	if err != nil {
		log.Printf("Couldn't look up! %s\n", f.Path)
		return err
	}
	defer f.fs.volume.GetNameStore().BlobStore.Release(cf)

	return FuseAttr(cf, attr, 0o400)
}

func (d *Dir) Attr(ctx context.Context, attr *fuse.Attr) error {
	log.Printf("Fuse module Attr() for %s\n", d.Path)

	cf, err := d.fs.volume.GetNameStore().Lookup(d.Path)
	if err != nil {
		log.Printf("Couldn't look up! %s\n", d.Path)
		return err
	}
	defer d.fs.volume.GetNameStore().BlobStore.Release(cf)

	return FuseAttr(cf, attr, os.ModeDir|0o500)
}
