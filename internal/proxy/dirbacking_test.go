package proxy

import (
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"
)

func setupDirBacking(t *testing.T) (*DirBacking, *BlobStore, string, func()) {
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

	srcPath := path.Join(dirPath, "src")
	destPath := path.Join(dirPath, "dest")

	os.Mkdir(srcPath, 0755)
	os.Mkdir(destPath, 0755)

	dirBacking := NewDirBacking(srcPath, destPath, blobStore)

	cleanup := func() {
		dirBacking.Stop()
		os.RemoveAll(dirPath)
	}

	return dirBacking, blobStore, srcPath, cleanup
}

func TestDirBacking_FileAddition(t *testing.T) {
	dirBacking, _, dirPath, cleanup := setupDirBacking(t)
	defer cleanup()

	dirBacking.Start()

	// Create a new file in the monitored directory
	filePath := filepath.Join(dirPath, "testfile.txt")
	err := os.WriteFile(filePath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Give some time for the file system watcher to detect the change
	time.Sleep(1 * time.Second)

	if _, exists := dirBacking.files[filePath]; !exists {
		t.Errorf("File %s was not added to DirBacking", filePath)
	}
}

func TestDirBacking_FileDeletion(t *testing.T) {
	dirBacking, _, dirPath, cleanup := setupDirBacking(t)
	defer cleanup()

	dirBacking.Start()

	// Create and then remove a file in the monitored directory
	filePath := filepath.Join(dirPath, "testfile.txt")
	err := os.WriteFile(filePath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	os.Remove(filePath)

	// Give some time for the file system watcher to detect the change
	time.Sleep(1 * time.Second)

	if _, exists := dirBacking.files[filePath]; exists {
		t.Errorf("File %s was not removed from DirBacking", filePath)
	}
}

// Additional tests for file modification, and starting/stopping the DirBacking can be structured similarly.
