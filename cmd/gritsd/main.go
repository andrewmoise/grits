package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"grits/internal/grits"
	"grits/internal/gritsd"
)

// isRunningAsRoot returns true if the current process is running as root (UID 0)
func isRunningAsRoot() bool {
	return os.Geteuid() == 0
}

// dropPrivileges changes the process to run as the specified user and group
func dropPrivileges(username, groupname string) error {
	// Look up the user
	targetUser, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("failed to lookup user %s: %v", username, err)
	}

	uid, err := strconv.Atoi(targetUser.Uid)
	if err != nil {
		return fmt.Errorf("invalid UID for user %s: %v", username, err)
	}

	gid, err := strconv.Atoi(targetUser.Gid)
	if err != nil {
		return fmt.Errorf("invalid GID for user %s: %v", username, err)
	}

	// If a specific group is specified, use that instead
	if groupname != "" {
		targetGroup, err := user.LookupGroup(groupname)
		if err != nil {
			return fmt.Errorf("failed to lookup group %s: %v", groupname, err)
		}
		gid, err = strconv.Atoi(targetGroup.Gid)
		if err != nil {
			return fmt.Errorf("invalid GID for group %s: %v", groupname, err)
		}
	}

	// Set the GID first (must be done before dropping root privileges)
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("failed to set GID to %d: %v", gid, err)
	}

	// Set the UID (this drops root privileges permanently)
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("failed to set UID to %d: %v", uid, err)
	}

	if grits.DebugServerLifecycle {
		log.Printf("Successfully dropped privileges to user %s (UID %d, GID %d)", username, uid, gid)
	}
	return nil
}

func setupLogging(dir string) *os.File {
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		panic(fmt.Sprintf("Failed to create log directory: %v", err))
	}

	logFileName := fmt.Sprintf(dir + "/grits-%s.log", time.Now().Format("2006-01-02"))
	f, err := os.OpenFile(logFileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}

	mw := io.MultiWriter(os.Stderr, f)
	log.SetOutput(mw)

	return f
}

func main() {
	var workingDir string
	flag.StringVar(&workingDir, "d", ".", "Working directory for server")
	flag.Parse()

	configFile := filepath.Join(workingDir, "grits.cfg") // Configuration file path

	// Check if the configuration file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Printf("Configuration file does not exist: %s\n", configFile)
		os.Exit(1) // Exit with an error code
	}

	// Load the full configuration
	config := grits.NewConfig(workingDir)
	if err := config.LoadFromFile(configFile); err != nil {
		log.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}
	config.ServerDir = workingDir

	// Open any ports we'll need later
	var err error
	if isRunningAsRoot() {
		if config.RunAsUser == "" {
			panic("Must specify runAsUser to run as root.")
		}

		// Pre-open privileged ports while we're still root
		// Pass the raw module configs before they're processed
		if err := gritsd.PreopenPrivilegedPorts(config.Modules); err != nil {
			log.Fatalf("Failed to pre-open privileged ports: %v", err)
		}

		// Now drop privileges
		if err := dropPrivileges(config.RunAsUser, config.RunAsGroup); err != nil {
			log.Fatalf("Failed to drop privileges: %v", err)
		}
	}

	if config.RunAsUser != "" || config.RunAsGroup != "" {
		err = dropPrivileges(config.RunAsUser, config.RunAsGroup)
		if err != nil {
			panic(fmt.Sprintf("Cannot drop privileges: %v", err))
		}
	} else if isRunningAsRoot() {
		panic("Must specify runAsUser to run as root.")
	}

	// Ensure the server directory exists
	err = os.MkdirAll(config.ServerDir, 0700)
	if err != nil {
		panic(fmt.Sprintf("Failed to create server directory: %v", err))
	}

	// Set up logging if we're doing it
	if config.DoLogging {
		logFile := setupLogging(config.ServerPath("var/log"))
		defer logFile.Close()
	}

	srv, err := gritsd.NewServer(config)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize server: %v", err))
	}

	// Setup signal handling for a graceful shutdown
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	<-signals

	log.Println("Shutting down server...")
	srv.Stop()
}
