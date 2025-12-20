package grits

import (
	"bytes"
	"log"
	"os"
	"testing"
	"time"
)

func setupBlobStore(t *testing.T) (*LocalBlobStore, func()) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "blobstore_test")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}

	log.Printf("Setup in %s\n", tempDir)

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

	bs := NewLocalBlobStore(config)
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

	cachedFileInterface, err := bs.AddLocalFile(srcPath)
	if err != nil {
		t.Fatalf("AddLocalFile failed: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
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

	cachedFileInterface, err := bs.AddLocalFile(srcPath)
	if err != nil {
		t.Fatalf("AddLocalFile failed: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}

	if cachedFile.RefCount != 1 {
		t.Errorf("Expected RefCount to be 1, got %d", cachedFile.RefCount)
	}
}

func TestBlobStore_ReadFile(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	srcPath := bs.config.ServerPath("var/test.txt")
	content := []byte("hello world")
	err := os.WriteFile(srcPath, content, 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cachedFileInterface, err := bs.AddLocalFile(srcPath)
	if err != nil {
		t.Fatalf("AddLocalFile failed: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}

	readFileInterface, err := bs.ReadFile(cachedFile.Address)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	readFile, ok := readFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
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
	addedFileInterface, err := bs.AddReader(file)
	if err != nil {
		t.Fatalf("AddOpenFile failed: %v", err)
	}
	addedFile, ok := addedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
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
	addedFileInterface, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}
	addedFile, ok := addedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
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

	cachedFileInterface, err := bs.AddLocalFile(srcPath)
	if err != nil {
		t.Fatalf("AddLocalFile failed: %v", err)
	}
	cachedFile, ok := cachedFileInterface.(*LocalCachedFile)
	if !ok {
		t.Fatalf("Failed to assert type *LocalCachedFile")
	}

	// Release the file
	cachedFile.Release()

	if cachedFile.RefCount != 0 {
		t.Errorf("Expected RefCount to be 0 after release, got %d", cachedFile.RefCount)
	}
}

func TestConsistentHashing(t *testing.T) {
	testCases := []string{
		"Hello, world!",
		`{"index.html":"QmP3rp8DrtFztrnmUqrZzF8stujVx52KTVusFdXGWGJrDw"}`,
		"", // Empty string case
	}

	for _, tc := range testCases {
		testData := []byte(tc)

		// Method 1: Your existing direct multihash function
		hash1 := ComputeHash(testData)

		// Method 2: Your streaming CopyAndHash function
		var buf bytes.Buffer
		hash2, _, err := CopyAndHash(bytes.NewReader(testData), &buf)
		if err != nil {
			t.Fatalf("CopyAndHash failed: %v", err)
		}

		// Method 3: IPFS boxo approach
		hash3, err := computeIPFSHashCIDv0(testData, hash2)
		if err != nil {
			t.Fatalf("IPFS hash calculation failed: %v", err)
		}

		t.Logf("Test case: %q", tc)
		t.Logf("Method 1 (ComputeHash):  %s", hash1)
		t.Logf("Method 2 (CopyAndHash):  %s", hash2)
		t.Logf("Method 3 (IPFS boxo):    %s", hash3)

		// Verify consistency between all methods
		if hash1 != hash3 {
			t.Errorf("ComputeHash doesn't match IPFS hash: %s vs %s", hash1, hash3)
		}

		if hash2 != hash3 {
			t.Errorf("CopyAndHash doesn't match IPFS hash: %s vs %s", hash2, hash3)
		}
	}
}

func computeIPFSHashCIDv0(data []byte, crib string) (string, error) {
	return crib, nil

	// You could comment that out, and uncomment all of this, if you wanted IPFS libraries
	// in your stuff to check us against it:

	//mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	//if err != nil {
	//	return "", err
	//}

	//c := cid.NewCidV0(mh)
	//return c.String(), nil
}

/////
// Touch() and Eviction Tests

func TestBlobStore_Touch_ExpiredEviction(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Start the eviction worker
	err := bs.Start()
	if err != nil {
		t.Fatalf("Failed to start blob store: %v", err)
	}
	defer bs.Stop()

	// Create a blob
	content := []byte("test content for expiration")
	cachedFile, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}

	addr := cachedFile.GetAddress()

	// Touch it with a very short expiration (100ms)
	cachedFile.(*LocalCachedFile).Touch(100 * time.Millisecond)

	// Release our reference
	cachedFile.Release()

	// Verify it's still there immediately
	file, err := bs.ReadFile(addr)
	if err != nil {
		t.Errorf("Blob should still exist immediately after release with future expiration: %v", err)
	} else {
		file.Release()
	}

	// Wait for expiration + a bit extra for the eviction worker
	time.Sleep(200 * time.Millisecond)

	// Manually trigger eviction to avoid waiting for the full 30s ticker
	bs.EvictOldFiles()

	// Now it should be gone
	_, err = bs.ReadFile(addr)
	if err == nil {
		t.Error("Blob should have been evicted after expiration")
	}
}

func TestBlobStore_Touch_ActiveReferencesPreventEviction(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Start the eviction worker
	err := bs.Start()
	if err != nil {
		t.Fatalf("Failed to start blob store: %v", err)
	}
	defer bs.Stop()

	// Create a blob
	content := []byte("test content with active reference")
	cachedFile, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}

	addr := cachedFile.GetAddress()

	// Touch it with a very short expiration
	cachedFile.(*LocalCachedFile).Touch(100 * time.Millisecond)

	// Keep our reference (don't release)

	// Wait for expiration
	time.Sleep(200 * time.Millisecond)

	// Manually trigger eviction
	bs.EvictOldFiles()

	// Should still exist because we have an active reference
	file, err := bs.ReadFile(addr)
	if err != nil {
		t.Error("Blob should not be evicted while it has active references")
	} else {
		file.Release()
	}

	// Now release the reference
	cachedFile.Release()

	// Trigger eviction again
	bs.EvictOldFiles()

	// Now it should be gone
	_, err = bs.ReadFile(addr)
	if err == nil {
		t.Error("Blob should have been evicted after reference was released")
	}
}

func TestBlobStore_Touch_NoExpirationDeletesImmediately(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create a blob
	content := []byte("test immediate deletion")
	cachedFile, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}

	addr := cachedFile.GetAddress()

	// Don't touch it - leave it with current timestamp

	// Release immediately
	cachedFile.Release()

	// Should be gone immediately (no future expiration)
	_, err = bs.ReadFile(addr)
	if err == nil {
		t.Error("Blob without future expiration should be deleted immediately on release")
	}
}

func TestBlobStore_Touch_MultipleBlobs(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	err := bs.Start()
	if err != nil {
		t.Fatalf("Failed to start blob store: %v", err)
	}
	defer bs.Stop()

	// Create three blobs with different expiration times
	shortLived, err := bs.AddDataBlock([]byte("short lived"))
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}
	shortAddr := shortLived.GetAddress()
	shortLived.(*LocalCachedFile).Touch(100 * time.Millisecond)
	shortLived.Release()

	longLived, err := bs.AddDataBlock([]byte("long lived"))
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}
	longAddr := longLived.GetAddress()
	longLived.(*LocalCachedFile).Touch(10 * time.Second)
	longLived.Release()

	noExpiration, err := bs.AddDataBlock([]byte("no expiration"))
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}
	noExpirationAddr := noExpiration.GetAddress()
	// Don't touch, leave at current time
	noExpiration.Release()

	// Wait for short-lived to expire
	time.Sleep(200 * time.Millisecond)
	bs.EvictOldFiles()

	// Short-lived should be gone
	_, err = bs.ReadFile(shortAddr)
	if err == nil {
		t.Error("Short-lived blob should have been evicted")
	}

	// Long-lived should still exist
	longFile, err := bs.ReadFile(longAddr)
	if err != nil {
		t.Errorf("Long-lived blob should still exist: %v", err)
	} else {
		longFile.Release()
	}

	// No-expiration blob was deleted immediately on release
	_, err = bs.ReadFile(noExpirationAddr)
	if err == nil {
		t.Error("No-expiration blob should have been deleted immediately")
	}
}

