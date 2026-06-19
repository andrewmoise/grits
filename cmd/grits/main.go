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

	// Special-case adduser:
	//   - -f can appear anywhere in the flag list
	//   - if only username given (no password arg), prompt from stdin
	//   - normalize the command before sending to the server
	if args[0] == "adduser" {
		hasForce := false
		positional := []string{}
		for _, a := range args[1:] {
			if a == "-f" {
				hasForce = true
			} else {
				positional = append(positional, a)
			}
		}
		switch len(positional) {
		case 1:
			fmt.Fprintf(os.Stderr, "Password for %s: ", positional[0])
			reader := bufio.NewReader(os.Stdin)
			password, err := reader.ReadString('\n')
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
				os.Exit(1)
			}
			positional = append(positional, strings.TrimRight(password, "\n\r"))
		case 2:
			// already have username + password — nothing to do
		default:
			fmt.Fprintln(os.Stderr, "usage: adduser [-f] <username> [<password>]")
			os.Exit(1)
		}
		args = []string{"adduser"}
		if hasForce {
			args = append(args, "-f")
		}
		args = append(args, positional...)
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
