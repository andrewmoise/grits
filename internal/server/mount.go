package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

/////
// Timeline

// * Already done, I think: NodeLookuper,
//   NodeReaddirer

// * Minimal implementation phase (we need this right away if we're
//   even trying to just do reading and navigation): NodeGetattrer,
//   NodeOpener, NodeReader

// * Writing implementation (somewhat complex because of copy-on-write
//   semantics -- I'll honestly have to construct a new abstraction of
//   an "in flight being-modified file" in order for this thing to
//   work): NodeWriter, NodeFlusher, NodeReleaser (we need to manage
//   some of our object lifetimes in ways that will wind up a little
//   bit complex), NodeCreater, NodeUnlinker

// * Directories (needs even more modifications for weird reasons,
//   nice to do it in 2 phases): NodeMkdirer, NodeRmdirer

// * Polish (not really needed but what the heck let's try to make it
//   nice and all): NodeStatfser, NodeFsyncer, NodeRenamer

/////
// Module stuff

type MountModuleConfig struct {
	MountPoint string `json:"MountPoint"`
	Volume     string `json:"Volume"`
}

type MountModule struct {
	config      *MountModuleConfig
	fsServer    *fuse.Server
	gritsServer *Server
}

func NewMountModule(config *MountModuleConfig, server *Server) *MountModule {
	return &MountModule{
		config:      config,
		gritsServer: server,
	}
}

func (*MountModule) GetModuleName() string {
	return "mount"
}

func (mm *MountModule) Start() error {
	mntDir := mm.config.MountPoint
	os.Mkdir(mntDir, 0755)

	volume, exists := mm.gritsServer.Volumes[mm.config.Volume]
	if !exists {
		return fmt.Errorf("can't open volume %s", mm.config.Volume)
	}

	root := &gritsNode{
		volume: volume,
		path:   "",
	}

	var err error
	mm.fsServer, err = fs.Mount(mntDir, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			Debug: true,
			//FsName:                   "grits",
			Name:                     "grits",
			DisableXAttrs:            true,
			ExplicitDataCacheControl: true,
		},
	})
	if err != nil {
		return err
	}

	log.Printf("Mounted on %s", mntDir)
	log.Printf("Unmount by calling 'fusermount -u %s'", mntDir)
	return nil
}

func (mm *MountModule) Stop() error {
	log.Printf("We are stopping mount module")

	// Wait until unmount before exiting
	mm.fsServer.Wait()
	return nil
}

/////
// FUSE stuff

type gritsNode struct {
	fs.Inode
	volume Volume
	path   string // Now storing path instead of a node reference
}

// FileHandle represents an open file. This will wrap os.File to handle file operations.
type FileHandle struct {
	file       *os.File
	cachedFile *grits.CachedFile
	node       *gritsNode

	isDirty bool
	mtx     sync.RWMutex
}

// Readdir opens a stream of directory entries.
//
// Readdir essentiallly returns a list of strings, and it is allowed
// for Readdir to return different results from Lookup. For example,
// you can return nothing for Readdir ("ls my-fuse-mount" is empty),
// while still implementing Lookup ("ls my-fuse-mount/a-specific-file"
// shows a single file). The DirStream returned must be deterministic;
// a randomized result (e.g. due to map iteration) can lead to entries
// disappearing if multiple processes read the same directory
// concurrently.

var _ = (fs.NodeReaddirer)((*gritsNode)(nil))

func (gn *gritsNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	node, _ := gn.volume.LookupNode(gn.path)
	if node == nil {
		// FIXME - Should detect internal errors and do it differently
		return nil, syscall.ENOENT
	}
	defer node.Release()

	children := node.Children()
	if children == nil {
		return nil, syscall.ENOTDIR
	}

	r := make([]fuse.DirEntry, 0, len(children))
	for name, node := range children {
		_, isDir := node.(*grits.TreeNode)
		var mode uint32
		if isDir {
			mode = fuse.S_IFDIR | 0o755
		} else {
			mode = fuse.S_IFREG | 0o644
		}

		d := fuse.DirEntry{
			Name: name,
			Mode: mode,
		}
		r = append(r, d)
	}

	sort.Slice(r, func(i, j int) bool {
		return r[i].Name < r[j].Name
	})

	return fs.NewListDirStream(r), 0
}

// Lookup should find a direct child of a directory by the child's name.  If
// the entry does not exist, it should return ENOENT and optionally
// set a NegativeTimeout in `out`. If it does exist, it should return
// attribute data in `out` and return the Inode for the child. A new
// inode can be created using `Inode.NewInode`. The new Inode will be
// added to the FS tree automatically if the return status is OK.
//
// If a directory does not implement NodeLookuper, the library looks
// for an existing child with the given name.
//
// The input to a Lookup is {parent directory, name string}.
//
// Lookup, if successful, must return an *Inode. Once the Inode is
// returned to the kernel, the kernel can issue further operations,
// such as Open or Getxattr on that node.
//
// A successful Lookup also returns an EntryOut. Among others, this
// contains file attributes (mode, size, mtime, etc.).
//
// FUSE supports other operations that modify the namespace. For
// example, the Symlink, Create, Mknod, Link methods all create new
// children in directories. Hence, they also return *Inode and must
// populate their fuse.EntryOut arguments.

