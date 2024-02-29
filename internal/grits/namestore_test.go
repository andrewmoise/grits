package grits

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

// Helper function to setup a test environment with NameStore and BlobStore.
func setupNameStoreTestEnv(t *testing.T) (*NameStore, *BlobStore, func()) {
	// Setup a temporary directory for BlobStore
	tempDir, err := os.MkdirTemp("", "namestore_test")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}

	// Create a BlobStore using the temporary directory
	config := NewConfig()
	config.ServerDir = tempDir
	blobStore := NewBlobStore(config)

	// Initialize the NameStore with the BlobStore
	nameStore, err := EmptyNameStore(blobStore)
	if err != nil {
		t.Fatalf("Failed to initialize NameStore: %v", err)
	}

	// Cleanup function to delete temporary directory after test
	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return nameStore, blobStore, cleanup
}

// Test creating and looking up files and directories.
func TestCreateAndLookupFilesAndDirectories(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Test data setup
	fileContent := []byte("Hello, NameStore!")
	cachedFile, err := blobStore.AddDataBlock(fileContent, ".txt")
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}

	// Link a blob to /file.txt
	err = nameStore.LinkBlob("file.txt", cachedFile.Address)
	if err != nil {
		t.Errorf("Failed to link blob to file.txt: %v", err)
	}

	// Link a blob to /dir/file.txt (implicitly creating /dir)
	err = nameStore.LinkBlob("dir/file.txt", cachedFile.Address)
	if err != nil {
		t.Errorf("Failed to link blob to dir/file.txt: %v", err)
	}

	// Lookup /file.txt
	cf, err := nameStore.Lookup("file.txt")
	if err != nil {
		t.Errorf("Failed to lookup file.txt: %v", err)
	}
	defer nameStore.blobStore.Release(cf)

	// Lookup /dir/file.txt
	cf, err = nameStore.Lookup("dir/file.txt")
	if err != nil {
		t.Errorf("Failed to lookup dir/file.txt: %v", err)
	}
	defer nameStore.blobStore.Release(cf)

	// Attempt to lookup a non-existent file
	_, err = nameStore.Lookup("nonexistent.txt")
	if err == nil {
		t.Errorf("Expected error when looking up nonexistent file, but got none")
	}
}

// TestUpdatingFiles ensures that updating an existing file's blob is reflected in the NameStore.
func TestUpdatingFiles(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Initial setup: Link a blob to /updateTest.txt
	initialContent := []byte("Initial content")
	initialBlob, err := blobStore.AddDataBlock(initialContent, ".txt")
	if err != nil {
		t.Fatalf("Failed to add initial data block to BlobStore: %v", err)
	}
	err = nameStore.LinkBlob("updateTest.txt", initialBlob.Address)
	if err != nil {
		t.Fatalf("Failed to link initial blob to updateTest.txt: %v", err)
	}

	// Update the file: Link a new blob to /updateTest.txt
	updatedContent := []byte("Updated content")
	updatedBlob, err := blobStore.AddDataBlock(updatedContent, ".txt")
	if err != nil {
		t.Fatalf("Failed to add updated data block to BlobStore: %v", err)
	}
	err = nameStore.LinkBlob("updateTest.txt", updatedBlob.Address)
	if err != nil {
		t.Fatalf("Failed to link updated blob to updateTest.txt: %v", err)
	}

	// Lookup /updateTest.txt and verify the blob reflects the update
	cf, err := nameStore.Lookup("updateTest.txt")
	if err != nil {
		t.Fatalf("Failed to lookup updateTest.txt after update: %v", err)
	}
	defer blobStore.Release(cf)

	// Read the content of the file from the BlobStore
	readBlob, err := blobStore.ReadFile(cf.Address)
	if err != nil {
		t.Fatalf("Failed to read updated blob from BlobStore: %v", err)
	}

	content, err := os.ReadFile(readBlob.Path)
	if err != nil {
		t.Fatalf("Failed to read content of the updated file: %v", err)
	}

	// Verify the content matches the updated content
	if string(content) != string(updatedContent) {
		t.Errorf("Content of updateTest.txt does not match updated content. Got: %s, Want: %s", string(content), string(updatedContent))
	}
}

