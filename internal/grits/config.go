package grits

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// CoreConfig represents the core server configuration.
type Config struct {
	// General networking configuration
	IsRootNode  bool   `json:"IsRootNode"`
	RootHost    string `json:"RootHost"`
	RootPort    int    `json:"RootPort"`
	ServerToken string `json:"ServerToken"`

	// File locations
	ServerDir      string `json:"-"`
	DirWatcherPath string `json:"DirWatcherPath"`

	// Storage configuration
	StorageSize         uint64 `json:"StorageSize"`
	StorageFreeSize     uint64 `json:"StorageFreeSize"`
	NamespaceSavePeriod int    `json:"NamespaceSavePeriod"`
	HardLinkBlobs       bool   `json:"HardLinkBlobs"`
	ValidateBlobs       bool   `json:"ValidateBlobs"`

	// Modules and configs for same
	Modules []json.RawMessage `json:"Modules"`
}

// NewConfig creates a new configuration instance with default values.
func NewConfig(serverDir string) *Config {
	return &Config{
		IsRootNode: false,
		RootHost:   "",
		RootPort:   0,

		ServerToken: "",

		ServerDir:      serverDir,
		DirWatcherPath: "/usr/local/bin/ogwatch",

		StorageSize:         100 * 1024 * 1024, // Max size of data
		StorageFreeSize:     80 * 1024 * 1024,  // Size to clean down to when overfull
		NamespaceSavePeriod: 30,                // # of seconds between namespace checkpoints
		HardLinkBlobs:       false,
		ValidateBlobs:       false,
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
