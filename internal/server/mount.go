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
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

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

	dirtyNodesMtx sync.RWMutex
	dirtyNodesMap map[string]*gritsNode
}

func NewMountModule(server *Server, config *MountModuleConfig) *MountModule {
	return &MountModule{
		config:        config,
		gritsServer:   server,
		inodeManager:  NewInodeManager(),
		dirtyNodesMap: make(map[string]*gritsNode),
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

	// Root is always inode 1 in FUSE
	mm.inodeManager.Lock()
	mm.inodeManager.inodeMap[""] = 1
	mm.inodeManager.Unlock()

	var err error
	mm.fsServer, err = fs.Mount(mntDir, root, &fs.Options{
		MountOptions: fuse.MountOptions{
			Debug:                    grits.DebugFuse,
			Name:                     "grits",
			DisableXAttrs:            true,
			ExplicitDataCacheControl: true,
			//AllowOther:               true,
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

	// First attempt to unmount
	err := mm.fsServer.Unmount()
	if err != nil {
		log.Printf("==========")
		log.Printf("FAILED to unmount: %v", err)
		log.Printf("Please close open files, and unmount by hand.")
		log.Printf("==========")
	} else {
		log.Printf("Successfully unmounted %s", mm.config.MountPoint)
	}

	// Wait until unmount completes
	mm.fsServer.Wait()
	return nil
}

/////
// FUSE stuff

type gritsNode struct {
	fs.Inode
	module *MountModule
	mtx    sync.Mutex // Protecting all the below

	path     string
	refCount int // Reference count of open FUSE file handles

	// These are mutually exclusive:
	tmpFile     *os.File             // If dirty, the temp file
	tmpMetadata *grits.GNodeMetadata // If dirty (and content hash + size in this are not valid)
	// and:
	cachedFile     grits.CachedFile     // If clean, the backing file for contents from the blob store
	cachedMetadata *grits.GNodeMetadata // If clean, the metadata

	// This is the open FH, in both cases, meaning you need to close + reopen it when you're switching:
	openFile io.ReadSeekCloser

	// Odd corner case: If this file is dirty, and has been unlinked already or something
	isDangling bool
}

func newGritsNode(ctx context.Context, parent *fs.Inode, path string, mode uint32, module *MountModule) (*fs.Inode, *gritsNode, error) {
	operations := &gritsNode{
		module:     module,
		path:       path,
		isDangling: false,
	}
	stable := fs.StableAttr{
		Mode: mode,
		Ino:  module.inodeManager.AssignInode(path),
	}
	return parent.NewInode(ctx, operations, stable), operations, nil
}

/////
// Dirty node tracking

// Track a node as dirty
func (mm *MountModule) addDirtyNode(path string, node *gritsNode) {
	mm.dirtyNodesMtx.Lock()
	defer mm.dirtyNodesMtx.Unlock()
	mm.dirtyNodesMap[path] = node

	if grits.DebugFuse {
		log.Printf("Added dirty node. Full list:")
		for path := range mm.dirtyNodesMap {
			log.Printf("  %s", path)
		}
	}
}

// Remove a node from the dirty list
func (mm *MountModule) removeDirtyNode(path string) {
	mm.dirtyNodesMtx.Lock()
	defer mm.dirtyNodesMtx.Unlock()
	delete(mm.dirtyNodesMap, path)

	if grits.DebugFuse {
		log.Printf("Removed dirty node. Full list after:")
		for path := range mm.dirtyNodesMap {
			log.Printf("  %s", path)
		}
	}
}

// Check if a path exists in the dirty nodes map
func (mm *MountModule) getDirtyNode(path string) *gritsNode {
	mm.dirtyNodesMtx.RLock()
	defer mm.dirtyNodesMtx.RUnlock()
	return mm.dirtyNodesMap[path]
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
	// FIXME: Lock gn

	node, err := gn.module.volume.LookupNode(gn.path)
	if grits.IsNotExist(err) {
		return nil, syscall.ENOENT
	} else if err != nil {
		return nil, syscall.EIO
	}
	defer node.Release()

	children := node.Children()
	if children == nil {
		return nil, syscall.ENOTDIR
	}

	// Build the initial result set from the underlying storage
	r := make([]fuse.DirEntry, 0, len(children))
	existingNames := make(map[string]struct{}) // Track existing names to avoid duplicates

	for name, nodeAddr := range children {
		childNode, err := gn.module.volume.GetFileNode(nodeAddr)
		if grits.DebugFuse {
			log.Printf("Loaded child node %s", name)
			log.Printf("  metadata: %v", childNode.Metadata())
		}
		if err != nil {
			log.Printf("Readdir fail %v", err)
			return nil, syscall.EIO
		}
		defer childNode.Release()

		_, isDir := childNode.(*grits.TreeNode)
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
		existingNames[name] = struct{}{} // Mark this name as existing
	}

	// Now check the dirty nodes map for additional entries
	gn.module.dirtyNodesMtx.RLock()
	prefix := gn.path
	if prefix != "/" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	for dirtyPath, dirtyNode := range gn.module.dirtyNodesMap {
		// Skip if this isn't a direct child of the current directory
		if !strings.HasPrefix(dirtyPath, prefix) {
			continue
		}

		// Skip if already deleted
		if dirtyNode.isDangling {
			continue
		}

		// Extract name (remaining path after prefix)
		remainingPath := dirtyPath[len(prefix):]

		// Skip if this contains any subdirectories
		if strings.Contains(remainingPath, "/") {
			continue
		}

		// Skip if we already have this entry from the underlying storage
		if _, exists := existingNames[remainingPath]; exists {
			continue
		}

		// Add this entry to the results
		d := fuse.DirEntry{
			Name: remainingPath,
			Mode: dirtyNode.tmpMetadata.Mode,
		}
		r = append(r, d)
	}
	gn.module.dirtyNodesMtx.RUnlock()

	// Sort the combined results
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
	// FIXME - What if multiple Lookup() calls return multiple gritsNodes, but then someone
	// notices that their inodes are different? Or what if one of them is written and becomes
	// dirty, and the others are not?

	fullPath := filepath.Join(gn.path, name)
	if grits.DebugFuse {
		log.Printf("--- Looking up %s %s => %s\n", gn.path, name, fullPath)
	}

	// First check if this path is in the dirty nodes map
	gn.module.dirtyNodesMtx.Lock()
	if dirtyNode := gn.module.dirtyNodesMap[fullPath]; dirtyNode != nil {
		// We found a dirty node, use its inode directly
		dirtyNode.mtx.Lock()
		defer dirtyNode.mtx.Unlock()
		gn.module.dirtyNodesMtx.Unlock()

		if dirtyNode.isDangling {
			log.Printf("Can't happen - node %s is dangling but in dirty map", fullPath)
			return nil, syscall.EIO
		}

		out.Size = 0 // Will be determined based on the dirty file
		out.Mode = dirtyNode.tmpMetadata.Mode
		out.Owner.Uid = ownerUid
		out.Owner.Gid = ownerGid

		// If the node has a tmpFile, get its size
		if dirtyNode.tmpFile != nil {
			stat, err := dirtyNode.tmpFile.Stat()
			if err == nil {
				out.Size = uint64(stat.Size())
			}
		}

		if grits.DebugFuse {
			log.Printf("  Return result %v for %s", &dirtyNode.Inode, name)
		}
		return &dirtyNode.Inode, fs.OK
	} else {
		gn.module.dirtyNodesMtx.Unlock()
	}

	node, err := gn.module.volume.LookupNode(fullPath)
	if grits.IsNotExist(err) {
		//log.Printf("---   lookup Not found")
		return nil, syscall.ENOENT
	} else if err != nil {
		//log.Printf("---   lookup Error! %v\n", err)
		return nil, syscall.EIO
	}
	defer node.Release()

	mode := node.Metadata().Mode
	_, isDir := node.(*grits.TreeNode)
	if isDir {
		mode |= fuse.S_IFDIR
	} else {
		mode |= fuse.S_IFREG
	}

	newInode, _, err := newGritsNode(ctx, &gn.Inode, fullPath, mode, gn.module)
	if err != nil {
		log.Printf("NGN fail %v", err)
		return nil, syscall.EIO
	}

	out.Size = uint64(node.ExportedBlob().GetSize())
	out.Mode = mode
	out.Owner.Uid = ownerUid
	out.Owner.Gid = ownerGid

	if grits.DebugFuse {
		log.Printf("  New clean result %v for %s", newInode, fullPath)
	}

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
	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	if grits.DebugFuse {
		log.Printf("Getattr")
	}

	if gn.tmpFile == nil {
		if grits.DebugFuse {
			log.Printf("  no tmpfile")
		}
		node, err := gn.module.volume.LookupNode(gn.path)
		if grits.IsNotExist(err) {
			if grits.DebugFuse {
				log.Printf("    no lookup success on %s", gn.path)
			}
			return syscall.ENOENT
		} else if err != nil {
			if grits.DebugFuse {
				log.Printf("    internal error")
			}
			return syscall.EIO
		}
		defer node.Release()

		out.Size = uint64(node.Address().Size)

		// Use metadata for mode if available
		metadata := node.Metadata()
		if grits.DebugFuse {
			log.Printf("  got metadata: %v", metadata)
		}

		if metadata != nil && metadata.Mode != 0 {
			if grits.DebugFuse {
				log.Printf("  mode read as %d", metadata.Mode)
			}
			out.Mode = metadata.Mode
		} else {
			out.Mode = 0 // Er... hopefully this doesn't happen.
		}

		if metadata.Type == grits.GNodeTypeDirectory {
			out.Mode |= fuse.S_IFDIR
		} else if metadata.Type == grits.GNodeTypeFile {
			out.Mode |= fuse.S_IFREG
		} else {
			log.Printf("Unrecognized type %d for %s", int(metadata.Type), gn.path)
		}

		// Set timestamps if available
		if metadata != nil && metadata.Timestamp != "" {
			if grits.DebugFuse {
				log.Printf("  metadata timestamp is %s", metadata.Timestamp)
			}
			if modTime, err := time.Parse(time.RFC3339, metadata.Timestamp); err == nil {
				out.Mtime = uint64(modTime.Unix())
				out.Mtimensec = uint32(modTime.Nanosecond())
				if grits.DebugFuse {
					log.Printf("    parse to %d", out.Mtime)
				}
			}
		}
	} else if gn.openFile != nil {
		if grits.DebugFuse {
			log.Printf("  no open file")
		}

		size, err := gn.openFile.Seek(0, io.SeekEnd)
		if err != nil {
			if grits.DebugFuse {
				log.Printf("    internal error")
			}

			log.Printf("Seek fail %v", err)
			return syscall.EIO
		}
		out.Size = uint64(size)
		out.Mode = gn.Mode()

		// For dirty files, use current time for timestamps
		now := time.Now()
		out.Mtime = uint64(now.Unix())
		out.Mtimensec = 0
		out.Ctime = out.Mtime
		out.Ctimensec = out.Mtimensec
		out.Atime = out.Mtime
		out.Atimensec = out.Mtimensec
	} else {
		log.Printf("  7 GN fail")
		return syscall.EIO
	}

	// Set owner regardless of path taken above
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

// FIXME - Merge this with the dirty file map? Something?
type FileHandle struct {
	isReadOnly bool
}

// Make sure we're okay to read from this file -- it is safe to call this if the file
// is already opened read/write; it will leave it as read/write without harming anything

func (gn *gritsNode) openCachedFile() syscall.Errno {
	if gn.cachedFile == nil {
		if gn.tmpFile != nil {
			return fs.OK
		}

		fileNode, err := gn.module.volume.LookupNode(gn.path)
		if grits.IsNotExist(err) {
			return syscall.ENOENT
		} else if err != nil {
			log.Printf("1 OCF fail %v", err)
			return syscall.EIO
		}
		defer fileNode.Release()

		gn.cachedFile = fileNode.ExportedBlob()
		gn.cachedFile.Take()

		gn.cachedMetadata = fileNode.Metadata()
	}

	if gn.openFile == nil {
		var err error
		gn.openFile, err = gn.cachedFile.Reader()
		if err != nil {
			return fs.ToErrno(err)
		}
	}

	return fs.OK
}

// Make sure we're okay to read or write from this file
// Note - you do NOT need to call this on Open(); it's meant to be called from Write()
//
// truncLen == -1 means no truncation
//
// You need to have the gn mutex held when calling this
//
// You need to have the cached file set up when you call this, if there is one; otherwise it'll create a
// new empty file for you to write to

func (gn *gritsNode) openTmpFile(truncLen int64) syscall.Errno {
	wasDirty := (gn.tmpFile != nil)

	if gn.tmpFile != nil {
		// Truncate if required
		if truncLen != -1 {
			err := gn.tmpFile.Truncate(truncLen)
			if err != nil {
				return fs.ToErrno(err)
			}
		}
		return fs.OK
	}

	// Set up a new temp file
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

	if grits.DebugFuse {
		log.Printf("Ready to set up - %s (TL %d)", gn.path, truncLen)
	}

	// Either copy metadata and contents from the previous file, or else make up some for a
	// new blank file

	if gn.cachedFile != nil {
		// Copy stuff from the cached file

		_, err = gn.openFile.Seek(0, 0)
		if err != nil {
			return fs.ToErrno(err)
		}

		if truncLen == -1 {
			truncLen = gn.cachedFile.GetSize()
		}

		n, err := io.CopyN(tmpFile, gn.openFile, truncLen)
		if err != nil {
			return fs.ToErrno(err)
		}
		if n != truncLen {
			log.Printf("TL fail %d != %d", n, truncLen)
			return syscall.EIO
		}

		//log.Printf("Ready to check cached MD")

		if gn.cachedMetadata == nil {
			log.Printf("Null metadata opening tmp file for %s", gn.path)
			return syscall.EIO
		}

		// Copy the metadata we had before.
		tmpMetadata := *gn.cachedMetadata
		gn.tmpMetadata = &tmpMetadata
		gn.cachedMetadata = nil

		gn.cachedFile.Release()
		gn.cachedFile = nil
	} else {
		// Create all new stuff
		tmpMetadata := grits.GNodeMetadata{
			Type: grits.GNodeTypeFile,
			Mode: 0644,
		}
		gn.tmpMetadata = &tmpMetadata
	}

	if grits.DebugFuse {
		log.Printf("Done - MD %v", gn.tmpMetadata)
	}

	gn.openFile = tmpFile
	gn.tmpFile = tmpFile

	tmpFile = nil // Prevent cleanup, now that we know we succeeded

	// Do we need to hold a global lock when calling this? In order to be safe? (FIXME - check)
	if !wasDirty {
		gn.module.addDirtyNode(gn.path, gn)
	}

	if grits.DebugFuse {
		log.Printf("All done")
	}

	return fs.OK
}

// Write this file to the blob cache if needed, make sure our edits if any are saved
//
// You need to hold the lock on gn when calling this

func (gn *gritsNode) flush() syscall.Errno {
	if gn.openFile == nil || gn.tmpFile == nil {
		return fs.OK
	}

	tmpFile := gn.tmpFile

	defer func() {
		err := tmpFile.Close()
		gn.tmpFile = nil
		if err != nil {
			log.Printf("Error closing tmp file %s! %v", gn.path, err)
		}

		// FIXME - A lot of other places need to handle openFile being nil, in case of
		// error from here
		if gn.cachedFile != nil {
			gn.openFile, err = gn.cachedFile.Reader()
			if err != nil {
				log.Printf("Error opening up cached file %s: %v", gn.path, err)
			}
		} else {
			gn.openFile = nil
		}

		os.Remove(tmpFile.Name())
	}()

	if gn.isDangling {
		// Er.. hopefully we don't care, then? Have we actually masked out all other places where
		// we might need to be ignoring this file?
		log.Printf("Warning - flush() on dangling file %s", gn.path)
		return fs.OK
	}

	//log.Printf("Ready to do blob store stuff")

	contentBlob, err := gn.module.volume.AddOpenBlob(gn.tmpFile)
	if err != nil {
		log.Printf("Couldn't add content blob for %s: %v", gn.path, err)
		return syscall.EIO
	}
	defer contentBlob.Release()

	gn.tmpMetadata.ContentAddr = contentBlob.GetAddress().String()
	gn.tmpMetadata.Size = contentBlob.GetSize()

	if grits.DebugFuse {
		log.Printf("All set up tmpMetadata for flush: %v", gn.tmpMetadata)
	}

	metadataBlob, err := gn.module.volume.AddMetadataBlob(gn.tmpMetadata)
	if err != nil {
		log.Printf("Error adding metadata for %s - %v", gn.path, err)
		return syscall.EIO
	}
	defer metadataBlob.Release()

	if grits.DebugFuse {
		log.Printf("Ready to make request (metadata addr is %s", metadataBlob.GetAddress().String())
	}

	req := &grits.LinkRequest{
		Path:    gn.path,
		NewAddr: metadataBlob.GetAddress(),
	}

	err = gn.module.volume.MultiLink([]*grits.LinkRequest{req})
	if err != nil {
		return fs.ToErrno(err)
	}

	gn.cachedFile = contentBlob
	gn.cachedFile.Take()
	gn.tmpFile = nil

	// Fix up metadata
	gn.cachedMetadata = gn.tmpMetadata
	gn.tmpMetadata = nil

	// Node is now clean, remove from dirty map
	gn.module.removeDirtyNode(gn.path)
	return fs.OK
}

// Wrap up all the I/O stuff for an inode; we're done with file I/O on it for the time being.
// Should have already flushed before calling this.

func (gn *gritsNode) finalize() syscall.Errno {
	if gn.cachedFile != nil {
		gn.cachedFile.Release()
		gn.cachedFile = nil
	}

	if gn.tmpFile != nil {
		log.Printf("Warning! Losing edits to %s because of previous errors", gn.path)

		tmpFile := gn.tmpFile.Name()

		err := gn.tmpFile.Close()
		if err != nil {
			log.Printf("Error closing tmp file %s! %v", gn.path, err)
		}

		err = os.Remove(tmpFile)
		if err != nil {
			log.Printf("Error deleting %s tmp file: %v", gn.path, err)
		}

		gn.tmpFile = nil
		gn.openFile = nil
	}

	if gn.openFile != nil {
		err := gn.openFile.Close()
		gn.openFile = nil
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
		errno := gn.openCachedFile()
		if errno != fs.OK {
			log.Printf("Can't open file %s: %d", gn.path, errno)
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
	//log.Printf("Create(%s)", fullPath)
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
		log.Printf("NGN fail %v", err)
		return nil, nil, 0, syscall.EIO
	}

	outFh := &FileHandle{
		isReadOnly: flags&fuse.O_ANYWRITE == 0,
	}

	errno := operations.openTmpFile(0)
	if errno != fs.OK {
		log.Printf("OTF fail %d", errno)
		return nil, nil, 0, errno
	}

	operations.refCount++

	operations.tmpMetadata.Mode = mode

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

	// FIXME - This definitely seems like it will malfunction on a dirtty file

	errno := gn.openCachedFile()
	if errno != fs.OK {
		return nil, errno
	}

	_, err := gn.openFile.Seek(off, io.SeekStart)
	if err != nil {
		return nil, fs.ToErrno(err)
	}

	n, err := gn.openFile.Read(dest)
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
		return 0, syscall.EBADF
	}

	if handle.isReadOnly {
		return 0, syscall.EACCES
	}

	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	errno := gn.openTmpFile(-1)
	if errno != fs.OK {
		return 0, errno
	}

	n, err := gn.tmpFile.WriteAt(data, off)
	if err != nil {
		return 0, fs.ToErrno(err)
	}

	// FIXME er... maybe store this as in int internally.
	gn.tmpMetadata.Timestamp = grits.CreateTimestamp()

	return uint32(n), fs.OK
}

/////
// Misc nonsense

var _ = (fs.NodeSetattrer)((*gritsNode)(nil))

func (gn *gritsNode) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	// Check what's being modified
	sizeChanged := in.Valid&fuse.FATTR_SIZE != 0
	modeChanged := in.Valid&fuse.FATTR_MODE != 0
	timestampChanged := in.Valid&fuse.FATTR_MTIME != 0

	if !sizeChanged && !modeChanged && !timestampChanged {
		// Oh well... hopefully, whatever the user was requesting wasn't all that important.
		return fs.OK
	}

	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	errno := gn.openCachedFile()
	if errno != fs.OK {
		log.Printf("Couldn't open cached file for Setattr on %s: %d", gn.path, errno)
		return errno
	}

	if sizeChanged {
		errno = gn.openTmpFile(int64(in.Size))
	} else {
		errno = gn.openTmpFile(-1)
	}
	if errno != fs.OK {
		log.Printf("Couldn't open tmp file for %s: %d", gn.path, errno)
		return syscall.EIO
	}

	// At this point, we're dirty, so we can just make the changes we need.

	// Size in metadata is ignored; the actual tmpFile size is what's authoritative. The other two,
	// we actually need to update.

	if modeChanged {
		gn.tmpMetadata.Mode = in.Mode
	}

	if timestampChanged {
		// Er... surely there is a better way.
		t := time.Unix(int64(in.Mtime), int64(in.Mtimensec))
		gn.tmpMetadata.Timestamp = t.UTC().Format(time.RFC3339)
	}

	errno = gn.flush() // Since we might not be open for write, we need to commit the change
	if errno != fs.OK {
		log.Printf("Problem flushing %s after Setattr() - %d", gn.path, errno)
		return errno
	}

	return fs.OK
}

var _ = (fs.NodeFsyncer)((*gritsNode)(nil))

func (gn *gritsNode) Fsync(ctx context.Context, f fs.FileHandle, flags uint32) syscall.Errno {
	log.Printf("--- We're fsyncing")

	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	errno := gn.flush()
	if errno != fs.OK {
		return errno
	}

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
	//log.Printf("Mkdir() called for %s with mode %o", name, mode)

	// FIXME - Empty dirs need timestamps, too

	fullPath := filepath.Join(gn.path, name)
	emptyAddr := gn.module.volume.GetEmptyDirMetadataAddr()

	req := &grits.LinkRequest{
		Path:     fullPath,
		NewAddr:  emptyAddr,
		PrevAddr: nil,
		Assert:   grits.AssertPrevValueMatches,
	}

	err := gn.module.volume.MultiLink([]*grits.LinkRequest{req})
	if grits.IsAssertionFailed(err) {
		return nil, syscall.EEXIST
	} else if err != nil {
		log.Printf("ML fail %v", err)
		return nil, syscall.EIO
	}

	newInode, _, err := newGritsNode(ctx, &gn.Inode, fullPath, mode|fuse.S_IFDIR, gn.module)
	if err != nil {
		log.Printf("3 NGN fail %v", err)
		return nil, syscall.EIO
	}

	return newInode, fs.OK
}

// Unlink should remove a child from this directory.  If the
// return status is OK, the Inode is removed as child in the
// FS tree automatically. Default is to return success.

var _ = (fs.NodeUnlinker)((*gritsNode)(nil))

func (gn *gritsNode) Unlink(ctx context.Context, name string) syscall.Errno {
	gn.mtx.Lock()
	defer gn.mtx.Unlock()

	if gn.isDangling {
		// Pretty sure this can't happen -- it's already been unlinked in this case
		return syscall.EIO
	}

	fullPath := filepath.Join(gn.path, name)

	// Create the LinkRequest with the required details
	req := &grits.LinkRequest{
		Path:    fullPath,
		NewAddr: nil,
		Assert:  grits.AssertIsNonEmpty | grits.AssertIsBlob,
	}

	err := gn.module.volume.MultiLink([]*grits.LinkRequest{req})
	if err != nil {
		log.Printf("4 NGN fail %v", err)
		return syscall.EIO
	}

	gn.module.removeDirtyNode(fullPath)

	return fs.OK
}

// Rmdir is like Unlink but for directories.
// Default is to return success.

var _ = (fs.NodeRmdirer)((*gritsNode)(nil))

func (gn *gritsNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	// FIXME - Need to lock?

	fullPath := filepath.Join(gn.path, name)

	dirNode, err := gn.module.volume.LookupNode(fullPath)
	if grits.IsNotExist(err) {
		return syscall.EEXIST
	} else if err != nil {
		return syscall.EIO
	}
	defer dirNode.Release()

	// FIXME - What about "{ }" or something?
	if dirNode.Metadata().Size != 2 {
		return syscall.ENOTEMPTY // Only for compatibility With Unix expectations
	}

	// Create the LinkRequest with the required details
	req := &grits.LinkRequest{
		Path:     fullPath,
		NewAddr:  nil,
		PrevAddr: dirNode.MetadataBlob().GetAddress(),
		Assert:   grits.AssertIsTree | grits.AssertPrevValueMatches,
	}

	err = gn.module.volume.MultiLink([]*grits.LinkRequest{req})
	if err != nil {
		log.Printf("5 NGN fail %v", err)
		return syscall.EIO
	}

	return fs.OK
}

// Rename should move a child from one directory to a different
// one. The change is effected in the FS tree if the return status is
// OK. Default is to return ENOTSUP.

var _ = (fs.NodeRenamer)((*gritsNode)(nil))

func (gn *gritsNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	if grits.DebugFuse {
		log.Printf("Rename (in %s): %s to %s", gn.path, name, newName)
	}

	fullPath := filepath.Join(gn.path, name)

	newGritsNode, ok := newParent.(*gritsNode)
	if !ok {
		log.Printf("Rename fail - new parent not a gritsNode")
		return syscall.EIO
	}
	fullNewPath := filepath.Join(newGritsNode.path, newName)

	gn.module.dirtyNodesMtx.Lock()

	if grits.DebugFuse {
		log.Printf("  dirty nodes lock")
		log.Printf("  look for %s in:", fullPath)
		for path, _ := range gn.module.dirtyNodesMap {
			log.Printf("    %s", path)
		}
	}

	dirtyNode, exists := gn.module.dirtyNodesMap[fullPath]
	if exists {
		// Source is dirty, so it's complex

		log.Printf("  dirty node lock")

		dirtyNode.mtx.Lock()
		dirtyNode.path = fullNewPath

		err := gn.module.volume.Link(fullPath, nil)
		if grits.IsNotDir(err) || grits.IsNotExist(err) {
			// Having an error is actually fine here; we don't care too much about the source
			// as it may not exist yet in the merkle tree. As long as it's not some internal
			// issue, we're fine.
		} else if err != nil {
			dirtyNode.mtx.Unlock()
			gn.module.dirtyNodesMtx.Unlock()
			log.Printf("Weird error on rename %s -> %s: %v", fullPath, fullNewPath, err)
			return syscall.EIO
		}

		// Note! if there's already something there, in the dirty map, we may make it
		// unreachable. That should be fine, FUSE can still find it and so we'll wind up
		// flushing it at some point, and then we're done with it.
		gn.module.dirtyNodesMap[fullNewPath] = dirtyNode
		delete(gn.module.dirtyNodesMap, fullPath)

		dirtyNode.mtx.Unlock()
		gn.module.dirtyNodesMtx.Unlock()

	} else {
		gn.module.dirtyNodesMtx.Unlock()

		// Source is clean, we do merkle tree operations

		if grits.DebugFuse {
			log.Printf("  lookup node")
		}

		prevNode, err := gn.module.volume.LookupNode(fullPath)
		if grits.IsNotExist(err) {
			return syscall.ENOENT
		} else if err != nil {
			log.Printf("Error on rename: %v", err)
			return syscall.EIO
		}

		oldNameReq := &grits.LinkRequest{
			Path:     fullPath,
			NewAddr:  nil,
			PrevAddr: prevNode.MetadataBlob().GetAddress(),
			Assert:   grits.AssertPrevValueMatches,
		}

		newNameReq := &grits.LinkRequest{
			Path:    fullNewPath,
			NewAddr: prevNode.MetadataBlob().GetAddress(),
			//PrevAddr: nil, // this is for refusing to overwrite an existing thing
			//Assert:   grits.AssertPrevValueMatches,
		}

		err = gn.module.volume.MultiLink([]*grits.LinkRequest{oldNameReq, newNameReq})
		prevNode.Release()
		if grits.IsNotDir(err) {
			return syscall.ENOTDIR
		} else if grits.IsAssertionFailed(err) {
			return syscall.EINVAL
		} else if err != nil {
			log.Printf("Problems doing rename of %s to %s: %v", fullPath, fullNewPath, err)
			// Internal error? Of some kind?
			return syscall.EIO
		}
	}

	// Try to get the node that's being renamed
	childInode := gn.GetChild(name)
	var childNode *gritsNode
	if childInode != nil {
		if cn, ok := childInode.Operations().(*gritsNode); ok {
			childNode = cn
			// Update the path in the node itself
			childNode.mtx.Lock()
			childNode.path = fullNewPath
			childNode.mtx.Unlock()
			if grits.DebugFuse {
				log.Printf("Updated path for node %s to %s", name, fullNewPath)
			}
		}
	}

	// Okay, if we got to this point, we succeeded. Need to update our inode manager
	// that things have changed.

	if grits.DebugFuse {
		log.Printf("  inode manager lock")
	}

	gn.module.inodeManager.Lock()
	gn.module.inodeManager.inodeMap[fullNewPath] = gn.module.inodeManager.inodeMap[fullPath]
	oldParentInode, oldParentExists := gn.module.inodeManager.inodeMap[gn.path]
	newParentInode, newParentExists := gn.module.inodeManager.inodeMap[newGritsNode.path]
	oldChildInode, oldChildExists := gn.module.inodeManager.inodeMap[fullPath]
	delete(gn.module.inodeManager.inodeMap, fullPath)
	gn.module.inodeManager.Unlock()

	if !oldParentExists {
		log.Printf("Can't find old parent in inode map for %s", gn.path)
		return syscall.EIO
	}
	if !oldChildExists {
		log.Printf("Can't find old child in inode map for %s", fullPath)
		return syscall.EIO
	}
	if !newParentExists {
		log.Printf("Can't find new parent in inode map for %s", fullNewPath)
		return syscall.EIO
	}

	go func() {
		if grits.DebugFuse {
			log.Printf("Notifying: oldParent=%d, child=%d, newParent=%d",
				oldParentInode, oldChildInode, newParentInode)
		}
		gn.module.fsServer.DeleteNotify(oldParentInode, oldChildInode, name)
		gn.module.fsServer.EntryNotify(newParentInode, newName)
		if grits.DebugFuse {
			log.Printf("Done with notifications.")
		}
	}()

	if grits.DebugFuse {
		log.Printf("  all done")
	}
	return fs.OK
}
