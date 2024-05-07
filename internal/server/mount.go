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

	volume       Volume
	inodeManager *InodeManager
}

func NewMountModule(config *MountModuleConfig, server *Server) *MountModule {
	return &MountModule{
		config:       config,
		gritsServer:  server,
		inodeManager: NewInodeManager(),
	}
}

func (*MountModule) GetModuleName() string {
	return "mount"
}

func (mm *MountModule) Start() error {
	mntDir := mm.config.MountPoint
	os.Mkdir(mntDir, 0755)

	var exists bool
	mm.volume, exists = mm.gritsServer.Volumes[mm.config.Volume]
	if !exists {
		return fmt.Errorf("can't open volume %s", mm.config.Volume)
	}

	root := &gritsNode{
		module: mm,
		path:   "",
	}

	var err error
	mm.fsServer, err = fs.Mount(mntDir, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			//Debug:                    true,
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
	module *MountModule
	path   string

	mtx      sync.Mutex // Protecting all the below
	refCount int        // Reference count of open FUSE file handles

	// If refCount > 0, then we also have file I/O information:
	file       *os.File         // Represents the actual file handle
	isTmpFile  bool             // True iff that's a handle to a writable temp file
	cachedFile grits.CachedFile // Immutable already-cached file info (if NOT a writeable temp file)
}

func newGritsNode(ctx context.Context, parent *fs.Inode, path string, mode uint32, module *MountModule) (*fs.Inode, *gritsNode, error) {
	operations := &gritsNode{
		module: module,
		path:   path,
	}
	stable := fs.StableAttr{
		Mode: mode,
		Ino:  module.inodeManager.AssignInode(path),
	}
	return parent.NewInode(ctx, operations, stable), operations, nil
}

/////
// Inode management

type InodeManager struct {
	sync.Mutex
	inodeMap  map[string]uint64
	lastInode uint64
}

func NewInodeManager() *InodeManager {
	return &InodeManager{
		inodeMap:  make(map[string]uint64),
		lastInode: 1,
	}
}

func (m *InodeManager) AssignInode(path string) uint64 {
	m.Lock()
	defer m.Unlock()

	if inode, exists := m.inodeMap[path]; exists {
		return inode
	}
	m.lastInode++
	m.inodeMap[path] = m.lastInode
	return m.lastInode
}

func (m *InodeManager) RemoveInode(path string) {
	m.Lock()
	defer m.Unlock()

	delete(m.inodeMap, path)
}

/////
// Structure reading

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
	node, _ := gn.module.volume.LookupNode(gn.path)
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
	//log.Printf("--- Looking up %s\n", fullPath)

	node, err := gn.module.volume.LookupNode(fullPath)
	if err != nil {
		//log.Printf("---   Error! %v\n", err)
		return nil, syscall.EIO
	}
	if node == nil {
		//log.Printf("---   Not found")
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

	newInode, _, err := newGritsNode(ctx, &gn.Inode, fullPath, mode, gn.module)
	if err != nil {
		return nil, syscall.EIO
	}

	out.Size = node.Address().Size
	out.Mode = mode
	out.Owner.Uid = ownerUid
	out.Owner.Gid = ownerGid

	return newInode, fs.OK
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

var ownerUid = uint32(syscall.Getuid())
var ownerGid = uint32(syscall.Getgid())

var _ = (fs.NodeGetattrer)((*gritsNode)(nil))

func (gn *gritsNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	//log.Printf("--- Getattr for %s\n", gn.path)

	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	if !gn.isTmpFile {
		node, _ := gn.module.volume.LookupNode(gn.path)
		if node == nil {
			return syscall.ENOENT
		}
		defer node.Release()

		out.Size = node.Address().Size
	} else if gn.file != nil {
		fileInfo, err := gn.file.Stat()
		if err != nil {
			return fs.ToErrno(err)
		}

		out.Size = uint64(fileInfo.Size())
	} else {
		return syscall.EIO
	}

	out.Mode = gn.Mode()
	out.Owner.Uid = ownerUid
	out.Owner.Gid = ownerGid

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

/* For later... */
func fillAttr(cacheFile string, out *fuse.AttrOut, isDir bool) error {
	fileInfo, err := os.Stat(cacheFile)
	if err != nil {
		return err
	}

	stat, ok := fileInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("failed to assert Stat_t")
	}

	out.Size = uint64(stat.Size)
	out.Atime = uint64(stat.Atim.Sec)
	out.Mtime = uint64(stat.Mtim.Sec)
	out.Ctime = uint64(stat.Ctim.Sec)
	out.Atimensec = uint32(stat.Atim.Nsec)
	out.Mtimensec = uint32(stat.Mtim.Nsec)
	out.Ctimensec = uint32(stat.Ctim.Nsec)

	if isDir {
		out.Mode = fuse.S_IFDIR | 0o755
	} else {
		out.Mode = fuse.S_IFREG | 0o644
	}

	out.Nlink = 1
	out.Uid = ownerUid
	out.Gid = ownerGid
	out.Rdev = 0

	return nil
}

/////
// File I/O

// FileHandle represents an open file. This will wrap os.File to handle file operations.
type FileHandle struct {
	isReadOnly bool
}

// Make sure we're okay to read from this file -- it is safe to call this if the file
// is already opened read/write; it will leave it as read/write without harming anything

func (gn *gritsNode) openCachedFile() syscall.Errno {
	if gn.cachedFile == nil {
		addr, err := gn.module.volume.Lookup(gn.path)
		if addr == nil {
			return syscall.ENOENT
		}
		if err != nil {
			return syscall.EIO
		}

		gn.cachedFile, err = gn.module.volume.ReadFile(addr)
		if err != nil {
			return syscall.EIO
		}
		gn.cachedFile.Take()
	}

	if gn.file == nil {
		var err error
		gn.file, err = os.Open(gn.cachedFile.GetPath())
		if err != nil {
			return fs.ToErrno(err)
		}

		gn.isTmpFile = false // Otherwise leave prev value alone
	}

	return fs.OK
}

// Make sure we're okay to read or write from this file
// Note - you do NOT need to call this on Open(); it's meant to be called from Write()
//
// truncLen == -1 means no truncation
//
// You need to call this either with truncLen == 0, or with a cachedFile
// all set up for it to copy the temp file from
func (gn *gritsNode) openTmpFile(truncLen int64) syscall.Errno {
	if gn.isTmpFile {
		// Easy case, we already have a temp file open; we just truncate if needed, and return
		if truncLen != -1 {
			err := gn.file.Truncate(truncLen)
			if err != nil {
				return fs.ToErrno(err)
			}
		}

		return fs.OK
	}

	// At this point, we know we need to set up a temp file

	tmpFile, err := os.CreateTemp("", "grits-temp-")
	if err != nil {
		return fs.ToErrno(err)
	}
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
		}
	}()

	// Check to see if we have a read-only file we need to copy in, and/or clean up

	if !gn.isTmpFile && gn.file != nil {
		// We have some read-only data already - nuke it (maybe copying some into the tmp file)

		//log.Printf("---     Truncating\n")

		if truncLen != 0 {
			_, err = gn.file.Seek(0, 0)
			if err != nil {
				return fs.ToErrno(err)
			}

			if truncLen == -1 {
				_, err = io.Copy(tmpFile, gn.file)
				if err != nil {
					return fs.ToErrno(err)
				}
			} else {
				n, err := io.CopyN(tmpFile, gn.file, truncLen)
				if err != nil {
					return fs.ToErrno(err)
				}
				if n != truncLen {
					return syscall.EIO
				}
			}
		}

		//log.Printf("---     Closing old file\n")

		err = gn.file.Close()
		if err != nil {
			log.Printf("Warning! Error when closing %s\n", gn.path)
		}
	}

	// Replace the data with the temp file we created

	if gn.cachedFile != nil {
		gn.cachedFile.Release()
		gn.cachedFile = nil
	}

	gn.file = tmpFile
	tmpFile = nil // Prevent cleanup
	gn.isTmpFile = true

	return fs.OK
}

