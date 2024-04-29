package grits

import (
	"bytes"
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

func TestBlobStore_AddLocalFile_HardLink(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	bs.config.HardLinkBlobs = true

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

func TestBlobStore_ReadBack_AddOpenFile(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create a temporary file to simulate an existing file
	tempFile, err := os.CreateTemp("", "blob_open_file_test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	content := "Hello, open file!"
	if _, err := tempFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tempFile.Sync(); err != nil {
		t.Fatalf("Failed to sync temp file: %v", err)
	}
	tempFile.Close() // Close before reopening for reading

	// Re-open the temp file for reading
	file, err := os.Open(tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to open temp file: %v", err)
	}
	defer file.Close()

	// Add the file to the blob store
	addedFile, err := bs.AddOpenFile(file)
	if err != nil {
		t.Fatalf("AddOpenFile failed: %v", err)
	}
	defer addedFile.Release()

	// Read back the file content from the blob store
	readFile, err := os.ReadFile(addedFile.Path)
	if err != nil {
		t.Fatalf("Failed to read back the file from blob store: %v", err)
	}

	if string(readFile) != content {
		t.Errorf("Content mismatch: expected %s, got %s", content, string(readFile))
	}
}

func TestBlobStore_ReadBack_AddDataBlock(t *testing.T) {
	bs, cleanup := setupBlobStore(t) // setupBlobStore without using hard links
	defer cleanup()

	content := []byte("Hello, data block!")

	// Add the data block to the blob store
	addedFile, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}
	defer addedFile.Release()

	// Read back the file content from the blob store
	readFile, err := os.ReadFile(addedFile.Path)
	if err != nil {
		t.Fatalf("Failed to read back the file from blob store: %v", err)
	}

	if !bytes.Equal(readFile, content) {
		t.Errorf("Content mismatch: expected %s, got %s", content, readFile)
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
	cachedFile.Release()

	if cachedFile.RefCount != 0 {
		t.Errorf("Expected RefCount to be 0 after release, got %d", cachedFile.RefCount)
	}
}

// generateTestBlock generates a 1MB block of data with a unique content based on the iteration.
func generateTestBlock(iteration int) []byte {
	// Create a slice of 1MB with a pattern based on the iteration
	block := make([]byte, 1*1024*1024) // 1MB
	for i := range block {
		block[i] = byte((i + iteration) % 256)
	}
	return block
}

func TestBlobStore_EvictOldFiles(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	bs.config.StorageSize = 10 * 1024 * 1024    // 10MB
	bs.config.StorageFreeSize = 8 * 1024 * 1024 // 8MB

	heldFiles := []*CachedFile{}

	// Add 11 blocks to the store, hold references to the first 3
	for i := 0; i < 11; i++ {
		dataBlock := generateTestBlock(i)
		cachedFile, err := bs.AddDataBlock(dataBlock)
		if err != nil {
			t.Fatalf("Failed to add data block %d: %v", i, err)
		}

		if i < 3 {
			// Hold references to the first 3 blocks
			heldFiles = append(heldFiles, cachedFile)
		} else {
			// Release the others immediately
			cachedFile.Release()
		}
	}

	bs.evictOldFiles()

	if bs.currentSize > bs.config.StorageSize {
		t.Errorf("BlobStore size after eviction is greater than StorageSize: got %d, want <= %d", bs.currentSize, bs.config.StorageSize)
	}

	// Check that held files are still present
	for _, file := range heldFiles {
		cf, err := bs.ReadFile(file.Address)
		if err != nil {
			t.Errorf("Couldn't find %s in store", file.Address.String())
			continue
		}
		defer cf.Release()

		if cf != file {
			t.Errorf("File for blob %s doesn't match", file.Address.String())
		}

		_, err = os.Stat(file.Path)
		if os.IsNotExist(err) {
			t.Errorf("File expected to be present was not found: %s", file.Path)
		}
	}
}
