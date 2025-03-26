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
	originServer, originHttpConfig, err := setupOriginServer()
	if err != nil {
		log.Fatalf("Failed to setup origin server: %v", err)
	}
	allServers = append(allServers, originServer)

	// Start the origin server
	if err := originServer.Start(); err != nil {
		log.Fatalf("Failed to start origin server: %v", err)
	}

	log.Println("Origin server started. Setting up mirror servers with peer modules only...")

	// Setup and start mirror servers with peer modules only
	mirrorServers := make([]*gritsd.Server, NUM_MIRRORS)
	for i := 0; i < NUM_MIRRORS; i++ {
		mirrorServer, _, err := setupMirrorServer(baseDir, originHttpConfig, i)
		if err != nil {
			log.Fatalf("Failed to setup mirror server %d: %v", i, err)
		}
		mirrorServers[i] = mirrorServer
		allServers = append(allServers, mirrorServer)

		configFilePath := filepath.Join(baseDir, fmt.Sprintf("mirror-%d", i), "grits.cfg")
		configExists := false
		if _, err := os.Stat(configFilePath); err == nil {
			configExists = true
		}

		if !configExists {
			// Only do certificate copying and peer setup for new configurations
			peerName := fmt.Sprintf("mirror-%d", i)
			if err := copyPeerCertToTracker(originServer.Config, baseDir, peerName, i); err != nil {
				log.Fatalf("Failed to copy certificate for mirror %d: %v", i, err)
			}
		}

		// Start the server (will either start all modules or just the peer module)
		log.Printf("Starting mirror server %d", i)
		if err := mirrorServer.Start(); err != nil {
			log.Printf("Warning: Issues starting mirror server %d: %v", i, err)
		}

		// If this is a brand new setup, we may need to add HTTP and Mirror modules
		if !configExists {
			log.Println("Waiting for peer registration...")
			time.Sleep(1 * time.Second)

			// Add HTTP and Mirror modules if this is a new configuration
			if err := addHttpAndMirrorModulesToServer(mirrorServer, originHttpConfig, i); err != nil {
				log.Fatalf("Failed to add modules to mirror %d: %v", i, err)
			}

			// Save the full configuration now that everything is set up
			if err := mirrorServer.SaveConfigToFile(configFilePath); err != nil {
				log.Printf("Warning: Failed to save mirror %d configuration: %v", i, err)
			}
		}
	}

	// Give time for modules to start up
	time.Sleep(1 * time.Second)

	log.Println("All servers fully started. Testing functionality...")

	// Run the tests after all modules are running
	originPort := originHttpConfig.ThisPort
	originHost := originHttpConfig.ThisHost
	enableTls := originHttpConfig.EnableTls

	// Test the origin-mirror system
	if err := testOriginMirrorSystem(originPort, originHost, enableTls, NUM_MIRRORS); err != nil {
		log.Printf("WARNING: Origin-Mirror test failed: %v", err)
	} else {
		log.Println("Origin-Mirror tests passed successfully!")
	}

	// Test the peer-tracker system again
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

	log.Println("All servers stopped successfully.")
}

