package gritsd

import (
	"context"
	"crypto/tls"
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
	ThisHost string `json:"thisHost"`
	ThisPort int    `json:"thisPort"`

	EnableTls bool  `json:"enableTLS,omitempty"`
	ReadOnly  *bool `json:"readOnly,omitempty"`

	// For TLS certificates, two options:

	// 1. Auto config:
	AutoCertificate bool   `json:"autoCertificate,omitempty"` // Enable automatic certificate acquisition
	CertbotEmail    string `json:"certbotEmail,omitempty"`    // Email for Let's Encrypt registration

	// 2. Manual config:
	CertPath string `json:"certPath,omitempty"` // Path to fullchain.pem
	KeyPath  string `json:"keyPath,omitempty"`  // Path to privkey.pem

	// Temporary: Use self-signed certs instead of Let's Encrypt
	// If both AutoCertificate and UseSelfSigned are true, AutoCertificate takes precedence
	UseSelfSigned bool `json:"useSelfSigned,omitempty"` // Use self-signed certificates
}

type HTTPModule struct {
	Config *HTTPModuleConfig
	Server *Server

	HTTPServer *http.Server
	Mux        *http.ServeMux

	deployments         []*DeploymentModule
	serviceWorkerModule *ServiceWorkerModule
	activeMirrorModule  *MirrorModule
}

func (*HTTPModule) GetModuleName() string {
	return "http"
}

func (*HTTPModule) GetDependencies() []*Dependency {
	return []*Dependency{
		{
			ModuleType: "peer",
			Type:       DependOptional,

			// Ordering only - we need to load up the peer first, to
			// get DNS working, if we're a mirror that needs certbot for TLS
		},
	}
}

func (m *HTTPModule) GetConfig() any {
	return m.Config
}

