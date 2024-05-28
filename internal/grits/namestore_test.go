package grits

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
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

	nameStore, err := NewNameStore(blobStore)
	if err != nil {
		t.Fatalf("error creating name store: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return nameStore, blobStore, cleanup
}

// Helper function to verify file content
func verifyFileContent(ns *NameStore, path string, expected []byte, t *testing.T) {
	cf, err := ns.LookupAndOpen(path)
	if err != nil {
		t.Fatalf("Failed to lookup %s: %v", path, err)
	}
	defer cf.Release()

	reader, err := cf.Reader()
	if err != nil {
		t.Fatalf("Couldn't open reader for %s", path)
	}
	defer reader.Close()

	actual, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read content of %s: %v", path, err)
	}

	if !bytes.Equal(actual, expected) {
		t.Errorf("Content mismatch for %s. Got: %s, Want: %s", path, string(actual), string(expected))
	}
}

// Helper function to create and link a GNode
func createAndLinkGNode(ns *NameStore, bs BlobStore, path string, content []byte, isDir bool) (*GNode, error) {
	if isDir {
		// Create directory GNode
		emptyChildren := make(map[string]*GNode)

		dirGNode, err := CreateTreeGNode(ns.BlobStore, emptyChildren)
		if err != nil {
			return nil, err
		}
		defer dirGNode.Release()

		return dirGNode, ns.LinkGNode(path, dirGNode)
	} else {
		// Create file GNode
		cachedFile, err := bs.AddDataBlock(content)
		if err != nil {
			return nil, err
		}
		defer cachedFile.Release()

		fileGNode, err := CreateBlobGNode(ns.BlobStore, FileContentAddr(cachedFile.GetAddress()))
		if err != nil {
			return nil, err
		}
		defer fileGNode.Release()

		return fileGNode, ns.LinkGNode(path, fileGNode)
	}
}

func TestCreateAndLookupFilesAndDirectories(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Add and link a file
	fileContent := []byte("Hello, NameStore!")

	fileGNode, err := createAndLinkGNode(nameStore, blobStore, "file.txt", fileContent, false)
	if err != nil {
		t.Fatalf("Failed to link blob to file.txt: %v", err)
	}
	defer fileGNode.Release()

	// Add and link an empty directory
	dirGNode, err := createAndLinkGNode(nameStore, blobStore, "dir", nil, true)
	if err != nil {
		t.Fatalf("Failed to mkdir for test dir: %v", err)
	}
	defer dirGNode.Release()

	// Link the file into the directory
	subFileGNode, err := createAndLinkGNode(nameStore, blobStore, "dir/file.txt", fileContent, false)
	if err != nil {
		t.Fatalf("Failed to link blob to dir/file.txt: %v", err)
	}
	defer subFileGNode.Release()

	// Lookup and open the main file
	mainCf, err := nameStore.LookupAndOpen("file.txt")
	if err != nil {
		t.Fatalf("Failed to lookup file.txt: %v", err)
	}
	defer mainCf.Release()
	verifyFileContent(nameStore, "file.txt", fileContent, t)

	// Lookup and open the file inside the directory
	subCf, err := nameStore.LookupAndOpen("dir/file.txt")
	if err != nil {
		t.Fatalf("Failed to lookup dir/file.txt: %v", err)
	}
	defer subCf.Release()
	verifyFileContent(nameStore, "dir/file.txt", fileContent, t)

	// Test a nonexistent file lookup
	_, err = nameStore.LookupAndOpen("nonexistent.txt")
	if err == nil {
		t.Errorf("Expected error when looking up nonexistent file, but got none")
	}
}

func TestUpdatingFiles(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Initial setup: Create and link a GNode with initial content
	initialContent := []byte("Initial content")
	initialGNode, err := createAndLinkGNode(nameStore, blobStore, "updateTest.txt", initialContent, false)
	if err != nil {
		t.Fatalf("Failed to create and link GNode for initial content: %v", err)
	}
	defer initialGNode.Release()

	// Update the file: Create and link a new GNode with updated content
	updatedContent := []byte("Updated content")
	updatedGNode, err := createAndLinkGNode(nameStore, blobStore, "updateTest.txt", updatedContent, false)
	if err != nil {
		t.Fatalf("Failed to create and link GNode for updated content: %v", err)
	}
	defer updatedGNode.Release()

	// Verify that the update is reflected
	verifyFileContent(nameStore, "updateTest.txt", updatedContent, t)
}

