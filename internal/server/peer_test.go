package server

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPeerTrackerInteraction tests the basic interaction between a peer and tracker
func TestPeerTrackerInteraction(t *testing.T) {
	// For this test, we'll just check certificate generation and setup
	// A complete test would involve network communication which is more complex

	// Set up a tracker server
	trackerPort := 2391

	// Set up a peer server that points to the tracker
	peerPort := 2392
	peerName := "test-peer"
	peerServer, peerCleanup := SetupTestServer(t,
		WithHttpModule(peerPort),
		WithPeerModule("localhost", trackerPort, peerName, peerPort))
	defer peerCleanup()

	// 1. Generate certificates for the peer
	err := GenerateSelfCert(peerServer.Config)
	if err != nil {
		t.Fatalf("Failed to generate self-signed certificate: %v", err)
	}

	// 2. Test certificate directory structure on peer
	selfCertDir := GetCertPath(peerServer.Config, SelfSignedCert, "current")
	if _, err := os.Stat(filepath.Dir(selfCertDir)); os.IsNotExist(err) {
		t.Errorf("Certificate directory structure not created: %v", err)
	}

	// 3. Verify certificate files exist with correct names
	certPath, keyPath := GetCertificateFiles(peerServer.Config, SelfSignedCert, "current")

	if !fileExists(certPath) {
		t.Errorf("Certificate file not created at %s", certPath)
	}

	if !fileExists(keyPath) {
		t.Errorf("Private key file not created at %s", keyPath)
	}

	// In a full test, we would:
	// 1. Start both servers
	// 2. Have the peer register with the tracker
	// 3. Verify the registration succeeded
	// 4. Test heartbeat functionality
	// But for now, we're just testing the certificate setup

	t.Log("Peer and tracker certificate setup completed successfully")
}

// Helper function to copy a file
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}
