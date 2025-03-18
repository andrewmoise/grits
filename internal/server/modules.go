package server

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
)

// Module is an interface that all modules must implement.
type Module interface {
	Start() error
	Stop() error
	GetModuleName() string
	GetConfig() interface{}
}

// More details for storage modules:
type Volume interface {
	GetVolumeName() string
	Start() error
	Stop() error
	isReadOnly() bool
	Checkpoint() error

	Lookup(path string) (*grits.TypedFileAddr, error)
	LookupNode(path string) (grits.FileNode, error)
	LookupFull(name string) ([]*grits.PathNodePair, bool, error)
	GetFileNode(metadataAddr *grits.BlobAddr) (grits.FileNode, error)

	// FIXME - This whole API needs a bunch of cleanup
	CreateMetadata(grits.CachedFile) (grits.CachedFile, error)

	Link(path string, addr *grits.TypedFileAddr) error
	LinkByMetadata(path string, metadataAddr *grits.BlobAddr) error
	MultiLink([]*grits.LinkRequest) error

	ReadFile(*grits.TypedFileAddr) (grits.CachedFile, error)
	AddBlob(path string) (grits.CachedFile, error)
	AddOpenBlob(*os.File) (grits.CachedFile, error)
	AddMetadataBlob(*grits.GNodeMetadata) (grits.CachedFile, error)

	GetEmptyDirMetadataAddr() *grits.BlobAddr
	GetEmptyDirAddr() *grits.TypedFileAddr

	Cleanup() error

	RegisterWatcher(watcher grits.FileTreeWatcher)
	UnregisterWatcher(watcher grits.FileTreeWatcher)
}

// ModuleConfig represents a generic module configuration.
type ModuleConfig struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

func (s *Server) GetModules(name string) []Module {
	var matches []Module
	for _, module := range s.Modules {
		if module.GetModuleName() == name {
			matches = append(matches, module)
		}
	}
	return matches
}

func (s *Server) FindVolumeByName(name string) Volume {
	if volume, exists := s.Volumes[name]; exists {
		return volume
	}
	return nil // Volume not found
}

func (s *Server) AddModule(module Module) {
	s.Modules = append(s.Modules, module)
	// Call all hooks for the newly added module
	for _, hook := range s.moduleHooks {
		hook(module)
	}
}

func (s *Server) AddVolume(volume Volume) {
	s.Volumes[volume.GetVolumeName()] = volume
}

// AddModuleHook adds a new hook to be called whenever a new module is added.
func (s *Server) AddModuleHook(hook func(Module)) {
	log.Printf("We add module hook\n")

	s.moduleHooks = append(s.moduleHooks, hook)
	// Call the hook immediately for all existing modules
	for _, module := range s.Modules {
		hook(module)
	}
}

