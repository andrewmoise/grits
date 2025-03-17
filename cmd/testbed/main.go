package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"grits/internal/grits"
	"grits/internal/server"
)

const (
	NUM_MIRRORS      = 5
	INACTIVE_TIMEOUT = 300 // seconds
	MIRROR_BASE_PORT = 1800
)

// TestServer contains a server instance and its cleanup function
type TestServer struct {
	Server  *server.Server
	Cleanup func()
}

func main() {
	baseDir := "./var/tmp/testbed" // Note, testbed gets blown away once we're done
	defer os.RemoveAll("var/tmp/testbed")

	// Ensure the base directory exists
	err := os.MkdirAll(baseDir, 0755)
	if err != nil {
		log.Fatalf("Failed to create base directory: %v", err)
	}

	// Setup signal handling for graceful shutdown
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	// Create a list to track all our servers
	var allServers []*TestServer
	var wg sync.WaitGroup

	// Setup origin server
	originServer, originPort, originHost, enableTls, err := setupOriginServer()
	if err != nil {
		log.Fatalf("Failed to setup origin server: %v", err)
	}
	allServers = append(allServers, originServer)

	// Start the origin server
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := originServer.Server.Start(); err != nil {
			log.Fatalf("Failed to start origin server: %v", err)
		}
	}()

	// Give the origin server a moment to start up
	time.Sleep(500 * time.Millisecond)

	// Setup and start mirror servers
	for i := 0; i < NUM_MIRRORS; i++ {
		mirrorServer, err := setupMirrorServer(baseDir, originPort, originHost, enableTls, i)
		if err != nil {
			log.Fatalf("Failed to setup mirror server %d: %v", i, err)
		}
		allServers = append(allServers, mirrorServer)

		wg.Add(1)
		go func(index int, server *TestServer) {
			defer wg.Done()
			log.Printf("Starting mirror server %d", index)
			if err := server.Server.Start(); err != nil {
				log.Fatalf("Failed to start mirror server %d: %v", index, err)
			}
		}(i, mirrorServer)
	}

	log.Println("All servers started. Testing functionality...")

	// Perform test operations to verify system is working
	if err := testOriginMirrorSystem(originPort, originHost, enableTls, NUM_MIRRORS); err != nil {
		log.Printf("WARNING: Test failed: %v", err)
	} else {
		log.Println("All tests passed successfully!")
	}

	log.Println("Press Ctrl+C to shut down.")

	// Wait for shutdown signal
	<-signals
	log.Println("Shutting down all servers...")

	// Shutdown all servers in reverse order (mirrors first, then origin)
	for i := len(allServers) - 1; i >= 0; i-- {
		server := allServers[i]
		server.Server.Stop()
		server.Cleanup()
	}

	// Wait for all server goroutines to exit
	wg.Wait()
	log.Println("All servers stopped successfully.")
}

