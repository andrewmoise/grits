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

func TestLookupAndLinkEndpoints(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "grits_server")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := grits.NewConfig(tempDir)

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	httpConfig := &HttpModuleConfig{
		ThisPort: 1887,
	}
	httpModule := NewHttpModule(server, httpConfig)
	server.Modules = append(server.Modules, httpModule)

	server.Start()
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

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
			{Path: "root/" + content, Addr: "blob:" + addresses[i]},
			{Path: "root/dir/subdir/" + content, Addr: "blob:" + addresses[i]},
		}

		linkPayload, _ := json.Marshal(linkData)
		resp, err = http.Post(url+"/link", "application/json", bytes.NewBuffer(linkPayload))
		if err != nil || resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			bodyString := string(bodyBytes)

			t.Fatalf("Failed to link blob '%s': %v %d %s", content, err, resp.StatusCode, bodyString)
		}
		resp.Body.Close()
	}

	// Perform a lookup on "dir/subdir/one"
	lookupPayload, _ := json.Marshal("root/dir/subdir/one")
	resp, err := http.Post(url+"/lookup", "application/json", bytes.NewBuffer(lookupPayload))
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
