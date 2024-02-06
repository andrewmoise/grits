package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"grits/internal/grits"
)

type FileNode struct {
	ExportedBlob *grits.CachedFile          `json:"-"` // This field is ignored by the JSON package.
	Children     map[string]*grits.FileAddr // Maps file names to their CachedFile
}

// AddFile adds a file to the FileNode.
func (fn *FileNode) AddFile(name string, file *grits.CachedFile) {
	if fn.ExportedBlob != nil {
		panic("Trying to change finalized FileNode")
	}
	fn.Children[name] = file.Address
}

// GetFile retrieves a file by name from the FileNode.
func (fn *FileNode) GetFile(name string) (*grits.FileAddr, bool) {
	file, exists := fn.Children[name]
	return file, exists
}

// RemoveFile removes a file by name from the FileNode.
func (fn *FileNode) RemoveFile(name string) {
	if fn.ExportedBlob != nil {
		panic("Trying to change finalized FileNode")
	}
	delete(fn.Children, name)
}

// FetchFileNode retrieves a FileNode from the blob store using its hash.
func (bs *BlobStore) FetchFileNode(hash string) (*FileNode, error) {
	// If not cached, read from the store, deserialize, and cache
	path := filepath.Join(bs.config.StorageDirectory, hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading FileNode from path %s: %v", path, err)
	}

	var node FileNode
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("error deserializing FileNode: %v", err)
	}

	//bs.fileNodes[hash] = &node // Cache it
	return &node, nil
}

// CreateFileNode creates a FileNode with the specified children,
// serializes it, stores it in the blob store, and caches it.
func (bs *BlobStore) CreateFileNode(children map[string]*grits.FileAddr) (*FileNode, error) {
	node := &FileNode{
		Children: children,
	}
	data, err := json.Marshal(node)
	if err != nil {
		return nil, fmt.Errorf("error serializing FileNode: %v", err)
	}

	node.ExportedBlob, err = bs.AddDataBlock(data)
	if err != nil {
		return nil, fmt.Errorf("error storing FileNode: %v", err)
	}

	//bs.fileNodes[node.ExportedBlob.Address.String()] = node // Cache it
	return node, nil
}

// RevNode represents a revision, containing a snapshot of the content at a point in time.
type RevNode struct {
	ExportedBlob *grits.CachedFile `json:"-"`
	Tree         *FileNode         // The current state of the content
	Previous     *RevNode          // Pointer to the previous revision, nil if it's the first
}

// NewRevNode creates a new instance of RevNode.
func NewRevNode(tree *FileNode, previous *RevNode) *RevNode {
	return &RevNode{
		Tree:     tree,
		Previous: previous,
	}
}
