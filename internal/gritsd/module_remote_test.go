package gritsd

import (
	"bytes"
	"fmt"
	"grits/internal/grits"
	"io"
	"testing"
	"time"
)

// Helper function to set up a server with HTTP and a local volume
func setupLocalServerWithData(t *testing.T, port int, volumeName string) (*Server, func(), map[string]grits.BlobAddr) {
	server, cleanup := SetupTestServer(t,
		WithHttpModule(port),
		WithLocalVolume(volumeName))

	// Start the server
	server.Start()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Get the local volume to populate it with test data
	var localVolume *LocalVolume
	for _, module := range server.Modules {
		if lv, ok := module.(*LocalVolume); ok && lv.GetVolumeName() == volumeName {
			localVolume = lv
			break
		}
	}

	if localVolume == nil {
		t.Fatalf("Could not find local volume %s", volumeName)
	}

	// Create test content
	testData := make(map[string]grits.BlobAddr)

	// Add some test files
	content1, err := server.BlobStore.AddDataBlock([]byte("Hello from remote server!"))
	if err != nil {
		t.Fatalf("Failed to add content1: %v", err)
	}
	defer content1.Release()

	content2, err := server.BlobStore.AddDataBlock([]byte("Second file content"))
	if err != nil {
		t.Fatalf("Failed to add content2: %v", err)
	}
	defer content2.Release()

	content3, err := server.BlobStore.AddDataBlock([]byte("Nested directory file"))
	if err != nil {
		t.Fatalf("Failed to add content3: %v", err)
	}
	defer content3.Release()

	// Create blob nodes
	node1, err := localVolume.CreateBlobNode(content1.GetAddress(), content1.GetSize())
	if err != nil {
		t.Fatalf("Failed to create node1: %v", err)
	}
	defer node1.Release()

	node2, err := localVolume.CreateBlobNode(content2.GetAddress(), content2.GetSize())
	if err != nil {
		t.Fatalf("Failed to create node2: %v", err)
	}
	defer node2.Release()

	node3, err := localVolume.CreateBlobNode(content3.GetAddress(), content3.GetSize())
	if err != nil {
		t.Fatalf("Failed to create node3: %v", err)
	}
	defer node3.Release()

	// Create directory nodes
	dir1, err := localVolume.CreateTreeNode()
	if err != nil {
		t.Fatalf("Failed to create dir1: %v", err)
	}
	defer dir1.Release()

	dir2, err := localVolume.CreateTreeNode()
	if err != nil {
		t.Fatalf("Failed to create dir2: %v", err)
	}
	defer dir2.Release()

	// Link everything into the volume
	err = localVolume.LinkByMetadata("testdir", dir1.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Failed to link testdir: %v", err)
	}

	err = localVolume.LinkByMetadata("testdir/file1.txt", node1.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Failed to link file1: %v", err)
	}
	testData["testdir/file1.txt"] = content1.GetAddress()

	err = localVolume.LinkByMetadata("testdir/file2.txt", node2.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Failed to link file2: %v", err)
	}
	testData["testdir/file2.txt"] = content2.GetAddress()

	err = localVolume.LinkByMetadata("testdir/subdir", dir2.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Failed to link subdir: %v", err)
	}

	err = localVolume.LinkByMetadata("testdir/subdir/file3.txt", node3.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Failed to link file3: %v", err)
	}
	testData["testdir/subdir/file3.txt"] = content3.GetAddress()

	// Also add a file at the root level
	rootFile, err := server.BlobStore.AddDataBlock([]byte("Root level file"))
	if err != nil {
		t.Fatalf("Failed to add root file: %v", err)
	}
	defer rootFile.Release()

	rootNode, err := localVolume.CreateBlobNode(rootFile.GetAddress(), rootFile.GetSize())
	if err != nil {
		t.Fatalf("Failed to create root node: %v", err)
	}
	defer rootNode.Release()

	err = localVolume.LinkByMetadata("root.txt", rootNode.MetadataBlob().GetAddress())
	if err != nil {
		t.Fatalf("Failed to link root file: %v", err)
	}
	testData["root.txt"] = rootFile.GetAddress()

	// Force a checkpoint to ensure everything is persisted
	localVolume.Checkpoint()

	return server, func() {
		server.Stop()
		cleanup()
	}, testData
}

// Helper to set up a server with a remote volume
func setupRemoteServer(t *testing.T, remoteURL string, volumeName string) (*Server, func(), *RemoteVolume) {
	server, cleanup := SetupTestServer(t)

	remoteConfig := &RemoteVolumeConfig{
		VolumeName: volumeName,
		RemoteURL:  remoteURL,
	}

	remoteVolume, err := NewRemoteVolume(remoteConfig, server)
	if err != nil {
		t.Fatalf("Failed to create remote volume: %v", err)
	}

	server.AddModule(remoteVolume)

	// Start the server
	server.Start()

	return server, func() {
		server.Stop()
		cleanup()
	}, remoteVolume
}

