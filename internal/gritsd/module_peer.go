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

type PeerModuleConfig struct {
	TrackerUrl string `json:"trackerUrl"`
	PeerName   string `json:"peerName"`
}

type PeerModule struct {
	Config *PeerModuleConfig
	Server *Server

	registered   bool
	fqdn         string
	heartbeatSec int

	heartbeatTicker *time.Ticker
	stopCh          chan struct{}
	stoppedCh       chan struct{}
	running         bool
}

func (*PeerModule) GetModuleName() string { return "peer" }
func (*PeerModule) GetDependencies() []*Dependency {
	return []*Dependency{
		{
			ModuleType: "http",
			Type:       DependRequired,
		},
	}
}
func (pm *PeerModule) GetConfig() any { return pm.Config }

func NewPeerModule(server *Server, config *PeerModuleConfig) (*PeerModule, error) {
	if config.TrackerUrl == "" {
		return nil, fmt.Errorf("trackerUrl is required")
	}
	if config.PeerName == "" {
		return nil, fmt.Errorf("peerName is required")
	}
	return &PeerModule{
		Config:    config,
		Server:    server,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}, nil
}

func (pm *PeerModule) Start() error {
	log.Printf("PeerModule: starting, connecting to tracker %s as %s",
		pm.Config.TrackerUrl, pm.Config.PeerName)

	if err := GenerateSelfCert(pm.Server.Config); err != nil {
		return fmt.Errorf("failed to generate self-signed cert: %v", err)
	}

	if err := pm.registerWithTracker(); err != nil {
		return fmt.Errorf("failed to register with tracker: %v", err)
	}

	// Ask the HTTP module to acquire a cert for our FQDN now rather than
	// waiting for the first inbound TLS handshake.
	httpMod := pm.findHTTPModule()
	if httpMod == nil {
		return fmt.Errorf("no HTTP module found — cannot warm up certificate")
	}
	if err := httpMod.WarmUpCert(pm.fqdn); err != nil {
		// Non-fatal: the cert will be acquired on first request instead.
		log.Printf("PeerModule: cert warm-up for %s failed: %v", pm.fqdn, err)
	} else {
		log.Printf("PeerModule: certificate ready for %s", pm.fqdn)
	}

	go pm.heartbeatLoop()
	pm.running = true
	return nil
}

func (pm *PeerModule) Stop() error {
	if !pm.running {
		return nil
	}
	log.Printf("PeerModule: stopping")
	if pm.heartbeatTicker != nil {
		pm.heartbeatTicker.Stop()
	}
	close(pm.stopCh)
	<-pm.stoppedCh
	pm.running = false
	return nil
}

func (pm *PeerModule) heartbeatLoop() {
	defer close(pm.stoppedCh)

	interval := 300 * time.Second
	if pm.heartbeatSec > 0 {
		interval = time.Duration(pm.heartbeatSec*9/10) * time.Second
	}
	pm.heartbeatTicker = time.NewTicker(interval)

	for {
		select {
		case <-pm.heartbeatTicker.C:
			if err := pm.registerWithTracker(); err != nil {
				log.Printf("PeerModule: heartbeat error: %v", err)
			}
		case <-pm.stopCh:
			return
		}
	}
}

func (pm *PeerModule) registerWithTracker() error {
	payload := struct {
		PeerName string `json:"peerName"`
		Port     int    `json:"port"`
	}{
		PeerName: pm.Config.PeerName,
		Port:     -1,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %v", err)
	}

	trackerUrl := pm.Config.TrackerUrl
	if !strings.HasPrefix(trackerUrl, "http") {
		trackerUrl = "https://" + trackerUrl
	}
	url := fmt.Sprintf("%s/grits/v1/tracker/register-peer", trackerUrl)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	certPath, keyPath := GetCertificateFiles(pm.Server.Config, SelfSignedCert, "current")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return fmt.Errorf("self-signed cert not found at %s", certPath)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("failed to load self-signed cert: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send registration: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("tracker rejected registration: status=%d body=%s",
			resp.StatusCode, string(body))
	}

	var response struct {
		Status           string `json:"status"`
		FQDN             string `json:"fqdn"`
		RegistrationType string `json:"registrationType"`
		HeartbeatSec     int    `json:"heartbeatSec"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to parse tracker response: %v", err)
	}

	pm.fqdn = response.FQDN
	pm.registered = true

	if response.HeartbeatSec > 0 && pm.heartbeatSec != response.HeartbeatSec {
		pm.heartbeatSec = response.HeartbeatSec
		if pm.heartbeatTicker != nil {
			pm.heartbeatTicker.Reset(time.Duration(pm.heartbeatSec*9/10) * time.Second)
		}
	}

	log.Printf("PeerModule: registered as %s (%s)", pm.fqdn, response.RegistrationType)
	return nil
}

func (pm *PeerModule) findHTTPModule() *HTTPModule {
	for _, mod := range pm.Server.Modules {
		if h, ok := mod.(*HTTPModule); ok {
			return h
		}
	}
	return nil
}
