package server

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
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
	GetNameStore() *grits.NameStore
	Checkpoint() error
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

	if volume, ok := module.(Volume); ok {
		s.Volumes[volume.GetVolumeName()] = volume
	}
}

// AddModuleHook adds a new hook to be called whenever a new module is added.
func (s *Server) AddModuleHook(hook func(Module)) {
	s.moduleHooks = append(s.moduleHooks, hook)
	// Call the hook immediately for all existing modules
	for _, module := range s.Modules {
		hook(module)
	}
}

func (s *Server) LoadModules(rawModuleConfigs []json.RawMessage) error {
	for _, rawConfig := range rawModuleConfigs {
		var baseConfig ModuleConfig
		if err := json.Unmarshal(rawConfig, &baseConfig); err != nil {
			return fmt.Errorf("error unmarshalling base module config: %v", err)
		}

		switch baseConfig.Type {
		case "http":
			var httpConfig HttpModuleConfig
			if err := json.Unmarshal(rawConfig, &httpConfig); err != nil {
				return fmt.Errorf("failed to unmarshal HTTP module config: %v", err)
			}

			s.AddModule(NewHttpModule(s, &httpConfig))

		case "dirmirror":
			var mirrorConfig DirToTreeMirrorConfig
			if err := json.Unmarshal(rawConfig, &mirrorConfig); err != nil {
				return fmt.Errorf("failed to unmarshal DirToTreeMirror module config: %v", err)
			}

			module, err := NewDirToTreeMirror(&mirrorConfig, s, s.Shutdown)
			if err != nil {
				return fmt.Errorf("failed to instantiate DirToTreeMirror: %v", err)
			}

			s.AddModule(module)

		default:
			return fmt.Errorf("unknown module type: %s", baseConfig.Type)
		}
	}
	return nil
}
