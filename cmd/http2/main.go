package main

import (
	"fmt"
	"grits/internal/grits"
	"grits/internal/proxy"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	m := make(map[string]*grits.FileAddr)

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
			m[info.Name()] = file.Address
		}
		return nil
	})
	if err != nil {
		fmt.Printf("Error walking through content directory: %v\n", err)
		return
	}

	fn, err := bs.CreateFileNode(m)
	if err != nil {
		fmt.Printf("Failed to create FileNode: %v\n", err)
		return
	}

	rn, err := bs.CreateRevNode(fn, nil)
	if err != nil {
		fmt.Printf("Failed to create RevNode: %v\n", err)
		return
	}

	ns := proxy.NewNameStore(rn)

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

		fileAddr, err := grits.NewFileAddrFromString(addrStr)
		if err != nil {
			http.Error(w, "Invalid file address format", http.StatusBadRequest)
			return
		}

		// Try to read the file from the blob store using the full address
		var cachedFile *grits.CachedFile
		cachedFile, err = bs.ReadFile(fileAddr)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer bs.Release(cachedFile)

		http.ServeFile(w, r, cachedFile.Path)
		bs.Touch(cachedFile)
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

		// Resolve the name to a file address
		fa := ns.ResolveName(name)
		if fa == nil {
			http.Error(w, "Name not found", http.StatusNotFound)
			return
		}

		// Try to read the file from the blob store using the resolved address
		cf, err := bs.ReadFile(fa)
		if err != nil {
			http.Error(w, "File not found in blob storage!", http.StatusNotFound)
			return
		}
		defer bs.Release(cf)

		// Update LastTouched and touch the file on disk
		http.ServeFile(w, r, cf.Path)
		bs.Touch(cf)
	}
}
