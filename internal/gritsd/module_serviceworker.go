package gritsd

import (
	"fmt"
	"grits/internal/grits"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

type ServiceWorkerModuleConfig struct {
	// No options
}

type ServiceWorkerModule struct {
	Config *ServiceWorkerModuleConfig
	Server *Server

	clientVolume Volume
}

func NewServiceWorkerModule(server *Server, config *ServiceWorkerModuleConfig) (*ServiceWorkerModule, error) {
	swm := &ServiceWorkerModule{
		Config: config,
		Server: server,
	}

	if grits.DebugHttp {
		log.Printf("Creating client volume")
	}

	wvc := &LocalVolumeConfig{
		VolumeName: "client",
	}
	wv, err := NewLocalVolume(wvc, server, true, false, false)
	if err != nil {
		return nil, err
	}

	emptyTreeNode, err := wv.CreateTreeNode()
	if err != nil {
		return nil, err
	}
	defer emptyTreeNode.Release()

	err = wv.LinkByMetadata("", emptyTreeNode.MetadataBlob().GetAddress())
	if err != nil {
		return nil, err
	}

	err = wv.LinkByMetadata("client", emptyTreeNode.MetadataBlob().GetAddress())
	if err != nil {
		return nil, err
	}

	err = wv.LinkByMetadata("serviceworker", emptyTreeNode.MetadataBlob().GetAddress())
	if err != nil {
		return nil, err
	}

	err = swm.loadClientFiles(wv)
	if err != nil {
		return nil, fmt.Errorf("failed to load client files: %v", err)
	}

	swm.clientVolume = wv

	server.AddModule(wv)
	server.AddVolume(wv)

	return swm, nil
}

func (swm *ServiceWorkerModule) loadClientFiles(wv *LocalVolume) error {
	templateFiles := []string{
		"client/GritsClient.js",
		"client/MirrorManager.js",
	}

	staticFiles := []string{
		"serviceworker/grits-bootstrap.js",
		"serviceworker/grits-serviceworker.js",
	}

	for _, templatePath := range templateFiles {
		if grits.DebugHttp {
			log.Printf("Loading template file: %s", templatePath)
		}
		templateCf, err := wv.AddBlob(templatePath)
		if err != nil {
			return fmt.Errorf("failed to load template file %s: %v", templatePath, err)
		}
		defer templateCf.Release()

		templateData, err := templateCf.Read(0, templateCf.GetSize())
		if err != nil {
			return fmt.Errorf("failed to read template data: %v", err)
		}

		moduleVersion, swVersion := processTemplate(string(templateData))

		baseName := filepath.Base(templatePath)
		fileContents := map[string]string{
			baseName: moduleVersion,
			strings.TrimSuffix(baseName, ".js") + "-sw.js": swVersion,
		}

		for filename, content := range fileContents {
			contentCf, err := swm.Server.BlobStore.AddDataBlock([]byte(content))
			if err != nil {
				return fmt.Errorf("failed to create blob for %s: %v", filename, err)
			}
			defer contentCf.Release()

			contentNode, err := wv.CreateBlobNode(contentCf.GetAddress(), contentCf.GetSize())
			if err != nil {
				return fmt.Errorf("failed to create metadata for %s: %v", filename, err)
			}
			defer contentNode.Release()

			if err := wv.LinkByMetadata(filename, contentNode.MetadataBlob().GetAddress()); err != nil {
				return fmt.Errorf("failed to link %s: %v", filename, err)
			}
		}
	}

	for _, filename := range staticFiles {
		if grits.DebugHttp {
			log.Printf("Loading static file: client/%s", filename)
		}
		fileCf, err := wv.AddBlob(fmt.Sprintf("client/%s", filename))
		if err != nil {
			return fmt.Errorf("failed to load %s: %v", filename, err)
		}
		defer fileCf.Release()

		blobNode, err := wv.CreateBlobNode(fileCf.GetAddress(), fileCf.GetSize())
		if err != nil {
			return err
		}
		defer blobNode.Release()

		if err := wv.LinkByMetadata(filename, blobNode.MetadataBlob().GetAddress()); err != nil {
			return fmt.Errorf("failed to link %s: %v", filename, err)
		}
	}

	return nil
}

func processTemplate(templateStr string) (moduleVersion, swVersion string) {
	lines := strings.Split(templateStr, "\n")
	var moduleLines, serviceWorkerLines []string
	commentRe := regexp.MustCompile(`^\s*//`)

	for _, line := range lines {
		isModule := strings.Contains(line, "%FOR MODULE%")
		isSW := strings.Contains(line, "%FOR SERVICEWORKER%")

		if isModule {
			moduleLines = append(moduleLines, line)
			if commentRe.MatchString(line) {
				serviceWorkerLines = append(serviceWorkerLines, line)
			} else {
				serviceWorkerLines = append(serviceWorkerLines, "// "+line)
			}
		} else if isSW {
			moduleLines = append(moduleLines, line)
			if commentRe.MatchString(line) {
				uncommented := regexp.MustCompile(`^(\s*)//`).ReplaceAllString(line, "$1")
				serviceWorkerLines = append(serviceWorkerLines, uncommented)
			} else {
				serviceWorkerLines = append(serviceWorkerLines, line)
			}
		} else {
			moduleLines = append(moduleLines, line)
			serviceWorkerLines = append(serviceWorkerLines, line)
		}
	}

	return strings.Join(moduleLines, "\n"), strings.Join(serviceWorkerLines, "\n")
}

func (swm *ServiceWorkerModule) getClientDirHash() grits.BlobAddr {
	node, err := swm.clientVolume.LookupNode("serviceworker")
	if err != nil {
		return grits.BlobAddr(fmt.Sprintf("(error: %v)", err))
	}
	if node == nil {
		return "(nil)"
	}
	defer node.Release()
	return node.Metadata().ContentHash
}

func (swm *ServiceWorkerModule) serveTemplate(w http.ResponseWriter, r *http.Request) {
	var templatePath string
	if strings.HasSuffix(r.URL.Path, "grits-bootstrap.js") {
		templatePath = "serviceworker/grits-bootstrap.js"
	} else if strings.HasSuffix(r.URL.Path, "grits-serviceworker.js") {
		templatePath = "serviceworker/grits-serviceworker.js"
	} else {
		http.Error(w, "Unknown file requested", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")

	swDirHash := swm.getClientDirHash()

	swNode, err := swm.clientVolume.LookupNode("serviceworker/grits-serviceworker.js")
	if err != nil {
		http.Error(w, "Service worker not found", http.StatusInternalServerError)
		return
	}
	defer swNode.Release()

	templateNode, err := swm.clientVolume.LookupNode(templatePath)
	if err != nil {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	defer templateNode.Release()

	templateCf, err := templateNode.ExportedBlob()
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	templateData, err := templateCf.Read(0, templateCf.GetSize())
	if err != nil {
		http.Error(w, "Error reading template", http.StatusInternalServerError)
		return
	}

	result := string(templateData)
	result = strings.ReplaceAll(result, "{{SW_DIR_HASH}}", string(swDirHash))
	result = strings.ReplaceAll(result, "{{SW_SCRIPT_HASH}}", string(swNode.Metadata().ContentHash))

	fmt.Fprint(w, result)
}

func (swm *ServiceWorkerModule) Start() error { return nil }
func (swm *ServiceWorkerModule) Stop() error  { return nil }

func (swm *ServiceWorkerModule) GetModuleName() string { return "serviceworker" }
func (*ServiceWorkerModule) GetDependencies() []*Dependency {
	return []*Dependency{}
}
func (swm *ServiceWorkerModule) GetConfig() any { return swm.Config }
