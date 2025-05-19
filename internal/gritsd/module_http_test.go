package gritsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"grits/internal/grits"
)

// setupEmptyDir prepares an empty directory node on the target server
// and returns its metadata address for linking operations
func setupEmptyDir(volume Volume, remoteUrl string) (*grits.BlobAddr, error) {
	// Get the empty directory metadata address from the volume
	emptyDir, err := volume.CreateTreeNode()
	if err != nil {
		return nil, err
	}
	defer emptyDir.Release()

	// Upload the content blob
	contentBlob := emptyDir.ExportedBlob()
	contentReader, err := contentBlob.Reader()
	if err != nil {
		return nil, fmt.Errorf("couldn't create reader for empty dir content blob: %v", err)
	}
	defer contentReader.Close()

	contentUploadResp, err := http.Post(remoteUrl+"/upload", "application/octet-stream", contentReader)
	if err != nil {
		return nil, fmt.Errorf("failed to upload empty dir content blob: %v", err)
	}
	defer contentUploadResp.Body.Close()

	if contentUploadResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload of empty dir content blob failed with status: %d", contentUploadResp.StatusCode)
	}

	// Verify the hash matches
	var uploadedContentHash string
	if err := json.NewDecoder(contentUploadResp.Body).Decode(&uploadedContentHash); err != nil {
		return nil, fmt.Errorf("failed to decode content blob upload response: %v", err)
	}

	if uploadedContentHash != contentBlob.GetAddress().Hash {
		return nil, fmt.Errorf("content blob hash mismatch. Expected: %s, Got: %s",
			contentBlob.GetAddress().Hash, uploadedContentHash)
	}

	// Upload the metadata blob
	metadataBlob := emptyDir.MetadataBlob()
	metadataReader, err := metadataBlob.Reader()
	if err != nil {
		return nil, fmt.Errorf("couldn't create reader for empty dir metadata blob: %v", err)
	}
	defer metadataReader.Close()

	metadataUploadResp, err := http.Post(remoteUrl+"/upload", "application/octet-stream", metadataReader)
	if err != nil {
		return nil, fmt.Errorf("failed to upload empty dir metadata blob: %v", err)
	}
	defer metadataUploadResp.Body.Close()

	if metadataUploadResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload of empty dir metadata blob failed with status: %d", metadataUploadResp.StatusCode)
	}

	var uploadedMetadataHash string
	if err := json.NewDecoder(metadataUploadResp.Body).Decode(&uploadedMetadataHash); err != nil {
		return nil, fmt.Errorf("failed to decode metadata blob upload response: %v", err)
	}

	// Verify the hash matches
	if uploadedMetadataHash != metadataBlob.GetAddress().Hash {
		return nil, fmt.Errorf("metadata blob hash mismatch. Expected: %s, Got: %s",
			metadataBlob.GetAddress().Hash, uploadedMetadataHash)
	}

	// Return the metadata address to use in link operations
	return &grits.BlobAddr{Hash: uploadedMetadataHash}, nil
}

// Main API endpoints

