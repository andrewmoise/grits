package proxy

import (
	"grits/internal/grits"
	"reflect"
	"testing"
)

func TestNameStore(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	ns, err := NewNameStore(bs)
	if err != nil {
		t.Fatalf("NewNameStore failed: %v", err)
	}

	// Test adding a name-blob association
	name := "example"
	addr := &grits.FileAddr{Hash: "hash", Size: 1234}
	ns.MapNameToBlob(name, addr)

	// Test resolving the name
	resolvedAddr, exists := ns.ResolveName(name)
	if !exists || !reflect.DeepEqual(addr, resolvedAddr) {
		t.Errorf("Expected to resolve '%s' to %+v, got %+v", name, addr, resolvedAddr)
	}

	// Test removing the name
	ns.RemoveName(name)
	_, exists = ns.ResolveName(name)
	if exists {
		t.Errorf("Expected '%s' to be removed, but it still exists", name)
	}

	// Optionally, add more tests here for edge cases and error conditions
}
