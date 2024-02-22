package grits

import (
	"fmt"
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

	// Create additional files
	for i := 1; i <= 5; i++ {
		fileName := filepath.Join(srcPath, fmt.Sprintf("file%d.txt", i))
		fileContent := fmt.Sprintf("Content for file %d", i)
		if err := os.WriteFile(fileName, []byte(fileContent), 0644); err != nil {
			t.Fatalf("Failed to create file %d: %v", i, err)
		}
	}

	// Allow some time for changes to be detected and processed
	time.Sleep(1 * time.Second)

	// Verify files are added to NameStore
	for i := 1; i <= 5; i++ {
		fa := nameStore.ResolveName(path.Join(destPath, fmt.Sprintf("file%d.txt", i)))
		if fa == nil {
			t.Fatalf("Failed to resolve file%d.txt in NameStore", i)
		}

		file, err := blobStore.ReadFile(fa)
		if err != nil {
			t.Fatalf("Failed to read file%d.txt from BlobStore: %v", i, err)
		}
		blobStore.Release(file)
	}

	// Delete one file and overwrite another
	os.Remove(filepath.Join(srcPath, "file3.txt"))

	newContent := "Updated content for file 5"
	os.WriteFile(filepath.Join(srcPath, "file5.txt"), []byte(newContent), 0644)

	// Allow some time for changes to be detected and processed
	time.Sleep(1 * time.Second)

	// Verify file3.txt is removed from NameStore
	fa := nameStore.ResolveName(path.Join(destPath, "file3.txt"))
	if fa != nil {
		t.Fatalf("file3.txt should have been removed from NameStore")
	}

	// Verify file5.txt content is updated
	fa = nameStore.ResolveName(path.Join(destPath, "file5.txt"))
	if fa == nil {
		t.Fatalf("Failed to resolve file5.txt in NameStore")
	}

	file, err := blobStore.ReadFile(fa)
	if err != nil {
		t.Fatalf("Failed to read file5.txt from BlobStore: %v", err)
	}
	defer blobStore.Release(file)

	updatedContent, err := os.ReadFile(file.Path)
	if err != nil {
		t.Fatalf("Failed to read updated content for file5.txt: %v", err)
	}

	if string(updatedContent) != newContent {
		t.Errorf("file5.txt content does not match: expected %q, got %q", newContent, string(updatedContent))
	}
}