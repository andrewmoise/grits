package gritsd

import (
	"grits/internal/grits"
	"io"
	"testing"
)

func TestLocalVolumePersistenceDirect(t *testing.T) {
	// Setup server and local volume
	server, cleanup := SetupTestServer(t)
	defer cleanup()

	volumeName := "testlocal"
	localConfig := &LocalVolumeConfig{VolumeName: volumeName}
	localVolume, err := NewLocalVolume(localConfig, server, false)
	if err != nil {
		t.Fatalf("Failed to create local volume: %v", err)
	}
	server.AddModule(localVolume)

	// Start the server to ensure all components are initialized properly.
	server.Start()
	defer server.Stop()

	// Create a blob representing content to be linked in the local volume.
	testContent := "Hello, local!"
	testPath := "testPage"
	blobFile, err := server.BlobStore.AddDataBlock([]byte(testContent))
	if err != nil {
		t.Fatalf("Failed to add content to blob store: %v", err)
	}
	defer blobFile.Release()

	typedAddr := grits.NewTypedFileAddr(blobFile.GetAddress().Hash, blobFile.GetSize(), grits.Blob)

	// Link the new blob to the local volume using the test path.
	err = localVolume.Link(testPath, typedAddr)
	if err != nil {
		t.Fatalf("Failed to link blob in local volume: %v", err)
	}

	// Simulate a server restart by explicitly invoking save and then reloading the volume.
	if err = localVolume.save(); err != nil {
		t.Fatalf("Failed to save local volume: %v", err)
	}

	// Reload the local volume to simulate reading from disk after a restart.
	localVolumeReloaded, err := NewLocalVolume(localConfig, server, false)
	if err != nil {
		t.Fatalf("Failed to reload local volume: %v", err)
	}

	// Verify the content persisted by looking up the previously linked path.
	addr, err := localVolumeReloaded.Lookup(testPath)
	if err != nil {
		t.Fatalf("Failed to lookup content in local volume: %v", err)
	}

	cachedFile, err := server.BlobStore.ReadFile(&addr.BlobAddr)
	if err != nil {
		t.Fatalf("Failed to read file contents: %v", err)
	}
	defer cachedFile.Release()

	// Read the file content from the blob store to verify it matches the original content.
	cf, err := server.BlobStore.ReadFile(cachedFile.GetAddress())
	if err != nil {
		t.Fatalf("Failed to read content from blob store: %v", err)
	}
	defer cf.Release()

	readFile, err := cf.Reader()
	if err != nil {
		t.Fatalf("Failed to open CF for checking")
	}
	defer readFile.Close()

	contentBytes, err := io.ReadAll(readFile)
	if err != nil {
		t.Fatalf("Can't read from CF: %v", err)
	}

	if string(contentBytes) != testContent {
		t.Errorf("Content mismatch: expected %s, got %s", testContent, string(contentBytes))
	}
}
