package server

import (
	"encoding/json"
	"fmt"
	"os"
)

// Module is an interface that all modules must implement.
type Module interface {
	Start() error
	Stop() error
	Name() string
}

// ModuleConfig represents a generic module configuration.
type ModuleConfig struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

// LoadModuleConfigs loads module configurations from a JSON file.
func LoadModuleConfigs(filename string) ([]ModuleConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var configs []ModuleConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, err
	}

	return configs, nil
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
			s.Modules = append(s.Modules, NewHttpModule(s, &httpConfig))

		case "dirmirror":
			var mirrorConfig DirToTreeMirrorConfig
			if err := json.Unmarshal(rawConfig, &mirrorConfig); err != nil {
				return fmt.Errorf("failed to unmarshal DirToTreeMirror module config: %v", err)
			}
			module, err := NewDirToTreeMirror(mirrorConfig.SourceDir, mirrorConfig.DestPath, s, s.Config.DirWatcherPath, s.Stop)
			if err != nil {
				return fmt.Errorf("failed to instantiate DirToTreeMirror: %v", err)
			}
			s.Modules = append(s.Modules, module)

		default:
			return fmt.Errorf("unknown module type: %s", baseConfig.Type)
		}
	}
	return nil
}
