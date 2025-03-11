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
	"path"
	"path/filepath"
	"strings"
	"time"
)

type HTTPModuleConfig struct {
	ThisHost string `json:"ThisHost"`
	ThisPort int    `json:"ThisPort"`

	EnableTls bool  `json:"EnableTLS,omitempty"`
	ReadOnly  *bool `json:"ReadOnly,omitempty"`
}

type HTTPModule struct {
	Config *HTTPModuleConfig
	Server *Server

	HTTPServer *http.Server
	Mux        *http.ServeMux

	deployments         []*DeploymentModule
	serviceWorkerModule *ServiceWorkerModule
}

func (*HTTPModule) GetModuleName() string {
	return "http"
}

// NewHTTPModule creates and initializes an HTTPModule instance based on the provided configuration.
func NewHTTPModule(server *Server, config *HTTPModuleConfig) *HTTPModule {
	// If ReadOnly wasn't specified, default to true
	if config.ReadOnly == nil {
		readOnly := true
		config.ReadOnly = &readOnly
	}

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

		deployments: make([]*DeploymentModule, 0),
	}

	// Set up routes within the constructor or an initialization method
	httpModule.setupRoutes()

	server.AddModuleHook(httpModule.addDeploymentModule)
	server.AddModuleHook(httpModule.addServiceWorkerModule)

	return httpModule
}

// Start begins serving HTTP requests.
func (hm *HTTPModule) Start() error {
	// Starting the HTTP server in a goroutine
	go func() {
		var err error
		if hm.Config.EnableTls {
			// Paths to cert and key files
			certPath := hm.Server.Config.ServerPath("certs/fullchain.pem")
			keyPath := hm.Server.Config.ServerPath("certs/privkey.pem")

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

func (hm *HTTPModule) addDeploymentModule(module Module) {
	deployment, ok := module.(*DeploymentModule)
	if !ok {
		return
	}

	hm.deployments = append(hm.deployments, deployment)
}

func (hm *HTTPModule) addServiceWorkerModule(module Module) {
	swModule, ok := module.(*ServiceWorkerModule)
	if !ok {
		return
	}

	// Check if we already have a service worker module
	if hm.serviceWorkerModule != nil {
		log.Fatalf("Only one ServiceWorkerModule can be registered")
	}

	log.Printf("Registering ServiceWorkerModule in HTTP module")

	// Store the service worker module
	hm.serviceWorkerModule = swModule

	// Add routes
	hm.Mux.HandleFunc("/grits-bootstrap.js", hm.corsMiddleware(swModule.serveTemplate))
	hm.Mux.HandleFunc("/grits-serviceworker.js", hm.corsMiddleware(swModule.serveTemplate))
	hm.Mux.HandleFunc("/grits-serviceworker-config.json", hm.corsMiddleware(swModule.serveConfig))
}

// corsMiddleware is a middleware function that adds CORS headers to the response.
// NOTE: Also adds service worker dir hash
// FIXME - this needs a new name
func (srv *HTTPModule) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tracker := NewPerformanceTracker(r)
		tracker.Start()

		// Basic request logging (kept from original)
		log.Printf("Received %s request (port %d): %s\n", r.Method, srv.Config.ThisPort, r.URL.Path)

		tracker.Step("Setting CORS headers")
		w.Header().Set("Access-Control-Allow-Origin", fmt.Sprintf("http://localhost:%d/", srv.Config.ThisPort))
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Set cache headers based on the request path
		if strings.HasPrefix(r.URL.Path, "/grits/v1/blob/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}

		// If it's an OPTIONS request, respond with OK status and return
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			tracker.End()
			return
		}

		tracker.Step("Setting service worker headers")
		// Also do service worker cache control header
		if srv.serviceWorkerModule != nil {
			// Get the hash from the service worker module's volume
			clientDirHash := srv.serviceWorkerModule.getClientDirHash()
			w.Header().Set("X-Grits-Service-Worker-Hash", clientDirHash)
		}

		// Wrap the response writer to capture when the handler finishes
		wrappedWriter := &responseWriterWrapper{
			ResponseWriter: w,
			tracker:        tracker,
		}

		tracker.Step("Calling next handler")
		next(wrappedWriter, r)

		// In case the wrapper didn't capture the end (e.g., if there was no write)
		if !wrappedWriter.ended {
			tracker.End()
		}
	}
}

// responseWriterWrapper wraps http.ResponseWriter to track when the response is written
type responseWriterWrapper struct {
	http.ResponseWriter
	tracker *PerformanceTracker
	ended   bool
}

// WriteHeader captures the performance metric at the end of the request
func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
	if !w.ended {
		w.tracker.End()
		w.ended = true
	}
}

