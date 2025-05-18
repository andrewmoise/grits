package gritsd

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
)

// CommandLineModuleConfig represents the configuration for the command line module
type CommandLineModuleConfig struct {
	SocketPath string `json:"socketPath,omitempty"`
}

// CommandRequest represents the structure of a command request
type CommandRequest struct {
	Command []string `json:"command"`
}

// CommandResponse represents the structure of a command response
type CommandResponse struct {
	Status int    `json:"status"`
	Output string `json:"output"`
}

// CommandLineModule implements a command-line interface via named pipe
type CommandLineModule struct {
	Config   *CommandLineModuleConfig
	Server   *Server
	listener net.Listener
	running  bool
	stopCh   chan struct{}
}

// NewCommandLineModule creates a new instance of CommandLineModule
func NewCommandLineModule(server *Server, config *CommandLineModuleConfig) (*CommandLineModule, error) {
	// Use default pipe path if not specified
	if config.SocketPath == "" {
		config.SocketPath = filepath.Join(server.Config.ServerPath("var"), "grits-cmd.pipe")
	}

	return &CommandLineModule{
		Config: config,
		Server: server,
		stopCh: make(chan struct{}),
	}, nil
}

func (cm *CommandLineModule) Start() error {
	log.Printf("Starting CommandLineModule with socket at %s", cm.Config.SocketPath)

	// Create directory if needed
	if err := os.MkdirAll(filepath.Dir(cm.Config.SocketPath), 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %v", err)
	}

	// Remove existing socket if it exists
	if err := os.RemoveAll(cm.Config.SocketPath); err != nil {
		return fmt.Errorf("failed to remove existing socket: %v", err)
	}

	// Create and listen on the socket
	listener, err := net.Listen("unix", cm.Config.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %v", err)
	}

	cm.listener = listener
	cm.running = true

	// Handle connections in background
	go cm.acceptConnections()

	return nil
}

func (cm *CommandLineModule) acceptConnections() {
	for cm.running {
		// Accept new connection
		conn, err := cm.listener.Accept()
		if err != nil {
			if cm.running {
				log.Printf("Error accepting connection: %v", err)
			}
			continue
		}

		// Handle each connection in its own goroutine
		go cm.handleConnection(conn)
	}
}

func (cm *CommandLineModule) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Read the command
	var request CommandRequest
	decoder := json.NewDecoder(conn)
	if err := decoder.Decode(&request); err != nil {
		// Handle error
		response := CommandResponse{
			Status: 1,
			Output: fmt.Sprintf("Error decoding command: %v", err),
		}
		json.NewEncoder(conn).Encode(response)
		return
	}

	// Process command
	response := cm.executeCommand(request)

	// Send response
	if err := json.NewEncoder(conn).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// Stop cleanly shuts down the command line interface
func (cm *CommandLineModule) Stop() error {
	if !cm.running {
		return nil
	}

	log.Printf("Stopping CommandLineModule")
	cm.running = false

	// Close the listener
	if cm.listener != nil {
		cm.listener.Close()
	}

	// Clean up the socket file
	if err := os.Remove(cm.Config.SocketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove socket during shutdown: %v", err)
	}

	close(cm.stopCh)
	return nil
}

// GetModuleName returns the name of this module
func (cm *CommandLineModule) GetModuleName() string {
	return "cmdline"
}

// GetDependencies returns the module dependencies
func (*CommandLineModule) GetDependencies() []*Dependency {
	return []*Dependency{} // No dependencies
}

// GetConfig returns the module configuration
func (cm *CommandLineModule) GetConfig() any {
	return cm.Config
}

// executeCommand handles the actual command processing
func (cm *CommandLineModule) executeCommand(request CommandRequest) CommandResponse {
	if len(request.Command) == 0 {
		return CommandResponse{
			Status: 1,
			Output: "Empty command",
		}
	}

	log.Printf("Received command: %s", request.Command[0])

	if request.Command[0] == "ping" {
		return CommandResponse{
			Status: 0,
			Output: "pong",
		}
	}

	// Handle other commands here...

	// Unknown command
	return CommandResponse{
		Status: 1,
		Output: fmt.Sprintf("Unknown command: %s", request.Command[0]),
	}
}
