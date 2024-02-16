package server

import (
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"net/http"
	"strings"
)

// corsMiddleware is a middleware function that adds CORS headers to the response.
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:1787") // Or "*" for a public API
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

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
	s.Mux.HandleFunc("/grits/v1/sha256/", corsMiddleware(s.handleSha256))
	s.Mux.HandleFunc("/grits/v1/home/", corsMiddleware(s.handleHome))

	// Special handling for serving the Service Worker JS from the root
	s.Mux.HandleFunc("/grits/v1/service-worker.js", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, s.Config.ServerPath("client/service-worker.js"))
	}))

	// Handling client files with CORS enabled
	s.Mux.Handle("/grits/v1/client/", http.StripPrefix("/grits/v1/client/", corsMiddleware(http.FileServer(http.Dir(s.Config.ServerPath("client"))).ServeHTTP)))

	// Using the middleware directly with HandleFunc for specific routes
	if s.Config.IsRootNode {
		s.Mux.HandleFunc("/grits/v1/heartbeat", s.tokenAuthMiddleware(s.handleHeartbeat()))
	}
	s.Mux.HandleFunc("/grits/v1/announce", s.tokenAuthMiddleware(s.handleAnnounce()))

	s.HTTPServer.Handler = s.Mux
}

func (s *Server) handleSha256(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request (port %d): %s\n", s.Config.ThisPort, r.URL.Path)

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

// handleHome manages requests for account-specific namespaces
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	// Extract account name and filepath from the URL
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/grits/v1/home/"), "/", 2)

	if len(parts) < 1 {
		http.Error(w, "Invalid request path", http.StatusBadRequest)
		return
	}

	accountName := parts[0]

	fmt.Printf("Received request for account: %s\n", accountName)

	s.AccountLock.Lock()
	ns, exists := s.AccountStores[accountName]
	s.AccountLock.Unlock()

	if !exists {
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	if len(parts) == 1 {
		handleNamespaceGetRaw(s.BlobStore, ns, w, r)
		return
	}

	filePath := parts[1]
	fmt.Printf("Received request for file: %s\n", filePath)

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

func handleNamespaceGetRaw(bs *grits.BlobStore, ns *grits.NameStore, w http.ResponseWriter, r *http.Request) {
	log.Printf("Received GET request for file root\n")

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

	fa := rn.ExportedBlob.Address
	http.Redirect(w, r, "/grits/v1/sha256/"+fa.String(), http.StatusFound)
}

func handleNamespaceGet(bs *grits.BlobStore, ns *grits.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	log.Printf("Received GET request for file: %s\n", path)

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

	bs.Release(cf)
}

func handleNamespaceDelete(bs *grits.BlobStore, ns *grits.NameStore, path string, w http.ResponseWriter, r *http.Request) {
	log.Printf("Received DELETE request for file: %s\n", path)

	err := ns.ReviseRoot(bs, func(m map[string]*grits.FileAddr) error {
		// Check if the key exists in the map
		if _, exists := m[path]; !exists {
			// If the key does not exist, return an error indicating the file was not found
			return fmt.Errorf("file not found: %s", path)
		}

		// If the key exists, delete it from the map
		delete(m, path)
		return nil
	})

	// If an error occurred during the revision, write an error response
	if err != nil {
		log.Printf("Error deleting file: %v\n", err)
		http.Error(w, fmt.Sprintf("Error deleting file: %v", err), http.StatusNotFound)
		return
	}
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
