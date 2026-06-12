package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// CommandRequest represents the request format for the pipe
type CommandRequest struct {
	Command []string `json:"command"`
}

// CommandResponse represents the response format from the pipe
type CommandResponse struct {
	Status int    `json:"status"`
	Output string `json:"output"`
}

func main() {
	// Default working directory
	workingDir := "."
	args := os.Args[1:]

	// Check if the first argument is -d
	if len(args) > 0 && args[0] == "-d" {
		if len(args) > 1 {
			workingDir = args[1]
			args = args[2:] // Remove -d and its value from args
		} else {
			fmt.Fprintln(os.Stderr, "Error: -d flag requires a directory")
			os.Exit(1)
		}
	}

	// Construct the pipe path
	socketPath := filepath.Join(workingDir, "var", "grits-cmd.pipe")

	// Check if pipe exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Command pipe not found at %s\n", socketPath)
		os.Exit(1)
	}

	// Prepare the command
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: No command specified")
		os.Exit(1)
	}

	// Special-case adduser: if only username given, prompt for password from stdin
	// so it doesn't end up in shell history.
	if args[0] == "adduser" && len(args) == 2 {
		fmt.Fprintf(os.Stderr, "Password for %s: ", args[1])
		reader := bufio.NewReader(os.Stdin)
		password, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		args = append(args, strings.TrimRight(password, "\n\r"))
	}

	// Connect to the socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to command socket: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Create and send request
	request := CommandRequest{
		Command: args,
	}

	if err := json.NewEncoder(conn).Encode(request); err != nil {
		fmt.Fprintf(os.Stderr, "Error sending command: %v\n", err)
		os.Exit(1)
	}

	// Read response
	var response CommandResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(response.Output)
	os.Exit(response.Status)
}
