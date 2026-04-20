package gritsd

import (
	"grits/internal/grits"
	"os"
	"path/filepath"
	"testing"
)

// buildTestTree creates a small directory tree on the real filesystem:
//
//	root/
//	  file1.txt       "hello"
//	  subdir/
//	    file2.txt     "world"
//
// Returns the root path and a cleanup func.
func buildTestTree(t *testing.T) (string, func()) {
	t.Helper()

	root, err := os.MkdirTemp("", "grits-import-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("MkdirAll %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}

	write("file1.txt", "hello")
	write("subdir/file2.txt", "world")

	return root, func() { os.RemoveAll(root) }
}

// refCountOf digs the RefCount out of a CachedFile, fataling if the
// underlying type isn't a *grits.LocalCachedFile.
func refCountOf(t *testing.T, cf grits.CachedFile) int {
	t.Helper()
	lcf, ok := cf.(*grits.LocalCachedFile)
	if !ok {
		t.Fatalf("expected *grits.LocalCachedFile, got %T", cf)
	}
	return lcf.RefCount
}

func TestImportLocalDir(t *testing.T) {
	server, cleanup := SetupTestServer(t)
	defer cleanup()

	localConfig := &LocalVolumeConfig{VolumeName: "testimport"}
	vol, err := NewLocalVolume(localConfig, server, false, false)
	if err != nil {
		t.Fatalf("NewLocalVolume: %v", err)
	}
	server.AddModule(vol)
	server.Start()
	defer server.Stop()

	root, fsCleanup := buildTestTree(t)
	defer fsCleanup()

	// --- Import ---
	if err := ImportLocalDir(vol, root, "imported"); err != nil {
		t.Fatalf("ImportLocalDir: %v", err)
	}

	// --- Structure checks ---

	// Root of import should be a directory.
	importedNode, err := vol.LookupNode("imported", grits.BackendPrincipal)
	if err != nil {
		t.Fatalf("LookupNode imported: %v", err)
	}
	defer importedNode.Release()

	if importedNode.Metadata().Type != grits.GNodeTypeDirectory {
		t.Errorf("imported: expected directory, got %v", importedNode.Metadata().Type)
	}

	// file1.txt should exist with correct content.
	file1Node, err := vol.LookupNode("imported/file1.txt", grits.BackendPrincipal)
	if err != nil {
		t.Fatalf("LookupNode file1.txt: %v", err)
	}
	defer file1Node.Release()

	if file1Node.Metadata().Type != grits.GNodeTypeFile {
		t.Errorf("file1.txt: expected file, got %v", file1Node.Metadata().Type)
	}

	file1Blob, err := file1Node.ExportedBlob()
	if err != nil {
		t.Fatalf("ExportedBlob file1.txt: %v", err)
	}
	file1Data, err := file1Blob.Read(0, file1Blob.GetSize())
	if err != nil {
		t.Fatalf("Read file1.txt: %v", err)
	}
	if string(file1Data) != "hello" {
		t.Errorf("file1.txt: expected %q, got %q", "hello", string(file1Data))
	}

	// subdir should be a directory.
	subdirNode, err := vol.LookupNode("imported/subdir", grits.BackendPrincipal)
	if err != nil {
		t.Fatalf("LookupNode subdir: %v", err)
	}
	defer subdirNode.Release()

	if subdirNode.Metadata().Type != grits.GNodeTypeDirectory {
		t.Errorf("subdir: expected directory, got %v", subdirNode.Metadata().Type)
	}

	// file2.txt should exist with correct content.
	file2Node, err := vol.LookupNode("imported/subdir/file2.txt", grits.BackendPrincipal)
	if err != nil {
		t.Fatalf("LookupNode file2.txt: %v", err)
	}
	defer file2Node.Release()

	file2Blob, err := file2Node.ExportedBlob()
	if err != nil {
		t.Fatalf("ExportedBlob file2.txt: %v", err)
	}
	file2Data, err := file2Blob.Read(0, file2Blob.GetSize())
	if err != nil {
		t.Fatalf("Read file2.txt: %v", err)
	}
	if string(file2Data) != "world" {
		t.Errorf("file2.txt: expected %q, got %q", "world", string(file2Data))
	}

	// --- Ref count checks ---
	// After ImportLocalDir returns and LinkByMetadata has completed, the ref
	// manager owns the tree. Every blob should have ref count exactly 1.
	// We check metadata and content blobs for each node we can reach.

	checkNodeRefs := func(name string, node grits.FileNode) {
		t.Helper()

		metaCF := node.MetadataBlob()
		if rc := refCountOf(t, metaCF); rc != 1 {
			t.Errorf("%s metadata blob: expected ref count 1, got %d", name, rc)
		}

		contentCF, err := node.ExportedBlob()
		if err != nil {
			t.Fatalf("%s ExportedBlob: %v", name, err)
		}
		if rc := refCountOf(t, contentCF); rc != 1 {
			t.Errorf("%s content blob: expected ref count 1, got %d", name, rc)
		}
	}

	checkNodeRefs("imported", importedNode)
	checkNodeRefs("imported/subdir", subdirNode)
	checkNodeRefs("imported/file1.txt", file1Node)
	checkNodeRefs("imported/file2.txt", file2Node)
}

// TestImportLocalDirStability verifies that importing the same directory twice
// produces identical metadata addresses — no churn for unchanged content.
func TestImportLocalDirStability(t *testing.T) {
	server, cleanup := SetupTestServer(t)
	defer cleanup()

	localConfig := &LocalVolumeConfig{VolumeName: "testimportstable"}
	vol, err := NewLocalVolume(localConfig, server, false, false)
	if err != nil {
		t.Fatalf("NewLocalVolume: %v", err)
	}
	server.AddModule(vol)
	server.Start()
	defer server.Stop()

	root, fsCleanup := buildTestTree(t)
	defer fsCleanup()

	if err := ImportLocalDir(vol, root, "first"); err != nil {
		t.Fatalf("first ImportLocalDir: %v", err)
	}
	if err := ImportLocalDir(vol, root, "second"); err != nil {
		t.Fatalf("second ImportLocalDir: %v", err)
	}

	check := func(rel string) {
		t.Helper()
		n1, err := vol.LookupNode("first/"+rel, grits.BackendPrincipal)
		if err != nil {
			t.Fatalf("LookupNode first/%s: %v", rel, err)
		}
		defer n1.Release()

		n2, err := vol.LookupNode("second/"+rel, grits.BackendPrincipal)
		if err != nil {
			t.Fatalf("LookupNode second/%s: %v", rel, err)
		}
		defer n2.Release()

		addr1 := n1.MetadataBlob().GetAddress()
		addr2 := n2.MetadataBlob().GetAddress()
		if addr1 != addr2 {
			t.Errorf("%s: addresses differ between imports: %s vs %s", rel, addr1, addr2)
		}
	}

	check("")
	check("file1.txt")
	check("subdir")
	check("subdir/file2.txt")
}
