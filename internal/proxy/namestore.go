package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"grits/internal/grits"
)

type NameStore struct {
	root *RevNode
	mtx  sync.RWMutex
}

func NewNameStore(rn *RevNode) *NameStore {
	return &NameStore{
		root: rn,
	}
}

func (ns *NameStore) GetRoot() *RevNode {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	if ns.root == nil {
		return nil
	}

	return ns.root
}

func (ns *NameStore) cloneRoot() map[string]*grits.FileAddr {
	if ns.root == nil {
		return nil
	}

	m := make(map[string]*grits.FileAddr)
	for k, v := range ns.root.Tree.Children {
		m[k] = v
	}

	return m
}

func (ns *NameStore) ReviseRoot(bs *BlobStore, modifyFn func(map[string]*grits.FileAddr) error) error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	fmt.Printf("ReviseRoot; starting hash is %s\n", ns.root.ExportedBlob.Address.String())
	fmt.Printf("  file hash is %s\n", ns.root.Tree.ExportedBlob.Address.String())

	// Clone the current root's children for modification
	newRoot := ns.cloneRoot()
	if newRoot == nil {
		newRoot = make(map[string]*grits.FileAddr)
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

	fmt.Printf("ReviseRoot; ending hash is %s\n", rn.ExportedBlob.Address.String())
	fmt.Printf("  file hash is %s\n", rn.Tree.ExportedBlob.Address.String())

	ns.root = rn
	return nil
}

func (ns *NameStore) ResolveName(name string) *grits.FileAddr {
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

func (bs *BlobStore) SerializeNameStore(ns *NameStore) error {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	if ns.root == nil || ns.root.ExportedBlob == nil {
		return fmt.Errorf("NameStore root or its ExportedBlob is nil")
	}

	// Only need to serialize the reference to the root RevNode's ExportedBlob
	data, err := json.MarshalIndent(map[string]string{"root": ns.root.ExportedBlob.Address.String()}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize NameStore: %w", err)
	}

	tempFileName := bs.config.NamespaceStoreFile + "-new"
	finalFileName := bs.config.NamespaceStoreFile

	if err := os.WriteFile(tempFileName, data, 0644); err != nil {
		return fmt.Errorf("failed to write NameStore to temp file: %w", err)
	}

	if err := os.Rename(tempFileName, finalFileName); err != nil {
		return fmt.Errorf("failed to replace old NameStore file: %w", err)
	}

	return nil
}

func (bs *BlobStore) DeserializeNameStore() (*NameStore, error) {
	filePath := bs.config.NamespaceStoreFile

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read NameStore file: %w", err)
	}

	var ref map[string]string
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, fmt.Errorf("failed to deserialize NameStore reference: %w", err)
	}

	rootStr, ok := ref["root"]
	if !ok {
		return nil, fmt.Errorf("root address not found in NameStore reference")
	}

	var rootAddr *grits.FileAddr
	rootAddr, err = grits.NewFileAddrFromString(rootStr)
	if err != nil {
		return nil, fmt.Errorf("error creating addr: %v", err)
	}

	// Fetch and deserialize the root RevNode based on its address
	rootRevNode, err := bs.FetchRevNode(rootAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch root RevNode: %w", err)
	}

	return &NameStore{root: rootRevNode}, nil
}
