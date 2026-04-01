package gritsd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"golang.org/x/sync/singleflight"
	"grits/internal/grits"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/////
// Configuration constants
/////

const (
	DefaultMaxUploadSize = 100 * 1024 * 1024
)

type limitedReader struct {
	r         io.Reader
	remaining int64
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if lr.remaining <= 0 {
		return 0, fmt.Errorf("request body too large")
	}
	if int64(len(p)) > lr.remaining {
		p = p[:lr.remaining]
	}
	n, err := lr.r.Read(p)
	lr.remaining -= int64(n)
	return n, err
}

func newLimitedReader(r io.Reader, maxSize int64) io.Reader {
	return &limitedReader{r: r, remaining: maxSize}
}

/////
// Module config and struct
/////

type HTTPModuleConfig struct {
	ThisPort int `json:"thisPort"`

	EnableTls bool  `json:"enableTLS,omitempty"`
	ReadOnly  *bool `json:"readOnly,omitempty"`

	// TLS certificate options.
	// AutoCertificate: acquire and renew via the certbot helper binary.
	AutoCertificate bool   `json:"autoCertificate,omitempty"`
	CertbotEmail    string `json:"certbotEmail,omitempty"`

	// Manual config: provide paths to existing cert and key files.
	CertPath string `json:"certPath,omitempty"`
	KeyPath  string `json:"keyPath,omitempty"`

	// Which volume holds deployed content. Defaults to "sites".
	// Content is served from {volume}/{hostname}/content/{path}.
	ContentVolume string `json:"contentVolume,omitempty"`

	MaxUploadSize int64 `json:"maxUploadSize,omitempty"`
}

type HTTPModule struct {
	Config *HTTPModuleConfig
	Server *Server

	HTTPServer *http.Server
	Mux        *http.ServeMux

	serviceWorkerModule *ServiceWorkerModule
	activeMirrorModule  *MirrorModule

	refHolder *ReferenceHolder

	// Dynamic TLS certificate management.
	// certCache holds the most recently loaded cert per hostname.
	certMu    sync.RWMutex
	certGroup singleflight.Group

	certCache map[string]*tls.Certificate // hostname → cert
	// renewalStopFns holds a cancel func per hostname renewal goroutine.
	renewalMu      sync.Mutex
	renewalStopFns map[string]context.CancelFunc

	stopCh chan struct{}
}

func (*HTTPModule) GetModuleName() string {
	return "http"
}

func (*HTTPModule) GetDependencies() []*Dependency {
	return []*Dependency{
		{
			ModuleType: "peer",
			Type:       DependOptional,
		},
	}
}

func (m *HTTPModule) GetConfig() any {
	return m.Config
}

func NewHTTPModule(server *Server, config *HTTPModuleConfig) (*HTTPModule, error) {
	if config.ThisPort < 1 || config.ThisPort > 65535 {
		return nil, fmt.Errorf("invalid port: must be between 1 and 65535")
	}
	if config.ReadOnly == nil {
		readOnly := true
		config.ReadOnly = &readOnly
	}
	if config.MaxUploadSize <= 0 {
		config.MaxUploadSize = DefaultMaxUploadSize
	}
	if config.ContentVolume == "" {
		config.ContentVolume = "sites"
	}
	if config.EnableTls {
		if config.AutoCertificate && config.CertbotEmail == "" {
			return nil, fmt.Errorf("certbotEmail is required when autoCertificate is enabled")
		}
		if !config.AutoCertificate && (config.CertPath == "" || config.KeyPath == "") {
			return nil, fmt.Errorf("certPath and keyPath are required when enableTLS is true and autoCertificate is false")
		}
	}

	mux := http.NewServeMux()
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", config.ThisPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	m := &HTTPModule{
		Config:         config,
		Server:         server,
		HTTPServer:     httpServer,
		Mux:            mux,
		refHolder:      NewReferenceHolder(45 * time.Second),
		certCache:      make(map[string]*tls.Certificate),
		renewalStopFns: make(map[string]context.CancelFunc),
		stopCh:         make(chan struct{}),
	}

	if config.EnableTls {
		httpServer.TLSConfig = &tls.Config{
			GetCertificate: m.getCertificate,
		}
	}

	m.setupRoutes()

	server.AddModuleHook(m.addServiceWorkerModule)
	server.AddModuleHook(m.addMirrorModule)

	return m, nil
}

/////
// Start / Stop
/////

