package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CertbotConfig holds configuration for certificate operations
type CertbotConfig struct {
	// Domain name for the certificate
	Domain string

	// Email address for Let's Encrypt notifications
	Email string

	// Directory to store certificates
	CertsDir string
}

// EnsureTLSCertificates generates keys and acquires certificates if needed
// Returns paths to fullchain.pem and privkey.pem for use with HTTP server
func EnsureTLSCertificates(cfg *CertbotConfig, autoCertificate bool) (certPath, keyPath string, err error) {
	if cfg.Domain == "" {
		return "", "", errors.New("domain name is required")
	}

	// Set up certbot directory structure
	configDir := filepath.Join(cfg.CertsDir, "config")
	workDir := filepath.Join(cfg.CertsDir, "work")
	logsDir := filepath.Join(cfg.CertsDir, "logs")
	failuresDir := filepath.Join(cfg.CertsDir, "failures")

	// Create necessary directories
	for _, dir := range []string{configDir, workDir, logsDir, failuresDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", "", fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Define paths where certbot will store certificates
	certPath = filepath.Join(configDir, "live", cfg.Domain, "fullchain.pem")
	keyPath = filepath.Join(configDir, "live", cfg.Domain, "privkey.pem")

	// Check if certificates already exist
	if fileExists(certPath) && fileExists(keyPath) {
		log.Printf("Using existing certificates for %s", cfg.Domain)
		return certPath, keyPath, nil
	}

	// If auto-certificate is disabled, return an error
	if !autoCertificate {
		return "", "", fmt.Errorf("certificates for %s not found and auto-certificate is disabled", cfg.Domain)
	}

	// Check for recent failures before attempting
	failurePath := filepath.Join(failuresDir, cfg.Domain+".json")
	if fileExists(failurePath) {
		var failure CertbotFailure
		failureData, err := os.ReadFile(failurePath)
		if err == nil && json.Unmarshal(failureData, &failure) == nil {
			return "", "", fmt.Errorf("previous certificate request for %s failed: %s. Fix error, delete certbot/failures/%s.json, try again",
				failure.Domain, failure.ErrorMsg, failure.Domain)
		}
	}

	// If email is missing, we can't proceed with Let's Encrypt
	if cfg.Email == "" {
		return "", "", errors.New("email address is required for Let's Encrypt")
	}

	// At this point we need to request new certificates
	log.Printf("Acquiring new certificates for %s", cfg.Domain)
	if err := obtainCertificate(cfg, configDir, workDir, logsDir); err != nil {
		// Record this failure
		saveCertbotFailure(failuresDir, cfg.Domain, err)
		return "", "", fmt.Errorf("failed to obtain certificate: %w", err)
	}

	// Double-check that the certificates were created
	if !fileExists(certPath) || !fileExists(keyPath) {
		err := errors.New("certificates not found after successful certbot run")
		saveCertbotFailure(failuresDir, cfg.Domain, err)
		return "", "", err
	}

	return certPath, keyPath, nil
}

// CertbotFailure represents a record of a failed certificate request
type CertbotFailure struct {
	Domain      string    `json:"domain"`
	LastAttempt time.Time `json:"lastAttempt"`
	ErrorMsg    string    `json:"errorMsg"`
	RetryAfter  time.Time `json:"retryAfter"`
}

// saveCertbotFailure records information about a failed certificate request
func saveCertbotFailure(failuresDir, domain string, err error) error {
	failure := CertbotFailure{
		Domain:      domain,
		LastAttempt: time.Now(),
		ErrorMsg:    err.Error(),
		RetryAfter:  time.Now().Add(24 * time.Hour), // Don't retry for 24 hours
	}

	data, err := json.Marshal(failure)
	if err != nil {
		return err
	}

	failurePath := filepath.Join(failuresDir, domain+".json")
	return os.WriteFile(failurePath, data, 0600)
}

// obtainCertificate gets a signed certificate from Let's Encrypt using certbot
func obtainCertificate(cfg *CertbotConfig, configDir, workDir, logsDir string) error {
	// Create temporary webroot for ACME challenge
	webRootPath := filepath.Join(cfg.CertsDir, "tmp-webroot")
	acmePath := filepath.Join(webRootPath, ".well-known", "acme-challenge")
	if err := os.MkdirAll(acmePath, 0755); err != nil {
		return fmt.Errorf("failed to create webroot directory: %w", err)
	}
	defer os.RemoveAll(webRootPath) // Clean up when done

	// Create a custom HTTP client for certbot that uses the ACME helper
	acmeHelperPath := filepath.Join(os.Getenv("PATH"), "acme-challenge-helper")
	if !fileExists(acmeHelperPath) {
		// Check in common locations
		altPaths := []string{
			"/usr/local/bin/acme-challenge-helper",
			"/usr/bin/acme-challenge-helper",
			"./acme-challenge-helper", // Current directory
		}

		for _, path := range altPaths {
			if fileExists(path) {
				var err error
				acmeHelperPath, err = filepath.Abs(path)
				if err != nil {
					return fmt.Errorf("cannot canonicalize %s", path)
				}
				break
			}
		}
	}

	// Path to the hook script we'll create
	hookScript := filepath.Join(workDir, "acme-http-hook.sh")

	// Create the hook script that will call our helper
	err := createAcmeHttpHook(hookScript, acmeHelperPath)
	if err != nil {
		return fmt.Errorf("failed to create ACME HTTP hook: %w", err)
	}

	// Make it executable
	if err := os.Chmod(hookScript, 0755); err != nil {
		return fmt.Errorf("failed to make hook script executable: %w", err)
	}

	// Prepare certbot command with manual hooks
	args := []string{
		"certonly",
		"--manual",
		"--preferred-challenges", "http",
		"--manual-auth-hook", hookScript,
		"--email", cfg.Email,
		"--agree-tos",
		"--no-eff-email",
		"--domain", cfg.Domain,
		"--key-type", "ecdsa",
		"--config-dir", configDir,
		"--work-dir", workDir,
		"--logs-dir", logsDir,
		"--non-interactive",
	}

	cmd := exec.Command("certbot", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Pass environment variables to the certbot process
	cmd.Env = os.Environ()

	// Run certbot
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("certbot failed: %w", err)
	}

	return nil
}

// createAcmeHttpHook creates a shell script that calls our acme-challenge-helper
func createAcmeHttpHook(scriptPath, helperPath string) error {
	script := `#!/bin/sh
# Auto-generated ACME HTTP challenge hook

# Check if the helper exists
if [ ! -x "` + helperPath + `" ]; then
  echo "Error: ACME challenge helper not found at ` + helperPath + `" >&2
  echo "Please install the acme-challenge-helper binary with setuid permissions." >&2
  exit 1
fi

echo "Starting ACME challenge helper..."

# Use nohup to make the process ignore SIGHUP when the parent exits
# Also redirect output to /dev/null
nohup "` + helperPath + `" --token "$CERTBOT_TOKEN" --response "$CERTBOT_VALIDATION" --timeout 120 > /dev/null 2>&1 &

# Sleep briefly to let the server start
sleep 1

echo "Auth hook completed, helper running as daemon"
`
	return os.WriteFile(scriptPath, []byte(script), 0644)
}

// Helper function to check if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
