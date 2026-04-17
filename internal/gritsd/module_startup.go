package gritsd

import (
	"fmt"
	"log"
)

type StartupModuleConfig struct {
	Commands [][]string `json:"commands"`
}

type StartupModule struct {
	Config *StartupModuleConfig
	Server *Server
}

func NewStartupModule(server *Server, config *StartupModuleConfig) *StartupModule {
	return &StartupModule{Config: config, Server: server}
}

func (m *StartupModule) GetModuleName() string { return "startup" }
func (m *StartupModule) GetConfig() any        { return m.Config }
func (m *StartupModule) Stop() error           { return nil }

func (m *StartupModule) GetDependencies() []*Dependency {
	return []*Dependency{
		{ModuleType: "volume", Type: DependOptional},
		{ModuleType: "cmdline", Type: DependOptional},
	}
}

func (m *StartupModule) Start() error {
	for i, cmd := range m.Config.Commands {
		if len(cmd) == 0 {
			continue
		}
		log.Printf("Startup: running command %d: %v", i, cmd)
		resp := m.Server.ExecuteCommand(cmd)
		if resp.Status != 0 {
			return fmt.Errorf("startup command %v failed: %s", cmd, resp.Output)
		}
	}
	return nil
}