// Write this file to the blob cache if needed, make sure our edits if any are saved

func (gn *gritsNode) flush() syscall.Errno {
	if gn.file == nil {
		return syscall.EIO
	}

	if !gn.isTmpFile {
		return fs.OK
	}

	tmpFile := gn.file.Name()
	defer os.Remove(tmpFile)

	err := gn.file.Close()
	gn.file = nil
	gn.isTmpFile = false
	if err != nil {
		return fs.ToErrno(err)
	}

	cf, err := gn.module.volume.AddBlob(tmpFile)
	if err != nil {
		return syscall.EIO
	}

	if gn.cachedFile != nil {
		log.Panicf("Can't happen - dirty file but CF is set")
	}
	gn.cachedFile = cf

	typedAddr := grits.NewTypedFileAddr(cf.GetAddress().Hash, cf.GetSize(), grits.Blob)
	//log.Printf("--- We are linking %s to %s\n", gn.path, typedAddr.String())
	err = gn.module.volume.Link(gn.path, typedAddr)

	//_, parent := gn.Parent()
	//pathParts := strings.Split(gn.path, "/")
	//errno := parent.NotifyEntry(pathParts[len(pathParts)-1])

	if err != nil {
		return syscall.EIO
	}
	//if errno != fs.OK && errno != syscall.ENOENT {
	//	return errno
	//}

	return fs.OK
}

