package client

import (
	"crypto/tls"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/quic-go/quic-go/http3"
)

func main() {
	// Replace with your server's address and the path you want to request
	serverAddr := "https://localhost:1787/grits/sha256/test"

	// Create an HTTP client with HTTP/3 support
	client := &http.Client{
		Transport: &http3.RoundTripper{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Skip TLS certificate verification; use only for testing
			},
		},
	}

	// Make a request
	resp, err := client.Get(serverAddr)
	if err != nil {
		log.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Read and print the response body
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}
	log.Printf("Response: %s", body)
}
