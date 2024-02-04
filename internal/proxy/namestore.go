package proxy

import (
	"grits/internal/grits"
	"sync"
)

type NameStore struct {
	names map[string]*grits.FileAddr
	mtx   sync.RWMutex
}

func NewNameStore() *NameStore {
	return &NameStore{
		names: make(map[string]*grits.FileAddr),
	}
}

// MapNameToBlob associates a human-readable name with a blob address.
func (ns *NameStore) MapNameToBlob(name string, addr *grits.FileAddr) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()
	ns.names[name] = addr
}

// ResolveName retrieves the blob address associated with a given name.
func (ns *NameStore) ResolveName(name string) (*grits.FileAddr, bool) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()
	addr, exists := ns.names[name]
	return addr, exists
}

func (ns *NameStore) RemoveName(name string) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()
	delete(ns.names, name)
}
