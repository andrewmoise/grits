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

func setupNameStoreTestEnv(t *testing.T) (*NameStore, *LocalBlobStore, func()) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "namestore_test")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}

	log.Printf("Setup in %s\n", tempDir)

	config := NewConfig(tempDir)
	blobStore := NewLocalBlobStore(config)

	nameStore, err := EmptyNameStore(blobStore)
	if err != nil {
		t.Fatalf("Failed to initialize NameStore: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return nameStore, blobStore, cleanup
}

func TestCreateAndLookupFilesAndDirectories(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	fileContent := []byte("Hello, NameStore!")
	cachedFileInterface, err := blobStore.AddDataBlock(fileContent)
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer cachedFile.Release()

	err = nameStore.LinkBlob("file.txt", cachedFile.GetAddress(), cachedFile.GetSize())
	if err != nil {
		t.Errorf("Failed to link blob to file.txt: %v", err)
	}

	emptyDirInterface, err := blobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Fatalf("Failed to add empty dir blob: %v", err)
	}
	emptyDir, ok := emptyDirInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer emptyDir.Release()

	err = nameStore.LinkTree("dir", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir for test dir: %v", err)
	}

	err = nameStore.LinkBlob("dir/file.txt", cachedFile.GetAddress(), cachedFile.GetSize())
	if err != nil {
		t.Errorf("Failed to link blob to dir/file.txt: %v", err)
	}

	mainCfInterface, err := nameStore.LookupAndOpen("file.txt")
	if err != nil {
		t.Errorf("Failed to lookup file.txt: %v", err)
	}
	mainCf, ok := mainCfInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer mainCf.Release()

	subCfInterface, err := nameStore.LookupAndOpen("dir/file.txt")
	if err != nil {
		t.Errorf("Failed to lookup dir/file.txt: %v", err)
	}
	subCf, ok := subCfInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer subCf.Release()

	_, err = nameStore.LookupAndOpen("nonexistent.txt")
	if err == nil {
		t.Errorf("Expected error when looking up nonexistent file, but got none")
	}
}

func TestUpdatingFiles(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Initial setup: Link a blob to /updateTest.txt
	initialContent := []byte("Initial content")
	initialBlobInterface, err := blobStore.AddDataBlock(initialContent)
	if err != nil {
		t.Fatalf("Failed to add initial data block to BlobStore: %v", err)
	}
	initialBlob, ok := initialBlobInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	err = nameStore.LinkBlob("updateTest.txt", initialBlob.GetAddress(), initialBlob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link initial blob to updateTest.txt: %v", err)
	}

	// Update the file: Link a new blob to /updateTest.txt
	updatedContent := []byte("Updated content")
	updatedBlobInterface, err := blobStore.AddDataBlock(updatedContent)
	if err != nil {
		t.Fatalf("Failed to add updated data block to BlobStore: %v", err)
	}
	updatedBlob, ok := updatedBlobInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	err = nameStore.LinkBlob("updateTest.txt", updatedBlob.GetAddress(), updatedBlob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link updated blob to updateTest.txt: %v", err)
	}

	// Lookup /updateTest.txt and verify the blob reflects the update
	cfInterface, err := nameStore.LookupAndOpen("updateTest.txt")
	if err != nil {
		t.Fatalf("Failed to lookup updateTest.txt after update: %v", err)
	}
	cf, ok := cfInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer cf.Release()

	// Read the content of the file from the BlobStore
	readBlobInterface, err := blobStore.ReadFile(cf.GetAddress())
	if err != nil {
		t.Fatalf("Failed to read updated blob from BlobStore: %v", err)
	}
	readBlob, ok := readBlobInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}

	content, err := os.ReadFile(readBlob.GetPath())
	if err != nil {
		t.Fatalf("Failed to read content of the updated file: %v", err)
	}

	// Verify the content matches the updated content
	if string(content) != string(updatedContent) {
		t.Errorf("Content of updateTest.txt does not match updated content. Got: %s, Want: %s", string(content), string(updatedContent))
	}
}

