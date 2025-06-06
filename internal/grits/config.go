package grits

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config represents the core server configuration.
type Config struct {
	// File locations
	ServerDir string `json:"-"`

	// Storage configuration
	StorageSize         int64 `json:"storageSize"`
	StorageFreeSize     int64 `json:"storageFreeSize"`
	NamespaceSavePeriod int   `json:"namespaceSavePeriod"`
	HardLinkBlobs       bool  `json:"hardLinkBlobs"`
	ValidateBlobs       bool  `json:"validateBlobs"`
	DelayedEviction     bool  `json:"delayedEviction"`

	// Modules and configs for same
	Modules []json.RawMessage `json:"modules"`
}

// NewConfig creates a new configuration instance with default values.
func NewConfig(serverDir string) *Config {
	return &Config{
		ServerDir: serverDir,

		StorageSize:         100 * 1024 * 1024, // Max size of data
		StorageFreeSize:     80 * 1024 * 1024,  // Size to clean down to when overfull
		NamespaceSavePeriod: 30,                // # of seconds between namespace checkpoints
		HardLinkBlobs:       false,
		ValidateBlobs:       false,
		DelayedEviction:     true,
	}
}

func (c *Config) ServerPath(path string) string {
	return filepath.Join(c.ServerDir, path)
}

func (c *Config) LoadFromFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Use json.Decoder to decode directly into the struct
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(c); err != nil {
		return err
	}

	return nil
}

// SaveToFile writes the config to the specified file in JSON format
func (c *Config) SaveToFile(filename string) error {
	// Create or truncate the file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create an encoder with pretty-printing for readability
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	// Write the JSON to the file
	if err := encoder.Encode(c); err != nil {
		return err
	}

	return nil
}
