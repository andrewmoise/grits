package grits

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
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
	config := NewConfig(tempDir)
	BlobStore := NewBlobStore(config)

	// Initialize the NameStore with the BlobStore
	nameStore, err := EmptyNameStore(BlobStore)
	if err != nil {
		t.Fatalf("Failed to initialize NameStore: %v", err)
	}

	// Cleanup function to delete temporary directory after test
	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return nameStore, BlobStore, cleanup
}

// Test creating and looking up files and directories.
func TestCreateAndLookupFilesAndDirectories(t *testing.T) {
	nameStore, BlobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Test data setup
	fileContent := []byte("Hello, NameStore!")
	cachedFile, err := BlobStore.AddDataBlock(fileContent)
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}
	defer cachedFile.Release()

	// Link a blob to /file.txt
	err = nameStore.LinkBlob("file.txt", cachedFile.Address)
	if err != nil {
		t.Errorf("Failed to link blob to file.txt: %v", err)
	}

	emptyDir, err := BlobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	defer emptyDir.Release()

	err = nameStore.LinkTree("dir", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir for test dir")
	}

	// Link a blob to /dir/file.txt (implicitly creating /dir)
	err = nameStore.LinkBlob("dir/file.txt", cachedFile.Address)
	if err != nil {
		t.Errorf("Failed to link blob to dir/file.txt: %v", err)
	}

	// Lookup /file.txt
	mainCf, err := nameStore.LookupAndOpen("file.txt")
	if err != nil {
		t.Errorf("Failed to lookup file.txt: %v", err)
	}
	defer mainCf.Release()

	// Lookup /dir/file.txt
	subCf, err := nameStore.LookupAndOpen("dir/file.txt")
	if err != nil {
		t.Errorf("Failed to lookup dir/file.txt: %v", err)
	}
	defer subCf.Release()

	// Attempt to lookup a non-existent file
	_, err = nameStore.LookupAndOpen("nonexistent.txt")
	if err == nil {
		t.Errorf("Expected error when looking up nonexistent file, but got none")
	}
}

// TestUpdatingFiles ensures that updating an existing file's blob is reflected in the NameStore.
func TestUpdatingFiles(t *testing.T) {
	nameStore, BlobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Initial setup: Link a blob to /updateTest.txt
	initialContent := []byte("Initial content")
	initialBlob, err := BlobStore.AddDataBlock(initialContent)
	if err != nil {
		t.Fatalf("Failed to add initial data block to BlobStore: %v", err)
	}
	err = nameStore.LinkBlob("updateTest.txt", initialBlob.Address)
	if err != nil {
		t.Fatalf("Failed to link initial blob to updateTest.txt: %v", err)
	}

	// Update the file: Link a new blob to /updateTest.txt
	updatedContent := []byte("Updated content")
	updatedBlob, err := BlobStore.AddDataBlock(updatedContent)
	if err != nil {
		t.Fatalf("Failed to add updated data block to BlobStore: %v", err)
	}
	err = nameStore.LinkBlob("updateTest.txt", updatedBlob.Address)
	if err != nil {
		t.Fatalf("Failed to link updated blob to updateTest.txt: %v", err)
	}

	// Lookup /updateTest.txt and verify the blob reflects the update
	cf, err := nameStore.LookupAndOpen("updateTest.txt")
	if err != nil {
		t.Fatalf("Failed to lookup updateTest.txt after update: %v", err)
	}
	defer cf.Release()

	// Read the content of the file from the BlobStore
	readBlob, err := BlobStore.ReadFile(cf.Address)
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

	emptyDir, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	defer emptyDir.Release()

	err = ns.LinkTree("dir", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir dir")
	}

	err = ns.LinkTree("dir/subdir", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir dir/subdir")
	}

	// Simulate linking a blob to "dir/subdir/file.txt"
	cachedFile, err := bs.AddDataBlock([]byte("file content"))
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
	if _, err := ns.LookupAndOpen("dir/subdir/file.txt"); err == nil {
		t.Error("Expected dir/subdir/file.txt to be removed, but it was found")
	}

	// Attempt to remove "dir" and verify its contents are also removed
	if err := ns.LinkBlob("dir", nil); err != nil {
		t.Fatalf("Failed to remove /dir: %v", err)
	}
	if _, err := ns.LookupAndOpen("dir/subdir"); err == nil {
		t.Error("Expected dir/subdir to be removed along with dir, but it was found")
	}

	// Verify that attempting to remove a non-existent directory does not cause errors
	if err := ns.LinkBlob("nonexistent", nil); err != nil {
		t.Errorf("Expected no error when attempting to remove a non-existent directory, got: %v", err)
	}
}

