package gritsd

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/////
// Certificate types (kept for self-signed peer certs)
/////

type CertificateType string

const (
	SelfSignedCert  CertificateType = "self"
	PeerCert        CertificateType = "peers"
	LetsEncryptCert CertificateType = "letsencrypt"
)

func GetCertPath(config *grits.Config, certType CertificateType, entityName string) string {
	return config.ServerPath(fmt.Sprintf("var/certs/%s/%s", certType, entityName))
}

func GetCertificateFiles(config *grits.Config, certType CertificateType, entityName string) (certPath, keyPath string) {
	basePath := GetCertPath(config, certType, entityName)
	if err := os.MkdirAll(basePath, 0700); err != nil {
		log.Printf("Couldn't create cert path! %v", err)
	}
	return filepath.Join(basePath, "fullchain.pem"), filepath.Join(basePath, "privkey.pem")
}

/////
// Cert daemon pipe — opened before privilege drop, used afterward
/////

// certDaemon holds the live connection to the certd child process.
var certDaemon struct {
	mu      sync.Mutex
	stdin   io.WriteCloser
	scanner *bufio.Scanner
}

type certResponse struct {
	Cert  string `json:"cert,omitempty"`
	Key   string `json:"key,omitempty"`
	Error string `json:"error,omitempty"`
}

// PreopenCertDaemon launches the certbot-helper child process while still running as
// root and wires up the pipes. Called from PreopenPrivilegedPorts before
// privilege drop. certdPath is the path to the certbot-helper binary,
// certBaseDir is the absolute path where certs will be stored.
func PreopenCertDaemon(certdPath, certBaseDir, email, daemonUser string) error {
	if _, err := os.Stat(certdPath); err != nil {
		return fmt.Errorf("certbot-helper binary not found at %s", certdPath)
	}
	if !filepath.IsAbs(certBaseDir) {
		return fmt.Errorf("certBaseDir must be absolute: %s", certBaseDir)
	}

	cmd := exec.Command(certdPath,
		"--cert-dir", certBaseDir,
		"--email", email,
	)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start certd: %v", err)
	}

	certDaemon.stdin = stdin
	certDaemon.scanner = bufio.NewScanner(stdout)

	log.Printf("certbot-helper started (pid %d)", cmd.Process.Pid)
	return nil
}

// RequestCert asks the certbot-helper child for a certificate for hostname.
// Returns the cert and key as PEM strings.
// Safe to call from multiple goroutines — the mutex serialises requests.
func RequestCert(hostname string) (certPEM, keyPEM string, err error) {
	certDaemon.mu.Lock()
	defer certDaemon.mu.Unlock()

	if certDaemon.stdin == nil {
		return "", "", fmt.Errorf("cert daemon not initialised")
	}

	// Send the hostname.
	if _, err := fmt.Fprintf(certDaemon.stdin, "%s\n", hostname); err != nil {
		return "", "", fmt.Errorf("failed to write to certbot-helper: %v", err)
	}

	// Read one JSON line back.
	if !certDaemon.scanner.Scan() {
		if err := certDaemon.scanner.Err(); err != nil {
			return "", "", fmt.Errorf("certbot-helper read error: %v", err)
		}
		return "", "", fmt.Errorf("certbot-helper closed unexpectedly")
	}

	var resp certResponse
	if err := json.Unmarshal(certDaemon.scanner.Bytes(), &resp); err != nil {
		return "", "", fmt.Errorf("certbot-helper response parse error: %v", err)
	}
	if resp.Error != "" {
		return "", "", fmt.Errorf("certbot-helper error: %s", resp.Error)
	}
	return resp.Cert, resp.Key, nil
}

/////
// Self-signed cert generation (for peer module, unchanged)
/////

func GenerateSelfCert(serverConfig *grits.Config) error {
	selfCertDir := GetCertPath(serverConfig, SelfSignedCert, "current")
	privateKeyPath := filepath.Join(selfCertDir, "privkey.pem")
	certPath := filepath.Join(selfCertDir, "fullchain.pem")

	if fileExists(privateKeyPath) && fileExists(certPath) {
		return nil
	}

	if err := os.MkdirAll(selfCertDir, 0700); err != nil {
		return fmt.Errorf("failed to create certificate directory: %v", err)
	}

	ecKeyCmd := exec.Command("openssl", "ecparam",
		"-name", "prime256v1", "-genkey", "-noout", "-out", privateKeyPath)
	if output, err := ecKeyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to generate EC key: %v\nOutput: %s", err, output)
	}

	selfSignCmd := exec.Command("openssl", "req",
		"-new", "-x509", "-key", privateKeyPath, "-out", certPath,
		"-subj", "/CN=localhost", "-days", "90")
	if output, err := selfSignCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to generate self-signed cert: %v\nOutput: %s", err, output)
	}

	log.Printf("Generated self-signed certificate")
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

/////
// Dynamic TLS certificate management
/////

// getCertificate is the tls.Config.GetCertificate callback.
// It returns a cached cert if available, or acquires one on demand.
func (hm *HTTPModule) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	hostname := hello.ServerName
	if hostname == "" {
		return nil, fmt.Errorf("no SNI hostname provided")
	}

	// Check cache first.
	hm.certMu.RLock()
	cert, ok := hm.certCache[hostname]
	if !ok {
		cert = hm.certCache["*"] // fall back to manual wildcard
	}
	hm.certMu.RUnlock()

	if ok || cert != nil {
		return cert, nil
	}

	if !hm.hostnameHasContent(hostname) {
		return nil, fmt.Errorf("no content configured for %s", hostname)
	}

	// Not cached — acquire if AutoCertificate is on.
	if !hm.Config.AutoCertificate {
		return nil, fmt.Errorf("no certificate available for %s", hostname)
	}

	log.Printf("HTTP: no cert for %s, acquiring on demand", hostname)
	v, err, _ := hm.certGroup.Do(hostname, func() (any, error) {
		return hm.acquireAndCacheCert(hostname)
	})
	if err != nil {
		return nil, err
	}
	return v.(*tls.Certificate), nil
}

