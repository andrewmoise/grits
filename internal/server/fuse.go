package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"path"
	"strings"
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

	fm.fs = &FS{volume: volume}
	//fm.fs = &HelloFS{}

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
}

type Dir struct {
	Volume Volume
	Path   string
}

type File struct {
	Volume Volume
	Path   string
}

// Main interface functions

func (fs *FS) Root() (fs.Node, error) {
	return LookupFuseNode(fs.volume, "/")
}

func (d *Dir) Lookup(ctx context.Context, name string) (fs.Node, error) {
	log.Printf("Fuse module Lookup() for %s in %s\n", name, d.Path)

	// Combine the directory's path with the name to form the full path
	fullPath := path.Join(d.Path, name)
	fullPath = strings.TrimLeft(fullPath, "/")

	return LookupFuseNode(d.Volume, fullPath)
}

func LookupFuseNode(v Volume, fullPath string) (fs.Node, error) {
	log.Printf("  LookupFuseNode() for %s\n", fullPath)

	// Lookup the node using the NameStore
	node, err := v.GetNameStore().LookupNode(fullPath)
	if err != nil {
		log.Printf("    Error: %v\n", err)
		// Convert the error into a FUSE ENOENT error if the node is not found
		if os.IsNotExist(err) {
			return nil, syscall.ENOENT
		}
		return nil, err
	}
	defer node.Release(v.GetNameStore().BlobStore)

	// Depending on the type of node, return a Dir or File object
	switch node.(type) {
	case *grits.TreeNode:
		log.Printf("    got tree\n")
		// For TreeNode, return a Dir object
		return &Dir{Volume: v, Path: fullPath}, nil
	case *grits.BlobNode:
		log.Printf("    got blob\n")
		// For BlobNode, return a File object
		return &File{Volume: v, Path: fullPath}, nil
	default:
		log.Printf("    Unknown node type", err)
		// If the node type is unknown, return an error
		return nil, fmt.Errorf("unknown node type for path %s", fullPath)
	}
}

func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	log.Printf("Fuse module ReadDirAll() for %s\n", d.Path)

	node, err := d.Volume.GetNameStore().LookupNode(d.Path)
	if err != nil {
		return nil, err
	}
	defer node.Release(d.Volume.GetNameStore().BlobStore)

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
			continue // Skip if the type is unknown
		}

		// Construct a fuse.Dirent for each child
		dirent := fuse.Dirent{
			// Inode numbers are not directly managed in this example; set to 0 if unknown
			// Some filesystems use unique hash values or other mechanisms to generate inode numbers
			Inode: 1,
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

	cf, err := f.Volume.GetNameStore().Lookup(f.Path)
	if err != nil {
		return err
	}
	defer f.Volume.GetNameStore().BlobStore.Release(cf)

	data, err := cf.Read(int(req.Offset), req.Size)
	if err != nil {
		return err
	}

	resp.Data = data
	return nil
}

func (f *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	return fmt.Errorf("not implemented")
}

func (f *File) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	return fmt.Errorf("not implemented")
}

func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	log.Printf("Fuse module Open() for %s\n", f.Path)

	return f, nil // Assuming the *File itself can act as a handle
}

func (f *File) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	log.Printf("Fuse module Create() for %s\n", f.Path)

	return nil, nil, fmt.Errorf("not implemented")
}

// Implementations

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

	attr.Inode = 1
	attr.Mode = mode
	attr.Size = uint64(fileInfo.Size())
	attr.Atime = time.Unix(stat.Atim.Sec, stat.Atim.Nsec) // Access time
	attr.Mtime = time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec) // Modification time
	attr.Ctime = time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec) // Change time

	return nil
}

func (f *File) Attr(ctx context.Context, attr *fuse.Attr) error {
	log.Printf("Fuse module Attr() for %s\n", f.Path)

	cf, err := f.Volume.GetNameStore().Lookup(f.Path)
	if err != nil {
		log.Printf("Couldn't look up! %s\n", f.Path)
		return err
	}
	defer f.Volume.GetNameStore().BlobStore.Release(cf)

	return FuseAttr(cf, attr, 0o400)
}

func (d *Dir) Attr(ctx context.Context, attr *fuse.Attr) error {
	log.Printf("Fuse module Attr() for %s\n", d.Path)

	cf, err := d.Volume.GetNameStore().Lookup(d.Path)
	if err != nil {
		log.Printf("Couldn't look up! %s\n", d.Path)
		return err
	}
	defer d.Volume.GetNameStore().BlobStore.Release(cf)

	return FuseAttr(cf, attr, os.ModeDir|0o500)
}
