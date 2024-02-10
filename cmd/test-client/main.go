package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"grits/internal/proxy"
	"grits/internal/server"
)

func main() {
	// Create two temporary directories for server data
	tempDir1, err := os.MkdirTemp("", "grits_server1")
	if err != nil {
		log.Fatalf("Failed to create temp directory for server 1: %v", err)
	}
	defer os.RemoveAll(tempDir1)

	tempDir2, err := os.MkdirTemp("", "grits_server2")
	if err != nil {
		log.Fatalf("Failed to create temp directory for server 2: %v", err)
	}
	defer os.RemoveAll(tempDir2)

	// Create content directories in both temporary directories
	contentDir1 := filepath.Join(tempDir1, "content")
	if err := os.Mkdir(contentDir1, 0755); err != nil {
		log.Fatalf("Failed to create content directory for server 1: %v", err)
	}

	contentDir2 := filepath.Join(tempDir2, "content")
	if err := os.Mkdir(contentDir2, 0755); err != nil {
		log.Fatalf("Failed to create content directory for server 2: %v", err)
	}

	// Create a file in both content directories
	filePath1 := filepath.Join(contentDir1, "testfile.txt")
	if err := os.WriteFile(filePath1, []byte("hello"), 0644); err != nil {
		log.Fatalf("Failed to write test file for server 1: %v", err)
	}

	filePath2 := filepath.Join(contentDir2, "testfile.txt")
	if err := os.WriteFile(filePath2, []byte("hello"), 0644); err != nil {
		log.Fatalf("Failed to write test file for server 2: %v", err)
	}

	// Initialize and start the first server
	config1 := proxy.NewConfig()
	config1.ServerDir = tempDir1
	config1.ThisPort = 1787
	config1.DirMirrors = append(config1.DirMirrors, proxy.DirMirrorConfig{
		SourceDir:     contentDir1,
		CacheLinksDir: filepath.Join(tempDir1, "cache_links"),
	})

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

	fileAddrStr, err := os.ReadFile(filepath.Join(tempDir1, "cache_links", "testfile.txt"))
	if err != nil {
		log.Fatalf("Failed to read file address from server 1: %v", err)
	}

	log.Printf("File address from server 1: %s\n", fileAddrStr)

	fmt.Println("Servers started. Proceed with manual testing.")

	time.Sleep(360 * time.Second)
}
