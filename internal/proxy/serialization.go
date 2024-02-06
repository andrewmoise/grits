package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"

	"grits/internal/grits"
)

// SerializeFileNode serializes a FileNode and stores it in the BlobStore.
func (bs *BlobStore) SerializeFileNode(fn *FileNode) error {
	if fn.ExportedBlob != nil {
		panic("Trying to serialize a finalized FileNode")
	}

	data, err := json.Marshal(fn)
	if err != nil {
		return err
	}

	// Create a new temporary file in the BlobStore directory for storing the serialized data.
	tempFilePath := bs.generateTempFilePath("filenode")
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name()) // Ensure the temporary file is removed after use.
	defer tempFile.Close()

	// Write the serialized data to the temporary file.
	if _, err := tempFile.Write(data); err != nil {
		return err
	}

	// Add the file to the BlobStore, which moves it from the temporary location.
	cachedFile, err := bs.AddLocalFile(tempFile.Name())
	if err != nil {
		return err
	}

	fn.ExportedBlob = cachedFile
	return nil
}

// SerializeRevNode serializes a RevNode and stores it in the BlobStore.
func (bs *BlobStore) SerializeRevNode(rn *RevNode) error {
	if rn.ExportedBlob != nil {
		panic("Trying to serialize a finalized RevNode")
	}

	data, err := json.Marshal(rn)
	if err != nil {
		return err
	}

	tempFilePath := bs.generateTempFilePath("revnode")
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.Write(data); err != nil {
		return err
	}

	cachedFile, err := bs.AddLocalFile(tempFile.Name())
	if err != nil {
		return err
	}

	rn.ExportedBlob = cachedFile
	return nil
}

// DeserializeFileNode retrieves a FileNode from the BlobStore.
func (bs *BlobStore) DeserializeFileNode(addr *grits.FileAddr) (*FileNode, error) {
	cachedFile, err := bs.ReadFile(addr)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(cachedFile.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var fn FileNode
	if err := json.NewDecoder(file).Decode(&fn); err != nil {
		return nil, err
	}

	return &fn, nil
}

// generateTempFilePath generates a path for a temporary file within the BlobStore directory.
func (bs *BlobStore) generateTempFilePath(prefix string) string {
	// Generate a unique temporary file path. This example does not ensure uniqueness.
	// You might want to use a more robust method to generate a unique filename.
	return filepath.Join(bs.storePath, prefix+"-temp.json")
}