func (hm *HTTPModule) Start() error {
	hm.refHolder.Start()

	if hm.Config.EnableTls && hm.Config.AutoCertificate {
		// Scan content volume and acquire certs for all known hostnames.
		if err := hm.initCertsFromContentVolume(); err != nil {
			// Non-fatal: log and continue; certs will be acquired on first request.
			log.Printf("HTTP: warning: initial cert scan failed: %v", err)
		}
	} else if hm.Config.EnableTls && !hm.Config.AutoCertificate {
		// Manual cert — load it once into the cache under a wildcard key.
		cert, err := tls.LoadX509KeyPair(hm.Config.CertPath, hm.Config.KeyPath)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificate: %v", err)
		}
		hm.certMu.Lock()
		hm.certCache["*"] = &cert
		hm.certMu.Unlock()
	}

	preopenedListenersMutex.Lock()
	listener, hasPreopened := preopenedListeners[hm.Config.ThisPort]
	preopenedListenersMutex.Unlock()

	go func() {
		var err error
		if hm.Config.EnableTls {
			tlsCfg := hm.HTTPServer.TLSConfig.Clone()
			if hasPreopened {
				tlsListener := tls.NewListener(listener, tlsCfg)
				err = hm.HTTPServer.Serve(tlsListener)
			} else {
				ln, listenErr := net.Listen("tcp", hm.HTTPServer.Addr)
				if listenErr != nil {
					log.Fatalf("HTTP: failed to listen on %s: %v", hm.HTTPServer.Addr, listenErr)
				}
				tlsListener := tls.NewListener(ln, tlsCfg)
				err = hm.HTTPServer.Serve(tlsListener)
			}
		} else {
			if hasPreopened {
				err = hm.HTTPServer.Serve(listener)
			} else {
				err = hm.HTTPServer.ListenAndServe()
			}
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP: server error: %v", err)
		}
	}()

	time.Sleep(250 * time.Millisecond)

	if grits.DebugHttp {
		log.Printf("HTTP module started on %s (TLS: %v)", hm.HTTPServer.Addr, hm.Config.EnableTls)
	}
	return nil
}

func (hm *HTTPModule) Stop() error {
	close(hm.stopCh)

	// Cancel all renewal goroutines.
	hm.renewalMu.Lock()
	for _, cancel := range hm.renewalStopFns {
		cancel()
	}
	hm.renewalMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := hm.HTTPServer.Shutdown(ctx)
	if err != nil {
		log.Printf("HTTP: shutdown error: %v", err)
	}

	hm.refHolder.Stop()
	return err
}

/////
// Pre-opening ports before privilege drop
/////

var preopenedListeners = make(map[int]net.Listener)
var preopenedListenersMutex sync.Mutex

// PreopenPrivilegedPorts opens any privileged (<1024) HTTP ports before the
// process drops root privileges. Certificate acquisition is handled later by
// the HTTP module itself.

func PreopenPrivilegedPorts(serverConfig *grits.Config, rawModuleConfigs []json.RawMessage) error {
	preopenedListenersMutex.Lock()
	defer preopenedListenersMutex.Unlock()

	certdNeeded := false
	var certbotEmail string

	for _, rawConfig := range rawModuleConfigs {
		var baseConfig ModuleConfig
		if err := json.Unmarshal(rawConfig, &baseConfig); err != nil {
			continue
		}
		if baseConfig.Type != "http" {
			continue
		}

		var httpConfig HTTPModuleConfig
		if err := json.Unmarshal(rawConfig, &httpConfig); err != nil {
			log.Printf("Warning: failed to parse HTTP module config: %v", err)
			continue
		}

		// Open the port.
		addr := fmt.Sprintf(":%d", httpConfig.ThisPort)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to open port %d: %v", httpConfig.ThisPort, err)
		}
		preopenedListeners[httpConfig.ThisPort] = listener

		if grits.DebugHttp {
			log.Printf("Pre-opened port %d", httpConfig.ThisPort)
		}

		// Note if certd is needed.
		if httpConfig.EnableTls && httpConfig.AutoCertificate {
			certdNeeded = true
			certbotEmail = httpConfig.CertbotEmail
		}
	}

	if certdNeeded {
		certdPath := serverConfig.ServerPath("bin/certbot-helper")
		certBaseDir, err := filepath.Abs(serverConfig.ServerPath("var/certs/letsencrypt"))
		if err != nil {
			return fmt.Errorf("failed to resolve cert base dir: %v", err)
		}
		if err := PreopenCertDaemon(certdPath, certBaseDir, certbotEmail, serverConfig.RunAsUser); err != nil {
			return fmt.Errorf("failed to start cert daemon: %v", err)
		}
	}

	return nil
}

