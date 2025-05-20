package gritsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"testing"
	"time"

	"grits/internal/grits"
)

func setupEmptyDir(volume Volume, remoteUrl string) (grits.BlobAddr, error) {
	// Get the empty directory metadata address from the volume
	emptyDir, err := volume.CreateTreeNode()
	if err != nil {
		return "", err
	}
	defer emptyDir.Release()

	// Upload the content blob
	contentBlob := emptyDir.ExportedBlob()
	contentReader, err := contentBlob.Reader()
	if err != nil {
		return "", fmt.Errorf("couldn't create reader for empty dir content blob: %v", err)
	}
	defer contentReader.Close()

	// Create PUT request for content blob
	contentReq, err := http.NewRequest(http.MethodPut, remoteUrl+"/blob/", contentReader)
	if err != nil {
		return "", fmt.Errorf("failed to create request for content upload: %v", err)
	}
	contentReq.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{}
	contentUploadResp, err := client.Do(contentReq)
	if err != nil {
		return "", fmt.Errorf("failed to upload empty dir content blob: %v", err)
	}
	defer contentUploadResp.Body.Close()

	if contentUploadResp.StatusCode != http.StatusOK && contentUploadResp.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("upload of empty dir content blob failed with status: %d", contentUploadResp.StatusCode)
	}

	var uploadedContentHash grits.BlobAddr

	// Only decode response if it's 200 OK (not 204 No Content)
	if contentUploadResp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(contentUploadResp.Body).Decode(&uploadedContentHash); err != nil {
			return "", fmt.Errorf("failed to decode content blob upload response: %v", err)
		}

		if uploadedContentHash != contentBlob.GetAddress() {
			return "", fmt.Errorf("content blob hash mismatch. Expected: %s, Got: %s",
				contentBlob.GetAddress(), uploadedContentHash)
		}
	} else {
		// For 204, just use the hash we already know
		uploadedContentHash = contentBlob.GetAddress()
	}

	// Upload the metadata blob using PUT
	metadataBlob := emptyDir.MetadataBlob()
	metadataReader, err := metadataBlob.Reader()
	if err != nil {
		return "", fmt.Errorf("couldn't create reader for empty dir metadata blob: %v", err)
	}
	defer metadataReader.Close()

	// Create PUT request for metadata blob
	metadataReq, err := http.NewRequest(http.MethodPut, remoteUrl+"/blob/", metadataReader)
	if err != nil {
		return "", fmt.Errorf("failed to create request for metadata upload: %v", err)
	}
	metadataReq.Header.Set("Content-Type", "application/octet-stream")

	metadataUploadResp, err := client.Do(metadataReq)
	if err != nil {
		return "", fmt.Errorf("failed to upload empty dir metadata blob: %v", err)
	}
	defer metadataUploadResp.Body.Close()

	if metadataUploadResp.StatusCode != http.StatusOK && metadataUploadResp.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("upload of empty dir metadata blob failed with status: %d", metadataUploadResp.StatusCode)
	}

	var uploadedMetadataHash grits.BlobAddr

	// Only decode response if it's 200 OK (not 204 No Content)
	if metadataUploadResp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(metadataUploadResp.Body).Decode(&uploadedMetadataHash); err != nil {
			return "", fmt.Errorf("failed to decode metadata blob upload response: %v", err)
		}

		// Verify the hash matches
		if uploadedMetadataHash != metadataBlob.GetAddress() {
			return "", fmt.Errorf("metadata blob hash mismatch. Expected: %s, Got: %s",
				metadataBlob.GetAddress(), uploadedMetadataHash)
		}
	} else {
		// For 204, just use the hash we already know
		uploadedMetadataHash = metadataBlob.GetAddress()
	}

	// Return the metadata address to use in link operations
	return uploadedMetadataHash, nil
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
	linkData := []grits.LinkRequest{
		{Path: "dir", NewAddr: emptyDirAddr},
		{Path: "dir/subdir", NewAddr: emptyDirAddr},
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
		resp, body, err := makeRequest(url, http.MethodPut, "/blob", []byte(content))
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to upload blob '%s': %v %d", content, err, resp.StatusCode)
		}

		if err := json.Unmarshal(body, &addresses[i]); err != nil {
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
		linkData := []grits.LinkRequest{
			{Path: content, NewAddr: metadataBlob},
			{Path: "dir/subdir/" + content, NewAddr: metadataBlob},
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
	uploadResp, body, err := makeRequest(url, http.MethodPut, "/blob", []byte(testContent))
	if err != nil || uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to upload blob: %v %d", err, uploadResp.StatusCode)
	}

	var blobAddr string
	if err := json.Unmarshal(body, &blobAddr); err != nil {
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
	linkData := []grits.LinkRequest{
		{Path: linkPaths[1], NewAddr: emptyDirAddr},
		{Path: linkPaths[2], NewAddr: emptyDirAddr},
		{Path: linkPaths[3], NewAddr: blobMetadataAddr},
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

// Helper function to make HTTP requests
func makeRequest(baseUrl, method, path string, body []byte) (*http.Response, []byte, error) {
	log.Printf("Make request: %s %s via %s", baseUrl, path, method)
	req, err := http.NewRequest(method, baseUrl+path, bytes.NewBuffer(body))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %v", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return resp, respBody, nil
}

func TestBlobEndpoint(t *testing.T) {
	// Use the provided helper to set up a test server with HTTP module and local volume
	server, cleanup := SetupTestServer(t,
		WithHttpModule(2288),
		WithLocalVolume("root"))
	defer cleanup()

	// Start the server and defer stopping it
	server.Start()
	defer server.Stop()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Base URL for our API requests
	url := "http://localhost:2288"

	// Test cases
	t.Run("Upload without specifying hash", func(t *testing.T) {
		content := []byte("Test content without hash")
		resp, body, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/", content)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", resp.StatusCode, body)
		}

		var hash string
		if err := json.Unmarshal(body, &hash); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if hash == "" {
			t.Errorf("Expected non-empty hash in response")
		}

		// Verify content is retrievable
		getResp, getBody, err := makeRequest(url, http.MethodGet, "/grits/v1/blob/"+hash, nil)
		if err != nil {
			t.Fatalf("Get request failed: %v", err)
		}

		if getResp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for GET, got %d", getResp.StatusCode)
		}

		if !bytes.Equal(getBody, content) {
			t.Errorf("Retrieved content doesn't match original. Got %s, want %s",
				string(getBody), string(content))
		}
	})

	t.Run("Upload with specified hash", func(t *testing.T) {
		// First upload to get a valid hash
		content := []byte("Test content with hash")
		_, body, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/", content)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		var hash string
		if err := json.Unmarshal(body, &hash); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Now upload again with the hash specified
		resp2, body2, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/"+hash, content)
		if err != nil {
			t.Fatalf("Request with hash failed: %v", err)
		}

		// Should get status no content since we already have it
		if resp2.StatusCode != http.StatusNoContent {
			t.Errorf("Expected status 204, got %d: %s", resp2.StatusCode, body2)
		}
	})

	t.Run("Upload existing content with hash", func(t *testing.T) {
		// Upload content first
		content := []byte("Test duplicate content")
		_, body, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/", content)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		var hash string
		if err := json.Unmarshal(body, &hash); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Upload again with hash - should return 204 No Content
		resp2, _, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/"+hash, content)
		if err != nil {
			t.Fatalf("Second request failed: %v", err)
		}

		if resp2.StatusCode != http.StatusNoContent {
			t.Errorf("Expected status 204 for duplicate content, got %d", resp2.StatusCode)
		}
	})

	t.Run("Upload with mismatched hash", func(t *testing.T) {
		// First, let's compute hash for two different contents
		firstContent := []byte("First content")
		secondContent := []byte("Second content")
		thirdContent := []byte("Third content")

		// Upload first content to get its hash - just for setup
		_, body1, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/", firstContent)
		if err != nil {
			t.Fatalf("First upload failed: %v", err)
		}
		var firstHash string
		if err := json.Unmarshal(body1, &firstHash); err != nil {
			t.Fatalf("Failed to decode first response: %v", err)
		}

		// Upload second content to get its hash - just for setup
		_, body2, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/", secondContent)
		if err != nil {
			t.Fatalf("Second upload failed: %v", err)
		}
		var secondHash string
		if err := json.Unmarshal(body2, &secondHash); err != nil {
			t.Fatalf("Failed to decode second response: %v", err)
		}

		// Compute hash for third content (without uploading it)
		thirdHash := grits.ComputeHash(thirdContent)

		// Now the actual test: upload second content but claiming it has third hash
		// This should fail with 400 Bad Request
		resp, _, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/"+thirdHash, secondContent)
		if err != nil {
			t.Fatalf("Test request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400 for hash mismatch, got %d", resp.StatusCode)
		}
	})

	t.Run("Upload with invalid hash format", func(t *testing.T) {
		content := []byte("Test content")
		resp, _, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/not-a-valid-hash", content)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400 for invalid hash, got %d", resp.StatusCode)
		}
	})

	t.Run("Fetch existing resource", func(t *testing.T) {
		content := []byte("Content to fetch")
		_, body, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/", content)
		if err != nil {
			t.Fatalf("Upload failed: %v", err)
		}

		var hash string
		if err := json.Unmarshal(body, &hash); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Now fetch it
		resp2, body2, err := makeRequest(url, http.MethodGet, "/grits/v1/blob/"+hash, nil)
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}

		if resp2.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for fetch, got %d", resp2.StatusCode)
		}

		if !bytes.Equal(body2, content) {
			t.Errorf("Retrieved content doesn't match original")
		}
	})

	t.Run("Fetch nonexistent resource", func(t *testing.T) {
		// Generate a valid hash format but one that shouldn't exist
		// This assumes the hash format matches what NewBlobAddrFromString accepts
		nonexistentHash := "abcdef1234567890"
		resp, _, err := makeRequest(url, http.MethodGet, "/grits/v1/blob/"+nonexistentHash, nil)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		// Could be 400 if the hash format is invalid or 404 if valid but not found
		// The exact behavior depends on how NewBlobAddrFromString validates hashes
		expectedStatuses := []int{http.StatusNotFound, http.StatusBadRequest}
		statusOK := false
		for _, status := range expectedStatuses {
			if resp.StatusCode == status {
				statusOK = true
				break
			}
		}

		if !statusOK {
			t.Errorf("Expected status in %v for nonexistent resource, got %d",
				expectedStatuses, resp.StatusCode)
		}
	})

	t.Run("Fetch with invalid hash format", func(t *testing.T) {
		resp, _, err := makeRequest(url, http.MethodGet, "/grits/v1/blob/invalid-hash-format", nil)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400 for invalid hash format, got %d", resp.StatusCode)
		}
	})

	t.Run("HEAD request for existing resource", func(t *testing.T) {
		// First upload content
		content := []byte("Content for HEAD test")
		_, body, err := makeRequest(url, http.MethodPut, "/grits/v1/blob/", content)
		if err != nil {
			t.Fatalf("Upload failed: %v", err)
		}

		var hash string
		if err := json.Unmarshal(body, &hash); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Make HEAD request
		resp2, _, err := makeRequest(url, http.MethodHead, "/grits/v1/blob/"+hash, nil)
		if err != nil {
			t.Fatalf("Failed to create HEAD request: %v", err)
		}

		if resp2.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200 for HEAD, got %d", resp2.StatusCode)
		}

		// Check content length is set correctly
		contentLength := resp2.Header.Get("Content-Length")
		expectedLength := fmt.Sprintf("%d", len(content))
		if contentLength != expectedLength {
			t.Errorf("Expected Content-Length %s, got %s", expectedLength, contentLength)
		}

		// Body should be empty for HEAD requests
		headBody, err := io.ReadAll(resp2.Body)
		if err != nil {
			t.Fatalf("Failed to read HEAD response body: %v", err)
		}

		if len(headBody) != 0 {
			t.Errorf("HEAD request returned non-empty body")
		}
	})
}

// Update the existing upload test to use the new API
func TestUploadAndDownloadBlob(t *testing.T) {
	// Use the helper function to set up the server
	server, cleanup := SetupTestServer(t,
		WithHttpModule(2287),
		WithLocalVolume("root"))
	defer cleanup()

	server.Start()
	defer server.Stop()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	url := "http://localhost:2287"

	// Test upload using the new PUT method
	testBlobContent := "Test blob content"

	req, err := http.NewRequest(http.MethodPut, url+"/grits/v1/blob/",
		bytes.NewBufferString(testBlobContent))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	client := &http.Client{}
	uploadResp, err := client.Do(req)
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
	if err != nil || downloadResp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to download blob: %v; HTTP status code: %d", err, downloadResp.StatusCode)
	}
	defer downloadResp.Body.Close()

	downloadedContent, err := io.ReadAll(downloadResp.Body)
	if err != nil {
		t.Fatalf("Failed to read download response body: %v", err)
	}

	if string(downloadedContent) != testBlobContent {
		t.Errorf("Downloaded content does not match uploaded content. Got %s, want %s",
			string(downloadedContent), testBlobContent)
	}
}
