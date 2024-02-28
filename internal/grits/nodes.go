package grits

import (
	"encoding/json"
	"fmt"
	"os"
)

// A file

type FileNode struct {
	Type     string    `json:"type"`
	Name     string    `json:"name"`
	FileAddr *FileAddr `json:"hash"` // Updated to use FileAddr type
}

// Custom JSON marshaling to maintain string format in JSON
func (fn *FileNode) MarshalJSON() ([]byte, error) {
	type Alias FileNode
	return json.Marshal(&struct {
		FileAddr string `json:"hash"`
		*Alias
	}{
		FileAddr: fn.FileAddr.String(), // Serialize FileAddr as string
		Alias:    (*Alias)(fn),
	})
}

// Custom JSON unmarshaling to handle FileAddr string format
func (fn *FileNode) UnmarshalJSON(data []byte) error {
	type Alias FileNode
	aux := &struct {
		FileAddr string `json:"hash"`
		*Alias
	}{
		Alias: (*Alias)(fn),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	fileAddr, err := NewFileAddrFromString(aux.FileAddr)
	if err != nil {
		return err
	}
	fn.FileAddr = fileAddr
	return nil
}

// NewFileNode creates a new instance of FileNode with a FileAddr
func NewFileNode(name string, fileAddr *FileAddr) *FileNode {
	return &FileNode{
		Type:     "file",
		Name:     name,
		FileAddr: fileAddr,
	}
}

// A directory

type DirNode struct {
	Children     []*FileNode          `json:"children"`
	ChildrenMap  map[string]*FileNode `json:"-"`
	ExportedBlob *CachedFile          `json:"-"`
}

func (dn *DirNode) GetFile(name string) (*FileNode, bool) {
	file, exists := dn.ChildrenMap[name]
	return file, exists
}

func (bs *BlobStore) CreateDirNode(children []*FileNode) (*DirNode, error) {
	dn := &DirNode{Children: children}
	dn.ChildrenMap = make(map[string]*FileNode)

	for _, child := range children {
		if _, exists := dn.ChildrenMap[child.Name]; exists {
			// Duplicate found
			return nil, fmt.Errorf("duplicate file name found: %s", child.Name)
		}
		dn.ChildrenMap[child.Name] = child
	}

	data, err := json.MarshalIndent(dn, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize DirNode: %w", err)
	}

	cf, err := bs.AddDataBlock(data, ".json")
	if err != nil {
		return nil, fmt.Errorf("error storing DirNode: %v", err)
	}

	dn.ExportedBlob = cf
	return dn, nil
}

func (bs *BlobStore) FetchDirNode(addr *FileAddr) (*DirNode, error) {
	cf, err := bs.ReadFile(addr)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", addr.Hash, err)
	}

	data, err := os.ReadFile(cf.Path)
	if err != nil {
		return nil, fmt.Errorf("error reading file at %s: %v", cf.Path, err)
	}

	var dn DirNode
	if err := json.Unmarshal(data, &dn); err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON from file at %s: %v", cf.Path, err)
	}

	dn.ChildrenMap = make(map[string]*FileNode)
	for _, child := range dn.Children {
		if _, exists := dn.ChildrenMap[child.Name]; exists {
			// Duplicate found
			return nil, fmt.Errorf("duplicate file name found: %s", child.Name)
		}
		dn.ChildrenMap[child.Name] = child
	}

	nameSet := make(map[string]struct{}) // Use an empty struct to minimize memory usage
	for _, child := range dn.Children {
		if _, exists := nameSet[child.Name]; exists {
			// Duplicate found
			return nil, fmt.Errorf("duplicate file name found: %s", child.Name)
		}
		nameSet[child.Name] = struct{}{}
	}

	dn.ExportedBlob = cf
	return &dn, nil
}
