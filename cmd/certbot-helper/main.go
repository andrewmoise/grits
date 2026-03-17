package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxDomainLen = 253
	maxEmailLen  = 254
)

var (
	validDomain = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)
	validEmail  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

func main() {
	domain := flag.String("domain", "", "Domain name to acquire certificate for")
	email := flag.String("email", "", "Email address for Let's Encrypt registration")
	certDir := flag.String("cert-dir", "", "Directory to store certificates in")
	flag.Parse()

	// Validate inputs
	if *domain == "" || *email == "" || *certDir == "" {
		log.Fatalf("--domain, --email, and --cert-dir are all required")
	}

	if len(*domain) > maxDomainLen {
		log.Fatalf("domain name too long (max %d characters)", maxDomainLen)
	}
	if len(*email) > maxEmailLen {
		log.Fatalf("email address too long (max %d characters)", maxEmailLen)
	}

	if !validDomain.MatchString(*domain) {
		log.Fatalf("invalid domain name: %s", *domain)
	}
	if !validEmail.MatchString(*email) {
		log.Fatalf("invalid email address: %s", *email)
	}

	// Sanitize certDir - must be absolute and must not contain suspicious elements
	cleanDir := filepath.Clean(*certDir)
	if !filepath.IsAbs(cleanDir) {
		log.Fatalf("cert-dir must be an absolute path")
	}
	if strings.Contains(cleanDir, "..") {
		log.Fatalf("cert-dir must not contain '..'")
	}

	// Set up certbot directory structure
	configDir := filepath.Join(cleanDir, "config")
	workDir := filepath.Join(cleanDir, "work")
	logsDir := filepath.Join(cleanDir, "logs")
	webRootPath := filepath.Join(workDir, "webroot")

	// Create webroot for ACME challenge
	if err := os.MkdirAll(filepath.Join(webRootPath, ".well-known", "acme-challenge"), 0755); err != nil {
		log.Fatalf("failed to create webroot: %v", err)
	}

	// Run certbot
	args := []string{
		"certonly",
		"--standalone",
		"--email", *email,
		"--agree-tos",
		"--no-eff-email",
		"--domain", *domain,
		"--key-type", "ecdsa",
		"--config-dir", configDir,
		"--work-dir", workDir,
		"--logs-dir", logsDir,
		"--non-interactive",
	}

	cmd := exec.Command("certbot", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("certbot failed: %v", err)
	}

	// Verify the cert files exist where we expect them
	certPath := filepath.Join(configDir, "live", *domain, "fullchain.pem")
	keyPath := filepath.Join(configDir, "live", *domain, "privkey.pem")

	if _, err := os.Stat(certPath); err != nil {
		log.Fatalf("certbot succeeded but cert not found at %s", certPath)
	}
	if _, err := os.Stat(keyPath); err != nil {
		log.Fatalf("certbot succeeded but key not found at %s", keyPath)
	}

	fmt.Printf("Successfully acquired certificate for %s\n", *domain)
	fmt.Printf("  cert: %s\n", certPath)
	fmt.Printf("  key:  %s\n", keyPath)
}