func TestRemoveFilesAndDirectories(t *testing.T) {
	ns, bs, cleanupBlobStore := setupNameStoreTestEnv(t)
	defer cleanupBlobStore()

	// Simulate linking a blob to "dir/subdir/file.txt"
	cachedFile, err := bs.AddDataBlock([]byte("file content"), ".txt")
	if err != nil {
		t.Fatalf("Failed to add blob to BlobStore: %v", err)
	}
	if err := ns.LinkBlob("dir/subdir/file.txt", cachedFile.Address); err != nil {
		t.Fatalf("Failed to link blob to dir/subdir/file.txt: %v", err)
	}

	// Remove "/dir/subdir/file.txt" and verify it's no longer found
	if err := ns.LinkBlob("dir/subdir/file.txt", nil); err != nil {
		t.Fatalf("Failed to remove dir/subdir/file.txt: %v", err)
	}
	if _, err := ns.Lookup("dir/subdir/file.txt"); err == nil {
		t.Error("Expected dir/subdir/file.txt to be removed, but it was found")
	}

	// Attempt to remove "dir" and verify its contents are also removed
	if err := ns.LinkBlob("dir", nil); err != nil {
		t.Fatalf("Failed to remove /dir: %v", err)
	}
	if _, err := ns.Lookup("dir/subdir"); err == nil {
		t.Error("Expected dir/subdir to be removed along with dir, but it was found")
	}

	// Verify that attempting to remove a non-existent directory does not cause errors
	if err := ns.LinkBlob("nonexistent", nil); err != nil {
		t.Errorf("Expected no error when attempting to remove a non-existent directory, got: %v", err)
	}
}

// TestComplexDirectoryStructures tests creating, looking up, and removing nested directory structures.
func TestComplexDirectoryStructures(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Add a file to the BlobStore for linking in the NameStore.
	fileContent := []byte("This is a test.")
	cachedFile, err := blobStore.AddDataBlock(fileContent, ".txt")
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}

	// Define a complex directory structure with nested directories and files.
	structure := []string{
		"dir1/file1.txt",
		"dir1/dir2/file2.txt",
		"dir1/dir2/dir3/file3.txt",
	}

	// Link the files into the NameStore according to the structure.
	for _, path := range structure {
		if err := nameStore.LinkBlob(path, cachedFile.Address); err != nil {
			t.Errorf("Failed to link blob to %s: %v", path, err)
		}
	}

	// Verify that each file can be looked up correctly.
	for _, path := range structure {
		if _, err := nameStore.Lookup(path); err != nil {
			t.Errorf("Failed to lookup %s: %v", path, err)
		}
	}

	// Now, remove a top-level directory and verify all nested contents are also removed.
	if err := nameStore.LinkBlob("dir1/dir2", nil); err != nil {
		t.Errorf("Failed to remove dir1/dir2: %v", err)
	}

	// Attempt to look up a file in the removed directory and expect an error.
	_, err = nameStore.Lookup("dir1/dir2/file2.txt")
	if err == nil {
		t.Errorf("Expected an error when looking up a file in a removed directory, but got none")
	}

	// Lastly, verify that removing the root directory clears everything.
	if err := nameStore.LinkBlob("", nil); err != nil {
		t.Errorf("Failed to remove the root directory: %v", err)
	}

	if nameStore.root != nil {
		t.Errorf("Root directory was not cleared")
	}

	// Attempt to look up any file after clearing the root directory.
	_, err = nameStore.Lookup("dir1/file1.txt")
	if err == nil {
		t.Errorf("Expected an error when looking up any file after clearing the root, but got none")
	}
}

// TestConcurrentAccess tests the NameStore's ability to handle concurrent operations safely.
func TestConcurrentAccess(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Prepare a blob to use for linking
	fileContent := []byte("Concurrent access test content")
	cachedFile, err := blobStore.AddDataBlock(fileContent, ".txt")
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}

	var wgLink, wgLookup sync.WaitGroup

	// Number of concurrent operations
	concurrencyLevel := 100

	// Perform concurrent LinkBlob operations
	for i := 0; i < concurrencyLevel; i++ {
		wgLink.Add(1)
		go func(i int) {
			defer wgLink.Done()
			filePath := fmt.Sprintf("concurrent/dir%d/file%d.txt", i, i)
			err := nameStore.LinkBlob(filePath, cachedFile.Address)
			if err != nil {
				t.Errorf("Failed to link blob to %s: %v", filePath, err)
			}
		}(i)
	}

	// Perform concurrent Lookup operations
	for i := 0; i < concurrencyLevel; i++ {
		wgLookup.Add(1)
		go func(i int) {
			defer wgLookup.Done()
			filePath := fmt.Sprintf("concurrent/dir%d/file%d.txt", i, i)
			_, err := nameStore.Lookup(filePath)
			if err == nil {
				return
			}

			wgLink.Wait()
			_, err = nameStore.Lookup(filePath)
			if err != nil {
				t.Errorf("Failed to lookup %s: %v", filePath, err)
			}
		}(i)
	}

	wgLink.Wait()
	wgLookup.Wait()

	// After all operations complete, verify the integrity of the NameStore
	for i := 0; i < concurrencyLevel; i++ {
		filePath := fmt.Sprintf("concurrent/dir%d/file%d.txt", i, i)
		_, err := nameStore.Lookup(filePath)
		if err != nil {
			t.Errorf("Expected %s to be present after concurrent operations, but it was not found", filePath)
		}
	}
}
