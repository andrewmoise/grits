package grits

import (
	"log"
	"os"
	"testing"
)

func setupBlobStore(t *testing.T) (*BlobStore, func()) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "blobstore_test")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}

	log.Printf("Setup in %s\n", tempDir)

	// Assuming default values are appropriate for testing.
	// Adjust if necessary.
	config := NewConfig(tempDir)
	config.StorageSize = 10 * 1024 * 1024    // 10MB for testing
	config.StorageFreeSize = 8 * 1024 * 1024 // 8MB for testing

	err = os.MkdirAll(config.ServerPath("var"), 0755)
	if err != nil {
		t.Fatalf("Failed to create storage directory: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	bs := NewBlobStore(config)
	return bs, cleanup
}

func TestBlobStore_AddLocalFile(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	srcPath := bs.config.ServerPath("var/test.txt")
	content := []byte("hello world")
	err := os.WriteFile(srcPath, content, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cachedFile, err := bs.AddLocalFile(srcPath)
	if err != nil {
		t.Fatalf("AddLocalFile failed: %v", err)
	}

	if cachedFile.RefCount != 1 {
		t.Errorf("Expected RefCount to be 1, got %d", cachedFile.RefCount)
	}
}

func TestBlobStore_ReadFile(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	if t == nil {
		defer cleanup()
	}

	// Setup file in BlobStore
	srcPath := bs.config.ServerPath("var/test.txt")
	content := []byte("hello world")
	err := os.WriteFile(srcPath, content, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cachedFile, err := bs.AddLocalFile(srcPath)
	if err != nil {
		t.Fatalf("AddLocalFile failed: %v", err)
	}

	// Test reading the file
	readFile, err := bs.ReadFile(cachedFile.Address)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if readFile.RefCount != 2 {
		t.Errorf("Expected RefCount to be 2 after read, got %d", readFile.RefCount)
	}
}

func TestBlobStore_Release(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Setup file in BlobStore
	srcPath := bs.config.ServerPath("var/test_release.txt")
	content := []byte("test release")
	err := os.WriteFile(srcPath, content, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cachedFile, err := bs.AddLocalFile(srcPath)
	if err != nil {
		t.Fatalf("AddLocalFile failed: %v", err)
	}

	// Release the file
	bs.Release(cachedFile)

	if cachedFile.RefCount != 0 {
		t.Errorf("Expected RefCount to be 0 after release, got %d", cachedFile.RefCount)
	}
}