func TestLookupAndLinkEndpoints(t *testing.T) {
	url := "http://localhost:1887/grits/v1"
	server, cleanup := SetupTestServer(t, WithHttpModule(1887), WithLocalVolume("root"))
	defer cleanup()

	server.Start()
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	emptyDirAddr, err := setupEmptyDir(server.FindVolumeByName("root"), url)
	if err != nil {
		t.Fatalf("Couldn't make empty dir: %v", err)
	}

	// Set up directory structure
	linkData := []LinkData{
		{Path: "dir", MetadataAddr: emptyDirAddr.Hash},
		{Path: "dir/subdir", MetadataAddr: emptyDirAddr.Hash},
	}

	linkPayload, _ := json.Marshal(linkData)
	resp, err := http.Post(url+"/link/root", "application/json", bytes.NewBuffer(linkPayload))
	if err != nil || resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyString := string(bodyBytes)

		t.Fatalf("Failed to setup dir structure: %v %d %s", err, resp.StatusCode, bodyString)
	}
	resp.Body.Close()

	// Upload blobs and link them
	blobContents := []string{"one", "two", "three", "four", "five"}
	addresses := make([]string, len(blobContents))

	for i, content := range blobContents {
		// Upload blob
		resp, err := http.Post(url+"/upload", "text/plain", bytes.NewBufferString(content))
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to upload blob '%s': %v %d", content, err, resp.StatusCode)
		}

		addressJson, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if err := json.Unmarshal(addressJson, &addresses[i]); err != nil {
			t.Fatalf("Failed to unmarshal address: %v", err)
		}
		resp.Body.Close()

		// Create metadata for the blob
		// We can use the local volume to create and upload the metadata node
		volume := server.FindVolumeByName("root")

		contentCf, err := server.BlobStore.AddDataBlock([]byte(content))
		if err != nil {
			t.Fatalf("Couldn't add %s to blob store: %v", content, err)
		}
		defer contentCf.Release()

		metadataBlob, err := CreateAndUploadMetadata(volume, contentCf, url)
		if err != nil {
			t.Fatalf("Couldn't upload metadata: %v", err)
		}

		// Link the blob to two paths using metadata address
		linkData := []LinkData{
			{Path: content, MetadataAddr: metadataBlob.Hash},
			{Path: "dir/subdir/" + content, MetadataAddr: metadataBlob.Hash},
		}

		linkPayload, _ := json.Marshal(linkData)
		resp, err = http.Post(url+"/link/root", "application/json", bytes.NewBuffer(linkPayload))
		if err != nil || resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			bodyString := string(bodyBytes)

			t.Fatalf("Failed to link blob '%s': %v %d %s", content, err, resp.StatusCode, bodyString)
		}
		resp.Body.Close()
	}

	// Perform a lookup on "dir/subdir/one"
	lookupPayload, _ := json.Marshal("dir/subdir/one")
	resp, err = http.Post(url+"/lookup/root", "application/json", bytes.NewBuffer(lookupPayload))
	if err != nil {
		t.Fatalf("Failed to perform lookup: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Lookup failed with status code %d", resp.StatusCode)
	}

	// Updated for new response format: [path, metadataHash, contentHash, contentSize]
	var lookupResponse [][]any
	if err := json.NewDecoder(resp.Body).Decode(&lookupResponse); err != nil {
		t.Fatalf("Failed to decode lookup response: %v", err)
	}

	// Verify response structure and content
	if len(lookupResponse) < 3 {
		t.Fatalf("Expected at least 3 path entries in the lookup response, got %d", len(lookupResponse))
	}

	// Check root path
	if lookupResponse[0][0] != "" { // First entry should be root with empty path
		t.Errorf("Expected root path to be empty string, got %v", lookupResponse[0][0])
	}

	// Check expected paths are in the response
	expectedPaths := []string{"", "dir", "dir/subdir", "dir/subdir/one"}
	for i, expectedPath := range expectedPaths {
		if i >= len(lookupResponse) {
			t.Errorf("Missing expected path in response: %s", expectedPath)
			continue
		}

		// Check path
		path, ok := lookupResponse[i][0].(string)
		if !ok {
			t.Errorf("Path at index %d is not a string: %v", i, lookupResponse[i][0])
			continue
		}
		if path != expectedPath {
			t.Errorf("Expected path %s at index %d, got %s", expectedPath, i, path)
		}

		// Verify metadata hash exists
		if _, ok := lookupResponse[i][1].(string); !ok {
			t.Errorf("Metadata hash at index %d is not a string: %v", i, lookupResponse[i][1])
		}

		// Verify content hash exists
		if _, ok := lookupResponse[i][2].(string); !ok {
			t.Errorf("Content hash at index %d is not a string: %v", i, lookupResponse[i][2])
		}

		// Verify content size is a number
		if _, ok := lookupResponse[i][3].(float64); !ok { // JSON numbers deserialize as float64
			t.Errorf("Content size at index %d is not a number: %v", i, lookupResponse[i][3])
		}
	}

	// For the final path (dir/subdir/one), verify the content size matches what we expect
	if len(lookupResponse) >= len(expectedPaths) {
		lastIndex := len(expectedPaths) - 1
		contentSize, ok := lookupResponse[lastIndex][3].(float64)
		if !ok {
			t.Errorf("Content size is not a number: %v", lookupResponse[lastIndex][3])
		} else if int(contentSize) != len("one") {
			t.Errorf("Expected content size %d for 'one', got %f", len("one"), contentSize)
		}
	}
}

