package gritsd

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"os"
	"sync"
	"time"
)

type Volume interface {
	GetVolumeName() string
	Start() error
	Stop() error
	isReadOnly() bool
	Checkpoint() error

	LookupNode(path string) (grits.FileNode, error)
	LookupFull(name []string) ([]*grits.PathNodePair, bool, error)
	GetFileNode(metadataAddr grits.BlobAddr) (grits.FileNode, error)

	CreateTreeNode() (*grits.TreeNode, error)
	CreateBlobNode(contentAddr grits.BlobAddr, size int64) (*grits.BlobNode, error)

	LinkByMetadata(path string, metadataAddr grits.BlobAddr) error
	MultiLink([]*grits.LinkRequest, bool) ([]*grits.PathNodePair, error)

	AddBlob(path string) (grits.CachedFile, error)
	AddOpenBlob(*os.File) (grits.CachedFile, error)
	AddMetadataBlob(*grits.GNodeMetadata) (grits.CachedFile, error)

	GetBlob(addr grits.BlobAddr) (grits.CachedFile, error)
	PutBlob(file *os.File) (grits.BlobAddr, error)

	Cleanup() error

	RegisterWatcher(watcher grits.FileTreeWatcher)
	UnregisterWatcher(watcher grits.FileTreeWatcher)
}

type LocalVolume struct {
	name     string
	server   *Server
	ns       *grits.NameStore
	readOnly bool

	persistMtx   sync.Mutex
	emptyDirNode grits.FileNode // Keep our reference to the empty directory node
}

type LocalVolumeConfig struct {
	VolumeName string `json:"volumeName"`
}

