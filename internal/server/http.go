package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type HTTPModuleConfig struct {
	ThisHost string `json:"ThisHost"`
	ThisPort int    `json:"ThisPort"`

	EnableTls bool `json:"EnableTLS,omitempty"`
}

type HTTPModule struct {
	Config *HTTPModuleConfig
	Server *Server

	HTTPServer *http.Server
	Mux        *http.ServeMux

	Deployments map[string]*DeploymentModule
}

func (*HTTPModule) GetModuleName() string {
	return "http"
}

// NewHTTPModule creates and initializes an HTTPModule instance based on the provided configuration.
func NewHTTPModule(server *Server, config *HTTPModuleConfig) *HTTPModule {
	mux := http.NewServeMux()
	HTTPServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.ThisPort),
		Handler: mux,
	}

	log.Printf("HTTP listening on %s\n", HTTPServer.Addr)

	httpModule := &HTTPModule{
		Config: config,
		Server: server,

		HTTPServer: HTTPServer,
		Mux:        mux,

		Deployments: make(map[string]*DeploymentModule),
	}

	// Set up routes within the constructor or an initialization method
	httpModule.setupRoutes()

	server.AddModuleHook(httpModule.addDeployment)

	return httpModule
}

// Start begins serving HTTP requests.
func (hm *HTTPModule) Start() error {
	// Starting the HTTP server in a goroutine
	go func() {
		var err error
		if hm.Config.EnableTls {
			// Paths to cert and key files
			certPath := hm.Server.Config.ServerPath("certs/server.crt")
			keyPath := hm.Server.Config.ServerPath("certs/server.key")

			log.Printf("Starting HTTPS server on %s\n", hm.HTTPServer.Addr)
			err = hm.HTTPServer.ListenAndServeTLS(certPath, keyPath)
		} else {
			log.Printf("Starting HTTP server on %s\n", hm.HTTPServer.Addr)
			err = hm.HTTPServer.ListenAndServe()
		}
		if err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
	log.Printf("HTTP module started on %s (TLS enabled: %t)\n", hm.HTTPServer.Addr, hm.Config.EnableTls)
	return nil
}

// Stop gracefully shuts down the HTTP server.
func (hm *HTTPModule) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := hm.HTTPServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP module shutdown error: %v", err)
		return err
	}

	log.Println("HTTP module stopped")
	return nil
}

func (hm *HTTPModule) addDeployment(module Module) {
	deployment, ok := module.(*DeploymentModule)
	if ok {
		hostname := deployment.Config.HostName
		if _, exists := hm.Deployments[hostname]; exists {
			log.Fatalf("Deployment for %s is defined twice", hostname)
		}

		hm.Deployments[hostname] = deployment
	}
}

// corsMiddleware is a middleware function that adds CORS headers to the response.
func (srv *HTTPModule) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received %s request (port %d): %s\n", r.Method, srv.Config.ThisPort, r.URL.Path)

		w.Header().Set("Access-Control-Allow-Origin", fmt.Sprintf("http://localhost:%d/", srv.Config.ThisPort)) // Or "*" for a public API
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

/*

General route API:

GET to /grits/v1/blob/{hash}-{size} returns the blob data

POST to /grits/v1/upload accepts binary data for the blob in the request body,
   and the response is the new address ("{hash}-{size}" format) as a JSON-encoded
   bare string

POST to /grits/v1/lookup/{volume} accepts a bare JSON-encoded string in the request body,
  and returns a JSON-encoded array of pairs of strings: [$path, $resource_addr_at_that_path]

POST to /grits/v1/link/{volume} accepts a JSON-encoded array of maps
  {'path': {path}, 'addr': {addr}} indicating a bunch of resources to link into
  the given volume's storage atomically.


GET to /grits/v1/content/{volume}/{path} just serves file data (mainly for debugging)

*/

func (s *HTTPModule) setupRoutes() {
	// Deployment routes:
	s.Mux.HandleFunc("/", s.corsMiddleware(s.handleDeployment))

	// Content routes:
	s.Mux.HandleFunc("/grits/v1/blob/", s.corsMiddleware(s.handleBlob))
	s.Mux.HandleFunc("/grits/v1/upload", s.corsMiddleware(s.handleBlobUpload))

	s.Mux.HandleFunc("/grits/v1/lookup/", s.corsMiddleware(s.handleLookup))
	s.Mux.HandleFunc("/grits/v1/link/", s.corsMiddleware(s.handleLink))

	s.Mux.HandleFunc("/grits/v1/content/", s.corsMiddleware(s.handleContent))

	// Client tooling routes:

	// Special handling for serving the Service Worker JS from the root
	s.Mux.HandleFunc("/grits/v1/service-worker.js", s.corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, s.Server.Config.ServerPath("client/service-worker.js"))
	}))

	// Handling client files with CORS enabled
	s.Mux.Handle("/grits/v1/client/", http.StripPrefix("/grits/v1/client/", s.corsMiddleware(http.FileServer(http.Dir(s.Server.Config.ServerPath("client"))).ServeHTTP)))

	s.HTTPServer.Handler = s.Mux
}

