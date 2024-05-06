package server

import (
	"grits/internal/grits"
	"os"
	"testing"
)

func TestWikiVolumePersistenceDirect(t *testing.T) {
	// Setup server and wiki volume
	server, cleanup := SetupTestServer(t)
	defer cleanup()

	volumeName := "testwiki"
	wikiConfig := &WikiVolumeConfig{VolumeName: volumeName}
	wikiVolume, err := NewWikiVolume(wikiConfig, server)
	if err != nil {
		t.Fatalf("Failed to create wiki volume: %v", err)
	}
	server.AddModule(wikiVolume)

	// Start the server to ensure all components are initialized properly.
	server.Start()
	defer server.Stop()

	// Create a blob representing content to be linked in the wiki volume.
	testContent := "Hello, wiki!"
	testPath := "testPage"
	blobFile, err := server.BlobStore.AddDataBlock([]byte(testContent))
	if err != nil {
		t.Fatalf("Failed to add content to blob store: %v", err)
	}
	defer blobFile.Release()

	typedAddr := grits.NewTypedFileAddr(blobFile.Address.Hash, blobFile.Size, grits.Blob)

	// Link the new blob to the wiki volume using the test path.
	err = wikiVolume.Link(testPath, typedAddr)
	if err != nil {
		t.Fatalf("Failed to link blob in wiki volume: %v", err)
	}

	// Simulate a server restart by explicitly invoking save and then reloading the volume.
	if err = wikiVolume.save(); err != nil {
		t.Fatalf("Failed to save wiki volume: %v", err)
	}

	// Reload the wiki volume to simulate reading from disk after a restart.
	wikiVolumeReloaded, err := NewWikiVolume(wikiConfig, server)
	if err != nil {
		t.Fatalf("Failed to reload wiki volume: %v", err)
	}

	// Verify the content persisted by looking up the previously linked path.
	addr, err := wikiVolumeReloaded.Lookup(testPath)
	if err != nil {
		t.Fatalf("Failed to lookup content in wiki volume: %v", err)
	}

	cachedFile, err := server.BlobStore.ReadFile(&addr.BlobAddr)
	if err != nil {
		t.Fatalf("Failed to read file contents: %v", err)
	}
	defer cachedFile.Release()

	// Read the file content from the blob store to verify it matches the original content.
	cf, err := server.BlobStore.ReadFile(cachedFile.Address)
	if err != nil {
		t.Fatalf("Failed to read content from blob store: %v", err)
	}
	defer cf.Release()

	contentBytes, err := os.ReadFile(cf.Path)
	if err != nil {
		t.Fatalf("Can't read from %s: %v", cf.Path, err)
	}

	if string(contentBytes) != testContent {
		t.Errorf("Content mismatch: expected %s, got %s", testContent, string(contentBytes))
	}
}
