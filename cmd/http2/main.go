package main

import (
	"fmt"
	"grits/internal/grits"
	"grits/internal/proxy"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	config := proxy.NewConfig("default_root_host", 1234)
	config.StorageDirectory = filepath.Join(os.TempDir(), "blobstore_test")
	config.StorageSize = 100 * 1024 * 1024    // 100MB for testing
	config.StorageFreeSize = 80 * 1024 * 1024 // 80MB for testing

	err := os.MkdirAll(config.StorageDirectory, 0755)
	if err != nil {
		panic("Failed to create storage directory")
	}

	contentDir := "content/"

	bs := proxy.NewBlobStore(config)
	ns, err := proxy.NewNameStore(bs)
	if err != nil {
		fmt.Printf("Error creating NameStore: %v\n", err)
		return
	}

	err = filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			var file *grits.CachedFile

			file, err = bs.AddLocalFile(path)
			if err != nil {
				return err
			}

			fmt.Printf("Mapped %s to %s\n", path, file.Address.String())
			ns.MapNameToBlob(info.Name(), file.Address)
		}
		return nil
	})
	if err != nil {
		fmt.Printf("Error walking through content directory: %v\n", err)
		return
	}

	//http.HandleFunc("/grits/v1/auth", handleLogin)
	http.HandleFunc("/grits/v1/sha256/", handleSHA256(bs))
	http.HandleFunc("/grits/v1/name/", handleName(bs, ns))

	http.ListenAndServe(":1787", nil)
}

func handleSHA256(bs *proxy.BlobStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
			return
		}

		// Extract file address from URL, expecting format "{hash}:{size}"
		addrStr := strings.TrimPrefix(r.URL.Path, "/grits/v1/sha256/")
		if addrStr == "" {
			http.Error(w, "Missing file address", http.StatusBadRequest)
			return
		}

		// Split the address into hash and size
		parts := strings.SplitN(addrStr, ":", 2)
		if len(parts) != 2 {
			http.Error(w, "Invalid file address format", http.StatusBadRequest)
			return
		}
		hash, sizeStr := parts[0], parts[1]

		// Convert size from string to uint64
		size, err := strconv.ParseUint(sizeStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid file size", http.StatusBadRequest)
			return
		}

		// Create FileAddr from extracted hash and size
		fileAddr := &grits.FileAddr{Hash: hash, Size: size}

		// Try to read the file from the blob store using the full address
		cachedFile, err := bs.ReadFile(fileAddr)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer bs.Release(cachedFile)

		// Update LastTouched and touch the file on disk
		cachedFile.LastTouched = time.Now()
		os.Chtimes(cachedFile.Path, time.Now(), time.Now())

		// Serve the file
		http.ServeFile(w, r, cachedFile.Path)
	}
}

func handleName(bs *proxy.BlobStore, ns *proxy.NameStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
			return
		}

		// Extract name from URL
		name := strings.TrimPrefix(r.URL.Path, "/grits/v1/name/")
		if name == "" {
			http.Error(w, "Missing name", http.StatusBadRequest)
			return
		}

		addr, exists := ns.ResolveName(name)
		if !exists {
			http.Error(w, "Name not found", http.StatusNotFound)
			return
		}

		cachedFile, err := bs.ReadFile(addr)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer bs.Release(cachedFile)

		// Update LastTouched and touch the file on disk
		cachedFile.LastTouched = time.Now()
		os.Chtimes(cachedFile.Path, time.Now(), time.Now())

		http.ServeFile(w, r, cachedFile.Path)
	}
}