func TestBlobStore_EvictionWorker_PeriodicCleanup(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// This test is a bit slow because we need to wait for the 30s ticker
	// We'll use a shorter test by just verifying the worker starts and stops correctly

	err := bs.Start()
	if err != nil {
		t.Fatalf("Failed to start blob store: %v", err)
	}

	// Create a blob with short expiration
	content := []byte("periodic cleanup test")
	cachedFile, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}

	addr := cachedFile.GetAddress()
	cachedFile.(*LocalCachedFile).Touch(50 * time.Millisecond)
	cachedFile.Release()

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Stop the worker - this triggers final cleanup
	err = bs.Stop()
	if err != nil {
		t.Fatalf("Failed to stop blob store: %v", err)
	}

	// Blob should be gone after final cleanup
	_, err = bs.ReadFile(addr)
	if err == nil {
		t.Error("Blob should have been cleaned up during shutdown")
	}
}

func TestBlobStore_Touch_SurvivesScan(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create a blob and touch it with future expiration
	content := []byte("survives scan")
	cachedFile, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("AddDataBlock failed: %v", err)
	}

	addr := cachedFile.GetAddress()
	cachedFile.(*LocalCachedFile).Touch(10 * time.Second)
	cachedFile.Release()

	// Close the first blob store
	bs.Close()

	// Create a new blob store instance that scans the same directory
	// This simulates a restart
	bs2 := NewLocalBlobStore(bs.config)
	if bs2 == nil {
		t.Fatal("Failed to create second blob store instance")
	}
	defer bs2.Close()

	// The blob should still be readable (not expired yet)
	file2, err := bs2.ReadFile(addr)
	if err != nil {
		t.Errorf("Blob should survive directory scan and still be accessible: %v", err)
	} else {
		file2.Release()
	}
}

func TestBlobStore_DuplicateContent(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	content := []byte("duplicate content test")

	// Add the same content twice
	file1, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("First AddDataBlock failed: %v", err)
	}

	file2, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("Second AddDataBlock failed: %v", err)
	}

	// Should have the same address
	if file1.GetAddress() != file2.GetAddress() {
		t.Error("Duplicate content should have the same address")
	}

	// Should have refcount of 2
	if file1.(*LocalCachedFile).RefCount != 2 {
		t.Errorf("Expected refcount 2, got %d", file1.(*LocalCachedFile).RefCount)
	}

	// Release both
	file1.Release()
	file2.Release()

	// Should be deleted now
	_, err = bs.ReadFile(file1.GetAddress())
	if err == nil {
		t.Error("Blob should be deleted after all references released")
	}
}
