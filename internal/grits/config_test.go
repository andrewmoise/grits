package grits

import (
	"os"
	"testing"
)

// Assuming you're testing core configuration fields now, adjust the test accordingly.
func TestCoreConfig_LoadFromFile(t *testing.T) {
	// Create a temporary JSON config file for testing core configurations.
	testFile, err := os.CreateTemp("", "core-config-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(testFile.Name()) // Clean up the file afterward

	// JSON content for the core configuration test
	jsonContent := `
{
    "StorageSize": 154857600,
    "StorageFreeSize": 53886080,
    "NamespaceSavePeriod": 30,
    "HardLinkBlobs": false,
    "ValidateBlobs": true
}
    `

	if _, err := testFile.Write([]byte(jsonContent)); err != nil {
		testFile.Close() // Make sure to close the file handle on error
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	testFile.Close() // Close the file

	// Load core configuration from the test file.
	coreConfig := NewConfig(".")
	err = coreConfig.LoadFromFile(testFile.Name())
	if err != nil {
		t.Fatalf("Failed to load core config: %v", err)
	}

	// Assertions to ensure the core configuration loaded the right values.
	if coreConfig.StorageSize != 154857600 {
		t.Errorf("Expected StorageSize to be 154857600, got %d", coreConfig.StorageSize)
	}
	if coreConfig.StorageFreeSize != 53886080 {
		t.Errorf("Expected StorageFreeSize to be 53886080, got %d", coreConfig.StorageFreeSize)
	}
	if coreConfig.NamespaceSavePeriod != 30 {
		t.Errorf("Expected NamespaceSavePeriod to be 30, got %d", coreConfig.NamespaceSavePeriod)
	}
	if coreConfig.HardLinkBlobs != false {
		t.Errorf("Expected HardLinkBlobs to be false, got %t", coreConfig.HardLinkBlobs)
	}
	if coreConfig.ValidateBlobs != true {
		t.Errorf("Expected ValidateBlobs to be true, got %t", coreConfig.ValidateBlobs)
	}
}
