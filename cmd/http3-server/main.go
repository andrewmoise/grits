//go:build http3
// +build http3

package server

import (
	"fmt"
	"log"
	"net/http"

	"grits/internal/grits"

	"github.com/quic-go/quic-go/http3"
)

func main() {
	config := grits.NewConfig()
	config.ServerDir = "."

	// FIXME - need to rewrite
	handler := setupFileServerHandler("")
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

	mux.HandleFunc("/grits/sha256/", func(w http.ResponseWriter, r *http.Request) {
		filePath := fmt.Sprintf("%s/%s", storageDir, r.URL.Path[len("/grits/sha256/"):])
		log.Println("Trying to serve file:", filePath)
		http.ServeFile(w, r, filePath)
	})

	return mux
}
