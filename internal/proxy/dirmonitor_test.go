package proxy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "dirmonitor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return dir, func() { os.RemoveAll(dir) }
}

func TestDirMonitor(t *testing.T) {
	testDir, cleanup := setupTestDir(t)
	defer cleanup()

	// Setup FileCache with a mock or minimal config
	config := &Config{StorageDirectory: testDir, StorageSize: 10 << 20, StorageFreeSize: 2 << 20} // Example config, adjust as needed
	fileCache := NewFileCache(config)

	dirMonitor := NewDirMonitor(testDir, fileCache)
	dirMonitor.Start()
	defer dirMonitor.Stop()

	// Create a new file
	testFilePath := filepath.Join(testDir, "testfile.txt")
	err := os.WriteFile(testFilePath, []byte("hello world"), 0644)
	if err != nil {
		t.Fatalf("Failed to write to test file: %v", err)
	}

	// Modify the file
	time.Sleep(1 * time.Second) // Ensure the filesystem has time to register the change
	err = os.WriteFile(testFilePath, []byte("hello again"), 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Delete the file
	time.Sleep(1 * time.Second) // Ensure the filesystem has time to register the change
	err = os.Remove(testFilePath)
	if err != nil {
		t.Fatalf("Failed to delete test file: %v", err)
	}

	// Verify changes were picked up by DirMonitor
	// This could involve checking internal state of DirMonitor or FileCache
	// For example, if you implement a method to query FileCache or DirMonitor state:
	if _, exists := dirMonitor.pathToFile[testFilePath]; exists {
		t.Errorf("File %s was deleted but still exists in DirMonitor", testFilePath)
	}

	// Add more verification as needed to test the correctness of your implementation
}
