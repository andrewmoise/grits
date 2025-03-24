package gritsd

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// TrackerModuleConfig defines configuration for tracker functionality
type TrackerModuleConfig struct {
	PeerSubdomain            string `json:"peerSubdomain"`        // Root domain for peers (e.g., "cache.mydomain.com")
	HeartbeatIntervalSec     int    `json:"heartbeatIntervalSec"` // How often peers should send heartbeats (in seconds)
	OverrideCertVerification *bool  `json:"-"`                    // Test-only flag FIXME FIXME
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

	dnsServer4 *dns.Server // DNS server instance
	dnsServer6 *dns.Server // DNS server instance
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

// Add this to the TrackerModule in gritsd/tracker_module.go

// Add this function to register the list-peers endpoint
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
	httpModule.Mux.HandleFunc("/grits/v1/tracker/list-peers",
		httpModule.requestMiddleware(tm.ListPeersHandler))

	// Request client certs
	httpModule.HTTPServer.TLSConfig.ClientAuth = tls.RequestClientCert
}

// Add this new handler function to the TrackerModule
func (tm *TrackerModule) ListPeersHandler(w http.ResponseWriter, r *http.Request) {
	// Check HTTP method
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get a snapshot of the current peers
	tm.peersMutex.RLock()
	peerList := make([]*PeerInfo, 0, len(tm.peers))
	for _, peer := range tm.peers {
		// Create a copy to avoid race conditions
		peerCopy := *peer
		peerList = append(peerList, &peerCopy)
	}
	tm.peersMutex.RUnlock()

	// Return the peer list as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(peerList); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err),
			http.StatusInternalServerError)
		return
	}
}

func (tm *TrackerModule) RegisterPeerHandler(w http.ResponseWriter, r *http.Request) {
	// Check HTTP method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request payload
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

	// Get peer's IP address
	peerIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		log.Printf("Error extracting peer IP: %v", err)
		http.Error(w, "Failed to determine peer IP address", http.StatusInternalServerError)
		return
	}

	// Handle certificate verification
	if tm.Config.OverrideCertVerification == nil {
		// Only perform verification in non-test mode if TLS is enabled
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			clientCert := r.TLS.PeerCertificates[0]
			log.Printf("Received client certificate for peer %s: Subject=%s",
				request.PeerName, clientCert.Subject.CommonName)

			// Get the peer cert path
			authFilePath := GetCertPath(tm.Server.Config, PeerCert, request.PeerName)
			authFilePath = filepath.Join(authFilePath, "fullchain.pem")
			log.Printf("Looking for peer authorization at: %s", authFilePath)

			if !tm.verifyPeerAuthorization(authFilePath, clientCert.PublicKey) {
				log.Printf("Unauthorized heartbeat attempt for peer %s - cert verification failed",
					request.PeerName)
				http.Error(w, "Unauthorized", http.StatusForbidden)
				return
			}

			log.Printf("Peer %s certificate successfully verified", request.PeerName)
		} else {
			log.Printf("Warning: No TLS certificates found for peer %s", request.PeerName)

			certificateCount := 0
			if r.TLS != nil {
				certificateCount = len(r.TLS.PeerCertificates)
			}
			log.Printf("TLS object present: %v, Certificate count: %d", r.TLS != nil, certificateCount)
			http.Error(w, "Unauthorized", http.StatusForbidden)

			return
		}
	} else {
		log.Printf("Test mode: Overriding certificate verification for peer %s", request.PeerName)
		if !*tm.Config.OverrideCertVerification {
			log.Printf("Unauthorized heartbeat attempt for peer %s (fake)", request.PeerName)
			http.Error(w, "Unauthorized", http.StatusForbidden)
			return
		}
	}

	// Generate the peer's FQDN
	peerFQDN := fmt.Sprintf("%s.%s", request.PeerName, tm.Config.PeerSubdomain)

	// Update peer registry
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
		// Update existing peer
		peer.LastHeartbeat = time.Now()
		peer.IsActive = true

		// [Existing code for updating port and IP]
	}

	tm.peersMutex.Unlock()

	// Enhanced response with registration status
	response := struct {
		Status           string `json:"status"`
		FQDN             string `json:"fqdn"`
		RegistrationType string `json:"registrationType"`
		HeartbeatSec     int    `json:"heartbeatSec"`
	}{
		Status:       "success",
		FQDN:         peerFQDN,
		HeartbeatSec: tm.Config.HeartbeatIntervalSec,
	}

	// Set registration type
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
	dns.HandleFunc(tm.Config.PeerSubdomain+".", tm.handleDNSQuery)

	// Start two servers - one for IPv4 and one for IPv6
	server4 := &dns.Server{
		Addr: "0.0.0.0:5353", // Explicitly IPv4
		Net:  "udp",
	}

	server6 := &dns.Server{
		Addr: "[::]:5353", // IPv6
		Net:  "udp",
	}

	go func() {
		log.Printf("Starting IPv4 DNS server for domain: peer.%s", tm.Config.PeerSubdomain)
		if err := server4.ListenAndServe(); err != nil {
			log.Printf("DNS server error: %v", err)
		}
	}()

	go func() {
		log.Printf("Starting IPv6 DNS server for domain: peer.%s", tm.Config.PeerSubdomain)
		if err := server4.ListenAndServe(); err != nil {
			log.Printf("DNS server error: %v", err)
		}
	}()

	// Store them for cleanup
	tm.dnsServer4 = server4
	tm.dnsServer6 = server6

	return nil
}

