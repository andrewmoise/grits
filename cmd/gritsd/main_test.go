package main_test

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
	"grits/internal/gritsd"
)

func TestFileOperations(t *testing.T) {
	// Setup
	baseURL := "http://localhost:2187"
	s, cleanup := gritsd.SetupTestServer(t, gritsd.WithHttpModule(2187), gritsd.WithLocalVolume("root"))
	defer cleanup()

	s.Start()
	defer s.Stop()

	// Wait a little for initialization
	time.Sleep(100 * time.Millisecond)

	// 1. Create 5 files
	for i := 1; i <= 5; i++ {
		url := fmt.Sprintf("%s/grits/v1/content/root/%d", baseURL, i)
		content := fmt.Sprintf("Test data %d", i)
		req, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString(content))
		if err != nil {
			t.Fatalf("Creating PUT request failed: %v", err)
		}
		log.Printf("Creating file %d\n", i)
		resp, err := http.DefaultClient.Do(req)
		log.Printf("File %d created\n", i)
		if err != nil {
			t.Fatalf("PUT request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected OK status; got %d (%s)", resp.StatusCode, string(respBody))
		}
		resp.Body.Close()
	}

	log.Printf("Files created\n")

	// 2. Get the list of files
	lookupURL := fmt.Sprintf("%s/grits/v1/lookup/root", baseURL)
	lookupPayload := []byte(`""`)
	resp, err := http.Post(lookupURL, "application/json", bytes.NewBuffer(lookupPayload))
	if err != nil {
		t.Fatalf("failed to perform lookup: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("couldn't read response body: %v", err)
	}
	log.Printf("Response body: %s", string(respBody))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Lookup failed with status code %d - %s", resp.StatusCode, string(respBody))
	}

	var lookupResponse [][]any
	if err := json.Unmarshal(respBody, &lookupResponse); err != nil {
		t.Fatalf("Failed to decode lookup response: %v", err)
	}

	// Extract metadata from the response
	if len(lookupResponse) < 1 {
		t.Fatalf("Lookup response did not include directory metadata")
	}

	volume := s.FindVolumeByName("root")
	if volume == nil {
		t.Fatalf("Couldn't load root volume")
	}

	rootAddrStr, ok := lookupResponse[0][1].(string)
	if !ok {
		t.Fatalf("Expected string for metadata address, got %T", lookupResponse[0][1])
	}
	rootAddr := grits.BlobAddr(rootAddrStr)
	rootNode, err := volume.GetFileNode(rootAddr)
	if err != nil {
		t.Fatalf("Couldn't load node for root: %v", err)
	}
	defer rootNode.Release()

	// First get the directory's content hash
	rootContentHash := rootNode.Metadata().ContentHash

	// Will be able to also access the serial number if needed
	//log.Printf("Volume serial number: %d", lookupResponse.SerialNumber)

	// Now fetch the directory content
	dirURL := fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, rootContentHash)
	dirResp, err := http.Get(dirURL)
	if err != nil {
		t.Fatalf("Failed to fetch directory content: %v", err)
	}
	defer dirResp.Body.Close()

	var dirListing map[string]string // filename => metadata CID
	if err := json.NewDecoder(dirResp.Body).Decode(&dirListing); err != nil {
		t.Fatalf("Failed to decode directory listing: %v", err)
	}

	log.Printf("Files listed\n")
	count := 0

	for name, metadataCID := range dirListing {
		log.Printf("%s -> %s\n", name, metadataCID)

		// Fetch file metadata
		metadataURL := fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, metadataCID)
		metadataResp, err := http.Get(metadataURL)
		if err != nil {
			t.Fatalf("Failed to fetch metadata for %s: %v", name, err)
		}

		var metadata grits.GNodeMetadata
		if err := json.NewDecoder(metadataResp.Body).Decode(&metadata); err != nil {
			metadataResp.Body.Close()
			t.Fatalf("Failed to decode metadata for %s: %v", name, err)
		}
		metadataResp.Body.Close()

		if metadata.Type != grits.GNodeTypeFile {
			t.Errorf("Expected file type for %s, got %v", name, metadata.Type)
		}

		// Fetch the actual content
		contentURL := fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, metadata.ContentHash)
		contentResp, err := http.Get(contentURL)
		if err != nil {
			t.Fatalf("Failed to fetch content for %s: %v", name, err)
		}

		if contentResp.StatusCode != http.StatusOK {
			contentResp.Body.Close()
			t.Fatalf("Expected OK status; got %d", contentResp.StatusCode)
		}

		expectedContent := fmt.Sprintf("Test data %s", name)
		body, err := io.ReadAll(contentResp.Body)
		contentResp.Body.Close()
		if err != nil {
			t.Fatalf("Error reading file %s: %v", name, err)
		}

		if string(body) != expectedContent {
			t.Errorf("Expected content %s; got %s", expectedContent, body)
		}

		if metadata.Size != int64(len(expectedContent)) {
			t.Errorf("Expected size %d for %s, got %d", len(expectedContent), name, metadata.Size)
		}

		count++
	}

	if count != 5 {
		t.Errorf("Expected 5 files; got %d", count)
	}

	// 3. Delete file 3
	url := fmt.Sprintf("%s/grits/v1/content/root/3", baseURL)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("Creating DELETE request failed: %v", err)
	}

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected OK status; got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4. Overwrite file 5
	url = fmt.Sprintf("%s/grits/v1/content/root/5", baseURL)
	content := "Overwritten test data 5"
	req, err = http.NewRequest(http.MethodPut, url, bytes.NewBufferString(content))
	if err != nil {
		t.Fatalf("Creating PUT request failed: %v", err)
	}

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected OK status; got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 5. Final verification
	resp, err = http.Post(lookupURL, "application/json", bytes.NewBuffer(lookupPayload))
	if err != nil {
		t.Fatalf("failed to perform lookup: %v", err)
	}
	defer resp.Body.Close()

	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("couldn't read response body: %v", err)
	}

	log.Printf("Response body: %s", string(respBody))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Lookup failed with status code %d - %s", resp.StatusCode, string(respBody))
	}

	lookupResponse = make([][]any, 0)
	if err := json.Unmarshal(respBody, &lookupResponse); err != nil {
		t.Fatalf("Failed to decode lookup response: %v", err)
	}

	if len(lookupResponse) < 1 {
		t.Fatalf("Lookup response did not include directory metadata")
	}

	rootContentStr, ok := lookupResponse[0][2].(string)
	if !ok {
		t.Fatalf("Expected string for content address, got %T", lookupResponse[0][1])
	}
	rootContentHash = grits.BlobAddr(rootContentStr)

	// Get the updated directory listing
	dirURL = fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, rootContentHash)
	dirResp, err = http.Get(dirURL)
	if err != nil {
		t.Fatalf("Failed to fetch directory content: %v", err)
	}
	defer dirResp.Body.Close()

	dirListing = make(map[string]string)
	if err := json.NewDecoder(dirResp.Body).Decode(&dirListing); err != nil {
		t.Fatalf("Failed to decode directory listing: %v", err)
	}

	log.Printf("Files listed\n")
	count = 0

	for name, metadataCID := range dirListing {
		log.Printf("%s -> %s\n", name, metadataCID)

		if name == "3" {
			t.Errorf("File 3 should have been deleted")
			continue
		}

		// Fetch file metadata
		metadataURL := fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, metadataCID)
		metadataResp, err := http.Get(metadataURL)
		if err != nil {
			t.Fatalf("Failed to fetch metadata for %s: %v", name, err)
		}

		var metadata grits.GNodeMetadata
		if err := json.NewDecoder(metadataResp.Body).Decode(&metadata); err != nil {
			metadataResp.Body.Close()
			t.Fatalf("Failed to decode metadata for %s: %v", name, err)
		}
		metadataResp.Body.Close()

		// Fetch content
		contentURL := fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, metadata.ContentHash)
		contentResp, err := http.Get(contentURL)
		if err != nil {
			t.Fatalf("Failed to fetch content for %s: %v", name, err)
		}

		if contentResp.StatusCode != http.StatusOK {
			contentResp.Body.Close()
			t.Fatalf("Expected OK status; got %d", contentResp.StatusCode)
		}

		var expectedContent string
		if name == "5" {
			expectedContent = "Overwritten test data 5"
		} else {
			expectedContent = fmt.Sprintf("Test data %s", name)
		}

		body, err := io.ReadAll(contentResp.Body)
		contentResp.Body.Close()
		if err != nil {
			t.Fatalf("Error reading file %s: %v", name, err)
		}

		if string(body) != expectedContent {
			t.Errorf("Expected content %s; got %s", expectedContent, body)
		}

		if metadata.Size != int64(len(expectedContent)) {
			t.Errorf("Expected size %d for %s, got %d", len(expectedContent), name, metadata.Size)
		}

		count++
	}

	if count != 4 {
		t.Errorf("Expected 4 files; got %d", count)
	}
}
