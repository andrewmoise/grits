package gritsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// RemoteVolumeConfig contains configuration for accessing a remote volume
type RemoteVolumeConfig struct {
	VolumeName string `json:"volumeName"`
	RemoteURL  string `json:"remoteUrl"`
	//FreshnessDuration   time.Duration `json:"freshnessDuration"`
	//CacheExpirationTime time.Duration `json:"cacheExpirationTime"`
}

// RemoteVolume implements the Volume interface by proxying to a local cache
// and fetching from a remote server when needed
type RemoteVolume struct {
	config     *RemoteVolumeConfig
	server     *Server
	localCache *LocalVolume // The underlying local volume that caches data
	volumeName string       // The name for this volume

	lastFetchTime time.Time // Timestamp of the most recent fetch from remote
	serialNumber  int64     // Track remote revision
	//mutex         sync.RWMutex // Protects access to shared fields
}

var _ = (Volume)((*RemoteVolume)(nil))

func NewRemoteVolume(config *RemoteVolumeConfig, server *Server) (*RemoteVolume, error) {
	config.RemoteURL = strings.TrimSuffix(config.RemoteURL, "/")

	// Extract host from RemoteURL
	parsedURL, err := url.Parse(config.RemoteURL)
	if err != nil {
		return nil, fmt.Errorf("invalid remote URL %s: %v", config.RemoteURL, err)
	}
	host := parsedURL.Host

	port := parsedURL.Port()
	if port != "" {
		standardPort := (parsedURL.Scheme == "http" && port == "80") ||
			(parsedURL.Scheme == "https" && port == "443")

		if !standardPort {
			host = fmt.Sprintf("[%s:%s]", parsedURL.Hostname(), port)
		}
	}

	// Create a LocalVolume as the cache with a name based on the remote host and volume
	localConfig := &LocalVolumeConfig{
		VolumeName: fmt.Sprintf("%s:%s (cache)", host, config.VolumeName),
	}

	// Create the local cache with read-write permissions
	localCache, err := NewLocalVolume(localConfig, server, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create local cache: %v", err)
	}

	rv := &RemoteVolume{
		config:        config,
		server:        server,
		localCache:    localCache,
		volumeName:    fmt.Sprintf("%s:%s", host, config.VolumeName),
		lastFetchTime: time.Time{},
		serialNumber:  0,
	}

	return rv, nil
}

// GetModuleName implements Module interface
func (rv *RemoteVolume) GetModuleName() string {
	return "remote"
}

// GetDependencies implements Module interface
func (rv *RemoteVolume) GetDependencies() []*Dependency {
	return []*Dependency{}
}

// GetVolumeName implements Volume interface
func (rv *RemoteVolume) GetVolumeName() string {
	return rv.volumeName
}

// Start implements Volume interface
func (rv *RemoteVolume) Start() error {
	return rv.localCache.Start()
}

// Stop implements Volume interface
func (rv *RemoteVolume) Stop() error {
	return rv.localCache.Stop()
}

// isReadOnly implements Volume interface
func (rv *RemoteVolume) isReadOnly() bool {
	// Remote volumes are read-only locally for now
	return true
}

// GetConfig implements Module interface
func (rv *RemoteVolume) GetConfig() any {
	return rv.config
}

// Checkpoint implements Volume interface
func (rv *RemoteVolume) Checkpoint() error {
	return rv.localCache.Checkpoint()
}

// LookupNode implements Volume interface
func (rv *RemoteVolume) LookupNode(path string) (grits.FileNode, error) {
	// Try local cache first
	//node, err := rv.localCache.LookupNode(path)
	//if err == nil {
	//	return node, nil // Cache hit!
	//}
	//if err != grits.ErrNotInStore {
	//	return nil, err // Real error, not just cache miss
	//}

	// Cache miss - fetch from remote
	lookupResponse := &grits.LookupResponse{
		Paths: make([]*grits.PathNodePair, 0),
	}

	err := rv.lookupFromRemote(path, lookupResponse)
	if err != nil {
		return nil, err
	}

	if lookupResponse.IsPartial {
		return nil, grits.ErrNotExist
	}
	if len(lookupResponse.Paths) <= 0 {
		return nil, fmt.Errorf("malformed lookup response, 0 paths")
	}

	metadataAddr := lookupResponse.Paths[len(lookupResponse.Paths)-1].Addr

	return rv.GetFileNode(metadataAddr)
}

// LookupFull implements Volume interface
func (rv *RemoteVolume) LookupFull(paths []string) (*grits.LookupResponse, error) {
	// FIXME - It would be better if this was atomic, but we don't have the HTTP API for it right now

	lookupResponse := &grits.LookupResponse{
		Paths: make([]*grits.PathNodePair, 0),
	}

	for _, path := range paths {
		err := rv.lookupFromRemote(path, lookupResponse)
		if err != nil {
			return nil, fmt.Errorf("%v looking up %s", err, path)
		}
	}

	return lookupResponse, nil
}

// GetFileNode implements Volume interface
func (rv *RemoteVolume) GetFileNode(metadataAddr grits.BlobAddr) (grits.FileNode, error) {
	metadataBlobFile, err := rv.GetBlob(metadataAddr)
	if err != nil {
		return nil, err
	}
	defer metadataBlobFile.Release()

	metadataReader, err := metadataBlobFile.Reader()
	if err != nil {
		return nil, err
	}
	defer metadataReader.Close()

	var metadata grits.GNodeMetadata
	decoder := json.NewDecoder(metadataReader)
	err = decoder.Decode(&metadata)
	if err != nil {
		return nil, fmt.Errorf("couldn't decode metadata: %v", err)
	}

	contentBlobFile, err := rv.GetBlob(metadata.ContentHash)
	if err != nil {
		return nil, fmt.Errorf("couldn't read content blob: %v", err)
	}
	defer contentBlobFile.Release()

	fileNode, err := rv.localCache.GetFileNode(metadataAddr)
	if err != nil {
		return nil, err
	}

	return fileNode, nil
}

