package grits

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
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
		ServerDir:       dirPath,
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

func TestDirBacking_FileOperations(t *testing.T) {
	dirBacking, blobStore, srcPath, destPath, cleanup := setupDirBacking(t)
	defer cleanup()

	// Step 1: Create files 1, 2, and 3 in the source directory
	for i := 1; i <= 3; i++ {
		filename := filepath.Join(srcPath, "file"+strconv.Itoa(i)+".txt")
		content := "Content for file " + strconv.Itoa(i)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %d: %v", i, err)
		}
	}

	// Start DirBacking to synchronize files
	dirBacking.Start()

	// Allow some time for DirBacking to process the files
	time.Sleep(100 * time.Millisecond)

	// Verify initial files are synchronized correctly
	verifyFileContent(t, destPath, "file1.txt", "Content for file 1", blobStore)
	goneAddr := verifyFileContent(t, destPath, "file2.txt", "Content for file 2", blobStore)
	verifyFileContent(t, destPath, "file3.txt", "Content for file 3", blobStore)

	// Step 2: Delete file 2, overwrite file 3, create a new file 4
	os.Remove(filepath.Join(srcPath, "file2.txt"))
	if err := os.WriteFile(filepath.Join(srcPath, "file3.txt"), []byte("New content for file 3"), 0644); err != nil {
		t.Fatalf("Failed to overwrite file 3: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcPath, "file4.txt"), []byte("Content for file 4"), 0644); err != nil {
		t.Fatalf("Failed to create file 4: %v", err)
	}

	// Allow some time for DirBacking to process the changes
	time.Sleep(100 * time.Millisecond)

	// Verify final state of files
	verifyFileAbsent(t, destPath, "file2.txt", blobStore, goneAddr)                  // file2 should be deleted
	verifyFileContent(t, destPath, "file3.txt", "New content for file 3", blobStore) // file3 should be overwritten
	verifyFileContent(t, destPath, "file4.txt", "Content for file 4", blobStore)     // file4 should be created
}

func verifyFileContent(t *testing.T, destPath, filename, expectedContent string, blobStore *BlobStore) *FileAddr {
	t.Helper()

	destFilePath := filepath.Join(destPath, filename)
	addressContent, err := os.ReadFile(destFilePath)
	if err != nil {
		t.Fatalf("Failed to read destination file address for %s: %v", filename, err)
	}

	fileAddr, err := NewFileAddrFromString(string(addressContent))
	if err != nil {
		t.Fatalf("Invalid file address format in destination file for %s: %v", filename, err)
	}

	cachedFile, err := blobStore.ReadFile(fileAddr)
	if err != nil {
		t.Fatalf("File with address %s not found in BlobStore for %s", fileAddr.String(), filename)
	}
	defer blobStore.Release(cachedFile)

	actualContent, err := os.ReadFile(cachedFile.Path)
	if err != nil {
		t.Fatalf("Failed to read cached file content for %s: %v", filename, err)
	}
	if string(actualContent) != expectedContent {
		t.Errorf("Cached file content mismatch for %s. Expected: %s, got: %s", filename, expectedContent, string(actualContent))
	}

	return fileAddr
}

func verifyFileAbsent(t *testing.T, destPath, filename string, blobStore *BlobStore, goneAddr *FileAddr) {
	t.Helper()

	destFilePath := filepath.Join(destPath, filename)
	if _, err := os.Stat(destFilePath); !os.IsNotExist(err) {
		t.Errorf("Expected file %s to be absent in destination directory, but it exists", filename)
	}

	cachedFile, err := blobStore.ReadFile(goneAddr)
	if err != nil {
		t.Fatalf("Deleted file with address %s not found in BlobStore for %s", goneAddr.String(), filename)
	}
	defer blobStore.Release(cachedFile)

	if cachedFile.RefCount > 1 {
		t.Errorf("Deleted file %s still has a reference count of %d", filename, cachedFile.RefCount)
	}
}