package proxy

import (
	"encoding/json"
	"errors"
)

// Assume NodeType and FileNode are defined as per your previous message.

// SerializableFileNode is a struct for JSON serialization.
type SerializableFileNode struct {
	Type     NodeType                         `json:"type"`
	AddrHash string                           `json:"addr,omitempty"`
	Children map[string]*SerializableFileNode `json:"children,omitempty"`
}

// SerializeFileNode serializes a FileNode to JSON.
func SerializeFileNode(node *FileNode) ([]byte, error) {
	if node == nil {
		return nil, errors.New("node is nil")
	}
	serializableNode := convertToSerializableNode(node)
	return json.MarshalIndent(serializableNode, "", "  ")
}

// convertToSerializableNode converts a FileNode to its serializable form.
func convertToSerializableNode(node *FileNode) *SerializableFileNode {
	sNode := &SerializableFileNode{
		Type:     node.Type,
		Children: make(map[string]*SerializableFileNode),
	}
	if node.Addr != nil {
		sNode.AddrHash = node.Addr.Hash // Assuming Addr has a Hash field
	}
	for name, child := range node.Children {
		sNode.Children[name] = convertToSerializableNode(child)
	}
	return sNode
}

// DeserializeFileNode creates a FileNode from JSON data, using blobStore to fetch CachedFiles.
func DeserializeFileNode(data []byte, blobStore *BlobStore) (*FileNode, error) {
	var sNode SerializableFileNode
	if err := json.Unmarshal(data, &sNode); err != nil {
		return nil, err
	}
	return reconstructFileNode(&sNode, blobStore), nil
}

// reconstructFileNode reconstructs a FileNode from its serializable form.
func reconstructFileNode(sNode *SerializableFileNode, blobStore *BlobStore) *FileNode {
	node := &FileNode{
		Type:     sNode.Type,
		Children: make(map[string]*FileNode),
	}
	if sNode.AddrHash != "" {
		// Fetch the CachedFile based on AddrHash from the blobStore.
		// This will require implementing logic within your BlobStore to fetch by hash.
	}
	for name, child := range sNode.Children {
		node.Children[name] = reconstructFileNode(child, blobStore)
	}
	return node
}