func NewLocalVolume(config *LocalVolumeConfig, server *Server, readOnly bool) (*LocalVolume, error) {
	ns, err := grits.EmptyNameStore(server.BlobStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	// Create empty directory node
	emptyDirMap := make(map[string]grits.BlobAddr)
	emptyDirNode, err := ns.CreateTreeNode(emptyDirMap)
	if err != nil {
		return nil, fmt.Errorf("failed to create empty directory node: %v", err)
	}
	emptyDirNode.Take() // Take reference that we'll hold

	wv := &LocalVolume{
		name:     config.VolumeName,
		server:   server,
		ns:       ns,
		readOnly: false,

		emptyDirNode: emptyDirNode,
	}

	err = wv.load()
	if err != nil {
		emptyDirNode.Release() // Clean up if we fail
		return nil, fmt.Errorf("failed to load LocalVolume %s: %v", wv.name, err)
	}

	return wv, nil
}

func (wv *LocalVolume) Start() error {
	if grits.DebugBlobStorage {
		wv.server.AddPeriodicTask(time.Second*10, wv.ns.PrintBlobStorageDebugging)
	}

	return nil
}

func (wv *LocalVolume) Stop() error {
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

func (wv *LocalVolume) isReadOnly() bool {
	return wv.readOnly
}

func (wv *LocalVolume) Checkpoint() error {
	return wv.save()
}

func (wv *LocalVolume) GetModuleName() string {
	return "volume"
}

func (*LocalVolume) GetDependencies() []*Dependency {
	return []*Dependency{}
}

func (wv *LocalVolume) GetConfig() any {
	return &LocalVolumeConfig{
		VolumeName: wv.name,
	}
}

func (wv *LocalVolume) GetVolumeName() string {
	return wv.name
}

// Get a FileNode from a metadata address, either from cache or loaded on demand.
// Takes a reference to the node before returning it.
func (wv *LocalVolume) GetFileNode(metadataAddr grits.BlobAddr) (grits.FileNode, error) {
	return wv.ns.GetFileNode(metadataAddr)
}

// CreateTreeNode creates a new empty directory node
// The returned node has an additional reference taken which the caller must release when done
func (v *LocalVolume) CreateTreeNode() (*grits.TreeNode, error) {
	emptyChildren := make(map[string]grits.BlobAddr)
	treeNode, err := v.ns.CreateTreeNode(emptyChildren)
	if err != nil {
		return nil, fmt.Errorf("failed to create empty tree node: %v", err)
	}

	// Take an extra reference for the caller
	treeNode.Take()
	return treeNode, nil
}

// CreateBlobNode creates a metadata node that points to the given content blob
// The returned node has an additional reference taken which the caller must release when done
func (v *LocalVolume) CreateBlobNode(contentAddr grits.BlobAddr, size int64) (*grits.BlobNode, error) {
	// Create the metadata for this blob
	_, metadataBlob, err := v.ns.CreateMetadataBlob(contentAddr, size, false, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata for blob: %v", err)
	}
	defer metadataBlob.Release()

	// Now get a FileNode for this metadata blob
	node, err := v.ns.GetFileNode(metadataBlob.GetAddress())
	if err != nil {
		return nil, fmt.Errorf("failed to create blob node: %v", err)
	}
	defer node.Release()

	// Convert to BlobNode (this should always succeed since we created it as such)
	blobNode, ok := node.(*grits.BlobNode)
	if !ok {
		return nil, fmt.Errorf("created node is not a BlobNode")
	}

	// Take an extra reference for the caller
	blobNode.Take()
	return blobNode, nil
}

// GetBlob retrieves a blob from the blobstore by its address
func (v *LocalVolume) GetBlob(addr grits.BlobAddr) (grits.CachedFile, error) {
	return v.ns.BlobStore.ReadFile(addr)
}

// PutBlob adds a file to the blob store and returns its address
func (v *LocalVolume) PutBlob(file *os.File) (grits.BlobAddr, error) {
	cachedFile, err := v.ns.BlobStore.AddReader(file)
	if err != nil {
		return "", err
	}
	defer cachedFile.Release() // We don't need to maintain this reference

	// Return a copy of the address
	return cachedFile.GetAddress(), nil
}

func (wv *LocalVolume) LookupFull(paths []string) ([]*grits.PathNodePair, bool, error) {
	return wv.ns.LookupFull(paths)
}

func (wv *LocalVolume) LookupNode(path string) (grits.FileNode, error) {
	return wv.ns.LookupNode(path)
}

func (wv *LocalVolume) LinkByMetadata(name string, metadataAddr grits.BlobAddr) error {
	return wv.ns.LinkByMetadata(name, metadataAddr)
}

// MultiLink also needs updating since it's part of the same transition
func (wv *LocalVolume) MultiLink(req []*grits.LinkRequest, returnResults bool) ([]*grits.PathNodePair, error) {
	return wv.ns.MultiLink(req, returnResults)
}

func (wv *LocalVolume) AddBlob(path string) (grits.CachedFile, error) {
	return wv.ns.BlobStore.AddLocalFile(path)
}

func (wv *LocalVolume) AddOpenBlob(file *os.File) (grits.CachedFile, error) {
	return wv.ns.BlobStore.AddReader(file)
}

// FIXME - This should go away, CreateBlobNode() instead

func (wv *LocalVolume) AddMetadataBlob(metadata *grits.GNodeMetadata) (grits.CachedFile, error) {
	// Serialize and store the metadata
	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("error marshalling metadata: %v", err)
	}

	metadataCf, err := wv.ns.BlobStore.AddDataBlock(metadataData)
	if err != nil {
		return nil, fmt.Errorf("error writing metadata: %v", err)
	}
	return metadataCf, nil
}

func (wv *LocalVolume) Cleanup() error {
	wv.ns.CleanupUnreferencedNodes()
	return nil
}

func (wv *LocalVolume) RegisterWatcher(watcher grits.FileTreeWatcher) {
	wv.ns.RegisterWatcher(watcher)
}

func (wv *LocalVolume) UnregisterWatcher(watcher grits.FileTreeWatcher) {
	wv.ns.UnregisterWatcher(watcher)
}

// RootState represents the serialized state of a NameStore root
type RootState struct {
	RootAddr     string `json:"rootAddr"`
	SerialNumber int64  `json:"serialNumber"`
	LastModified string `json:"lastModified,omitempty"`
}

// load retrieves the volume's NameStore root from persistent storage.
func (wv *LocalVolume) load() error {
	err := os.MkdirAll(wv.server.Config.ServerPath("var/localroots"), 0755)
	if err != nil {
		return fmt.Errorf("can't make localroot directory: %v", err)
	}

	wv.persistMtx.Lock()
	defer wv.persistMtx.Unlock()

	filename := wv.server.Config.ServerPath("var/localroots/" + wv.name + ".json")
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

	var state RootState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to unmarshal volume state: %v", err)
	}

	rootAddr, err := grits.NewTypedFileAddrFromString(state.RootAddr)
	if err != nil {
		return fmt.Errorf("failed to parse root address: %v", err)
	}

	ns, err := grits.DeserializeNameStore(wv.server.BlobStore, rootAddr, state.SerialNumber)
	if err != nil {
		return fmt.Errorf("failed to deserialize name store: %v", err)
	}

	wv.ns = ns
	return nil
}

// save writes the volume's NameStore root to persistent storage
func (wv *LocalVolume) save() error {
	wv.persistMtx.Lock()
	defer wv.persistMtx.Unlock()

	// Create the state to be serialized
	state := RootState{
		RootAddr:     string(wv.ns.GetRoot()),
		SerialNumber: wv.ns.GetSerialNumber(),
		LastModified: time.Now().UTC().Format(time.RFC3339),
	}

	// Marshal to JSON
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal root address: %v", err)
	}

	// Write to a temporary file first
	tempFilename := wv.server.Config.ServerPath("var/localroots/" + wv.name + ".json.new")
	if err := os.WriteFile(tempFilename, data, 0644); err != nil {
		return fmt.Errorf("failed to write to temp file: %v", err)
	}

	// Rename the temporary file to the final filename atomically
	finalFilename := wv.server.Config.ServerPath("var/localroots/" + wv.name + ".json")
	if err := os.Rename(tempFilename, finalFilename); err != nil {
		return fmt.Errorf("failed to rename temp file to final file: %v", err)
	}

	return nil
}
