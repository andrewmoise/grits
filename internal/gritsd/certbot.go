package gritsd

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

/////
// Certificate types and paths
/////

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

// GetCertPath returns the path where certificates for a particular entity should be stored
func GetCertPath(config *grits.Config, certType CertificateType, entityName string) string {
	return config.ServerPath(fmt.Sprintf("var/certs/%s/%s", certType, entityName))
}

// GetCertificateFiles returns the paths to the certificate and key files for an entity
func GetCertificateFiles(config *grits.Config, certType CertificateType, entityName string) (certPath, keyPath string) {
	basePath := GetCertPath(config, certType, entityName)
	err := os.MkdirAll(basePath, 0700)
	if err != nil {
		log.Printf("Couldn't create cert path! %v", err)
	}

	certPath = filepath.Join(basePath, "fullchain.pem")
	keyPath = filepath.Join(basePath, "privkey.pem")
	return certPath, keyPath
}

// GetLetsEncryptCertFiles returns the paths to the Let's Encrypt cert and key for a domain.
// These are nested inside the certbot config directory structure.
func GetLetsEncryptCertFiles(config *grits.Config, domain string) (certPath, keyPath string) {
	certsDir := GetCertPath(config, LetsEncryptCert, domain)
	configDir := filepath.Join(certsDir, "config")
	certPath = filepath.Join(configDir, "live", domain, "fullchain.pem")
	keyPath = filepath.Join(configDir, "live", domain, "privkey.pem")
	return certPath, keyPath
}

// CertExpiresWithin returns true if the certificate at certPath will expire within d,
// or if it cannot be read/parsed.
func CertExpiresWithin(certPath string, d time.Duration) bool {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return true
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return true
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}

	return time.Until(cert.NotAfter) < d
}

// EnsureCertificate checks whether a valid Let's Encrypt certificate exists for domain,
// and runs the certbot helper to acquire one if not (or if expiring within 7 days).
// Returns the cert and key paths on success.
func EnsureCertificate(serverConfig *grits.Config, domain, email string) (certPath, keyPath string, err error) {
	certPath, keyPath = GetLetsEncryptCertFiles(serverConfig, domain)

	needsCert := !fileExists(certPath) || !fileExists(keyPath) || CertExpiresWithin(certPath, 7*24*time.Hour)
	if !needsCert {
		if grits.DebugHttp {
			log.Printf("Certificate for %s is valid and not expiring soon", domain)
		}
		return certPath, keyPath, nil
	}

	log.Printf("Acquiring/renewing certificate for %s", domain)
	if err := runCertbotHelper(serverConfig, domain, email); err != nil {
		return "", "", fmt.Errorf("certbot helper failed for %s: %v", domain, err)
	}

	if !fileExists(certPath) || !fileExists(keyPath) {
		return "", "", fmt.Errorf("certbot helper succeeded but cert files not found for %s", domain)
	}

	return certPath, keyPath, nil
}

func runCertbotHelper(serverConfig *grits.Config, domain, email string) error {
	helperPath := serverConfig.ServerPath("bin/certbot-helper")

	if _, err := os.Stat(helperPath); err != nil {
		return fmt.Errorf("certbot helper not found at %s", helperPath)
	}

	certDir := GetCertPath(serverConfig, LetsEncryptCert, domain)

	// Helper requires an absolute path
	absCertDir, err := filepath.Abs(certDir)
	if err != nil {
		return fmt.Errorf("failed to resolve cert dir to absolute path: %v", err)
	}

	cmd := exec.Command(helperPath,
		"--domain", domain,
		"--email", email,
		"--cert-dir", absCertDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// StartCertRenewalWatcher starts a goroutine that watches for certificate expiry
// and renews via the certbot helper when within 7 days of expiry.
// On failure it retries once per day. stopCh signals shutdown.
func StartCertRenewalWatcher(serverConfig *grits.Config, domain, email string, stopCh <-chan struct{}) {
	go func() {
		for {
			certPath, _ := GetLetsEncryptCertFiles(serverConfig, domain)

			// Figure out how long to sleep until 7 days before expiry
			sleepDur := certRenewalSleep(certPath)

			select {
			case <-time.After(sleepDur):
				// Try to renew
				log.Printf("Attempting certificate renewal for %s", domain)
				if err := runCertbotHelper(serverConfig, domain, email); err != nil {
					log.Printf("Certificate renewal failed for %s: %v — will retry in 24 hours", domain, err)
					select {
					case <-time.After(24 * time.Hour):
					case <-stopCh:
						return
					}
				} else {
					log.Printf("Certificate renewed successfully for %s", domain)
				}
			case <-stopCh:
				return
			}
		}
	}()
}

// certRenewalSleep returns how long to sleep before attempting renewal.
// We target waking up when 7 days remain before expiry.
// If the cert can't be read or is already within 7 days, returns 0.
func certRenewalSleep(certPath string) time.Duration {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return 0
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return 0
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return 0
	}

	// Wake up when 7 days remain
	renewAt := cert.NotAfter.Add(-7 * 24 * time.Hour)
	sleepDur := time.Until(renewAt)
	if sleepDur < 0 {
		return 0
	}
	return sleepDur
}

// GenerateSelfCert creates a self-signed certificate for peer authentication using
// elliptic curve keys. Does nothing if the certificate already exists.
func GenerateSelfCert(serverConfig *grits.Config) error {
	selfCertDir := GetCertPath(serverConfig, SelfSignedCert, "current")
	privateKeyPath := filepath.Join(selfCertDir, "privkey.pem")
	certPath := filepath.Join(selfCertDir, "fullchain.pem")

	if fileExists(privateKeyPath) && fileExists(certPath) {
		if grits.DebugHttp {
			log.Printf("Self-signed certificate already exists")
		}
		return nil
	}

	if err := os.MkdirAll(selfCertDir, 0700); err != nil {
		return fmt.Errorf("failed to create certificate directory: %v", err)
	}

	if grits.DebugHttp {
		log.Printf("Generating new self-signed certificate using elliptic curve")
	}

	ecKeyCmd := exec.Command("openssl", "ecparam",
		"-name", "prime256v1",
		"-genkey", "-noout",
		"-out", privateKeyPath)

	if output, err := ecKeyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to generate EC key: %v\nOutput: %s", err, output)
	}

	selfSignCmd := exec.Command("openssl", "req",
		"-new", "-x509",
		"-key", privateKeyPath,
		"-out", certPath,
		"-subj", "/CN=localhost",
		"-days", "90")

	if output, err := selfSignCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to generate self-signed cert: %v\nOutput: %s", err, output)
	}

	log.Printf("Successfully generated self-signed certificate")
	return nil
}

// resolveCertPaths returns absolute paths to the cert and key files from explicit config.
func resolveCertPaths(config *HTTPModuleConfig) (certPath, keyPath string, err error) {
	if config.CertPath == "" || config.KeyPath == "" {
		return "", "", fmt.Errorf("certPath and keyPath are required when enableTLS is true and autoCertificate is false")
	}

	certPath = config.CertPath
	if !filepath.IsAbs(certPath) {
		certPath, err = filepath.Abs(certPath)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve certPath: %v", err)
		}
	}

	keyPath = config.KeyPath
	if !filepath.IsAbs(keyPath) {
		keyPath, err = filepath.Abs(keyPath)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve keyPath: %v", err)
		}
	}

	if !fileExists(certPath) || !fileExists(keyPath) {
		return "", "", fmt.Errorf("certificate files not found at %s and %s", certPath, keyPath)
	}

	return certPath, keyPath, nil
}

// fileExists returns true if the path exists on disk.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
