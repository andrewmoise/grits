package gritsd

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OriginModuleConfig defines configuration for the origin server functionality
type OriginModuleConfig struct {
	// List of allowed mirror domains that can register
	AllowedMirrors []string `json:"allowedMirrors,omitempty"`

	// How long (in seconds) a mirror can go without a registration before being marked inactive
	InactiveTimeoutSecs int `json:"inactiveTimeoutSecs,omitempty"`
}

// MirrorInfo tracks information about a registered mirror
type MirrorInfo struct {
	Hostname         string    `json:"hostname"`
	LastRegistration time.Time `json:"lastRegistration"`
	IsActive         bool      `json:"isActive"`
}

// OriginModule implements Origin server functionality
type OriginModule struct {
	Config *OriginModuleConfig
	Server *Server

	mirrors      map[string]*MirrorInfo // key is hostname
	mirrorsMutex sync.RWMutex           // protect concurrent access to mirrors map

	stopCh    chan struct{} // Signal channel for stopping goroutines
	stoppedCh chan struct{} // Channel to signal when goroutines are stopped
	running   bool          // Track if the module is running
}

func NewOriginModule(server *Server, config *OriginModuleConfig) (*OriginModule, error) {
	// Set defaults if not specified
	if config.InactiveTimeoutSecs == 0 {
		config.InactiveTimeoutSecs = 300 // 5 minutes default
	}

	om := &OriginModule{
		Config:    config,
		Server:    server,
		mirrors:   make(map[string]*MirrorInfo),
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
	return om, nil
}

func (om *OriginModule) GetModuleName() string {
	return "origin"
}

func (m *OriginModule) GetConfig() any {
	return m.Config
}

// Modified ListMirrorsHandler to use fully qualified URLs
func (om *OriginModule) ListMirrorsHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("List mirrors handler")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	activeMirrors := om.GetActiveMirrors()

	// Define a client-facing mirror format
	type MirrorResponse struct {
		URL string `json:"url"` // Complete URL with protocol, hostname, and port
	}

	// Build the response in the client-friendly format
	clientMirrors := make([]MirrorResponse, 0, len(activeMirrors))

	for _, mirror := range activeMirrors {
		// Ensure hostname is a fully qualified URL
		url := mirror.Hostname

		// Add protocol if missing
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://" + url
		}

		// Ensure there's a port
		if !strings.Contains(url[8:], ":") { // Skip protocol part when checking for port
			log.Printf("Warning: Mirror %s has no port specified", url)
			// Don't add this mirror to the response as it's incomplete
			continue
		}

		clientMirror := MirrorResponse{
			URL: url,
		}

		clientMirrors = append(clientMirrors, clientMirror)
		log.Printf("Added mirror to response: %s", url)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clientMirrors)
}

// Modified RegisterMirror to expect and validate fully qualified URLs
func (om *OriginModule) RegisterMirror(localURL string) error {
	om.mirrorsMutex.Lock()
	defer om.mirrorsMutex.Unlock()

	if !om.IsMirrorAllowed(localURL) {
		return fmt.Errorf("mirror %s not in allowed list", localURL)
	}

	// Add or update mirror with normalized hostname
	om.mirrors[localURL] = &MirrorInfo{
		Hostname:         localURL,
		LastRegistration: time.Now(),
		IsActive:         true,
	}

	log.Printf("Mirror registered: %s", localURL)
	return nil
}

// IsMirrorAllowed checks if a given URL is in the allowed mirrors list
func (om *OriginModule) IsMirrorAllowed(localURL string) bool {
	// If the AllowedMirrors list is empty, don't allow any
	if len(om.Config.AllowedMirrors) == 0 {
		return false
	}

	// Extract the hostname part from the URL for comparison
	hostnameOnly := localURL

	// Remove protocol if present
	if strings.HasPrefix(hostnameOnly, "http://") {
		hostnameOnly = hostnameOnly[7:]
	} else if strings.HasPrefix(hostnameOnly, "https://") {
		hostnameOnly = hostnameOnly[8:]
	}

	// Remove port if present
	if colonIndex := strings.Index(hostnameOnly, ":"); colonIndex != -1 {
		hostnameOnly = hostnameOnly[:colonIndex]
	}

	// First check exact matches (for fully qualified URLs in the allowed list)
	for _, allowed := range om.Config.AllowedMirrors {
		// Check if the exact URL is allowed
		if allowed == localURL {
			return true
		}

		// If allowed has protocol, strip it and compare hostnames
		allowedHost := allowed
		if strings.HasPrefix(allowed, "http://") {
			allowedHost = allowed[7:]
		} else if strings.HasPrefix(allowed, "https://") {
			allowedHost = allowed[8:]
		}

		// Remove port from allowed if present
		if colonIndex := strings.Index(allowedHost, ":"); colonIndex != -1 {
			allowedHost = allowedHost[:colonIndex]
		}

		// Compare just the hostnames
		if allowedHost == hostnameOnly {
			return true
		}
	}

	// If we got here, no match was found
	return false
}

