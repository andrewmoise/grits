package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"
)

// isValidToken checks if the token only contains safe characters
func isValidToken(token string) bool {
	// ACME challenge tokens are typically alphanumeric with some special chars
	// This is a strict regex pattern to prevent security issues
	pattern := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	return pattern.MatchString(token) && len(token) > 0 && len(token) < 256
}

// isValidResponse checks if the response only contains safe characters
func isValidResponse(response string) bool {
	// ACME challenge responses are typically alphanumeric with some special chars
	// This is a strict regex pattern to prevent security issues
	pattern := regexp.MustCompile(`^[a-zA-Z0-9_\-.]+$`)
	return pattern.MatchString(response) && len(response) > 0 && len(response) < 1024
}

func main() {
	// Parse command line arguments
	token := flag.String("token", "", "ACME challenge token")
	response := flag.String("response", "", "ACME challenge response")
	timeout := flag.Int("timeout", 60, "Timeout in seconds")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	flag.Parse()

	// Set up logging
	if *verbose {
		log.SetOutput(os.Stdout)
	} else {
		log.SetOutput(os.NewFile(0, os.DevNull))
	}

	// Validate inputs to prevent security issues
	if !isValidToken(*token) {
		fmt.Fprintf(os.Stderr, "Error: Invalid token format\n")
		os.Exit(1)
	}
	if !isValidResponse(*response) {
		fmt.Fprintf(os.Stderr, "Error: Invalid response format\n")
		os.Exit(1)
	}

	log.Printf("Setting up ACME challenge server for token: %s", *token)
	log.Printf("Response will be: %s", *response)

	// Create a mux to have more control over routing
	mux := http.NewServeMux()

	// Set up the challenge endpoint
	challengePath := "/.well-known/acme-challenge/" + *token
	mux.HandleFunc(challengePath, func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received request for %s from %s", r.URL.Path, r.RemoteAddr)
		fmt.Fprintf(w, *response)
		log.Printf("Served challenge response")
	})

	// Add a catch-all handler to provide useful errors for other paths
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received unexpected request for %s from %s", r.URL.Path, r.RemoteAddr)
		http.Error(w, "Not Found", http.StatusNotFound)
	})

	// Set up the server
	server := &http.Server{
		Addr:    ":80",
		Handler: mux,
	}

	// Channel to signal completion
	done := make(chan bool, 1)

	// Channel to receive server errors
	serverError := make(chan error, 1)

	// Start the server in a goroutine
	go func() {
		log.Printf("Starting server on port 80 for path %s", challengePath)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
			serverError <- err
		}
	}()

	// Set up automatic timeout
	go func() {
		time.Sleep(time.Duration(*timeout) * time.Second)
		log.Printf("Timeout reached after %d seconds", *timeout)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
		done <- true
	}()

	// Wait for either timeout or server error
	select {
	case <-done:
		log.Printf("Server shut down gracefully")
		os.Exit(0)
	case err := <-serverError:
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
