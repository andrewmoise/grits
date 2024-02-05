package proxy

import (
	"errors"
	"grits/internal/grits"
	"strings"
)

// NodeType represents the type of a FileNode.
type NodeType int

const (
	NodeTypeUnknown NodeType = iota
	NodeTypeBlob
	NodeTypeTree
)

// FileNode represents a node in the file system structure, which can be a file or a directory.
type FileNode struct {
	Type     NodeType
	Addr     *grits.FileAddr // Address of the CachedFile in BlobStore
	Children map[string]*FileNode
}

// NewFileNode creates a new FileNode.
func NewFileNode(nodeType NodeType, addr *grits.FileAddr) *FileNode {
	node := &FileNode{
		Type: nodeType,
		Addr: addr,
	}
	if nodeType == NodeTypeTree {
		node.Children = make(map[string]*FileNode)
	}
	return node
}

func cloneFileNode(node *FileNode) *FileNode {
	if node == nil {
		return nil
	}

	if node.Type == NodeTypeBlob {
		return &FileNode{
			Type: node.Type,
			Addr: node.Addr,
		}
	} else if node.Type == NodeTypeTree {
		clone := &FileNode{
			Type: node.Type,
			Addr: nil,
		}

		clone.Children = make(map[string]*FileNode)
		for name, child := range node.Children {
			clone.Children[name] = child
		}

		return clone
	} else {
		// Can't happen
		return nil
	}
}

// AddChild adds a child node to a directory node.
func (fn *FileNode) AddChild(name string, child *FileNode) {
	if fn.Type == NodeTypeTree {
		fn.Children[name] = child
	}
}

// GetChild retrieves a child node by name from a directory node.
func (fn *FileNode) GetChild(name string) (*FileNode, bool) {
	if fn.Type != NodeTypeTree {
		return nil, false
	}
	child, exists := fn.Children[name]
	return child, exists
}

// RemoveChild removes a child node by name from a directory node.
func (fn *FileNode) RemoveChild(name string) {
	if fn.Type == NodeTypeTree {
		delete(fn.Children, name)
	}
}

// Get searches for a file in the tree structure starting from the root node.
// The path is a "/"-separated string representing the file's location in the tree.
func Get(root *FileNode, path string) (*FileNode, error) {
	// Split the path into parts
	parts := strings.Split(path, "/")

	// Start the recursive search from the root node
	return findFileInNode(root, parts)
}

// findFileInNode is a helper function that performs the recursive search.
func findFileInNode(node *FileNode, parts []string) (*FileNode, error) {
	// Base case: if we've consumed all parts, we should be at the target file node
	if len(parts) == 0 {
		if node.Type == NodeTypeBlob {
			return node, nil
		}
		return nil, errors.New("path does not point to a file")
	}

	// Recursive case: navigate down the tree
	currentPart := parts[0]
	if node.Type != NodeTypeTree || node.Children == nil {
		return nil, errors.New("path leads to a non-directory before reaching the target")
	}

	childNode, exists := node.Children[currentPart]
	if !exists {
		return nil, errors.New("path segment not found in current directory: " + currentPart)
	}

	// Continue the search in the child node
	return findFileInNode(childNode, parts[1:])
}

func Put(root *FileNode, path string, file *FileNode) error {
	parts := strings.Split(path, "/")
	return putFileInNode(root, parts, file)
}

func putFileInNode(node *FileNode, parts []string, file *FileNode) (*FileNode, error) {
	if len(parts) == 0 {
		return nil, errors.New("path is not deep enough")
	}

	var newChild *FileNode
	var err error
	if len(parts) == 1 {
		newChild = file
	} else {
		newChild, err = putFileInNode(node, parts[1:], file)
		if err != nil {
			return nil, err
		}
	}

	currentPart := parts[0]
	newNode := cloneFileNode(node)
	if newChild != nil {
		newNode.Children[currentPart] = newChild
	} else {
		delete(newNode.Children, currentPart)
	}

	if len(parts) == 1 {
		if node.Type != NodeTypeTree {
			return errors.New("path does not point to a directory")
		}
		node.Children[currentPart] = file
		return nil
	}

	if node.Type != NodeTypeTree || node.Children == nil {
		return errors.New("path leads to a non-directory before reaching the target")
	}

	childNode, exists := node.Children[currentPart]
	if !exists {
		return errors.New("path segment not found in current directory: " + currentPart)
	}

	return putFileInNode(childNode, parts[1:], file)
}

// RevNode represents a revision, containing a snapshot of the content at a point in time.
type RevNode struct {
	Tree     *FileNode // The current state of the content
	Previous *RevNode  // Pointer to the previous revision, nil if it's the first
}

// NewRevNode creates a new instance of RevNode.
func NewRevNode(tree *FileNode, previous *RevNode) *RevNode {
	return &RevNode{
		Tree:     tree,
		Previous: previous,
	}
}
