package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
		Volume string `json:"volume"`
		Path   string `json:"path"`
		Addr   string `json:"addr"`
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
			Volume string `json:"volume"`
			Path   string `json:"path"`
			Addr   string `json:"addr"`
		}{
			{Path: content, Addr: fmt.Sprintf("blob:%s-%d", addresses[i], len(content))},
			//{Path: "dir", Addr: "tree:QmSvPd3sHK7iWgZuW47fyLy4CaZQe2DwxvRhrJ39VpBVMK-2"},
			//{Path: "dir/subdir", Addr: "tree:QmSvPd3sHK7iWgZuW47fyLy4CaZQe2DwxvRhrJ39VpBVMK-2"},
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

	var lookupResponse [][]string
	if err := json.NewDecoder(resp.Body).Decode(&lookupResponse); err != nil {
		t.Fatalf("Failed to decode lookup response: %v", err)
	}

	// Check the lookup response
	// You may want to add specific checks here based on your expectations
	fmt.Printf("Lookup response: %+v\n", lookupResponse)
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

	httpConfig := &HTTPModuleConfig{
		ThisHost: "localhost",
		ThisPort: 2287, // Just for setup, actual port not used with httptest
	}
	url := "http://" + httpConfig.ThisHost + ":" + fmt.Sprintf("%d", httpConfig.ThisPort)

	httpModule := NewHTTPModule(srv, httpConfig)
	srv.AddModule(httpModule)

	srv.Start()
	defer srv.Stop()

	time.Sleep(1 * time.Second)

	// Test upload
	testBlobContent := "Test blob content"
	uploadResp, err := http.Post(url+"/grits/v1/upload", "text/plain", bytes.NewBufferString(testBlobContent))
	if err != nil {
		t.Fatalf("Failed to upload blob: %v", err)
	} else if uploadResp.StatusCode != http.StatusOK {
		uploadBody, err := io.ReadAll(uploadResp.Body)
		if err == nil {
			uploadBody = []byte("")
		}
		t.Fatalf("Failed to upload blob: code %d %s", uploadResp.StatusCode, uploadBody)
	}
	defer uploadResp.Body.Close()

	var blobAddress string
	if err := json.NewDecoder(uploadResp.Body).Decode(&blobAddress); err != nil {
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
		t.Errorf("Downloaded content does not match uploaded content. Got %s, want %s", string(downloadedContent), testBlobContent)
	}
}