func TestRemoteVolumeLookup(t *testing.T) {
	// Set up the source server with data
	_, sourceCleanup, testData := setupLocalServerWithData(t, 3001, "testvolume")
	defer sourceCleanup()

	// Set up the client server with remote volume
	_, clientCleanup, remoteVolume := setupRemoteServer(t, "http://localhost:3001", "testvolume")
	defer clientCleanup()

	// Test looking up a file
	t.Run("LookupFile", func(t *testing.T) {
		node, err := remoteVolume.LookupNode("testdir/file1.txt")
		if err != nil {
			t.Fatalf("Failed to lookup file1.txt: %v", err)
		}
		defer node.Release()

		// Verify we got the right node type
		blobNode, ok := node.(*grits.BlobNode)
		if !ok {
			t.Fatalf("Expected BlobNode, got %T", node)
		}

		// Verify content
		cf, err := blobNode.ExportedBlob().Reader()
		if err != nil {
			t.Fatalf("Failed to get reader: %v", err)
		}
		defer cf.Close()

		content, err := io.ReadAll(cf)
		if err != nil {
			t.Fatalf("Failed to read content: %v", err)
		}

		expectedContent := "Hello from remote server!"
		if string(content) != expectedContent {
			t.Errorf("Content mismatch: expected %q, got %q", expectedContent, string(content))
		}
	})

	// Test looking up a directory
	t.Run("LookupDirectory", func(t *testing.T) {
		node, err := remoteVolume.LookupNode("testdir")
		if err != nil {
			t.Fatalf("Failed to lookup testdir: %v", err)
		}
		defer node.Release()

		// Verify we got a tree node
		treeNode, ok := node.(*grits.TreeNode)
		if !ok {
			t.Fatalf("Expected TreeNode, got %T", node)
		}

		// Check children
		children := treeNode.Children()
		if len(children) != 3 { // file1.txt, file2.txt, subdir
			t.Errorf("Expected 3 children, got %d", len(children))
		}

		// Verify expected children exist
		expectedChildren := []string{"file1.txt", "file2.txt", "subdir"}
		for _, expected := range expectedChildren {
			if _, exists := children[expected]; !exists {
				t.Errorf("Expected child %s not found", expected)
			}
		}
	})

	// Test looking up nested file
	t.Run("LookupNestedFile", func(t *testing.T) {
		node, err := remoteVolume.LookupNode("testdir/subdir/file3.txt")
		if err != nil {
			t.Fatalf("Failed to lookup nested file: %v", err)
		}
		defer node.Release()

		// Verify content
		cf, err := node.ExportedBlob().Reader()
		if err != nil {
			t.Fatalf("Failed to get reader: %v", err)
		}
		defer cf.Close()

		content, err := io.ReadAll(cf)
		if err != nil {
			t.Fatalf("Failed to read content: %v", err)
		}

		expectedContent := "Nested directory file"
		if string(content) != expectedContent {
			t.Errorf("Content mismatch: expected %q, got %q", expectedContent, string(content))
		}
	})

	// Test non-existent file
	t.Run("LookupNonExistent", func(t *testing.T) {
		_, err := remoteVolume.LookupNode("nonexistent/file.txt")
		if err != grits.ErrNotExist {
			t.Errorf("Expected ErrNotExist, got %v", err)
		}
	})

	// Test root level file
	t.Run("LookupRootFile", func(t *testing.T) {
		node, err := remoteVolume.LookupNode("root.txt")
		if err != nil {
			t.Fatalf("Failed to lookup root.txt: %v", err)
		}
		defer node.Release()

		cf, err := node.ExportedBlob().Reader()
		if err != nil {
			t.Fatalf("Failed to get reader: %v", err)
		}
		defer cf.Close()

		content, err := io.ReadAll(cf)
		if err != nil {
			t.Fatalf("Failed to read content: %v", err)
		}

		expectedContent := "Root level file"
		if string(content) != expectedContent {
			t.Errorf("Content mismatch: expected %q, got %q", expectedContent, string(content))
		}
	})

	// Test GetBlob directly
	t.Run("GetBlob", func(t *testing.T) {
		// Get the blob address for one of our test files
		blobAddr := testData["testdir/file1.txt"]

		cf, err := remoteVolume.GetBlob(blobAddr)
		if err != nil {
			t.Fatalf("Failed to get blob: %v", err)
		}
		defer cf.Release()

		reader, err := cf.Reader()
		if err != nil {
			t.Fatalf("Failed to get reader: %v", err)
		}
		defer reader.Close()

		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Failed to read content: %v", err)
		}

		expectedContent := "Hello from remote server!"
		if string(content) != expectedContent {
			t.Errorf("Content mismatch: expected %q, got %q", expectedContent, string(content))
		}
	})

	// Test GetFileNode with metadata address
	t.Run("GetFileNode", func(t *testing.T) {
		// First lookup to get the metadata address
		node, err := remoteVolume.LookupNode("testdir/file2.txt")
		if err != nil {
			t.Fatalf("Failed to lookup file2.txt: %v", err)
		}
		metadataAddr := node.MetadataBlob().GetAddress()
		node.Release()

		// Now use GetFileNode directly
		node2, err := remoteVolume.GetFileNode(metadataAddr)
		if err != nil {
			t.Fatalf("Failed to get file node: %v", err)
		}
		defer node2.Release()

		// Verify content
		cf, err := node2.ExportedBlob().Reader()
		if err != nil {
			t.Fatalf("Failed to get reader: %v", err)
		}
		defer cf.Close()

		content, err := io.ReadAll(cf)
		if err != nil {
			t.Fatalf("Failed to read content: %v", err)
		}

		expectedContent := "Second file content"
		if string(content) != expectedContent {
			t.Errorf("Content mismatch: expected %q, got %q", expectedContent, string(content))
		}
	})
}

