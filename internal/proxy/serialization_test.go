package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileNodeSerializationDeserialization(t *testing.T) {
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create FileNode
	originalNode := NewFileNode()

	// Populate it with some children
	for i := 0; i < 10; i++ {
		fileName := fmt.Sprintf("%d.txt", i)
		filePath := filepath.Join(bs.config.StorageDirectory, fileName)
		fileContent := []byte(fmt.Sprintf("file content %d", i))

		if err := os.WriteFile(filePath, fileContent, 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", fileName, err)
		}

		cachedFile, err := bs.AddLocalFile(filePath)
		if err != nil {
			t.Fatalf("AddLocalFile failed for file %s: %v", fileName, err)
		}
		originalNode.AddFile(fileName, cachedFile)
	}

	// Serialize the FileNode to JSON
	data, err := json.Marshal(originalNode)
	if err != nil {
		t.Fatalf("Failed to serialize FileNode: %v", err)
	}

	// Deserialize the JSON back into a FileNode
	var deserializedNode FileNode
	if err := json.Unmarshal(data, &deserializedNode); err != nil {
		t.Fatalf("Failed to deserialize FileNode: %v", err)
	}

	// Verify that the deserialized FileNode matches the original one
	// Note: Since ExportedBlob is ignored during serialization, it won't be compared.
	if !reflect.DeepEqual(originalNode.Children, deserializedNode.Children) {
		t.Errorf("Deserialized FileNode does not match the original. Want: %+v, got: %+v", originalNode.Children, deserializedNode.Children)
	}
}