// NewHTTPModule creates and initializes an HTTPModule instance based on the provided configuration.
func NewHTTPModule(server *Server, config *HTTPModuleConfig) (*HTTPModule, error) {
	// If ReadOnly wasn't specified, default to true
	if config.ReadOnly == nil {
		readOnly := true
		config.ReadOnly = &readOnly
	}

	// Validate certificate configuration
	if config.EnableTls {
		// If auto certificate is enabled, email is required
		if config.AutoCertificate && config.CertbotEmail == "" {
			return nil, fmt.Errorf("certbotEmail is required when autoCertificate is enabled")
		}

		// If manual paths are provided, check that both cert and key are provided
		if !config.AutoCertificate && !config.UseSelfSigned &&
			(config.CertPath == "" || config.KeyPath == "") {
			return nil, fmt.Errorf("certPath and keyPath are required when not using automatic certificates")
		}

		// If hostname is empty, can't use Let's Encrypt
		if config.AutoCertificate && config.ThisHost == "" {
			return nil, fmt.Errorf("thisHost is required for Let's Encrypt certificates")
		}
	}

	mux := http.NewServeMux()
	HTTPServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", config.ThisPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Only set TLS config if TLS is enabled
	if config.EnableTls {
		HTTPServer.TLSConfig = &tls.Config{
			NextProtos: []string{"h2", "http/1.1"}, // Support HTTP/2 and fallback to HTTP/1.1
		}
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
	server.AddModuleHook(httpModule.addMirrorModule)

	return httpModule, nil
}

func (hm *HTTPModule) Start() error {
	var err error
	var certPath, keyPath string

	if hm.Config.EnableTls {
		if hm.Config.AutoCertificate {
			// Create certificate config for Let's Encrypt
			certbotConfig := &CertbotConfig{
				Domain: hm.Config.ThisHost,
				Email:  hm.Config.CertbotEmail,
			}

			// Use the new certificate management structure for Let's Encrypt certificates
			certPath, keyPath, err = EnsureTLSCertificates(hm.Server.Config, certbotConfig, true)
			if err != nil {
				return fmt.Errorf("let's encrypt certificate error: %v", err)
			}

			log.Printf("Using Let's Encrypt certificates for %s", hm.Config.ThisHost)
		} else if hm.Config.UseSelfSigned {
			// For self-signed certificates, generate them if they don't exist
			err := GenerateSelfCert(hm.Server.Config)
			if err != nil {
				return fmt.Errorf("self-signed certificate generation error: %v", err)
			}

			// Get paths to the self-signed certificates
			certPath, keyPath = GetCertificateFiles(hm.Server.Config, SelfSignedCert, hm.Config.ThisHost)
			log.Printf("Using self-signed certificates for %s", hm.Config.ThisHost)
		} else {
			// Using manually specified certificate paths
			// If manual certificate paths are provided, check if they're absolute paths
			// or paths relative to the server configuration
			if !filepath.IsAbs(hm.Config.CertPath) {
				certPath = hm.Server.Config.ServerPath(hm.Config.CertPath)
			} else {
				certPath = hm.Config.CertPath
			}

			if !filepath.IsAbs(hm.Config.KeyPath) {
				keyPath = hm.Server.Config.ServerPath(hm.Config.KeyPath)
			} else {
				keyPath = hm.Config.KeyPath
			}

			// Verify that the certificate files exist
			if !fileExists(certPath) || !fileExists(keyPath) {
				return fmt.Errorf("certificate files not found at %s and %s", certPath, keyPath)
			}

			log.Printf("Using manual certificate paths: cert=%s, key=%s", certPath, keyPath)
		}
	}

	go func() {
		if hm.Config.EnableTls {
			err = hm.HTTPServer.ListenAndServeTLS(certPath, keyPath)
		} else {
			err = hm.HTTPServer.ListenAndServe()
		}

		// FIXME - better handling
		if err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
	time.Sleep(250 * time.Millisecond)

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
	hm.Mux.HandleFunc("/grits-bootstrap.js", hm.requestMiddleware(swModule.serveTemplate))
	hm.Mux.HandleFunc("/grits-serviceworker.js", hm.requestMiddleware(swModule.serveTemplate))
	hm.Mux.HandleFunc("/grits-serviceworker-config.json", hm.requestMiddleware(swModule.serveConfig))
}

func (hm *HTTPModule) addMirrorModule(module Module) {
	mirror, ok := module.(*MirrorModule)
	if !ok {
		return
	}

	if hm.activeMirrorModule != nil {
		log.Fatalf("Only one mirror module at a time is currently supported.")
	}

	hm.activeMirrorModule = mirror
}

// requestMiddleware is a middleware function that adds various headers to the response.
func (srv *HTTPModule) requestMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tracker := NewPerformanceTracker(r)
		tracker.Start()

		// Basic request logging
		log.Printf("Received %s request (port %d): %s\n", r.Method, srv.Config.ThisPort, r.URL.Path)

		tracker.Step("Setting CORS headers")

		// CORS - Allow requests from origin server and our own origin
		thisScheme := "http"
		if srv.Config.EnableTls {
			thisScheme = "https"
		}
		// Our own origin
		thisOrigin := fmt.Sprintf("%s://%s:%d", thisScheme, srv.Config.ThisHost, srv.Config.ThisPort)

		// If we're a mirror, also include the origin server we're mirroring
		if srv.activeMirrorModule != nil {
			originServer := fmt.Sprintf("%s://%s",
				srv.activeMirrorModule.Config.Protocol,
				srv.activeMirrorModule.Config.RemoteHost)

			// Set the header to allow the origin server
			w.Header().Set("Access-Control-Allow-Origin", originServer)
		} else {
			// Set to our own origin otherwise
			w.Header().Set("Access-Control-Allow-Origin", thisOrigin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS, POST")
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
			w.Header().Set("X-Grits-Service-Worker-Hash", string(clientDirHash))
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

GET to /grits/v1/blob/{hash} gets blob data
PUT to /grits/v1/blob uploads a new blob, the response is the new address ("{hash}-{size}" format)
  as a JSON-encoded bare string
Or, PUT to /grits/v1/blob/{hash} which will do an early HTTP 204 return if the server already has that blob
POST to /grits/v1/lookup/{volume} accepts a bare JSON-encoded string in the request body,
  and returns a JSON-encoded array of pairs of strings: [$path, $resource_addr_at_that_path]

POST to /grits/v1/link/{volume} accepts a JSON-encoded array of maps
  {'path': {path}, 'addr': {addr}} indicating a bunch of resources to link into
  the given volume's storage atomically.


GET to /grits/v1/content/{volume}/{path} just serves file data (mainly for debugging)

*/

func (s *HTTPModule) setupRoutes() {
	// Deployment routes:
	s.Mux.HandleFunc("/", s.requestMiddleware(s.handleDeployment))

	// Content routes:
	s.Mux.HandleFunc("/grits/v1/blob", s.requestMiddleware(s.handleBlob))
	s.Mux.HandleFunc("/grits/v1/blob/", s.requestMiddleware(s.handleBlob))
	s.Mux.HandleFunc("/grits/v1/lookup/", s.requestMiddleware(s.handleLookup))
	s.Mux.HandleFunc("/grits/v1/link/", s.requestMiddleware(s.handleLink))

	s.Mux.HandleFunc("/grits/v1/content/", s.requestMiddleware(s.handleContent))

	// Client tooling routes:

	// Special handling for serving the Service Worker JS from the root
	s.Mux.HandleFunc("/grits/v1/service-worker.js", s.requestMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, s.Server.Config.ServerPath("client/service-worker.js"))
	}))

	// Handling client files with CORS enabled
	s.Mux.Handle("/grits/v1/client/", http.StripPrefix("/grits/v1/client/", s.requestMiddleware(http.FileServer(http.Dir(s.Server.Config.ServerPath("client"))).ServeHTTP)))

	s.HTTPServer.Handler = s.Mux
}

func (s *HTTPModule) handleBlob(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		s.handleBlobFetch(w, r) // Automatically skips sending the file for HEAD
	case http.MethodGet:
		s.handleBlobFetch(w, r)
	case http.MethodPut:
		s.handleBlobUpload(w, r)
	default:
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
	}
}

