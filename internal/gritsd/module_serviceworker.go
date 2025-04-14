package gritsd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
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
	// List of template files to process
	templateFiles := []string{
		"client/GritsClient.js",
		"client/MirrorManager.js",
	}

	// List of static files to load directly (no processing needed)
	staticFiles := []string{
		"serviceworker/grits-bootstrap.js",
		"serviceworker/grits-serviceworker.js",
		"GritsClientTests.js",
		"client-test.html",
	}

	// Process each template file
	for _, templatePath := range templateFiles {
		log.Printf("Loading template file: %s", templatePath)
		templateCf, err := wv.AddBlob(templatePath)
		if err != nil {
			return fmt.Errorf("failed to load template file %s: %v", templatePath, err)
		}
		defer templateCf.Release()

		// Read the template content
		templateData, err := templateCf.Read(0, templateCf.GetSize())
		if err != nil {
			return fmt.Errorf("failed to read template data: %v", err)
		}
		templateStr := string(templateData)

		// Process template for both module and service worker versions
		moduleVersion, swVersion := processTemplate(templateStr)

		// Determine output filenames based on input template name
		baseName := filepath.Base(templatePath)
		moduleName := baseName
		swName := strings.TrimSuffix(baseName, ".js") + "-sw.js"

		// Create file content map
		fileContents := map[string]string{
			moduleName: moduleVersion,
			swName:     swVersion,
		}

		// Handle each processed file
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
	// Split the content into lines for processing
	lines := strings.Split(templateStr, "\n")

	// Prepare slices for each version
	var moduleLines, serviceWorkerLines []string

	commentRe := regexp.MustCompile(`^\s*//`)

	// Process each line
	for _, line := range lines {
		// Check if line contains module-specific marker
		isModuleSpecific := strings.Contains(line, "%FOR MODULE%")

		// Check if line contains service worker-specific marker
		isServiceWorkerSpecific := strings.Contains(line, "%FOR SERVICEWORKER%")

		if isModuleSpecific {
			// For module version, include the line without the marker
			moduleLines = append(moduleLines, line)

			// For service worker version, comment out this line if not already commented
			if matched := commentRe.MatchString(line); matched {
				// Already commented, just keep the line
				serviceWorkerLines = append(serviceWorkerLines, line)
			} else {
				// Comment it out
				serviceWorkerLines = append(serviceWorkerLines, "// "+line)
			}
		} else if isServiceWorkerSpecific {
			// For module version, keep it commented (as it already is)
			moduleLines = append(moduleLines, line)

			// For service worker version, uncomment the line if it starts with //
			if matched := commentRe.MatchString(line); matched {
				// Replace just the first // that appears after any leading whitespace
				uncommentedLine := regexp.MustCompile(`^(\s*)//`).ReplaceAllString(line, "$1")
				serviceWorkerLines = append(serviceWorkerLines, uncommentedLine)
			} else {
				// Already uncommented, just keep the line
				serviceWorkerLines = append(serviceWorkerLines, line)
			}
		} else {
			// Regular line, include in both versions
			moduleLines = append(moduleLines, line)
			serviceWorkerLines = append(serviceWorkerLines, line)
		}
	}

	// Join the lines back together
	moduleVersion = strings.Join(moduleLines, "\n")
	swVersion = strings.Join(serviceWorkerLines, "\n")

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

func (*ServiceWorkerModule) GetDependencies() []*Dependency {
	return []*Dependency{}
}

func (swm *ServiceWorkerModule) GetConfig() any {
	return swm.Config
}
