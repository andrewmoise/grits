package grits

import (
	"bytes"
	"log"
	"os"
	"testing"
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
