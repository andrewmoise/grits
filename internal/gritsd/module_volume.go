package gritsd

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type Volume interface {
	GetVolumeName() string
	Start() error
	Stop() error
	isReadOnly() bool
	Checkpoint() error

	Lookup(paths []string, startAddr grits.BlobAddr, holdRef grits.RefHoldFunc, principal *grits.Principal) (*grits.LookupResponse, error)
	LookupNode(path string, principal *grits.Principal) (grits.FileNode, error)
	lookupFromRoot(path string, rootNode grits.FileNode) (*grits.LookupResponse, error)
	GetFileNode(metadataAddr grits.BlobAddr) (grits.FileNode, error)

	CreateTreeNode() (grits.FileNode, error)
	CreateBlobNode(contentAddr grits.BlobAddr, size int64) (grits.FileNode, error)

	LinkByMetadata(path string, metadataAddr grits.BlobAddr, principal *grits.Principal) error
	MultiLink(requests []*grits.LinkRequest, returnResults bool, principal *grits.Principal) (*grits.LookupResponse, error)

	AddBlob(path string) (grits.CachedFile, error)
	AddOpenBlob(*os.File) (grits.CachedFile, error)
	AddDataBlock(data []byte) (grits.CachedFile, error)
	AddMetadataBlob(*grits.GNodeMetadata) (grits.CachedFile, error)

	GetBlob(addr grits.BlobAddr) (grits.CachedFile, error)

	Cleanup() error

	RegisterWatcher(watcher grits.FileTreeWatcher)
	UnregisterWatcher(watcher grits.FileTreeWatcher)

	FatalIfBlobMissing(err error)
}

type LocalVolume struct {
	name         string
	server       *Server
	ns           *grits.NameStore
	volumeConfig *LocalVolumeConfig

	persistMtx sync.Mutex
	doPersist  bool

}

var _ = (Volume)((*LocalVolume)(nil))

// LocalVolumeConfig
type LocalVolumeConfig struct {
	VolumeName string `json:"volumeName"`
	ReadOnly   bool   `json:"readOnly,omitempty"`
}

func NewLocalVolume(config *LocalVolumeConfig, server *Server, sparse bool, persist bool) (*LocalVolume, error) {
	ns, err := grits.EmptyNameStore(server.BlobStore, sparse)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	wv := &LocalVolume{
		name:         config.VolumeName,
		server:       server,
		ns:           ns,
		volumeConfig: config,
		doPersist:    persist,
	}

	if persist {
		err = wv.load()
		if err != nil {
			return nil, fmt.Errorf("failed to load LocalVolume %s: %v", wv.name, err)
		}
	}

	// Watch for an auth module to appear and register its permission callbacks.
	server.AddModuleHook(func(module Module) {
		auth, ok := module.(*AuthModule)
		if !ok {
			return
		}
		log.Printf("LocalVolume %q: registering auth permission callbacks", wv.name)
		wv.ns.AddLookupCallback(auth.MakeLookupCallback(wv))
			wv.ns.AddLinkCallback(auth.MakeLinkCallback(wv))
	})

	return wv, nil
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
	return wv.volumeConfig.ReadOnly
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
	return wv.volumeConfig
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

// LocalVolume implementation
func (wv *LocalVolume) Lookup(paths []string, startAddr grits.BlobAddr, holdRef grits.RefHoldFunc, principal *grits.Principal) (*grits.LookupResponse, error) {
	return wv.ns.Lookup(paths, startAddr, holdRef, principal)
}

func (wv *LocalVolume) lookupFromRoot(path string, rootNode grits.FileNode) (*grits.LookupResponse, error) {
	return wv.ns.LookupFromRoot(path, rootNode)
}

func (wv *LocalVolume) LookupNode(path string, principal *grits.Principal) (grits.FileNode, error) {
	path = strings.TrimRight(path, "/") // FIXME

	resp, err := wv.Lookup([]string{path}, "", nil, principal)
	if err != nil {
		return nil, err
	}

	paths := resp.Paths
	if len(paths) == 0 {
		return nil, fmt.Errorf("LookupNode: no paths returned for %q", path)
	}
	leaf := paths[len(paths)-1]

	if leaf.Error == "not_found" {
		return nil, grits.ErrNotExist
	}
	if leaf.Error == "access_denied" {
		return nil, &grits.ErrAccessDenied{Path: leaf.Path}
	}
	if leaf.Error != "" {
		return nil, fmt.Errorf("LookupNode: unexpected error at %q: %s", leaf.Path, leaf.Error)
	}
	if leaf.Path != path {
		return nil, fmt.Errorf("LookupNode: expected leaf at %q, got %q", path, leaf.Path)
	}

	node, err := wv.ns.GetFileNode(leaf.Addr)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (wv *LocalVolume) FatalIfBlobMissing(err error) {
	if err == nil {
		return
	}
	if _, ok := grits.IsBlobMissing(err); ok {
		log.Fatalf("FATAL: blob missing: %v", err)
	}
}

func (wv *LocalVolume) MultiLink(requests []*grits.LinkRequest, returnResults bool, principal *grits.Principal) (*grits.LookupResponse, error) {
	if wv.isReadOnly() {
		return nil, fmt.Errorf("cannot write to read-only volume")
	}
	result, err := wv.ns.MultiLink(requests, returnResults, principal)
	if err != nil {
		return nil, err
	}
	if wv.doPersist {
		if saveErr := wv.save(); saveErr != nil {
			// Log but don't fail the operation — the link succeeded
			// FIXME
			log.Printf("Warning: failed to checkpoint after MultiLink: %v", saveErr)
		}
	}
	return result, nil
}

func (wv *LocalVolume) LinkByMetadata(name string, metadataAddr grits.BlobAddr, principal *grits.Principal) error {
	_, err := wv.MultiLink([]*grits.LinkRequest{{
		Path:    name,
		NewAddr: metadataAddr,
	}}, false, principal)
	return err
}

func (wv *LocalVolume) AddBlob(path string) (grits.CachedFile, error) {
	if wv.isReadOnly() {
		return nil, fmt.Errorf("cannot write to read-only volume")
	}

	return wv.ns.BlobStore.AddLocalFile(path)
}

func (wv *LocalVolume) AddOpenBlob(file *os.File) (grits.CachedFile, error) {
	if wv.isReadOnly() {
		return nil, fmt.Errorf("cannot write to read-only volume")
	}

	return wv.ns.BlobStore.AddReader(file)
}

func (wv *LocalVolume) AddDataBlock(data []byte) (grits.CachedFile, error) {
	if wv.isReadOnly() {
		return nil, fmt.Errorf("cannot write to read-only volume")
	}

	return wv.ns.BlobStore.AddDataBlock(data)
}

// FIXME - This should go away, CreateBlobNode() instead

func (wv *LocalVolume) AddMetadataBlob(metadata *grits.GNodeMetadata) (grits.CachedFile, error) {
	if wv.isReadOnly() {
		return nil, fmt.Errorf("cannot write to read-only volume")
	}

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

	err = wv.ns.DeserializeNameStore(grits.BlobAddr(state.RootAddr), state.SerialNumber, 0)
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