var _ = (fs.NodeLookuper)((*gritsNode)(nil))

func (gn *gritsNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	fullPath := filepath.Join(gn.path, name)
	node, _ := gn.volume.LookupNode(fullPath)
	if node == nil {
		// FIXME - Detect internal errors
		return nil, syscall.ENOENT
	}
	defer node.Release()

	_, isDir := node.(*grits.TreeNode)
	var mode uint32
	if isDir {
		mode = fuse.S_IFDIR | 0o755
	} else {
		mode = fuse.S_IFREG | 0o644
	}

	stable := fs.StableAttr{
		Mode: mode,
	}
	operations := &gritsNode{
		volume: gn.volume,
		path:   fullPath,
	}

	// The NewInode call wraps the `operations` object into an Inode.
	return gn.NewInode(ctx, operations, stable), fs.OK
}

// GetAttr reads attributes for an Inode. The library will ensure that
// Mode and Ino are set correctly. For files that are not opened with
// FOPEN_DIRECTIO, Size should be set so it can be read correctly.  If
// returning zeroed permissions, the default behavior is to change the
// mode of 0755 (directory) or 0644 (files). This can be switched off
// with the Options.NullPermissions setting. If blksize is unset, 4096
// is assumed, and the 'blocks' field is set accordingly. The 'f'
// argument is provided for consistency, however, in practice the
// kernel never sends a file handle, even if the Getattr call
// originated from a fstat system call.

var _ = (fs.NodeGetattrer)((*gritsNode)(nil))

func (gn *gritsNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	node, _ := gn.volume.LookupNode(gn.path)
	if node == nil {
		return syscall.ENOENT
	}
	defer node.Release()

	out.Size = node.Address().Size

	_, isDir := node.(*grits.TreeNode)
	var mode uint32
	if isDir {
		mode = fuse.S_IFDIR | 0o755
	} else {
		mode = fuse.S_IFREG | 0o644
	}
	out.Mode = mode

	return fs.OK
}

type AttrForReference struct {
	Ino  uint64
	Size uint64

	// Blocks is the number of 512-byte blocks that the file occupies on disk.
	Blocks    uint64
	Atime     uint64
	Mtime     uint64
	Ctime     uint64
	Atimensec uint32
	Mtimensec uint32
	Ctimensec uint32
	Mode      uint32
	Nlink     uint32
	fuse.Owner
	Rdev uint32

	// Blksize is the preferred size for file system operations.
	Blksize uint32
	Padding uint32
}

// Open opens an Inode (of regular file type) for reading. It
// is optional but recommended to return a FileHandle.

var _ = (fs.NodeOpener)((*gritsNode)(nil))

func (gn *gritsNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	node, err := gn.volume.LookupNode(gn.path)
	if err != nil {
		return nil, 0, fs.ToErrno(err)
	}
	defer node.Release()

	blobFile, err := os.Open(node.ExportedBlob().Path)
	if err != nil {
		return nil, 0, fs.ToErrno(err)
	}
	//defer blobFile.Close()

	outFh := &FileHandle{node: gn}

	if flags&fuse.O_ANYWRITE != 0 && flags&uint32(os.O_TRUNC) != 0 {
		// Directly open the temp file instead of starting with the blob file
		var tmpFile *os.File
		tmpFile, err = os.CreateTemp("", "grits-modified-*")
		if err != nil {
			return nil, 0, fs.ToErrno(err)
		}
		outFh.file = tmpFile
		outFh.isDirty = true
	} else {
		// Start with the blob file and marked as clean

		outFh.file = blobFile
		blobFile = nil // Prevent deferred close
		outFh.isDirty = false
		outFh.cachedFile = node.ExportedBlob()
	}

	return outFh, 0, fs.OK
}

// Create is similar to Lookup, but should create a new
// child. It typically also returns a FileHandle as a
// reference for future reads/writes.
// Default is to return EROFS.

var _ = (fs.NodeCreater)((*gritsNode)(nil))

func (gn *gritsNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	fullPath := filepath.Join(gn.path, name)
	if flags&uint32(os.O_EXCL) != 0 {
		addr, _ := gn.volume.Lookup(fullPath)
		if addr != nil {
			return nil, nil, 0, syscall.EEXIST
		}
	}

	outFh := &FileHandle{}

	tmpFile, err := os.CreateTemp("", "grits-modified-*")
	if err != nil {
		return nil, nil, 0, fs.ToErrno(err)
	}
	outFh.file = tmpFile
	outFh.isDirty = true

	operations := &gritsNode{
		volume: gn.volume,
		path:   fullPath,
	}
	outFh.node = operations

	stable := fs.StableAttr{
		Mode: fuse.S_IFREG | 0o644,
	}

	// The NewInode call wraps the `operations` object into an Inode.
	return gn.NewInode(ctx, operations, stable), outFh, 0, fs.OK
}

