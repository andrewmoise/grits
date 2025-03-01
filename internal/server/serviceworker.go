package server

import (
	"encoding/json"
	"fmt"
	"log"
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

func (swm *ServiceWorkerModule) Start() error {
	return nil
}

func (swm *ServiceWorkerModule) Stop() error {
	return nil
}

func (swm *ServiceWorkerModule) GetModuleName() string {
	return "serviceworker"
}
