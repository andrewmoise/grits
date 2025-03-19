package server

import (
	"grits/internal/grits"
	"log"
	"os"
	"testing"
)

// TestModuleInitializer is a function that initializes a specific module for a server.
type TestModuleInitializer func(*testing.T, *Server)

// SetupTestServer sets up a test server with optional modules.
func SetupTestServer(t *testing.T, initializers ...TestModuleInitializer) (*Server, func()) {
	tempDir, err := os.MkdirTemp("", "grits_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	config := grits.NewConfig(tempDir)

	s, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to initialize server: %v", err)
	}

	// Initialize requested modules
	for _, init := range initializers {
		init(t, s)
	}

	cleanup := func() {
		log.Printf("Doing server cleanup")
		os.RemoveAll(tempDir)
	}

	return s, cleanup
}

// WithHTTPModule is an initializer for adding the HTTP module to the server.
func WithHttpModule(port int) TestModuleInitializer {
	return func(t *testing.T, s *Server) {
		readOnly := false
		httpConfig := &HTTPModuleConfig{
			ThisPort: port,
			ReadOnly: &readOnly,
		}
		httpModule, err := NewHTTPModule(s, httpConfig)
		if err != nil {
			log.Fatalf("can't create http module: %v", err)
		}
		s.AddModule(httpModule)
	}
}

// WithWikiVolume is an initializer for adding a WikiVolume module to the server.
func WithWikiVolume(volumeName string) TestModuleInitializer {
	return func(t *testing.T, s *Server) {
		wikiConfig := &WikiVolumeConfig{
			VolumeName: volumeName,
		}

		wikiVolume, err := NewWikiVolume(wikiConfig, s, false)
		if err != nil {
			t.Fatalf("Can't create %s volume: %v", volumeName, err)
		}
		s.AddModule(wikiVolume)
		s.AddVolume(wikiVolume)
	}
}

// WithTrackerModule is an initializer for adding a tracker module to the server.
// It sets up a server to track peer registrations and provide DNS services.
func WithTrackerModule(subdomain string, heartbeatIntervalSec int) TestModuleInitializer {
	return func(t *testing.T, s *Server) {
		trackerConfig := &TrackerModuleConfig{
			PeerSubdomain:        subdomain,
			HeartbeatIntervalSec: heartbeatIntervalSec,
		}

		trackerModule, err := NewTrackerModule(s, trackerConfig)
		if err != nil {
			t.Fatalf("Failed to create tracker module: %v", err)
		}

		// Create token directory
		tokensDir := s.Config.ServerPath("var/peertokens")
		if err := os.MkdirAll(tokensDir, 0755); err != nil {
			t.Fatalf("Failed to create token directory: %v", err)
		}

		// Create peer certificate directory
		certDir := s.Config.ServerPath("peercerts")
		if err := os.MkdirAll(certDir, 0755); err != nil {
			t.Fatalf("Failed to create peer certificate directory: %v", err)
		}

		s.AddModule(trackerModule)
	}
}

// WithPeerModule is an initializer for adding a peer module to the server.
// It sets up a peer that will register with the specified tracker.
func WithPeerModule(trackerHost string, trackerPort int, peerName string, port int) TestModuleInitializer {
	return func(t *testing.T, s *Server) {
		peerConfig := &PeerModuleConfig{
			TrackerHost: trackerHost,
			TrackerPort: trackerPort,
			PeerName:    peerName,
		}

		peerModule, err := NewPeerModule(s, peerConfig)
		if err != nil {
			t.Fatalf("Failed to create peer module: %v", err)
		}
		s.AddModule(peerModule)
	}
}
