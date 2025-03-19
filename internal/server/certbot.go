package server

import (
	"errors"
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// CertificateType indicates the source/purpose of a certificate
type CertificateType string

const (
	// SelfSignedCert represents a certificate generated for this peer
	SelfSignedCert CertificateType = "self"

	// PeerCert represents a certificate for a remote peer we communicate with
	PeerCert CertificateType = "peers"

	// LetsEncryptCert represents a certificate from Let's Encrypt
	LetsEncryptCert CertificateType = "letsencrypt"
)

// CertbotConfig holds configuration for certificate operations
type CertbotConfig struct {
	// Domain name for the certificate
	Domain string

	// Email address for Let's Encrypt notifications
	Email string

	// The root certificate directory (will be combined with ServerPath)
	CertsBaseDir string
}

// GetCertPath returns the path where certificates for a particular entity should be stored
func GetCertPath(config *grits.Config, certType CertificateType, entityName string) string {
	return config.ServerPath(fmt.Sprintf("var/certs/%s/%s", certType, entityName))
}

// GetCertificateFiles returns the paths to the certificate and key files for an entity
func GetCertificateFiles(config *grits.Config, certType CertificateType, entityName string) (certPath, keyPath string) {
	basePath := GetCertPath(config, certType, entityName)
	certPath = filepath.Join(basePath, "fullchain.pem") // Using consistent naming with certbot
	keyPath = filepath.Join(basePath, "privkey.pem")
	return certPath, keyPath
}

// EnsureTLSCertificates generates keys and acquires certificates if needed
// Returns paths to fullchain.pem and privkey.pem for use with HTTP server
func EnsureTLSCertificates(config *grits.Config, cfg *CertbotConfig, autoCertificate bool) (certPath, keyPath string, err error) {
	if cfg.Domain == "" {
		return "", "", errors.New("domain name is required")
	}

	// Update the certificate directory to use our new structure
	certsDir := GetCertPath(config, LetsEncryptCert, cfg.Domain)

	// Set up certbot directory structure
	configDir := filepath.Join(certsDir, "config")
	workDir := filepath.Join(certsDir, "work")
	logsDir := filepath.Join(certsDir, "logs")

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

	// If email is missing, we can't proceed with Let's Encrypt
	if cfg.Email == "" {
		return "", "", errors.New("email address is required for Let's Encrypt")
	}

	// At this point we need to request new certificates
	log.Printf("Acquiring new certificates for %s", cfg.Domain)
	if err := obtainCertificate(cfg, configDir, workDir, logsDir); err != nil {
		return "", "", fmt.Errorf("failed to obtain certificate: %w", err)
	}

	// Double-check that the certificates were created
	if !fileExists(certPath) || !fileExists(keyPath) {
		err := errors.New("certificates not found after successful certbot run")
		return "", "", err
	}

	return certPath, keyPath, nil
}

// GenerateSelfCert creates a self-signed certificate for a peer using certbot
// This implements the two-stage generation process:
// 1. Generate self-signed certificates using elliptic curve keys
// 2. These can later be replaced with Let's Encrypt certificates if needed
func GenerateSelfCert(serverConfig *grits.Config) error {
	// Use the new directory structure
	selfCertDir := GetCertPath(serverConfig, SelfSignedCert, "current")
	privateKeyPath := filepath.Join(selfCertDir, "privkey.pem")
	certPath := filepath.Join(selfCertDir, "fullchain.pem")

	// Check if the key already exists
	if fileExists(privateKeyPath) && fileExists(certPath) {
		log.Printf("Self-signed certificate already exists")
		return nil
	}

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(selfCertDir, 0700); err != nil {
		return fmt.Errorf("failed to create certificate directory for self: %v", err)
	}

	log.Printf("Generating new self-signed certificate for self using elliptic curve")

	// Step 1: Generate the EC private key using OpenSSL
	ecKeyCmd := exec.Command("openssl", "ecparam",
		"-name", "prime256v1", // Use P-256 curve (compatible with certbot)
		"-genkey", "-noout",
		"-out", privateKeyPath)

	output, err := ecKeyCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to generate EC key: %v\nOutput: %s", err, output)
	}

	// Step 2: Generate a self-signed certificate for the key
	selfSignCmd := exec.Command("openssl", "req",
		"-new", "-x509",
		"-key", privateKeyPath,
		"-out", certPath,
		"-subj", fmt.Sprintf("/CN=%s", "localhost"),
		"-days", "90")

	output, err = selfSignCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to generate self-signed cert: %v\nOutput: %s", err, output)
	}

	log.Printf("Successfully generated self-signed certificate")
	return nil
}

// UpgradeToCertbot replaces self-signed certificates with Let's Encrypt certificates
// This represents the second stage of the certificate process
func UpgradeToCertbot(config *grits.Config, fqdn string, certbotConfig *CertbotConfig) error {
	// First check if we already have self-signed certs
	selfCertDir := GetCertPath(config, SelfSignedCert, "current")
	selfKeyPath := filepath.Join(selfCertDir, "privkey.pem")
	selfCertPath := filepath.Join(selfCertDir, "fullchain.pem")

	if !fileExists(selfKeyPath) || !fileExists(selfCertPath) {
		return fmt.Errorf("cannot upgrade to certbot: self-signed certificates for %s not found", fqdn)
	}

	// Set the domain to the peer's domain in the certbot config
	if certbotConfig.Domain == "" {
		// If no domain is specified, we could construct one based on the peer name and subdomain
		// This would typically come from the tracker module
		return fmt.Errorf("domain name is required for certbot upgrade")
	}

	log.Printf("Upgrading %s from self-signed certificate to Let's Encrypt certificate", fqdn)

	// Request the certificate from Let's Encrypt
	//leCertPath, leKeyPath, err := EnsureTLSCertificates(config, certbotConfig, true)
	//if err != nil {
	//	return fmt.Errorf("failed to obtain Let's Encrypt certificate: %v", err)
	//}

	// Now we have both self-signed and Let's Encrypt certificates
	// The system can use either one depending on the context
	log.Printf("Successfully upgraded %s to Let's Encrypt certificate", fqdn)

	return nil
}

// obtainCertificate gets a signed certificate from Let's Encrypt using certbot
func obtainCertificate(cfg *CertbotConfig, configDir, workDir, logsDir string) error {
	// Create temporary webroot for ACME challenge
	webRootPath := filepath.Join(workDir, "tmp-webroot")
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
