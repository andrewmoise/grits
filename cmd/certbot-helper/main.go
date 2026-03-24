// cmd/certd/main.go
//
// Certificate helper daemon. Runs as root, launched by gritsd before privilege
// drop. Reads one hostname per line from stdin, runs certbot, writes one JSON
// line to stdout with the cert and key PEM, then loops.
//
// Protocol:
//   request:  "<hostname>\n"
//   response: "<json>\n"  where json is {"cert":"...","key":"..."} or {"error":"..."}
//
// JSON encoding guarantees no literal newlines in string values, so one line =
// one complete message in both directions.

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var validDomain = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)*$`)

type certResponse struct {
	Cert  string `json:"cert,omitempty"`
	Key   string `json:"key,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	certDir := flag.String("cert-dir", "", "Base directory for certificates (absolute path)")
	email := flag.String("email", "", "Email address for Let's Encrypt registration")
	flag.Parse()

	if *certDir == "" || *email == "" {
		log.Fatalf("certd: --cert-dir and --email are required")
	}
	if !filepath.IsAbs(*certDir) {
		log.Fatalf("certd: --cert-dir must be an absolute path")
	}

	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		hostname := strings.TrimSpace(scanner.Text())
		if hostname == "" {
			continue
		}

		cert, key, err := acquireCert(hostname, *certDir, *email)
		if err != nil {
			enc.Encode(certResponse{Error: err.Error()})
		} else {
			enc.Encode(certResponse{Cert: cert, Key: key})
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("certd: stdin error: %v", err)
	}
}

func acquireCert(hostname, certDir, email string) (certPEM, keyPEM string, err error) {
	if !validDomain.MatchString(hostname) {
		return "", "", fmt.Errorf("invalid hostname: %s", hostname)
	}

	domainDir := filepath.Join(certDir, hostname)
	configDir := filepath.Join(domainDir, "config")
	workDir := filepath.Join(domainDir, "work")
	logsDir := filepath.Join(domainDir, "logs")

	args := []string{
		"certonly",
		"--standalone",
		"--email", email,
		"--agree-tos",
		"--no-eff-email",
		"--no-autorenew",
		"--domain", hostname,
		"--key-type", "ecdsa",
		"--config-dir", configDir,
		"--work-dir", workDir,
		"--logs-dir", logsDir,
		"--non-interactive",
	}

	cmd := exec.Command("certbot", args...)
	cmd.Stdout = os.Stderr // certbot output goes to our stderr, not stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("certbot failed for %s: %v", hostname, err)
	}

	certPath := filepath.Join(configDir, "live", hostname, "fullchain.pem")
	keyPath := filepath.Join(configDir, "live", hostname, "privkey.pem")

	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read cert for %s: %v", hostname, err)
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read key for %s: %v", hostname, err)
	}

	return string(certBytes), string(keyBytes), nil
}
