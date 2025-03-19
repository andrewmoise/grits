package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// TrackerModuleConfig defines configuration for tracker functionality
type TrackerModuleConfig struct {
	PeerSubdomain        string `json:"peerSubdomain"`        // Root domain for peers (e.g., "cache.mydomain.com")
	HeartbeatIntervalSec int    `json:"heartbeatIntervalSec"` // How often peers should send heartbeats (in seconds)
}

// PeerInfo tracks information about a registered peer
type PeerInfo struct {
	Name          string    `json:"name"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	IsActive      bool      `json:"isActive"`
	Port          int       `json:"port"`
	IPAddress     string    `json:"ipAddress"` // Store peer's IP address for DNS resolution
}

// TrackerModule implements tracker server functionality
type TrackerModule struct {
	Config *TrackerModuleConfig
	Server *Server

	peers      map[string]*PeerInfo // key is peer name
	peersMutex sync.RWMutex         // protect concurrent access to peers map

	stopCh    chan struct{} // Signal channel for stopping goroutines
	stoppedCh chan struct{} // Channel to signal when goroutines are stopped
	running   bool          // Track if the module is running

	dnsServer *dns.Server // DNS server instance
}

// NewTrackerModule creates a new TrackerModule instance
func NewTrackerModule(server *Server, config *TrackerModuleConfig) (*TrackerModule, error) {
	if config.PeerSubdomain == "" {
		return nil, fmt.Errorf("peerSubdomain is required")
	}
	if config.HeartbeatIntervalSec <= 0 {
		config.HeartbeatIntervalSec = 300 // Default to 5 minutes
	}

	tm := &TrackerModule{
		Config:    config,
		Server:    server,
		peers:     make(map[string]*PeerInfo),
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}

	// Add module hook to find HTTP module when it's added
	server.AddModuleHook(tm.onModuleAdded)

	return tm, nil
}

// onModuleAdded is called whenever a new module is added to the server
func (tm *TrackerModule) onModuleAdded(module Module) {
	// Check if it's an HTTP module
	httpModule, ok := module.(*HTTPModule)
	if !ok {
		return
	}

	// Register our endpoint with the HTTP module
	log.Printf("TrackerModule: Registering HTTP handlers")
	httpModule.Mux.HandleFunc("/grits/v1/tracker/register-peer",
		httpModule.requestMiddleware(tm.RegisterPeerHandler))
}

// RegisterPeerHandler handles both initial peer registration and periodic heartbeats
func (tm *TrackerModule) RegisterPeerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract client certificates from the request
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "Client certificate required", http.StatusUnauthorized)
		return
	}

	clientCert := r.TLS.PeerCertificates[0]
	peerPublicKey := clientCert.PublicKey

	// Parse the request body
	var request struct {
		PeerName string `json:"peerName"`
		Port     int    `json:"port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request format", http.StatusBadRequest)
		return
	}

	// Validate request data
	if request.PeerName == "" {
		http.Error(w, "PeerName is required", http.StatusBadRequest)
		return
	}

	if request.Port <= 0 || request.Port > 65535 {
		http.Error(w, "Invalid port number", http.StatusBadRequest)
		return
	}

	// Check if we have an authorization file for this peer
	// Update to use the new certificate path structure
	authFilePath := GetCertPath(tm.Server.Config, PeerCert, request.PeerName)
	if !tm.verifyPeerAuthorization(authFilePath, peerPublicKey) {
		log.Printf("Unauthorized registration attempt for peer %s", request.PeerName)
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	// Get peer's IP address
	peerIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		log.Printf("Error extracting peer IP: %v", err)
		http.Error(w, "Failed to determine peer IP address", http.StatusInternalServerError)
		return
	}

	// Generate the peer's FQDN
	peerFQDN := fmt.Sprintf("%s.%s", request.PeerName, tm.Config.PeerSubdomain)

	// Check if this is a new registration or update
	tm.peersMutex.Lock()
	peer, exists := tm.peers[request.PeerName]
	wasInactive := exists && !peer.IsActive

	if !exists {
		// New peer registration
		tm.peers[request.PeerName] = &PeerInfo{
			Name:          request.PeerName,
			LastHeartbeat: time.Now(),
			IsActive:      true,
			Port:          request.Port,
			IPAddress:     peerIP,
		}

		log.Printf("New peer registered: %s (IP: %s, Port: %d)",
			request.PeerName, peerIP, request.Port)
	} else {
		// Existing peer - update information
		peer.LastHeartbeat = time.Now()
		peer.IsActive = true

		if peer.Port != request.Port {
			log.Printf("Peer %s updated port from %d to %d",
				request.PeerName, peer.Port, request.Port)
			peer.Port = request.Port
		}

		if peerIP != peer.IPAddress {
			log.Printf("Peer %s updated IP from %s to %s",
				request.PeerName, peer.IPAddress, peerIP)
			peer.IPAddress = peerIP
		}

		if wasInactive {
			log.Printf("Inactive peer %s is now active again", request.PeerName)
		}
	}
	tm.peersMutex.Unlock()

	// Enhanced response with status information and heartbeat interval
	response := struct {
		Status           string `json:"status"`
		FQDN             string `json:"fqdn"`             // Full domain name for the peer
		RegistrationType string `json:"registrationType"` // Type of registration (new/reactivated/heartbeat)
		HeartbeatSec     int    `json:"heartbeatSec"`     // How often peer should send heartbeats
	}{
		Status:       "success",
		FQDN:         peerFQDN,
		HeartbeatSec: tm.Config.HeartbeatIntervalSec,
	}

	// Set registration type based on conditions
	if !exists {
		response.RegistrationType = "new"
	} else if wasInactive {
		response.RegistrationType = "reactivated"
	} else {
		response.RegistrationType = "heartbeat"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// checkPeerActivity runs as a goroutine to mark peers as inactive if they haven't sent a heartbeat
func (tm *TrackerModule) checkPeerActivity() {
	ticker := time.NewTicker(60 * time.Second) // Check every minute
	defer ticker.Stop()
	defer close(tm.stoppedCh) // Signal that we've stopped

	for {
		select {
		case <-ticker.C:
			tm.updatePeerActivity()
		case <-tm.stopCh:
			return // Exit when stop signal received
		}
	}
}

// updatePeerActivity marks peers as inactive if they haven't sent a heartbeat recently
func (tm *TrackerModule) updatePeerActivity() {
	tm.peersMutex.Lock()
	defer tm.peersMutex.Unlock()

	// Peers are considered inactive if no heartbeat for 5 minutes
	inactiveThreshold := time.Now().Add(-5 * time.Minute)

	for name, peer := range tm.peers {
		if peer.LastHeartbeat.Before(inactiveThreshold) {
			if peer.IsActive {
				log.Printf("Peer marked inactive due to missing heartbeat: %s", name)
				peer.IsActive = false
			}
		}
	}
}

// startDNSServer starts a simple DNS server for peer subdomains
func (tm *TrackerModule) startDNSServer() error {
	// Set up DNS handler for our domain
	dns.HandleFunc("peer."+tm.Config.PeerSubdomain+".", tm.handleDNSQuery)

	// Start the DNS server
	server := &dns.Server{
		Addr: ":53", // Standard DNS port
		Net:  "udp",
	}

	tm.dnsServer = server

	go func() {
		log.Printf("Starting DNS server for domain: peer.%s", tm.Config.PeerSubdomain)
		if err := server.ListenAndServe(); err != nil {
			log.Printf("DNS server error: %v", err)
		}
	}()

	return nil
}

// handleDNSQuery responds to DNS queries for peer subdomains
func (tm *TrackerModule) handleDNSQuery(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	for _, q := range r.Question {
		// Extract peer name from the query (e.g., "peer1.peer.example.com.")
		domainParts := strings.Split(q.Name, ".")
		if len(domainParts) < 4 {
			continue // Invalid domain format
		}

		peerName := domainParts[0]

		tm.peersMutex.RLock()
		peer, exists := tm.peers[peerName]
		active := exists && peer.IsActive
		ipAddress := ""
		if exists {
			ipAddress = peer.IPAddress
		}
		tm.peersMutex.RUnlock()

		if !active || ipAddress == "" {
			continue // Peer not active or no IP
		}

		switch q.Qtype {
		case dns.TypeA:
			// Create A record pointing to peer's IP
			rr := &dns.A{
				Hdr: dns.RR_Header{
					Name:   q.Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    300, // 5 minute TTL
				},
				A: net.ParseIP(ipAddress),
			}
			m.Answer = append(m.Answer, rr)
		}
	}

	w.WriteMsg(m)
}

// verifyPeerAuthorization checks if a peer is authorized based on its public key
func (tm *TrackerModule) verifyPeerAuthorization(authFilePath string, peerPublicKey interface{}) bool {
	// This is a simplified implementation - you'd need to implement
	// actual public key verification based on your specific requirements

	// Check if authorization file exists
	_, err := os.Stat(authFilePath)
	if os.IsNotExist(err) {
		return false
	}

	// For now, we'll assume the file's existence is enough for authorization
	// In a real implementation, you'd verify the public key matches what's in the file
	return true
}

// Start initializes and starts the tracker module
func (tm *TrackerModule) Start() error {
	log.Printf("Starting TrackerModule with subdomain %s", tm.Config.PeerSubdomain)

	// Start the DNS server
	if err := tm.startDNSServer(); err != nil {
		return fmt.Errorf("failed to start DNS server: %v", err)
	}

	// Start background peer activity checker
	go tm.checkPeerActivity()

	tm.running = true
	return nil
}

// Stop halts the tracker module operations
func (tm *TrackerModule) Stop() error {
	if !tm.running {
		return nil // Already stopped
	}

	log.Printf("Stopping TrackerModule")

	// Stop the DNS server if running
	if tm.dnsServer != nil {
		tm.dnsServer.Shutdown()
	}

	// Send stop signal to goroutines
	close(tm.stopCh)

	// Wait for confirmation that goroutines have exited
	<-tm.stoppedCh

	tm.running = false
	log.Printf("TrackerModule stopped")

	return nil
}

func (tm *TrackerModule) GetModuleName() string {
	return "tracker"
}

func (tm *TrackerModule) GetConfig() interface{} {
	return tm.Config
}
