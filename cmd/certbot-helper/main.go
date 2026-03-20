package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
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
	certDir := flag.String("cert-dir", "", "Directory to store certificates in (must be absolute)")
	daemonUser := flag.String("daemon-user", "", "User the daemon runs as (certs will be readable by this user)")
	flag.Parse()

	// Validate inputs
	if *domain == "" || *email == "" || *certDir == "" || *daemonUser == "" {
		log.Fatalf("--domain, --email, --cert-dir, and --daemon-user are all required")
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

	// cert-dir must be absolute
	cleanDir := filepath.Clean(*certDir)
	if !filepath.IsAbs(cleanDir) {
		log.Fatalf("cert-dir must be an absolute path")
	}

	// Look up the daemon user
	targetUser, err := user.Lookup(*daemonUser)
	if err != nil {
		log.Fatalf("failed to look up daemon user %s: %v", *daemonUser, err)
	}
	uid, err := strconv.Atoi(targetUser.Uid)
	if err != nil {
		log.Fatalf("invalid UID for user %s: %v", *daemonUser, err)
	}
	gid, err := strconv.Atoi(targetUser.Gid)
	if err != nil {
		log.Fatalf("invalid GID for user %s: %v", *daemonUser, err)
	}

	// Set up certbot directory structure
	domainDir := filepath.Join(cleanDir, *domain)
	configDir := filepath.Join(domainDir, "config")
	workDir := filepath.Join(domainDir, "work")
	logsDir := filepath.Join(domainDir, "logs")

	// Run certbot
	args := []string{
		"certonly",
		"--standalone",
		"--email", *email,
		"--agree-tos",
		"--no-eff-email",
		"--no-autorenew",
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

	// Verify the cert files exist
	certPath := filepath.Join(configDir, "live", *domain, "fullchain.pem")
	keyPath := filepath.Join(configDir, "live", *domain, "privkey.pem")

	if _, err := os.Stat(certPath); err != nil {
		log.Fatalf("certbot succeeded but cert not found at %s", certPath)
	}
	if _, err := os.Stat(keyPath); err != nil {
		log.Fatalf("certbot succeeded but key not found at %s", keyPath)
	}

	// Fix up permissions on the cert directory tree so the daemon user can read
	// the certs but no other user can. We walk the whole tree under cleanDir to
	// handle the symlinks certbot creates in live/.
	if err := chownAndChmod(cleanDir, uid, gid); err != nil {
		log.Fatalf("failed to set permissions on cert directory: %v", err)
	}

	fmt.Printf("Successfully acquired certificate for %s\n", *domain)
	fmt.Printf("  cert: %s\n", certPath)
	fmt.Printf("  key:  %s\n", keyPath)
}

func chownAndChmod(root string, uid, gid int) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Use Lstat to detect symlinks since Walk follows them by default
		linfo, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("lstat %s: %v", path, err)
		}

		// Always lchown, whether symlink or not
		if err := os.Lchown(path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %v", path, err)
		}

		// Don't chmod symlinks themselves (no-op on Linux anyway)
		if linfo.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		var mode os.FileMode
		if linfo.IsDir() {
			mode = 0700
		} else {
			mode = 0600
		}

		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("chmod %s: %v", path, err)
		}

		return nil
	})
}