// Wrap up all the I/O stuff for an inode; we're done with file I/O on it for the time being.
// Should have already flushed before calling this.

func (gn *gritsNode) finalize() syscall.Errno {
	if gn.cachedFile != nil {
		gn.cachedFile.Release()
		gn.cachedFile = nil
	}

	if gn.isTmpFile {
		log.Printf("Warning! Losing edits to %s because of previous errors", gn.path)
		gn.isTmpFile = false
	}

	if gn.file != nil {
		err := gn.file.Close()
		gn.file = nil
		if err != nil {
			return fs.ToErrno(err)
		}
	}

	return fs.OK
}

// Open opens an Inode (of regular file type) for reading. It
// is optional but recommended to return a FileHandle.

var _ = (fs.NodeOpener)((*gritsNode)(nil))

func (gn *gritsNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	if gn.refCount == 0 {
		var errno syscall.Errno
		if flags&uint32(os.O_TRUNC) == 0 {
			errno = gn.openCachedFile()
		} else {
			errno = gn.openTmpFile(0)
		}

		if errno != fs.OK {
			return nil, 0, errno
		}
	}
	gn.refCount++

	fh := &FileHandle{
		isReadOnly: flags&fuse.O_ANYWRITE == 0,
	}

	return fh, 0, fs.OK
}

// Create is similar to Lookup, but should create a new
// child. It typically also returns a FileHandle as a
// reference for future reads/writes.
// Default is to return EROFS.

var _ = (fs.NodeCreater)((*gritsNode)(nil))

func (gn *gritsNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	fullPath := filepath.Join(gn.path, name)
	if flags&uint32(os.O_EXCL) != 0 {
		addr, _ := gn.module.volume.Lookup(fullPath)
		if addr != nil {
			return nil, nil, 0, syscall.EEXIST
		}
	}

	// No lock -- we're the only one with a reference to this at this stage, so no point
	// hopefully

	newInode, operations, err := newGritsNode(ctx, &gn.Inode, fullPath, mode, gn.module)
	if err != nil {
		return nil, nil, 0, syscall.EIO
	}

	outFh := &FileHandle{
		isReadOnly: flags&fuse.O_ANYWRITE == 0,
	}

	errno := operations.openTmpFile(0)
	if errno != fs.OK {
		return nil, nil, 0, errno
	}

	operations.refCount++

	//errno = gn.NotifyEntry(name)
	//if errno != fs.OK && errno != syscall.ENOENT {
	//	return nil, nil, 0, errno
	//}

	return newInode, outFh, 0, fs.OK
}

