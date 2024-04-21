package server

import (
	"encoding/json"
	"log"
	"net/http"
)

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
}

// NewServiceWorkerModule initializes a new instance of the service worker module.
func NewServiceWorkerModule(server *Server, config *ServiceWorkerModuleConfig) *ServiceWorkerModule {
	swm := &ServiceWorkerModule{
		Config: config,
		Server: server,
	}

	server.AddModuleHook(swm.setupRoutes)

	return swm
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

// setupRoutes sets up the routes for serving the service worker and its configuration.
func (swm *ServiceWorkerModule) setupRoutes(module Module) {
	log.Printf("We do hook for %s\n", module.GetModuleName())

	httpModule, ok := module.(*HTTPModule)
	if !ok {
		return
	}

	log.Printf("We set up routes\n")

	httpModule.Mux.HandleFunc("/grits/v1/serviceworker/config", swm.serveConfig)

	// Add routes for serving serviceworker.js and bootstrap.js
	httpModule.Mux.HandleFunc("/serviceworker.js", func(w http.ResponseWriter, r *http.Request) {
		swm.serveJSFile(w, r, "client/serviceworker/serviceworker.js")
	})
	httpModule.Mux.HandleFunc("/bootstrap.js", func(w http.ResponseWriter, r *http.Request) {
		swm.serveJSFile(w, r, "client/serviceworker/bootstrap.js")
	})
}

// serveConfig handles requests for the service worker configuration.
func (swm *ServiceWorkerModule) serveConfig(w http.ResponseWriter, r *http.Request) {
	// Serialize and send the configuration to the client.
	json.NewEncoder(w).Encode(swm.Config)
}

// serveJSFile serves a JavaScript file with appropriate caching and content type headers.
func (swm *ServiceWorkerModule) serveJSFile(w http.ResponseWriter, r *http.Request, filePath string) {
	// Set headers to instruct the client to revalidate the file every time and set MIME type to JavaScript
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/javascript")

	filePath = swm.Server.Config.ServerPath(filePath)
	log.Printf("We attempt to serve bootstrapping %s\n", filePath)

	// Serve the file from the given path
	http.ServeFile(w, r, swm.Server.Config.ServerPath(filePath))
}
