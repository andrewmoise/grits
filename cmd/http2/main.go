package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

	srv.Start()

	<-signals

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("Shutting down server...")
	if err := srv.Stop(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v\n", err)
	}
}
