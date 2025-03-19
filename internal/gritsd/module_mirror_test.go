package gritsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// WithMirrorModule is an initializer for adding a Mirror module that points to another server
func WithMirrorModule(remoteHost string, maxStorageMB int, port int) TestModuleInitializer {
	return func(t *testing.T, s *Server) {
		config := &MirrorModuleConfig{
			RemoteHost:    remoteHost,
			MaxStorageMB:  maxStorageMB,
			Protocol:      "http",
			LocalHostname: "localhost:" + fmt.Sprintf("%d", port), // Add proper local hostname
		}

		mirrorModule, err := NewMirrorModule(s, config)
		if err != nil {
			t.Fatalf("Failed to create mirror module: %v", err)
		}
		s.AddModule(mirrorModule)
	}
}

func TestMirrorModule(t *testing.T) {
	// Create an origin server with content to mirror
	originPort := 2387
	originServer, originCleanup := SetupTestServer(t,
		WithHttpModule(originPort),
		WithWikiVolume("source"),
		WithOriginModule([]string{"localhost:2388"})) // Add allowed mirrors
	defer originCleanup()

	originServer.Start()
	defer originServer.Stop()
	time.Sleep(500 * time.Millisecond)

	originHost := "localhost:2387"

	// Add some content to the origin server
	testContent := "This is test content to be mirrored"
	uploadResp, err := http.Post("http://"+originHost+"/grits/v1/upload", "text/plain",
		bytes.NewBufferString(testContent))
	if err != nil {
		t.Fatalf("Failed to upload content to origin server: %v", err)
	}

	// Read the response body to get the blob address
	responseData, err := io.ReadAll(uploadResp.Body)
	uploadResp.Body.Close()
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Parse the blob address from the JSON response
	var blobAddress string
	if err := json.Unmarshal(responseData, &blobAddress); err != nil {
		t.Fatalf("Failed to unmarshal blob address: %v", err)
	}

	t.Logf("Uploaded blob address: %s", blobAddress)

	// Double-check that the fetch works okay from the actual origin server

	req, _ := http.NewRequest("GET", "http://"+originHost+"/grits/v1/blob/"+blobAddress, nil)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to fetch blob from mirror: %v", err)
	}
	defer resp.Body.Close()

	// When the mirror returns a non-OK status, read and print the response body
	if resp.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("Mirror returned non-OK status: %d, body: %s", resp.StatusCode, string(errorBody))
	}

	// Read the response and verify content matches
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if string(body) != testContent {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, string(body))
	}

	// Create a mirror server that points to the origin
	mirrorPort := 2388
	mirrorServer, mirrorCleanup := SetupTestServer(t,
		WithHttpModule(mirrorPort),
		WithMirrorModule(originHost, 10, mirrorPort)) // 10MB cache
	defer mirrorCleanup()

	mirrorServer.Start()
	defer mirrorServer.Stop()
	time.Sleep(500 * time.Millisecond)

	// Now fetch the content via the mirror using the correct mirror specifier format
	req, _ = http.NewRequest("GET", "http://localhost:"+
		fmt.Sprintf("%d", mirrorPort)+"/grits/v1/blob/"+blobAddress, nil)

	client = &http.Client{}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Failed to fetch blob from mirror: %v", err)
	}
	defer resp.Body.Close()

	// When the mirror returns a non-OK status, read and print the response body
	if resp.StatusCode != http.StatusOK {
		errorBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("Mirror returned non-OK status: %d, body: %s", resp.StatusCode, string(errorBody))
	}

	// Read the response and verify content matches
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if string(body) != testContent {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, string(body))
	}

	// Make a second request to verify it comes from the cache
	// This doesn't actually verify the cache hit, but confirms functionality
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to fetch blob from mirror (second request): %v", err)
	}
	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("Failed to read response body (second request): %v", err)
	}

	if string(body2) != testContent {
		t.Errorf("Content mismatch on second request. Expected: %s, Got: %s",
			testContent, string(body2))
	}
}
