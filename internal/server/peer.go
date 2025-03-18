package server

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// PeerModuleConfig defines configuration for peer functionality
type PeerModuleConfig struct {
	TrackerHost string `json:"trackerHost"`           // Host of the tracker to connect to
	TrackerPort int    `json:"trackerPort,omitempty"` // Port the tracker is listening on (optional)
	PeerName    string `json:"peerName"`              // Name this peer identifies as
	Port        int    `json:"port,omitempty"`        // Port this peer will serve on
}

// PeerModule implements peer functionality
type PeerModule struct {
	Config     *PeerModuleConfig
	Server     *Server
	httpModule *HTTPModule // Reference to the detected HTTP module

	// Registration status
	registered   bool
	fqdn         string
	heartbeatSec int

	// For heartbeat management
	heartbeatTicker *time.Ticker
	stopCh          chan struct{}
	stoppedCh       chan struct{}
	running         bool
}

func NewPeerModule(server *Server, config *PeerModuleConfig) (*PeerModule, error) {
	if config.TrackerHost == "" {
		return nil, fmt.Errorf("TrackerHost is required")
	}

	if config.PeerName == "" {
		return nil, fmt.Errorf("PeerName is required")
	}

	// Default to standard HTTPS port if not specified
	if config.TrackerPort <= 0 {
		config.TrackerPort = 443 // Default HTTPS port
	}

	pm := &PeerModule{
		Config:    config,
		Server:    server,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}

	// Set up module hook to find HTTP modules when they're added
	server.AddModuleHook(pm.onModuleAdded)

	return pm, nil
}

// Start initializes and starts the peer module
func (pm *PeerModule) Start() error {
	log.Printf("Starting PeerModule with name %s connecting to tracker %s",
		pm.Config.PeerName, pm.Config.TrackerHost)

	// Attempt initial registration
	err := pm.registerWithTracker()
	if err != nil {
		return fmt.Errorf("failed to register with tracker: %v", err)
	}

	// Start heartbeat loop
	go pm.heartbeatLoop()

	pm.running = true
	return nil
}

// heartbeatLoop sends periodic heartbeats to the tracker
func (pm *PeerModule) heartbeatLoop() {
	defer close(pm.stoppedCh)

	// Default interval if tracker didn't specify one
	interval := 300 * time.Second
	if pm.heartbeatSec > 0 {
		interval = time.Duration(pm.heartbeatSec*9/10) * time.Second
	}

	pm.heartbeatTicker = time.NewTicker(interval)

	for {
		select {
		case <-pm.heartbeatTicker.C:
			if err := pm.registerWithTracker(); err != nil {
				log.Printf("Error sending heartbeat: %v", err)
			}
		case <-pm.stopCh:
			return
		}
	}
}

// registerWithTracker sends registration/heartbeat to the tracker
func (pm *PeerModule) registerWithTracker() error {
	if pm.Config.Port <= 0 {
		log.Printf("No port for peer, not registering.")
		return nil
	}

	// Prepare request payload
	payload := struct {
		PeerName string `json:"peerName"`
		Port     int    `json:"port"`
	}{
		PeerName: pm.Config.PeerName,
		Port:     pm.Config.Port,
	}

	// Convert to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %v", err)
	}

	// Create the request
	url := fmt.Sprintf("https://%s:%d/grits/v1/tracker/register-peer",
		pm.Config.TrackerHost, pm.Config.TrackerPort)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Load client certificate for authentication
	certPath := pm.Server.Config.ServerPath(fmt.Sprintf("peercerts/%s.key", pm.Config.PeerName))
	cert, err := tls.LoadX509KeyPair(certPath, certPath)
	if err != nil {
		return fmt.Errorf("failed to load client certificate: %v", err)
	}

	// Configure TLS with client certificate
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tracker rejected registration: status=%d, body=%s",
			resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var response struct {
		Status           string `json:"status"`
		FQDN             string `json:"fqdn"`
		RegistrationType string `json:"registrationType"`
		HeartbeatSec     int    `json:"heartbeatSec"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	// Update our status
	pm.fqdn = response.FQDN
	pm.registered = true

	// If heartbeat interval changed, update ticker
	if pm.heartbeatSec != response.HeartbeatSec && response.HeartbeatSec > 0 {
		pm.heartbeatSec = response.HeartbeatSec
		if pm.heartbeatTicker != nil {
			pm.heartbeatTicker.Reset(time.Duration(pm.heartbeatSec) * time.Second)
			log.Printf("Updated heartbeat interval to %d seconds", pm.heartbeatSec)
		}
	}

	log.Printf("Registration status: %s as %s", response.RegistrationType, pm.fqdn)
	return nil
}

// Stop halts the peer module operations
func (pm *PeerModule) Stop() error {
	if !pm.running {
		return nil // Already stopped
	}

	log.Printf("Stopping PeerModule")

	if pm.heartbeatTicker != nil {
		pm.heartbeatTicker.Stop()
	}

	close(pm.stopCh)
	<-pm.stoppedCh

	pm.running = false
	log.Printf("PeerModule stopped")

	return nil
}

func (pm *PeerModule) GetModuleName() string {
	return "peer"
}

func (pm *PeerModule) GetConfig() interface{} {
	return pm.Config
}

// onModuleAdded is called whenever a new module is added to the server
func (pm *PeerModule) onModuleAdded(module Module) {
	// Only process if we don't have a port configured
	if pm.Config.Port > 0 {
		return
	}

	// Check if it's an HTTP module
	httpModule, ok := module.(*HTTPModule)
	if !ok {
		return
	}

	// We found an HTTP module - check if we already found one before
	if pm.httpModule != nil {
		log.Fatalf("Multiple HTTP modules found but no port configured for peer. Please specify a port explicitly.")
	}

	pm.httpModule = httpModule
	pm.Config.Port = httpModule.Config.ThisPort

	log.Printf("PeerModule: Using HTTP module port %d", pm.Config.Port)
}