func TestRemoteVolumeLocalOperations(t *testing.T) {
	// Set up the source server with data
	_, sourceCleanup, _ := setupLocalServerWithData(t, 3002, "testvolume")
	defer sourceCleanup()

	// Set up the client server with remote volume
	clientServer, clientCleanup, remoteVolume := setupRemoteServer(t, "http://localhost:3002", "testvolume")
	defer clientCleanup()

	// Test creating local nodes (these should work even though we can't link them remotely)
	t.Run("CreateLocalTreeNode", func(t *testing.T) {
		treeNode, err := remoteVolume.CreateTreeNode()
		if err != nil {
			t.Fatalf("Failed to create tree node: %v", err)
		}
		defer treeNode.Release()

		// Verify it's a valid tree node
		children := treeNode.Children()
		if len(children) != 0 {
			t.Errorf("Expected empty tree node, got %d children", len(children))
		}
	})

	t.Run("CreateLocalBlobNode", func(t *testing.T) {
		// First add some content to the local blob store
		testContent := "Local content in remote volume"
		cf, err := clientServer.BlobStore.AddDataBlock([]byte(testContent))
		if err != nil {
			t.Fatalf("Failed to add content: %v", err)
		}
		defer cf.Release()

		// Create a blob node for it
		blobNode, err := remoteVolume.CreateBlobNode(cf.GetAddress(), cf.GetSize())
		if err != nil {
			t.Fatalf("Failed to create blob node: %v", err)
		}
		defer blobNode.Release()

		// Verify we can read the content back
		reader, err := blobNode.ExportedBlob().Reader()
		if err != nil {
			t.Fatalf("Failed to get reader: %v", err)
		}
		defer reader.Close()

		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Failed to read content: %v", err)
		}

		if string(content) != testContent {
			t.Errorf("Content mismatch: expected %q, got %q", testContent, string(content))
		}
	})

	t.Run("AddAndGetLocalBlob", func(t *testing.T) {
		// Add a blob locally
		testData := []byte("Test blob data")
		cf, err := remoteVolume.server.BlobStore.AddDataBlock(testData)
		if err != nil {
			t.Fatalf("Failed to add blob: %v", err)
		}
		blobAddr := cf.GetAddress()
		cf.Release()

		// Get it back
		retrieved, err := remoteVolume.GetBlob(blobAddr)
		if err != nil {
			t.Fatalf("Failed to get blob: %v", err)
		}
		defer retrieved.Release()

		reader, err := retrieved.Reader()
		if err != nil {
			t.Fatalf("Failed to get reader: %v", err)
		}
		defer reader.Close()

		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("Failed to read content: %v", err)
		}

		if !bytes.Equal(content, testData) {
			t.Errorf("Content mismatch: expected %v, got %v", testData, content)
		}
	})

	// Test that write operations fail as expected
	t.Run("WriteOperationsFail", func(t *testing.T) {
		// Create a node to try to link
		cf, err := clientServer.BlobStore.AddDataBlock([]byte("test"))
		if err != nil {
			t.Fatalf("Failed to add content: %v", err)
		}
		defer cf.Release()

		node, err := remoteVolume.CreateBlobNode(cf.GetAddress(), cf.GetSize())
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
		defer node.Release()

		// LinkByMetadata should fail
		err = remoteVolume.LinkByMetadata("test.txt", node.MetadataBlob().GetAddress())
		if err == nil {
			t.Error("Expected LinkByMetadata to fail on remote volume")
		}

		// MultiLink should also fail
		linkReq := []*grits.LinkRequest{{
			Path:    "test.txt",
			NewAddr: node.MetadataBlob().GetAddress(),
		}}
		_, err = remoteVolume.MultiLink(linkReq, false)
		if err == nil {
			t.Error("Expected MultiLink to fail on remote volume")
		}
	})
}

