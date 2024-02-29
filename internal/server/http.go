package server

import (
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

// corsMiddleware is a middleware function that adds CORS headers to the response.
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received %s request (port %d): %s\n", r.Method, 1787, r.URL.Path)

		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:1787/") // Or "*" for a public API
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Set cache headers based on the request path
		if strings.HasPrefix(r.URL.Path, "/grits/v1/blob/") {
			// Indicate that the content can be cached indefinitely
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Advise clients to revalidate every time
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache") // For compatibility with HTTP/1.0
			w.Header().Set("Expires", "0")
		}

		// If it's an OPTIONS request, respond with OK status and return
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// tokenAuthMiddleware is a middleware function that checks for a valid token.
func (s *Server) tokenAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		_, exists := s.Peers.GetPeer(token)
		if !exists {
			http.Error(w, "Invalid or missing token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) setupRoutes() {
	// Content routes:

	s.Mux.HandleFunc("/grits/v1/blob/", corsMiddleware(s.handleBlob))
	s.Mux.HandleFunc("/grits/v1/content/root/", corsMiddleware(s.handleContent))
	s.Mux.HandleFunc("/grits/v1/tree", corsMiddleware(s.handleTree))

	// Client tooling routes:

	// Special handling for serving the Service Worker JS from the root
	s.Mux.HandleFunc("/grits/v1/service-worker.js", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, s.Config.ServerPath("client/service-worker.js"))
	}))

	// Handling client files with CORS enabled
	s.Mux.Handle("/grits/v1/client/", http.StripPrefix("/grits/v1/client/", corsMiddleware(http.FileServer(http.Dir(s.Config.ServerPath("client"))).ServeHTTP)))

	// DHT routes:

	// Using the middleware directly with HandleFunc for specific routes
	if s.Config.IsRootNode {
		s.Mux.HandleFunc("/grits/v1/heartbeat", s.tokenAuthMiddleware(s.handleHeartbeat()))
	}
	s.Mux.HandleFunc("/grits/v1/announce", s.tokenAuthMiddleware(s.handleAnnounce()))

	s.HTTPServer.Handler = s.Mux
}

func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request (port %d): %s\n", s.Config.ThisPort, r.URL.Path)

	if r.Method != http.MethodGet {
		http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
		return
	}

	// Extract file address from URL, expecting format "{hash}:{size}"
	addrStr := strings.TrimPrefix(r.URL.Path, "/grits/v1/blob/")
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

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request (port %d): %s\n", s.Config.ThisPort, r.URL.Path)

	if r.Method != http.MethodGet {
		http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
		return
	}

	accountName := "root"
	s.AccountLock.Lock()
	ns, exists := s.AccountStores[accountName]
	s.AccountLock.Unlock()
	if !exists {
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	fn, err := ns.Lookup("/")
	if err != nil {
		http.Error(w, "Root namespace not found", http.StatusNotFound)
		return
	}

	fa := fn.ExportedBlob().Address
	http.Redirect(w, r, "/grits/v1/blob/"+fa.String(), http.StatusFound)
}

// handleFile manages requests for account-specific namespaces
func (s *Server) handleContent(w http.ResponseWriter, r *http.Request) {
	// Extract account name and filepath from the URL
	filePath := strings.TrimPrefix(r.URL.Path, "/grits/v1/content/root/")
	accountName := "root"

	s.AccountLock.Lock()
	ns, exists := s.AccountStores[accountName]
	s.AccountLock.Unlock()
	if !exists {
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	log.Printf("Received request for file: %s\n", filePath)
	log.Printf("Method is %s\n", r.Method)

	switch r.Method {
	case http.MethodGet:
		handleNamespaceGet(s.BlobStore, ns, filePath, w, r)
	case http.MethodPut:
		handleNamespacePut(s.BlobStore, ns, filePath, w, r)
	case http.MethodDelete:
		handleNamespaceDelete(s.BlobStore, ns, filePath, w, r)
	default:
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
	}
}

func handleNamespaceGet(bs *grits.BlobStore, ns *grits.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	log.Printf("Received GET request for file: %s\n", path)

	fn, err := ns.Lookup(path)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	cachedFile, err := bs.ReadFile(fn.ExportedBlob().Address)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer bs.Release(cachedFile)

	http.ServeFile(w, r, cachedFile.Path)
	bs.Touch(cachedFile)
}

func handleNamespacePut(bs *grits.BlobStore, ns *grits.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	log.Printf("Received PUT request for file: %s\n", path)

	if path == "" || path == "/" {
		http.Error(w, "Cannot modify root of namespace", http.StatusForbidden)
		return
	}

	// Read the file content from the request body
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Extract the file extension, including the leading dot
	ext := filepath.Ext(path)

	// Store the file content in the blob store
	cf, err := bs.AddDataBlock(data, ext)
	if err != nil {
		http.Error(w, "Failed to store file content", http.StatusInternalServerError)
		return
	}
	defer bs.Release(cf)

	err = ns.LinkBlob(path, cf.Address)
	if err != nil {
		http.Error(w, "Failed to link file to namespace", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File linked successfully")
}

func handleNamespaceDelete(bs *grits.BlobStore, ns *grits.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	log.Printf("Received DELETE request for file: %s\n", path)

	if path == "" || path == "/" {
		http.Error(w, "Cannot modify root of namespace", http.StatusForbidden)
		return
	}

	err := ns.LinkBlob(path, nil)
	if err != nil {
		http.Error(w, "Failed to link file to namespace", http.StatusInternalServerError)
		return
	}

	// If the file was successfully deleted, you can return an appropriate success response
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File deleted successfully")
}

func (s *Server) handleHeartbeat() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		peer, exists := s.Peers.GetPeer(token)
		if !exists {
			http.Error(w, "Unauthorized: Unknown or invalid token", http.StatusUnauthorized)
			return
		}

		peer.UpdateLastSeen()

		peerList, error := s.Peers.Serialize()
		if error != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.Write(peerList)
	}
}

func (s *Server) handleAnnounce() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		peer, exists := s.Peers.GetPeer(token)
		if !exists {
			http.Error(w, "Unauthorized: Unknown or invalid token", http.StatusUnauthorized)
			return
		}

		peer.UpdateLastSeen()
		// Process the announcement...
	}
}
