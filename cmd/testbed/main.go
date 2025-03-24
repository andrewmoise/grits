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
	"syscall"
	"time"

	"grits/internal/grits"
	"grits/internal/gritsd"
)

const (
	NUM_MIRRORS      = 5
	INACTIVE_TIMEOUT = 300 // seconds
	MIRROR_BASE_PORT = 1800
)

func main() {
	baseDir := "./testbed"

	// Ensure the base directory exists
	err := os.MkdirAll(baseDir, 0755)
	if err != nil {
		log.Fatalf("Failed to create base directory: %v", err)
	}

	// Setup signal handling for graceful shutdown
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	// Create a list to track all our servers
	var allServers []*gritsd.Server

	// Setup origin server
	originServer, originPort, originHost, enableTls, err := setupOriginServer()
	if err != nil {
		log.Fatalf("Failed to setup origin server: %v", err)
	}
	allServers = append(allServers, originServer)

	// Start the origin server
	if err := originServer.Start(); err != nil {
		log.Fatalf("Failed to start origin server: %v", err)
	}

	// Setup and start mirror servers
	for i := 0; i < NUM_MIRRORS; i++ {
		mirrorServer, err := setupMirrorServer(baseDir, originServer.Config, originPort, originHost, enableTls, i)
		if err != nil {
			log.Fatalf("Failed to setup mirror server %d: %v", i, err)
		}
		allServers = append(allServers, mirrorServer)

		log.Printf("Starting mirror server %d", i)
		if err := mirrorServer.Start(); err != nil {
			log.Fatalf("Failed to start mirror server %d: %v", i, err)
		}
	}

	log.Println("All servers started. Testing functionality...")

	// Test the origin-mirror system
	if err := testOriginMirrorSystem(originPort, originHost, enableTls, NUM_MIRRORS); err != nil {
		log.Printf("WARNING: Origin-Mirror test failed: %v", err)
	} else {
		log.Println("Origin-Mirror tests passed successfully!")
	}

	// Test the peer-tracker system
	if err := testPeerTrackerSystem(originPort, originHost, enableTls); err != nil {
		log.Printf("WARNING: Peer-Tracker test failed: %v", err)
	} else {
		log.Println("Peer-Tracker tests passed successfully!")
	}

	log.Println("Press Ctrl+C to shut down.")

	// Wait for shutdown signal
	<-signals
	log.Println("Shutting down all servers...")

	// Shutdown all servers in reverse order (mirrors first, then origin)
	for i := len(allServers) - 1; i >= 0; i-- {
		server := allServers[i]
		server.Stop()
	}

	// Wait for all server goroutines to exit
	log.Println("All servers stopped successfully.")
}

func setupOriginServer() (*gritsd.Server, int, string, bool, error) {
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

	// 2. Add tracker module configuration
	// For testing purposes, override cert verification
	trackerOverride := true
	trackerModuleConfig, err := json.Marshal(map[string]interface{}{
		"type":                     "tracker",
		"peerSubdomain":            fmt.Sprintf("cache.%s", originHost),
		"heartbeatIntervalSec":     60, // More frequent for testing
		"overrideCertVerification": trackerOverride,
	})
	if err != nil {
		return nil, -1, "", false, fmt.Errorf("failed to marshal tracker module config: %v", err)
	}

	log.Printf("Setting up origin server on port %d with tracker module", originPort)

	// Save existing modules and append the new ones
	existingModules := config.Modules
	config.Modules = append(existingModules, originModuleConfig, trackerModuleConfig)

	// Create the server with the augmented config
	srv, err := gritsd.NewServer(config)
	if err != nil {
		return nil, -1, "", false, fmt.Errorf("failed to create origin server: %v", err)
	}

	return srv, originPort, originHost, enableTls, nil
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

	var mirrors []*gritsd.MirrorInfo
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

	// Immediately link the blob
	linkData := []struct {
		Path string `json:"path"`
		Addr string `json:"addr"`
	}{
		{
			Path: "test-blob",
			Addr: fmt.Sprintf("blob:%s-%d", blobAddr, len(testContent)), // Ugh
		},
	}

	linkReqBody, err := json.Marshal(linkData)
	if err != nil {
		return fmt.Errorf("failed to marshal link request data: %v", err)
	}

	// Send link request to root volume
	linkResp, err := http.Post(
		fmt.Sprintf("%s://%s:%d/grits/v1/link/root", scheme, originHost, originPort),
		"application/json",
		bytes.NewBuffer(linkReqBody),
	)

	if err != nil {
		return fmt.Errorf("failed to send link request: %v", err)
	}
	defer linkResp.Body.Close()

	if linkResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(linkResp.Body)
		return fmt.Errorf("link request returned non-OK status: %d, body: %s",
			linkResp.StatusCode, string(respBody))
	}

	log.Printf("Successfully linked blob to /test-blob in root volume")

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

