package main

import (
	"fmt"
	"grits/internal/grits"
	"grits/internal/proxy"
	"io"
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
	http.HandleFunc("/grits/v1/namespace/", handleNamespace(bs, ns))
	http.HandleFunc("/grits/v1/root/", handleRoot(ns))

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

func handleRoot(ns *proxy.NameStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
			return
		}

		// Extract account name from URL
		account := strings.TrimPrefix(r.URL.Path, "/grits/v1/root/")
		if account == "" {
			http.Error(w, "Missing account name", http.StatusBadRequest)
			return
		}

		// For now, only 'root' account is supported
		if account != "root" {
			http.Error(w, "Only 'root' account is supported for now", http.StatusForbidden)
			return
		}

		rn := ns.GetRoot() // Assuming GetRoot() method exists and returns *grits.FileAddr
		if rn == nil {
			http.Error(w, "Root namespace not found", http.StatusNotFound)
			return
		}

		fn := rn.Tree
		if fn == nil {
			http.Error(w, "Root namespace tree not found", http.StatusNotFound)
			return
		}

		fa := fn.ExportedBlob.Address

		// Return the address of the root blob as a simple string
		w.Write([]byte(fa.String()))
	}
}

func handleNamespace(bs *proxy.BlobStore, ns *proxy.NameStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract account name from URL
		path := strings.TrimPrefix(r.URL.Path, "/grits/v1/namespace/")
		if path == "" {
			http.Error(w, "Missing account name", http.StatusBadRequest)
			return
		}

		parts := strings.SplitN(path, "/", 2)
		if len(parts) < 2 {
			http.Error(w, "Incomplete namespace path", http.StatusBadRequest)
			return
		}

		account, path := parts[0], parts[1]
		if account != "root" {
			http.Error(w, "Only 'root' account is supported for now", http.StatusForbidden)
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleNamespaceGet(bs, ns, path, w, r)
		case http.MethodPut:
			// Handle PUT request: write new file content to the namespace
			handleNamespacePut(bs, ns, path, w, r)
		case http.MethodDelete:
			// Handle DELETE request: remove file from the namespace
			handleNamespaceDelete(bs, ns, path, w, r)
		default:
			http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
		}
	}
}

func handleNamespaceGet(bs *proxy.BlobStore, ns *proxy.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	rn := ns.GetRoot()
	if rn == nil {
		http.Error(w, "Root namespace not found", http.StatusNotFound)
		return
	}

	fn := rn.Tree
	if fn == nil {
		http.Error(w, "Root namespace tree not found", http.StatusNotFound)
		return
	}

	fa, exists := fn.Children[path]
	if !exists {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Resolve the file address and redirect to the file
	http.Redirect(w, r, "/grits/v1/sha256/"+fa.String(), http.StatusFound)
}

func handleNamespacePut(bs *proxy.BlobStore, ns *proxy.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	// Read the file content from the request body
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Store the file content in the blob store
	cf, err := bs.AddDataBlock(data)
	if err != nil {
		http.Error(w, "Failed to store file content", http.StatusInternalServerError)
		return
	}

	ns.ReviseRoot(bs, func(m map[string]*grits.FileAddr) error {
		m[path] = cf.Address
		return nil
	})

	cf.Release()
}

func handleNamespaceDelete(bs *proxy.BlobStore, ns *proxy.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	ns.ReviseRoot(bs, func(m map[string]*grits.FileAddr) error {
		delete(m, path)
		return nil
	})
}
