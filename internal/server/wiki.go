package server

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"os"
	"sync"
)

// WikiVolume represents a writable volume for storing and retrieving content.
type WikiVolume struct {
	name       string
	server     *Server
	ns         *grits.NameStore
	persistMtx sync.Mutex
}

type WikiVolumeConfig struct {
	VolumeName string `json:"VolumeName"`
}

// NewWikiVolume creates a new WikiVolume with the given name.
func NewWikiVolume(config *WikiVolumeConfig, server *Server) (*WikiVolume, error) {
	ns, err := grits.EmptyNameStore(server.BlobStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	// Initialize WikiVolume with name from config
	wv := &WikiVolume{
		name:   config.VolumeName,
		server: server,
		ns:     ns,
	}

	err = wv.load()
	if err != nil {
		return nil, fmt.Errorf("failed to load WikiVolume %s: %v", wv.name, err)
	}

	return wv, nil
}

func (wv *WikiVolume) Start() error {
	// Potentially load the volume or other start tasks
	return nil
}

func (wv *WikiVolume) Stop() error {
	// Ensure any final persistence operations are completed
	return wv.save()
}

func (wv *WikiVolume) isReadOnly() bool {
	return false
}

func (wv *WikiVolume) GetModuleName() string {
	return "wiki"
}

func (wv *WikiVolume) GetVolumeName() string {
	return wv.name
}

func (wv *WikiVolume) GetNameStore() *grits.NameStore {
	return wv.ns
}

// load retrieves the volume's NameStore root from persistent storage.
func (wv *WikiVolume) load() error {
	err := os.MkdirAll(wv.server.Config.ServerPath("var/wikiroots"), 0755)
	if err != nil {
		return fmt.Errorf("can't make wikiroot directory: %v", err)
	}

	wv.persistMtx.Lock()
	defer wv.persistMtx.Unlock()

	filename := wv.server.Config.ServerPath("var/wikiroots/" + wv.name + ".json")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// It's fine if the file doesn't exist yet
			ns, err := grits.EmptyNameStore(wv.server.BlobStore)
			if err != nil {
				return fmt.Errorf("cannot init empty name store: %v", err)
			}

			wv.ns = ns
			return nil
		}
		return err
	}

	var rootAddrStr string
	if err := json.Unmarshal(data, &rootAddrStr); err != nil {
		return fmt.Errorf("failed to unmarshal root address: %v", err)
	}

	rootAddr, err := grits.NewTypedFileAddrFromString(rootAddrStr)
	if err != nil {
		return fmt.Errorf("failed to parse root address: %v", err)
	}

	ns, err := grits.DeserializeNameStore(wv.server.BlobStore, rootAddr)
	if err != nil {
		return fmt.Errorf("failed to deserialize name store: %v", err)
	}

	wv.ns = ns
	return nil
}

// save writes the volume's NameStore root to persistent storage.
func (wv *WikiVolume) save() error {
	wv.persistMtx.Lock()
	defer wv.persistMtx.Unlock()

	filename := wv.server.Config.ServerPath("var/wikiroots/" + wv.name + ".json")
	rootAddrStr := wv.ns.GetRoot()

	data, err := json.Marshal(rootAddrStr)
	if err != nil {
		return fmt.Errorf("failed to marshal root address: %v", err)
	}

	return os.WriteFile(filename, data, 0644)
}