func TestRemoteVolumeMultipleFiles(t *testing.T) {
	// Set up source server with many files
	sourceServer, sourceCleanup, _ := setupLocalServerWithData(t, 3003, "multitest")
	defer sourceCleanup()

	// Add more files to test scaling
	var localVolume *LocalVolume
	for _, module := range sourceServer.Modules {
		if lv, ok := module.(*LocalVolume); ok && lv.GetVolumeName() == "multitest" {
			localVolume = lv
			break
		}
	}

	// Add 10 more files
	for i := 0; i < 10; i++ {
		content := fmt.Sprintf("File %d content", i)
		cf, err := sourceServer.BlobStore.AddDataBlock([]byte(content))
		if err != nil {
			t.Fatalf("Failed to add content %d: %v", i, err)
		}
		defer cf.Release()

		node, err := localVolume.CreateBlobNode(cf.GetAddress(), cf.GetSize())
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		defer node.Release()

		path := fmt.Sprintf("testdir/file%d.txt", i)
		err = localVolume.LinkByMetadata(path, node.MetadataBlob().GetAddress())
		if err != nil {
			t.Fatalf("Failed to link file %d: %v", i, err)
		}
	}

	// Set up remote client
	_, clientCleanup, remoteVolume := setupRemoteServer(t, "http://localhost:3003", "multitest")
	defer clientCleanup()

	// Test accessing multiple files
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("testdir/file%d.txt", i)
		node, err := remoteVolume.LookupNode(path)
		if err != nil {
			t.Errorf("Failed to lookup %s: %v", path, err)
			continue
		}

		cf, err := node.ExportedBlob().Reader()
		if err != nil {
			node.Release()
			t.Errorf("Failed to get reader for %s: %v", path, err)
			continue
		}

		content, err := io.ReadAll(cf)
		cf.Close()
		node.Release()
		if err != nil {
			t.Errorf("Failed to read %s: %v", path, err)
			continue
		}

		expected := fmt.Sprintf("File %d content", i)
		if string(content) != expected {
			t.Errorf("Content mismatch for %s: expected %q, got %q", path, expected, string(content))
		}
	}
}

func TestRemoteVolumeCheckpoint(t *testing.T) {
	// Set up the source server
	_, sourceCleanup, _ := setupLocalServerWithData(t, 3004, "checkpoint")
	defer sourceCleanup()

	// Set up the client server with remote volume
	_, clientCleanup, remoteVolume := setupRemoteServer(t, "http://localhost:3004", "checkpoint")
	defer clientCleanup()

	// Fetch some data to populate the local cache
	node, err := remoteVolume.LookupNode("testdir/file1.txt")
	if err != nil {
		t.Fatalf("Failed to lookup file: %v", err)
	}
	node.Release()

	// Test checkpoint
	err = remoteVolume.Checkpoint()
	if err != nil {
		t.Errorf("Checkpoint failed: %v", err)
	}

	// Test cleanup
	err = remoteVolume.Cleanup()
	if err != nil {
		t.Errorf("Cleanup failed: %v", err)
	}
}

func TestRemoteVolumeProperties(t *testing.T) {
	// Set up servers
	_, sourceCleanup, _ := setupLocalServerWithData(t, 3005, "props")
	defer sourceCleanup()

	_, clientCleanup, remoteVolume := setupRemoteServer(t, "http://localhost:3005", "props")
	defer clientCleanup()

	// Test volume name
	volumeName := remoteVolume.GetVolumeName()
	expectedName := "[localhost:3005]:props" // Format: host:volumename
	if volumeName != expectedName {
		t.Errorf("Expected volume name %s, got %s", expectedName, volumeName)
	}

	// Test read-only status
	if !remoteVolume.isReadOnly() {
		t.Error("Remote volume should be read-only")
	}

	// Test module name
	if remoteVolume.GetModuleName() != "remote" {
		t.Errorf("Expected module name 'remote', got %s", remoteVolume.GetModuleName())
	}

	// Test config
	config := remoteVolume.GetConfig().(*RemoteVolumeConfig)
	if config.VolumeName != "props" {
		t.Errorf("Expected volume name in config to be 'props', got %s", config.VolumeName)
	}
	if config.RemoteURL != "http://localhost:3005" {
		t.Errorf("Expected remote URL to be 'http://localhost:3005', got %s", config.RemoteURL)
	}
}