// Reads data from a file. The data should be returned as
// ReadResult, which may be constructed from the incoming
// `dest` buffer. If the file was opened without FileHandle,
// the FileHandle argument here is nil. The default
// implementation forwards to the FileHandle.

var _ = (fs.NodeReader)((*gritsNode)(nil))

func (gn *gritsNode) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	errno := gn.openCachedFile()
	if errno != fs.OK {
		return nil, errno
	}

	n, err := gn.file.ReadAt(dest, off)
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
	//log.Printf("--- Writing at offset %d\n", off)

	handle, ok := f.(*FileHandle)
	if !ok {
		return 0, syscall.EBADF
	}

	if handle.isReadOnly {
		return 0, syscall.EACCES
	}

	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	//log.Printf("---   Opening tmp file\n")

	errno := gn.openTmpFile(-1)
	if errno != fs.OK {
		return 0, errno
	}

	//log.Printf("---   Writing\n")

	n, err := gn.file.WriteAt(data, off)
	if err != nil {
		return 0, fs.ToErrno(err)
	}

	return uint32(n), fs.OK
}

// Misc nonsense. Setattr() needs to be in here, in order to support setting size to 0 to truncate.

var _ = (fs.NodeSetattrer)((*gritsNode)(nil))

func (gn *gritsNode) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	//log.Printf("--- Setattr with FH %v\n", fh)

	// As currently implemented, we're only interested in changing the size.
	if in.Valid&fuse.FATTR_SIZE == 0 {
		return fs.OK
	}

	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	if gn.file == nil {
		errno := gn.openCachedFile()
		if errno != fs.OK {
			return errno
		}
	}

	info, err := gn.file.Stat()
	if err != nil {
		return syscall.EIO
	}

	fileSize := info.Size()

	if in.Size != uint64(fileSize) {
		errno := gn.openTmpFile(int64(in.Size))
		if errno != fs.OK {
			return errno
		}
	}

	return fs.OK
}

var _ = (fs.NodeFsyncer)((*gritsNode)(nil))

func (gn *gritsNode) Fsync(ctx context.Context, f fs.FileHandle, flags uint32) syscall.Errno {
	return fs.OK
}

// Flush is called for the close(2) call on a file descriptor. In case
// of a descriptor that was duplicated using dup(2), it may be called
// more than once for the same FileHandle.  The default implementation
// forwards to the FileHandle, or if the handle does not support
// FileFlusher, returns OK.
var _ = (fs.NodeFlusher)((*gritsNode)(nil))

func (gn *gritsNode) Flush(ctx context.Context, f fs.FileHandle) syscall.Errno {
	//log.Printf("--- We're flushing!")

	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	errno := gn.flush()
	if errno != fs.OK {
		return errno
	}

	return fs.OK
}

// This is called to before a FileHandle is forgotten. The
// kernel ignores the return value of this method,
// so any cleanup that requires specific synchronization or
// could fail with I/O errors should happen in Flush instead.
// The default implementation forwards to the FileHandle.

var _ = (fs.NodeReleaser)((*gritsNode)(nil))

func (gn *gritsNode) Release(ctx context.Context, f fs.FileHandle) syscall.Errno {
	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	flushErrno := gn.flush()

	gn.refCount--
	if gn.refCount == 0 {
		errno := gn.finalize()

		if errno != fs.OK {
			return errno
		}
	}

	if flushErrno != fs.OK {
		return flushErrno
	}

	return fs.OK
}

