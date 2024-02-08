package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"grits/internal/proxy"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Server holds the configuration and state of the web server.
type Server struct {
	Config     *proxy.Config
	BlobStore  *proxy.BlobStore
	NameStore  *proxy.NameStore
	HTTPServer *http.Server
}

// NewServer initializes and returns a new Server instance.
func NewServer(config *proxy.Config) (*Server, error) {
	bs := proxy.NewBlobStore(config)
	if bs == nil {
		return nil, fmt.Errorf("Failed to initialize blob store")
	}

	var ns *proxy.NameStore

	info, err := os.Stat(config.NamespaceStoreFile)
	if err != nil {
		ns, err = initStore(bs)
		if err != nil {
			return nil, fmt.Errorf("Failed to initialize namespace store: %v", err)
		}
	} else {
		if info.IsDir() {
			return nil, fmt.Errorf("Namespace store file is a directory")
		}

		ns, err = bs.DeserializeNameStore()
		if err != nil {
			return nil, fmt.Errorf("Failed to deserialize namespace store: %v", err)
		}
	}

	srv := &Server{
		Config:    config,
		BlobStore: bs,
		NameStore: ns,
		HTTPServer: &http.Server{
			Addr: ":1787",
			// Handler: nil, // Set this if your handlers are not using http.DefaultServeMux
		},
	}
	srv.setupRoutes() // Assuming you have a method to setup your routes
	return srv, nil
}

func (s *Server) setupRoutes() {
	http.HandleFunc("/grits/v1/sha256/", s.handleSHA256())
	http.HandleFunc("/grits/v1/namespace/", s.handleNamespace())
	http.HandleFunc("/grits/v1/root/", s.handleRoot())
	// If you're not using http.DefaultServeMux, set your custom mux in s.HTTPServer.Handler
}

func (s *Server) Start() {
	go func() {
		if err := s.HTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server ListenAndServe: %v\n", err)
		}
	}()
}

func (s *Server) Run() error {
	// This will block
	return s.HTTPServer.ListenAndServe()
}

func (s *Server) Stop() error {
	// Create a context to attempt a graceful shutdown within a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.HTTPServer.Shutdown(ctx)
}

func initStore(bs *proxy.BlobStore) (*proxy.NameStore, error) {
	m := make(map[string]*grits.FileAddr)

	fn, err := bs.CreateFileNode(m)
	if err != nil {
		return nil, err
	}

	rn, err := bs.CreateRevNode(fn, nil)
	if err != nil {
		return nil, err
	}

	ns := proxy.NewNameStore(rn)
	return ns, nil
}

func initDemoStore(bs *proxy.BlobStore) (*proxy.NameStore, error) {
	contentDir := "content/"

	m := make(map[string]*grits.FileAddr)

	err := filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
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
		return nil, err
	}

	fn, err := bs.CreateFileNode(m)
	if err != nil {
		return nil, err
	}

	rn, err := bs.CreateRevNode(fn, nil)
	if err != nil {
		return nil, err
	}

	ns := proxy.NewNameStore(rn)
	return ns, nil
}

func (s *Server) handleSHA256() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Received request: %s\n", r.URL.Path)

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
		cachedFile, err = s.BlobStore.ReadFile(fileAddr)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer s.BlobStore.Release(cachedFile)

		http.ServeFile(w, r, cachedFile.Path)
		s.BlobStore.Touch(cachedFile)
	}
}

func (s *Server) handleRoot() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Received request: %s\n", r.URL.Path)

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

		rn := s.NameStore.GetRoot() // Assuming GetRoot() method exists and returns *grits.FileAddr
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

func (s *Server) handleNamespace() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("Received request: %s\n", r.URL.Path)

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
			handleNamespaceGet(s.BlobStore, s.NameStore, path, w, r)
		case http.MethodPut:
			// Handle PUT request: write new file content to the namespace
			handleNamespacePut(s.BlobStore, s.NameStore, path, w, r)
		case http.MethodDelete:
			// Handle DELETE request: remove file from the namespace
			handleNamespaceDelete(s.BlobStore, s.NameStore, path, w, r)
		default:
			http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
		}
	}
}

func handleNamespaceGet(bs *proxy.BlobStore, ns *proxy.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Received GET request for file: %s\n", path)

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
	fmt.Printf("Received PUT request for file: %s\n", path)

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

	bs.SerializeNameStore(ns)
}

func handleNamespaceDelete(bs *proxy.BlobStore, ns *proxy.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Received DELETE request for file: %s\n", path)

	ns.ReviseRoot(bs, func(m map[string]*grits.FileAddr) error {
		delete(m, path)
		return nil
	})

	bs.SerializeNameStore(ns)
}
