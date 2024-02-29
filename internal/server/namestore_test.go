package server

import (
	"fmt"
	"os"
	"testing"

	"grits/internal/grits"
)

func TestNamespacePersistence(t *testing.T) {
	// Set up a temporary directory for the server's working environment.
	tempDir, err := os.MkdirTemp("", "server_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	//defer os.RemoveAll(tempDir) // Clean up after the test.

	fmt.Printf("tempDir: %s\n", tempDir)

	// Create a new server instance using this temporary directory.
	config := grits.NewConfig()
	config.ServerDir = tempDir

	srv, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Populate test content.

	testAccountName := "test"
	ns, err := grits.EmptyNameStore(srv.BlobStore)
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}

	srv.AccountStores[testAccountName] = ns

	blocks := []string{"one", "two", "three"}

	for _, block := range blocks {
		cf, err := srv.BlobStore.AddDataBlock([]byte(block), "")
		if err != nil {
			t.Fatalf("Failed to add data block: %v", err)
		}
		defer srv.BlobStore.Release(cf)

		err = ns.LinkBlob(block, cf.Address)
		if err != nil {
			t.Fatalf("Failed to link blob: %v", err)
		}
	}

	if err != nil {
		t.Fatalf("Failed to revise root: %v", err)
	}

	fmt.Printf("All ready to save -- Root: %v\n", ns.GetRoot())

	// Save the state of the server.
	if err := srv.SaveAccounts(); err != nil {
		t.Fatalf("Failed to save accounts: %v", err)
	}

	// Reload the namespace state.
	if err := srv.LoadAccounts(); err != nil {
		t.Fatalf("Failed to load accounts: %v", err)
	}

	// Check that the namespace state is the same.
	for _, block := range blocks {
		fn, err := ns.Lookup(block)
		if err != nil {
			t.Errorf("Failed to look up name: %v", err)
		}

		cf, err := srv.BlobStore.ReadFile(fn.ExportedBlob().Address)
		if err != nil {
			t.Errorf("Failed to read file: %v", err)
		}
		defer srv.BlobStore.Release(cf)

		content, err := os.ReadFile(cf.Path)
		if err != nil {
			t.Errorf("Failed to read file: %v", err)
		}
		if string(content) != block {
			t.Errorf("Content mismatch: got %v, want %v", string(content), block)
		}
	}
}
