package grits

import (
	"fmt"
	"log"
	"sync"
)

type NameStore struct {
	root *RevNode
	mtx  sync.RWMutex
}

func (ns *NameStore) GetRoot() *RevNode {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	if ns.root == nil {
		return nil
	}

	return ns.root
}

func (ns *NameStore) cloneRoot() map[string]*FileAddr {
	if ns.root == nil {
		return nil
	}

	m := make(map[string]*FileAddr)
	for k, v := range ns.root.Tree.Children {
		m[k] = v
	}

	return m
}

func (ns *NameStore) ReviseRoot(bs *BlobStore, modifyFn func(map[string]*FileAddr) error) error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	log.Printf("ReviseRoot; starting hash is %s\n", ns.root.ExportedBlob.Address.String())
	log.Printf("  file hash is %s\n", ns.root.Tree.ExportedBlob.Address.String())

	// Clone the current root's children for modification
	newRoot := ns.cloneRoot()
	if newRoot == nil {
		newRoot = make(map[string]*FileAddr)
	}

	// Call the passed function to get the modified version of children
	err := modifyFn(newRoot)
	if err != nil {
		return fmt.Errorf("failed to modify root children: %w", err)
	}

	// Create a new FileNode with the modified children
	fn, err := bs.CreateFileNode(newRoot)
	if err != nil {
		return fmt.Errorf("failed to create new FileNode: %w", err)
	}

	// Create a new RevNode with the new FileNode and set it as the new root
	rn, err := bs.CreateRevNode(fn, ns.root)
	if err != nil {
		return fmt.Errorf("failed to create new RevNode: %w", err)
	}

	log.Printf("ReviseRoot; ending hash is %s\n", rn.ExportedBlob.Address.String())
	log.Printf("  file hash is %s\n", rn.Tree.ExportedBlob.Address.String())

	ns.root = rn
	return nil
}

func (ns *NameStore) ResolveName(name string) *FileAddr {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	if ns.root == nil {
		return nil
	}

	if ns.root.Tree == nil {
		return nil
	}

	if ns.root.Tree.Children == nil {
		return nil
	}

	return ns.root.Tree.Children[name]
}

func EmptyNameStore(bs *BlobStore) (*NameStore, error) {
	m := make(map[string]*FileAddr)

	fn, err := bs.CreateFileNode(m)
	if err != nil {
		return nil, err
	}

	rn, err := bs.CreateRevNode(fn, nil)
	if err != nil {
		return nil, err
	}

	ns := &NameStore{
		root: rn,
	}
	return ns, nil
}

func DeserializeNameStore(bs *BlobStore, rootAddr *FileAddr) (*NameStore, error) {
	// Fetch and deserialize the root RevNode based on its address
	rootRevNode, err := bs.FetchRevNode(rootAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch root RevNode: %w", err)
	}

	return &NameStore{root: rootRevNode}, nil
}
