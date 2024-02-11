package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"grits/internal/proxy"
	"grits/internal/server"
)

func main() {
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

	// Create a file in both content directories
	filePath1 := filepath.Join("content", "testfile.txt")
	if err := os.WriteFile(filePath1, []byte("hello"), 0644); err != nil {
		log.Fatalf("Failed to write test file for server 1: %v", err)
	}

	filePath2 := filepath.Join(contentDir2, "testfile.txt")
	if err := os.WriteFile(filePath2, []byte("hello"), 0644); err != nil {
		log.Fatalf("Failed to write test file for server 2: %v", err)
	}

	// Initialize and start the first server
	config1 := proxy.NewConfig()
	config1.ServerDir = "."
	config1.ThisPort = 1787

	srv1, err := server.NewServer(config1)
	if err != nil {
		log.Fatalf("Failed to initialize server 1: %v", err)
	}
	go func() {
		if err := srv1.Run(); err != nil {
			log.Printf("Server 1 error: %v\n", err)
		}
	}()

	// Initialize and start the second server
	config2 := proxy.NewConfig()
	config2.ServerDir = tempDir2
	config2.ThisPort = 1788
	config2.DirMirrors = append(config2.DirMirrors, proxy.DirMirrorConfig{
		SourceDir:     contentDir2,
		CacheLinksDir: filepath.Join(tempDir2, "cache_links"),
	})

	srv2, err := server.NewServer(config2)
	if err != nil {
		log.Fatalf("Failed to initialize server 2: %v", err)
	}
	go func() {
		if err := srv2.Run(); err != nil {
			log.Printf("Server 2 error: %v\n", err)
		}
	}()

	// Allow some time for servers to start
	time.Sleep(2 * time.Second)

	fileAddrStr, err := os.ReadFile(filepath.Join(tempDir2, "cache_links", "testfile.txt"))
	if err != nil {
		log.Fatalf("Failed to read file address from server 1: %v", err)
	}

	log.Printf("File address from server 1: %s\n", fileAddrStr)
	log.Printf("http://localhost:1787/grits/v1/sha256/%s\n", fileAddrStr)
	log.Println("")
	log.Println("Servers started. Proceed with manual testing.")

	// Setup channel to listen for SIGINT and SIGTERM signals
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	// Block until a signal is received
	<-signals

	log.Println("Signal received, shutting down servers...")

	// Create a context with a timeout for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt to gracefully shut down server1
	if err := srv1.Stop(ctx); err != nil {
		log.Printf("Error shutting down server 1: %v\n", err)
	}

	// Attempt to gracefully shut down server2
	if err := srv2.Stop(ctx); err != nil {
		log.Printf("Error shutting down server 2: %v\n", err)
	}

	log.Println("Servers shut down successfully.")
}
