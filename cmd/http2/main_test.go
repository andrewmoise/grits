package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"grits/internal/proxy"
	"grits/internal/server"
)

func setupTestServer(t *testing.T) (*server.Server, func()) {
	tempDir, err := os.MkdirTemp("", "grits_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	config := proxy.NewConfig("test_host", 1234)
	config.StorageDirectory = filepath.Join(tempDir, "blobstore_test")
	config.StorageSize = 10 * 1024 * 1024    // 10MB for testing
	config.StorageFreeSize = 8 * 1024 * 1024 // 8MB for testing
	config.NamespaceStoreFile = filepath.Join(tempDir, "namespace_store.json")

	os.MkdirAll(config.StorageDirectory, 0755)

	fmt.Printf("Created temp directory: %s\n", tempDir)

	s, err := server.NewServer(config)
	if err != nil {
		t.Fatalf("Failed to initialize server: %v", err)
	}

	fmt.Printf("Server initialized\n")

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return s, cleanup
}

func TestFileOperations(t *testing.T) {
	baseURL := "http://localhost:1787"

	fmt.Printf("Running test with base URL: %s\n", baseURL)

	s, cleanup := setupTestServer(t)
	defer cleanup()

	fmt.Printf("Server initialized\n")

	s.Start()

	fmt.Printf("Server started\n")

	// 1. Create 5 files

	for i := 1; i <= 5; i++ {
		url := fmt.Sprintf("%s/grits/v1/namespace/root/%d", baseURL, i)
		content := fmt.Sprintf("Test data %d", i)
		req, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString(content))
		if err != nil {
			t.Fatalf("Creating PUT request failed: %v", err)
		}
		fmt.Printf("Creating file %d\n", i)
		resp, err := http.DefaultClient.Do(req)
		fmt.Printf("File %d created\n", i)
		if err != nil {
			t.Fatalf("PUT request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected OK status; got %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	fmt.Printf("Files created\n")

	// 2. Get the list of files

	url := fmt.Sprintf("%s/grits/v1/root/root", baseURL)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected OK status; got %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading root namespace response:", err)
		return
	}
	rootHash := string(bodyBytes)

	url = fmt.Sprintf("%s/grits/v1/sha256/%s", baseURL, rootHash)
	listResp, err := http.Get(url)
	if err != nil {
		fmt.Println("Error listing files:", err)
		return
	}
	defer listResp.Body.Close()

	var files map[string]string
	if err := json.NewDecoder(listResp.Body).Decode(&files); err != nil {
		fmt.Println("Error decoding file list:", err)
		return
	}

	fmt.Printf("Files listed\n")

	count := 0

	for name, hash := range files {
		fmt.Printf("%s -> %s\n", name, hash)

		url = fmt.Sprintf("%s/grits/v1/sha256/%s", baseURL, hash)
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
			t.Errorf("Expected content %s; got %s", content, resp.Body)
		}

		count += 1
	}

	if count != 5 {
		t.Errorf("Expected 5 files; got %d", count)
	}

	// Simulate listing files using GET request to /grits/v1/root/root
	// The actual listing logic should replace this with the appropriate request and parsing logic

	// Simulate deleting file 3 using DELETE request

	// Simulate overwriting file 5 using PUT request

	// Final listing and verification of files and their content
}
