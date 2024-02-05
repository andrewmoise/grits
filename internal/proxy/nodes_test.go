package proxy

import (
	"grits/internal/grits"
	"testing"
)

func TestFileNode_AddGetRemoveChild(t *testing.T) {
	// Setup
	rootAddr := grits.NewFileAddr([]byte("root"), 0)
	rootNode := NewFileNode(NodeTypeTree, rootAddr)

	childAddr := grits.NewFileAddr([]byte("child"), 0)
	childNode := NewFileNode(NodeTypeBlob, childAddr)

	// Test adding a child
	rootNode.AddChild("child", childNode)
	if _, exists := rootNode.GetChild("child"); !exists {
		t.Errorf("Child node was not added correctly")
	}

	// Test retrieving a child
	retrievedChild, exists := rootNode.GetChild("child")
	if !exists || retrievedChild.Addr != childAddr {
		t.Errorf("Failed to retrieve the correct child node")
	}

	// Test removing a child
	rootNode.RemoveChild("child")
	if _, exists := rootNode.GetChild("child"); exists {
		t.Errorf("Child node was not removed correctly")
	}
}

func TestRevNode_Navigation(t *testing.T) {
	// Setup initial revision
	initialTreeAddr := grits.NewFileAddr([]byte("initial"), 0)
	initialTreeNode := NewFileNode(NodeTypeTree, initialTreeAddr)
	initialRevNode := NewRevNode(initialTreeNode, nil)

	// Setup subsequent revision
	subsequentTreeAddr := grits.NewFileAddr([]byte("subsequent"), 0)
	subsequentTreeNode := NewFileNode(NodeTypeTree, subsequentTreeAddr)
	subsequentRevNode := NewRevNode(subsequentTreeNode, initialRevNode)

	// Navigate from subsequent revision to initial
	if subsequentRevNode.Previous != initialRevNode {
		t.Errorf("Failed to navigate from subsequent revision to initial revision")
	}

	// Check the tree of the initial revision
	if subsequentRevNode.Previous.Tree != initialTreeNode {
		t.Errorf("Initial revision's tree node does not match expected")
	}
}
