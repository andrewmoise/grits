package gritsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// WithOriginModule is an initializer for adding an Origin module with allowed mirrors
func WithOriginModule(allowedMirrors []string) TestModuleInitializer {
	return func(t *testing.T, s *Server) {
		config := &OriginModuleConfig{
			AllowedMirrors:      allowedMirrors,
			InactiveTimeoutSecs: 1,
		}

		originModule, err := NewOriginModule(s, config)
		if err != nil {
			t.Fatalf("Failed to create origin module: %v", err)
		}
		s.AddModule(originModule)
	}
}

func TestOriginModule(t *testing.T) {
	// Create an origin server
	originPort := 2387
	// Update allowed mirrors to include protocol and port
	allowedMirrors := []string{"http://test-mirror-1.example.com:80", "http://test-mirror-2.example.com:80"}
	originServer, originCleanup := SetupTestServer(t,
		WithHttpModule(originPort),
		WithOriginModule(allowedMirrors))
	defer originCleanup()

	originServer.Start()
	defer originServer.Stop()

	time.Sleep(time.Millisecond * 100)

	originURL := "localhost:2387"

	// Test 1: Register a mirror - update with protocol and port
	registrationPayload := struct {
		LocalURL string `json:"localURL"`
	}{
		LocalURL: "http://test-mirror-1.example.com:80",
	}

	payloadBytes, _ := json.Marshal(registrationPayload)

	registerResp, err := http.Post(
		fmt.Sprintf("http://%s/grits/v1/origin/register-mirror", originURL),
		"application/json",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		t.Fatalf("Failed to register mirror: %v", err)
	}
	defer registerResp.Body.Close()

	if registerResp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(registerResp.Body)
		t.Fatalf("Failed to register mirror, status=%d, body=%s",
			registerResp.StatusCode, string(bodyBytes))
	}

	// Parse registration response to verify heartbeat interval
	var registerResponse struct {
		Status                string `json:"status"`
		HeartbeatIntervalSecs int    `json:"heartbeatIntervalSecs"`
	}

	if err := json.NewDecoder(registerResp.Body).Decode(&registerResponse); err != nil {
		t.Fatalf("Failed to decode registration response: %v", err)
	}

	// Verify the heartbeat interval is what we expect
	if registerResponse.HeartbeatIntervalSecs != 1 {
		t.Errorf("Expected heartbeat interval of 1 second, got %d",
			registerResponse.HeartbeatIntervalSecs)
	}

	// Test 2: List mirrors to verify registration worked
	listResp, err := http.Get(
		fmt.Sprintf("http://%s/grits/v1/origin/list-mirrors", originURL),
	)
	if err != nil {
		t.Fatalf("Failed to list mirrors: %v", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to list mirrors, status=%d", listResp.StatusCode)
	}

	var mirrorResponses []struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&mirrorResponses); err != nil {
		t.Fatalf("Failed to decode mirrors list: %v", err)
	}

	if len(mirrorResponses) != 1 {
		t.Fatalf("Expected 1 active mirror, got %d", len(mirrorResponses))
	}

	// Extract hostname from URL for comparison
	mirrorURL := mirrorResponses[0].URL
	// Check if URL contains the expected hostname
	if !strings.Contains(mirrorURL, "test-mirror-1.example.com") {
		t.Errorf("Expected mirror URL to contain 'test-mirror-1.example.com', got '%s'", mirrorURL)
	}

	// Test 3: Verify mirror activity checking
	// Wait for the mirror to be marked inactive (timeout + a little buffer)
	time.Sleep(time.Duration(3) * time.Second)

	// Check that the mirror is now inactive
	listResp2, err := http.Get(
		fmt.Sprintf("http://%s/grits/v1/origin/list-mirrors", originURL),
	)
	if err != nil {
		t.Fatalf("Failed to list mirrors after timeout: %v", err)
	}
	defer listResp2.Body.Close()

	var mirrorsAfterTimeout []*MirrorInfo
	if err := json.NewDecoder(listResp2.Body).Decode(&mirrorsAfterTimeout); err != nil {
		t.Fatalf("Failed to decode mirrors list after timeout: %v", err)
	}

	if len(mirrorsAfterTimeout) != 0 {
		t.Errorf("Expected 0 active mirrors after timeout, got %d",
			len(mirrorsAfterTimeout))
	}

	// Test 4: Test rejected registration for disallowed mirror
	badRegistrationPayload := struct {
		LocalURL string `json:"localURL"`
	}{
		LocalURL: "http://unauthorized-mirror.example.com:80",
	}

	badPayloadBytes, _ := json.Marshal(badRegistrationPayload)
	badRegisterResp, err := http.Post(
		fmt.Sprintf("http://%s/grits/v1/origin/register-mirror", originURL),
		"application/json",
		bytes.NewBuffer(badPayloadBytes),
	)
	if err != nil {
		t.Fatalf("Failed to send unauthorized registration: %v", err)
	}
	defer badRegisterResp.Body.Close()

	// Should be rejected with 403 Forbidden
	if badRegisterResp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for unauthorized mirror, got %d",
			badRegisterResp.StatusCode)
	}
}