func setupOriginServer() (*gritsd.Server, *gritsd.HTTPModuleConfig, error) {
	// Load the existing configuration
	config := grits.NewConfig(".")
	if err := config.LoadFromFile("grits.cfg"); err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %v", err)
	}
	config.ServerDir = "." // Ensure server directory is set

	// For testing, we need to know the HTTP configuration
	var originHttpConfig *gritsd.HTTPModuleConfig

	for _, moduleRaw := range config.Modules {
		var moduleMap map[string]any
		if err := json.Unmarshal(moduleRaw, &moduleMap); err != nil {
			continue // Skip modules that can't be unmarshaled
		}

		if moduleType, ok := moduleMap["type"].(string); ok && moduleType == "http" {
			// Create a new HTTPModuleConfig instance
			originHttpConfig = &gritsd.HTTPModuleConfig{}

			// Unmarshal the raw JSON data into the config struct
			if err := json.Unmarshal(moduleRaw, originHttpConfig); err != nil {
				return nil, nil, fmt.Errorf("failed to parse HTTP module config: %v", err)
			}

			break // Found the HTTP module, no need to continue
		}
	}

	if originHttpConfig == nil {
		return nil, nil, fmt.Errorf("couldn't find HTTP module in origin server")
	}

	// Create additional module configurations

	var originProtocol string
	if originHttpConfig.EnableTls {
		originProtocol = "https"
	} else {
		originProtocol = "http"
	}

	// 1. Origin module with mirror settings - using fully qualified URLs
	allowedMirrors := make([]string, NUM_MIRRORS)
	for i := 0; i < NUM_MIRRORS; i++ {
		// Use http:// protocol, localhost, and the expected mirror port
		allowedMirrors[i] = fmt.Sprintf("%s://mirror-%d.cache.%s:%d", originProtocol, i, originHttpConfig.ThisHost, MIRROR_BASE_PORT+i)
	}

	// Log the allowed mirrors for debugging
	log.Printf("Configuring origin with allowed mirrors: %v", allowedMirrors)

	originModuleConfig, err := json.Marshal(map[string]any{
		"type":                "origin",
		"allowedMirrors":      allowedMirrors,
		"inactiveTimeoutSecs": INACTIVE_TIMEOUT,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal origin module config: %v", err)
	}

	// 2. Add tracker module configuration
	// For testing purposes, override cert verification
	trackerOverride := true
	trackerModuleConfig, err := json.Marshal(map[string]any{
		"type":                     "tracker",
		"peerSubdomain":            fmt.Sprintf("cache.%s", originHttpConfig.ThisHost),
		"heartbeatIntervalSec":     60, // More frequent for testing
		"overrideCertVerification": trackerOverride,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal tracker module config: %v", err)
	}

	log.Printf("Setting up origin server on port %d with tracker module", originHttpConfig.ThisPort)

	// Save existing modules and append the new ones
	existingModules := config.Modules
	config.Modules = append(existingModules, originModuleConfig, trackerModuleConfig)

	// Create the server with the augmented config
	srv, err := gritsd.NewServer(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create origin server: %v", err)
	}

	return srv, originHttpConfig, nil
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
		peerName := fmt.Sprintf("mirror-%d", i)
		mirrorHost := fmt.Sprintf("%s.cache.%s", peerName, originHost)

		mirrorResp, err := http.Get(fmt.Sprintf("%s://%s:%d/grits/v1/blob/%s",
			scheme, mirrorHost, mirrorPort, blobAddr))
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

func setupMirrorServer(baseDir string, originHttpConfig *gritsd.HTTPModuleConfig, index int) (*gritsd.Server, *gritsd.PeerModule, error) {
	// Create directory for this mirror
	peerName := fmt.Sprintf("mirror-%d", index)
	serverDir := filepath.Join(baseDir, peerName)
	configFilePath := filepath.Join(serverDir, "grits.cfg")

	// Check if the config file already exists
	configExists := false
	if _, err := os.Stat(configFilePath); err == nil {
		configExists = true
		log.Printf("Found existing config for mirror %d, using it instead of bootstrapping", index)
	}

	// Ensure server directory exists
	err := os.MkdirAll(filepath.Join(serverDir, "var"), 0755)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create mirror server directory: %v", err)
	}

	var srv *gritsd.Server
	var peerModule *gritsd.PeerModule

	// Either load existing config or create a new one
	if configExists {
		// Load existing configuration
		config := grits.NewConfig(serverDir)
		if err := config.LoadFromFile(configFilePath); err != nil {
			return nil, nil, fmt.Errorf("failed to load existing configuration for mirror %d: %v", index, err)
		}

		// Create the server with the loaded config
		srv, err = gritsd.NewServer(config)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create mirror server from existing config: %v", err)
		}

		// Find the peer module
		for _, module := range srv.Modules {
			if pm, ok := module.(*gritsd.PeerModule); ok {
				peerModule = pm
				break
			}
		}
	} else {
		// Create new configuration from scratch (bootstrapping)
		config := grits.NewConfig(serverDir)
		config.ServerDir = serverDir

		// Create the server instance
		srv, err = gritsd.NewServer(config)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create mirror server: %v", err)
		}

		// Generate self-signed certificate
		err = gritsd.GenerateSelfCert(config)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to generate self-certificate for mirror %d: %v", index, err)
		}

		// Create peer module and add it
		protocol := "http"
		if originHttpConfig.EnableTls {
			protocol = "https"
		}
		trackerURL := fmt.Sprintf("%s://%s", protocol, originHttpConfig.ThisHost)

		peerConfig := &gritsd.PeerModuleConfig{
			TrackerUrl: trackerURL,
			PeerName:   peerName,
		}

		peerModule, err = gritsd.NewPeerModule(srv, peerConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create peer module: %v", err)
		}

		srv.AddModule(peerModule)
		log.Printf("Mirror server %d configured with peer module", index)
	}

	return srv, peerModule, nil
}

