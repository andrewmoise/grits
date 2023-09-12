package proxy

import (
	"io/ioutil"
	"os"
	"testing"
)

func TestConfig_LoadFromFile(t *testing.T) {
	// Create a temporary JSON config file for testing.
	testFile, err := ioutil.TempFile("", "config-*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(testFile.Name())

	// JSON content for the test
	jsonContent := `
{
	"ThisHost": "192.168.1.1",
	"ThisPort": 9000
}
	`
	
	if _, err := testFile.Write([]byte(jsonContent)); err != nil {
		testFile.Close()
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	testFile.Close()

	// Create a new Config and load from the test file.
	config := NewConfig("localhost", 8080)
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
