package server

import (
	"fmt"
	"grits/internal/grits"
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
// host - We can separate a bunch of the http endpoints aside from blob/ into a separate optional module

// Endpoints
//
// Served by origin module:
// grits/v1/origin/register - For mirrors to register themselves
// grits/v1/origin/heartbeat - For mirrors to maintain their active status
// grits/v1/origin/list - For clients to get the list of available mirrors
//
// grits/v1/origin/telemetry - (future) For clients to submit connection stats from mirrors

// Served by tracker module:
//
// grits/v1/peer/register - For peers to authenticate to
// grits/v1/peer/heartbeat

// MirrorModuleConfig defines configuration for mirror functionality
type MirrorModuleConfig struct {
	RemoteHost   string `json:"remoteHost"`
	RemoteVolume string `json:"remoteVolume"`
	MaxStorageMB int    `json:"maxStorageMB,omitempty"`
	Protocol     string `json:"protocol,omitempty"` // Protocol to use (http/https)
}

// MirrorModule implements mirror functionality for a swarm
type MirrorModule struct {
	Config *MirrorModuleConfig
	Server *Server

	// Blob cache for mirrored content
	blobCache *grits.BlobCache

	// For periodic heartbeat
	heartbeatTicker *time.Ticker
	stopCh          chan struct{}

	// HTTP mux for handling requests
	mux *http.ServeMux
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

	// Create the mirror module
	mm := &MirrorModule{
		Config: config,
		Server: server,
		stopCh: make(chan struct{}),
		mux:    http.NewServeMux(),
	}

	// Create a blob cache with a fetch function that retrieves from the upstream
	mm.blobCache = grits.NewBlobCache(
		server.BlobStore.(*grits.LocalBlobStore), // Local blob store for storage
		maxSizeBytes,
		mm.fetchBlobFromUpstream, // Function to fetch blobs from upstream
	)

	return mm, nil
}

// Start initializes and starts the mirror module
func (mm *MirrorModule) Start() error {
	log.Printf("Starting MirrorModule with upstream %s", mm.Config.RemoteHost)

	// Start heartbeat to upstream if needed
	mm.heartbeatTicker = time.NewTicker(60 * time.Second)
	go mm.heartbeatLoop()

	return nil
}

// Stop halts the mirror module operations
func (mm *MirrorModule) Stop() error {
	log.Printf("Stopping MirrorModule")

	if mm.heartbeatTicker != nil {
		mm.heartbeatTicker.Stop()
	}

	close(mm.stopCh)
	return nil
}

func (mm *MirrorModule) GetModuleName() string {
	return "mirror"
}

// heartbeatLoop sends periodic heartbeats to the upstream server
func (mm *MirrorModule) heartbeatLoop() {
	for {
		select {
		case <-mm.heartbeatTicker.C:
			err := mm.sendHeartbeat()
			if err != nil {
				log.Printf("Error sending heartbeat: %v", err)
			}
		case <-mm.stopCh:
			return
		}
	}
}

// sendHeartbeat sends a heartbeat to the upstream server
func (mm *MirrorModule) sendHeartbeat() error {
	// TODO: Implement heartbeat logic
	return nil
}

// fetchBlobFromUpstream retrieves a blob from the upstream server
func (mm *MirrorModule) fetchBlobFromUpstream(addr *grits.BlobAddr) (grits.CachedFile, error) {
	log.Printf("Fetching blob %s from upstream %s", addr.Hash, mm.Config.RemoteHost)

	upstreamURL := fmt.Sprintf("%s://%s/grits/v1/blob/%s",
		mm.Config.Protocol,
		mm.Config.RemoteHost,
		addr.String())

	resp, err := http.Get(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from upstream: %v", err)
	}
	defer resp.Body.Close()

	// Check for successful response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	// Add the blob to our local blob store
	cachedFile, err := mm.Server.BlobStore.AddReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to store downloaded blob: %v", err)
	}

	// Verify the hash matches what we expected
	if cachedFile.GetAddress().Hash != addr.Hash {
		cachedFile.Release() // Release our reference before returning error
		return nil, fmt.Errorf("hash mismatch: expected %s, got %s",
			addr.Hash, cachedFile.GetAddress().Hash)
	}

	log.Printf("Successfully mirrored blob %s from upstream", addr.Hash)
	return cachedFile, nil
}