// UnregisterMirror removes a mirror
func (om *OriginModule) UnregisterMirror(hostname string) {
	om.mirrorsMutex.Lock()
	defer om.mirrorsMutex.Unlock()

	delete(om.mirrors, hostname)
	log.Printf("Mirror unregistered: %s", hostname)
}

// GetActiveMirrors returns a list of active mirrors
func (om *OriginModule) GetActiveMirrors() []*MirrorInfo {
	om.mirrorsMutex.RLock()
	defer om.mirrorsMutex.RUnlock()

	var activeMirrors []*MirrorInfo
	for _, mirror := range om.mirrors {
		if mirror.IsActive {
			activeMirrors = append(activeMirrors, mirror)
		}
	}

	return activeMirrors
}

// checkMirrorActivity runs as a goroutine to mark mirrors as inactive if they haven't sent a Registration
func (om *OriginModule) checkMirrorActivity() {
	ticker := time.NewTicker(time.Second * time.Duration(om.Config.InactiveTimeoutSecs) / 2)
	defer ticker.Stop()
	defer close(om.stoppedCh) // Signal that we've stopped

	for {
		select {
		case <-ticker.C:
			om.updateMirrorActivity()
		case <-om.stopCh:
			return // Exit when stop signal received
		}
	}
}

func (om *OriginModule) updateMirrorActivity() {
	om.mirrorsMutex.Lock()
	defer om.mirrorsMutex.Unlock()

	inactiveThreshold := time.Now().Add(-time.Duration(om.Config.InactiveTimeoutSecs) * time.Second)

	for hostname, mirror := range om.mirrors {
		if mirror.LastRegistration.Before(inactiveThreshold) {
			if mirror.IsActive {
				log.Printf("Mirror marked inactive due to missing registration: %s", hostname)
				mirror.IsActive = false
			}
		}
	}
}

// RegisterMirrorHandler handles /grits/v1/origin/register-mirror
func (om *OriginModule) RegisterMirrorHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the URL from the request
	var request struct {
		LocalURL string `json:"localURL"` // Changed from Hostname
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Register the mirror
	if err := om.RegisterMirror(request.LocalURL); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	// Respond with the heartbeat interval
	response := struct {
		Status                string `json:"status"`
		HeartbeatIntervalSecs int    `json:"heartbeatIntervalSecs"`
	}{
		Status:                "success",
		HeartbeatIntervalSecs: om.Config.InactiveTimeoutSecs,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// UnregisterMirrorHandler handles /grits/v1/origin/unregister-mirror
func (om *OriginModule) UnregisterMirrorHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		Hostname string `json:"hostname"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	om.UnregisterMirror(request.Hostname)

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Mirror unregistered successfully")
}

func (om *OriginModule) Start() error {
	// Find the HTTP module
	httpModules := om.Server.GetModules("http")
	if len(httpModules) == 0 {
		log.Printf("Warning: No HTTP module found, origin endpoints won't be available")
		return nil
	}

	// Register handlers with the first HTTP module found
	httpModule := httpModules[0].(*HTTPModule)

	httpModule.Mux.HandleFunc("/grits/v1/origin/register-mirror", httpModule.requestMiddleware(om.RegisterMirrorHandler))
	httpModule.Mux.HandleFunc("/grits/v1/origin/unregister-mirror", httpModule.requestMiddleware(om.UnregisterMirrorHandler))
	httpModule.Mux.HandleFunc("/grits/v1/origin/list-mirrors", httpModule.requestMiddleware(om.ListMirrorsHandler))

	// Start the background mirror activity checker
	om.running = true
	go om.checkMirrorActivity()

	return nil
}

func (om *OriginModule) Stop() error {
	if !om.running {
		return nil // Already stopped
	}

	log.Printf("Stopping Origin module")

	// Send stop signal to goroutine
	close(om.stopCh)

	// Wait for confirmation that the goroutine has exited
	<-om.stoppedCh

	om.running = false
	log.Printf("Origin module stopped")

	return nil
}
