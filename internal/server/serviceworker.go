package server

import (
	"encoding/json"
	"fmt"
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

You should also probably do the same for paths that are mapped in the service worker config,
otherwise things may get confusing if the service worker is ever not working.

Here's what's needed in your pages:

<script src="/grits-bootstrap.js" async></script>
*/

type ServiceWorkerModuleConfig struct {
	// No options
}

func (swm *ServiceWorkerModule) addDeploymentModule(module Module) {
	deployment, ok := module.(*DeploymentModule)
	if !ok {
		return
	}

	// Convert DeploymentConfig to internal PathMapping
	mapping := PathMapping{
		HostName:   deployment.Config.HostName,
		URLPath:    deployment.Config.UrlPath,
		Volume:     deployment.Config.Volume,
		VolumePath: deployment.Config.VolumePath,
	}

	// Add to internal path mappings
	swm.pathMappings = append(swm.pathMappings, &mapping)

	log.Printf("About to update service worker config")

	// Update the service worker configuration JSON
	swm.updateServiceWorkerConfig()
}

// PathMapping defines a mapping from a URL path to a volume and path in storage.
type PathMapping struct {
	HostName   string `json:"hostName"`
	URLPath    string `json:"urlPrefix"`
	Volume     string `json:"volume"`
	VolumePath string `json:"path"`
}

// ServiceWorkerModule manages the configuration for service workers.
type ServiceWorkerModule struct {
	Config *ServiceWorkerModuleConfig
	Server *Server

	clientVolume Volume
	pathMappings []*PathMapping
}

func (swm *ServiceWorkerModule) loadClientFiles(wv *WikiVolume) error {
	// Define the template file to load first
	templatePath := "client/GritsClient.js"

	log.Printf("Loading template file: %s", templatePath)
	templateCf, err := wv.AddBlob(templatePath)
	if err != nil {
		return fmt.Errorf("failed to load template file: %v", err)
	}
	defer templateCf.Release()

	// Read the template content
	templateData, err := templateCf.Read(0, templateCf.GetSize())
	if err != nil {
		return fmt.Errorf("failed to read template data: %v", err)
	}
	templateStr := string(templateData)

	// Process template for both versions
	moduleVersion, swVersion := processTemplate(templateStr)

	// List of files to generate
	fileContents := map[string]string{
		"GritsClient.js":    moduleVersion,
		"GritsClient-sw.js": swVersion,
	}

	// List of static files to load directly
	staticFiles := []string{
		"serviceworker/grits-bootstrap.js",
		"serviceworker/grits-serviceworker.js",
		"GritsClientTests.js",
		"MirrorManager-sw.js",
		"client-test.html",
	}

	// Handle template-generated files
	for filename, content := range fileContents {
		// Create a blob from the processed content
		contentBytes := []byte(content)
		contentCf, err := swm.Server.BlobStore.AddDataBlock(contentBytes)
		if err != nil {
			return fmt.Errorf("failed to create blob for %s: %v", filename, err)
		}
		defer contentCf.Release()

		// Create metadata and link the file
		contentMetadata, err := wv.CreateMetadata(contentCf)
		if err != nil {
			return fmt.Errorf("failed to create metadata for %s: %v", filename, err)
		}

		log.Printf("Linking generated file: %s", filename)
		err = wv.LinkByMetadata(filename, contentMetadata.GetAddress())
		if err != nil {
			return fmt.Errorf("failed to link %s: %v", filename, err)
		}
	}

	// Load static files
	for _, filename := range staticFiles {
		log.Printf("Loading static file: client/%s", filename)

		fileCf, err := wv.AddBlob(fmt.Sprintf("client/%s", filename))
		if err != nil {
			return fmt.Errorf("failed to load %s: %v", filename, err)
		}
		defer fileCf.Release()

		log.Printf("Linking static file: %s", filename)
		err = wv.ns.LinkBlob(filename, fileCf.GetAddress(), fileCf.GetSize())
		if err != nil {
			return fmt.Errorf("failed to link %s: %v", filename, err)
		}
	}

	return nil
}

// Process the template to generate both versions
func processTemplate(templateStr string) (moduleVersion, swVersion string) {
	// Find marker positions
	moduleStart := strings.Index(templateStr, "// %MODULE%")
	serviceWorkerStart := strings.Index(templateStr, "// %SERVICEWORKER%")

	// Base content is everything up to the first marker
	baseContent := templateStr
	if moduleStart != -1 {
		baseContent = templateStr[:moduleStart]
	}

	// Extract module section
	moduleSection := ""
	if moduleStart != -1 && serviceWorkerStart != -1 {
		moduleSection = templateStr[moduleStart:serviceWorkerStart]
	} else if moduleStart != -1 {
		moduleSection = templateStr[moduleStart:]
	}

	// Extract service worker section
	swSection := ""
	if serviceWorkerStart != -1 {
		swSection = templateStr[serviceWorkerStart:]
		// Remove the comment marker from the self.GritsClient line
		swSection = strings.Replace(swSection, "//self.GritsClient", "self.GritsClient", 1)
	}

	// Create the two versions
	moduleVersion = baseContent + moduleSection
	swVersion = baseContent + swSection

	return moduleVersion, swVersion
}

// Replace the client files initialization in NewServiceWorkerModule
func NewServiceWorkerModule(server *Server, config *ServiceWorkerModuleConfig) (*ServiceWorkerModule, error) {
	swm := &ServiceWorkerModule{
		Config: config,
		Server: server,
	}

	log.Printf("Creating client volume")

	wvc := &WikiVolumeConfig{
		VolumeName: "client",
	}
	wv, err := NewWikiVolume(wvc, server, true)
	if err != nil {
		return nil, err
	}

	// Initialize the directory structure
	err = wv.Link("", wv.GetEmptyDirAddr())
	if err != nil {
		return nil, err
	}

	err = wv.Link("client", wv.GetEmptyDirAddr())
	if err != nil {
		return nil, err
	}

	err = wv.ns.Link("serviceworker", wv.GetEmptyDirAddr())
	if err != nil {
		return nil, err
	}

	// Load and process client files - replace the old clientFiles loop
	err = swm.loadClientFiles(wv)
	if err != nil {
		return nil, fmt.Errorf("failed to load client files: %v", err)
	}

	swm.clientVolume = wv

	server.AddModule(wv)
	server.AddVolume(wv)

	server.AddModuleHook(swm.addDeploymentModule)

	swm.updateServiceWorkerConfig()

	return swm, nil
}

func (swm *ServiceWorkerModule) updateServiceWorkerConfig() error {
	configBytes, err := json.Marshal(swm.pathMappings)
	if err != nil {
		return err
	}

	configCf, err := swm.Server.BlobStore.AddDataBlock(configBytes)
	if err != nil {
		return err
	}
	defer configCf.Release()

	configMetadata, err := swm.clientVolume.CreateMetadata(configCf)
	if err != nil {
		return err
	}

	err = swm.clientVolume.LinkByMetadata("serviceworker/grits-serviceworker-config.json", configMetadata.GetAddress())
	if err != nil {
		return err
	}

	log.Printf("New service worker config all linked up")

	return nil
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

func (swm *ServiceWorkerModule) serveTemplate(w http.ResponseWriter, r *http.Request) {
	// Determine which file to serve based on URL path
	var templatePath string
	if strings.HasSuffix(r.URL.Path, "grits-bootstrap.js") {
		templatePath = "serviceworker/grits-bootstrap.js"
		w.Header().Set("Content-Type", "application/javascript")
	} else if strings.HasSuffix(r.URL.Path, "grits-serviceworker.js") {
		templatePath = "serviceworker/grits-serviceworker.js"
		w.Header().Set("Content-Type", "application/javascript")
	} else {
		http.Error(w, "Unknown template file requested", http.StatusBadRequest)
		return
	}

	// Set appropriate headers
	w.Header().Set("Cache-Control", "no-cache")

	// Get current hash values for template substitution
	swDirHash := swm.getClientDirHash()

	swAddr, err := swm.clientVolume.Lookup("serviceworker/grits-serviceworker.js")
	if err != nil {
		http.Error(w, "Service worker not found", http.StatusInternalServerError)
		return
	}

	configAddr, err := swm.clientVolume.Lookup("serviceworker/grits-serviceworker-config.json")
	if err != nil {
		http.Error(w, "Service worker config not found", http.StatusInternalServerError)
		return
	}

	// Read the template file
	templateNode, err := swm.clientVolume.LookupNode(templatePath)
	if err != nil {
		http.Error(w, "Error looking up template file", http.StatusInternalServerError)
		return
	}
	defer templateNode.Release()

	templateCf := templateNode.ExportedBlob()
	templateData, err := templateCf.Read(0, templateCf.GetSize())
	if err != nil {
		http.Error(w, "Error loading template file", http.StatusInternalServerError)
		return
	}

	// Perform the substitutions
	templateStr := string(templateData)
	templateStr = strings.Replace(templateStr, "{{SW_DIR_HASH}}", swDirHash, -1)
	templateStr = strings.Replace(templateStr, "{{SW_SCRIPT_HASH}}", swAddr.Hash, -1)
	templateStr = strings.Replace(templateStr, "{{SW_CONFIG_HASH}}", configAddr.Hash, -1)

	// Send the processed template
	fmt.Fprint(w, templateStr)
}

func (swm *ServiceWorkerModule) serveConfig(w http.ResponseWriter, r *http.Request) {
	log.Printf("Serve config")

	// Get the hash from query parameter
	requestedHash := r.URL.Query().Get("dirHash")
	if requestedHash == "" {
		http.Error(w, "Missing hash parameter", http.StatusBadRequest)
		return
	}

	log.Printf("  got hash")

	// Eh, whatever... this is maybe worth worrying about in the long run

	// Validate hash against current config hash
	//currentDir, err := swm.clientVolume.Lookup("serviceworker")
	//if err != nil {
	//	http.Error(w, "Config file not found", http.StatusInternalServerError)
	//	return
	//}

	//log.Printf("  validated")

	//if currentDir.Hash != requestedHash {
	//	http.Error(w, "Hash mismatch - config has been updated", http.StatusBadRequest)
	//	return
	//}

	//log.Printf("  matched")

	// Serve the config file
	w.Header().Set("Content-Type", "application/json")

	currentConfig, err := swm.clientVolume.LookupNode("serviceworker/grits-serviceworker-config.json")
	if err != nil {
		http.Error(w, "Can't load config", http.StatusInternalServerError)
		return
	}
	defer currentConfig.Release()

	configReader, err := currentConfig.ExportedBlob().Reader()
	if err != nil {
		http.Error(w, "Can't read config", http.StatusInternalServerError)
		return
	}

	log.Printf("  ready to copy")

	_, err = io.Copy(w, configReader)
	if err != nil {
		log.Printf("Error streaming client config: %v", err)
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

func (swm *ServiceWorkerModule) GetConfig() interface{} {
	return swm.Config
}
