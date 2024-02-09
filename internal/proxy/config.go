package proxy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
)

type Config struct {
	// General networking configuration
	ThisHost   string `json:"ThisHost"`
	ThisPort   int    `json:"ThisPort"`
	IsRootNode bool   `json:"IsRootNode"`
	RootHost   string `json:"RootHost"`
	RootPort   int    `json:"RootPort"`

	// File locations
	ServerDir string `json:"ServerDir"`

	// Storage configuration
	StorageSize     uint64 `json:"StorageSize"`
	StorageFreeSize uint64 `json:"StorageFreeSize"`

	// Directories to cache
	DirMirrors []DirMirrorConfig `json:"DirMirrors"`

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

type DirMirrorConfig struct {
	SourceDir     string `json:"SourceDir"`
	CacheLinksDir string `json:"CacheLinksDir"`
}

// NewConfig creates a new configuration instance with default values.
func NewConfig() *Config {
	return &Config{
		ThisHost:                 "127.0.0.1",
		ThisPort:                 1787,
		IsRootNode:               false,
		RootHost:                 "",
		RootPort:                 0,
		ServerDir:                ".", // Default server directory is the current directory
		StorageSize:              20 * 1024 * 1024,
		StorageFreeSize:          18 * 1024 * 1024,
		DirMirrors:               []DirMirrorConfig{},
		DhtNotifyNumber:          5,
		DhtNotifyPeriod:          20,
		DhtMaxResponseNodes:      10,
		DhtRefreshTime:           8 * 60 * 60,
		DhtExpiryTime:            24 * 60 * 60,
		MaxProxyMapAge:           24 * 60 * 60,
		ProxyMapCleanupPeriod:    60 * 60,
		ProxyHeartbeatPeriod:     10,
		RootUpdatePeerListPeriod: 8,
		RootProxyDropTimeout:     180,
	}
}

func (c *Config) VarPath(path string) string {
	return filepath.Join(c.ServerDir, "var", path)
}

// LoadFromFile updates the configuration values based on a provided JSON configuration file.
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

	return nil
}