/////
// Directory and inode operations

// Mkdir is similar to Lookup, but must create a directory entry and Inode.
// Default is to return ENOTSUP.

var _ = (fs.NodeMkdirer)((*gritsNode)(nil))

func (gn *gritsNode) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	fullPath := filepath.Join(gn.path, name)
	emptyAddr := grits.NewTypedFileAddr("QmSvPd3sHK7iWgZuW47fyLy4CaZQe2DwxvRhrJ39VpBVMK", 2, grits.Tree)

	// Create the LinkRequest with the required details
	req := &grits.LinkRequest{
		Path:     fullPath,
		Addr:     emptyAddr,
		PrevAddr: nil,
		Assert:   grits.AssertPrevValueMatches,
	}

	err := gn.module.volume.MultiLink([]*grits.LinkRequest{req})
	if err != nil {
		// FIXME - distinguish 'already exists' from general internal error
		return nil, syscall.EIO
	}

	newInode, _, err := newGritsNode(ctx, &gn.Inode, fullPath, mode|fuse.S_IFDIR, gn.module)
	if err != nil {
		return nil, syscall.EIO
	}

	//errno := gn.NotifyEntry(name)
	//if errno != fs.OK {
	//	return nil, errno
	//}

	return newInode, fs.OK
}

// Unlink should remove a child from this directory.  If the
// return status is OK, the Inode is removed as child in the
// FS tree automatically. Default is to return success.

var _ = (fs.NodeUnlinker)((*gritsNode)(nil))

func (gn *gritsNode) Unlink(ctx context.Context, name string) syscall.Errno {
	fullPath := filepath.Join(gn.path, name)

	// Create the LinkRequest with the required details
	req := &grits.LinkRequest{
		Path:   fullPath,
		Addr:   nil,
		Assert: grits.AssertIsNonEmpty | grits.AssertIsBlob,
	}

	err := gn.module.volume.MultiLink([]*grits.LinkRequest{req})
	if err != nil {
		return syscall.EIO
	}

	return fs.OK
}

// Rmdir is like Unlink but for directories.
// Default is to return success.

var _ = (fs.NodeRmdirer)((*gritsNode)(nil))

func (gn *gritsNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	fullPath := filepath.Join(gn.path, name)

	// Create the LinkRequest with the required details
	req := &grits.LinkRequest{
		Path:   fullPath,
		Addr:   nil,
		Assert: grits.AssertIsNonEmpty | grits.AssertIsTree,
	}

	err := gn.module.volume.MultiLink([]*grits.LinkRequest{req})
	if err != nil {
		return syscall.EIO
	}

	return fs.OK
}

// Rename should move a child from one directory to a different
// one. The change is effected in the FS tree if the return status is
// OK. Default is to return ENOTSUP.

var _ = (fs.NodeRmdirer)((*gritsNode)(nil))

func (gn *gritsNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	fullPath := filepath.Join(gn.path, name)

	newGritsNode, ok := newParent.(*gritsNode)
	if !ok {
		return syscall.EIO
	}
	newFullPath := filepath.Join(newGritsNode.path, newName)

	addr, err := gn.module.volume.Lookup(fullPath)
	if err != nil {
		return syscall.EIO
	}
	if addr == nil {
		return syscall.ENOENT
	}

	// Create the LinkRequest with the required details
	oldNameReq := &grits.LinkRequest{
		Path:     fullPath,
		Addr:     nil,
		PrevAddr: addr,
		Assert:   grits.AssertPrevValueMatches,
	}

	newNameReq := &grits.LinkRequest{
		Path:     newFullPath,
		Addr:     addr,
		PrevAddr: nil,
		Assert:   grits.AssertPrevValueMatches,
	}

	err = gn.module.volume.MultiLink([]*grits.LinkRequest{oldNameReq, newNameReq})
	if err != nil {
		return syscall.EINVAL
	}

	return fs.OK
}
