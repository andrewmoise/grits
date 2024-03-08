package server

import (
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"
)

// DHT related stuff, should go in DHT module tests at some point

func startHubNode(serverDir string, port int) *Server {
	config := grits.NewConfig(serverDir)
	config.IsRootNode = true

	hubServer, err := NewServer(config)
	if err != nil {
		log.Fatalf("Failed to start hub node: %v", err)
		return nil
	}

	httpConfig := &HttpModuleConfig{
		ThisPort: port,
	}
	httpModule := NewHttpModule(hubServer, httpConfig)
	hubServer.AddModule(httpModule)

	hubServer.Start()
	return hubServer
}

func startEdgeNode(serverDir string, port int, rootHost string, rootPort int) *Server {
	config := grits.NewConfig(serverDir)
	config.RootHost = rootHost
	config.RootPort = rootPort

	edgeServer, err := NewServer(config)
	if err != nil {
		log.Fatalf("Failed to start edge node on port %d: %v", port, err)
		return nil
	}

	httpConfig := &HttpModuleConfig{
		ThisPort: port,
	}
	httpModule := NewHttpModule(edgeServer, httpConfig)
	edgeServer.AddModule(httpModule)

	edgeServer.Start()
	return edgeServer
}

func TestFakeNetwork(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "server_test")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}

	// Defer a function to stop all servers
	var servers []*Server = make([]*Server, 0, 51)
	defer func() {
		for _, server := range servers {
			server.Stop()
		}
	}()

	hub := startHubNode(path.Join(tempDir, "hub"), 1987)
	if hub == nil {
		t.Fatal("Failed to start hub node")
	}
	defer hub.Stop()
	servers = append(servers, hub)

	for i := 0; i < 50; i++ {
		node := startEdgeNode(path.Join(tempDir, fmt.Sprintf("%d", i)), 1900+i, "localhost", 1987)
		if node == nil {
			t.Fatalf("Failed to start edge node %d", i)
		}
		servers = append(servers, node)
	}

	// Wait 0.5 seconds to make sure all heartbeats can take place
	time.Sleep(500 * time.Millisecond)

	// Absorb a big content directory into the hub's blob store
	filepath.Walk("test-content", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			_, err := hub.BlobStore.AddLocalFile(path)
			if err != nil {
				t.Fatalf("Failed to add file %s to hub's blob store: %v", path, err)
			}
		}
		return nil
	})
}