func setupMirrorServer(baseDir string, originConfig *grits.Config, originPort int, originHost string, enableTls bool, index int) (*gritsd.Server, error) {
	// Create directory for this mirror
	serverDir := filepath.Join(baseDir, fmt.Sprintf("mirror-%d", index))
	err := os.MkdirAll(filepath.Join(serverDir, "var"), 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create mirror server directory: %v", err)
	}

	configFilename := filepath.Join(serverDir, "grits.cfg")

	if _, err = os.Stat(configFilename); os.IsNotExist(err) {
		// No preexisting config, make one.

		config := grits.NewConfig(serverDir)
		config.ServerDir = serverDir

		// Mirror port is base port + index
		mirrorPort := MIRROR_BASE_PORT + index

		// 1. Create HTTP module configuration
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

		// 2. Create Mirror module configuration
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

		// 3. Add Peer module configuration
		// Determine tracker URL based on origin protocol and port
		trackerURL := fmt.Sprintf("%s://%s:%d", protocol, originHost, originPort)
		peerName := fmt.Sprintf("mirror-%d", index)

		peerModuleConfig, err := json.Marshal(map[string]interface{}{
			"type":       "peer",
			"trackerUrl": trackerURL,
			"peerName":   peerName,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal Peer module config: %v", err)
		}

		// Add all modules to config
		config.Modules = []json.RawMessage{
			httpModuleConfig,
			mirrorModuleConfig,
			peerModuleConfig,
		}

		err = config.SaveToFile(configFilename)
		if err != nil {
			log.Fatalf("Couldn't save config to %s: %v", configFilename, err)
		}
	} else if err != nil {
		log.Fatalf("Problem trying to find %s: %v", configFilename, err)
	}

	newConfig := grits.NewConfig(serverDir)
	err = newConfig.LoadFromFile(configFilename)
	if err != nil {
		log.Fatalf("Couldn't load config from %s: %v", configFilename, err)
	}

	// Create the server instance
	srv, err := gritsd.NewServer(newConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create mirror server: %v", err)
	}

	log.Printf("Mirror server %d configured for origin %s with peer module", index, originHost)

	// Generate self-signed certificate for this mirror
	peerName := fmt.Sprintf("mirror-%d", index)
	err = gritsd.GenerateSelfCert(newConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to generate self-certificate for mirror %d: %v", index, err)
	}

	// Copy the certificate to the tracker's peer cert directory
	err = copyPeerCertToTracker(originConfig, baseDir, peerName, index)
	if err != nil {
		return nil, fmt.Errorf("failed to copy certificate for mirror %d: %v", index, err)
	}

	log.Printf("Mirror server %d configured for origin %s with peer module", index, originHost)

	return srv, nil
}

// Add this new function to testbed.go
func testPeerTrackerSystem(originPort int, originHost string, enableTls bool) error {
	log.Println("Testing peer-tracker functionality...")

	// Allow some time for peers to register with the tracker
	log.Println("Waiting for peer registration...")
	time.Sleep(3 * time.Second)

	// Determine protocol based on TLS setting
	scheme := "http"
	if enableTls {
		scheme = "https"
	}

	// Get active peers from tracker
	log.Printf("Requesting list of peers from tracker...")
	resp, err := http.Get(fmt.Sprintf("%s://%s:%d/grits/v1/tracker/list-peers",
		scheme, originHost, originPort))
	if err != nil {
		return fmt.Errorf("failed to get peer list: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list-peers returned non-OK status: %d", resp.StatusCode)
	}

	var peers []*gritsd.PeerInfo
	if err = json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return fmt.Errorf("failed to decode peer list: %v", err)
	}

	log.Printf("Found %d active peers (expected: %d)", len(peers), NUM_MIRRORS)
	if len(peers) != NUM_MIRRORS {
		log.Printf("Warning: Expected %d active peers, but found %d", NUM_MIRRORS, len(peers))
	}

	// Print details of each peer
	for i, peer := range peers {
		log.Printf("Peer %d: Name=%s, Active=%v, IP=%s, Port=%d",
			i, peer.Name, peer.IsActive, peer.IPAddress, peer.Port)
	}

	return nil
}

func copyPeerCertToTracker(originConfig *grits.Config, baseDir, peerName string, index int) error {
	log.Printf("Copying certificate for peer %s to tracker", peerName)

	// Source certificate path (from peer's self-signed cert directory)
	// Using correct format: var/certs/self/current/fullchain.pem
	mirrorDir := filepath.Join(baseDir, fmt.Sprintf("mirror-%d", index))
	mirrorConfig := grits.NewConfig(mirrorDir)
	mirrorConfig.ServerDir = mirrorDir

	// Use the GetCertificateFiles helper for consistency
	sourceCertPath, _ := gritsd.GetCertificateFiles(mirrorConfig, gritsd.SelfSignedCert, "current")

	// Check if source cert exists
	if _, err := os.Stat(sourceCertPath); os.IsNotExist(err) {
		return fmt.Errorf("source certificate does not exist at %s", sourceCertPath)
	}

	destCertPath, _ := gritsd.GetCertificateFiles(originConfig, gritsd.PeerCert, peerName)

	// Ensure destination directory exists
	destCertDir := filepath.Dir(destCertPath)
	if err := os.MkdirAll(destCertDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Read source cert
	certData, err := os.ReadFile(sourceCertPath)
	if err != nil {
		return fmt.Errorf("failed to read source certificate: %v", err)
	}

	// Write to destination
	if err := os.WriteFile(destCertPath, certData, 0644); err != nil {
		return fmt.Errorf("failed to write destination certificate: %v", err)
	}

	log.Printf("Successfully copied certificate for peer %s from %s to %s",
		peerName, sourceCertPath, destCertPath)
	return nil
}
