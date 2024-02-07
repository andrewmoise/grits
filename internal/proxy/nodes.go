package proxy

import (
	"encoding/json"
	"fmt"
	"os"

	"grits/internal/grits"
)

type FileNode struct {
	ExportedBlob *grits.CachedFile          `json:"-"` // This field is ignored by the JSON package.
	Children     map[string]*grits.FileAddr // Maps file names to their CachedFile
}

// GetFile retrieves a file by name from the FileNode.
func (fn *FileNode) GetFile(name string) (*grits.FileAddr, bool) {
	file, exists := fn.Children[name]
	return file, exists
}

// CreateFileNode creates a FileNode with the specified children,
// serializes it, stores it in the blob store, and caches it.
func (bs *BlobStore) CreateFileNode(children map[string]*grits.FileAddr) (*FileNode, error) {
	m := make(map[string]string)
	for k, v := range children {
		m[k] = v.String()
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize NameStore: %w", err)
	}

	var cf *grits.CachedFile
	cf, err = bs.AddDataBlock(data)
	if err != nil {
		return nil, fmt.Errorf("error storing FileNode: %v", err)
	}

	return &FileNode{
		ExportedBlob: cf,
		Children:     children,
	}, nil
}

func (bs *BlobStore) FetchFileNode(addr *grits.FileAddr) (*FileNode, error) {
	cf, err := bs.ReadFile(addr)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", addr.Hash, err)
	}

	data, err := os.ReadFile(cf.Path)
	if err != nil {
		return nil, fmt.Errorf("error reading file at %s: %v", cf.Path, err)
	}

	var ref map[string]string
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON from file at %s: %v", cf.Path, err)
	}

	m := make(map[string]*grits.FileAddr)
	for k, v := range ref {
		fa, err := grits.NewFileAddrFromString(v)
		if err != nil {
			return nil, fmt.Errorf("error creating addr: %v", err)
		}
		m[k] = fa
	}

	return &FileNode{
		ExportedBlob: cf,
		Children:     m,
	}, nil

}

func (fn *FileNode) CloneChildren() map[string]*grits.FileAddr {
	clone := make(map[string]*grits.FileAddr)
	for k, v := range fn.Children {
		clone[k] = v
	}
	return clone
}

// RevNode represents a revision, containing a snapshot of the content at a point in time.
type RevNode struct {
	ExportedBlob *grits.CachedFile `json:"-"`
	Tree         *FileNode         // The current state of the content
	Previous     *RevNode          // Pointer to the previous revision, nil if it's the first
}

// NewRevNode creates a new instance of RevNode.
func NewRevNode(previous *RevNode, tree *FileNode) *RevNode {
	return &RevNode{
		Tree:     tree,
		Previous: previous,
	}
}

// CreateRevNode creates a RevNode with the specified FileNode as its tree,
// serializes it, stores it in the blob store, and caches it.
func (bs *BlobStore) CreateRevNode(tree *FileNode, previous *RevNode) (*RevNode, error) {
	m := make(map[string]string)
	if previous != nil {
		m["previous"] = previous.ExportedBlob.Address.String()
	}
	if tree != nil {
		m["tree"] = tree.ExportedBlob.Address.String()
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize NameStore: %w", err)
	}

	var cf *grits.CachedFile
	cf, err = bs.AddDataBlock(data)
	if err != nil {
		return nil, fmt.Errorf("error storing FileNode: %v", err)
	}

	return &RevNode{
		ExportedBlob: cf,
		Tree:         tree,
		Previous:     previous,
	}, nil
}

// FetchRevNode retrieves a RevNode from the blob store using its address.
func (bs *BlobStore) FetchRevNode(addr *grits.FileAddr) (*RevNode, error) {
	cf, err := bs.ReadFile(addr)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", addr.Hash, err)
	}

	data, err := os.ReadFile(cf.Path)
	if err != nil {
		cf.Release()
		return nil, fmt.Errorf("error reading file at %s: %v", cf.Path, err)
	}

	var ref map[string]string
	if err := json.Unmarshal(data, &ref); err != nil {
		cf.Release()
		return nil, fmt.Errorf("error unmarshaling JSON from file at %s: %v", cf.Path, err)
	}

	rn := &RevNode{}

	previousStr, exists := ref["previous"]
	if exists {
		var previousAddr *grits.FileAddr
		previousAddr, err = grits.NewFileAddrFromString(previousStr)
		if err != nil {
			cf.Release()
			return nil, fmt.Errorf("error creating addr: %v", err)
		}

		rn.Previous, err = bs.FetchRevNode(previousAddr)
		if err != nil {
			cf.Release()
			return nil, fmt.Errorf("error fetching previous revnode: %v", err)
		}
	} else {
		rn.Previous = nil
	}

	var treeStr string
	treeStr, exists = ref["tree"]
	if exists {
		var treeAddr *grits.FileAddr
		treeAddr, err = grits.NewFileAddrFromString(treeStr)
		if err != nil {
			if rn.Previous != nil {
				rn.Previous.ExportedBlob.Release()
			}
			cf.Release()
			return nil, fmt.Errorf("error creating addr: %v", err)
		}

		rn.Tree, err = bs.FetchFileNode(treeAddr)
		if err != nil {
			if rn.Previous != nil {
				rn.Previous.ExportedBlob.Release()
			}
			cf.Release()
			return nil, fmt.Errorf("error fetching tree: %v", err)
		}
	} else {
		rn.Tree = nil
	}

	rn.ExportedBlob = cf
	return rn, nil
}
