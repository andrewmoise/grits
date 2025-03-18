package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"net/http"
	"time"
)

// Overall module design:
//
// For now:
// origin - Can replicate content to mirrors
// mirror - Can serve content from origins

// Future:
// tracker - Can accept connections from peers and set them up as first-class citizens
// peer - Can identify this server to a tracker, and take part in the network without preexisting DNS
// host - Separate place for a bunch of "content source" http endponts (e.g. lookup) so we can run http without them

// Endpoints
//
// Served by origin module:
// grits/v1/origin/register-mirror - For mirrors to maintain their active status
// grits/v1/origin/unregister-mirror - If a mirror knows it's going offline
// grits/v1/origin/list-mirrors - For clients to get the list of available mirrors
//
// grits/v1/origin/submit-telemetry - (future) For clients to submit connection stats from mirrors

// Served by tracker module:
//
// grits/v1/peer/register - For peers to authenticate to
// grits/v1/peer/heartbeat

// MirrorModuleConfig defines configuration for mirror functionality
type MirrorModuleConfig struct {
	RemoteHost    string `json:"remoteHost"`
	MaxStorageMB  int    `json:"maxStorageMB,omitempty"`
	Protocol      string `json:"protocol,omitempty"` // Protocol to use (http/https)
	LocalHostname string `json:"localHostname"`      // Hostname of this mirror server
}

// MirrorModule implements mirror functionality for a remote volume
type MirrorModule struct {
	Config *MirrorModuleConfig
	Server *Server

	// Blob cache for mirrored content
	blobCache *grits.BlobCache

	// For periodic heartbeat
	heartbeatTicker *time.Ticker
	stopCh          chan struct{}
	stoppedCh       chan struct{} // Signal when goroutine has stopped
	running         bool          // Track running state
}

// NewMirrorModule creates a new MirrorModule instance
func NewMirrorModule(server *Server, config *MirrorModuleConfig) (*MirrorModule, error) {
	if config.RemoteHost == "" {
		return nil, fmt.Errorf("RemoteHost is required")
	}

	if config.Protocol == "" {
		config.Protocol = "https" // Default to https
	}

	// Convert MB to bytes for the cache size
	maxSizeBytes := int64(config.MaxStorageMB) * 1024 * 1024
	if maxSizeBytes <= 0 {
		// Default to 1GB if not specified
		maxSizeBytes = 1 * 1024 * 1024 * 1024
	}

	mm := &MirrorModule{
		Config:    config,
		Server:    server,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}

	// Create a blob cache with a fetch function that retrieves from the upstream
	mm.blobCache = grits.NewBlobCache(
		server.BlobStore.(*grits.LocalBlobStore), // Local blob store for storage
		maxSizeBytes,
		mm.fetchBlobFromUpstream, // Function to fetch blobs from upstream
	)

	// Web requests are handled via the http module. Mirror requests are normal blob requests,
	// so they can still be cached the same as any other request for that same blob
	// from whatever source. The HTTP module blob handler sees a special header, though, finds
	// us in its list of mirrors, than asks our blob cache directly for the blob. If our
	// blob cache doesn't have it, it dispatches to fetchBlobFromUpstream() down below, which
	// actually makes the request.

	return mm, nil
}

// Start initializes and starts the mirror module
func (mm *MirrorModule) Start() error {
	log.Printf("Starting MirrorModule with upstream %s", mm.Config.RemoteHost)

	// Start heartbeat to upstream if needed
	mm.heartbeatTicker = time.NewTicker(300 * time.Second)
	go mm.heartbeatLoop()

	return nil
}

// Stop halts the mirror module operations
func (mm *MirrorModule) Stop() error {
	if !mm.running {
		return nil // Already stopped
	}

	log.Printf("Stopping MirrorModule")

	if mm.heartbeatTicker != nil {
		mm.heartbeatTicker.Stop()
	}

	// Send stop signal to goroutine
	close(mm.stopCh)

	// Wait for confirmation that the goroutine has exited
	<-mm.stoppedCh

	mm.running = false
	log.Printf("Mirror module stopped")

	return nil
}

func (mm *MirrorModule) GetModuleName() string {
	return "mirror"
}

func (m *MirrorModule) GetConfig() interface{} {
	return m.Config
}

