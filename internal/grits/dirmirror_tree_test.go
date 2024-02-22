package grits

import (
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"
)

func TestDirToTreeMirror(t *testing.T) {
	// Setup temporary directory for source files
	serverDir, err := os.MkdirTemp("", "dirtotree_test")
	if err != nil {
		t.Fatalf("Failed to create temporary source directory: %v", err)
	}
	defer os.RemoveAll(serverDir)

	// Initialize BlobStore and NameStore
	config := NewConfig()
	config.ServerDir = serverDir

	blobStore := NewBlobStore(config)
	nameStore, err := EmptyNameStore(blobStore)
	if err != nil {
		t.Fatalf("Failed to create NameStore: %v", err)
	}

	// src and destination
	srcPath := path.Join(serverDir, "src")
	os.Mkdir(srcPath, 0755)

	destPath := "mirrorDest"

	// Instantiate and start DirToTreeMirror
	dirMirror := NewDirToTreeMirror(srcPath, destPath, blobStore, nameStore)
	dirMirror.Start()
	defer dirMirror.Stop()

	// Allow some time for the initial scan to complete
	time.Sleep(1 * time.Second)

	// Perform file operations (add, update, delete) in srcDir
	// For example, create a new file:
	newFilePath := filepath.Join(srcPath, "newFile.txt")
	testContent := "Hello, Grits!"
	if err := os.WriteFile(newFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}

	// Allow some time for changes to be detected and processed
	time.Sleep(1 * time.Second)

	fa := nameStore.ResolveName(path.Join(destPath, "newFile.txt"))
	if fa == nil {
		t.Fatalf("Failed to resolve new file in NameStore")
	}

	newFile, err := blobStore.ReadFile(fa)
	if err != nil {
		t.Fatalf("Failed to read new file from BlobStore: %v", err)
	}
	defer blobStore.Release(newFile)

	newContent, err := os.ReadFile(newFile.Path)
	if err != nil {
		t.Fatalf("Failed to read new file content: %v", err)
	}

	if string(newContent) != testContent {
		t.Errorf("New file content does not match: expected %q, got %q", testContent, string(newContent))
	}
}
