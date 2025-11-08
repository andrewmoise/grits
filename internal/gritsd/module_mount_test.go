// mount_test.go
package gritsd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBasicMountOperations(t *testing.T) {
	// Create a temporary mount point
	mountPoint, err := os.MkdirTemp("", "grits_mount_test")
	if err != nil {
		t.Fatalf("Failed to create temp mount point: %v", err)
	}
	defer os.RemoveAll(mountPoint)

	server, cleanup := SetupTestServer(t,
		WithLocalVolume("root"),
		WithMountModule("root", mountPoint))
	defer cleanup()

	err = server.Start()
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Give the mount a moment to initialize
	time.Sleep(100 * time.Millisecond)

	t.Run("CreateAndReadFile", func(t *testing.T) {
		testFile := filepath.Join(mountPoint, "test.txt")
		testContent := []byte("hello world")

		// Write a file
		err := os.WriteFile(testFile, testContent, 0644)
		if err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}

		// Read it back
		data, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		if !bytes.Equal(data, testContent) {
			t.Fatalf("Expected %q, got %q", testContent, data)
		}
	})

	t.Run("CreateDirectory", func(t *testing.T) {
		testDir := filepath.Join(mountPoint, "testdir")

		// Create directory
		err := os.Mkdir(testDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}

		// Verify it exists
		info, err := os.Stat(testDir)
		if err != nil {
			t.Fatalf("Failed to stat directory: %v", err)
		}
		if !info.IsDir() {
			t.Fatalf("Expected directory, got file")
		}
	})

	t.Run("ListDirectory", func(t *testing.T) {
		// Create some files
		for i := 0; i < 3; i++ {
			testFile := filepath.Join(mountPoint, "file"+string(rune('0'+i))+".txt")
			err := os.WriteFile(testFile, []byte("content"), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}
		}

		// List directory
		entries, err := os.ReadDir(mountPoint)
		if err != nil {
			t.Fatalf("Failed to read directory: %v", err)
		}
		if len(entries) < 3 {
			t.Fatalf("Expected at least 3 entries, got %d", len(entries))
		}
	})

	t.Run("RenameFile", func(t *testing.T) {
		oldPath := filepath.Join(mountPoint, "old.txt")
		newPath := filepath.Join(mountPoint, "new.txt")

		// Create file
		err := os.WriteFile(oldPath, []byte("rename test"), 0644)
		if err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}

		// Rename it
		err = os.Rename(oldPath, newPath)
		if err != nil {
			t.Fatalf("Failed to rename file: %v", err)
		}

		// Verify old doesn't exist
		_, err = os.Stat(oldPath)
		if !os.IsNotExist(err) {
			t.Fatalf("Old file still exists after rename")
		}

		// Verify new exists
		data, err := os.ReadFile(newPath)
		if err != nil {
			t.Fatalf("Failed to read renamed file: %v", err)
		}
		expected := []byte("rename test")
		if !bytes.Equal(data, expected) {
			t.Fatalf("Expected %q, got %q", expected, data)
		}
	})

	t.Run("DeleteFile", func(t *testing.T) {
		testFile := filepath.Join(mountPoint, "delete.txt")

		// Create file
		err := os.WriteFile(testFile, []byte("delete me"), 0644)
		if err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}

		// Delete it
		err = os.Remove(testFile)
		if err != nil {
			t.Fatalf("Failed to delete file: %v", err)
		}

		// Verify it's gone
		_, err = os.Stat(testFile)
		if !os.IsNotExist(err) {
			t.Fatalf("File still exists after deletion")
		}
	})

	t.Run("WriteToExistingFile", func(t *testing.T) {
		testFile := filepath.Join(mountPoint, "modify.txt")

		// Create file
		err := os.WriteFile(testFile, []byte("initial"), 0644)
		if err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}

		// Overwrite it
		err = os.WriteFile(testFile, []byte("modified content"), 0644)
		if err != nil {
			t.Fatalf("Failed to modify file: %v", err)
		}

		// Verify new content
		data, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		expected := []byte("modified content")
		if !bytes.Equal(data, expected) {
			t.Fatalf("Expected %q, got %q", expected, data)
		}
	})

	t.Run("NestedDirectories", func(t *testing.T) {
		nestedPath := filepath.Join(mountPoint, "a", "b", "c")

		// Create nested directories
		err := os.MkdirAll(nestedPath, 0755)
		if err != nil {
			t.Fatalf("Failed to create nested directories: %v", err)
		}

		// Create file in nested directory
		testFile := filepath.Join(nestedPath, "nested.txt")
		err = os.WriteFile(testFile, []byte("nested content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create nested file: %v", err)
		}

		// Read it back
		data, err := os.ReadFile(testFile)
		if err != nil {
			t.Fatalf("Failed to read nested file: %v", err)
		}
		expected := []byte("nested content")
		if !bytes.Equal(data, expected) {
			t.Fatalf("Expected %q, got %q", expected, data)
		}
	})
}