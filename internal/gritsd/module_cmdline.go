package gritsd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"grits/internal/grits"
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
	if grits.DebugServerLifecycle {
		log.Printf("Starting CommandLineModule with socket at %s", cm.Config.SocketPath)
	}

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

	if grits.DebugServerLifecycle {
		log.Printf("Stopping CommandLineModule")
	}
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
	return cm.Server.ExecuteCommand(request.Command)
}

func (s *Server) ExecuteCommand(cmd []string) CommandResponse {
	if len(cmd) == 0 {
		return CommandResponse{Status: 1, Output: "empty command"}
	}

	switch cmd[0] {

	case "ping":
		return CommandResponse{Status: 0, Output: "pong"}

	case "import":
		// import local/src/path //volume/dest/path
		if len(cmd) != 3 {
			return CommandResponse{Status: 1, Output: "usage: import local/src/path //volume/dest/path"}
		}

		srcPath := cmd[1]
		if !filepath.IsAbs(srcPath) {
			srcPath = s.Config.ServerPath(srcPath)
		}

		volumeName, destPath, err := parseVolumePath(cmd[2])
		if err != nil {
			return CommandResponse{Status: 1, Output: fmt.Sprintf("invalid destination: %v", err)}
		}
		volume := s.FindVolumeByName(volumeName)
		if volume == nil {
			return CommandResponse{Status: 1, Output: fmt.Sprintf("volume %q not found", volumeName)}
		}

		if err := ImportLocalDir(volume, srcPath, destPath); err != nil {
			return CommandResponse{Status: 1, Output: fmt.Sprintf("import failed: %v", err)}
		}
		return CommandResponse{Status: 0, Output: fmt.Sprintf("imported %s into //%s/%s", cmd[2], volumeName, destPath)}

	case "adduser":
		// adduser <username> <password>
		if len(cmd) != 3 {
			return CommandResponse{Status: 1, Output: "usage: adduser <username> <password>"}
		}
		username := cmd[1]
		password := cmd[2]
		if !Validate("username", username) {
			return CommandResponse{Status: 1, Output: "invalid username format"}
		}
		if password == "" {
			return CommandResponse{Status: 1, Output: "password must not be empty"}
		}

		pwdHash, err := Argon2idEncode(password)
		if err != nil {
			return CommandResponse{Status: 1, Output: fmt.Sprintf("failed to hash password: %v", err)}
		}

		lines, err := ReadJSONL(s, "primary", usersFilePath, grits.BackendPrincipal)
		if err != nil && !errors.Is(err, grits.ErrNotExist) {
			return CommandResponse{Status: 1, Output: fmt.Sprintf("reading users file: %v", err)}
		}

		var records []map[string]any
		found := false
		for _, line := range lines {
			var rec map[string]any
			if err := json.Unmarshal(line, &rec); err != nil {
				continue
			}
			if rec["username"] == username {
				rec["pwdHash"] = pwdHash
				found = true
			}
			records = append(records, rec)
		}
		if !found {
			records = append(records, map[string]any{
				"username": username,
				"pwdHash":  pwdHash,
			})
		}

		if err := WriteJSONL(s, "primary", usersFilePath, records, grits.BackendPrincipal); err != nil {
			return CommandResponse{Status: 1, Output: fmt.Sprintf("writing users file: %v", err)}
		}

		// Create home directory with owner permission.
		homeDir := "home/" + username
		volume := s.FindVolumeByName("primary")
		if volume != nil {
			// Ensure home parent exists, then import skeleton files first
			// so the access.json write below is the final step and won't
			// be overwritten by a later LinkByMetadata.
			if err := ensureVolumeParentDirs(volume, "home"); err != nil {
				log.Printf("adduser: ensuring home dir: %v", err)
			}

			skelPath := s.Config.ServerPath("skel/sys/skel")
			if info, statErr := os.Stat(skelPath); statErr == nil && info.IsDir() {
				if importErr := ImportLocalDir(volume, skelPath, homeDir); importErr != nil {
					log.Printf("adduser: importing skel: %v", importErr)
				}
			} else {
				// No skel — at least create the empty home directory.
				if err := ensureVolumeParentDirs(volume, homeDir); err != nil {
					log.Printf("adduser: creating home dir: %v", err)
				}
			}

			// Write owner access.json last so it's not overwritten.
			homeAccess, _ := json.Marshal(AccessConfig{
				Allow: []Grant{
					{User: username, Origin: "gimbal", Permission: PermOwner},
				},
			})
			if err := WriteVolumeFile(s, "primary", homeDir+"/.grits/access.json", homeAccess, grits.BackendPrincipal); err != nil {
				log.Printf("adduser: writing home access.json: %v", err)
			}
		}

		return CommandResponse{Status: 0, Output: "user added"}

	case "deluser":
		// deluser <username>
		if len(cmd) != 2 {
			return CommandResponse{Status: 1, Output: "usage: deluser <username>"}
		}
		username := cmd[1]

		lines, err := ReadJSONL(s, "primary", usersFilePath, grits.BackendPrincipal)
		if err != nil {
			if errors.Is(err, grits.ErrNotExist) {
				return CommandResponse{Status: 1, Output: "users file not found"}
			}
			return CommandResponse{Status: 1, Output: fmt.Sprintf("reading users file: %v", err)}
		}

		var records []map[string]any
		for _, line := range lines {
			var rec map[string]any
			if err := json.Unmarshal(line, &rec); err != nil {
				continue
			}
			if rec["username"] == username {
				continue // skip — deleting this user
			}
			records = append(records, rec)
		}

		if err := WriteJSONL(s, "primary", usersFilePath, records, grits.BackendPrincipal); err != nil {
			return CommandResponse{Status: 1, Output: fmt.Sprintf("writing users file: %v", err)}
		}
		return CommandResponse{Status: 0, Output: "user deleted"}

	default:
		return CommandResponse{Status: 1, Output: fmt.Sprintf("unknown command: %s", cmd[0])}
	}
}

// parseVolumePath parses //volume/path into (volumeName, path).
func parseVolumePath(s string) (volume, path string, err error) {
	if !strings.HasPrefix(s, "//") {
		return "", "", fmt.Errorf("volume path must start with //")
	}
	s = strings.TrimPrefix(s, "//")
	parts := strings.SplitN(s, "/", 2)
	volume = parts[0]
	if volume == "" {
		return "", "", fmt.Errorf("missing volume name")
	}
	if len(parts) == 2 {
		path = strings.TrimRight(parts[1], "/")
	}
	// path stays "" if no slash, or if everything after the slash was trimmed
	return volume, path, nil
}