// heartbeatLoop sends periodic heartbeats to the upstream server
func (mm *MirrorModule) heartbeatLoop() {
	defer close(mm.stoppedCh) // Signal that the goroutine has stopped

	err := mm.sendHeartbeat()
	if err != nil {
		log.Printf("Error sending mirror heartbeat: %v", err)
	}

	for {
		select {
		case <-mm.heartbeatTicker.C:
			err = mm.sendHeartbeat()
			if err != nil {
				log.Printf("Error sending heartbeat: %v", err)
			}
		case <-mm.stopCh:
			return // Exit when stop signal received
		}
	}
}

// sendHeartbeat sends a heartbeat to the upstream server
// sendHeartbeat registers with the origin server
func (mm *MirrorModule) sendHeartbeat() error {
	// Construct the registration URL
	registrationURL := fmt.Sprintf("%s://%s/grits/v1/origin/register-mirror",
		mm.Config.Protocol, mm.Config.RemoteHost)

	log.Printf("Register mirror %s via %s", mm.Config.LocalHostname, registrationURL)

	// Create request payload with our hostname
	payload := struct {
		Hostname string `json:"hostname"`
	}{
		Hostname: mm.Config.LocalHostname,
	}

	// Convert payload to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal registration payload: %v", err)
	}

	// Send the registration request
	resp, err := http.Post(registrationURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to send registration to origin: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("origin server rejected registration: status=%d, body=%s",
			resp.StatusCode, string(bodyBytes))
	}

	// Parse response to get the heartbeat interval
	var response struct {
		HeartbeatIntervalSecs int `json:"heartbeatIntervalSecs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		// If we can't parse the response, use a default interval
		log.Printf("Warning: couldn't parse heartbeat interval from response: %v", err)
		return nil // Still consider registration successful
	}

	// If we got a valid interval, update our timer to 90% of the server's interval
	if response.HeartbeatIntervalSecs > 0 {
		newInterval := time.Duration(response.HeartbeatIntervalSecs) * time.Second * 9 / 10
		if mm.heartbeatTicker != nil {
			mm.heartbeatTicker.Reset(newInterval)
		}
		log.Printf("Updated heartbeat interval to %v (90%% of server's %ds)",
			newInterval, response.HeartbeatIntervalSecs)
	}

	return nil
}

// fetchBlobFromUpstream retrieves a blob from the upstream server
func (mm *MirrorModule) fetchBlobFromUpstream(addr *grits.BlobAddr) (grits.CachedFile, error) {
	log.Printf("Fetching blob %s from upstream %s", addr.Hash, mm.Config.RemoteHost)

	upstreamURL := fmt.Sprintf("%s://%s/grits/v1/blob/%s",
		mm.Config.Protocol,
		mm.Config.RemoteHost,
		addr.String())

	log.Printf("Fetch: %s", upstreamURL)

	resp, err := http.Get(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from upstream: %v", err)
	}
	defer resp.Body.Close()

	// Check for successful response
	if resp.StatusCode != http.StatusOK {
		log.Printf("Error on %s: %d", upstreamURL, resp.StatusCode)

		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	// Use the buffered content for processing
	contentLength := resp.Header.Get("Content-Length")
	log.Printf("Content-Length from origin: %s bytes", contentLength)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	// Log the content length and first part of the response
	log.Printf("Received %d bytes from upstream", len(bodyBytes))
	if len(bodyBytes) < 100 {
		log.Printf("Response body: %s", string(bodyBytes))
	} else {
		log.Printf("Response first 100 bytes: %x", bodyBytes[:100])
	}

	// Create a new reader from the bytes for your existing code
	bodyReader := bytes.NewReader(bodyBytes)

	// Replace resp.Body with bodyReader in your AddReader call
	cachedFile, err := mm.Server.BlobStore.AddReader(bodyReader)

	// Add the blob to our local blob store
	//cachedFile, err := mm.Server.BlobStore.AddReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to store downloaded blob: %v", err)
	}

	// Verify the hash matches what we expected
	if cachedFile.GetAddress().Hash != addr.Hash {
		log.Printf("  hash mismatch!")
		log.Printf("    %s", cachedFile.GetAddress().Hash)
		log.Printf("    %s", addr.Hash)
		cachedFile.Release() // Release our reference before returning error
		return nil, fmt.Errorf("hash mismatch: expected %s, got %s",
			addr.Hash, cachedFile.GetAddress().Hash)
	}

	log.Printf("Successfully mirrored blob %s from upstream", addr.Hash)
	return cachedFile, nil
}
