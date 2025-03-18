package server

import (
	"grits/internal/grits"
	"log"
)

// PinConfig represents the configuration for a pin module
type PinConfig struct {
	Volume string `json:"volume"`
	Path   string `json:"path"`
}

// PinModule keeps FileNodes in memory by maintaining references to them
type PinModule struct {
	Config *PinConfig
	Server *Server
	Volume Volume

	refCount map[string]int
}

// NewPinModule creates a new instance of PinModule
func NewPinModule(server *Server, config *PinConfig) *PinModule {
	pm := &PinModule{
		Config:   config,
		Server:   server,
		refCount: make(map[string]int),
	}

	return pm
}

// Start implements the Module interface
func (pm *PinModule) Start() error {
	log.Printf("PinModule: Starting")
	// Initial setup was done in constructor
	return nil
}

func (pm *PinModule) Stop() error {
	log.Printf("PinModule: Stopping and releasing all pinned nodes")

	for metadataHash := range pm.refCount {
		node, err := pm.Volume.GetFileNode(grits.NewBlobAddr(metadataHash))
		if err != nil {
			log.Printf("Error getting node for cleanup: %v", err)
			continue // Skip but continue with others
		}
		node.Release()
	}

	// Clear the map
	pm.refCount = make(map[string]int)
	return nil
}

// GetModuleName implements the Module interface
func (pm *PinModule) GetModuleName() string {
	return "pin"
}

func (pm *PinModule) GetConfig() interface{} {
	return pm.Config
}