// TestRemoveFilesAndDirectories tests removing files and directories.
func TestRemoveFilesAndDirectories(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	emptyDirInterface, err := blobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	emptyDir, ok := emptyDirInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer emptyDir.Release()

	err = nameStore.LinkTree("dir", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir")
	}

	err = nameStore.LinkTree("dir/subdir", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir/subdir")
	}

	// Simulate linking a blob to "dir/subdir/file.txt"
	cachedFileInterface, err := blobStore.AddDataBlock([]byte("file content"))
	if err != nil {
		t.Fatalf("Failed to add blob to BlobStore: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	if err := nameStore.LinkBlob("dir/subdir/file.txt", cachedFile.GetAddress(), cachedFile.GetSize()); err != nil {
		t.Fatalf("Failed to link blob to dir/subdir/file.txt: %v", err)
	}

	// Remove "/dir/subdir/file.txt" and verify it's no longer found
	if err := nameStore.Link("dir/subdir/file.txt", nil); err != nil {
		t.Fatalf("Failed to remove dir/subdir/file.txt: %v", err)
	}
	if _, err := nameStore.LookupAndOpen("dir/subdir/file.txt"); err == nil {
		t.Error("Expected dir/subdir/file.txt to be removed, but it was found")
	}

	// Attempt to remove "dir" and verify its contents are also removed
	if err := nameStore.Link("dir", nil); err != nil {
		t.Fatalf("Failed to remove /dir: %v", err)
	}
	if _, err := nameStore.LookupAndOpen("dir/subdir"); err == nil {
		t.Error("Expected dir/subdir to be removed along with dir, but it was found")
	}

	// Verify that attempting to remove a non-existent directory does not cause errors
	if err := nameStore.Link("nonexistent", nil); err != nil {
		t.Errorf("Expected no error when attempting to remove a non-existent directory, got: %v", err)
	}
}

// TestComplexDirectoryStructures tests creating, looking up, and removing nested directory structures.
func TestComplexDirectoryStructures(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Add a file to the BlobStore for linking in the NameStore.
	fileContent := []byte("This is a test.")
	cachedFileInterface, err := blobStore.AddDataBlock(fileContent)
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}

	// Define a complex directory structure with nested directories and files.
	structure := []string{
		"dir1/file1.txt",
		"dir1/dir2/file2.txt",
		"dir1/dir2/dir3/file3.txt",
	}

	emptyDirInterface, err := blobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	emptyDir, ok := emptyDirInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer emptyDir.Release()

	err = nameStore.LinkTree("dir1", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir1")
	}

	err = nameStore.LinkTree("dir1/dir2", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir2")
	}

	err = nameStore.LinkTree("dir1/dir2/dir3", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir3")
	}

	// Link the files into the NameStore according to the structure.
	for _, path := range structure {
		if err := nameStore.LinkBlob(path, cachedFile.GetAddress(), cachedFile.GetSize()); err != nil {
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
	if err := nameStore.Link("dir1/dir2", nil); err != nil {
		t.Errorf("Failed to remove dir1/dir2: %v", err)
	}

	// Attempt to look up a file in the removed directory and expect an error.
	_, err = nameStore.LookupAndOpen("dir1/dir2/file2.txt")
	if err == nil {
		t.Errorf("Expected an error when looking up a file in a removed directory, but got none")
	}

	// Lastly, verify that removing the root directory clears everything.
	if err := nameStore.Link("", nil); err != nil {
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
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Prepare a blob to use for linking
	fileContent := []byte("Concurrent access test content")
	cachedFileInterface, err := blobStore.AddDataBlock(fileContent)
	if err != nil {
		t.Fatalf("Failed to add data block to BlobStore: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}

	emptyDirInterface, err := blobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	emptyDir, ok := emptyDirInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer emptyDir.Release()

	err = nameStore.LinkTree("concurrent", emptyDir.GetAddress())
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
			err = nameStore.LinkTree(dirPath, emptyDir.GetAddress())
			if err != nil {
				t.Errorf("Failed to mkdir %s", dirPath)
			}

			filePath := fmt.Sprintf("concurrent/dir%d/file%d.txt", i, i)
			err := nameStore.LinkBlob(filePath, cachedFile.GetAddress(), cachedFile.GetSize())
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

	allFiles := make([]*LocalCachedFile, 0)

	// 1. Create blobs and directories
	for i := 0; i < 10; i++ {
		data := []byte("Content of file " + strconv.Itoa(i))
		cfInterface, err := bs.AddDataBlock(data)
		if err != nil {
			t.Fatalf("Failed to add data block for file %d: %v", i, err)
		}
		cf, ok := cfInterface.(*LocalCachedFile)
		if !ok {
			t.Fatalf("Failed to assert type *LocalCachedFile for file %d", i)
		}
		allFiles = append(allFiles, cf)
	}

	emptyDirInterface, err := bs.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Errorf("Failed to add empty dir blob")
	}
	emptyDir, ok := emptyDirInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile for empty dir")
	}
	defer emptyDir.Release()

	// 2. Link blobs and dirs
	dirNames := []string{"someplace", "someplace/else", "dir", "dir/sub"}
	for _, dirName := range dirNames {
		err := ns.LinkTree(dirName, emptyDir.GetAddress())
		if err != nil {
			t.Errorf("Failed to mkdir %s: %v", dirName, err)
		}
	}

	fileNames := []string{"zero", "one", "someplace/else/two", "dir/three", "dir/sub/four", "five"}
	for i, fileName := range fileNames {
		err := ns.LinkBlob(fileName, allFiles[i].GetAddress(), allFiles[i].GetSize())
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
	err = ns.LinkBlob(fileNames[1], nil, allFiles[1].GetSize())
	if err != nil {
		t.Errorf("Failed to unlink one: %v", err)
	}

	err = ns.LinkBlob(fileNames[5], nil, allFiles[5].GetSize())
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

		fn, exists := ns.fileCache[NewTypedFileAddr(cf.GetAddress().Hash, cf.GetSize(), Blob).String()]
		if !exists {
			if expectedRefCount-1 <= 0 {
				continue
			}
			t.Errorf("FileNode for %s not found in cache", cf.GetAddress().String())
		}

		bn, ok := fn.(*BlobNode)
		if !ok {
			t.Errorf("FileNode for %s is not a BlobNode", cf.GetAddress().String())
		}

		if bn.refCount != expectedRefCount-1 {
			t.Errorf("After unlinking, expected reference count of %d for file node %d, got %d", expectedRefCount-1, i, bn.refCount)
		}
	}

	// 5. Try unlinking a whole subdirectory
	err = ns.Link("someplace", nil)
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

	tfa := NewTypedFileAddr(cf.GetAddress().Hash, cf.GetSize(), Tree)

	ns.Link("", tfa)
	cf.Release()

	log.Printf("--- Verify tree setup")
	DebugPrintTree(ns.root, "")

	cf, err = ns.LookupAndOpen("tree/zero")
	if err != nil {
		t.Fatalf("Failed to lookup tree/zero: %v", err)
	}

	if cf.GetAddress().Hash != allFiles[0].GetAddress().Hash {
		t.Errorf("Expected tree/zero to point to %s, got %s", allFiles[0].GetAddress().Hash, cf.GetAddress().Hash)
	}
	cf.Release()

	log.Printf("--- Scaffolding directories")

	err = ns.LinkTree("tree/someplace", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir %s: %v", "tree/someplace", err)
	}

	err = ns.LinkTree("tree/someplace/else", emptyDir.GetAddress())
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

		tfa := NewTypedFileAddr(cf.GetAddress().Hash, cf.GetSize(), Tree)
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

		tfa = NewTypedFileAddr(cf.GetAddress().Hash, cf.GetSize(), Tree)

		err = ns.Link("", tfa)
		if err != nil {
			t.Fatalf("Failed to link new root: %v", err)
		}

		err = ns.LinkBlob(fmt.Sprintf("tree/%d", i), allFiles[i].GetAddress(), allFiles[i].GetSize())
		if err != nil {
			t.Fatalf("Failed to link tree/%d: %v", i, err)
		}

		err = ns.LinkBlob(fmt.Sprintf("tree/%s", fileNames[i-6]), allFiles[i].GetAddress(), allFiles[i].GetSize())
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
		tfa := NewTypedFileAddr(cf.GetAddress().Hash, cf.GetSize(), Blob)

		fileNode, exists := ns.fileCache[tfa.String()]
		if !exists {
			t.Errorf("FileNode for %s not found in cache", cf.GetAddress().String())
			continue
		}

		blobNode, isBlobNode := fileNode.(*BlobNode)
		if !isBlobNode {
			t.Errorf("FileNode for %s is not a BlobNode", cf.GetAddress().String())
			continue
		}

		actualRefCount := blobNode.refCount

		var fileName string
		if i < 6 {
			fileName = fileNames[i]
		} else {
			fileName = fmt.Sprintf("%d", i)
		}
		log.Printf("File %d - %s: expected %d, got %d (hash is %s)", i, fileName, expectedRefCounts[i], actualRefCount, cf.GetAddress().Hash)

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