func (s *Server) LoadModules(rawModuleConfigs []json.RawMessage) error {
	log.Printf("Loading modules\n")

	for _, rawConfig := range rawModuleConfigs {
		var baseConfig ModuleConfig
		if err := json.Unmarshal(rawConfig, &baseConfig); err != nil {
			return fmt.Errorf("error unmarshalling base module config: %v", err)
		}

		switch baseConfig.Type {
		case "deployment":
			var deploymentConfig DeploymentConfig
			if err := json.Unmarshal(rawConfig, &deploymentConfig); err != nil {
				return fmt.Errorf("failed to unmarshal DeploymentModule config: %v", err)
			}

			module := NewDeploymentModule(s, &deploymentConfig)

			s.AddModule(module)

		case "http":
			var httpConfig HTTPModuleConfig
			if err := json.Unmarshal(rawConfig, &httpConfig); err != nil {
				return fmt.Errorf("failed to unmarshal HTTP module config: %v", err)
			}

			s.AddModule(NewHTTPModule(s, &httpConfig))

		case "mirror":
			var mirrorConfig MirrorModuleConfig
			if err := json.Unmarshal(rawConfig, &mirrorConfig); err != nil {
				return fmt.Errorf("failed to unmarshal Mirror module config: %v", err)
			}

			mirrorModule, err := NewMirrorModule(s, &mirrorConfig)
			if err != nil {
				return fmt.Errorf("failed to create Mirror module: %v", err)
			}
			s.AddModule(mirrorModule)

		case "mount":
			var mountConfig MountModuleConfig
			if err := json.Unmarshal(rawConfig, &mountConfig); err != nil {
				return fmt.Errorf("failed to unmarshal mount config: %v", err)
			}

			s.AddModule(NewMountModule(s, &mountConfig))

		case "origin":
			var originConfig OriginModuleConfig
			if err := json.Unmarshal(rawConfig, &originConfig); err != nil {
				return fmt.Errorf("failed to unmarshal Origin module config: %v", err)
			}

			originModule, err := NewOriginModule(s, &originConfig)
			if err != nil {
				return fmt.Errorf("failed to create Origin module: %v", err)
			}
			s.AddModule(originModule)

		case "peer":
			var peerConfig PeerModuleConfig
			if err := json.Unmarshal(rawConfig, &peerConfig); err != nil {
				return fmt.Errorf("failed to unmarshal Peer module config: %v", err)
			}

			peerModule, err := NewPeerModule(s, &peerConfig)
			if err != nil {
				return fmt.Errorf("failed to create Peer module: %v", err)
			}
			s.AddModule(peerModule)

		// Configured pins are not enabled for a bit longer

		//case "pin":
		//	var pinConfig PinConfig
		//	if err := json.Unmarshal(rawConfig, &pinConfig); err != nil {
		//		return fmt.Errorf("failed to unmarshal pin config: %v", err)
		//	}

		//	s.AddModule(NewPinModule(s, &pinConfig))

		case "serviceworker":
			var swConfig ServiceWorkerModuleConfig
			if err := json.Unmarshal(rawConfig, &swConfig); err != nil {
				return fmt.Errorf("failed to unmarshal ServiceWorker module config: %v", err)
			}

			swModule, err := NewServiceWorkerModule(s, &swConfig)
			if err != nil {
				return fmt.Errorf("failed to instantiate service worker module: %v", err)
			}
			s.AddModule(swModule)

		// Add to the switch statement in LoadModules
		case "tracker":
			var trackerConfig TrackerModuleConfig
			if err := json.Unmarshal(rawConfig, &trackerConfig); err != nil {
				return fmt.Errorf("failed to unmarshal Tracker module config: %v", err)
			}

			trackerModule, err := NewTrackerModule(s, &trackerConfig)
			if err != nil {
				return fmt.Errorf("failed to create Tracker module: %v", err)
			}
			s.AddModule(trackerModule)

		case "wiki":
			var wikiConfig WikiVolumeConfig
			if err := json.Unmarshal(rawConfig, &wikiConfig); err != nil {
				return fmt.Errorf("failed to unmarshal WikiVolume module config: %v", err)
			}
			wikiVolume, err := NewWikiVolume(&wikiConfig, s, false)
			if err != nil {
				return fmt.Errorf("failed to instantiate WikiVolume: %v", err)
			}
			s.AddModule(wikiVolume)
			s.AddVolume(wikiVolume)

		default:
			return fmt.Errorf("unknown module type: %s", baseConfig.Type)
		}
	}
	return nil
}

// SerializeModuleConfig converts any module's configuration into JSON
// with its type field set properly
func SerializeModuleConfig(module Module, config interface{}) (json.RawMessage, error) {
	// Convert config to a map
	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %v", err)
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal(configBytes, &configMap); err != nil {
		return nil, fmt.Errorf("failed to convert config to map: %v", err)
	}

	// Set the type from the module's name
	configMap["type"] = module.GetModuleName()

	// Marshal back to JSON
	return json.Marshal(configMap)
}
