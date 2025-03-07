package server

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"os"
	"sync"
	"time"
)

type WikiVolume struct {
	name     string
	server   *Server
	ns       *grits.NameStore
	readOnly bool

	persistMtx   sync.Mutex
	emptyDirNode grits.FileNode // Keep our reference to the empty directory node
}

type WikiVolumeConfig struct {
	VolumeName string `json:"VolumeName"`
}

func NewWikiVolume(config *WikiVolumeConfig, server *Server, readOnly bool) (*WikiVolume, error) {
	ns, err := grits.EmptyNameStore(server.BlobStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	// Create empty directory node
	emptyDirMap := make(map[string]*grits.BlobAddr)
	emptyDirNode, err := ns.CreateTreeNode(emptyDirMap)
	if err != nil {
		return nil, fmt.Errorf("failed to create empty directory node: %v", err)
	}
	emptyDirNode.Take() // Take reference that we'll hold

	wv := &WikiVolume{
		name:     config.VolumeName,
		server:   server,
		ns:       ns,
		readOnly: false,

		emptyDirNode: emptyDirNode,
	}

	err = wv.load()
	if err != nil {
		emptyDirNode.Release() // Clean up if we fail
		return nil, fmt.Errorf("failed to load WikiVolume %s: %v", wv.name, err)
	}

	return wv, nil
}

func (wv *WikiVolume) Start() error {
	if grits.DebugBlobStorage {
		wv.server.AddPeriodicTask(time.Second*10, wv.ns.PrintBlobStorageDebugging)
	}

	return nil
}

func (wv *WikiVolume) Stop() error {
	// Ensure any final persistence operations are completed
	result := wv.save()

	// Clean up our empty dir reference when stopping
	if wv.emptyDirNode != nil {
		wv.emptyDirNode.Release()
		wv.emptyDirNode = nil
	}

	return result
	// FIXME - We do stop, whether or not we return error -- we should think about that
}

func (wv *WikiVolume) isReadOnly() bool {
	return wv.readOnly
}

func (wv *WikiVolume) Checkpoint() error {
	return wv.save()
}

func (wv *WikiVolume) GetModuleName() string {
	return "wiki"
}

func (wv *WikiVolume) GetVolumeName() string {
	return wv.name
}

// Get a FileNode from a metadata address, either from cache or loaded on demand.
// Takes a reference to the node before returning it.
func (wv *WikiVolume) GetFileNode(metadataAddr *grits.BlobAddr) (grits.FileNode, error) {
	return wv.ns.GetFileNode(metadataAddr)
}

func (wv *WikiVolume) Lookup(path string) (*grits.TypedFileAddr, error) {
	node, err := wv.ns.LookupNode(path) // FIXME FIXME - Need to release after this
	if err != nil {
		return nil, err
	}

	return node.Address(), nil
}

func (wv *WikiVolume) LookupFull(path string) ([][]string, error) {
	return wv.ns.LookupFull(path)
}

func (wv *WikiVolume) LookupNode(path string) (grits.FileNode, error) {
	return wv.ns.LookupNode(path)
}

func (wv *WikiVolume) Link(path string, addr *grits.TypedFileAddr) error {
	return wv.ns.Link(path, addr)
}

func (wv *WikiVolume) GetEmptyDirMetadataAddr() *grits.BlobAddr {
	return wv.emptyDirNode.MetadataBlob().GetAddress()
}

func (wv *WikiVolume) GetEmptyDirAddr() *grits.TypedFileAddr {
	return wv.emptyDirNode.Address()
}

// MultiLink also needs updating since it's part of the same transition
func (wv *WikiVolume) MultiLink(req []*grits.LinkRequest) error {
	return wv.ns.MultiLink(req)
}

func (wv *WikiVolume) ReadFile(addr *grits.TypedFileAddr) (grits.CachedFile, error) {
	return wv.ns.BlobStore.ReadFile(&addr.BlobAddr)
}

func (wv *WikiVolume) AddBlob(path string) (grits.CachedFile, error) {
	return wv.ns.BlobStore.AddLocalFile(path)
}

func (wv *WikiVolume) AddOpenBlob(file *os.File) (grits.CachedFile, error) {
	return wv.ns.BlobStore.AddOpenFile(file)
}

func (wv *WikiVolume) Cleanup() error {
	wv.ns.CleanupUnreferencedNodes()
	return nil
}

func (wv *WikiVolume) RegisterWatcher(watcher grits.FileTreeWatcher) {
	wv.ns.RegisterWatcher(watcher)
}

func (wv *WikiVolume) UnregisterWatcher(watcher grits.FileTreeWatcher) {
	wv.ns.UnregisterWatcher(watcher)
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

// save writes the volume's NameStore root to persistent storage
func (wv *WikiVolume) save() error {
	wv.persistMtx.Lock()
	defer wv.persistMtx.Unlock()

	// Prepare the data to be saved
	rootAddrStr := wv.ns.GetRoot()
	data, err := json.Marshal(rootAddrStr)
	if err != nil {
		return fmt.Errorf("failed to marshal root address: %v", err)
	}

	// Write to a temporary file first
	tempFilename := wv.server.Config.ServerPath("var/wikiroots/" + wv.name + ".json.new")
	if err := os.WriteFile(tempFilename, data, 0644); err != nil {
		return fmt.Errorf("failed to write to temp file: %v", err)
	}

	// Rename the temporary file to the final filename atomically
	finalFilename := wv.server.Config.ServerPath("var/wikiroots/" + wv.name + ".json")
	if err := os.Rename(tempFilename, finalFilename); err != nil {
		return fmt.Errorf("failed to rename temp file to final file: %v", err)
	}

	return nil
}
