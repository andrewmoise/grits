package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

/////
// Timeline

// * Already done, I think: NodeLookuper, NodeOpendirer, NodeReaddirer
// * Minimal implementation phase (we need this right away if we're even trying to just do reading and navigation): NodeGetattrer, NodeOpener, NodeReader
// * Writing implementation (somewhat complex because of copy-on-write semantics -- I'll honestly have to construct a new abstraction of an "in flight being-modified file" in order for this thing to work): NodeWriter, NodeFlusher, NodeReleaser (we need to manage some of our object lifetimes in ways that will wind up a little bit complex), NodeCreater, NodeUnlinker
// * Directories (needs even more modifications for weird reasons, nice to do it in 2 phases): NodeMkdirer, NodeRmdirer
// * Polish (not really needed but what the heck let's try to make it nice and all): NodeStatfser, NodeFsyncer, NodeRenamer

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

// Ensure we are implementing the NodeReaddirer interface
var _ = (fs.NodeReaddirer)((*gritsNode)(nil))

// Readdir is part of the NodeReaddirer interface
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

// Ensure we are implementing the NodeLookuper interface
var _ = (fs.NodeLookuper)((*gritsNode)(nil))

// Lookup is part of the NodeLookuper interface
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

// Ensure we are implementing the NodeLookuper interface
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
