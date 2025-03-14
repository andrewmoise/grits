package server

import (
	"log"
	"net/http"
	"time"
)

// MirrorModuleConfig defines configuration for the mirror
type MirrorModuleConfig struct {
	// HubEndpoint is the URL of the hub to connect to
	HubEndpoint string `json:"hubEndpoint"`

	// Volume is the name of the volume to mirror
	Volume string `json:"volume"`

	// MirrorID is the unique identifier for this mirror
	MirrorID string `json:"mirrorID"`

	// MaxCacheSize is maximum storage space in bytes
	MaxCacheSize int64 `json:"maxCacheSize"`

	// Port to serve content on (if 0, a port will be chosen)
	ServingPort int `json:"servingPort"`
}

// MirrorModule serves as a content mirror
type MirrorModule struct {
	Config *MirrorModuleConfig
	Server *Server

	hubClient       *http.Client
	heartbeatTicker *time.Ticker

	// Track if we're registered with the hub
	isRegistered bool
}

func (*MirrorModule) GetModuleName() string {
	return "mirror"
}

// NewMirrorModule creates a new mirror module
func NewMirrorModule(server *Server, config *MirrorModuleConfig) *MirrorModule {
	// Set defaults if not specified
	if config.MaxCacheSize <= 0 {
		config.MaxCacheSize = 1 * 1024 * 1024 * 1024 // 1GB default
	}

	mirror := &MirrorModule{
		Config:    config,
		Server:    server,
		hubClient: &http.Client{Timeout: 10 * time.Second},
	}

	return mirror
}

// Start begins mirror operations
func (m *MirrorModule) Start() error {
	log.Printf("Starting mirror module")

	// If no port specified, choose one
	if m.Config.ServingPort == 0 {
		// For now, just log - would need actual port selection logic
		log.Printf("Need to select a port for mirror")
	}

	// Start heartbeat ticker (every 30 seconds)
	m.heartbeatTicker = time.NewTicker(30 * time.Second)
	go m.heartbeatLoop()

	return nil
}

// Stop shuts down the mirror
func (m *MirrorModule) Stop() error {
	log.Printf("Stopping mirror module")

	if m.heartbeatTicker != nil {
		m.heartbeatTicker.Stop()
	}

	return nil
}

// heartbeatLoop periodically sends heartbeats to the hub
func (m *MirrorModule) heartbeatLoop() {
	// First try to register
	if !m.isRegistered {
		if err := m.registerWithHub(); err != nil {
			log.Printf("Failed to register with hub: %v", err)
		} else {
			m.isRegistered = true
		}
	}

	// Then start heartbeat loop
	for range m.heartbeatTicker.C {
		if err := m.sendHeartbeat(); err != nil {
			log.Printf("Failed to send heartbeat: %v", err)
			m.isRegistered = false
		}
	}
}

// registerWithHub registers this mirror with the hub
func (m *MirrorModule) registerWithHub() error {
	// Stub - would implement actual registration logic
	log.Printf("Would register with hub at %s", m.Config.HubEndpoint)
	return nil
}

// sendHeartbeat sends a heartbeat to the hub
func (m *MirrorModule) sendHeartbeat() error {
	// Stub - would implement actual heartbeat logic
	log.Printf("Would send heartbeat to hub")
	return nil
}