// setupOriginServer creates and configures the origin server
func setupOriginServer() (*TestServer, int, string, bool, error) {
	// Load the existing configuration
	config := grits.NewConfig(".")
	if err := config.LoadFromFile("grits.cfg"); err != nil {
		return nil, -1, "", false, fmt.Errorf("failed to load configuration: %v", err)
	}
	config.ServerDir = "." // Ensure server directory is set

	// For testing, we need to know the HTTP configuration
	var originPort int
	var originHost string
	var enableTls bool

	for _, moduleRaw := range config.Modules {
		var moduleMap map[string]interface{}
		if err := json.Unmarshal(moduleRaw, &moduleMap); err != nil {
			continue // Skip modules that can't be unmarshaled
		}

		if moduleType, ok := moduleMap["type"].(string); ok && moduleType == "http" {
			// Extract port
			if port, ok := moduleMap["thisPort"].(float64); ok {
				originPort = int(port)
			}

			// Extract host
			if host, ok := moduleMap["thisHost"].(string); ok {
				originHost = host
			} else {
				originHost = "localhost" // Default if not specified
			}

			// Extract TLS setting
			if tls, ok := moduleMap["enableTls"].(bool); ok {
				enableTls = tls
			}

			break // Found the HTTP module, no need to continue
		}
	}

	// Create additional module configurations

	// 1. Origin module with mirror settings - using fully qualified URLs
	allowedMirrors := make([]string, NUM_MIRRORS)
	for i := 0; i < NUM_MIRRORS; i++ {
		// Use http:// protocol, localhost, and the expected mirror port
		allowedMirrors[i] = fmt.Sprintf("http://%s:%d", originHost, MIRROR_BASE_PORT+i)
	}

	// Log the allowed mirrors for debugging
	log.Printf("Configuring origin with allowed mirrors: %v", allowedMirrors)

	originModuleConfig, err := json.Marshal(map[string]interface{}{
		"type":                "origin",
		"allowedMirrors":      allowedMirrors,
		"inactiveTimeoutSecs": INACTIVE_TIMEOUT,
	})
	if err != nil {
		return nil, -1, "", false, fmt.Errorf("failed to marshal origin module config: %v", err)
	}

	// 2. Mount module
	mountDir := "mount"
	err = os.MkdirAll(mountDir, 0755)
	if err != nil {
		return nil, -1, "", false, fmt.Errorf("couldn't make mount dir: %v", err)
	}

	mountModuleConfig, err := json.Marshal(map[string]interface{}{
		"type":       "mount",
		"mountPoint": mountDir,
		"volume":     "root", // Using "root" as you did in your second example
	})
	if err != nil {
		return nil, -1, "", false, fmt.Errorf("failed to marshal mount module config: %v", err)
	}

	// Save existing modules and append the new ones
	existingModules := config.Modules
	config.Modules = append(existingModules, originModuleConfig, mountModuleConfig)

	// Create the server with the augmented config
	srv, err := server.NewServer(config)
	if err != nil {
		return nil, -1, "", false, fmt.Errorf("failed to create origin server: %v", err)
	}

	// Return the server and a cleanup function
	cleanup := func() {
		log.Println("Cleaning up origin server resources")
		// Additional cleanup if needed
	}

	return &TestServer{Server: srv, Cleanup: cleanup}, originPort, originHost, enableTls, nil
}

// setupMirrorServer creates and configures a mirror server
// testOriginMirrorSystem verifies that:
// 1. Mirrors register with the origin
// 2. Content uploaded to origin can be fetched from mirrors
func testOriginMirrorSystem(originPort int, originHost string, enableTls bool, numMirrors int) error {
	// Step 1: Check that mirrors have registered with the origin
	log.Println("Testing mirror registration...")

	// Allow some time for all mirrors to register
	time.Sleep(2 * time.Second)

	// Determine protocol based on TLS setting
	scheme := "http"
	if enableTls {
		scheme = "https"
	}

	// Get active mirrors from origin server
	log.Printf("Request list mirrors")
	resp, err := http.Get(fmt.Sprintf("%s://%s:%d/grits/v1/origin/list-mirrors",
		scheme, originHost, originPort))
	if err != nil {
		return fmt.Errorf("failed to get mirror list: %v", err)
	}
	defer resp.Body.Close()
	log.Printf("Done with request list mirrors")

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list-mirrors returned non-OK status: %d", resp.StatusCode)
	}

	var mirrors []*server.MirrorInfo
	if err = json.NewDecoder(resp.Body).Decode(&mirrors); err != nil {
		return fmt.Errorf("failed to decode mirror list: %v", err)
	}

	log.Printf("Found %d active mirrors (expected: %d)", len(mirrors), numMirrors)
	if len(mirrors) != numMirrors {
		return fmt.Errorf("expected %d active mirrors, but found %d", numMirrors, len(mirrors))
	}

	// Step 2: Upload content to origin and verify it can be fetched from mirrors
	log.Println("Testing content replication...")

	// Create test content
	testContent := []byte("This is test content from the origin server - " + time.Now().String())

	// Upload to origin server
	uploadResp, err := http.Post(
		fmt.Sprintf("%s://%s:%d/grits/v1/upload", scheme, originHost, originPort),
		"application/octet-stream",
		bytes.NewBuffer(testContent),
	)
	if err != nil {
		return fmt.Errorf("failed to upload test content: %v", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		// Read the response body to include in the error message
		respBody, readErr := io.ReadAll(uploadResp.Body)
		if readErr != nil {
			// If we can't read the body, still report the status code
			return fmt.Errorf("upload returned non-OK status: %d (could not read response body: %v)",
				uploadResp.StatusCode, readErr)
		}

		// Include both status code and response body in the error
		return fmt.Errorf("upload returned non-OK status: %d, body: %s",
			uploadResp.StatusCode, string(respBody))
	}

	// Get the blob address
	var blobAddr string
	if err = json.NewDecoder(uploadResp.Body).Decode(&blobAddr); err != nil {
		return fmt.Errorf("failed to decode blob address: %v", err)
	}

	log.Printf("Uploaded test content, got blob address: %s", blobAddr)

	// Fetch from each mirror to verify replication
	for i := 0; i < numMirrors; i++ {
		mirrorPort := MIRROR_BASE_PORT + i

		// Give the mirror some time to fetch content from origin
		// In a real test, we might need to implement retry logic with backoff
		time.Sleep(500 * time.Millisecond)

		log.Printf("Attempting to fetch from mirror %d (port %d)...", i, mirrorPort)
		mirrorResp, err := http.Get(fmt.Sprintf("http://localhost:%d/grits/v1/blob/%s", mirrorPort, blobAddr))
		if err != nil {
			return fmt.Errorf("failed to fetch from mirror %d: %v", i, err)
		}
		if mirrorResp.StatusCode != http.StatusOK {
			// Read the response body to include in the error message
			respBody, readErr := io.ReadAll(mirrorResp.Body)
			if readErr != nil {
				// If we can't read the body, still report the status code
				return fmt.Errorf("blob fetch returned non-OK status: %d (could not read response body: %v)",
					mirrorResp.StatusCode, readErr)
			}

			// Include both status code and response body in the error
			return fmt.Errorf("blob fetch returned non-OK status: %d, body: %s",
				mirrorResp.StatusCode, string(respBody))
		}

		// Read content from mirror
		mirrorContent, err := io.ReadAll(mirrorResp.Body)
		mirrorResp.Body.Close()

		if err != nil {
			return fmt.Errorf("failed to read content from mirror %d: %v", i, err)
		}

		// Verify content matches
		if !bytes.Equal(mirrorContent, testContent) {
			return fmt.Errorf("content from mirror %d doesn't match original (got %d bytes, expected %d bytes)",
				i, len(mirrorContent), len(testContent))
		}

		log.Printf("Mirror %d successfully served the content", i)
	}

	return nil
}

