package grits

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
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

	err = nameStore.linkBlob("file.txt", cachedFile.GetAddress(), cachedFile.GetSize())
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

	err = nameStore.linkTree("dir", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir for test dir: %v", err)
	}

	err = nameStore.linkBlob("dir/file.txt", cachedFile.GetAddress(), cachedFile.GetSize())
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

	nameStore.DebugDumpNamespace()

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
	err = nameStore.linkBlob("updateTest.txt", initialBlob.GetAddress(), initialBlob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link initial blob to updateTest.txt: %v", err)
	}
	nameStore.DebugDumpNamespace()

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
	err = nameStore.linkBlob("updateTest.txt", updatedBlob.GetAddress(), updatedBlob.GetSize())
	if err != nil {
		t.Fatalf("Failed to link updated blob to updateTest.txt: %v", err)
	}
	nameStore.DebugDumpNamespace()

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
	nameStore.DebugDumpNamespace()

	// Read the content of the file from the BlobStore
	readBlobInterface, err := blobStore.ReadFile(cf.GetAddress())
	if err != nil {
		t.Fatalf("Failed to read updated blob from BlobStore: %v", err)
	}
	readBlob, ok := readBlobInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	nameStore.DebugDumpNamespace()

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

	nameStore.DebugDumpNamespace()

	err = nameStore.linkTree("dir", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir")
	}
	nameStore.DebugDumpNamespace()

	err = nameStore.linkTree("dir/subdir", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir/subdir")
	}
	nameStore.DebugDumpNamespace()

	// Simulate linking a blob to "dir/subdir/file.txt"
	cachedFileInterface, err := blobStore.AddDataBlock([]byte("file content"))
	if err != nil {
		t.Fatalf("Failed to add blob to BlobStore: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	if err := nameStore.linkBlob("dir/subdir/file.txt", cachedFile.GetAddress(), cachedFile.GetSize()); err != nil {
		t.Fatalf("Failed to link blob to dir/subdir/file.txt: %v", err)
	}
	nameStore.DebugDumpNamespace()

	// Remove "/dir/subdir/file.txt" and verify it's no longer found
	if err := nameStore.Link("dir/subdir/file.txt", nil); err != nil {
		t.Fatalf("Failed to remove dir/subdir/file.txt: %v", err)
	}
	nameStore.DebugDumpNamespace()
	if _, err := nameStore.LookupAndOpen("dir/subdir/file.txt"); err == nil {
		t.Error("Expected dir/subdir/file.txt to be removed, but it was found")
	}

	// Attempt to remove "dir" and verify its contents are also removed
	if err := nameStore.Link("dir", nil); err != nil {
		t.Fatalf("Failed to remove /dir: %v", err)
	}
	nameStore.DebugDumpNamespace()
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

	err = nameStore.linkTree("dir1", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir1")
	}

	err = nameStore.linkTree("dir1/dir2", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir2")
	}

	err = nameStore.linkTree("dir1/dir2/dir3", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir dir3")
	}

	// Link the files into the NameStore according to the structure.
	for _, path := range structure {
		if err := nameStore.linkBlob(path, cachedFile.GetAddress(), cachedFile.GetSize()); err != nil {
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

	if nameStore.rootAddr != "" {
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

	err = nameStore.linkTree("concurrent", emptyDir.GetAddress())
	if err != nil {
		t.Errorf("Failed to mkdir concurrent/")
	}

	var wgLink, wgLookup sync.WaitGroup

	// Number of concurrent operations
	concurrencyLevel := 100

	// Perform concurrent linkBlob operations
	for i := 0; i < concurrencyLevel; i++ {
		wgLink.Add(1)
		go func(i int) {
			defer wgLink.Done()

			dirPath := fmt.Sprintf("concurrent/dir%d", i)
			localErr := nameStore.linkTree(dirPath, emptyDir.GetAddress())
			if localErr != nil {
				t.Errorf("Failed to mkdir %s", dirPath)
			}

			filePath := fmt.Sprintf("concurrent/dir%d/file%d.txt", i, i)
			err := nameStore.linkBlob(filePath, cachedFile.GetAddress(), cachedFile.GetSize())
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

// Helper to find a FileNode by its content blob address
func (ns *NameStore) lookupNodeByContent(contentHash BlobAddr) FileNode {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	for _, node := range ns.fileCache {
		if node != nil && node.ExportedBlob().GetAddress() == contentHash {
			return node
		}
	}
	return nil
}

func TestFileNodeReferenceCounting(t *testing.T) {
	ns, bs, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	allFiles := make([]*LocalCachedFile, 0)

	// 1. Create content blobs
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

	// Create empty directory node
	emptyDirMap := make(map[string]BlobAddr)
	emptyDirNode, err := ns.CreateTreeNode(emptyDirMap)
	if err != nil {
		t.Fatalf("Failed to create empty tree node: %v", err)
	}
	emptyDirNode.Take()
	defer emptyDirNode.Release()

	emptyTfa := &TypedFileAddr{
		BlobAddr: emptyDirNode.blob.GetAddress(),
		Type:     Tree,
	}

	ns.DumpFileCache()

	if emptyDirNode.refCount != 2 {
		t.Fatalf("Expected ref count 2 for emptyDirNode, got %d", emptyDirNode.refCount)
	}

	// Check the metadata blob ref count
	metadataBlob, ok := emptyDirNode.metadataBlob.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to cast metadata blob to LocalCachedFile")
	}
	if metadataBlob.RefCount != 1 {
		t.Fatalf("Expected metadata blob ref count 1, got %d", metadataBlob.RefCount)
	}

	// Check the content blob ref count
	contentBlob, ok := emptyDirNode.blob.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to cast content blob to LocalCachedFile")
	}
	if contentBlob.RefCount != 1 {
		t.Fatalf("Expected content blob ref count 1, got %d", contentBlob.RefCount)
	}

	// 2. Link blobs and dirs
	dirNames := []string{"someplace", "someplace/else", "dir", "dir/sub"}
	for _, dirName := range dirNames {
		err := ns.Link(dirName, emptyTfa)
		if err != nil {
			t.Fatalf("Failed to mkdir %s: %v", dirName, err)
		}
	}

	ns.DumpFileCache()

	fileNames := []string{"zero", "one", "someplace/else/two", "dir/three", "dir/sub/four", "five"}
	for i, fileName := range fileNames {
		err := ns.linkBlob(fileName, allFiles[i].GetAddress(), allFiles[i].GetSize())
		if err != nil {
			t.Fatalf("Failed to link blob to %s: %v", fileName, err)
		}
	}

	log.Printf("About to check step 2.")
	ns.DebugDumpNamespace()
	ns.DumpFileCache()

	var errors []string

	// Check both content and metadata reference counts
	for i, cf := range allFiles {
		expectedContentRefCount := 1
		if i <= 5 {
			expectedContentRefCount = 2
		}

		// Check content blob reference count
		if cf.RefCount != expectedContentRefCount {
			errors = append(errors, fmt.Sprintf(
				"File %d: Expected content reference count of %d, got %d",
				i, expectedContentRefCount, cf.RefCount))
		}

		// Only files 0-5 should have FileNodes
		fn := ns.lookupNodeByContent(cf.GetAddress())
		if i <= 5 {
			// Should have FileNode with ref count 1
			if fn == nil {
				errors = append(errors, fmt.Sprintf(
					"File %d: Expected FileNode but not found in cache",
					i))
				continue
			}

			bn, ok := fn.(*BlobNode)
			if !ok {
				errors = append(errors, fmt.Sprintf(
					"File %d: FileNode is not a BlobNode",
					i))
				continue
			}

			// Both node and metadata blob should have ref count 1
			if bn.refCount != 1 {
				errors = append(errors, fmt.Sprintf(
					"File %d: Expected node reference count of 1, got %d",
					i, bn.refCount))
			}

			metadataRef := bn.metadataBlob.(*LocalCachedFile)
			if metadataRef.RefCount != 1 {
				errors = append(errors, fmt.Sprintf(
					"File %d: Expected metadata reference count of 1, got %d",
					i, metadataRef.RefCount))
			}
		} else {
			// Files > 5 should not have FileNodes
			if fn != nil {
				errors = append(errors, fmt.Sprintf(
					"File %d: Expected no FileNode but found one in cache",
					i))
			}
		}
	}

	if len(errors) > 0 {
		t.Fatalf("Found %d errors:\n%s", len(errors), strings.Join(errors, "\n"))
	}

	// 3. Unlink one and five
	err = ns.linkBlob(fileNames[1], "", allFiles[1].GetSize())
	if err != nil {
		t.Fatalf("Failed to unlink one: %v", err)
	}

	err = ns.linkBlob(fileNames[5], "", allFiles[5].GetSize())
	if err != nil {
		t.Fatalf("Failed to unlink five: %v", err)
	}

	log.Printf("About to check step 4.")
	ns.DebugDumpNamespace()
	ns.DumpFileCache()

	log.Printf("Cleanup unreferenced nodes.")
	ns.CleanupUnreferencedNodes()
	ns.DebugDumpNamespace()
	ns.DumpFileCache()

	// FIXME -- All this reference count stuff is semi-obselete at this point,
	// now that we're allowing sparse trees again

	// 4. Check reference counts again
	for i, cf := range allFiles {
		var expectedContentRefCount int
		if i > 5 || i == 1 || i == 5 {
			expectedContentRefCount = 1
		} else {
			expectedContentRefCount = 2
		}

		// Check content blob reference
		if cf.RefCount != expectedContentRefCount {
			errors = append(errors, fmt.Sprintf(
				"File %d: After unlinking, expected content reference count of %d, got %d",
				i, expectedContentRefCount, cf.RefCount))
		}

		// Check for FileNode presence/absence
		fn := ns.lookupNodeByContent(cf.GetAddress())

		// Shouldn't have FileNodes for stuff that was never added
		if i >= 6 || expectedContentRefCount == 1 {
			if fn != nil {
				errors = append(errors, fmt.Sprintf(
					"File %d: FileNode in cache, it shouldn't be",
					i))
			}
			continue
		}

		// Should have FileNode
		if fn == nil {
			errors = append(errors, fmt.Sprintf(
				"File %d: FileNode not found in cache",
				i))
			continue
		}

		bn, ok := fn.(*BlobNode)
		if !ok {
			errors = append(errors, fmt.Sprintf(
				"File %d: FileNode is not a BlobNode",
				i))
			continue
		}

		// Check node and metadata reference counts
		if bn.refCount != 1 {
			errors = append(errors, fmt.Sprintf(
				"File %d: After unlinking, expected node reference count of %d, got %d",
				i, 0, bn.refCount))
		}

		metadataRef := bn.metadataBlob.(*LocalCachedFile)
		if metadataRef.RefCount != expectedContentRefCount-1 {
			errors = append(errors, fmt.Sprintf(
				"File %d: After unlinking, expected metadata reference count of %d, got %d",
				i, expectedContentRefCount-1, metadataRef.RefCount))
		}
	}

	if len(errors) > 0 {
		t.Fatalf("Found %d errors:\n%s", len(errors), strings.Join(errors, "\n"))
	}

	// 5. Try unlinking a whole subdirectory
	err = ns.Link("someplace", nil)
	if err != nil {
		t.Fatalf("Failed to unlink someplace: %v", err)
	}

	log.Printf("About to check step 5.")
	ns.DebugDumpNamespace()
	ns.DumpFileCache()

	for i, cf := range allFiles {
		var expectedRefCount int
		if i > 5 || i == 1 || i == 5 {
			expectedRefCount = 1
		} else {
			expectedRefCount = 2
		}
		if cf.RefCount != expectedRefCount {
			t.Fatalf("After unlinking someplace, expected content reference count of %d for %d, got %d", expectedRefCount, i, cf.RefCount)
		}

		fn := ns.lookupNodeByContent(cf.GetAddress())
		if fn != nil {
			bn, ok := fn.(*BlobNode)
			if !ok {
				t.Fatalf("FileNode for %s is not a BlobNode", cf.GetAddress())
				continue
			}
			metadataRef := bn.metadataBlob.(*LocalCachedFile)
			if metadataRef.RefCount != expectedRefCount-1 {
				t.Fatalf("After unlinking someplace, expected metadata reference count of %d for %d, got %d",
					expectedRefCount-1, i, metadataRef.RefCount)
			}
		}
	}

	// 6. Try revision of the whole tree
	log.Printf("--- Starting test 6, tree revision")

	ns.DumpFileCache()

	newRoot := make(map[string]string)
	log.Printf("Old root metadata is: %s", ns.rootAddr)
	newRoot["tree"] = string(ns.rootAddr)

	newRootJson, err := json.Marshal(newRoot)
	if err != nil {
		t.Fatalf("Failed to serialize newRoot to JSON: %v", err)
	}

	rootContent, err := bs.AddDataBlock(newRootJson)
	if err != nil {
		t.Fatalf("Failed to add new root JSON to BlobStore: %v", err)
	}

	tfa := NewTypedFileAddr(rootContent.GetAddress(), rootContent.GetSize(), Tree)

	ns.Link("", tfa)
	rootContent.Release()

	log.Printf("--- Verify tree setup")
	ns.DebugDumpNamespace()

	cf, err := ns.LookupAndOpen("tree/zero")
	if err != nil {
		t.Fatalf("Failed to lookup tree/zero: %v", err)
	}

	if cf.GetAddress() != allFiles[0].GetAddress() {
		t.Fatalf("Expected tree/zero to point to %s, got %s", allFiles[0].GetAddress(), cf.GetAddress())
	}
	cf.Release()

	log.Printf("--- Scaffolding directories")

	err = ns.Link("tree/someplace", emptyTfa)
	if err != nil {
		t.Fatalf("Failed to mkdir %s: %v", "tree/someplace", err)
	}

	err = ns.Link("tree/someplace/else", emptyTfa)
	if err != nil {
		t.Fatalf("Failed to mkdir %s: %v", "tree/someplace/else", err)
	}

	log.Printf("--- Starting revisions")

	for i := 6; i < 10; i++ {
		newRoot := make(map[string]string)
		newRoot["prev"] = string(ns.rootAddr)

		treeNode, err := ns.LookupNode("tree")
		if err != nil {
			t.Fatalf("Failed to lookup tree: %v", err)
		}

		newRoot["tree"] = string(treeNode.MetadataBlob().GetAddress())
		treeNode.Release()

		newRootJson, err = json.Marshal(newRoot)
		if err != nil {
			t.Fatalf("Failed to serialize newRoot to JSON: %v", err)
		}

		log.Printf("We serialize JSON: %s\n", newRootJson)

		rootContent, err := bs.AddDataBlock(newRootJson)
		if err != nil {
			t.Fatalf("Failed to add new root content to BlobStore: %v", err)
		}

		err = ns.linkTree("", rootContent.GetAddress())
		if err != nil {
			rootContent.Release()
			t.Fatalf("Failed to link new root: %v", err)
		}

		rootContent.Release()

		err = ns.linkBlob(fmt.Sprintf("tree/%d", i), allFiles[i].GetAddress(), allFiles[i].GetSize())
		if err != nil {
			t.Fatalf("Failed to link tree/%d: %v", i, err)
		}

		err = ns.linkBlob(fmt.Sprintf("tree/%s", fileNames[i-6]), allFiles[i].GetAddress(), allFiles[i].GetSize())
		if err != nil {
			t.Fatalf("Failed to link tree/%s: %v", fileNames[i-6], err)
		}

		log.Printf("--- Modification %d\n", i)
		ns.DebugDumpNamespace()
	}

	log.Printf("--- Release allFiles")
	for _, cf := range allFiles {
		cf.Release()
	}

	log.Printf("About to check step 6.")
	ns.CleanupUnreferencedNodes()

	// This is in the root pin refCount structure:
	//expectedRefCounts := []int{1, 0, 0, 1, 1, 0, 2, 2, 2, 2}

	// And this is actual in-node refCount:
	expectedRefCounts := []int{1, 0, 0, 1, 1, 0, 1, 1, 1, 1}

	for i, cf := range allFiles {
		fn := ns.lookupNodeByContent(cf.GetAddress())
		if fn == nil && expectedRefCounts[i] == 0 {
			continue
		} else if fn != nil && expectedRefCounts[i] == 0 {
			t.Fatalf("FileNode for %s shouldn't be in cache", cf.GetAddress())
		}

		blobNode, isBlobNode := fn.(*BlobNode)
		if !isBlobNode {
			t.Fatalf("FileNode for %s is not a BlobNode", cf.GetAddress())
			continue
		}

		actualRefCount := blobNode.refCount
		metadataRef := blobNode.metadataBlob.(*LocalCachedFile)

		var fileName string
		if i < 6 {
			fileName = fileNames[i]
		} else {
			fileName = fmt.Sprintf("%d", i)
		}
		log.Printf("File %d - %s: expected %d, got node:%d content:%d metadata:%d (hash is %s)",
			i, fileName, expectedRefCounts[i], actualRefCount, cf.RefCount, metadataRef.RefCount, cf.GetAddress())

		if actualRefCount != expectedRefCounts[i] {
			t.Fatalf("After big revision setup, expected reference count of %d for file %d, got %d",
				expectedRefCounts[i], i, actualRefCount)
		}

		// Content and metadata blobs should have same reference count
		if cf.RefCount != metadataRef.RefCount {
			t.Fatalf("After big revision setup, content ref count %d doesn't match metadata ref count %d for file %d",
				cf.RefCount, metadataRef.RefCount, i)
		}
	}

	// 7. Try unlinking the whole tree and make sure things get cleaned up
	log.Printf("--- Starting test 7, full unlink")
	ns.DebugDumpNamespace()

	err = ns.Link("", nil)
	if err != nil {
		t.Fatalf("Failed to unlink root: %v", err)
	}
	ns.CleanupUnreferencedNodes()

	log.Printf("About to check step 7.")

	for i, cf := range allFiles {
		//var expectedRefCount int
		//if i <= 5 && i != 1 && i != 5 {
		//	expectedRefCount := 1
		//} else {
		expectedRefCount := 0
		//}

		if cf.RefCount != expectedRefCount {
			t.Fatalf("Expected ending content reference count of %d for %d, got %d",
				expectedRefCount, i, cf.RefCount)
		}

		fn := ns.lookupNodeByContent(cf.GetAddress())
		if fn != nil {
			bn, ok := fn.(*BlobNode)
			if !ok {
				t.Fatalf("FileNode for %s is not a BlobNode", cf.GetAddress())
				continue
			}
			metadataRef := bn.metadataBlob.(*LocalCachedFile)
			if metadataRef.RefCount != expectedRefCount {
				t.Fatalf("Expected ending metadata reference count of %d for %d, got %d", expectedRefCount, i, metadataRef.RefCount)
			}
		}
	}
}

// TestLookupMultiplePaths tests the NameStore's ability to look up multiple paths in one batch,
// including handling cases where some paths don't exist.
func TestLookupMultiplePaths(t *testing.T) {
	nameStore, blobStore, cleanup := setupNameStoreTestEnv(t)
	defer cleanup()

	// Create some test content
	testData := []struct {
		path    string
		content string
	}{
		{"file1.txt", "Content of file 1"},
		{"dir/file2.txt", "Content of file 2"},
		{"dir/subdir/file3.txt", "Content of file 3"},
		{"dir/subdir/file4.txt", "Content of file 4"},
	}

	// Setup directory structure first
	emptyDirInterface, err := blobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		t.Fatalf("Failed to add empty dir blob: %v", err)
	}
	emptyDir, ok := emptyDirInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}
	defer emptyDir.Release()

	// Create necessary directories
	err = nameStore.linkTree("dir", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	err = nameStore.linkTree("dir/subdir", emptyDir.GetAddress())
	if err != nil {
		t.Fatalf("Failed to create dir/subdir: %v", err)
	}

	// Add files
	for _, item := range testData {
		content := []byte(item.content)
		blobInterface, err := blobStore.AddDataBlock(content)
		if err != nil {
			t.Fatalf("Failed to add content for %s: %v", item.path, err)
		}
		blob, ok := blobInterface.(*LocalCachedFile)
		if !ok {
			t.Fatalf("Failed to assert type *LocalCachedFile for %s", item.path)
		}
		defer blob.Release()

		err = nameStore.linkBlob(item.path, blob.GetAddress(), blob.GetSize())
		if err != nil {
			t.Fatalf("Failed to link %s: %v", item.path, err)
		}
	}

	// Test batch lookup with multiple paths - mix of existing and non-existing
	paths := []string{
		"file1.txt",                // Exists
		"dir/file2.txt",            // Exists
		"nonexistent.txt",          // Doesn't exist
		"dir/subdir/file3.txt",     // Exists
		"dir/subdir/nonexistent",   // Doesn't exist
		"dir/nonexistent/file.txt", // Middle part doesn't exist
	}

	// Call LookupFull
	pathNodePairs, wasPartialFailure, err := nameStore.LookupFull(paths)
	if err != nil {
		t.Fatalf("Failed to lookup paths: %v", err)
	}

	// We expect some paths to fail, so wasPartialFailure should be true
	if !wasPartialFailure {
		t.Error("Expected partial failure flag to be true, but it was false")
	}

	// Build a map of path -> node for easier verification
	resultsByPath := make(map[string]FileNode)
	for _, pair := range pathNodePairs {
		resultsByPath[pair.Path] = pair.Node
	}

	// 1. Verify we got results for existing paths
	for _, path := range []string{"file1.txt", "dir/file2.txt", "dir/subdir/file3.txt"} {
		node, ok := resultsByPath[path]
		if !ok {
			t.Errorf("Expected to find result for %s, but it was missing", path)
			continue
		}

		// Verify content matches what we expect
		contentReader, err := node.ExportedBlob().Reader()
		if err != nil {
			t.Errorf("Failed to get reader for %s: %v", path, err)
			continue
		}

		contentBytes, err := io.ReadAll(contentReader)
		contentReader.Close()
		if err != nil {
			t.Errorf("Failed to read content for %s: %v", path, err)
			continue
		}

		// Find the expected content for this path
		var expectedContent string
		for _, item := range testData {
			if item.path == path {
				expectedContent = item.content
				break
			}
		}

		if string(contentBytes) != expectedContent {
			t.Errorf("Content mismatch for %s: got %s, expected %s",
				path, string(contentBytes), expectedContent)
		}
	}

	// 2. Check we didn't get results for non-existing paths
	for _, path := range []string{"nonexistent.txt", "dir/subdir/nonexistent", "dir/nonexistent/file.txt"} {
		if _, ok := resultsByPath[path]; ok {
			t.Errorf("Got unexpected result for non-existent path %s", path)
		}
	}

	// 3. For paths with non-existent intermediate components, check we got the existing parts
	if _, ok := resultsByPath["dir"]; !ok {
		t.Errorf("Expected to find result for 'dir' as partial path, but it was missing")
	}

	// Clean up - ensure all references are released
	for _, pair := range pathNodePairs {
		pair.Node.Release()
	}
}