// CreateTreeNode implements Volume interface
func (rv *RemoteVolume) CreateTreeNode() (*grits.TreeNode, error) {
	return rv.localCache.CreateTreeNode()
}

// CreateBlobNode implements Volume interface
func (rv *RemoteVolume) CreateBlobNode(contentAddr grits.BlobAddr, size int64) (*grits.BlobNode, error) {
	return rv.localCache.CreateBlobNode(contentAddr, size)
}

// LinkByMetadata implements Volume interface
func (rv *RemoteVolume) LinkByMetadata(_ string, _ grits.BlobAddr) error {
	return fmt.Errorf("cannot write to remote volume yet")
}

// MultiLink implements Volume interface
func (rv *RemoteVolume) MultiLink(req []*grits.LinkRequest, returnResults bool) (*grits.LookupResponse, error) {
	return nil, fmt.Errorf("cannot write to remote volume yet")
}

// AddBlob implements Volume interface
func (rv *RemoteVolume) AddBlob(path string) (grits.CachedFile, error) {
	return rv.localCache.AddBlob(path)
}

// AddOpenBlob implements Volume interface
func (rv *RemoteVolume) AddOpenBlob(file *os.File) (grits.CachedFile, error) {
	return rv.localCache.AddOpenBlob(file)
}

// AddMetadataBlob implements Volume interface
func (rv *RemoteVolume) AddMetadataBlob(metadata *grits.GNodeMetadata) (grits.CachedFile, error) {
	return rv.localCache.AddMetadataBlob(metadata)
}

// GetBlob implements Volume interface
func (rv *RemoteVolume) GetBlob(addr grits.BlobAddr) (grits.CachedFile, error) {
	cf, _ := rv.server.BlobStore.ReadFile(addr)
	if cf != nil {
		return cf, nil
	}

	url := fmt.Sprintf("%s/grits/v1/blob/%s", rv.config.RemoteURL, addr)

	resp, err := rv.httpClient().Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blob %s: %v", addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch blob %s: status %d", addr, resp.StatusCode)
	}

	cf, err = rv.server.BlobStore.AddReader(resp.Body)
	if err != nil {
		return nil, err
	}
	if cf.GetAddress() != addr {
		cf.Release()
		return nil, fmt.Errorf("mismatch of blob addr! %s != %s", cf.GetAddress(), addr)
	}

	return cf, nil
}

// PutBlob implements Volume interface
func (rv *RemoteVolume) PutBlob(file *os.File) (grits.BlobAddr, error) {
	return rv.localCache.PutBlob(file)
}

// Cleanup implements Volume interface
func (rv *RemoteVolume) Cleanup() error {
	return rv.localCache.Cleanup()
}

// RegisterWatcher implements Volume interface
func (rv *RemoteVolume) RegisterWatcher(watcher grits.FileTreeWatcher) {
	rv.localCache.RegisterWatcher(watcher)
}

// UnregisterWatcher implements Volume interface
func (rv *RemoteVolume) UnregisterWatcher(watcher grits.FileTreeWatcher) {
	rv.localCache.UnregisterWatcher(watcher)
}

// Helper methods for remote operations

// isFresh checks if our local cache is still fresh based on configuration
func (rv *RemoteVolume) isFresh() bool {
	//rv.mutex.RLock()
	//defer rv.mutex.RUnlock()

	if rv.lastFetchTime.IsZero() {
		return false // Never fetched before
	}

	return false // No caching yes
	//return time.Since(rv.lastFetchTime) < rv.config.FreshnessDuration
}

// httpClient returns a configured HTTP client for making requests
func (rv *RemoteVolume) httpClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
	}
}

func (rv *RemoteVolume) lookupFromRemote(path string, lookupResponse *grits.LookupResponse) error {
	url := fmt.Sprintf("%s/grits/v1/lookup/%s", rv.config.RemoteURL, rv.config.VolumeName)

	// Encode the path as JSON
	pathJSON, err := json.Marshal(path)
	if err != nil {
		return fmt.Errorf("failed to encode path: %v", err)
	}

	resp, err := rv.httpClient().Post(url, "application/json", bytes.NewReader(pathJSON))
	if err != nil {
		return fmt.Errorf("failed to lookup path %s: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMultiStatus {
		lookupResponse.IsPartial = true
	}
	if resp.StatusCode == http.StatusNotFound {
		return grits.ErrNotExist
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMultiStatus {
		log.Printf("    lookup failed with status %d", resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			log.Printf("    %s", body)
		}
		return fmt.Errorf("lookup failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// The HTTP handler returns [][]any where each inner array contains:
	// [path (string), metadataHash (BlobAddr), contentHash (BlobAddr), contentSize (int64)]
	var pathData [][]interface{}
	err = json.Unmarshal(body, &pathData)
	if err != nil {
		return fmt.Errorf("failed to decode lookup response: %v", err)
	}

	for _, entry := range pathData {
		if len(entry) != 4 {
			return fmt.Errorf("malformed path entry: expected 4 elements, got %d", len(entry))
		}

		path, ok := entry[0].(string)
		if !ok {
			return fmt.Errorf("invalid path type in response")
		}

		metadataAddr, ok := entry[1].(string)
		if !ok {
			return fmt.Errorf("invalid metadata address type in response")
		}

		// We only need the path and metadata address for the PathNodePair
		lookupResponse.Paths = append(lookupResponse.Paths, &grits.PathNodePair{
			Path: path,
			Addr: grits.BlobAddr(metadataAddr),
		})
	}

	return nil
}