// TestComplexDirectoryStructures tests creating, looking up, and removing nested directory structures.
func TestComplexDirectoryStructures(t *testing.T) {
	nameStore, BlobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Add a file to the BlobStore for linking in the NameStore.
	fileContent := []byte("This is a test.")
	cachedFile, err := BlobStore.AddDataBlock(fileContent)
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}

	// Define a complex directory structure with nested directories and files.
	structure := []string{
		"dir1/file1.txt",
		"dir1/dir2/file2.txt",
		"dir1/dir2/dir3/file3.txt",
	}

	emptyDir, err := BlobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	defer emptyDir.Release()

	err = nameStore.LinkTree("dir1", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir dir1")
	}

	err = nameStore.LinkTree("dir1/dir2", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir dir2")
	}

	err = nameStore.LinkTree("dir1/dir2/dir3", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir dir3")
	}

	// Link the files into the NameStore according to the structure.
	for _, path := range structure {
		if err := nameStore.LinkBlob(path, cachedFile.Address); err != nil {
			t.Errorf("Failed to link blob to %s: %v", path, err)
		}
	}

	// Verify that each file can be looked up correctly.
	for _, path := range structure {
		if _, err := nameStore.LookupAndOpen(path); err != nil {
			t.Errorf("Failed to lookup %s: %v", path, err)
		}
	}

	// Now, remove a top-level directory and verify all nested contents are also removed.
	if err := nameStore.LinkBlob("dir1/dir2", nil); err != nil {
		t.Errorf("Failed to remove dir1/dir2: %v", err)
	}

	// Attempt to look up a file in the removed directory and expect an error.
	_, err = nameStore.LookupAndOpen("dir1/dir2/file2.txt")
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
	_, err = nameStore.LookupAndOpen("dir1/file1.txt")
	if err == nil {
		t.Errorf("Expected an error when looking up any file after clearing the root, but got none")
	}
}

// TestConcurrentAccess tests the NameStore's ability to handle concurrent operations safely.
func TestConcurrentAccess(t *testing.T) {
	nameStore, BlobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Prepare a blob to use for linking
	fileContent := []byte("Concurrent access test content")
	cachedFile, err := BlobStore.AddDataBlock(fileContent)
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}

	emptyDir, err := BlobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	defer emptyDir.Release()

	err = nameStore.LinkTree("concurrent", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir concurrent/")
	}

	var wgLink, wgLookup sync.WaitGroup

	// Number of concurrent operations
	concurrencyLevel := 100

	// Perform concurrent LinkBlob operations
	for i := 0; i < concurrencyLevel; i++ {
		wgLink.Add(1)
		go func(i int) {
			defer wgLink.Done()

			dirPath := fmt.Sprintf("concurrent/dir%d", i)
			err = nameStore.LinkTree(dirPath, emptyDir.Address)
			if err != nil {
				t.Errorf("Failed to mkdir %s", dirPath)
			}

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
			_, err := nameStore.LookupAndOpen(filePath)
			if err == nil {
				return
			}

			wgLink.Wait()
			_, err = nameStore.LookupAndOpen(filePath)
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
		_, err := nameStore.LookupAndOpen(filePath)
		if err != nil {
			t.Errorf("Expected %s to be present after concurrent operations, but it was not found", filePath)
		}
	}
}

