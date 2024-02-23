package server

import (
	"bytes"
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/tebeka/selenium"
	"github.com/tebeka/selenium/chrome"
)

// CustomLogWriter captures log output for inspection
type CustomLogWriter struct {
	buf bytes.Buffer
}

func (w *CustomLogWriter) Write(p []byte) (n int, err error) {
	return w.buf.Write(p)
}

// NewCustomLogWriter creates a new instance of CustomLogWriter
func NewCustomLogWriter() *CustomLogWriter {
	return &CustomLogWriter{}
}

// GetLogs returns the captured log messages as a string
func (w *CustomLogWriter) GetLogs() string {
	return w.buf.String()
}

// SetupLogging redirects log output to a CustomLogWriter
func SetupLogging() *CustomLogWriter {
	logWriter := NewCustomLogWriter()
	log.SetOutput(logWriter)
	return logWriter
}

// ResetLogging restores the default log output
func ResetLogging() {
	log.SetOutput(os.Stderr)
}

func TestServerInteraction(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		log.Printf("Error getting working directory: %v", err)
	} else {
		log.Printf("Current working directory: %s", dir)
	}

	// Create temporary directory for temp server data
	tempDir2, err := os.MkdirTemp("", "grits_server2")
	if err != nil {
		log.Fatalf("Failed to create temp directory for server 2: %v", err)
	}
	defer os.RemoveAll(tempDir2)

	// Create content directories for temp server
	contentDir2 := filepath.Join(tempDir2, "content")
	if err := os.Mkdir(contentDir2, 0755); err != nil {
		log.Fatalf("Failed to create content directory for server 2: %v", err)
	}

	projectDir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		log.Fatalf("Failed to get project directory: %v", err)
	}

	// Create a file in both content directories
	filePath1 := filepath.Join(filepath.Join(projectDir, "content"), "testfile.txt")
	if err := os.WriteFile(filePath1, []byte("hello"), 0644); err != nil {
		log.Fatalf("Failed to write test file for server 1: %v", err)
	}

	filePath2 := filepath.Join(contentDir2, "testfile.txt")
	if err := os.WriteFile(filePath2, []byte("hello"), 0644); err != nil {
		log.Fatalf("Failed to write test file for server 2: %v", err)
	}

	// Initialize and start the first server
	config1 := grits.NewConfig()
	config1.ServerDir = projectDir
	config1.ThisPort = 1787

	srv1, err := NewServer(config1)
	if err != nil {
		log.Fatalf("Failed to initialize server 1: %v", err)
	}
	srv1.Start()

	dir, err = os.Getwd()
	if err != nil {
		log.Printf("Error getting working directory: %v", err)
	} else {
		log.Printf("Current working directory: %s", dir)
	}

	// Initialize and start the second server
	config2 := grits.NewConfig()
	config2.ServerDir = tempDir2
	config2.ThisPort = 1788
	config2.DirMirrors = append(config2.DirMirrors, grits.DirMirrorConfig{
		Type:          "DirToBlob",
		SourceDir:     contentDir2,
		CacheLinksDir: filepath.Join(tempDir2, "cache_links"),
	})

	srv2, err := NewServer(config2)
	if err != nil {
		log.Fatalf("Failed to initialize server 2: %v", err)
	}
	srv2.Start()

	// Allow some time for servers to start
	time.Sleep(1 * time.Second)

	_, err = os.ReadFile(filepath.Join(tempDir2, "cache_links", "testfile.txt"))
	if err != nil {
		log.Fatalf("Failed to read file address from server 1: %v", err)
	}

	service, err := selenium.NewChromeDriverService("/usr/local/bin/chromedriver", 4444)
	if err != nil {
		t.Fatalf("Error starting ChromeDriver service: %v", err)
	}
	defer service.Stop()

	caps := selenium.Capabilities{"browserName": "chrome"}
	chromeCaps := chrome.Capabilities{
		Args: []string{
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-gpu",
			"--headless",
			"--window-size=800x600",
			"--incognito",
		},
	}
	caps.AddChrome(chromeCaps)

	wd, err := selenium.NewRemote(caps, "")
	if err != nil {
		t.Fatalf("Failed to open session: %v", err)
	}
	defer wd.Quit()

	logWriter := SetupLogging()
	defer ResetLogging()

	// Replace the URL with the path to your index.html served by your Go server
	if err := wd.Get("http://localhost:1787/grits/v1/client/index.html"); err != nil {
		t.Fatalf("Failed to load page: %v", err)
	}

	// Wait for the link to be clickable
	time.Sleep(1 * time.Second) // Consider using explicit waits instead of sleep

	// Find and click the link
	link, err := wd.FindElement(selenium.ByLinkText, "Click for link")
	if err != nil {
		t.Fatalf("Failed to find link: %v", err)
	}
	if err := link.Click(); err != nil {
		t.Fatalf("Failed to click link: %v", err)
	}

	time.Sleep(1 * time.Second)

	logs := logWriter.GetLogs()

	fmt.Printf("Logs: %s\n", logs)

	expectedLog := regexp.MustCompile(`1788`)
	if !expectedLog.MatchString(logs) {
		t.Errorf("Expected log pattern not found in logs")
	}
}
