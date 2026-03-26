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
	"sync"
	"time"
)

/////
// Config stuff

// RemoteVolumeConfig contains configuration for accessing a remote volume
type RemoteVolumeConfig struct {
	VolumeName        string        `json:"volumeName"`
	RemoteURL         string        `json:"remoteUrl"`
	BlobCacheDuration time.Duration `json:"blobCacheDuration,omitempty"`
}

func (c *RemoteVolumeConfig) MarshalJSON() ([]byte, error) {
	return grits.MarshalDurationFields(c)
}

func (c *RemoteVolumeConfig) UnmarshalJSON(data []byte) error {
	return grits.UnmarshalDurationFields(data, c)
}

const maxPrefetchQueueSize = 128

// const numPrefetchWorkers = 32
// const numHttpConnections = 32

/////
// Core struct + interface

// RemoteVolume implements the Volume interface by proxying to a local cache
// and fetching from a remote server when needed
type RemoteVolume struct {
	config     *RemoteVolumeConfig
	server     *Server
	volumeName string // Fully qualified name, {remote server}:{volume}

	localCache *LocalVolume // The actual volume that does the heavy lifting

	httpClient *http.Client

	// Prefetch queue and worker (currently disabled)
	prefetchQueue []grits.BlobAddr
	queueMutex    sync.Mutex
	queueCond     *sync.Cond
	stopWorker    chan struct{}
	workerWg      sync.WaitGroup
}

var _ = (Volume)((*RemoteVolume)(nil))
var _ = (grits.BlobFetcher)((*RemoteVolume)(nil))

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

	// Create a local volume to handle the actual tree/namespace management
	localConfig := &LocalVolumeConfig{
		VolumeName: config.VolumeName,
		ReadOnly: true,
	}
	localCache, err := NewLocalVolume(localConfig, server, true, false) // readOnly=true, sparse=true, persist=false
	if err != nil {
		return nil, err
	}

	rv := &RemoteVolume{
		config:        config,
		server:        server,
		volumeName:    fmt.Sprintf("%s:%s", host, config.VolumeName),
		localCache:    localCache,
		httpClient:    httpClient(),
		prefetchQueue: make([]grits.BlobAddr, 0, maxPrefetchQueueSize),
		stopWorker:    make(chan struct{}),
	}
	rv.queueCond = sync.NewCond(&rv.queueMutex)

	localCache.ns.RegisterFetcher(rv)

	// NOTE: We need to be more idempotent than this... wrapping the thing would be better
	localCache.ns.BlobStore.RegisterFetcher(rv)

	return rv, nil
}

/////
// Module interface

// GetModuleName implements Module interface
func (rv *RemoteVolume) GetModuleName() string {
	return "remote"
}

// GetDependencies implements Module interface
func (rv *RemoteVolume) GetDependencies() []*Dependency {
	return []*Dependency{}
}

// GetConfig implements Module interface
func (rv *RemoteVolume) GetConfig() any {
	return rv.config
}

/////
// Volume interface - all proxied to localCache

// GetVolumeName implements Volume interface
func (rv *RemoteVolume) GetVolumeName() string {
	return rv.volumeName
}

// Start implements Volume interface
func (rv *RemoteVolume) Start() error {
	err := rv.localCache.Start()
	if err != nil {
		return err
	}

	rv.fetchVolumeConfig()

	// for i := 0; i < numPrefetchWorkers; i++ {
	// 	rv.workerWg.Add(1)
	// 	go rv.prefetchWorker(i)
	// }

	return nil
}

// Stop implements Volume interface
func (rv *RemoteVolume) Stop() error {
	// close(rv.stopWorker)
	// rv.queueCond.Broadcast()
	// rv.workerWg.Wait()

	// Unregister ourselves as a fetcher
	rv.localCache.ns.UnregisterFetcher(rv)
	return rv.localCache.Stop()
}

