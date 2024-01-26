package main

import (
	"fmt"
	"log"
	"net/http"

	"grits/internal/proxy"

	"github.com/quic-go/quic-go/http3"
)

func main() {
	config := proxy.NewConfig("127.0.0.1", 1787)
	config.StorageDirectory = "content"

	handler := setupFileServerHandler(config.StorageDirectory)
	server := http3.Server{
		Addr:    fmt.Sprintf("%s:%d", config.ThisHost, config.ThisPort),
		Handler: handler,
	}

	log.Printf("Starting HTTP/3 server on %s:%d", config.ThisHost, config.ThisPort)
	err := server.ListenAndServeTLS("certs/cert.pem", "certs/privkey.pem") // Replace with your actual cert paths
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func setupFileServerHandler(storageDir string) http.Handler {
	mux := http.NewServeMux()

	// Serve files from the specified directory
	//fileServer := http.FileServer(http.Dir(storageDir))

	//mux.HandleFunc("/grits/sha256/", func(w http.ResponseWriter, r *http.Request) {
	// Strip the leading "/grits/sha256/" part of the URL path and serve the file
	//http.StripPrefix("/grits/sha256/", fileServer).ServeHTTP(w, r)
	//})

	mux.HandleFunc("/grits/sha256/", func(w http.ResponseWriter, r *http.Request) {
		filePath := fmt.Sprintf("%s/%s", storageDir, r.URL.Path[len("/grits/sha256/"):])
		log.Println("Trying to serve file:", filePath)
		http.ServeFile(w, r, filePath)
	})

	return mux
}