func (hm *HTTPModule) hostnameHasContent(hostname string) bool {
	volume := hm.Server.FindVolumeByName(hm.Config.ContentVolume)
	if volume == nil {
		return false
	}
	node, err := volume.LookupNode(hostname)
	if err != nil || node == nil {
		return false
	}
	defer node.Release()
	return node.Metadata().Type == grits.GNodeTypeDirectory
}

/////
// Module hooks
/////

func (hm *HTTPModule) addServiceWorkerModule(module Module) {
	swModule, ok := module.(*ServiceWorkerModule)
	if !ok {
		return
	}
	if hm.serviceWorkerModule != nil {
		log.Fatalf("Only one ServiceWorkerModule can be registered")
	}
	if grits.DebugHttp {
		log.Printf("Registering ServiceWorkerModule in HTTP module")
	}
	hm.serviceWorkerModule = swModule
	hm.Mux.HandleFunc("/grits-bootstrap.js", hm.requestMiddleware(swModule.serveTemplate))
	hm.Mux.HandleFunc("/grits-serviceworker.js", hm.requestMiddleware(swModule.serveTemplate))
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

/////
// Routes
/////

func (s *HTTPModule) setupRoutes() {
	s.Mux.HandleFunc("/", s.requestMiddleware(s.handleDeployedContent))

	s.Mux.HandleFunc("/grits/v1/blob", s.requestMiddleware(s.handleBlob))
	s.Mux.HandleFunc("/grits/v1/blob/", s.requestMiddleware(s.handleBlob))
	s.Mux.HandleFunc("/grits/v1/lookup/", s.requestMiddleware(s.handleLookup))
	s.Mux.HandleFunc("/grits/v1/link/", s.requestMiddleware(s.handleLink))
	s.Mux.HandleFunc("/grits/v1/content/", s.requestMiddleware(s.handleContent))

	s.Mux.HandleFunc("/grits/v1/service-worker.js", s.requestMiddleware(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, s.Server.Config.ServerPath("client/service-worker.js"))
	}))
	s.Mux.Handle("/grits/v1/client/", http.StripPrefix("/grits/v1/client/",
		s.requestMiddleware(http.FileServer(http.Dir(s.Server.Config.ServerPath("client"))).ServeHTTP)))

	s.HTTPServer.Handler = s.Mux
}

/////
// Middleware
/////

func (srv *HTTPModule) requestMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if grits.DebugHttp {
			log.Printf("Incoming request: %s %s (Proto: %s)", r.Method, r.URL.Path, r.Proto)
		}

		grits.DebugLogWithTime(grits.DebugHttpPerformance, r.URL.Path, "Request start\n")

		thisScheme := "http"
		if srv.Config.EnableTls {
			thisScheme = "https"
		}
		thisOrigin := fmt.Sprintf("%s://%s:%d", thisScheme, r.Host, srv.Config.ThisPort)

		if srv.activeMirrorModule != nil {
			originServer := fmt.Sprintf("%s://%s",
				srv.activeMirrorModule.Config.Protocol,
				srv.activeMirrorModule.Config.RemoteHost)
			w.Header().Set("Access-Control-Allow-Origin", originServer)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", thisOrigin)
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS, POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if strings.HasPrefix(r.URL.Path, "/grits/v1/blob/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if srv.serviceWorkerModule != nil {
			clientDirHash := srv.serviceWorkerModule.getClientDirHash()
			w.Header().Set("X-Grits-Service-Worker-Hash", string(clientDirHash))
		}

		grits.DebugLogWithTime(grits.DebugHttpPerformance, r.URL.Path, "Calling handler\n")
		next(w, r)
		grits.DebugLogWithTime(grits.DebugHttpPerformance, r.URL.Path, "Request complete\n")
	}
}

/////
// Content handlers
/////

