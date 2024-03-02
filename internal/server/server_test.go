package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"grits/internal/grits"
)

func startHubNode(serverDir string, port int) *Server {
	config := grits.NewConfig()
	config.ThisPort = port
	config.IsRootNode = true
	config.ServerDir = serverDir

	hubServer, err := NewServer(config)
	if err != nil {
		log.Fatalf("Failed to start hub node: %v", err)
		return nil
	}
	hubServer.Start()
	return hubServer
}

func startEdgeNode(serverDir string, port int, rootHost string, rootPort int) *Server {
	config := grits.NewConfig()
	config.ThisPort = port
	config.RootHost = rootHost
	config.RootPort = rootPort
	config.ServerDir = serverDir

	edgeServer, err := NewServer(config)
	if err != nil {
		log.Fatalf("Failed to start edge node on port %d: %v", port, err)
		return nil
	}
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
			server.Stop(context.Background())
		}
	}()

	hub := startHubNode(path.Join(tempDir, "hub"), 1987)
	if hub == nil {
		t.Fatal("Failed to start hub node")
	}
	defer hub.Stop(context.Background())
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

func TestNamespacePersistence(t *testing.T) {
	// Set up a temporary directory for the server's working environment.
	tempDir, err := os.MkdirTemp("", "server_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up after the test.

	fmt.Printf("tempDir: %s\n", tempDir)

	// Create a new server instance using this temporary directory.
	config := grits.NewConfig()
	config.ServerDir = tempDir

	srv, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Populate test content.

	testAccountName := "test"
	ns, err := grits.EmptyNameStore(srv.BlobStore)
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}

	srv.AccountStores[testAccountName] = ns

	blocks := []string{"one", "two", "three"}

	for _, block := range blocks {
		cf, err := srv.BlobStore.AddDataBlock([]byte(block))
		if err != nil {
			t.Fatalf("Failed to add data block: %v", err)
		}
		defer srv.BlobStore.Release(cf)

		err = ns.LinkBlob(block, cf.Address)
		if err != nil {
			t.Fatalf("Failed to link blob: %v", err)
		}
	}

	if err != nil {
		t.Fatalf("Failed to revise root: %v", err)
	}

	fmt.Printf("All ready to save -- Root: %v\n", ns.GetRoot())

	// Save the state of the server.
	if err := srv.SaveAccounts(); err != nil {
		t.Fatalf("Failed to save accounts: %v", err)
	}

	// Reload the namespace state.
	if err := srv.LoadAccounts(); err != nil {
		t.Fatalf("Failed to load accounts: %v", err)
	}

	// Check that the namespace state is the same.
	for _, block := range blocks {
		cf, err := ns.Lookup(block)
		if err != nil {
			t.Errorf("Failed to look up name: %v", err)
		}
		defer srv.BlobStore.Release(cf)

		content, err := os.ReadFile(cf.Path)
		if err != nil {
			t.Errorf("Failed to read file: %v", err)
		}
		if string(content) != block {
			t.Errorf("Content mismatch: got %v, want %v", string(content), block)
		}
	}
}
