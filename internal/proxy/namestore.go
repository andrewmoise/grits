package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func SerializeNameStore(ns *NameStore, storageDirectory string) error {
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

	tempFileName := filepath.Join(storageDirectory, "namestore.json.tmp")
	finalFileName := filepath.Join(storageDirectory, "namestore.json")

	if err := os.WriteFile(tempFileName, data, 0644); err != nil {
		return fmt.Errorf("failed to write NameStore to temp file: %w", err)
	}

	if err := os.Rename(tempFileName, finalFileName); err != nil {
		return fmt.Errorf("failed to replace old NameStore file: %w", err)
	}

	return nil
}

func DeserializeNameStore(filePath string, bs *BlobStore) (*NameStore, error) {
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
