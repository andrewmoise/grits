package gritsd

import (
	"grits/internal/grits"
	"io"
	"os"
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

	blobNode, err := localVolume.CreateBlobNode(blobFile.GetAddress(), blobFile.GetSize())
	if err != nil {
		t.Fatalf("Failed to create metadata for blob node: %v", err)
	}
	defer blobNode.Release()

	// Link the new blob to the local volume using the test path.
	err = localVolume.LinkByMetadata(testPath, blobNode.MetadataBlob().GetAddress())
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
	testNode, err := localVolumeReloaded.LookupNode(testPath)
	if err != nil {
		t.Fatalf("Failed to lookup content in local volume: %v", err)
	}
	defer testNode.Release()

	cachedFile, err := server.BlobStore.ReadFile(&grits.BlobAddr{Hash: testNode.Metadata().ContentHash})
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

func TestLocalVolumeOperations(t *testing.T) {
	// Setup server and local volume
	server, cleanup := SetupTestServer(t)
	defer cleanup()

	volumeName := "testops"
	localConfig := &LocalVolumeConfig{VolumeName: volumeName}
	localVolume, err := NewLocalVolume(localConfig, server, false)
	if err != nil {
		t.Fatalf("Failed to create local volume: %v", err)
	}
	server.AddModule(localVolume)

	// Start the server
	server.Start()
	defer server.Stop()

	// Test PutBlob - Create and add a temp file
	testContent := "Test content for PutBlob"
	tmpFile, err := os.CreateTemp("", "putblob-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(testContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// Reopen the file for reading
	file, err := os.Open(tmpPath)
	if err != nil {
		t.Fatalf("Failed to open temp file: %v", err)
	}
	defer file.Close()

	// Use PutBlob to add the file to the blob store
	contentAddr, err := localVolume.PutBlob(file)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	// Test GetBlob - Retrieve the blob we just put
	retrievedBlob, err := localVolume.GetBlob(contentAddr)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	defer retrievedBlob.Release()

	// Verify the content matches
	reader, err := retrievedBlob.Reader()
	if err != nil {
		t.Fatalf("Failed to get reader for blob: %v", err)
	}
	defer reader.Close()

	contentBytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Failed to read blob content: %v", err)
	}

	if string(contentBytes) != testContent {
		t.Errorf("Content mismatch: expected %q, got %q", testContent, string(contentBytes))
	}

	// Test CreateBlobNode - Create a node for the content we just added
	blobNode, err := localVolume.CreateBlobNode(contentAddr, int64(len(testContent)))
	if err != nil {
		t.Fatalf("CreateBlobNode failed: %v", err)
	}

	// Test CreateTreeNode - Create an empty directory
	treeNode, err := localVolume.CreateTreeNode()
	if err != nil {
		t.Fatalf("CreateTreeNode failed: %v", err)
	}

	// Scenario 1: Create a node, link it, then release our reference
	err = localVolume.LinkByMetadata("test", treeNode.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Can't make empty 'test' directory: %v", err)
	}

	blobPath := "test/file.txt"
	err = localVolume.LinkByMetadata(blobPath, blobNode.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("LinkByMetadata for blob failed: %v", err)
	}
	// Now we can release our reference - the NameStore has its own reference
	blobNode.Release()

	// Scenario 2: Create a node, get its address, then release without linking
	// This demonstrates how you might send the address to another system without linking locally
	treeMetadataAddr := treeNode.MetadataBlob().GetAddress()
	// Just use the address, no need to link it
	if treeMetadataAddr.Hash == "" {
		t.Fatalf("Tree metadata address is empty")
	}
	// Now release our reference since we're done with it
	treeNode.Release()

	// Create another tree node to demonstrate direct linking
	anotherTreeNode, err := localVolume.CreateTreeNode()
	if err != nil {
		t.Fatalf("Failed to create another tree node: %v", err)
	}

	// Scenario 3: Link and release
	treePath := "test/emptydir"
	err = localVolume.LinkByMetadata(treePath, anotherTreeNode.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("LinkByMetadata for tree failed: %v", err)
	}
	anotherTreeNode.Release() // Release our reference now that it's linked

	// Verify we can look up the blob we linked in scenario 1
	retrievedBlobNode, err := localVolume.LookupNode(blobPath)
	if err != nil {
		t.Fatalf("Failed to lookup blob path: %v", err)
	}
	defer retrievedBlobNode.Release() // Make sure to release after lookup

	// Verify content matches
	retrievedContent, err := retrievedBlobNode.ExportedBlob().Reader()
	if err != nil {
		t.Fatalf("Failed to get reader for retrieved blob: %v", err)
	}
	defer retrievedContent.Close()

	retrievedBytes, err := io.ReadAll(retrievedContent)
	if err != nil {
		t.Fatalf("Failed to read retrieved content: %v", err)
	}

	if string(retrievedBytes) != testContent {
		t.Errorf("Retrieved content mismatch: expected %q, got %q",
			testContent, string(retrievedBytes))
	}

	// Verify we can access the tree we linked in scenario 3
	retrievedTreeNode, err := localVolume.LookupNode(treePath)
	if err != nil {
		t.Fatalf("Failed to lookup tree path: %v", err)
	}
	defer retrievedTreeNode.Release() // Make sure to release after lookup

	// Verify it's empty as expected
	children := retrievedTreeNode.Children()
	if len(children) != 0 {
		t.Errorf("Expected empty tree node, but found %d children", len(children))
	}

	// Link a child into the tree to show we can modify after creation
	childPath := treePath + "/childfile.txt"

	// First create a new blob node for the child
	childNode, err := localVolume.CreateBlobNode(contentAddr, int64(len(testContent)))
	if err != nil {
		t.Fatalf("Failed to create child blob node: %v", err)
	}

	// Link it into the tree and release
	err = localVolume.LinkByMetadata(childPath, childNode.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Failed to link child to tree: %v", err)
	}
	childNode.Release() // Release our reference now that it's linked

	// Verify we can access the child
	retrievedChild, err := localVolume.LookupNode(childPath)
	if err != nil {
		t.Fatalf("Failed to lookup child path: %v", err)
	}
	defer retrievedChild.Release() // Make sure to release after lookup

	// Verify content matches
	childContent, err := retrievedChild.ExportedBlob().Reader()
	if err != nil {
		t.Fatalf("Failed to get reader for child content: %v", err)
	}
	defer childContent.Close()

	childBytes, err := io.ReadAll(childContent)
	if err != nil {
		t.Fatalf("Failed to read child content: %v", err)
	}

	if string(childBytes) != testContent {
		t.Errorf("Child content mismatch: expected %q, got %q",
			testContent, string(childBytes))
	}
}