func (s *HTTPModule) handleBlobFetch(w http.ResponseWriter, r *http.Request) {
	tracker := NewPerformanceTracker(r)
	tracker.Start()
	defer tracker.End()

	// Extract the path part after /grits/v1/blob/
	fullPath := strings.TrimPrefix(r.URL.Path, "/grits/v1/blob/")
	if fullPath == "" {
		http.Error(w, "Missing file address", http.StatusBadRequest)
		return
	}

	// Split the path to separate hash and extension
	addrStr := fullPath
	var extension string

	// Check if there's an extension
	if lastDotIndex := strings.LastIndex(fullPath, "."); lastDotIndex != -1 {
		addrStr = fullPath[:lastDotIndex]
		extension = fullPath[lastDotIndex+1:]
	}

	// Strip out the size component if present (format: hash-size)
	if dashIndex := strings.LastIndex(addrStr, "-"); dashIndex != -1 {
		addrStr = addrStr[:dashIndex]
	}

	fileAddr, err := grits.NewBlobAddrFromString(addrStr)
	if err != nil {
		http.Error(w, "Invalid file address format", http.StatusBadRequest)
		return
	}

	var cachedFile grits.CachedFile

	if s.activeMirrorModule != nil {
		// This is fine whether the blob is local or remote; it'll muck up the mirror stats
		// a bit in some cases if it's local, but it's basically fine.
		cachedFile, err = s.activeMirrorModule.blobCache.Get(fileAddr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Can't find %s in mirror", fileAddr), http.StatusInternalServerError)
			return
		}
	} else {
		// No mirror, just get it from the local store
		cachedFile, err = s.Server.BlobStore.ReadFile(fileAddr)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
	}
	defer cachedFile.Release()

	// Set content type based on extension if provided
	if extension != "" {
		contentType := getContentTypeFromExtension(extension)
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
	}

	// Set content length
	w.Header().Set("Content-Length", fmt.Sprintf("%d", cachedFile.GetSize()))

	// Get reader and serve
	reader, err := cachedFile.Reader()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	// Serve the content
	http.ServeContent(w, r, filepath.Base(string(fileAddr)), time.Now(), reader)
	cachedFile.Touch()
}