func TestRemoveFilesAndDirectories(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Create and link an empty directory
	dirGNode, err := createAndLinkGNode(nameStore, blobStore, "dir", nil, true)
	if err != nil {
		t.Fatalf("Failed to create and link GNode for directory 'dir': %v", err)
	}
	defer dirGNode.Release()

	// Create and link a sub-directory within "dir"
	subDirGNode, err := createAndLinkGNode(nameStore, blobStore, "dir/subdir", nil, true)
	if err != nil {
		t.Fatalf("Failed to create and link GNode for sub-directory 'dir/subdir': %v", err)
	}
	defer subDirGNode.Release()

	// Create and link a file within "dir/subdir"
	fileContent := []byte("file content")
	fileGNode, err := createAndLinkGNode(nameStore, blobStore, "dir/subdir/file.txt", fileContent, false)
	if err != nil {
		t.Fatalf("Failed to create and link GNode for file 'dir/subdir/file.txt': %v", err)
	}
	defer fileGNode.Release()

	// Attempt to remove "dir/subdir/file.txt"
	if err := nameStore.LinkGNode("dir/subdir/file.txt", nil); err != nil {
		t.Fatalf("Failed to unlink 'dir/subdir/file.txt': %v", err)
	}
	if _, err := nameStore.LookupAndOpen("dir/subdir/file.txt"); err == nil {
		t.Error("Expected 'dir/subdir/file.txt' to be removed, but it was found")
	}

	// Attempt to remove "dir" and verify its contents are also removed
	if err := nameStore.LinkGNode("dir", nil); err != nil {
		t.Fatalf("Failed to unlink '/dir': %v", err)
	}
	if _, err := nameStore.LookupAndOpen("dir/subdir"); err == nil {
		t.Error("Expected 'dir/subdir' to be removed along with 'dir', but it was found")
	}

	// Verify that attempting to remove a non-existent directory does not cause errors
	if err := nameStore.LinkGNode("nonexistent", nil); err != nil {
		t.Errorf("Expected no error when attempting to remove a non-existent directory, got: %v", err)
	}
}
func TestComplexDirectoryStructures(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Prepare a GNode for the root directory
	rootDir, err := CreateTreeGNode(blobStore, make(map[string]*GNode))
	if err != nil {
		t.Fatalf("Failed to create root directory GNode: %v", err)
	}
	defer rootDir.Release()

	// Link the root directory as the starting point
	if err := nameStore.LinkGNode("", rootDir); err != nil {
		t.Fatalf("Failed to link root directory: %v", err)
	}

	// Define a complex directory structure with nested directories and files.
	structure := map[string]bool{
		"dir1":                     true,
		"dir1/dir2":                true,
		"dir1/dir2/dir3":           true,
		"dir1/file1.txt":           false,
		"dir1/dir2/file2.txt":      false,
		"dir1/dir2/dir3/file3.txt": false,
	}

	// Convert map to a slice for sorting
	paths := make([]string, 0, len(structure))
	for path := range structure {
		paths = append(paths, path)
	}
	sort.Strings(paths) // Sort paths to ensure directory creation order

	// Create and link directories and files in order
	for _, path := range paths {
		isDir := structure[path]
		if isDir {
			dirGNode, err := CreateTreeGNode(blobStore, make(map[string]*GNode))
			if err != nil {
				t.Fatalf("Failed to create directory GNode for %s: %v", path, err)
			}

			if err := nameStore.LinkGNode(path, dirGNode); err != nil {
				t.Errorf("Failed to link directory %s: %v", path, err)
			}
		} else {
			fileContent := []byte("This is a test file for " + path)
			fileCachedFile, err := blobStore.AddDataBlock(fileContent)
			if err != nil {
				t.Errorf("Failed to create cached file for %s: %v", path, err)
			}

			fileGNode, err := CreateBlobGNode(blobStore, FileContentAddr(fileCachedFile.GetAddress()))
			if err != nil {
				t.Fatalf("Failed to create file GNode for %s: %v", path, err)
			}

			if err := nameStore.LinkGNode(path, fileGNode); err != nil {
				t.Errorf("Failed to link file %s: %v", path, err)
			}
		}
	}

	// Verify that each file can be looked up correctly.
	for _, path := range paths {
		_, err := nameStore.LookupAndOpen(path)
		if err != nil {
			t.Errorf("Failed to lookup %s correctly: %v", path, err)
		}
	}

	// Now, remove a top-level directory and verify all nested contents are also removed.
	if err := nameStore.LinkGNode("dir1/dir2/", nil); err != nil {
		t.Errorf("Failed to remove dir1/dir2/: %v", err)
	}

	// Attempt to look up a file in the removed directory and expect an error.
	_, err = nameStore.LookupAndOpen("dir1/dir2/file2.txt")
	if err == nil {
		t.Error("Expected an error when looking up a file in a removed directory, but got none")
	}
}