func (s *HTTPModule) handleDeployedContent(w http.ResponseWriter, r *http.Request) {
	hostname := r.Host
	if h, _, err := net.SplitHostPort(hostname); err == nil {
		hostname = h
	}

	if !Validate("hostname", hostname) {
		log.Printf("HTTP: invalid hostname in request: %s", hostname)
		http.Error(w, "Invalid hostname", http.StatusBadRequest)
		return
	}

	urlPath := strings.TrimPrefix(r.URL.Path, "/")
	urlPath = strings.TrimRight(urlPath, "/")

	volumePath := path.Join(hostname, "content", urlPath)

	if !Validate("relativePath", volumePath) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	s.handleContentRequest(s.Config.ContentVolume, volumePath, w, r)
}

func (s *HTTPModule) handleBlob(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodHead:
		s.handleBlobFetch(w, r)
	case http.MethodGet:
		s.handleBlobFetch(w, r)
	case http.MethodPut:
		s.handleBlobUpload(w, r)
	default:
		http.Error(w, "Method not supported", http.StatusMethodNotAllowed)
	}
}

func (s *HTTPModule) handleBlobFetch(w http.ResponseWriter, r *http.Request) {
	fullPath := strings.TrimPrefix(r.URL.Path, "/grits/v1/blob/")
	if fullPath == "" {
		http.Error(w, "Missing file address", http.StatusBadRequest)
		return
	}

	addrStr := fullPath
	var extension string
	if lastDotIndex := strings.LastIndex(fullPath, "."); lastDotIndex != -1 {
		addrStr = fullPath[:lastDotIndex]
		extension = fullPath[lastDotIndex+1:]
	}
	if dashIndex := strings.LastIndex(addrStr, "-"); dashIndex != -1 {
		addrStr = addrStr[:dashIndex]
	}

	if !Validate("blobAddr", addrStr) {
		http.Error(w, "Invalid blob address format", http.StatusBadRequest)
		return
	}

	fileAddr, err := grits.NewBlobAddrFromString(addrStr)
	if err != nil {
		http.Error(w, "Invalid file address format", http.StatusBadRequest)
		return
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(fileAddr), "Blob fetch start\n")

	var cachedFile grits.CachedFile
	if s.activeMirrorModule != nil {
		cachedFile, err = s.activeMirrorModule.blobCache.Get(fileAddr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Can't find %s in mirror", fileAddr), http.StatusInternalServerError)
			return
		}
	} else {
		cachedFile, err = s.Server.BlobStore.ReadFile(fileAddr)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
	}
	defer cachedFile.Release()

	if extension != "" {
		if ct := getContentTypeFromExtension(extension); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", cachedFile.GetSize()))

	reader, err := cachedFile.Reader()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	http.ServeContent(w, r, filepath.Base(string(fileAddr)), time.Now(), reader)
	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(fileAddr), "Blob fetch complete\n")
}

