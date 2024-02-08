package proxy

import (
	"os"
	"reflect"
	"testing"

	"grits/internal/grits"
)

// TestNameStoreSerialization ensures that a NameStore can be serialized and deserialized correctly.
func TestNameStoreSerialization(t *testing.T) {
	// Setup a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "namestore_test")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up after the test

	// Create a NameStore with some data
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Add a mock FileAddr to the NameStore for testing
	// Assuming you have a way to add entries to the NameStore
	testAddr := &grits.FileAddr{Hash: "testhash", Size: 1234}
	m := make(map[string]*grits.FileAddr)
	m["test"] = testAddr

	var fn *FileNode
	fn, err = bs.CreateFileNode(m)
	if err != nil {
		t.Fatalf("Failed to create FileNode: %v", err)
	}

	var rn *RevNode
	rn, err = bs.CreateRevNode(fn, nil)
	if err != nil {
		t.Fatalf("Failed to create RevNode: %v", err)
	}

	ns := NewNameStore(rn)

	// Serialize the NameStore
	if err := bs.SerializeNameStore(ns); err != nil {
		t.Fatalf("Failed to serialize NameStore: %v", err)
	}

	// Deserialize the NameStore
	deserializedNS, err := bs.DeserializeNameStore()
	if err != nil {
		t.Fatalf("Failed to deserialize NameStore: %v", err)
	}

	// Compare the original and deserialized NameStores
	if !reflect.DeepEqual(ns.root, deserializedNS.root) {
		t.Errorf("Original and deserialized NameStores do not match.\nOriginal: %+v\nDeserialized: %+v", ns, deserializedNS)
	}
}