func (s *HTTPModule) handleBlob(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		s.handleBlobFetch(w, r) // Automatically skips sending the file for HEAD
	case http.MethodGet:
		s.handleBlobFetch(w, r)
	default:
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
	}
}

func (s *HTTPModule) handleBlobFetch(w http.ResponseWriter, r *http.Request) {
	addrStr := strings.TrimPrefix(r.URL.Path, "/grits/v1/blob/")
	if addrStr == "" {
		http.Error(w, "Missing file address", http.StatusBadRequest)
		return
	}

	fileAddr, err := grits.NewBlobAddrFromString(addrStr)
	if err != nil {
		http.Error(w, "Invalid file address format", http.StatusBadRequest)
		return
	}

	cachedFile, err := s.Server.BlobStore.ReadFile(fileAddr)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer cachedFile.Release()

	reader, err := cachedFile.Reader()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	// Seek to the beginning of the file
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Serve the content directly using the reader
	http.ServeContent(w, r, filepath.Base(fileAddr.Hash), time.Now(), reader)
	cachedFile.Touch()
}

// validateFileContents opens the file, computes its SHA-256 hash and size,
// and compares them with the expected values.
func validateFileContents(filePath string, expectedAddr *grits.BlobAddr) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	hasher := sha256.New()
	_, err = io.Copy(hasher, file)
	if err != nil {
		return false, err
	}

	computedHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if computedHash != expectedAddr.Hash {
		return false, fmt.Errorf("hash or size mismatch")
	}

	return true, nil
}

