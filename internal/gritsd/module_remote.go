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
	"reflect"
	"strings"
	"sync"
	"time"
)

/////
// Config stuff

// RemoteVolumeConfig contains configuration for accessing a remote volume
type RemoteVolumeConfig struct {
	VolumeName          string        `json:"volumeName"`
	RemoteURL           string        `json:"remoteUrl"`
	FreshnessDuration   time.Duration `json:"freshnessDuration"`
	CacheExpirationTime time.Duration `json:"cacheExpirationTime"`
}

const maxPrefetchQueueSize = 128
const numPrefetchWorkers = 32
const numHttpConnections = 32

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

	// Track cached blobs and their expiration (currently disabled)
	blobCacheMtx sync.Mutex
	cachedBlobs  []*cachedBlobEntry
}

// cachedBlobEntry tracks a cached blob and when to release it
type cachedBlobEntry struct {
	file      grits.CachedFile
	expiresAt time.Time
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
	}
	localCache, err := NewLocalVolume(localConfig, server, true, true, false) // readOnly=true, sparse=true, persist=false
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
		cachedBlobs:   make([]*cachedBlobEntry, 0),
	}
	rv.queueCond = sync.NewCond(&rv.queueMutex)

	localCache.ns.RegisterFetcher(rv)
	localCache.ns.SetForceFetch(true)

	// NOTE: We need to be more idempotent than this... really, this is a little awkward
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
	// Start the local cache
	err := rv.localCache.Start()
	if err != nil {
		return err
	}

	// Prefetch workers disabled for now
	// Uncomment to enable:
	// for i := 0; i < numPrefetchWorkers; i++ {
	// 	rv.workerWg.Add(1)
	// 	go rv.prefetchWorker(i)
	// }
	// rv.workerWg.Add(1)
	// go rv.cleanupWorker()

	return nil
}

// Stop implements Volume interface
func (rv *RemoteVolume) Stop() error {
	// Stop prefetch workers if they're running
	// Uncomment when prefetch is enabled:
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

// FetchBlob retrieves a single blob by address from the remote server
func (rv *RemoteVolume) FetchBlob(addr grits.BlobAddr) (grits.CachedFile, error) {
	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "FetchBlob()")

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
		cf.Release()
		return nil, fmt.Errorf("mismatch of blob addr! %s != %s", cf.GetAddress(), addr)
	}

	// Prefetch disabled for now
	// Uncomment to enable:
	// rv.enqueuePrefetch(addr)

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "FetchBlob(): done")

	return cf, nil
}

// FetchPath retrieves path lookup information from the remote server
// This is currently not used, but could be used for optimized bulk fetching
func (rv *RemoteVolume) FetchPath(path string) (*grits.LookupResponse, error) {
	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "FetchPath()")

	url := fmt.Sprintf("%s/grits/v1/lookup/%s", rv.config.RemoteURL, rv.config.VolumeName)

	// Encode the path as JSON
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

	// The HTTP handler returns [][]any where each inner array contains:
	// [path (string), metadataHash (BlobAddr), contentHash (BlobAddr), contentSize (int64)]
	var pathData [][]interface{}
	err = json.Unmarshal(body, &pathData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode lookup response: %v", err)
	}
	if len(pathData) == 0 {
		return nil, fmt.Errorf("empty lookup response for %s", path)
	}

	lookupResponse := &grits.LookupResponse{
		Paths:     make([]*grits.PathNodePair, 0, len(pathData)),
		IsPartial: resp.StatusCode == http.StatusMultiStatus,
	}

	for i, entry := range pathData {
		if len(entry) != 4 {
			return nil, fmt.Errorf("malformed path entry: expected 4 elements, got %d", len(entry))
		}

		entryPath, ok := entry[0].(string)
		if !ok {
			return nil, fmt.Errorf("invalid path type in response")
		}

		metadataAddr, ok := entry[1].(string)
		if !ok {
			return nil, fmt.Errorf("invalid metadata address type in response")
		}

		contentAddr, ok := entry[2].(string)
		if !ok {
			return nil, fmt.Errorf("invalid content address type in response")
		}

		// Prefetch disabled for now
		// Uncomment to enable:
		// rv.enqueuePrefetch(grits.BlobAddr(metadataAddr))
		// if i < len(pathData)-1 {
		// 	rv.enqueuePrefetch(grits.BlobAddr(contentAddr))
		// }

		lookupResponse.Paths = append(lookupResponse.Paths, &grits.PathNodePair{
			Path: entryPath,
			Addr: grits.BlobAddr(metadataAddr),
		})

		_ = i           // Suppress unused warning until prefetch is enabled
		_ = contentAddr // Suppress unused warning until prefetch is enabled
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "FetchPath(): done")

	return lookupResponse, nil
}

/////
// Helper methods

// httpClient returns a configured HTTP client for making requests
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        numHttpConnections,
			MaxIdleConnsPerHost: numHttpConnections,
			MaxConnsPerHost:     0,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
		},
	}
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

		// Store in our cache map with expiration time
		expiresAt := time.Now().Add(rv.config.CacheExpirationTime)

		// Need to lock when appending to cachedBlobs since multiple workers access it
		rv.blobCacheMtx.Lock()
		rv.cachedBlobs = append(rv.cachedBlobs, &cachedBlobEntry{
			file:      cf,
			expiresAt: expiresAt,
		})
		rv.blobCacheMtx.Unlock()

		grits.DebugLogWithTime(grits.DebugHttpPerformance, string(entry), "Prefetch: Held reference")
	}
}

// cleanupWorker periodically cleans up expired cache entries
func (rv *RemoteVolume) cleanupWorker() {
	defer rv.workerWg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rv.stopWorker:
			rv.cleanupExpiredCache(true)
			return
		case <-ticker.C:
			rv.cleanupExpiredCache(false)
		}
	}
}

// cleanupExpiredCache removes expired entries from the cache
func (rv *RemoteVolume) cleanupExpiredCache(doAll bool) {
	rv.blobCacheMtx.Lock()
	defer rv.blobCacheMtx.Unlock()

	now := time.Now()
	expiredCount := 0

	for _, entry := range rv.cachedBlobs {
		if now.After(entry.expiresAt) || doAll {
			entry.file.Release()
			expiredCount++
		} else {
			break
		}
	}

	if expiredCount > 0 {
		rv.cachedBlobs = rv.cachedBlobs[expiredCount:]
	}
}

/////
// Config unmarshaling

// Some silliness for parsing durations
func (c *RemoteVolumeConfig) UnmarshalJSON(data []byte) error {
	// First unmarshal into a map to get raw values
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Use reflection to iterate over struct fields
	v := reflect.ValueOf(c).Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		jsonTag := field.Tag.Get("json")

		// Parse the json tag (handle "name,omitempty" format)
		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" || jsonName == "-" {
			continue
		}

		// Get the raw value from JSON
		rawValue, ok := raw[jsonName]
		if !ok {
			continue
		}

		fieldValue := v.Field(i)

		// Special handling for duration fields
		if field.Type == reflect.TypeOf(time.Duration(0)) {
			if str, ok := rawValue.(string); ok {
				duration, err := time.ParseDuration(str)
				if err != nil {
					return fmt.Errorf("invalid %s: %v", jsonName, err)
				}
				fieldValue.Set(reflect.ValueOf(duration))
			}
		} else if fieldValue.Kind() == reflect.String {
			if str, ok := rawValue.(string); ok {
				fieldValue.SetString(str)
			}
		}
		// Add more type handling as needed
	}

	return nil
}