// Helper to get content type from extension
func getContentTypeFromExtension(ext string) string {
	// Map of common extensions to MIME types
	mimeTypes := map[string]string{
		"html":  "text/html",
		"css":   "text/css",
		"js":    "application/javascript",
		"json":  "application/json",
		"png":   "image/png",
		"jpg":   "image/jpeg",
		"jpeg":  "image/jpeg",
		"svg":   "image/svg+xml",
		"woff":  "font/woff",
		"woff2": "font/woff2",
		"ttf":   "font/ttf",
		"eot":   "application/vnd.ms-fontobject",
	}

	return mimeTypes[strings.ToLower(ext)]
}

// handleBlobUpload handles PUT requests to /grits/v1/blob/ and /grits/v1/blob/{hash}
func (s *HTTPModule) handleBlobUpload(w http.ResponseWriter, r *http.Request) {
	// Check if we're in read-only mode
	if *s.Config.ReadOnly {
		http.Error(w, "Volume is read-only", http.StatusForbidden)
		return
	}

	// Extract hash from path if present
	blobPath := strings.TrimPrefix(r.URL.Path, "/grits/v1/blob")
	blobPath = strings.TrimPrefix(blobPath, "/")

	expectedHash := ""
	if blobPath != "" {
		// Path includes a hash, we'll use it for verification
		expectedHash = blobPath

		// Remove any extension if present
		if lastDotIndex := strings.LastIndex(expectedHash, "."); lastDotIndex != -1 {
			expectedHash = expectedHash[:lastDotIndex]
		}

		// Handle hash-size format
		//if dashIndex := strings.LastIndex(expectedHash, "-"); dashIndex != -1 {
		//	expectedHash = expectedHash[:dashIndex]
		//}

		// Validate the hash format
		_, err := grits.NewBlobAddrFromString(expectedHash)
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid blob address format: %s", expectedHash), http.StatusBadRequest)
			return
		}

		// Early optimization: Check if we already have this blob
		existingCf, _ := s.Server.BlobStore.ReadFile(grits.BlobAddr(expectedHash))
		if existingCf != nil {
			existingCf.Release()
			// We already have this blob, no need to upload again
			log.Printf("Blob %s already exists, skipping upload", expectedHash)
			w.WriteHeader(http.StatusNoContent)
			json.NewEncoder(w).Encode(expectedHash)
			return
		}
	}

	// Create a temporary file for the upload
	tmpFile, err := os.CreateTemp("", "blob-upload-*")
	if err != nil {
		log.Printf("Failed to create temporary file: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up the file afterwards

	// Read the request body and write it to the temporary file
	_, err = io.Copy(tmpFile, r.Body)
	tmpFile.Close() // Close the file now that we're done writing
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Add the file to the blob store
	cachedFile, err := s.Server.BlobStore.AddLocalFile(tmpPath)
	if err != nil {
		log.Printf("Failed to add file to blob store: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get the actual hash of the uploaded content
	actualHash := cachedFile.GetAddress()

	// Verification step when expectedHash is provided
	if expectedHash != "" && expectedHash != string(actualHash) {
		// Clean up temporary resources and return error
		cachedFile.Release()

		http.Error(w, fmt.Sprintf(
			"Hash mismatch: expected %s but got %s",
			expectedHash, actualHash),
			http.StatusBadRequest)
		return
	}

	// Instead of immediately releasing the reference, hold it and set up
	// a delayed release after 5 minutes
	go func(file grits.CachedFile) {
		time.Sleep(5 * time.Minute)
		// Then release it, allowing GC to potentially clean it up if no other references exist
		file.Release()
		log.Printf("Released temporary reference to %s", file.GetAddress())
	}(cachedFile)

	// Log that we're holding a temporary reference
	log.Printf("Holding temporary reference to %s for 5 minutes", cachedFile.GetAddress())

	// Return the hash of the uploaded blob
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(actualHash)
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

	pathNodePairs, partialResult, err := volume.LookupFull([]string{lookupPath})
	if err != nil {
		http.Error(w, fmt.Sprintf("Lookup failed: %v", err), http.StatusNotFound)
		return
	}

	// Transform to the format expected by the client
	response := make([][]any, len(pathNodePairs))
	for i, pair := range pathNodePairs {
		node := pair.Node
		metadataHash := node.MetadataBlob().GetAddress()
		contentHash := node.ExportedBlob().GetAddress()
		contentSize := node.ExportedBlob().GetSize()

		response[i] = []any{
			pair.Path,
			metadataHash,
			contentHash,
			contentSize,
		}

		// Release the reference we took in LookupFull
		node.Release()
	}

	w.Header().Set("Content-Type", "application/json")
	if !partialResult {
		// Set 200 OK status, if we found the whole path
		w.WriteHeader(http.StatusOK)
	} else {
		// Set 207 Multi-Status to indicate partial success
		w.WriteHeader(http.StatusMultiStatus)
	}

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

	var allLinkRequests []*grits.LinkRequest

	if err := json.NewDecoder(r.Body).Decode(&allLinkRequests); err != nil {
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

	pathNodePairs, err := volume.MultiLink(allLinkRequests, true)
	if err != nil {
		log.Printf("HTTP API MultiLink() failed: %v", err)
		http.Error(w, fmt.Sprintf("Link failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Checkpoint the volume after all links are performed
	err = volume.Checkpoint()
	if err != nil {
		log.Printf("Failed to checkpoint %s: %v", volume, err)
		http.Error(w, fmt.Sprintf("Failed to checkpoint %s: %v", volume, err), http.StatusInternalServerError)
		return
	}

	// Transform to the format expected by the client (same as lookup endpoint)
	response := make([][]any, len(pathNodePairs))
	for i, pair := range pathNodePairs {
		node := pair.Node
		metadataHash := node.MetadataBlob().GetAddress()
		contentHash := node.ExportedBlob().GetAddress()
		contentSize := node.ExportedBlob().GetSize()

		response[i] = []any{
			pair.Path,
			metadataHash,
			contentHash,
			contentSize,
		}

		// Release the reference we took in MultiLink
		node.Release()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
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
	case http.MethodGet, http.MethodHead:
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

func handleNamespaceGet(_ grits.BlobStore, volume Volume, path string, w http.ResponseWriter, r *http.Request) {
	tracker := NewPerformanceTracker(r)
	tracker.Start()
	defer tracker.End()

	log.Printf("Received %s request for file: %s\n", r.Method, path)

	tracker.Step("Looking up resource in volume")
	// Look up the resource in the volume to get its address

	pathNodes, isPartial, err := volume.LookupFull([]string{path})
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal error: %v", err), http.StatusInternalServerError)
		return
	}
	if isPartial {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if len(pathNodes) <= 0 {
		http.Error(w, "No nodes returned", http.StatusInternalServerError)
		return
	}
	defer func() {
		for _, node := range pathNodes {
			node.Node.Release()
		}
	}()

	tracker.Step("Checking index.html")
	node := pathNodes[len(pathNodes)-1].Node
	if _, ok := node.(*grits.TreeNode); ok {
		// We have a directory, try index.html instead
		// FIXME more flexible
		indexPath := strings.TrimRight(path, "/") + "/index.html"
		indexNode, err := volume.LookupNode(indexPath)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		path = indexPath
		node = indexNode
		pathNodes = append(pathNodes, &grits.PathNodePair{Path: indexPath, Node: indexNode})
	}

	tracker.Step("Building path metadata")
	pathMetadata := make([]map[string]any, 0, len(pathNodes))
	for _, pathNode := range pathNodes {
		// Extract just the component name from the full path
		pathComponent := ""
		if pathNode.Path != "" { // Skip this logic for root
			pathParts := strings.Split(pathNode.Path, "/")
			if len(pathParts) > 0 {
				pathComponent = pathParts[len(pathParts)-1]
			}
		}

		pathMetadata = append(pathMetadata, map[string]any{
			"path":         pathComponent,
			"metadataHash": pathNode.Node.MetadataBlob().GetAddress(),
			"contentHash":  pathNode.Node.ExportedBlob().GetAddress(),
			"contentSize":  pathNode.Node.ExportedBlob().GetSize(),
		})
	}

	// Encode the entire array as JSON and set in a single header
	jsonData, err := json.Marshal(pathMetadata)
	if err != nil {
		// Handle error appropriately
		log.Printf("Failed to encode path metadata: %v", err)
	} else {
		w.Header().Set("X-Path-Metadata-JSON", string(jsonData))
	}

	// Use the address hash as the ETag
	etag := fmt.Sprintf("\"%s\"", node.MetadataBlob().GetAddress())
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

	// Check if there's an extension
	//if lastDotIndex := strings.LastIndex(path, "."); lastDotIndex != -1 {
	//	extension := path[lastDotIndex+1:]
	//	contentType := getContentTypeFromExtension(extension)
	//	if contentType != "" {
	//		w.Header().Set("Content-Type", contentType)
	//	}
	//}

	if r.Method == http.MethodHead {
		// For HEAD requests, we've already set all needed headers
		// No need to read the actual content
		w.WriteHeader(http.StatusOK)
		return
	}

	reader, err := node.ExportedBlob().Reader()
	if err != nil {
		http.Error(w, fmt.Sprintf("Can't read blob for %s: %v", path, err), http.StatusInternalServerError)
		return
	}
	defer func() {
		err := reader.Close()
		if err != nil {
			log.Printf("Error closing reader for %s: %v", path, err)
		}
	}()

	http.ServeContent(w, r, filepath.Base(path), time.Now(), reader)
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
	contentCf, err := bs.AddDataBlock(data)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to store %s content", path), http.StatusInternalServerError)
		return
	}
	defer contentCf.Release()

	// Create metadata for the content
	metadataNode, err := volume.CreateBlobNode(contentCf.GetAddress(), contentCf.GetSize())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create metadata for %s", path), http.StatusInternalServerError)
		return
	}
	defer metadataNode.Release()

	// Link using the metadata address
	log.Printf("Linking %s to %s", path, metadataNode.Metadata().ContentHash)
	err = volume.LinkByMetadata(path, metadataNode.MetadataBlob().GetAddress())
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

	err := volume.LinkByMetadata(path, "")
	if err != nil {
		http.Error(w, "Failed to link file to namespace", http.StatusInternalServerError)
		return
	}

	// If the file was successfully deleted, you can return an appropriate success response
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File deleted successfully")
}

// Temporary helper function. As long as we (for now) have to do this outside of a remote volume
// abstraction, we might as well unify the definition of how to do it.

// CreateAndUploadMetadata creates a metadata blob for a content blob and uploads it to the server
// Returns the metadata blob address
func CreateAndUploadMetadata(volume Volume, contentCf grits.CachedFile, remoteUrl string) (grits.BlobAddr, error) {
	// Create a metadata node for the content
	contentNode, err := volume.CreateBlobNode(contentCf.GetAddress(), contentCf.GetSize())
	if err != nil {
		return "", err
	}
	defer contentNode.Release()

	// Get a reader for the metadata blob
	metadataReader, err := contentNode.MetadataBlob().Reader()
	if err != nil {
		return "", fmt.Errorf("couldn't create reader for metadata blob: %v", err)
	}
	defer metadataReader.Close()

	// Create a PUT request instead of using http.Post
	req, err := http.NewRequest(http.MethodPut, remoteUrl+"/blob/", metadataReader)
	if err != nil {
		return "", fmt.Errorf("failed to create request for metadata upload: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	// Send the request
	client := &http.Client{}
	metadataUploadResp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to upload metadata blob: %v", err)
	}
	defer metadataUploadResp.Body.Close()

	// Handle both 200 OK and 204 No Content responses
	if metadataUploadResp.StatusCode == http.StatusNoContent {
		// If we get a "No Content" response, the server already had this blob
		// Return the hash we already know
		return contentNode.MetadataBlob().GetAddress(), nil
	} else if metadataUploadResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload of metadata blob failed with status: %d", metadataUploadResp.StatusCode)
	}

	// For 200 OK responses, decode the hash from the response
	var uploadedMetadataHash grits.BlobAddr
	if err := json.NewDecoder(metadataUploadResp.Body).Decode(&uploadedMetadataHash); err != nil {
		return "", fmt.Errorf("failed to decode metadata blob upload response: %v", err)
	}

	// Verify the hash matches
	if uploadedMetadataHash != contentNode.MetadataBlob().GetAddress() {
		return "", fmt.Errorf("metadata blob hash mismatch. Expected: %s, Got: %s",
			contentNode.MetadataBlob().GetAddress(), uploadedMetadataHash)
	}

	return uploadedMetadataHash, nil
}
