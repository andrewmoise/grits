package grits

import (
	"os"
	"testing"
)

func TestConfig_LoadFromFile(t *testing.T) {
	// Create a temporary JSON config file for testing.
	testFile, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(testFile.Name()) // Clean up the file after

	// JSON content for the test
	jsonContent := `
{
	"ThisHost": "192.168.1.1",
	"ThisPort": 9000
}
	`

	if _, err := testFile.Write([]byte(jsonContent)); err != nil {
		testFile.Close() // Make sure to close the file handle on error
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	testFile.Close() // Close the file

	// Create a new Config and load from the test file.
	config := NewConfig()
	if err := config.LoadFromFile(testFile.Name()); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Assertions to ensure the config loaded the right values.
	if config.ThisHost != "192.168.1.1" {
		t.Errorf("Expected ThisHost to be '192.168.1.1', got '%s'", config.ThisHost)
	}
	if config.ThisPort != 9000 {
		t.Errorf("Expected ThisPort to be 9000, got %d", config.ThisPort)
	}
}
