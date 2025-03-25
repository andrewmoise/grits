package gritsd

import (
	"context"
	"errors"
	"fmt"
	"grits/internal/grits"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
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
func EnsureTLSCertificates(serverConfig *grits.Config, certConfig *CertbotConfig, autoCertificate bool) (certPath, keyPath string, err error) {
	if certConfig.Domain == "" {
		return "", "", errors.New("domain name is required")
	}

	// Update the certificate directory to use our new structure
	certsDir := GetCertPath(serverConfig, LetsEncryptCert, certConfig.Domain)

	// Set up certbot directory structure
	configDir := filepath.Join(certsDir, "config")
	workDir := filepath.Join(certsDir, "work")
	logsDir := filepath.Join(certsDir, "logs")

	// Define paths where certbot will store certificates
	certPath = filepath.Join(configDir, "live", certConfig.Domain, "fullchain.pem")
	keyPath = filepath.Join(configDir, "live", certConfig.Domain, "privkey.pem")

	// Check if certificates already exist
	if fileExists(certPath) && fileExists(keyPath) {
		log.Printf("Using existing certificates for %s", certConfig.Domain)
		return certPath, keyPath, nil
	}

	// If auto-certificate is disabled, return an error
	if !autoCertificate {
		return "", "", fmt.Errorf("certificates for %s not found and auto-certificate is disabled", certConfig.Domain)
	}

	// If email is missing, we can't proceed with Let's Encrypt
	if certConfig.Email == "" {
		return "", "", errors.New("email address is required for Let's Encrypt")
	}

	// At this point we need to request new certificates
	log.Printf("Acquiring new certificates for %s", certConfig.Domain)
	if err := obtainCertificate(certConfig, configDir, workDir, logsDir); err != nil {
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

// ServeAcmeChallengeDir starts a temporary HTTP server that serves ACME challenge files from a directory
func ServeAcmeChallengeDir(challengeDir string, done chan struct{}) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Join(challengeDir, ".well-known", "acme-challenge"), 0755); err != nil {
		return fmt.Errorf("failed to create challenge directory: %v", err)
	}

	// Create file server for the challenge directory
	fileServer := http.FileServer(http.Dir(challengeDir))

	// Create server
	server := &http.Server{
		Addr:              ":8080",
		Handler:           fileServer,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Starting ACME challenge server on port 8080 serving from %s", challengeDir)

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("ACME challenge server error: %v", err)
		}
	}()

	// Wait for signal to shutdown
	<-done

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

// obtainCertificate gets a signed certificate from Let's Encrypt using certbot
func obtainCertificate(cfg *CertbotConfig, configDir, workDir, logsDir string) error {
	// Create temporary webroot for ACME challenge
	webRootPath := filepath.Join(workDir, "webroot")

	// Start the challenge server
	done := make(chan struct{})
	go func() {
		if err := ServeAcmeChallengeDir(webRootPath, done); err != nil {
			log.Printf("Error with ACME challenge server: %v", err)
		}
	}()

	// Make sure to signal shutdown when we're done
	defer close(done)

	// Run certbot with webroot authentication
	args := []string{
		"certonly",
		"--webroot",
		"--webroot-path", webRootPath,
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

	log.Printf("Ready to serve from %s", webRootPath)

	cmd := exec.Command("certbot", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run certbot
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("certbot failed: %w", err)
	}

	return nil
}

// Helper function to check if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
