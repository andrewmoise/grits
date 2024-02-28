package grits

import (
	"fmt"
	"log"
	"sync"
)

type NameStore struct {
	root *DirNode
	mtx  sync.RWMutex
}

func (ns *NameStore) GetRoot() *DirNode {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	if ns.root == nil {
		return nil
	}

	return ns.root
}

func (ns *NameStore) ReviseRoot(bs *BlobStore, modifyFn func([]*FileNode) ([]*FileNode, error)) error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	log.Printf("ReviseRoot; starting hash is %s\n", ns.root.ExportedBlob.Address.String())
	log.Printf("  file hash is %s\n", ns.root.ExportedBlob.Address.String())

	// Assuming ns.root.Tree is a *DirNode
	// Prepare the current children slice for modification
	currentChildren := ns.root.Children // This is already a slice of *FileNode

	// Call the passed function to get the modified version of children
	modifiedChildren, err := modifyFn(currentChildren)
	if err != nil {
		return fmt.Errorf("failed to modify root children: %w", err)
	}

	// Create a new DirNode with the modified children
	newDirNode, err := bs.CreateDirNode(modifiedChildren)
	if err != nil {
		return fmt.Errorf("failed to create new DirNode: %w", err)
	}

	ns.root = newDirNode
	log.Printf("ReviseRoot; ending hash is %s\n", newDirNode.ExportedBlob.Address.String())
	return nil
}

func (ns *NameStore) ResolveName(name string) *FileAddr {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	if ns.root == nil {
		return nil
	}

	if ns.root.Children == nil {
		return nil
	}

	child, exists := ns.root.ChildrenMap[name]
	if !exists {
		return nil
	}

	return child.FileAddr
}

func EmptyNameStore(bs *BlobStore) (*NameStore, error) {
	dn, err := bs.CreateDirNode(make([]*FileNode, 0))
	if err != nil {
		return nil, err
	}

	ns := &NameStore{
		root: dn,
	}
	return ns, nil
}

func DeserializeNameStore(bs *BlobStore, rootAddr *FileAddr) (*NameStore, error) {
	// Fetch and deserialize the root RevNode based on its address
	rootDirNode, err := bs.FetchDirNode(rootAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch root RevNode: %w", err)
	}

	return &NameStore{root: rootDirNode}, nil
}
