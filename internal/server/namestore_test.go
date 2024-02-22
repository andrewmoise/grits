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
		// FIXME -- do we need to release here? Not relevant for the test environment
		// but it will be for other things

		err = ns.ReviseRoot(srv.BlobStore, func(files map[string]*grits.FileAddr) error {
			files[block] = cf.Address
			return nil
		})
		if err != nil {
			t.Fatalf("Failed to revise root: %v", err)
		}
	}

	if err != nil {
		t.Fatalf("Failed to revise root: %v", err)
	}

	fmt.Printf("All ready to save -- Root: %v\n", ns.GetRoot().ExportedBlob.Address.String())

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
		fa := ns.ResolveName(block)
		if fa == nil {
			t.Errorf("Failed to resolve name: %v", block)
		}

		cf, err := srv.BlobStore.ReadFile(fa)
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