// isReadOnly implements Volume interface
func (rv *RemoteVolume) isReadOnly() bool {
	return true // Remote volumes are read-only
}

// Checkpoint implements Volume interface
func (rv *RemoteVolume) Checkpoint() error {
	return rv.localCache.Checkpoint()
}

// LookupNode implements Volume interface
func (rv *RemoteVolume) LookupNode(path string) (grits.FileNode, error) {
	return rv.localCache.LookupNode(path)
}

// LookupFull implements Volume interface
func (rv *RemoteVolume) LookupFull(paths []string) (*grits.LookupResponse, error) {
	return rv.localCache.LookupFull(paths)
}

// GetFileNode implements Volume interface
func (rv *RemoteVolume) GetFileNode(metadataAddr grits.BlobAddr) (grits.FileNode, error) {
	return rv.localCache.GetFileNode(metadataAddr)
}

// CreateTreeNode implements Volume interface
func (rv *RemoteVolume) CreateTreeNode() (grits.FileNode, error) {
	return nil, fmt.Errorf("cannot write to remote volume")
}

// CreateBlobNode implements Volume interface
func (rv *RemoteVolume) CreateBlobNode(contentAddr grits.BlobAddr, size int64) (grits.FileNode, error) {
	return nil, fmt.Errorf("cannot write to remote volume")
}

// LinkByMetadata implements Volume interface
func (rv *RemoteVolume) LinkByMetadata(_ string, _ grits.BlobAddr) error {
	return fmt.Errorf("cannot write to remote volume")
}

// MultiLink implements Volume interface
func (rv *RemoteVolume) MultiLink(req []*grits.LinkRequest, returnResults bool) (*grits.LookupResponse, error) {
	return nil, fmt.Errorf("cannot write to remote volume")
}

// AddBlob implements Volume interface
func (rv *RemoteVolume) AddBlob(path string) (grits.CachedFile, error) {
	return nil, fmt.Errorf("cannot write to remote volume")
}

// AddOpenBlob implements Volume interface
func (rv *RemoteVolume) AddOpenBlob(file *os.File) (grits.CachedFile, error) {
	return nil, fmt.Errorf("cannot write to remote volume")
}

// AddOpenBlob implements Volume interface
func (rv *RemoteVolume) AddDataBlock(data []byte) (grits.CachedFile, error) {
	return nil, fmt.Errorf("cannot write to remote volume")
}

// AddMetadataBlob implements Volume interface
func (rv *RemoteVolume) AddMetadataBlob(metadata *grits.GNodeMetadata) (grits.CachedFile, error) {
	return nil, fmt.Errorf("cannot write to remote volume")
}

// GetBlob implements Volume interface
func (rv *RemoteVolume) GetBlob(addr grits.BlobAddr) (grits.CachedFile, error) {
	return rv.localCache.GetBlob(addr)
}

// PutBlob implements Volume interface
func (rv *RemoteVolume) PutBlob(file *os.File) (grits.BlobAddr, error) {
	return "", fmt.Errorf("cannot write to remote volume")
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

/////
// BlobFetcher interface

func (rv *RemoteVolume) FetchBlob(addr grits.BlobAddr) (grits.CachedFile, error) {
	start := time.Now()
	grits.DebugLogWithTime(grits.DebugRemotePerformance, string(addr), "FetchBlob: requested")

	url := fmt.Sprintf("%s/grits/v1/blob/%s", rv.config.RemoteURL, addr)

	resp, err := rv.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blob %s: %v", addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch blob %s: status %d", addr, resp.StatusCode)
	}

	cf, err := rv.server.BlobStore.AddReader(resp.Body)
	if err != nil {
		return nil, err
	}

	if cf.GetAddress() != addr {
		return nil, fmt.Errorf("mismatch of blob addr! %s != %s", cf.GetAddress(), addr)
	}

	cf.Touch(rv.localCache.ns.CacheDuration)

	grits.DebugLogWithTime(grits.DebugRemotePerformance, string(addr),
		"FetchBlob: done (%.1fms)", float64(time.Since(start).Microseconds())/1000.0)

	return cf, nil
}

