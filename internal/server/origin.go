package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	// Any additional mirror metadata you want to track
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

// IsMirrorAllowed checks if a mirror hostname is in the allowed list
func (om *OriginModule) IsMirrorAllowed(hostname string) bool {
	for _, allowed := range om.Config.AllowedMirrors {
		if allowed == hostname {
			return true
		}
	}
	return false
}

// RegisterMirror adds a new mirror or updates an existing one
func (om *OriginModule) RegisterMirror(hostname string) error {
	om.mirrorsMutex.Lock()
	defer om.mirrorsMutex.Unlock()

	if !om.IsMirrorAllowed(hostname) {
		return fmt.Errorf("mirror %s not in allowed list", hostname)
	}

	// Add or update mirror
	om.mirrors[hostname] = &MirrorInfo{
		Hostname:         hostname,
		LastRegistration: time.Now(),
		IsActive:         true,
	}

	log.Printf("Mirror registered: %s", hostname)
	return nil
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

	// Get the hostname from the request
	var request struct {
		Hostname string `json:"hostname"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Register the mirror
	if err := om.RegisterMirror(request.Hostname); err != nil {
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

// ListMirrorsHandler handles /grits/v1/origin/list-mirrors
func (om *OriginModule) ListMirrorsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	activeMirrors := om.GetActiveMirrors()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(activeMirrors)
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

	httpModule.Mux.HandleFunc("/grits/v1/origin/register-mirror", httpModule.corsMiddleware(om.RegisterMirrorHandler))
	httpModule.Mux.HandleFunc("/grits/v1/origin/unregister-mirror", httpModule.corsMiddleware(om.UnregisterMirrorHandler))
	httpModule.Mux.HandleFunc("/grits/v1/origin/list-mirrors", httpModule.corsMiddleware(om.ListMirrorsHandler))

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
