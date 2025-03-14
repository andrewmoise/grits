package server

import (
	"log"
	"sync"
	"time"
)

// HubModuleConfig defines configuration for the hub
type HubModuleConfig struct {
	// HeartbeatTimeout is how long a mirror can go without heartbeat before removal (seconds)
	HeartbeatTimeout int `json:"heartbeatTimeout"`

	// AllowedMirrorsDir is the directory containing public keys of authorized mirrors
	AllowedMirrorsDir string `json:"allowedMirrorsDir"`
}

// MirrorInfo stores information about a registered mirror
type MirrorInfo struct {
	ID        string    `json:"id"`
	Endpoint  string    `json:"endpoint"`
	PublicKey string    `json:"publicKey"`
	LastSeen  time.Time `json:"lastSeen"`
	Volume    string    `json:"volume"`
	Capacity  int64     `json:"capacity"`
}

// HubModule coordinates the swarm of mirrors
type HubModule struct {
	Config *HubModuleConfig
	Server *Server

	mirrors     map[string]*MirrorInfo // ID -> Mirror
	mirrorsLock sync.RWMutex

	cleanupTicker *time.Ticker
}

func (*HubModule) GetModuleName() string {
	return "hub"
}

// NewHubModule creates a new hub module
func NewHubModule(server *Server, config *HubModuleConfig) *HubModule {
	// Set defaults if not specified
	if config.HeartbeatTimeout <= 0 {
		config.HeartbeatTimeout = 60 // 1 minute default
	}

	if config.AllowedMirrorsDir == "" {
		config.AllowedMirrorsDir = server.Config.ServerPath("mirrors")
	}

	hub := &HubModule{
		Config:  config,
		Server:  server,
		mirrors: make(map[string]*MirrorInfo),
	}

	return hub
}

// Start begins the hub operations
func (h *HubModule) Start() error {
	log.Printf("Starting hub module")

	// Start cleanup ticker
	h.cleanupTicker = time.NewTicker(time.Duration(h.Config.HeartbeatTimeout) * time.Second)
	go h.cleanupLoop()

	return nil
}

// Stop shuts down the hub
func (h *HubModule) Stop() error {
	log.Printf("Stopping hub module")

	if h.cleanupTicker != nil {
		h.cleanupTicker.Stop()
	}

	return nil
}

// cleanupLoop periodically removes stale mirrors
func (h *HubModule) cleanupLoop() {
	for range h.cleanupTicker.C {
		h.cleanupStaleMirrors()
	}
}

// cleanupStaleMirrors removes mirrors that haven't sent heartbeats
func (h *HubModule) cleanupStaleMirrors() {
	h.mirrorsLock.Lock()
	defer h.mirrorsLock.Unlock()

	cutoff := time.Now().Add(-time.Duration(h.Config.HeartbeatTimeout) * time.Second)

	for id, mirror := range h.mirrors {
		if mirror.LastSeen.Before(cutoff) {
			log.Printf("Removing stale mirror: %s", id)
			delete(h.mirrors, id)
		}
	}
}