func TestFileNodeReferenceCounting(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	allFiles := make([]*CachedFile, 0)

	// 1. Create blobs and directories
	for i := 0; i < 10; i++ {
		data := []byte("Content of file " + strconv.Itoa(i))
		cf, err := bs.AddDataBlock(data)
		if err != nil {
			t.Fatalf("Failed to add data block for file %d: %v", i, err)
		}
		allFiles = append(allFiles, cf)
	}

	emptyDir, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	defer emptyDir.Release()

	// 2. Link blobs and dirs
	dirNames := []string{"someplace", "someplace/else", "dir", "dir/sub"}
	for _, dirName := range dirNames {
		err := ns.LinkTree(dirName, emptyDir.Address)
		if err != nil {
			t.Errorf("Failed to mkdir %s: %v", dirName, err)
		}
	}

	fileNames := []string{"zero", "one", "someplace/else/two", "dir/three", "dir/sub/four", "five"}
	for i, fileName := range fileNames {
		err := ns.LinkBlob(fileName, allFiles[i].Address)
		if err != nil {
			t.Errorf("Failed to link blob to %s: %v", fileName, err)
		}
	}

	for i, cf := range allFiles {
		expectedRefCount := 1
		if i <= 5 {
			expectedRefCount = 2
		}
		if cf.RefCount != expectedRefCount {
			t.Errorf("Expected reference count of %d for %d, got %d", expectedRefCount, i, cf.RefCount)
		}
	}

	// 3. Unlink one and five
	err = ns.LinkBlob(fileNames[1], nil)
	if err != nil {
		t.Errorf("Failed to unlink one: %v", err)
	}

	err = ns.LinkBlob(fileNames[5], nil)
	if err != nil {
		t.Errorf("Failed to unlink five: %v", err)
	}

	// 4. Check reference counts again
	for i, cf := range allFiles {
		var expectedRefCount int
		if i == 1 || i == 5 || i > 5 {
			expectedRefCount = 1
		} else {
			expectedRefCount = 2
		}
		if cf.RefCount != expectedRefCount {
			t.Errorf("After unlinking, expected reference count of %d for %d, got %d", expectedRefCount, i, cf.RefCount)
		}

		fn, exists := ns.fileCache[NewTypedFileAddr(cf.Address.Hash, cf.Address.Size, Blob).String()]
		if !exists {
			if expectedRefCount-1 <= 0 {
				continue
			}
			t.Errorf("FileNode for %s not found in cache", cf.Address.String())
		}

		bn, ok := fn.(*BlobNode)
		if !ok {
			t.Errorf("FileNode for %s is not a BlobNode", cf.Address.String())
		}

		if bn.refCount != expectedRefCount-1 {
			t.Errorf("After unlinking, expected reference count of %d for file node %d, got %d", expectedRefCount-1, i, bn.refCount)
		}
	}

	// 5. Try unlinking a whole subdirectory
	err = ns.LinkBlob("someplace", nil)
	if err != nil {
		t.Errorf("Failed to unlink someplace: %v", err)
	}

	for i, cf := range allFiles {
		var expectedRefCount int
		if i == 1 || i == 2 || i == 5 || i > 5 {
			expectedRefCount = 1
		} else {
			expectedRefCount = 2
		}
		if cf.RefCount != expectedRefCount {
			t.Errorf("After unlinking, expected reference count of %d for %d, got %d", expectedRefCount, i, cf.RefCount)
		}
	}

	// 6. Try revision of the whole tree
	log.Printf("--- Starting test 6, tree revision")
	DebugPrintTree(ns.root, "")

	newRoot := make(map[string]string)
	newRoot["tree"] = ns.GetRoot()

	newRootJson, err := json.Marshal(newRoot)
	if err != nil {
		t.Fatalf("Failed to serialize newRoot to JSON: %v", err)
	}

	// Add the JSON as a new data block to the BlobStore
	cf, err := bs.AddDataBlock(newRootJson)
	if err != nil {
		t.Fatalf("Failed to add new root JSON to BlobStore: %v", err)
	}

	tfa := NewTypedFileAddr(cf.Address.Hash, cf.Address.Size, Tree)

	ns.Link("", tfa)
	cf.Release()

	log.Printf("--- Verify tree setup")
	DebugPrintTree(ns.root, "")

	cf, err = ns.LookupAndOpen("tree/zero")
	if err != nil {
		t.Fatalf("Failed to lookup tree/zero: %v", err)
	}

	if cf.Address.Hash != allFiles[0].Address.Hash {
		t.Errorf("Expected tree/zero to point to %s, got %s", allFiles[0].Address.Hash, cf.Address.Hash)
	}
	cf.Release()

	log.Printf("--- Scaffolding directories")

	err = ns.LinkTree("tree/someplace", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir %s: %v", "tree/someplace", err)
	}

	err = ns.LinkTree("tree/someplace/else", emptyDir.Address)
	if err != nil {
		t.Errorf("Failed to mkdir %s: %v", "tree/someplace/else", err)
	}

	log.Printf("--- Starting revisions")

	for i := 6; i < 10; i++ {
		newRoot := make(map[string]string)

		newRoot["prev"] = ns.GetRoot()

		cf, err := ns.LookupAndOpen("tree")
		if err != nil {
			t.Fatalf("Failed to lookup tree: %v", err)
		}

		tfa := NewTypedFileAddr(cf.Address.Hash, cf.Address.Size, Tree)
		newRoot["tree"] = tfa.String()

		newRootJson, err = json.Marshal(newRoot)
		if err != nil {
			t.Fatalf("Failed to serialize newRoot to JSON: %v", err)
		}

		log.Printf("We serialize JSON: %s\n", newRootJson)

		// Add the JSON as a new data block to the BlobStore
		cf, err = bs.AddDataBlock(newRootJson)
		if err != nil {
			t.Fatalf("Failed to add new root JSON to BlobStore: %v", err)
		}

		tfa = NewTypedFileAddr(cf.Address.Hash, cf.Address.Size, Tree)

		err = ns.Link("", tfa)
		if err != nil {
			t.Fatalf("Failed to link new root: %v", err)
		}

		err = ns.LinkBlob(fmt.Sprintf("tree/%d", i), allFiles[i].Address)
		if err != nil {
			t.Fatalf("Failed to link tree/%d: %v", i, err)
		}

		err = ns.LinkBlob(fmt.Sprintf("tree/%s", fileNames[i-6]), allFiles[i].Address)
		if err != nil {
			t.Fatalf("Failed to link tree/%s: %v", fileNames[i-6], err)
		}

		log.Printf("--- Modification %d\n", i)
		DebugPrintTree(ns.root, "")
	}

	log.Printf("--- Release allFiles")

	for _, cf := range allFiles {
		cf.Release()
	}

	log.Printf("--- Check reference counts (in nodes)")
	DebugPrintTree(ns.root, "")

	expectedRefCounts := []int{1, 0, 0, 1, 1, 0, 8, 6, 3, 2}

	for i, cf := range allFiles {
		tfa := NewTypedFileAddr(cf.Address.Hash, cf.Address.Size, Blob)

		fileNode, exists := ns.fileCache[tfa.String()]
		if !exists {
			t.Errorf("FileNode for %s not found in cache", cf.Address.String())
			continue
		}

		blobNode, isBlobNode := fileNode.(*BlobNode)
		if !isBlobNode {
			t.Errorf("FileNode for %s is not a BlobNode", cf.Address.String())
			continue
		}

		actualRefCount := blobNode.refCount

		var fileName string
		if i < 6 {
			fileName = fileNames[i]
		} else {
			fileName = fmt.Sprintf("%d", i)
		}
		log.Printf("File %d - %s: expected %d, got %d (hash is %s)", i, fileName, expectedRefCounts[i], actualRefCount, cf.Address.Hash)

		if actualRefCount != expectedRefCounts[i] {
			t.Errorf("After big revision setup, expected reference count of %d for file %d, got %d", expectedRefCounts[i], i, actualRefCount)
		}
	}

	// 7. Try unlinking the whole tree and make sure things get cleaned up
	log.Printf("--- Starting test 7, full unlink")
	DebugPrintTree(ns.root, "")

	err = ns.Link("", nil)
	if err != nil {
		t.Errorf("Failed to unlink root: %v", err)
	}

	DebugPrintTree(ns.root, "")
	for i, cf := range allFiles {
		if cf.RefCount != 0 {
			t.Errorf("Expected ending reference count of 0 for %d, got %d", i, cf.RefCount)
		}
	}
}