func setupMirrorServer(baseDir string, originPort int, originHost string, enableTls bool, index int) (*TestServer, error) {
	// Create directory for this mirror
	serverDir := filepath.Join(baseDir, fmt.Sprintf("mirror-%d", index))
	err := os.MkdirAll(filepath.Join(serverDir, "var"), 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create mirror server directory: %v", err)
	}

	// Create config for mirror server
	config := grits.NewConfig(serverDir)
	config.ServerDir = serverDir

	// Mirror port is base port + index
	mirrorPort := MIRROR_BASE_PORT + index

	// Create module configuration as json.RawMessage objects
	httpModuleConfig, err := json.Marshal(map[string]interface{}{
		"type":     "http",
		"thisPort": mirrorPort,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal HTTP module config: %v", err)
	}

	// Determine protocol based on TLS setting
	protocol := "http"
	if enableTls {
		protocol = "https"
	}

	// Use the fully qualified URL format for localHostname
	localHostname := fmt.Sprintf("http://%s:%d", originHost, mirrorPort)

	mirrorModuleConfig, err := json.Marshal(map[string]interface{}{
		"type":          "mirror",
		"remoteHost":    fmt.Sprintf("%s:%d", originHost, originPort),
		"remoteVolume":  "",  // Default volume
		"maxStorageMB":  100, // 100MB cache
		"protocol":      protocol,
		"localHostname": localHostname,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Mirror module config: %v", err)
	}

	// Add modules to config
	config.Modules = []json.RawMessage{
		httpModuleConfig,
		mirrorModuleConfig,
	}

	// Create the server instance
	srv, err := server.NewServer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create mirror server: %v", err)
	}

	log.Printf("Mirror server %d configured at http://%s:%d", index, originHost, mirrorPort)

	// Return the server and a cleanup function
	cleanup := func() {
		log.Printf("Cleaning up mirror server %d resources", index)
		// Additional cleanup if needed
	}

	return &TestServer{Server: srv, Cleanup: cleanup}, nil
}
