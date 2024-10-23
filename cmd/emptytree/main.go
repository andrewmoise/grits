package main

import (
	"crypto/sha256"
	"fmt"
	"log"

	"github.com/mr-tron/base58"
	"github.com/multiformats/go-multihash"
)

func main() {
	// Input data
	data := "{}"

	// Compute SHA-256 hash
	hasher := sha256.New()
	hasher.Write([]byte(data))
	hashBytes := hasher.Sum(nil)

	// Create a multihash
	encodedMH, err := multihash.Encode(hashBytes, multihash.SHA2_256)
	if err != nil {
		log.Fatalf("multihash encode failed: %v", err)
	}

	// Base58 Encode the multihash
	base58Encoded := base58.Encode(encodedMH)
	fmt.Printf("Base58 Encoded CID: %s\n", base58Encoded)
}
