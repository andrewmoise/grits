package grits

import (
	"fmt"
	"log"
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

	// src and destination
	srcPath := path.Join(serverDir, "src")
	os.Mkdir(srcPath, 0755)

	destPath := "mirrorDest"

	log.Printf("--- Start test\n")

	// Instantiate and start DirToTreeMirror
	dirMirror, error := NewDirToTreeMirror(srcPath, destPath, blobStore)
	if error != nil {
		t.Fatalf("Failed to create DirToTreeMirror: %v", error)
	}

	nameStore := dirMirror.ns

	err = dirMirror.Start()
	if err != nil {
		t.Fatalf("Failed to start DirToTreeMirror: %v", err)
	}
	defer dirMirror.Stop()

	// Allow some time for the initial scan to complete
	time.Sleep(1 * time.Second)

	log.Printf("--- Initial add\n")

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

	log.Printf("--- Check initial add\n")

	// Verify files are added to NameStore
	for i := 1; i <= 5; i++ {
		cf, err := nameStore.Lookup(path.Join(destPath, fmt.Sprintf("file%d.txt", i)))
		if err != nil {
			t.Fatalf("Failed to lookup file%d.txt in NameStore: %v", i, err)
		}
		defer blobStore.Release(cf)

		file, err := blobStore.ReadFile(cf.Address)
		if err != nil {
			t.Fatalf("Failed to read file%d.txt from BlobStore: %v", i, err)
		}
		blobStore.Release(file)
	}

	log.Printf("--- Modifications\n")

	// Delete one file and overwrite another
	os.Remove(filepath.Join(srcPath, "file3.txt"))

	newContent := "Updated content for file 5"
	os.WriteFile(filepath.Join(srcPath, "file5.txt"), []byte(newContent), 0644)

	// Allow some time for changes to be detected and processed
	time.Sleep(1 * time.Second)

	log.Printf("--- Check modifications\n")

	// Verify file3.txt is removed from NameStore
	_, err = nameStore.Lookup(path.Join(destPath, "file3.txt"))
	if err == nil {
		t.Fatalf("file3.txt should have been removed from NameStore")
	}

	// Verify file5.txt content is updated
	cf, err := nameStore.Lookup(path.Join(destPath, "file5.txt"))
	if err != nil {
		t.Fatalf("Failed to resolve file5.txt in NameStore")
	}
	defer blobStore.Release(cf)

	file, err := blobStore.ReadFile(cf.Address)
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

	log.Printf("--- Subdirectory operations\n")

	// Create a subdirectory and a file within it
	subDirPath := filepath.Join(srcPath, "subdir")
	if err := os.Mkdir(subDirPath, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	subFileName := filepath.Join(subDirPath, "subfile1.txt")
	subFileContent := "Content for subfile 1"
	if err := os.WriteFile(subFileName, []byte(subFileContent), 0644); err != nil {
		t.Fatalf("Failed to create file in subdirectory: %v", err)
	}

	// Allow some time for changes to be detected and processed
	time.Sleep(1 * time.Second)

	log.Printf("--- Check subdirectory add\n")

	// Verify the subdirectory file is added to NameStore
	subCf, err := nameStore.Lookup(path.Join(destPath, "subdir", "subfile1.txt"))
	if err != nil {
		t.Fatalf("Failed to resolve subdir/subfile1.txt in NameStore")
	}
	defer blobStore.Release(subCf)

	subFile, err := blobStore.ReadFile(subCf.Address)
	if err != nil {
		t.Fatalf("Failed to read subdir/subfile1.txt from BlobStore: %v", err)
	}
	blobStore.Release(subFile)

	// Optional: Delete the subdirectory file and verify removal
	os.Remove(subFileName)

	// Allow some time for changes to be detected and processed
	time.Sleep(1 * time.Second)

	log.Printf("--- Check subdirectory file removal\n")

	// Verify subdirectory file is removed from NameStore
	_, err = nameStore.Lookup(path.Join(destPath, "subdir", "subfile1.txt"))
	if err == nil {
		t.Fatalf("subdir/subfile1.txt should have been removed from NameStore")
	}

}