// handleDNSQuery responds to DNS queries for peer subdomains
func (tm *TrackerModule) handleDNSQuery(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	for _, q := range r.Question {
		log.Printf("Got DNS query for %s", q.Name)

		// Extract peer name from the query (e.g., "peer1.peer.example.com.")
		domainParts := strings.Split(q.Name, ".")
		if len(domainParts) < 4 {
			continue // Invalid domain format
		}

		// Convert to lowercase for case-insensitive matching
		peerName := strings.ToLower(domainParts[0])

		tm.peersMutex.RLock()
		peer, exists := tm.peers[peerName]
		active := exists && peer.IsActive
		ipAddress := ""
		if exists {
			ipAddress = peer.IPAddress
		}
		tm.peersMutex.RUnlock()

		log.Printf("  basic checks passed (ip '%s' type %d)", ipAddress, q.Qtype)

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

	log.Printf("Sending DNS response with %d answer records", len(m.Answer))
	responseErr := w.WriteMsg(m)
	if responseErr != nil {
		log.Printf("Error sending DNS response: %v", responseErr)
	}
}

// Make sure the peer's authorization matches the one we were configured with
func (tm *TrackerModule) verifyPeerAuthorization(authFilePath string, peerPublicKey interface{}) bool {
	// Check if authorization file exists
	_, err := os.Stat(authFilePath)
	if os.IsNotExist(err) {
		log.Printf("Peer authorization file not found: %s", authFilePath)
		return false
	}

	// Read the authorized certificate file
	authorizedCertBytes, err := os.ReadFile(authFilePath)
	if err != nil {
		log.Printf("Failed to read peer authorization file: %s, error: %v", authFilePath, err)
		return false
	}

	// Parse the certificate
	authorizedCert, err := x509.ParseCertificate(authorizedCertBytes)
	if err != nil {
		// Try parsing as PEM format if binary DER format fails
		block, _ := pem.Decode(authorizedCertBytes)
		if block == nil || block.Type != "CERTIFICATE" {
			log.Printf("Failed to parse peer certificate from %s: not a valid certificate format", authFilePath)
			return false
		}

		authorizedCert, err = x509.ParseCertificate(block.Bytes)
		if err != nil {
			log.Printf("Failed to parse peer certificate from %s: %v", authFilePath, err)
			return false
		}
	}

	// Extract public key from authorized certificate
	authorizedPublicKey := authorizedCert.PublicKey

	// Compare the public keys
	switch authorizedKey := authorizedPublicKey.(type) {
	case *rsa.PublicKey:
		if peerKey, ok := peerPublicKey.(*rsa.PublicKey); ok {
			// Compare RSA public keys
			return peerKey.N.Cmp(authorizedKey.N) == 0 && peerKey.E == authorizedKey.E
		}
	case *ecdsa.PublicKey:
		if peerKey, ok := peerPublicKey.(*ecdsa.PublicKey); ok {
			// Compare ECDSA public keys
			return peerKey.X.Cmp(authorizedKey.X) == 0 &&
				peerKey.Y.Cmp(authorizedKey.Y) == 0 &&
				peerKey.Curve.Params().Name == authorizedKey.Curve.Params().Name
		}
	case ed25519.PublicKey:
		if peerKey, ok := peerPublicKey.(ed25519.PublicKey); ok {
			// Compare Ed25519 public keys
			return bytes.Equal(peerKey, authorizedKey)
		}
	default:
		log.Printf("Unsupported public key type: %T", authorizedPublicKey)
		return false
	}

	log.Printf("Public key types don't match. Authorized: %T, Provided: %T",
		authorizedPublicKey, peerPublicKey)
	return false
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

	// Stop the DNS servers if running
	if tm.dnsServer4 != nil {
		tm.dnsServer4.Shutdown()
	}
	if tm.dnsServer6 != nil {
		tm.dnsServer6.Shutdown()
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