func (rv *RemoteVolume) FetchPath(path string) (*grits.LookupResponse, error) {
	start := time.Now()
	grits.DebugLogWithTime(grits.DebugRemotePerformance, path, "FetchPath: requested")

	url := fmt.Sprintf("%s/grits/v1/lookup/%s", rv.config.RemoteURL, rv.config.VolumeName)

	pathJSON, err := json.Marshal(path)
	if err != nil {
		return nil, fmt.Errorf("failed to encode path: %v", err)
	}

	resp, err := rv.httpClient.Post(url, "application/json", bytes.NewReader(pathJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to lookup path %s: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, grits.ErrNotExist
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMultiStatus {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			log.Printf("lookup failed with status %d: %s", resp.StatusCode, body)
		}
		return nil, fmt.Errorf("lookup failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var lookupResponse grits.LookupResponse
	if err := json.Unmarshal(body, &lookupResponse); err != nil {
		return nil, fmt.Errorf("failed to decode lookup response: %v", err)
	}
	if len(lookupResponse.Paths) == 0 {
		return nil, fmt.Errorf("empty lookup response for %s", path)
	}

	grits.DebugLogWithTime(grits.DebugRemotePerformance, path,
		"FetchPath: got response, %d entries, serial %d (%.1fms)",
		len(lookupResponse.Paths),
		lookupResponse.SerialNumber,
		float64(time.Since(start).Microseconds())/1000.0)

	for _, pair := range lookupResponse.Paths {
		grits.DebugLogWithTime(grits.DebugRemotePerformance, path,
			"FetchPath: entry [%s] meta=%s content=%s",
			pair.Path, pair.Addr, pair.ContentHash)
	}

	lookupResponse.IsPartial = resp.StatusCode == http.StatusMultiStatus

	grits.DebugLogWithTime(grits.DebugRemotePerformance, path,
		"FetchPath: complete (%.1fms total)",
		float64(time.Since(start).Microseconds())/1000.0)

	return &lookupResponse, nil
}

/////
// Helper methods

// httpClient returns a configured HTTP client for making requests
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			// MaxIdleConns:        numHttpConnections,
			// MaxIdleConnsPerHost: numHttpConnections,
			MaxConnsPerHost:   0,
			IdleConnTimeout:   90 * time.Second,
			DisableKeepAlives: false,
		},
	}
}

// volumeJsonConfig is the schema for the hand-authored .grits/volume.json file.
type volumeJsonConfig struct {
	ClientCacheDuration time.Duration `json:"clientCacheDuration,omitempty"`
}

func (c *volumeJsonConfig) MarshalJSON() ([]byte, error) {
	return grits.MarshalDurationFields(c)
}

func (c *volumeJsonConfig) UnmarshalJSON(data []byte) error {
	return grits.UnmarshalDurationFields(data, c)
}

