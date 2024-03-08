package server

import (
	"encoding/json"
	"fmt"
)

// Module is an interface that all modules must implement.
type Module interface {
	Start() error
	Stop() error
	Name() string
}

// ModuleConfig represents a generic module configuration.
type ModuleConfig struct {
	Type   string          `json:"Type"`
	Config json.RawMessage `json:"Config"`
}

func (s *Server) GetModules(name string) []Module {
	var matches []Module
	for _, module := range s.Modules {
		if module.Name() == name {
			matches = append(matches, module)
		}
	}
	return matches
}

func (s *Server) AddModule(module Module) {
	s.Modules = append(s.Modules, module)
	// Call all hooks for the newly added module
	for _, hook := range s.moduleHooks {
		hook(module)
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

			module, err := NewDirToTreeMirror(mirrorConfig.SourceDir, mirrorConfig.DestPath, s, s.Config.DirWatcherPath, s.Shutdown)
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
