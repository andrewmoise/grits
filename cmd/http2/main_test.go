package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"grits/internal/grits"
	"grits/internal/server"
)

func TestFileOperations(t *testing.T) {
	// Setup
	baseURL := "http://localhost:2187"
	s, cleanup := server.SetupTestServer(t, server.WithHttpModule(2187), server.WithWikiVolume("root"))
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

	var lookupResponse [][]string
	if err := json.Unmarshal(respBody, &lookupResponse); err != nil {
		t.Fatalf("Failed to decode lookup response: %v", err)
	}

	// Extract blob address for the directory tree node and download it
	if len(lookupResponse) < 1 {
		t.Fatalf("Lookup response did not include directory tree node")
	}

	treeBlobAddr := grits.NewGNodeAddr(lookupResponse[0][1])

	blobURL := fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, treeBlobAddr.Hash)
	blobResp, err := http.Get(blobURL)
	if err != nil {
		t.Fatalf("Failed to download tree blob: %v", err)
	}
	defer blobResp.Body.Close()

	if blobResp.StatusCode != http.StatusOK {
		t.Fatalf("Blob download failed with status code %d", blobResp.StatusCode)
	}

	// Read the response body
	bodyBytes, err := io.ReadAll(blobResp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	// Print the response body as text
	bodyString := string(bodyBytes)
	fmt.Println("Response Body:", bodyString)

	// Decode the directory listing from the tree blob
	var files map[string]string // Or whatever structure you expect
	if err := json.NewDecoder(bytes.NewBuffer(bodyBytes)).Decode(&files); err != nil {
		t.Fatalf("Failed to decode tree blob content: %v", err)
	}

	log.Printf("Files listed\n")

	count := 0

	for name, hash := range files {
		log.Printf("%s -> %s\n", name, hash)

		parts := strings.Split(hash, ":")
		if len(parts) != 2 {
			t.Errorf("Invalid hash: %s", hash)
		}

		parts = strings.Split(parts[1], "-")
		if len(parts) != 2 {
			t.Errorf("Invalid blob+size: %s", parts[0])
		}

		url := fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, parts[0])
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected OK status; got %d", resp.StatusCode)
		}

		content := fmt.Sprintf("Test data %s", name)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Error reading file %s: %v", name, err)
		}

		if string(body) != content {
			t.Errorf("Expected content %s; got %s", content, body)
		}

		count += 1
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

	// 5. Final listing and verification of files and their content

	lookupURL = fmt.Sprintf("%s/grits/v1/lookup/root", baseURL)
	lookupPayload = []byte(`""`)
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

	lookupResponse = make([][]string, 0)
	if err := json.Unmarshal(respBody, &lookupResponse); err != nil {
		t.Fatalf("Failed to decode lookup response: %v", err)
	}

	// Extract blob address for the directory tree node and download it
	if len(lookupResponse) < 1 {
		t.Fatalf("Lookup response did not include directory tree node")
	}

	treeBlobAddr = grits.NewGNodeAddr(lookupResponse[0][1])

	blobURL = fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, treeBlobAddr.Hash)
	blobResp, err = http.Get(blobURL)
	if err != nil {
		t.Fatalf("Failed to download tree blob: %v", err)
	}
	defer blobResp.Body.Close()

	if blobResp.StatusCode != http.StatusOK {
		t.Fatalf("Blob download failed with status code %d", blobResp.StatusCode)
	}

	files = make(map[string]string)
	if err := json.NewDecoder(blobResp.Body).Decode(&files); err != nil {
		t.Fatalf("Error decoding file list: %v", err)
		return
	}

	log.Printf("Files listed\n")

	count = 0

	for name, hash := range files {
		log.Printf("%s -> %s\n", name, hash)

		if name == "3" {
			t.Errorf("File 3 should have been deleted")
		}

		parts := strings.Split(hash, ":")
		if len(parts) != 2 {
			t.Errorf("Invalid hash: %s", hash)
		}

		parts = strings.Split(parts[1], "-")
		if len(parts) != 2 {
			t.Errorf("Invalid blob+size: %s", parts[0])
		}

		url = fmt.Sprintf("%s/grits/v1/blob/%s", baseURL, parts[0])
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected OK status; got %d", resp.StatusCode)
		}

		var content string
		if name == "5" {
			content = "Overwritten test data 5"
		} else {
			content = fmt.Sprintf("Test data %s", name)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Error reading file %s: %v", name, err)
		}

		if string(body) != content {
			t.Errorf("Expected content %s; got %s", content, resp.Body)
		}

		count += 1
	}

	if count != 4 {
		t.Errorf("Expected 4 files; got %d", count)
	}

}
