package gritsd

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"log"
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
	LookupFull(name []string) (*grits.LookupResponse, error)
	GetFileNode(metadataAddr grits.BlobAddr) (grits.FileNode, error)

	CreateTreeNode() (grits.FileNode, error)
	CreateBlobNode(contentAddr grits.BlobAddr, size int64) (grits.FileNode, error)

	LinkByMetadata(path string, metadataAddr grits.BlobAddr) error
	MultiLink([]*grits.LinkRequest, bool) (*grits.LookupResponse, error)

	AddBlob(path string) (grits.CachedFile, error)
	AddOpenBlob(*os.File) (grits.CachedFile, error)
	AddMetadataBlob(*grits.GNodeMetadata) (grits.CachedFile, error)

	GetBlob(addr grits.BlobAddr) (grits.CachedFile, error)

	Cleanup() error

	RegisterWatcher(watcher grits.FileTreeWatcher)
	UnregisterWatcher(watcher grits.FileTreeWatcher)
}

type LocalVolume struct {
	name         string
	server       *Server
	ns           *grits.NameStore
	readOnly     bool
	volumeConfig *LocalVolumeConfig

	persistMtx sync.Mutex
	doPersist  bool
}

var _ = (Volume)((*LocalVolume)(nil))

type LocalVolumeConfig struct {
	VolumeName          string        `json:"volumeName"`
	ClientCacheDuration time.Duration `json:"clientCacheDuration,omitempty"`
}

func NewLocalVolumeConfig(name string) *LocalVolumeConfig {
	return &LocalVolumeConfig{
		VolumeName:          name,
		ClientCacheDuration: 30 * time.Second,
	}
}

func NewLocalVolume(config *LocalVolumeConfig, server *Server, readOnly bool, sparse bool, persist bool) (*LocalVolume, error) {
	// Apply defaults for any zero values
	if config.ClientCacheDuration == 0 {
		config.ClientCacheDuration = 30 * time.Second
	}

	ns, err := grits.EmptyNameStore(server.BlobStore, sparse)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	wv := &LocalVolume{
		name:         config.VolumeName,
		server:       server,
		ns:           ns,
		readOnly:     false,
		volumeConfig: config,
		doPersist:    persist,
	}

	if persist {
		err = wv.load()
		if err != nil {
			return nil, fmt.Errorf("failed to load LocalVolume %s: %v", wv.name, err)
		}
	}

	if err := wv.publishVolumeConfig(); err != nil {
		return nil, fmt.Errorf("failed to publish volume config: %v", err)
	}

	return wv, nil
}

func (c *LocalVolumeConfig) MarshalJSON() ([]byte, error) {
	return grits.MarshalDurationFields(c)
}

func (c *LocalVolumeConfig) UnmarshalJSON(data []byte) error {
	return grits.UnmarshalDurationFields(data, c)
}

func (wv *LocalVolume) publishVolumeConfig() error {
	// Create .grits/ if it doesn't exist — ignore assertion failure meaning it already exists
	gritsDir, err := wv.ns.CreateTreeNode(make(map[string]grits.BlobAddr))
	if err != nil {
		return fmt.Errorf("failed to create .grits dir node: %v", err)
	}

	req := &grits.LinkRequest{
		Path:     ".grits",
		NewAddr:  gritsDir.MetadataBlob().GetAddress(),
		PrevAddr: grits.NilAddr,
		Assert:   grits.AssertPrevValueMatches,
	}
	_, err = wv.ns.MultiLink([]*grits.LinkRequest{req}, false)
	if err != nil && !grits.IsAssertionFailed(err) {
		return fmt.Errorf("failed to ensure .grits dir: %v", err)
	}

	// Marshal config as JSON
	data, err := json.MarshalIndent(wv.volumeConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal volume config: %v", err)
	}

	cf, err := wv.ns.BlobStore.AddDataBlock(data)
	if err != nil {
		return fmt.Errorf("failed to store volume config blob: %v", err)
	}
	defer cf.Release()

	_, metadataBlob, err := wv.ns.CreateMetadataBlob(cf.GetAddress(), cf.GetSize(), false, 0644)
	if err != nil {
		return fmt.Errorf("failed to create volume config metadata: %v", err)
	}
	defer metadataBlob.Release()

	return wv.ns.LinkByMetadata(".grits/volume", metadataBlob.GetAddress())
}

func (wv *LocalVolume) Start() error {
	if grits.DebugBlobStorage {
		wv.server.AddPeriodicTask(time.Second*10, wv.ns.PrintBlobStorageDebugging)
	}

	return nil
}

func (wv *LocalVolume) Stop() error {
	return nil
}

func (wv *LocalVolume) isReadOnly() bool {
	return wv.readOnly
}

func (wv *LocalVolume) Checkpoint() error {
	if wv.doPersist {
		return wv.save()
	} else {
		return nil
	}
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
func (v *LocalVolume) CreateTreeNode() (grits.FileNode, error) {
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
func (v *LocalVolume) CreateBlobNode(contentAddr grits.BlobAddr, size int64) (grits.FileNode, error) {
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
	if node.Metadata().Type != grits.GNodeTypeFile {
		return nil, fmt.Errorf("created node is not a BlobNode")
	}

	// Take an extra reference for the caller
	node.Take()
	return node, nil
}

// GetBlob retrieves a blob from the blobstore by its address
func (v *LocalVolume) GetBlob(addr grits.BlobAddr) (grits.CachedFile, error) {
	return v.ns.BlobStore.ReadFile(addr)
}

func (wv *LocalVolume) LookupFull(paths []string) (*grits.LookupResponse, error) {
	return wv.ns.LookupFull(paths)
}

func (wv *LocalVolume) LookupNode(path string) (grits.FileNode, error) {
	return wv.ns.LookupNode(path)
}

func (wv *LocalVolume) MultiLink(req []*grits.LinkRequest, returnResults bool) (*grits.LookupResponse, error) {
	result, err := wv.ns.MultiLink(req, returnResults)
	if err != nil {
		return nil, err
	}
	if wv.doPersist {
		if saveErr := wv.save(); saveErr != nil {
			// Log but don't fail the operation — the link succeeded
			log.Printf("Warning: failed to checkpoint after MultiLink: %v", saveErr)
		}
	}
	return result, nil
}

func (wv *LocalVolume) LinkByMetadata(name string, metadataAddr grits.BlobAddr) error {
	if err := wv.ns.LinkByMetadata(name, metadataAddr); err != nil {
		return err
	}
	if wv.doPersist {
		if saveErr := wv.save(); saveErr != nil {
			log.Printf("Warning: failed to checkpoint after LinkByMetadata: %v", saveErr)
		}
	}
	return nil
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
	wv.persistMtx.Lock()
	defer wv.persistMtx.Unlock()

	err := os.MkdirAll(wv.server.Config.ServerPath("var/localroots"), 0755)
	if err != nil {
		return fmt.Errorf("can't make localroot directory: %v", err)
	}

	filename := wv.server.Config.ServerPath("var/localroots/" + wv.name + ".json")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// It's fine if the file doesn't exist yet
			return nil
		}
		return err
	}

	var state RootState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to unmarshal volume state: %v", err)
	}

	err = wv.ns.DeserializeNameStore(grits.BlobAddr(state.RootAddr), state.SerialNumber, wv.volumeConfig.ClientCacheDuration)
	if err != nil {
		return fmt.Errorf("failed to deserialize name store: %v", err)
	}

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
