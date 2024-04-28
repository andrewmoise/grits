package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"path/filepath"
	"syscall"

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
			// Set to true to see how the file system works.
			Debug: true,
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
	path   string
}

// Ensure we are implementing the NodeReaddirer interface
var _ = (fs.NodeReaddirer)((*gritsNode)(nil))

// Readdir is part of the NodeReaddirer interface
func (gn *gritsNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	log.Printf("--- Okay reading directory %s\n", gn.path)

	node, _ := gn.volume.LookupNode(gn.path)
	if node == nil {
		// FIXME - might be better for Lookup() to return (nil, nil) for "not found"
		// And for us to return an internal error if we see non-nil error
		return nil, syscall.ENOENT
	}
	defer gn.volume.Release(node)

	dirNode, ok := node.(*grits.TreeNode)
	if !ok {
		return nil, syscall.ENOTDIR
	}

	r := make([]fuse.DirEntry, 0, len(dirNode.Children()))
	for name, node := range dirNode.Children() {
		_, isDir := node.(*grits.TreeNode)
		var mode uint32
		if isDir {
			mode = fuse.S_IFDIR | 0o755
		} else {
			mode = fuse.S_IFREG | 0o644
		}

		d := fuse.DirEntry{
			Name: name,
			Ino:  node.Inode(),
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
	log.Printf("--- Okay looking up %s %s\n", gn.path, name)

	fullPath := filepath.Join(gn.path, name)

	node, _ := gn.volume.LookupNode(fullPath)
	if node == nil {
		return nil, syscall.ENOENT
	}
	defer gn.volume.Release(node)

	_, isDir := node.(*grits.TreeNode)
	var mode uint32
	if isDir {
		mode = fuse.S_IFDIR | 0o755
	} else {
		mode = fuse.S_IFREG | 0o644
	}

	stable := fs.StableAttr{
		Mode: mode,
		// The child inode is identified by its Inode number.
		// If multiple concurrent lookups try to find the same
		// inode, they are deduplicated on this key.
		Ino: node.Inode(),
	}
	operations := &gritsNode{
		volume: gn.volume,
		path:   fullPath,
	}

	// The NewInode call wraps the `operations` object into an Inode.
	child := gn.NewInode(ctx, operations, stable)

	// In case of concurrent lookup requests, it can happen that operations !=
	// child.Operations().
	return child, 0
}
