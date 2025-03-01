package server

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"net/http"
	"strings"
)

/*
Here's your needed nginx config:

location /grits-[a-f0-9\.\-]+ {
    proxy_pass http://localhost:1787/$request_uri;
}

location /grits/ {
    proxy_pass http://localhost:1787/grits/;
}
*/

// ServiceWorkerModuleConfig holds the configuration for the service worker module.
type ServiceWorkerModuleConfig struct {
	PathMappings []PathMapping `json:"Paths"`
}

// PathMapping defines a mapping from a URL path to a volume and path in storage.
type PathMapping struct {
	URLPath    string `json:"urlPrefix"`
	Volume     string `json:"volume"`
	VolumePath string `json:"path"`
}

// ServiceWorkerModule manages the configuration for service workers.
type ServiceWorkerModule struct {
	Config *ServiceWorkerModuleConfig
	Server *Server

	clientVolume Volume
}

// NewServiceWorkerModule initializes a new instance of the service worker module.
func NewServiceWorkerModule(server *Server, config *ServiceWorkerModuleConfig) (*ServiceWorkerModule, error) {
	swm := &ServiceWorkerModule{
		Config: config,
		Server: server,
	}

	log.Printf("Making volume")

	wvc := &WikiVolumeConfig{
		VolumeName: "_client",
	}
	wv, err := NewWikiVolume(wvc, server, true)
	if err != nil {
		return nil, err
	}

	err = wv.Link("", wv.GetEmptyDirAddr())
	if err != nil {
		return nil, err
	}

	err = wv.Link("client", wv.GetEmptyDirAddr())
	if err != nil {
		return nil, err
	}

	log.Printf("Loading client/serviceworker/grits-bootstrap.js")

	bootstrapCf, err := wv.AddBlob("client/serviceworker/grits-bootstrap.js")
	if err != nil {
		return nil, err
	}
	defer bootstrapCf.Release()

	log.Printf("Linking")
	err = wv.ns.Link("serviceworker", wv.GetEmptyDirAddr())
	if err != nil {
		return nil, err
	}

	// FIXME fix up the API pls
	err = wv.ns.LinkBlob("serviceworker/grits-bootstrap.js", bootstrapCf.GetAddress(), bootstrapCf.GetSize())
	if err != nil {
		return nil, err
	}

	log.Printf("Loading client/serviceworker/grits-serviceworker.js")

	swCf, err := wv.AddBlob("client/serviceworker/grits-serviceworker.js")
	if err != nil {
		return nil, err
	}
	defer swCf.Release()
	// FIXME fix up the API pls
	err = wv.ns.LinkBlob("serviceworker/grits-serviceworker.js", swCf.GetAddress(), swCf.GetSize())
	if err != nil {
		return nil, err
	}

	configBytes, err := json.Marshal(swm.Config)
	if err != nil {
		return nil, err
	}
	configCf, err := server.BlobStore.AddDataBlock(configBytes)
	if err != nil {
		return nil, err
	}
	defer configCf.Release()
	err = wv.ns.LinkBlob("serviceworker/grits-serviceworker-config.json", configCf.GetAddress(), configCf.GetSize())
	if err != nil {
		return nil, err
	}

	swm.clientVolume = wv

	server.AddModule(wv)
	server.AddVolume(wv)

	return swm, nil
}

func (swm *ServiceWorkerModule) getClientDirHash() string {
	clientDirNode, err := swm.clientVolume.LookupNode("serviceworker")
	if err != nil {
		return fmt.Sprintf("(error: %v)", err)
	}
	if clientDirNode == nil {
		return "(nil)"
	}
	defer clientDirNode.Release()

	return clientDirNode.Address().Hash
}

func (swm *ServiceWorkerModule) serveBootstrap(w http.ResponseWriter, r *http.Request) {
	// Set appropriate headers
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")

	// Get current hash of the serviceworker directory
	swDirHash := swm.getClientDirHash()

	// Look up TypedFileAddr for the service worker script
	swAddr, err := swm.clientVolume.Lookup("serviceworker/grits-serviceworker.js")
	if err != nil {
		http.Error(w, "Service worker not found", http.StatusInternalServerError)
		return
	}

	// Look up TypedFileAddr for the configuration
	configAddr, err := swm.clientVolume.Lookup("serviceworker/grits-serviceworker-config.json")
	if err != nil {
		http.Error(w, "Service worker config not found", http.StatusInternalServerError)
		return
	}

	// Read the bootstrap template
	bootstrapNode, err := swm.clientVolume.LookupNode("serviceworker/grits-bootstrap.js")
	if err != nil {
		http.Error(w, "Error looking up bootstrap script", http.StatusInternalServerError)
		return
	}
	defer bootstrapNode.Release()

	bootstrapCf := bootstrapNode.ExportedBlob()

	bootstrapData, err := bootstrapCf.Read(0, bootstrapCf.GetSize())
	if err != nil {
		http.Error(w, "Error loading bootstrap script", http.StatusInternalServerError)
		return
	}

	// Replace placeholders with actual values
	bootstrapStr := string(bootstrapData)
	bootstrapStr = strings.Replace(bootstrapStr, "{{SW_DIR_HASH}}", swDirHash, -1)
	bootstrapStr = strings.Replace(bootstrapStr, "{{SW_SCRIPT_HASH}}", swAddr.Hash, -1)
	bootstrapStr = strings.Replace(bootstrapStr, "{{SW_CONFIG_HASH}}", configAddr.Hash, -1)

	// Send the final script
	fmt.Fprint(w, bootstrapStr)
}

func (swm *ServiceWorkerModule) serveServiceWorker(w http.ResponseWriter, r *http.Request) {
	// Get the hash from query parameter instead of the URL path
	scriptHash := r.URL.Query().Get("hash")
	if scriptHash == "" {
		http.Error(w, "Missing script hash parameter", http.StatusBadRequest)
		return
	}

	log.Printf("Requested service worker with hash: %s", scriptHash)

	// Set appropriate headers for no caching during development
	if strings.HasSuffix(r.URL.Path, ".json") {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "application/javascript")
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Serve the service worker file from the blob store
	swAddr, err := grits.NewBlobAddrFromString(scriptHash)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid hash format: %s", scriptHash), http.StatusBadRequest)
		return
	}

	swCf, err := swm.Server.BlobStore.ReadFile(swAddr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Service worker script not found: %s", scriptHash), http.StatusNotFound)
		return
	}
	defer swCf.Release()

	swReader, err := swCf.Reader()
	if err != nil {
		http.Error(w, "Failed to read service worker script", http.StatusInternalServerError)
		return
	}
	defer swReader.Close()

	_, err = io.Copy(w, swReader)
	if err != nil {
		log.Printf("Error streaming service worker script: %v", err)
	}
}

func (swm *ServiceWorkerModule) Start() error {
	return nil
}

func (swm *ServiceWorkerModule) Stop() error {
	return nil
}

func (swm *ServiceWorkerModule) GetModuleName() string {
	return "serviceworker"
}