func TestConcurrentAccess(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Prepare a directory GNode for testing
	rootDir, err := CreateTreeGNode(blobStore, make(map[string]*GNode))
	if err != nil {
		t.Fatalf("Failed to create root directory GNode: %v", err)
	}

	if err := nameStore.LinkGNode("concurrent", rootDir); err != nil {
		t.Fatalf("Failed to link root directory for concurrent testing: %v", err)
	}

	var wgLink, wgLookup sync.WaitGroup

	// Number of concurrent operations
	concurrencyLevel := 100

	// Perform concurrent linking operations
	for i := 0; i < concurrencyLevel; i++ {
		wgLink.Add(1)
		go func(i int) {
			defer wgLink.Done()

			dirPath := fmt.Sprintf("concurrent/dir%d", i)
			dirGNode, err := CreateTreeGNode(blobStore, make(map[string]*GNode))
			if err != nil {
				t.Errorf("Failed to create directory GNode for %s: %v", dirPath, err)
				return
			}

			if err := nameStore.LinkGNode(dirPath, dirGNode); err != nil {
				t.Errorf("Failed to link directory %s: %v", dirPath, err)
			}

			filePath := fmt.Sprintf("%s/file%d.txt", dirPath, i)
			fileContent := []byte(fmt.Sprintf("File %d content", i))
			fileCachedFile, err := blobStore.AddDataBlock(fileContent)
			if err != nil {
				t.Errorf("Failed to create cached file for %s: %v", filePath, err)
			}

			fileGNode, err := CreateBlobGNode(blobStore, FileContentAddr(fileCachedFile.GetAddress()))
			if err != nil {
				t.Errorf("Failed to create file GNode for %s: %v", filePath, err)
				return
			}

			if err := nameStore.LinkGNode(filePath, fileGNode); err != nil {
				t.Errorf("Failed to link file %s: %v", filePath, err)
			}
		}(i)
	}

	wgLink.Wait() // Wait for all link operations to complete

	// Perform concurrent lookup operations
	for i := 0; i < concurrencyLevel; i++ {
		wgLookup.Add(1)
		go func(i int) {
			defer wgLookup.Done()
			filePath := fmt.Sprintf("concurrent/dir%d/file%d.txt", i, i)
			_, err := nameStore.LookupAndOpen(filePath)
			if err != nil {
				t.Errorf("Failed to lookup %s: %v", filePath, err)
			}
		}(i)
	}

	done := make(chan bool)
	go func() {
		wgLookup.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Test completed within the time limit
	case <-time.After(10 * time.Second):
		t.Error("Test exceeded time limit of 10 seconds")
	}
}

