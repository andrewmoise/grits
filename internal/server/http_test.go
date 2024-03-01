package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"grits/internal/grits"
)

func TestLookupAndLinkEndpoints(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "grits_server")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := grits.NewConfig()
	config.ServerDir = tempDir
	config.ThisPort = 1887

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	server.Start()
	defer server.Stop(context.Background())

	url := "http://localhost:1887/grits/v1"

	// Upload blobs and link them
	blobContents := []string{"one", "two", "three", "four", "five"}
	addresses := make([]string, len(blobContents))

	for i, content := range blobContents {
		// Upload blob
		resp, err := http.Post(url+"/upload", "text/plain", bytes.NewBufferString(content))
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("Failed to upload blob '%s': %v %d", content, err, resp.StatusCode)
		}

		// Read blob address from response
		addr, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}
		addresses[i] = string(addr)

		// Link the blob to two paths
		for _, path := range []string{"root/" + content, "root/dir/subdir/" + content} {
			resp, err = http.Post(url+"/link/"+path, "application/json", bytes.NewBufferString(`blob:`+string(addr)))
			if err != nil {
				t.Fatalf("Failed to link blob '%s' to path '%s': %v", content, path, err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Failed to link blob '%s' to path '%s': status code %d", content, path, resp.StatusCode)
			}
		}
	}

	// Perform a lookup on "dir/subdir/one"
	resp, err := http.Get(url + "/lookup/root/dir/subdir/one")
	if err != nil {
		t.Fatalf("Failed to perform lookup: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Lookup failed with status code %d", resp.StatusCode)
	}

	var lookupResponse [][]string
	err = json.NewDecoder(resp.Body).Decode(&lookupResponse)
	if err != nil {
		t.Fatalf("Failed to decode lookup response: %v", err)
	}

	// Check the lookup response
}
