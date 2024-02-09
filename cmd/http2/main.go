package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"grits/internal/proxy"
	"grits/internal/server"
)

func main() {
	config := proxy.NewConfig("default_root_host", 1234)
	config.VarDirectory = "var"
	config.StorageSize = 100 * 1024 * 1024
	config.StorageFreeSize = 80 * 1024 * 1024

	err := os.MkdirAll(config.VarDirectory, 0755)
	if err != nil {
		panic("Failed to create storage directory")
	}

	srv, err := server.NewServer(config)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize server: %v", err))
	}

	// Create a channel to listen for interrupt or termination signals
	signals := make(chan os.Signal, 1)
	// Notify the channel on SIGINT and SIGTERM
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	// Start the server in a goroutine so that it doesn't block
	go func() {
		if err := srv.Run(); err != nil {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	// Block until we receive a signal
	<-signals

	// Create a context to attempt a graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("Shutting down server...")
	if err := srv.Stop(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	}
}