// Write captures the performance metric when the response is written
func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if !w.ended {
		w.tracker.End()
		w.ended = true
	}
	return n, err
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
	tracker := NewPerformanceTracker(r)
	tracker.Start()

	addrStr := strings.TrimPrefix(r.URL.Path, "/grits/v1/blob/")
	if addrStr == "" {
		http.Error(w, "Missing file address", http.StatusBadRequest)
		tracker.End()
		return
	}

	tracker.Step("Parsing blob address")
	// Strip out the size component if present (format: hash-size)
	if dashIndex := strings.LastIndex(addrStr, "-"); dashIndex != -1 {
		addrStr = addrStr[:dashIndex]
	}

	fileAddr, err := grits.NewBlobAddrFromString(addrStr)
	if err != nil {
		http.Error(w, "Invalid file address format", http.StatusBadRequest)
		tracker.End()
		return
	}

	tracker.Step("Reading file from blob store")
	cachedFile, err := s.Server.BlobStore.ReadFile(fileAddr)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		tracker.End()
		return
	}
	defer cachedFile.Release()

	tracker.Step("Getting reader")
	reader, err := cachedFile.Reader()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		tracker.End()
		return
	}
	defer reader.Close()

	tracker.Step("Seeking to file start")
	// Seek to the beginning of the file
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		tracker.End()
		return
	}

	tracker.Step("Serving content")
	// Serve the content directly using the reader
	http.ServeContent(w, r, filepath.Base(fileAddr.Hash), time.Now(), reader)
	cachedFile.Touch()

	tracker.End()
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

	if *s.Config.ReadOnly {
		http.Error(w, "Volume is read-only", http.StatusForbidden)
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

	pathNodePairs, err := volume.LookupFull(lookupPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Lookup failed: %v", err), http.StatusNotFound)
		return
	}

	// Transform to the format expected by the client
	response := make([][]interface{}, len(pathNodePairs))
	for i, pair := range pathNodePairs {
		node := pair.Node
		metadataHash := node.MetadataBlob().GetAddress().Hash
		contentHash := node.ExportedBlob().GetAddress().Hash
		contentSize := node.ExportedBlob().GetSize()

		response[i] = []interface{}{
			pair.Path,
			metadataHash,
			contentHash,
			contentSize,
		}

		// Release the reference we took in LookupFull
		node.Release()
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

	if *s.Config.ReadOnly {
		http.Error(w, "Volume is read-only", http.StatusForbidden)
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

	// Find all matching deployments for this hostname
	var matchingDeployments []*DeploymentModule
	for _, deployment := range s.deployments {
		log.Printf("Compare %s %s", deployment.Config.HostName, hostname)
		if deployment.Config.HostName == hostname {
			matchingDeployments = append(matchingDeployments, deployment)
		}
	}

	if len(matchingDeployments) == 0 {
		http.Error(w, fmt.Sprintf("Deployment for %s on %s not found", r.URL.Path, hostname), http.StatusNotFound)
		return
	}

	// Try to find a deployment that matches the request path
	for _, deployment := range matchingDeployments {
		if strings.HasPrefix(r.URL.Path, deployment.Config.UrlPath) {
			volume := deployment.Config.Volume
			volumePath := strings.TrimPrefix(r.URL.Path, deployment.Config.UrlPath)
			volumePath = path.Join(deployment.Config.VolumePath, volumePath)

			s.handleContentRequest(volume, volumePath, w, r)
			return
		}
	}

	http.Error(w, fmt.Sprintf("Path %s not found in any deployment for %s", r.URL.Path, hostname), http.StatusNotFound)
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
		if *s.Config.ReadOnly {
			http.Error(w, "Volume is read-only", http.StatusForbidden)
		} else {
			handleNamespacePut(s.Server.BlobStore, volume, filePath, w, r)
		}

	case http.MethodDelete:
		if *s.Config.ReadOnly {
			http.Error(w, "Volume is read-only", http.StatusForbidden)
		} else {
			handleNamespaceDelete(volume, filePath, w)
		}

	default:
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
	}
}

// Modified handleNamespaceGet with performance tracking
func handleNamespaceGet(bs grits.BlobStore, volume Volume, path string, w http.ResponseWriter, r *http.Request) {
	tracker := NewPerformanceTracker(r)
	tracker.Start()

	log.Printf("Received GET request for file: %s\n", path)

	tracker.Step("Looking up resource in volume")
	// Look up the resource in the volume to get its address
	pathAddr, err := volume.Lookup(path)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		tracker.End()
		return
	}

	tracker.Step("Setting HTTP headers")
	// Use the address hash as the ETag
	etag := fmt.Sprintf("\"%s\"", pathAddr.String())
	w.Header().Set("ETag", etag)

	// Tell browsers to revalidate every time
	w.Header().Set("Cache-Control", "no-cache")

	// Check If-None-Match header for conditional requests
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		// Resource hasn't changed, return 304 Not Modified
		w.WriteHeader(http.StatusNotModified)
		tracker.End()
		return
	}

	var cf grits.CachedFile

	tracker.Step("Resolving content address")
	if pathAddr.Type == grits.Tree {
		filePath := strings.TrimRight(path, "/")
		filePath += "/index.html"
		filePath = strings.TrimPrefix(filePath, "/")

		tracker.Step("Looking up index.html")
		addr, err := volume.Lookup(filePath)
		if err != nil {
			http.Error(w, fmt.Sprintf("No index: %v", err), http.StatusNotFound)
			tracker.End()
			return
		}

		tracker.Step("Reading index file from blob store")
		cf, err = bs.ReadFile(&addr.BlobAddr)
		if err != nil {
			http.Error(w, "Can't open file for read", http.StatusInternalServerError)
			tracker.End()
			return
		}
	} else {
		tracker.Step("Reading blob file")
		cf, err = bs.ReadFile(&grits.BlobAddr{Hash: pathAddr.Hash})
		if err != nil {
			http.Error(w, "Cannot open blob", http.StatusInternalServerError)
			tracker.End()
			return
		}
	}
	defer cf.Release()

	tracker.Step("Opening file for reading")
	// Open the file for reading
	file, err := cf.Reader()
	if err != nil {
		log.Printf("Error opening file: %v\n", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		tracker.End()
		return
	}
	defer file.Close()

	tracker.Step("Serving file content")
	// Serve the content
	log.Printf("Serving file %s\n", cf.GetAddress().String())
	http.ServeContent(w, r, filepath.Base(path), time.Now(), file)

	tracker.End()
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