// Reads data from a file. The data should be returned as
// ReadResult, which may be constructed from the incoming
// `dest` buffer. If the file was opened without FileHandle,
// the FileHandle argument here is nil. The default
// implementation forwards to the FileHandle.

var _ = (fs.NodeReader)((*gritsNode)(nil))

func (gn *gritsNode) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	handle, ok := f.(*FileHandle)
	if !ok {
		return nil, syscall.EBADF
	}

	handle.mtx.RLock()
	defer handle.mtx.RUnlock()

	n, err := handle.file.ReadAt(dest, off)
	if err != nil && err != io.EOF {
		return nil, fs.ToErrno(err)
	}

	// If EOF was reached, it's still a successful read, but n will be less than len(dest)
	return fuse.ReadResultData(dest[:n]), fs.OK
}

// Writes the data into the file handle at given offset. After
// returning, the data will be reused and may not referenced.
// The default implementation forwards to the FileHandle.

var _ = (fs.NodeWriter)((*gritsNode)(nil))

func (gn *gritsNode) Write(ctx context.Context, f fs.FileHandle, data []byte, off int64) (uint32, syscall.Errno) {
	handle, ok := f.(*FileHandle)
	if !ok {
		log.Printf("--- It's not a file descriptor!")
		return 0, syscall.EBADF
	}

	handle.mtx.Lock()
	defer handle.mtx.Unlock()

	if !handle.isDirty {
		tempFile, err := os.CreateTemp("", "grits-temp-")
		if err != nil {
			return 0, syscall.EIO
		}

		_, err = handle.file.Seek(0, 0)
		if err != nil {
			os.Remove(tempFile.Name())
			return 0, syscall.EIO
		}

		_, err = io.Copy(tempFile, handle.file)
		if err != nil {
			os.Remove(tempFile.Name())
			return 0, syscall.EIO
		}

		err = handle.file.Close()
		if err != nil {
			os.Remove(tempFile.Name())
			return 0, syscall.EIO
		}
		handle.file = tempFile

		handle.cachedFile.Release()
		handle.cachedFile = nil

		handle.isDirty = true
	}

	log.Printf("About to write...")

	n, err := handle.file.WriteAt(data, off)
	if err != nil {
		log.Printf("Error while writing")
		return 0, fs.ToErrno(err)
	}
	return uint32(n), fs.OK
}

// Flush is called for the close(2) call on a file descriptor. In case
// of a descriptor that was duplicated using dup(2), it may be called
// more than once for the same FileHandle.  The default implementation
// forwards to the FileHandle, or if the handle does not support
// FileFlusher, returns OK.
var _ = (fs.NodeFlusher)((*gritsNode)(nil))

func (gn *gritsNode) Flush(ctx context.Context, f fs.FileHandle) syscall.Errno {
	log.Printf("--- We're flushing!")

	handle, ok := f.(*FileHandle)
	if !ok {
		return syscall.EBADF
	}

	handle.mtx.Lock()
	defer handle.mtx.Unlock()

	if !handle.isDirty {
		return fs.OK
	}

	if handle.cachedFile != nil {
		log.Panicf("Can't happen, CF non nil")
	}

	cf, err := gn.volume.AddBlob(handle.file.Name())
	if err != nil {
		return syscall.EIO
	}

	err = os.Remove(handle.file.Name())
	if err != nil {
		return syscall.EIO
	}

	handle.cachedFile = cf
	handle.file, err = os.Open(cf.Path)
	if err != nil {
		log.Panicf("Couldn't open blob cache file!")
	}
	handle.isDirty = false

	typedAddr := grits.NewTypedFileAddr(cf.Address.Hash, cf.Address.Size, grits.Blob)
	log.Printf("--- We are linking %s to %s\n", handle.node.path, typedAddr.String())
	gn.volume.Link(handle.node.path, typedAddr)

	_, parent := gn.Parent()
	pathParts := strings.Split(gn.path, "/")
	parent.NotifyEntry(pathParts[len(pathParts)-1])

	return fs.OK
}

// This is called to before a FileHandle is forgotten. The
// kernel ignores the return value of this method,
// so any cleanup that requires specific synchronization or
// could fail with I/O errors should happen in Flush instead.
// The default implementation forwards to the FileHandle.

var _ = (fs.NodeReleaser)((*gritsNode)(nil))

func (gn *gritsNode) Release(ctx context.Context, f fs.FileHandle) syscall.Errno {
	handle, ok := f.(*FileHandle)
	if !ok {
		return syscall.EBADF
	}

	handle.mtx.Lock()
	defer handle.mtx.Unlock()

	if handle.isDirty {
		log.Panicf("Releasing not-closed file handle")
	}
	if handle.cachedFile == nil {
		log.Panicf("Releasing with no CF set")
	}
	if handle.file == nil {
		log.Panic("Releasing with no file set")
	}

	// Release the cachedFile
	handle.cachedFile.Release()

	// Close the file
	if err := handle.file.Close(); err != nil {
		return fs.ToErrno(err)
	}

	return fs.OK
}
