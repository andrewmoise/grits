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
		httpModule := NewHTTPModule(s, httpConfig)
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
