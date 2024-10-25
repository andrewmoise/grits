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
}

// Special for storage modules:
type Volume interface {
	GetVolumeName() string

	Start() error
	Stop() error

	isReadOnly() bool
	Checkpoint() error

	Lookup(path string) (*grits.TypedFileAddr, error)
	LookupNode(path string) (grits.FileNode, error)
	LookupFull(name string) ([][]string, error)

	Link(path string, addr *grits.TypedFileAddr) error
	MultiLink([]*grits.LinkRequest) error

	ReadFile(*grits.TypedFileAddr) (grits.CachedFile, error)

	AddBlob(path string) (grits.CachedFile, error)
	AddOpenBlob(*os.File) (grits.CachedFile, error)
}

// ModuleConfig represents a generic module configuration.
type ModuleConfig struct {
	Type   string          `json:"Type"`
	Config json.RawMessage `json:"Config"`
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

		case "mount":
			var mountConfig MountModuleConfig
			if err := json.Unmarshal(rawConfig, &mountConfig); err != nil {
				return fmt.Errorf("failed to unmarshal mount config: %v", err)
			}

			s.AddModule(NewMountModule(&mountConfig, s))

		case "serviceworker":
			var swConfig ServiceWorkerModuleConfig
			if err := json.Unmarshal(rawConfig, &swConfig); err != nil {
				return fmt.Errorf("failed to unmarshal ServiceWorker module config: %v", err)
			}

			swModule := NewServiceWorkerModule(s, &swConfig)
			s.AddModule(swModule)

		case "wiki":
			var wikiConfig WikiVolumeConfig
			if err := json.Unmarshal(rawConfig, &wikiConfig); err != nil {
				return fmt.Errorf("failed to unmarshal WikiVolume module config: %v", err)
			}
			wikiVolume, err := NewWikiVolume(&wikiConfig, s)
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