func TestLinkReturnsPathMetadata(t *testing.T) {
	url := "http://localhost:1888/grits/v1"
	server, cleanup := SetupTestServer(t, WithHttpModule(1888), WithLocalVolume("root"))
	defer cleanup()

	server.Start()
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	// Test linking a blob and verify we get path metadata in response
	testContent := "Test content"

	// First upload a blob
	uploadResp, err := http.Post(url+"/upload", "text/plain",
		bytes.NewBufferString(testContent))
	if err != nil || uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to upload blob: %v %d", err, uploadResp.StatusCode)
	}

	var blobAddr string
	if err := json.NewDecoder(uploadResp.Body).Decode(&blobAddr); err != nil {
		t.Fatalf("Failed to decode upload response: %v", err)
	}
	uploadResp.Body.Close()

	// Upload an empty directory to put it in
	emptyDirAddr, err := setupEmptyDir(server.FindVolumeByName("root"), url)
	if err != nil {
		t.Fatalf("Couldn't make empty directory: %v", err)
	}

	// Create metadata for the blob content
	contentCf, err := server.BlobStore.AddDataBlock([]byte(testContent))
	if err != nil {
		t.Fatalf("Couldn't add test content to blob store: %v", err)
	}
	defer contentCf.Release()

	blobMetadataAddr, err := CreateAndUploadMetadata(server.FindVolumeByName("root"), contentCf, url)
	if err != nil {
		t.Fatalf("Couldn't create and upload metadata: %v", err)
	}

	linkPaths := []string{"", "test", "test/nested", "test/nested/path.txt"}
	linkData := []LinkData{
		{Path: linkPaths[1], MetadataAddr: emptyDirAddr.Hash},
		{Path: linkPaths[2], MetadataAddr: emptyDirAddr.Hash},
		{Path: linkPaths[3], MetadataAddr: blobMetadataAddr.Hash},
	}

	linkPayload, _ := json.Marshal(linkData)
	linkResp, err := http.Post(url+"/link/root", "application/json",
		bytes.NewBuffer(linkPayload))
	if err != nil {
		t.Fatalf("Failed to perform link: %v", err)
	}
	defer linkResp.Body.Close()

	if linkResp.StatusCode != http.StatusOK && linkResp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Link failed with status code %d", linkResp.StatusCode)
	}

	// Decode the response
	var linkResponse [][]any
	if err := json.NewDecoder(linkResp.Body).Decode(&linkResponse); err != nil {
		t.Fatalf("Failed to decode link response: %v", err)
	}

	// Verify response structure
	// We should get entries for "", "test", "test/nested", and "test/nested/path.txt"

	if len(linkResponse) < len(linkPaths) {
		t.Fatalf("Expected at least %d path entries in the link response, got %d",
			len(linkPaths), len(linkResponse))
	}

	// Build a map of paths in the response for easier verification
	responsePaths := make(map[string]bool)
	for _, entry := range linkResponse {
		path, ok := entry[0].(string)
		if !ok {
			t.Errorf("Path is not a string: %v", entry[0])
			continue
		}
		responsePaths[path] = true
	}

	// Check that all expected paths are present
	for _, path := range linkPaths {
		if !responsePaths[path] {
			t.Errorf("Expected path %s in response, but it was not found", path)
		}
	}

	// Find the entry for the linked file and verify it
	for i, entry := range linkResponse {
		path, ok := entry[0].(string)
		if !ok || path != linkPaths[i] {
			continue
		}

		// Verify we have content size, metadata, and content hash at least
		_, ok = entry[3].(float64)
		if !ok {
			t.Errorf("Content size is not a number: %v", entry[3])
		}

		//
		if _, ok := entry[1].(string); !ok {
			t.Errorf("Metadata hash is not a string: %v", entry[1])
		}
		if _, ok := entry[2].(string); !ok {
			t.Errorf("Content hash is not a string: %v", entry[2])
		}
	}
}

func TestUploadAndDownloadBlob(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "grits_server")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := grits.NewConfig(tempDir)
	srv, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	readOnly := false
	httpConfig := &HTTPModuleConfig{
		ThisHost: "localhost",
		ThisPort: 2287, // Just for setup, actual port not used with httptest
		ReadOnly: &readOnly,
	}
	url := "http://" + httpConfig.ThisHost + ":" + fmt.Sprintf("%d", httpConfig.ThisPort)

	httpModule, err := NewHTTPModule(srv, httpConfig)
	if err != nil {
		t.Fatalf("can't create http module: %v", err)
	}
	srv.AddModule(httpModule)

	srv.Start()
	defer srv.Stop()

	time.Sleep(1 * time.Second)

	// Test upload
	testBlobContent := "Test blob content"
	uploadResp, err := http.Post(url+"/grits/v1/upload", "text/plain", bytes.NewBufferString(testBlobContent))
	if err != nil {
		t.Fatalf("Failed to upload blob: %v", err)
	}
	defer uploadResp.Body.Close()

	// Read the response body
	uploadBody, _ := io.ReadAll(uploadResp.Body)
	t.Logf("Upload response: %s", string(uploadBody))

	if uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to upload blob: code %d, body: %s", uploadResp.StatusCode, string(uploadBody))
	}

	// Decode the JSON string
	var blobAddress string
	if err := json.Unmarshal(uploadBody, &blobAddress); err != nil {
		t.Fatalf("Failed to decode upload response: %v", err)
	}

	// Test download using the received blob address
	downloadURL := url + "/grits/v1/blob/" + blobAddress
	downloadResp, err := http.Get(downloadURL)
	log.Printf("Download %s", downloadURL)
	if err != nil || downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to download blob: %v; HTTP status code: %d", err, downloadResp.StatusCode)
	}
	defer downloadResp.Body.Close()

	downloadedContent, err := io.ReadAll(downloadResp.Body)
	if err != nil {
		t.Fatalf("Failed to read download response body: %v", err)
	}

	if string(downloadedContent) != testBlobContent {
		t.Errorf("Downloaded content does not match uploaded content. Got %s, want %s", string(downloadedContent), testBlobContent)
	}
}
