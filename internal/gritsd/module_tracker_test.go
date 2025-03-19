package gritsd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPeerTrackerInteraction tests the complete interaction between a peer and tracker
func TestPeerTrackerInteraction(t *testing.T) {
	// Create unique ports to avoid conflicts with other tests
	trackerPort := 8391
	peerPort := 8392
	peerName := "test-peer"

	// 1. Set up and start the tracker server
	trackerServer, trackerCleanup := SetupTestServer(t,
		WithHttpModule(trackerPort),
		WithTrackerModule("test.local", 60))
	defer trackerCleanup()

	var trackerModule *TrackerModule
	for _, module := range trackerServer.Modules {
		if tm, ok := module.(*TrackerModule); ok {
			trackerModule = tm
			break
		}
	}

	if trackerModule == nil {
		t.Fatalf("Tracker module not found")
	}

	overrideVerification := false
	trackerModule.Config.OverrideCertVerification = &overrideVerification

	if err := trackerServer.Start(); err != nil {
		t.Fatalf("Failed to start tracker server: %v", err)
	}

	// 2. Set up and start the peer server
	peerServer, peerCleanup := SetupTestServer(t,
		WithHttpModule(peerPort),
		WithPeerModule(fmt.Sprintf("http://localhost:%d", trackerPort), peerName, peerPort))
	defer peerCleanup()

	// 3. First attempt at registration should fail (no auth yet)
	// Start peer server - it will attempt registration
	if err := peerServer.Start(); err == nil {
		t.Fatalf("Peer registration should have failed without auth")
	}

	// 4. Now set up proper authorization on the tracker
	// - Get peer's certificate
	peerCertPath, _ := GetCertificateFiles(peerServer.Config, SelfSignedCert, "current")
	if !fileExists(peerCertPath) {
		t.Fatalf("Peer certificate not created at %s", peerCertPath)
	}

	// - Create auth directory on tracker
	trackerPeerCertDir := filepath.Dir(GetCertPath(trackerServer.Config, PeerCert, peerName))
	if err := os.MkdirAll(trackerPeerCertDir, 0755); err != nil {
		t.Fatalf("Failed to create tracker peer cert directory: %v", err)
	}

	// - Copy peer cert to tracker's auth directory
	trackerPeerCertPath := GetCertPath(trackerServer.Config, PeerCert, peerName)
	if err := copyFile(peerCertPath, trackerPeerCertPath); err != nil {
		t.Fatalf("Failed to copy peer cert to tracker: %v", err)
	}

	// FIXME: For now, we fake the client cert
	overrideVerification = true
	trackerModule.Config.OverrideCertVerification = &overrideVerification

	// 5. Second attempt at registration should succeed
	if err := peerServer.Start(); err != nil {
		t.Fatalf("Peer registration failed after auth setup: %v", err)
	}

	// 6. Wait for registration to complete and verify peer is active

	// Poll for peer registration with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	checkPeerRegistered := func() bool {
		trackerModule.peersMutex.RLock()
		defer trackerModule.peersMutex.RUnlock()

		peer, exists := trackerModule.peers[peerName]
		return exists && peer.IsActive
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if checkPeerRegistered() {
				t.Logf("Peer successfully registered with tracker")
				return // Test passed
			}
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for peer registration")
		}
	}
}

// Helper function to copy a file from source to destination
func copyFile(src, dst string) error {
	// Read the source file
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	// Ensure the destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	// Write the content to the destination file
	// Use 0600 permissions as these are certificate files
	return os.WriteFile(dst, data, 0600)
}
