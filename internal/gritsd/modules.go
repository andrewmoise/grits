package gritsd

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
	GetConfig() any
	GetDependencies() []*Dependency // Return required module types
}

// DependencyType represents how a dependency should be handled
type DependencyType int

const (
	// DependOptional - Use if available, but don't require it. Only affects ordering of
	// when the module loads (which maybe means it needs a different name).
	DependOptional DependencyType = iota

	// DependRequired - Must exist, error if not found
	DependRequired

	// DependAutoCreate - Create with default config if not found
	DependAutoCreate
)

// Dependency represents a module dependency with its handling type
type Dependency struct {
	ModuleType string
	Type       DependencyType

	// For dynamic configuration based on parent module
	ConfigGenerator func(parentModule Module) (json.RawMessage, error)
}

// More details for storage modules:
type Volume interface {
	GetVolumeName() string
	Start() error
	Stop() error
	isReadOnly() bool
	Checkpoint() error

	LookupNode(path string) (grits.FileNode, error)
	LookupFull(name []string) ([]*grits.PathNodePair, bool, error)
	GetFileNode(metadataAddr *grits.BlobAddr) (grits.FileNode, error)

	CreateTreeNode() (*grits.TreeNode, error)
	CreateBlobNode(contentAddr *grits.BlobAddr, size int64) (*grits.BlobNode, error)

	Link(path string, addr *grits.TypedFileAddr) error
	LinkByMetadata(path string, metadataAddr *grits.BlobAddr) error
	MultiLink([]*grits.LinkRequest) error

	ReadFile(*grits.TypedFileAddr) (grits.CachedFile, error)
	AddBlob(path string) (grits.CachedFile, error)
	AddOpenBlob(*os.File) (grits.CachedFile, error)
	AddMetadataBlob(*grits.GNodeMetadata) (grits.CachedFile, error)

	GetBlob(addr *grits.BlobAddr) (grits.CachedFile, error)
	PutBlob(file *os.File) (*grits.BlobAddr, error)

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

// FIXME - Rename to GetVolume()
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

// Create a helper function that can create a single module
func (s *Server) createModuleFromConfig(moduleType string, rawConfig json.RawMessage) (Module, error) {
	switch moduleType {
	case "cmdline":
		var cmdlineConfig CommandLineModuleConfig
		if err := json.Unmarshal(rawConfig, &cmdlineConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal CommandLine module config: %v", err)
		}

		cmdlineModule, err := NewCommandLineModule(s, &cmdlineConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create CommandLine module: %v", err)
		}
		return cmdlineModule, nil

	case "deployment":
		var deploymentConfig DeploymentConfig
		if err := json.Unmarshal(rawConfig, &deploymentConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal DeploymentModule config: %v", err)
		}

		module := NewDeploymentModule(s, &deploymentConfig)
		return module, nil

	case "http":
		var httpConfig HTTPModuleConfig
		if err := json.Unmarshal(rawConfig, &httpConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal HTTP module config: %v", err)
		}

		httpModule, err := NewHTTPModule(s, &httpConfig)
		if err != nil {
			return nil, fmt.Errorf("can't create http module: %v", err)
		}
		return httpModule, nil

	case "mirror":
		var mirrorConfig MirrorModuleConfig
		if err := json.Unmarshal(rawConfig, &mirrorConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Mirror module config: %v", err)
		}

		mirrorModule, err := NewMirrorModule(s, &mirrorConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Mirror module: %v", err)
		}
		return mirrorModule, nil

	case "mount":
		var mountConfig MountModuleConfig
		if err := json.Unmarshal(rawConfig, &mountConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal mount config: %v", err)
		}

		mountModule := NewMountModule(s, &mountConfig)

		return mountModule, nil

	case "origin":
		var originConfig OriginModuleConfig
		if err := json.Unmarshal(rawConfig, &originConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal origin module config: %v", err)
		}

		originModule, err := NewOriginModule(s, &originConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create origin module: %v", err)
		}
		return originModule, nil

	case "peer":
		var peerConfig PeerModuleConfig
		if err := json.Unmarshal(rawConfig, &peerConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Peer module config: %v", err)
		}

		peerModule, err := NewPeerModule(s, &peerConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Peer module: %v", err)
		}
		return peerModule, nil

	// Configured pins are not enabled for a bit longer

	//case "pin":
	//	var pinConfig PinConfig
	//	if err := json.Unmarshal(rawConfig, &pinConfig); err != nil {
	//		return nil, fmt.Errorf("failed to unmarshal pin config: %v", err)
	//	}

	case "serviceworker":
		var swConfig ServiceWorkerModuleConfig
		if err := json.Unmarshal(rawConfig, &swConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ServiceWorker module config: %v", err)
		}

		swModule, err := NewServiceWorkerModule(s, &swConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate service worker module: %v", err)
		}
		return swModule, nil

	case "tracker":
		var trackerConfig TrackerModuleConfig
		if err := json.Unmarshal(rawConfig, &trackerConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Tracker module config: %v", err)
		}

		trackerModule, err := NewTrackerModule(s, &trackerConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Tracker module: %v", err)
		}
		return trackerModule, nil

	case "localvolume":
		var localConfig LocalVolumeConfig
		if err := json.Unmarshal(rawConfig, &localConfig); err != nil {
			return nil, fmt.Errorf("failed to unmarshal LocalVolume module config: %v", err)
		}
		localVolume, err := NewLocalVolume(&localConfig, s, false)
		if err != nil {
			return nil, fmt.Errorf("failed to instantiate LocalVolume: %v", err)
		}
		return localVolume, nil

	default:
		return nil, fmt.Errorf("unknown module type: %s", moduleType)
	}

}

func (s *Server) LoadModules(rawModuleConfigs []json.RawMessage) error {
	for _, rawConfig := range rawModuleConfigs {
		var baseConfig ModuleConfig
		if err := json.Unmarshal(rawConfig, &baseConfig); err != nil {
			return fmt.Errorf("error unmarshalling base module config: %v", err)
		}

		module, err := s.createModuleFromConfig(baseConfig.Type, rawConfig)
		if err != nil {
			return err
		}

		s.AddModule(module)

		// Special case for volumes
		if volume, ok := module.(Volume); ok {
			s.AddVolume(volume)
		}
	}
	return nil
}

func (s *Server) resolveModuleDependencies() error {
	// Track which module types exist
	moduleTypesExist := make(map[string]bool)

	// Initial population of existing module types
	for _, module := range s.Modules {
		moduleTypesExist[module.GetModuleName()] = true
	}

	// Process modules, allowing for list growth during iteration
	for i := 0; i < len(s.Modules); i++ {
		module := s.Modules[i]

		// Check each dependency
		for _, dependency := range module.GetDependencies() {
			// Skip if this type already exists
			if moduleTypesExist[dependency.ModuleType] {
				continue
			}

			// Handle based on dependency type
			switch dependency.Type {
			case DependRequired:
				// Error if required and not found
				return fmt.Errorf("module %s requires %s, but no such module exists",
					module.GetModuleName(), dependency.ModuleType)

			case DependAutoCreate:
				// Generate config using the parent module
				if dependency.ConfigGenerator == nil {
					return fmt.Errorf("auto-create dependency on %s requires a config generator",
						dependency.ModuleType)
				}

				config, err := dependency.ConfigGenerator(module)
				if err != nil {
					return fmt.Errorf("failed to generate config for %s: %v",
						dependency.ModuleType, err)
				}

				// Create the new module
				newModule, err := s.createModuleFromConfig(dependency.ModuleType, config)
				if err != nil {
					return fmt.Errorf("failed to auto-create %s module: %v",
						dependency.ModuleType, err)
				}

				// Add the new module
				s.AddModule(newModule)
				// Special handling for volume modules
				if volume, ok := newModule.(Volume); ok {
					s.AddVolume(volume)
				}

				// Mark this type as existing now
				moduleTypesExist[dependency.ModuleType] = true

				log.Printf("Auto-created %s module as dependency of %s",
					dependency.ModuleType, module.GetModuleName())

			case DependOptional:
				// Nothing to do for optional dependencies that don't exist
			}
		}
	}

	return nil
}

// sortModulesByDependency performs topological sort of modules based on dependencies
func sortModulesByDependency(modules []Module) []Module {
	// Map module types to their actual modules
	modulesByType := make(map[string][]Module)
	for _, module := range modules {
		moduleType := module.GetModuleName()
		modulesByType[moduleType] = append(modulesByType[moduleType], module)
	}

	// Track visited and sorted modules
	visited := make(map[Module]bool)
	sorted := make([]Module, 0, len(modules))

	// Visit function for depth-first search
	var visit func(Module)
	visit = func(module Module) {
		if visited[module] {
			return
		}

		visited[module] = true

		// Visit dependencies first (of any type)
		for _, dep := range module.GetDependencies() {
			if deps, exists := modulesByType[dep.ModuleType]; exists {
				for _, depModule := range deps {
					visit(depModule)
				}
			}
		}

		// Add this module after its dependencies
		sorted = append(sorted, module)
	}

	// Visit all modules
	for _, module := range modules {
		if !visited[module] {
			visit(module)
		}
	}

	return sorted
}

// SerializeModuleConfig converts any module's configuration into JSON
// with its type field set properly
func SerializeModuleConfig(module Module, config any) (json.RawMessage, error) {
	// Convert config to a map
	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %v", err)
	}

	var configMap map[string]any
	if err := json.Unmarshal(configBytes, &configMap); err != nil {
		return nil, fmt.Errorf("failed to convert config to map: %v", err)
	}

	// Set the type from the module's name
	configMap["type"] = module.GetModuleName()

	// Marshal back to JSON
	return json.Marshal(configMap)
}