func (s *HTTPModule) handleBlobUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST is supported", http.StatusMethodNotAllowed)
		return
	}

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "blob-upload-*")
	if err != nil {
		log.Printf("Failed to create temporary file: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpFile.Name()) // Clean up the file afterwards

	// Read the request body and write it to the temporary file
	_, err = io.Copy(tmpFile, r.Body)
	tmpFile.Close()
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Add the file to the blob store
	cachedFile, err := s.Server.BlobStore.AddLocalFile(tmpFile.Name())
	if err != nil {
		log.Printf("Failed to add file to blob store: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer cachedFile.Release()

	// Respond with the address of the new blob
	addrStr := cachedFile.GetAddress().String()
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(addrStr)
}

func (s *HTTPModule) handleLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST is supported", http.StatusMethodNotAllowed)
		return
	}

	var lookupPath string
	if err := json.NewDecoder(r.Body).Decode(&lookupPath); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	volumeName := strings.TrimPrefix(r.URL.Path, "/grits/v1/lookup/")
	if volumeName == "" {
		http.Error(w, "Volume name is required", http.StatusBadRequest)
		return
	}

	volume := s.Server.FindVolumeByName(volumeName)
	if volume == nil {
		http.Error(w, "Volume not found", http.StatusNotFound)
		return
	}

	response, err := volume.LookupFull(lookupPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Lookup failed: %v", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func (s *HTTPModule) handleLink(w http.ResponseWriter, r *http.Request) {
	log.Printf("Handling link request\n")

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST is supported", http.StatusMethodNotAllowed)
		return
	}

	var allLinkData []struct {
		Path string `json:"path"`
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&allLinkData); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	volumeName := strings.TrimPrefix(r.URL.Path, "/grits/v1/link/")
	if volumeName == "" {
		http.Error(w, "Volume name is required", http.StatusBadRequest)
		return
	}

	volume := s.Server.FindVolumeByName(volumeName)
	if volume == nil {
		http.Error(w, fmt.Sprintf("Volume %s not found", volume), http.StatusNotFound)
		return
	}

	if volume.isReadOnly() {
		http.Error(w, fmt.Sprintf("Volume %s is read-only", volume), http.StatusForbidden)
		return
	}

	for _, linkData := range allLinkData {
		addr, err := grits.NewTypedFileAddrFromString(linkData.Addr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to decode TypedFileAddr %s", linkData.Addr), http.StatusBadRequest)
			return
		}

		// Perform link
		fmt.Printf("Perform link: %s to %s\n", linkData.Path, addr.String())
		if err := volume.Link(linkData.Path, addr); err != nil {
			http.Error(w, fmt.Sprintf("Link failed: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("Link successful for path\n")
	}

	err := volume.Checkpoint()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to checkpoint %s: %v", volume, err), http.StatusInternalServerError)
	}

	result := make([][]string, 0) // TODO

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)

	fmt.Fprintf(w, "Link successful")
}

func (s *HTTPModule) handleDeployment(w http.ResponseWriter, r *http.Request) {
	// Extract hostname from the request
	hostname := r.Host
	deployment, exists := s.Deployments[hostname]
	if !exists {
		http.Error(w, fmt.Sprintf("Host deployment %s not found", hostname), http.StatusNotFound)
		return
	}

	for _, mapping := range deployment.Config.PathMappings {
		if strings.HasPrefix(r.URL.Path, mapping.UrlPath) {
			volume := mapping.Volume
			volumePath := strings.TrimPrefix(r.URL.Path, mapping.UrlPath)

			s.handleContentRequest(volume, volumePath, w, r)
			return
		}
	}

	http.Error(w, fmt.Sprintf("Path %s not found in deployment", r.URL.Path), http.StatusNotFound)
}

func (s *HTTPModule) handleContent(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/grits/v1/content/")

	// Example path: /grits/v1/content/volumeName/some/path
	pathParts := strings.SplitN(path, "/", 2)
	if len(pathParts) < 2 {
		http.Error(w, "URL must include a volume name and path", http.StatusBadRequest)
		return
	}

	volumeName := pathParts[0]
	filePath := pathParts[1] // Remaining path

	s.handleContentRequest(volumeName, filePath, w, r)
}

func (s *HTTPModule) handleContentRequest(volumeName, filePath string, w http.ResponseWriter, r *http.Request) {
	volume := s.Server.FindVolumeByName(volumeName)
	if volume == nil {
		http.Error(w, fmt.Sprintf("Volume %s not found", volumeName), http.StatusNotFound)
		return
	}

	log.Printf("Received request for file: %s\n", filePath)
	log.Printf("Method is %s\n", r.Method)

	switch r.Method {
	case http.MethodGet:
		handleNamespaceGet(s.Server.BlobStore, volume, filePath, w, r)
	case http.MethodPut:
		handleNamespacePut(s.Server.BlobStore, volume, filePath, w, r)
	case http.MethodDelete:
		handleNamespaceDelete(volume, filePath, w)
	default:
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
	}
}

func handleNamespaceGet(bs grits.BlobStore, volume Volume, path string, w http.ResponseWriter, r *http.Request) {
	log.Printf("Received GET request for file: %s\n", path)

	fullPath, err := volume.LookupFull(path)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if len(fullPath) < 1 {
		http.Error(w, "Empty volume", http.StatusNotFound)
		return
	}

	pathAddrStr := fullPath[len(fullPath)-1][1]
	pathAddr, err := grits.NewTypedFileAddrFromString(pathAddrStr)
	if err != nil {
		http.Error(w, "Invalid tree node", http.StatusInternalServerError)
		return
	}

	var cf grits.CachedFile
	if pathAddr.Type == grits.Tree {
		addr, err := volume.Lookup(strings.TrimRight(path, "/") + "/index.html")
		if err != nil {
			http.Error(w, fmt.Sprintf("No index: %v", err), http.StatusNotFound)
			return
		}

		cf, err = bs.ReadFile(&addr.BlobAddr)
		if err != nil {
			http.Error(w, "Can't open file for read", http.StatusInternalServerError)
			return
		}
	} else {
		cf, err = bs.ReadFile(&grits.BlobAddr{Hash: pathAddr.Hash})
		if err != nil {
			http.Error(w, "Cannot open blob", http.StatusInternalServerError)
		}
	}
	defer cf.Release()

	// FIXME - What? Seems like this is unnecessary:

	//cachedFile, err := bs.ReadFile(cf.Address)
	//if err != nil {
	//	http.Error(w, "File not found", http.StatusNotFound)
	//	return
	//}
	//defer cachedFile.Release()

	// Open the file for reading
	file, err := cf.Reader()
	if err != nil {
		log.Printf("Error opening file: %v\n", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Serve the content
	log.Printf("Serving file %s\n", cf.GetAddress().String())
	http.ServeContent(w, r, filepath.Base(path) /* FIXME */, time.Now(), file)
}

func handleNamespacePut(bs grits.BlobStore, volume Volume, path string, w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, fmt.Sprintf("Failed to store %s content", path), http.StatusInternalServerError)
		return
	}
	defer cf.Release()

	addr := grits.NewTypedFileAddr(cf.GetAddress().Hash, cf.GetSize(), grits.Blob)
	err = volume.Link(path, addr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to link %s to namespace", path), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File linked successfully")
}

func handleNamespaceDelete(volume Volume, path string, w http.ResponseWriter) {
	log.Printf("Received DELETE request for file: %s\n", path)

	if path == "" || path == "/" {
		http.Error(w, "Cannot modify root of namespace", http.StatusForbidden)
		return
	}

	err := volume.Link(path, nil)
	if err != nil {
		http.Error(w, "Failed to link file to namespace", http.StatusInternalServerError)
		return
	}

	// If the file was successfully deleted, you can return an appropriate success response
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File deleted successfully")
}