// fetchVolumeConfig reads .grits/volume.json from the remote volume and applies
// clientCacheDuration to the local cache. If BlobCacheDuration is set in the
// local config it takes precedence. Falls back to 1 minute if the file is
// absent or the field is not specified.
func (rv *RemoteVolume) fetchVolumeConfig() {
	const defaultCacheDuration = 1 * time.Minute

	// Local config override wins outright — don't even read the file.
	if rv.config.BlobCacheDuration != 0 {
		rv.localCache.ns.CacheDuration = rv.config.BlobCacheDuration
		return
	}

	node, err := rv.localCache.LookupNode(".grits/volume.json")
	if err != nil {
		// File absent is normal; anything else is worth noting.
		if !grits.IsNotExist(err) {
			log.Printf("Warning: error looking up .grits/volume.json on %s: %v", rv.volumeName, err)
		}
		rv.localCache.ns.CacheDuration = defaultCacheDuration
		return
	}
	defer node.Release()

	blob, err := node.ExportedBlob()
	if err != nil {
		log.Printf("Warning: couldn't load .grits/volume.json content from %s: %v", rv.volumeName, err)
		rv.localCache.ns.CacheDuration = defaultCacheDuration
		return
	}

	data, err := blob.Read(0, blob.GetSize())
	if err != nil {
		log.Printf("Warning: couldn't read .grits/volume.json from %s: %v", rv.volumeName, err)
		rv.localCache.ns.CacheDuration = defaultCacheDuration
		return
	}

	var cfg volumeJsonConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("Warning: couldn't parse .grits/volume.json from %s: %v", rv.volumeName, err)
		rv.localCache.ns.CacheDuration = defaultCacheDuration
		return
	}

	if cfg.ClientCacheDuration == 0 {
		rv.localCache.ns.CacheDuration = defaultCacheDuration
		return
	}

	rv.localCache.ns.CacheDuration = cfg.ClientCacheDuration
	log.Printf("Remote volume %s: clientCacheDuration = %v (from .grits/volume.json)", rv.volumeName, cfg.ClientCacheDuration)
}

/////
// Prefetch functionality (currently disabled)

// enqueuePrefetch adds a blob address to the prefetch queue
// If the queue is full, it drops the oldest entries to make room
func (rv *RemoteVolume) enqueuePrefetch(addr grits.BlobAddr) {
	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "enqueuePrefetch()\n")

	rv.queueMutex.Lock()
	defer rv.queueMutex.Unlock()

	// Add the new entry
	rv.prefetchQueue = append(rv.prefetchQueue, addr)

	// If we exceed the max size, drop oldest entries from the front
	if len(rv.prefetchQueue) > maxPrefetchQueueSize {
		grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "enqueuePrefetch():  dropping entries\n")

		// Drop the oldest entries
		dropCount := len(rv.prefetchQueue) - maxPrefetchQueueSize
		rv.prefetchQueue = rv.prefetchQueue[dropCount:]
	}

	// Signal the worker that there's work available
	rv.queueCond.Signal()
	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "enqueuePrefetch():  all done\n")
}

func (rv *RemoteVolume) prefetchWorker(id int) {
	defer rv.workerWg.Done()

	for {
		// Get the next entry from the queue
		rv.queueMutex.Lock()
		for len(rv.prefetchQueue) == 0 {
			// Check if we should stop
			select {
			case <-rv.stopWorker:
				rv.queueMutex.Unlock()
				return
			default:
			}

			// Queue is empty, wait for work or shutdown
			rv.queueCond.Wait()

			// Check again after waking up
			select {
			case <-rv.stopWorker:
				rv.queueMutex.Unlock()
				return
			default:
			}
		}

		// Check again before processing
		select {
		case <-rv.stopWorker:
			rv.queueMutex.Unlock()
			return
		default:
		}

		// Pop the first (oldest) entry from the queue
		entry := rv.prefetchQueue[0]
		rv.prefetchQueue = rv.prefetchQueue[1:]
		rv.queueMutex.Unlock()

		grits.DebugLogWithTime(grits.DebugHttpPerformance, string(entry), "Prefetching")

		// Try to fetch and cache the blob
		cf, err := rv.FetchBlob(entry) // Use FetchBlob instead of GetBlob to avoid BlobStore cache check
		if err != nil {
			log.Printf("prefetch worker %d failed for %s: %v", id, entry, err)
			continue
		}

		grits.DebugLogWithTime(grits.DebugHttpPerformance, string(entry), "Prefetch: Got blob")

		cf.Touch(rv.localCache.ns.CacheDuration)
		cf.Release()

		grits.DebugLogWithTime(grits.DebugHttpPerformance, string(entry), "Prefetch: All done")
	}
}