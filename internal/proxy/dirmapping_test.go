package proxy

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupDirMapping(t *testing.T) (*DirMapping, *BlobStore, string, func()) {
	t.Helper()

	// Create a temporary directory for testing
	dirPath, err := ioutil.TempDir("", "dirmapping_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Set up a BlobStore with a temporary directory
	blobStoreConfig := &Config{
		StorageDirectory: dirPath,
		StorageSize:      10 * 1024 * 1024,
		StorageFreeSize:  8 * 1024 * 1024,
	}
	blobStore := NewBlobStore(blobStoreConfig)

	dirMapping := NewDirMapping(dirPath, blobStore)

	cleanup := func() {
		dirMapping.Stop()
		os.RemoveAll(dirPath)
	}

	return dirMapping, blobStore, dirPath, cleanup
}

func TestDirMapping_FileAddition(t *testing.T) {
	dirMapping, _, dirPath, cleanup := setupDirMapping(t)
	defer cleanup()

	dirMapping.Start()

	// Create a new file in the monitored directory
	filePath := filepath.Join(dirPath, "testfile.txt")
	err := ioutil.WriteFile(filePath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Give some time for the file system watcher to detect the change
	time.Sleep(1 * time.Second)

	if _, exists := dirMapping.files[filePath]; !exists {
		t.Errorf("File %s was not added to DirMapping", filePath)
	}
}

func TestDirMapping_FileDeletion(t *testing.T) {
	dirMapping, _, dirPath, cleanup := setupDirMapping(t)
	defer cleanup()

	dirMapping.Start()

	// Create and then remove a file in the monitored directory
	filePath := filepath.Join(dirPath, "testfile.txt")
	err := ioutil.WriteFile(filePath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	os.Remove(filePath)

	// Give some time for the file system watcher to detect the change
	time.Sleep(1 * time.Second)

	if _, exists := dirMapping.files[filePath]; exists {
		t.Errorf("File %s was not removed from DirMapping", filePath)
	}
}

// Additional tests for file modification, and starting/stopping the DirMapping can be structured similarly.
