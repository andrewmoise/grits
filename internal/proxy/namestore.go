package proxy

import (
	"grits/internal/grits"
	"sync"
)

type NameStore struct {
	root *FileNode
	mtx  sync.RWMutex
}

func NewNameStore(bs *BlobStore) (*NameStore, error) {
	fn, err := bs.CreateFileNode(make(map[string]*grits.FileAddr))
	if err != nil {
		return nil, err
	}

	return &NameStore{
		root: fn,
	}, nil
}

// MapNameToBlob associates a human-readable name with a blob address.
func (ns *NameStore) MapNameToBlob(name string, addr *grits.FileAddr) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()
	ns.root.Children[name] = addr
}

// ResolveName retrieves the blob address associated with a given name.
func (ns *NameStore) ResolveName(name string) (*grits.FileAddr, bool) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()
	addr, exists := ns.root.Children[name]
	return addr, exists
}

func (ns *NameStore) RemoveName(name string) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()
	delete(ns.root.Children, name)
}
