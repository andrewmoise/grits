package server

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

// Main API endpoints

func TestLookupAndLinkEndpoints(t *testing.T) {
	url := "http://localhost:1887/grits/v1"
	server, cleanup := SetupTestServer(t, WithHttpModule(1887), WithWikiVolume("root"))
	defer cleanup()

	server.Start()
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	// Set up directory structure
	linkData := []struct {
		Path string `json:"path"`
		Addr string `json:"addr"`
	}{
		{Path: "dir", Addr: "tree:QmSvPd3sHK7iWgZuW47fyLy4CaZQe2DwxvRhrJ39VpBVMK-2"},
		{Path: "dir/subdir", Addr: "tree:QmSvPd3sHK7iWgZuW47fyLy4CaZQe2DwxvRhrJ39VpBVMK-2"},
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

		// Link the blob to two paths
		linkData := []struct {
			Path string `json:"path"`
			Addr string `json:"addr"`
		}{
			{Path: content, Addr: fmt.Sprintf("blob:%s-%d", addresses[i], len(content))},
			{Path: "dir/subdir/" + content, Addr: fmt.Sprintf("blob:%s-%d", addresses[i], len(content))},
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
	var lookupResponse [][]interface{}
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
