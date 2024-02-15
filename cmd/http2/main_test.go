package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"grits/internal/grits"
	"grits/internal/server"
)

func setupTestServer(t *testing.T) (*server.Server, func()) {
	tempDir, err := os.MkdirTemp("", "grits_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	config := grits.NewConfig()
	config.ServerDir = tempDir
	config.StorageSize = 10 * 1024 * 1024    // 10MB for testing
	config.StorageFreeSize = 8 * 1024 * 1024 // 8MB for testing

	log.Printf("Created temp directory: %s\n", tempDir)

	s, err := server.NewServer(config)
	if err != nil {
		t.Fatalf("Failed to initialize server: %v", err)
	}

	log.Printf("Server initialized\n")

	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return s, cleanup
}

func TestFileOperations(t *testing.T) {
	// Setup
	baseURL := "http://localhost:1787"
	s, cleanup := setupTestServer(t)
	defer cleanup()
	s.Start()

	// Ensure graceful shutdown and capture any errors
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer func() {
		if err := s.Stop(ctx); err != nil {
			t.Logf("Server shutdown error: %v", err)
		}
	}()

	// 1. Create 5 files

	for i := 1; i <= 5; i++ {
		url := fmt.Sprintf("%s/grits/v1/namespace/root/%d", baseURL, i)
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
			t.Errorf("Expected OK status; got %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	log.Printf("Files created\n")

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

	log.Printf("Files listed\n")

	count := 0

	for name, hash := range files {
		log.Printf("%s -> %s\n", name, hash)

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
			t.Errorf("Expected content %s; got %s", content, body)
		}

		count += 1
	}

	if count != 5 {
		t.Errorf("Expected 5 files; got %d", count)
	}

	// 3. Delete file 3

	url = fmt.Sprintf("%s/grits/v1/namespace/root/3", baseURL)
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

	url = fmt.Sprintf("%s/grits/v1/namespace/root/5", baseURL)
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

	url = fmt.Sprintf("%s/grits/v1/root/root", baseURL)
	resp, err = http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected OK status; got %d", resp.StatusCode)
	}

	bodyBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading root namespace response:", err)
		return
	}
	rootHash = string(bodyBytes)

	url = fmt.Sprintf("%s/grits/v1/sha256/%s", baseURL, rootHash)
	listResp, err = http.Get(url)
	if err != nil {
		fmt.Println("Error listing files:", err)
		return
	}
	defer listResp.Body.Close()

	files = make(map[string]string)
	if err = json.NewDecoder(listResp.Body).Decode(&files); err != nil {
		fmt.Println("Error decoding file list:", err)
		return
	}

	log.Printf("Files listed\n")

	count = 0

	for name, hash := range files {
		log.Printf("%s -> %s\n", name, hash)

		if name == "3" {
			t.Errorf("File 3 should have been deleted")
		}

		url = fmt.Sprintf("%s/grits/v1/sha256/%s", baseURL, hash)
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
