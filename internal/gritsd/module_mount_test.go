package gritsd

import (
	"bytes"
	"grits/internal/grits"
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

func TestExternalModificationsWithInvalidation(t *testing.T) {
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

	// Get the volume for direct manipulation
	vol := server.Volumes["root"]
	if vol == nil {
		t.Fatalf("Could not find root volume")
	}

	// Create some initial content via FUSE
	t.Log("Creating initial content via FUSE...")

	// Create directory structure: testdir/subdir/
	testDir := filepath.Join(mountPoint, "testdir")
	err = os.Mkdir(testDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create testdir: %v", err)
	}

	subDir := filepath.Join(testDir, "subdir")
	err = os.Mkdir(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Create some files
	file1Path := filepath.Join(testDir, "file1.txt")
	err = os.WriteFile(file1Path, []byte("file1 content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	file2Path := filepath.Join(testDir, "file2.txt")
	err = os.WriteFile(file2Path, []byte("file2 content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	file3Path := filepath.Join(subDir, "file3.txt")
	err = os.WriteFile(file3Path, []byte("file3 content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create file3: %v", err)
	}

	// Verify initial state via FUSE
	t.Log("Verifying initial state...")
	data, err := os.ReadFile(file1Path)
	if err != nil {
		t.Fatalf("Failed to read file1: %v", err)
	}
	if !bytes.Equal(data, []byte("file1 content")) {
		t.Fatalf("file1 has wrong content: %q", data)
	}

	// Now make external modifications directly to the volume
	t.Log("Making external modifications...")

	// 1. Delete file1.txt
	t.Log("  Deleting testdir/file1.txt...")
	err = vol.LinkByMetadata("testdir/file1.txt", grits.NilAddr)
	if err != nil {
		t.Fatalf("Failed to delete file1 via volume: %v", err)
	}

	// 2. Rename file2.txt to file2_renamed.txt
	t.Log("  Renaming testdir/file2.txt...")
	file2Node, err := vol.LookupNode("testdir/file2.txt")
	if err != nil {
		t.Fatalf("Failed to lookup file2: %v", err)
	}
	file2Addr := file2Node.MetadataBlob().GetAddress()
	file2Node.Release()

	linkRequests := []*grits.LinkRequest{
		{Path: "testdir/file2.txt", NewAddr: grits.NilAddr},
		{Path: "testdir/file2_renamed.txt", NewAddr: file2Addr},
	}
	_, err = vol.MultiLink(linkRequests, false)
	if err != nil {
		t.Fatalf("Failed to rename file2 via volume: %v", err)
	}

	// 3. Create a new file with content
	t.Log("  Creating testdir/newfile.txt...")
	newContent := []byte("externally created content")
	newBlob, err := server.BlobStore.AddDataBlock(newContent)
	if err != nil {
		t.Fatalf("Failed to create blob: %v", err)
	}
	defer newBlob.Release()

	newNode, err := vol.CreateBlobNode(newBlob.GetAddress(), newBlob.GetSize())
	if err != nil {
		t.Fatalf("Failed to create blob node: %v", err)
	}
	defer newNode.Release()

	err = vol.LinkByMetadata("testdir/newfile.txt", newNode.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Failed to link newfile: %v", err)
	}

	// 4. Delete the subdir directory (which has file3.txt in it)
	t.Log("  Deleting testdir/subdir/ (non-empty)...")
	// First delete the file inside
	err = vol.LinkByMetadata("testdir/subdir/file3.txt", grits.NilAddr)
	if err != nil {
		t.Fatalf("Failed to delete file3: %v", err)
	}
	// Then delete the directory
	err = vol.LinkByMetadata("testdir/subdir", grits.NilAddr)
	if err != nil {
		t.Fatalf("Failed to delete subdir: %v", err)
	}

	// Give kernel a moment to process invalidations
	t.Log("Waiting for invalidations to propagate...")
	time.Sleep(10 * time.Millisecond)

	// Now verify the state via FUSE
	t.Log("Verifying state after external modifications...")

	// 1. file1.txt should no longer exist
	t.Log("  Checking file1.txt is gone...")
	_, err = os.Stat(file1Path)
	if !os.IsNotExist(err) {
		t.Fatalf("file1 should not exist after deletion, but stat returned: %v", err)
	}

	// 2. file2.txt should not exist, but file2_renamed.txt should
	t.Log("  Checking file2.txt rename...")
	_, err = os.Stat(file2Path)
	if !os.IsNotExist(err) {
		t.Fatalf("file2.txt should not exist after rename, but stat returned: %v", err)
	}

	file2RenamedPath := filepath.Join(testDir, "file2_renamed.txt")
	data, err = os.ReadFile(file2RenamedPath)
	if err != nil {
		t.Fatalf("Failed to read file2_renamed.txt: %v", err)
	}
	if !bytes.Equal(data, []byte("file2 content")) {
		t.Fatalf("file2_renamed.txt has wrong content: %q", data)
	}

	// 3. newfile.txt should exist with correct content
	t.Log("  Checking newfile.txt exists...")
	newFilePath := filepath.Join(testDir, "newfile.txt")
	data, err = os.ReadFile(newFilePath)
	if err != nil {
		t.Fatalf("Failed to read newfile.txt: %v", err)
	}
	if !bytes.Equal(data, newContent) {
		t.Fatalf("newfile.txt has wrong content: expected %q, got %q", newContent, data)
	}

	// 4. subdir and file3.txt should not exist
	t.Log("  Checking subdir is gone...")
	_, err = os.Stat(subDir)
	if !os.IsNotExist(err) {
		t.Fatalf("subdir should not exist after deletion, but stat returned: %v", err)
	}

	_, err = os.Stat(file3Path)
	if !os.IsNotExist(err) {
		t.Fatalf("file3.txt should not exist after deletion, but stat returned: %v", err)
	}

	// 5. Verify testdir listing shows only the expected files
	t.Log("  Checking testdir listing...")
	entries, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatalf("Failed to read testdir: %v", err)
	}

	expectedFiles := map[string]bool{
		"file2_renamed.txt": false,
		"newfile.txt":       false,
	}

	for _, entry := range entries {
		if _, expected := expectedFiles[entry.Name()]; expected {
			expectedFiles[entry.Name()] = true
		} else {
			t.Errorf("Unexpected file in testdir: %s", entry.Name())
		}
	}

	for name, found := range expectedFiles {
		if !found {
			t.Errorf("Expected file not found in testdir: %s", name)
		}
	}

	t.Log("All external modifications verified successfully!")
}
