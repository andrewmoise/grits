package proxy

import (
	"os"
	"path/filepath"
	"testing"

	"grits/internal/grits"
)

func setupDirBacking(t *testing.T) (*DirBacking, *BlobStore, string, string, func()) {
	t.Helper()

	// Create a temporary directory for testing
	dirPath, err := os.MkdirTemp("", "dirBacking_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Set up a BlobStore with a temporary directory
	blobStoreConfig := &Config{
		VarDirectory:    dirPath,
		StorageSize:     10 * 1024 * 1024, // 10MB
		StorageFreeSize: 8 * 1024 * 1024,  // 8MB
	}
	blobStore := NewBlobStore(blobStoreConfig)

	srcPath := filepath.Join(dirPath, "src")
	destPath := filepath.Join(dirPath, "dest")

	os.Mkdir(srcPath, 0755)
	os.Mkdir(destPath, 0755)

	dirBacking := NewDirBacking(srcPath, destPath, blobStore)

	cleanup := func() {
		dirBacking.Stop()
		os.RemoveAll(dirPath)
	}

	return dirBacking, blobStore, srcPath, destPath, cleanup
}

func TestDirBacking_Synchronization(t *testing.T) {
	dirBacking, blobStore, srcPath, destPath, cleanup := setupDirBacking(t)
	defer cleanup()

	// Create a new file in the source directory
	srcFilePath := filepath.Join(srcPath, "testfile.txt")
	expectedContent := []byte("hello world")
	if err := os.WriteFile(srcFilePath, expectedContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Start DirBacking to synchronize files
	dirBacking.Start()

	// Verify that the file is in the BlobStore and the destination file exists
	destFilePath := filepath.Join(destPath, "testfile.txt")
	if _, err := os.Stat(destFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected file %s does not exist in destination directory", destFilePath)
	}

	// Read the address from the destination file and verify it's in the BlobStore
	addressContent, err := os.ReadFile(destFilePath)
	if err != nil {
		t.Fatalf("Failed to read destination file address: %v", err)
	}

	fileAddr, err := grits.NewFileAddrFromString(string(addressContent))
	if err != nil {
		t.Fatalf("Invalid file address format in destination file: %v", err)
	}

	cachedFile, err := blobStore.ReadFile(fileAddr)
	if err != nil {
		t.Fatalf("File with address %s not found in BlobStore", fileAddr.String())
	}

	// Verify the content of the cached file matches the expected content
	actualContent, err := os.ReadFile(cachedFile.Path)
	if err != nil {
		t.Fatalf("Failed to read cached file content: %v", err)
	}
	if string(actualContent) != string(expectedContent) {
		t.Errorf("Cached file content mismatch. Expected: %s, got: %s", string(expectedContent), string(actualContent))
	}

	// Test for modification and deletion follows similar structure
	// Remember to perform cleanup and verify the results after each operation
}