func addHttpAndMirrorModulesToServer(server *gritsd.Server, originHttpConfig *gritsd.HTTPModuleConfig, index int) error {
	// Get the existing config
	config := server.Config
	peerName := fmt.Sprintf("mirror-%d", index)
	mirrorPort := MIRROR_BASE_PORT + index
	thisHost := fmt.Sprintf("%s.cache.%s", peerName, originHttpConfig.ThisHost)

	// Create HTTP module configuration
	httpModuleConfig, err := json.Marshal(map[string]interface{}{
		"type":            "http",
		"thisPort":        mirrorPort,
		"thisHost":        thisHost,
		"enableTls":       originHttpConfig.EnableTls,
		"autoCertificate": originHttpConfig.AutoCertificate,
		"certbotEmail":    originHttpConfig.CertbotEmail,
		"useSelfSigned":   originHttpConfig.UseSelfSigned,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal HTTP module config: %v", err)
	}

	// Determine protocol based on TLS setting
	protocol := "http"
	if originHttpConfig.EnableTls {
		protocol = "https"
	}

	// Create fully qualified URL for the mirror
	localURL := fmt.Sprintf("%s://%s:%d", protocol, thisHost, mirrorPort)

	// Create Mirror module configuration
	mirrorModuleConfig, err := json.Marshal(map[string]interface{}{
		"type":         "mirror",
		"remoteHost":   originHttpConfig.ThisHost,
		"remoteVolume": "",
		"maxStorageMB": 100,
		"protocol":     protocol,
		"localURL":     localURL,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal Mirror module config: %v", err)
	}

	// Get the existing modules (should include peer module)
	existingModules := config.Modules

	// Add the new modules
	config.Modules = append(existingModules, httpModuleConfig, mirrorModuleConfig)

	// Save the updated config
	err = config.SaveToFile(filepath.Join(config.ServerDir, "grits.cfg"))
	if err != nil {
		return fmt.Errorf("couldn't save updated config: %v", err)
	}

	// Create and add the new modules
	httpModule, err := gritsd.NewHTTPModule(server, &gritsd.HTTPModuleConfig{
		ThisHost:        thisHost,
		ThisPort:        mirrorPort,
		EnableTls:       originHttpConfig.EnableTls,
		AutoCertificate: originHttpConfig.AutoCertificate,
		CertbotEmail:    originHttpConfig.CertbotEmail,
		UseSelfSigned:   originHttpConfig.UseSelfSigned,
	})
	if err != nil {
		return fmt.Errorf("failed to create HTTP module: %v", err)
	}

	// Start the HTTP module
	server.AddModule(httpModule)
	if err := httpModule.Start(); err != nil {
		return fmt.Errorf("failed to start HTTP module: %v", err)
	}

	// Create and add the mirror module
	mirrorModule, err := gritsd.NewMirrorModule(server, &gritsd.MirrorModuleConfig{
		RemoteHost:   originHttpConfig.ThisHost,
		MaxStorageMB: 100,
		Protocol:     protocol,
		LocalURL:     localURL, // Use LocalURL instead of LocalHostname
	})
	if err != nil {
		return fmt.Errorf("failed to create Mirror module: %v", err)
	}

	server.AddModule(mirrorModule)
	if err := mirrorModule.Start(); err != nil {
		return fmt.Errorf("failed to start Mirror module: %v", err)
	}

	log.Printf("Added and started HTTP and Mirror modules for server %d", index)
	return nil
}

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