func getContentTypeFromExtension(ext string) string {
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
	if *s.Config.ReadOnly {
		http.Error(w, "Volume is read-only", http.StatusForbidden)
		return
	}
	if r.ContentLength > 0 && r.ContentLength > s.Config.MaxUploadSize {
		http.Error(w, "Upload too large", http.StatusRequestEntityTooLarge)
		return
	}

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
		if !Validate("blobAddr", expectedHash) {
			http.Error(w, "Invalid blob address format", http.StatusBadRequest)
			return
		}

		grits.DebugLogWithTime(grits.DebugHttpPerformance, expectedHash, "Upload start (checking if exists)\n")

		// Early optimization: Check if we already have this blob
		existingCf, _ := s.Server.BlobStore.ReadFile(grits.BlobAddr(expectedHash))
		if existingCf != nil {
			existingCf.Release()
			// We already have this blob, no need to upload again
			if grits.DebugHttp {
				log.Printf("Blob %s already exists, skipping upload", expectedHash)
			}
			grits.DebugLogWithTime(grits.DebugHttpPerformance, expectedHash, "Upload skipped (already exists)\n")
			w.WriteHeader(http.StatusNoContent)
			json.NewEncoder(w).Encode(expectedHash)
			return
		}
	}

	if grits.DebugHttpPerformance {
		tag := expectedHash
		if tag == "" {
			tag = "upload"
		}
		grits.DebugLogWithTime(grits.DebugHttpPerformance, tag, "Creating temp file\n")
	}

	// Create a temporary file for the upload
	tmpFile, err := os.CreateTemp("", "blob-upload-*")
	if err != nil {
		log.Printf("Failed to create temporary file: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	limitedBody := newLimitedReader(r.Body, s.Config.MaxUploadSize)

	if grits.DebugHttpPerformance {
		tag := expectedHash
		if tag == "" {
			tag = "upload"
		}
		grits.DebugLogWithTime(grits.DebugHttpPerformance, tag, "Reading body\n")
	}

	// Read the request body and write it to the temporary file
	_, err = io.Copy(tmpFile, limitedBody)
	tmpFile.Close()
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		if strings.Contains(err.Error(), "too large") {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	if grits.DebugHttpPerformance {
		tag := expectedHash
		if tag == "" {
			tag = "upload"
		}
		grits.DebugLogWithTime(grits.DebugHttpPerformance, tag, "Adding to blob store\n")
	}

	// Add the file to the blob store
	cachedFile, err := s.Server.BlobStore.AddLocalFile(tmpPath)
	if err != nil {
		log.Printf("Failed to add file to blob store: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	actualHash := cachedFile.GetAddress()

	// Verification step when expectedHash is provided
	if expectedHash != "" && expectedHash != string(actualHash) {
		cachedFile.Release()
		http.Error(w, fmt.Sprintf("Hash mismatch: expected %s but got %s", expectedHash, actualHash), http.StatusBadRequest)
		return
	}

	// Instead of immediately releasing the reference, hold it and set up
	// a delayed release after 5 minutes
	s.refHolder.Hold(cachedFile, 5*time.Minute)

	// Log that we're holding a temporary reference
	if grits.DebugHttp {
		log.Printf("Holding temporary reference to %s for 5 minutes", cachedFile.GetAddress())
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(actualHash), "Upload complete\n")

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

	grits.DebugLogWithTime(grits.DebugHttpPerformance, lookupPath, "Lookup start\n")

	if !Validate("relativePath", lookupPath) {
		http.Error(w, "Invalid lookup path", http.StatusBadRequest)
		return
	}

	volumeName := strings.TrimPrefix(r.URL.Path, "/grits/v1/lookup/")
	if volumeName == "" {
		http.Error(w, "Volume name is required", http.StatusBadRequest)
		return
	}
	if !Validate("volumeName", volumeName) {
		http.Error(w, "Invalid volume name", http.StatusBadRequest)
		return
	}

	volume := s.Server.FindVolumeByName(volumeName)
	if volume == nil {
		http.Error(w, "Volume not found", http.StatusNotFound)
		return
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, lookupPath, "Calling LookupFull\n")

	lookupResponse, err := volume.LookupFull([]string{lookupPath})
	if err != nil {
		http.Error(w, fmt.Sprintf("Lookup failed: %v", err), http.StatusNotFound)
		return
	}

	// Hold refs so blobs don't get GC'd before client fetches them
	for _, pair := range lookupResponse.Paths {
		node, err := volume.GetFileNode(pair.Addr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error looking up %s: %v", pair.Addr, err), http.StatusInternalServerError)
			return
		}
		defer node.Release()
		s.refHolder.Hold(node.MetadataBlob(), 45*time.Second)
		if node.Metadata().Type == grits.GNodeTypeDirectory {
			contentBlob, err := node.ExportedBlob()
			if err != nil {
				http.Error(w, fmt.Sprintf("Couldn't load content: %v", err), http.StatusInternalServerError)
				return
			}
			s.refHolder.Hold(contentBlob, 45*time.Second)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if lookupResponse.IsPartial {
		w.WriteHeader(http.StatusMultiStatus)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	if err := json.NewEncoder(w).Encode(lookupResponse); err != nil {
		log.Printf("Failed to encode lookup response: %v", err)
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, lookupPath, "Lookup complete\n")
}

func (s *HTTPModule) handleLink(w http.ResponseWriter, r *http.Request) {
	if grits.DebugHttp {
		log.Printf("Handling link request\n")
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST is supported", http.StatusMethodNotAllowed)
		return
	}
	if *s.Config.ReadOnly {
		http.Error(w, "Volume is read-only", http.StatusForbidden)
		return
	}
	if r.ContentLength > 0 && r.ContentLength > s.Config.MaxUploadSize {
		http.Error(w, "Request too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Use limited reader for body
	// There's not a real obvious max size, but certainly 100 MB is too large
	limitedBody := newLimitedReader(r.Body, DefaultMaxUploadSize)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		if strings.Contains(err.Error(), "too large") {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
		}
		return
	}

	var allLinkRequests []*grits.LinkRequest
	if err := UnmarshalAndValidate(body, &allLinkRequests); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	volumeName := strings.TrimPrefix(r.URL.Path, "/grits/v1/link/")
	if volumeName == "" {
		http.Error(w, "Volume name is required", http.StatusBadRequest)
		return
	}
	if !Validate("volumeName", volumeName) {
		http.Error(w, "Invalid volume name", http.StatusBadRequest)
		return
	}

	volume := s.Server.FindVolumeByName(volumeName)
	if volume == nil {
		http.Error(w, fmt.Sprintf("Volume %s not found", volumeName), http.StatusNotFound)
		return
	}
	if volume.isReadOnly() {
		http.Error(w, fmt.Sprintf("Volume %s is read-only", volumeName), http.StatusForbidden)
		return
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, volumeName, "Link start\n")

	linkResponse, err := volume.MultiLink(allLinkRequests, true)
	if err != nil {
		log.Printf("HTTP API MultiLink() failed: %v", err)
		http.Error(w, fmt.Sprintf("Link failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Hold refs so blobs don't get GC'd before client fetches them
	for _, pair := range linkResponse.Paths {
		node, err := volume.GetFileNode(pair.Addr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Couldn't load node for %s: %v", pair.Path, err), http.StatusInternalServerError)
			return
		}
		defer node.Release()
		s.refHolder.Hold(node.MetadataBlob(), 45*time.Second)
		if node.Metadata().Type == grits.GNodeTypeDirectory {
			contentBlob, err := node.ExportedBlob()
			if err != nil {
				http.Error(w, fmt.Sprintf("Couldn't load content: %v", err), http.StatusInternalServerError)
				return
			}
			s.refHolder.Hold(contentBlob, 45*time.Second)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(linkResponse); err != nil {
		log.Printf("Failed to encode link response: %v", err)
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, volumeName, "Link complete\n")
}

func (s *HTTPModule) handleContent(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/grits/v1/content/")
	pathParts := strings.SplitN(p, "/", 2)
	if len(pathParts) < 2 {
		http.Error(w, "URL must include a volume name and path", http.StatusBadRequest)
		return
	}
	volumeName := pathParts[0]
	filePath := pathParts[1]

	if !Validate("volumeName", volumeName) {
		http.Error(w, "Invalid volume name", http.StatusBadRequest)
		return
	}

	s.handleContentRequest(volumeName, filePath, w, r)
}

func (s *HTTPModule) handleContentRequest(volumeName, filePath string, w http.ResponseWriter, r *http.Request) {
    filePath = strings.TrimRight(filePath, "/")

	if !Validate("relativePath", filePath) {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	volume := s.Server.FindVolumeByName(volumeName)
	if volume == nil {
		http.Error(w, fmt.Sprintf("Volume %s not found", volumeName), http.StatusNotFound)
		return
	}

	if grits.DebugHttp {
		log.Printf("Received request for file: %s\n", filePath)
		log.Printf("Method is %s\n", r.Method)
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handleNamespaceGet(volume, filePath, w, r)
	case http.MethodPut:
		if *s.Config.ReadOnly {
			http.Error(w, "Volume is read-only", http.StatusForbidden)
		} else {
			handleNamespacePut(s.Server.BlobStore, volume, filePath, w, r, s.Config.MaxUploadSize)
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

func handleNamespaceGet(volume Volume, path string, w http.ResponseWriter, r *http.Request) {
	if grits.DebugHttp {
		log.Printf("Received %s request for file: %s\n", r.Method, path)
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Namespace GET start\n")
	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Looking up in volume\n")
	// Look up the resource in the volume to get its address

	lookupResponse, err := volume.LookupFull([]string{path})
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal error: %v", err), http.StatusInternalServerError)
		return
	}
	if lookupResponse.IsPartial {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if len(lookupResponse.Paths) <= 0 {
		http.Error(w, "No nodes returned", http.StatusInternalServerError)
		return
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Getting leaf node\n")

	leafNode, err := volume.GetFileNode(lookupResponse.Paths[len(lookupResponse.Paths)-1].Addr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Can't read leaf node: %v", err), http.StatusInternalServerError)
		return
	}
	defer leafNode.Release()

	if leafNode.Metadata().Type == grits.GNodeTypeDirectory {
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "application/json") {
			// JS client wants the raw directory listing — fall through and serve
			// the directory's content blob (which is JSON).
		} else {
			// Browser request — try to serve index.html instead.
			indexPath := strings.TrimRight(path, "/") + "/index.html"
			indexNode, err := volume.LookupNode(indexPath)
			
			// Fail if we don't have an index.html to serve
			if err != nil {
				http.Error(w, "File not found", http.StatusNotFound)
				return
			}

			// Redirect if no trailing slash, so relative paths resolve correctly
			if !strings.HasSuffix(r.URL.Path, "/") {
				http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
				return
			}

			// Otherwise, just silently serve index.html
			path = indexPath
			leafNode = indexNode
			lookupResponse.Paths = append(lookupResponse.Paths, &grits.PathNodePair{
				Path: indexPath,
				Addr: indexNode.MetadataBlob().GetAddress(),
			})
		}
 	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Building path metadata\n")

	pathMetadata := make([]map[string]any, 0, len(lookupResponse.Paths))
	for _, pathResponse := range lookupResponse.Paths {
		pathComponent := ""
		if pathResponse.Path != "" { // Skip this logic for root
			pathParts := strings.Split(pathResponse.Path, "/")
			if len(pathParts) > 0 {
				pathComponent = pathParts[len(pathParts)-1]
			}
		}
		node, err := volume.GetFileNode(pathResponse.Addr)
		if err != nil {
			http.Error(w, fmt.Sprintf("Couldn't read %s: %v", pathComponent, err), http.StatusInternalServerError)
			return
		}
		defer node.Release()
		pathMetadata = append(pathMetadata, map[string]any{
			"path":         pathComponent,
			"metadataHash": node.MetadataBlob().GetAddress(),
			"contentHash":  node.Metadata().ContentHash,
			"contentSize":  node.Metadata().Size,
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
	etag := fmt.Sprintf("\"%s\"", leafNode.MetadataBlob().GetAddress())
	w.Header().Set("ETag", etag)

	// Tell browsers to revalidate every time
	w.Header().Set("Cache-Control", "no-cache")

	// Check If-None-Match header for conditional requests
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		// Resource hasn't changed, return 304 Not Modified
		w.WriteHeader(http.StatusNotModified)
		grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Returning 304 Not Modified\n")
		return
	}
	if r.Method == http.MethodHead {
		// For HEAD requests, we've already set all needed headers
		// No need to read the actual content
		w.WriteHeader(http.StatusOK)
		return
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Getting reader\n")

	blobContent, err := leafNode.ExportedBlob()
	if err != nil {
		http.Error(w, fmt.Sprintf("Can't read content for %s: %v", path, err), http.StatusInternalServerError)
		return
	}

	reader, err := blobContent.Reader()
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

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Serving content\n")

	http.ServeContent(w, r, filepath.Base(path), time.Now(), reader)
	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Namespace GET complete\n")
}

func handleNamespacePut(bs grits.BlobStore, volume Volume, path string, w http.ResponseWriter, r *http.Request, maxSize int64) {
	if grits.DebugHttp {
		log.Printf("Received PUT request for file: %s\n", path)
	}

	if path == "" || path == "/" {
		http.Error(w, "Cannot modify root of namespace", http.StatusForbidden)
		return
	}

	// Check content length
	if r.ContentLength > 0 && r.ContentLength > maxSize {
		http.Error(w, "Upload too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Read the file content from the request body with size limit
	limitedBody := newLimitedReader(r.Body, maxSize)
	data, err := io.ReadAll(limitedBody)
	if err != nil {
		if strings.Contains(err.Error(), "too large") {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		}
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
	if grits.DebugHttp {
		log.Printf("Linking %s to %s", path, metadataNode.Metadata().ContentHash)
	}

	err = volume.LinkByMetadata(path, metadataNode.MetadataBlob().GetAddress())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to link %s to namespace", path), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File linked successfully")
}

func handleNamespaceDelete(volume Volume, path string, w http.ResponseWriter) {
	if grits.DebugHttp {
		log.Printf("Received DELETE request for file: %s\n", path)
	}

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
	contentNode, err := volume.CreateBlobNode(contentCf.GetAddress(), contentCf.GetSize())
	if err != nil {
		return "", err
	}
	defer contentNode.Release()

	metadataReader, err := contentNode.MetadataBlob().Reader()
	if err != nil {
		return "", fmt.Errorf("couldn't create reader for metadata blob: %v", err)
	}
	defer metadataReader.Close()

	req, err := http.NewRequest(http.MethodPut, remoteUrl+"/blob/", metadataReader)
	if err != nil {
		return "", fmt.Errorf("failed to create request for metadata upload: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

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