func TestFileNodeReferenceCounting(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	allFiles := make([]*LocalCachedFile, 0)
	allGNodes := make([]*GNode, 0)

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

		gNode, err := CreateBlobGNode(bs, FileContentAddr(cf.Address))
		if err != nil {
			t.Fatalf("Failed to create gnode for file %d", i)
		}

		allGNodes = append(allGNodes, gNode)
	}

	// 2. Link blobs and dirs
	dirNames := []string{"someplace", "someplace/else", "dir", "dir/sub"}
	for _, dirName := range dirNames {
		gnode, err := createAndLinkGNode(ns, bs, dirName, nil, true)
		if err != nil {
			t.Errorf("Failed to mkdir %s: %v", dirName, err)
		}
		gnode.Release()
	}

	for i, gnode := range allGNodes {
		expectedRefCount := 1
		if gnode.refCount != expectedRefCount {
			t.Errorf("1: Expected GN reference count of %d for %d, got %d", expectedRefCount, i, gnode.refCount)
		}
	}

	fileNames := []string{"zero", "one", "someplace/else/two", "dir/three", "dir/sub/four", "five"}
	for i, fileName := range fileNames {
		err := ns.LinkGNode(fileName, allGNodes[i])
		if err != nil {
			t.Errorf("Failed to link blob to %s: %v", fileName, err)
		}
	}

	for i, gnode := range allGNodes {
		expectedRefCount := 1
		if i <= 5 {
			expectedRefCount = 2
		}
		if gnode.refCount != expectedRefCount {
			t.Errorf("2: Expected GN reference count of %d for %d, got %d", expectedRefCount, i, gnode.refCount)
		}
	}

	for i, cf := range allFiles {
		expectedRefCount := 1
		if cf.RefCount != 1 {
			t.Errorf("Expected CF reference count of %d for %d, got %d", expectedRefCount, i, cf.RefCount)
		}
	}

	// 3. Unlink one and five
	err := ns.LinkGNode(fileNames[1], nil)
	if err != nil {
		t.Errorf("Failed to unlink one: %v", err)
	}

	err = ns.LinkGNode(fileNames[5], nil)
	if err != nil {
		t.Errorf("Failed to unlink five: %v", err)
	}

	// 4. Check reference counts again
	for i, gn := range allGNodes {
		var expectedRefCount int
		if i == 1 || i == 5 || i > 5 {
			expectedRefCount = 1
		} else {
			expectedRefCount = 2
		}
		if allFiles[i].RefCount != expectedRefCount {
			t.Errorf("After unlinking, expected reference count of %d for %d, got %d", expectedRefCount, i, allFiles[i].RefCount)
		}

		if gn.IsDirectory {
			t.Errorf("FileNode for %d is not a BlobNode", i)
		}

		if gn.refCount != expectedRefCount-1 {
			t.Errorf("After unlinking, expected reference count of %d for file node %d, got %d", expectedRefCount-1, i, gn.refCount)
		}
	}

	// 5. Try unlinking a whole subdirectory
	err = ns.LinkGNode("someplace", nil)
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

	newRoot := make(map[string]GNodeAddr)
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

	gNode, err := CreateBlobGNode(bs, FileContentAddr(cf.GetAddress()))
	if err != nil {
		t.Fatalf("Failed to create gnode for whole tree rev")
	}

	ns.LinkGNode("", gNode)
	cf.Release()

	log.Printf("--- Verify tree setup")
	DebugPrintTree(ns.root, "")

	cf, err = ns.LookupAndOpen("tree/zero")
	if err != nil {
		t.Fatalf("Failed to lookup tree/zero: %v", err)
	}

	if cf.GetAddress() != allFiles[0].GetAddress() {
		t.Errorf("Expected tree/zero to point to %s, got %s", allFiles[0].GetAddress(), cf.GetAddress())
	}
	cf.Release()

	log.Printf("--- Scaffolding directories")

	_, err = createAndLinkGNode(ns, bs, "tree/someplace", nil, true)
	if err != nil {
		t.Errorf("Failed to mkdir %s: %v", "tree/someplace", err)
	}

	_, err = createAndLinkGNode(ns, bs, "tree/someplace/else", nil, true)
	if err != nil {
		t.Errorf("Failed to mkdir %s: %v", "tree/someplace/else", err)
	}

	log.Printf("--- Starting revisions")

	for i := 6; i < 10; i++ {
		newRoot := make(map[string]GNodeAddr)

		currentRootGNode, err := ns.LookupAndOpen("tree")
		if err != nil {
			t.Fatalf("Failed to lookup tree: %v", err)
		}

		newRoot["prev"] = GNodeAddr(currentRootGNode.GetAddress()) // Using GNodeAddr to convert address to GNode address type
		currentRootGNode.Release()                                 // Release after use

		// Serialize the new root with its metadata to JSON
		newRootJson, err := json.Marshal(newRoot)
		if err != nil {
			t.Fatalf("Failed to serialize newRoot to JSON: %v", err)
		}

		log.Printf("We serialize JSON: %s\n", newRootJson)

		// Add the serialized new root JSON as a new data block to the BlobStore
		cf, err := bs.AddDataBlock(newRootJson)
		if err != nil {
			t.Fatalf("Failed to add new root JSON to BlobStore: %v", err)
		}

		newRootGNode, err := CreateBlobGNode(bs, FileContentAddr(cf.GetAddress()))
		if err != nil {
			t.Fatalf("Failed to create GNode for whole tree revision: %v", err)
		}

		err = ns.LinkGNode("", newRootGNode)
		if err != nil {
			t.Fatalf("Failed to link new root: %v", err)
		}
		cf.Release() // Release the cached file after linking

		// Link new files under the new root directory
		fileGNode, err := CreateBlobGNode(bs, FileContentAddr(allFiles[i].GetAddress()))
		if err != nil {
			t.Fatalf("Failed to create GNode for file %s: %v", fileNames[i-6], err)
		}

		err = ns.LinkGNode(fmt.Sprintf("tree/%d", i), fileGNode)
		if err != nil {
			t.Fatalf("Failed to link tree/%d: %v", i, err)
		}

		err = ns.LinkGNode(fmt.Sprintf("tree/%s", fileNames[i-6]), fileGNode)
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
		gNode, exists := ns.fileCache[GNodeAddr(cf.GetAddress())]
		if !exists {
			t.Errorf("FileNode for %s not found in cache", cf.GetAddress())
			continue
		}

		actualRefCount := gNode.refCount

		var fileName string
		if i < 6 {
			fileName = fileNames[i]
		} else {
			fileName = fmt.Sprintf("%d", i)
		}
		log.Printf("File %d - %s: expected %d, got %d (hash is %s)", i, fileName, expectedRefCounts[i], actualRefCount, cf.GetAddress())

		if actualRefCount != expectedRefCounts[i] {
			t.Errorf("After big revision setup, expected reference count of %d for file %d, got %d", expectedRefCounts[i], i, actualRefCount)
		}
	}

	// 7. Try unlinking the whole tree and make sure things get cleaned up
	log.Printf("--- Starting test 7, full unlink")
	DebugPrintTree(ns.root, "")

	err = ns.LinkGNode("", nil)
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
