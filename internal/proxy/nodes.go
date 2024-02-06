package proxy

import (
	"grits/internal/grits"
)

type FileNode struct {
	ExportedBlob *grits.CachedFile          `json:"-"` // This field is ignored by the JSON package.
	Children     map[string]*grits.FileAddr // Maps file names to their CachedFile
}

// NewFileNode creates a new FileNode.
func NewFileNode() *FileNode {
	return &FileNode{
		ExportedBlob: nil,
		Children:     make(map[string]*grits.FileAddr),
	}
}

func CloneFileNode(node *FileNode) *FileNode {
	clone := &FileNode{
		ExportedBlob: nil,
		Children:     make(map[string]*grits.FileAddr),
	}

	for name, child := range node.Children {
		clone.Children[name] = child
	}

	return clone
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

// RevNode represents a revision, containing a snapshot of the content at a point in time.
type RevNode struct {
	ExportedBlob *grits.CachedFile
	Tree         *FileNode // The current state of the content
	Previous     *RevNode  // Pointer to the previous revision, nil if it's the first
}

// NewRevNode creates a new instance of RevNode.
func NewRevNode(tree *FileNode, previous *RevNode) *RevNode {
	return &RevNode{
		Tree:     tree,
		Previous: previous,
	}
}
