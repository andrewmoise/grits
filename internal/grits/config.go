package grits

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
)

type Config struct {
	// General networking configuration
	ThisHost    string `json:"ThisHost"`
	ThisPort    int    `json:"ThisPort"`
	IsRootNode  bool   `json:"IsRootNode"`
	RootHost    string `json:"RootHost"`
	RootPort    int    `json:"RootPort"`
	ServerToken string `json:"ServerToken"`

	// File locations
	ServerDir string `json:"-"`

	// Storage configuration
	StorageSize         uint64 `json:"StorageSize"`
	StorageFreeSize     uint64 `json:"StorageFreeSize"`
	NamespaceSavePeriod int    `json:"NamespaceSavePeriod"`

	// Directories to cache
	Volumes []VolumeConfig `json:"Volumes"`

	// Nitty-gritty DHT tuning
	DhtNotifyNumber     int `json:"DhtNotifyNumber"`
	DhtNotifyPeriod     int `json:"DhtNotifyPeriod"`
	DhtMaxResponseNodes int `json:"DhtMaxResponseNodes"`
	DhtRefreshTime      int `json:"DhtRefreshTime"`
	DhtExpiryTime       int `json:"DhtExpiryTime"`

	MaxProxyMapAge           int `json:"MaxProxyMapAge"`
	ProxyMapCleanupPeriod    int `json:"ProxyMapCleanupPeriod"`
	ProxyHeartbeatPeriod     int `json:"ProxyHeartbeatPeriod"`
	RootUpdatePeerListPeriod int `json:"RootUpdatePeerListPeriod"`
	RootProxyDropTimeout     int `json:"RootProxyDropTimeout"`
}

type VolumeConfig struct {
	Type          string `json:"Type"`
	SourceDir     string `json:"SourceDir"`
	CacheLinksDir string `json:"CacheLinksDir,omitempty"`
	DestPath      string `json:"DestPath,omitempty"`
}

// NewConfig creates a new configuration instance with default values.
func NewConfig() *Config {
	return &Config{
		ThisHost:   "127.0.0.1",
		ThisPort:   1787,
		IsRootNode: false,
		RootHost:   "",
		RootPort:   0,

		ServerToken: "",
		ServerDir:   ".",

		StorageSize:         100 * 1024 * 1024, // Max size of data
		StorageFreeSize:     80 * 1024 * 1024,  // Size to clean down to when overfull
		NamespaceSavePeriod: 30,                // # of seconds between namespace checkpoints

		Volumes: []VolumeConfig{}, // Dirs to put in the blob cache

		DhtNotifyNumber:          5,  // # of peers to notify in the DHT
		DhtNotifyPeriod:          20, // # of seconds between DHT notifications
		DhtMaxResponseNodes:      10, // Max # of nodes to return in a DHT response
		DhtRefreshTime:           8 * 60 * 60,
		DhtExpiryTime:            24 * 60 * 60,
		MaxProxyMapAge:           24 * 60 * 60,
		ProxyMapCleanupPeriod:    60 * 60,
		ProxyHeartbeatPeriod:     5 * 60, // # of seconds between proxy heartbeats
		RootUpdatePeerListPeriod: 6 * 60, // ?
		RootProxyDropTimeout:     6 * 60, // # of seconds before root drops a proxy
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

	// Decode into a map so we can iterate over keys
	data := make(map[string]interface{})
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return err
	}

	valConfig := reflect.ValueOf(c).Elem()
	typeConfig := valConfig.Type()

	for key, value := range data {
		if key == "Volumes" {
			// Handle Volumes separately
			continue
		}

		fieldValue := valConfig.FieldByName(key)
		if !fieldValue.IsValid() {
			return errors.New("invalid field name: " + key)
		}
		if !fieldValue.CanSet() {
			return errors.New("cannot set field: " + key)
		}

		field, ok := typeConfig.FieldByName(key)
		if !ok {
			return errors.New("field not found: " + key)
		}
		requiredType := field.Type

		val := reflect.ValueOf(value)
		if val.Type().ConvertibleTo(requiredType) {
			valConverted := val.Convert(requiredType)
			fieldValue.Set(valConverted)
		} else {
			return errors.New("type mismatch for field: " + key)
		}
	}

	// Manually decode Volumes if present
	if volumesData, ok := data["Volumes"]; ok {
		volumesJson, err := json.Marshal(volumesData)
		if err != nil {
			return err
		}
		var volumes []VolumeConfig
		if err := json.Unmarshal(volumesJson, &volumes); err != nil {
			return err
		}
		c.Volumes = volumes
	}

	return nil
}
