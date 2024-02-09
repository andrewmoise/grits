package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"grits/internal/proxy"
	"grits/internal/server"
)

func main() {
	var configFile string
	flag.StringVar(&configFile, "c", "./grits.cfg", "Path to configuration file")
	flag.Parse()

	// Check if the configuration file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Printf("Configuration file does not exist: %s\n", configFile)
		os.Exit(1) // Exit with an error code
	}

	// Load the configuration
	config := proxy.NewConfig()
	if err := config.LoadFromFile(configFile); err != nil {
		log.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1) // Exit with an error code
	}

	// Proceed to create server directory and server initialization
	err := os.MkdirAll(config.VarPath("."), 0755)
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

	go func() {
		if err := srv.Run(); err != nil {
			log.Printf("Server error: %v\n", err)
		}
	}()

	<-signals

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("Shutting down server...")
	if err := srv.Stop(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v\n", err)
	}
}