func (hm *HTTPModule) acquireAndCacheCert(hostname string) (*tls.Certificate, error) {
	// Safety: only allow FQDNs (must contain a dot) to avoid invalid certbot requests
	if !strings.Contains(hostname, ".") {
		return nil, fmt.Errorf("refusing to acquire certificate for non-FQDN hostname %q", hostname)
	}
	certPEM, keyPEM, err := RequestCert(hostname)
	if err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("failed to parse cert for %s: %v", hostname, err)
	}

	hm.certMu.Lock()
	hm.certCache[hostname] = &cert
	hm.certMu.Unlock()

	hm.startRenewalWatcher(hostname)
	return &cert, nil
}

// initCertsFromContentVolume scans the top-level directories in the content
// volume and acquires certificates for each one that looks like a hostname.
func (hm *HTTPModule) initCertsFromContentVolume() error {
	volume := hm.Server.FindVolumeByName(hm.Config.ContentVolume)
	if volume == nil {
		return fmt.Errorf("content volume %q not found", hm.Config.ContentVolume)
	}

	hostnames, err := hm.listContentHostnames(volume)
	if err != nil {
		return err
	}

	for _, hostname := range hostnames {
		if _, err := hm.acquireAndCacheCert(hostname); err != nil {
			log.Printf("HTTP: cert acquisition failed for %s: %v", hostname, err)
		}
	}
	return nil
}

// WarmUpCert proactively acquires and caches a TLS certificate for the given
// hostname. Blocks until the certificate is ready or an error occurs.
// Safe to call from other modules during their Start() — intended for use by
// the peer module so the cert is ready before the first inbound connection.
func (hm *HTTPModule) WarmUpCert(hostname string) error {
	if !hm.Config.EnableTls || !hm.Config.AutoCertificate {
		return nil // Nothing to do if TLS/autocert is not enabled.
	}

	// Already cached — nothing to do.
	hm.certMu.RLock()
	_, ok := hm.certCache[hostname]
	hm.certMu.RUnlock()
	if ok {
		return nil
	}

	log.Printf("HTTP: warming up certificate for %s", hostname)
	_, err := hm.acquireAndCacheCert(hostname)
	return err
}

// listContentHostnames returns the hostname directories under sites/ in the volume.
func (hm *HTTPModule) listContentHostnames(volume Volume) ([]string, error) {
	node, err := volume.LookupNode("sites", grits.BackendPrincipal)
	if err != nil {
		return nil, fmt.Errorf("failed to look up sites directory: %v", err)
	}
	defer node.Release()

	if node.Metadata().Type != grits.GNodeTypeDirectory {
		return nil, fmt.Errorf("sites directory is not a directory")
	}

	blob, err := node.ExportedBlob()
	if err != nil {
		return nil, fmt.Errorf("failed to read sites directory: %v", err)
	}

	data, err := blob.Read(0, blob.GetSize())
	if err != nil {
		return nil, fmt.Errorf("failed to read sites directory blob: %v", err)
	}

	var dirListing map[string]string
	if err := json.Unmarshal(data, &dirListing); err != nil {
		return nil, fmt.Errorf("failed to decode sites directory: %v", err)
	}

	var hostnames []string
	for name := range dirListing {
		if Validate("hostname", name) && strings.Contains(name, ".") {
			hostnames = append(hostnames, name)
		} else {
			log.Printf("HTTP: skipping non-FQDN hostname %q from sites directory", name)
		}
	}
	return hostnames, nil
}

// startRenewalWatcher starts a background goroutine that renews the cert for
// hostname every 15 days. If the hostname is no longer present in the content
// volume when renewal fires, the goroutine stops and the cert is evicted.
func (hm *HTTPModule) startRenewalWatcher(hostname string) {
	hm.renewalMu.Lock()
	defer hm.renewalMu.Unlock()

	// Cancel any existing watcher for this host.
	if cancel, ok := hm.renewalStopFns[hostname]; ok {
		cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	hm.renewalStopFns[hostname] = cancel

	go func() {
		ticker := time.NewTicker(15 * 24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			case <-hm.stopCh:
				return
			}

			// Check whether this hostname still exists in content volume.
			volume := hm.Server.FindVolumeByName(hm.Config.ContentVolume)
			if volume != nil {
				hostnames, err := hm.listContentHostnames(volume)
				if err == nil {
					found := false
					for _, h := range hostnames {
						if h == hostname {
							found = true
							break
						}
					}
					if !found {
						log.Printf("HTTP: hostname %s no longer in content volume, stopping cert renewal", hostname)
						hm.certMu.Lock()
						delete(hm.certCache, hostname)
						hm.certMu.Unlock()
						hm.renewalMu.Lock()
						delete(hm.renewalStopFns, hostname)
						hm.renewalMu.Unlock()
						return
					}
				}
			}

			log.Printf("HTTP: renewing certificate for %s", hostname)
			if _, err := hm.acquireAndCacheCert(hostname); err != nil {
				log.Printf("HTTP: cert renewal failed for %s: %v", hostname, err)
			} else {
				log.Printf("HTTP: cert renewal succeeded for %s", hostname)
			}
		}
	}()
}
