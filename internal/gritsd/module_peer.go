package gritsd

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// PeerModuleConfig defines configuration for peer functionality
type PeerModuleConfig struct {
	TrackerUrl string `json:"trackerUrl"` // Host of the tracker to connect to
	PeerName   string `json:"peerName"`   // Name this peer identifies as
}

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
	if config.TrackerUrl == "" {
		return nil, fmt.Errorf("TrackerUrl is required")
	}

	if config.PeerName == "" {
		return nil, fmt.Errorf("PeerName is required")
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

func (pm *PeerModule) Start() error {
	log.Printf("Starting PeerModule with name %s connecting to tracker %s",
		pm.Config.PeerName, pm.Config.TrackerUrl)

	// First, ensure we have self-signed certificates in the new location
	err := GenerateSelfCert(pm.Server.Config)
	if err != nil {
		return fmt.Errorf("failed to generate self-certificates: %v", err)
	}

	// Attempt initial registration
	err = pm.registerWithTracker()
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

func (pm *PeerModule) registerWithTracker() error {
	log.Printf("Registering peer with tracker: %s", pm.Config.PeerName)

	// Check if we have an HTTP module
	if pm.httpModule == nil {
		log.Printf("No HTTP module for peer, not registering.")
		return nil
	}

	// Prepare request payload
	payload := struct {
		PeerName string `json:"peerName"`
		Port     int    `json:"port"`
	}{
		PeerName: pm.Config.PeerName,
		Port:     pm.httpModule.Config.ThisPort,
	}

	// Convert to JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %v", err)
	}

	// Create the request URL - ensure it uses HTTPS if tracker is using TLS
	trackerUrl := pm.Config.TrackerUrl
	if !strings.HasPrefix(trackerUrl, "http") {
		// Default to https if not specified
		trackerUrl = "https://" + trackerUrl
	}

	url := fmt.Sprintf("%s/grits/v1/tracker/register-peer", trackerUrl)
	log.Printf("Posting to tracker at URL: %s", url)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Create HTTP client with TLS certificate
	var client *http.Client

	// Use client certificate for authentication
	err = GenerateSelfCert(pm.Server.Config)
	if err != nil {
		return fmt.Errorf("couldn't generate self certificates: %v", err)
	}

	certPath, keyPath := GetCertificateFiles(pm.Server.Config, SelfSignedCert, "current")
	log.Printf("Using certificate files: cert=%s, key=%s", certPath, keyPath)

	// Check if the files exist
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return fmt.Errorf("certificate file does not exist: %s", certPath)
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return fmt.Errorf("key file does not exist: %s", keyPath)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("failed to load client certificate: %v", err)
	}

	// Create TLS config with our certificate and skip verification for self-signed server cert
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	// Send the request
	log.Printf("Sending registration request with TLS client certificate")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Log response headers for debugging
	log.Printf("Response status: %s", resp.Status)

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
	// Check if it's an HTTP module
	httpModule, ok := module.(*HTTPModule)
	if !ok {
		return
	}

	// We found an HTTP module - check if we already found one before
	if pm.httpModule != nil {
		log.Fatalf("Multiple HTTP modules found.")
	}

	pm.httpModule = httpModule
	log.Printf("PeerModule: Using HTTP module port %d", httpModule.Config.ThisPort)
}
