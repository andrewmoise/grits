package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"os"
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

	rootNode, err := volume.LookupNode("")
	if err != nil {
		return fmt.Errorf("can't find root in %s", mm.config.Volume)
	}

	root := &gritsNode{
		volume: volume,
		node:   rootNode,
	}

	mm.fsServer, err = fs.Mount(mntDir, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			// Set to true to see how the file system works.
			Debug:                    true,
			FsName:                   "grits",
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
	node   grits.FileNode
}

// FileHandle represents an open file. This will wrap os.File to handle file operations.
type FileHandle struct {
	file       *os.File
	cachedFile *grits.CachedFile
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
	children := gn.node.Children()
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
	children := gn.node.Children()
	if children == nil {
		return nil, syscall.ENOTDIR
	}

	child, exists := children[name]
	if !exists {
		return nil, syscall.ENOENT
	}

	_, isDir := child.(*grits.TreeNode)
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
		node:   child,
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
	out.Size = gn.node.Address().Size

	_, isDir := gn.node.(*grits.TreeNode)
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
	// Convert fuse flags to os flags
	var fFlags int
	if flags&fuse.O_ANYWRITE != 0 {
		return nil, 0, syscall.EROFS
	} else {
		fFlags = os.O_RDONLY
	}

	cf, err := gn.volume.ReadFile(gn.node.Address())
	if err != nil {
		return nil, 0, syscall.EIO
	}

	// Open the file at the given path
	file, err := os.OpenFile(cf.Path, fFlags, 0666)
	if err != nil {
		cf.Release()
		return nil, 0, fs.ToErrno(err)
	}

	outFh := &FileHandle{
		file:       file,
		cachedFile: cf,
	}

	return outFh, 0, fs.OK
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

	n, err := handle.file.ReadAt(dest, off)
	if err != nil && err != io.EOF {
		return nil, fs.ToErrno(err)
	}

	// If EOF was reached, it's still a successful read, but n will be less than len(dest)
	return fuse.ReadResultData(dest[:n]), fs.OK
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

	// Release the cachedFile
	handle.cachedFile.Release()

	// Close the file
	if err := handle.file.Close(); err != nil {
		return fs.ToErrno(err)
	}

	return fs.OK
}
