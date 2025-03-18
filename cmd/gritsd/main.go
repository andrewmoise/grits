package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"grits/internal/grits"
	"grits/internal/server"
)

func main() {
	var workingDir string
	flag.StringVar(&workingDir, "d", ".", "Working directory for server")
	flag.Parse()

	configFile := filepath.Join(workingDir, "grits.cfg") // Configuration file path

	// Check if the configuration file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Printf("Configuration file does not exist: %s\n", configFile)
		os.Exit(1) // Exit with an error code
	}

	// Load the configuration
	config := grits.NewConfig(workingDir)
	if err := config.LoadFromFile(configFile); err != nil {
		log.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1) // Exit with an error code
	}
	config.ServerDir = workingDir // Ensure server directory is set

	// Ensure the server directory exists
	err := os.MkdirAll(config.ServerDir, 0755)
	if err != nil {
		panic(fmt.Sprintf("Failed to create server directory: %v", err))
	}

	srv, err := server.NewServer(config)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize server: %v", err))
	}

	// Setup signal handling for a graceful shutdown
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	<-signals

	log.Println("Shutting down server...")
	srv.Stop()
}
