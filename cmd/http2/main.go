package main

import (
	"fmt"
	"grits/internal/proxy"
	"grits/internal/server"
	"os"
	"path/filepath"
)

func main() {
	config := proxy.NewConfig("default_root_host", 1234)
	config.StorageDirectory = filepath.Join(os.TempDir(), "blobstore_test")
	config.StorageSize = 100 * 1024 * 1024
	config.StorageFreeSize = 80 * 1024 * 1024
	config.NamespaceStoreFile = "store/namespace_store.json"

	err := os.MkdirAll(config.StorageDirectory, 0755)
	if err != nil {
		panic("Failed to create storage directory")
	}

	server, err := server.NewServer(config)
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize server: %v", err))
	}

	server.Run()
}
