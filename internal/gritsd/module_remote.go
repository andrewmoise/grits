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
	config        *RemoteVolumeConfig
	server        *Server
	localCache    *LocalVolume   // The underlying local volume that caches data
	localRootAddr grits.BlobAddr // Current root for the local cache
	volumeName    string         // The name for this volume

	lastFetchTime time.Time    // Timestamp of the most recent fetch from remote
	serialNumber  int64        // Track remote revision
	cacheMutex    sync.RWMutex // Protects lastFetchTime and localCache root addr

	// HTTP stuff
	httpClient *http.Client

	// Prefetch queue and worker
	prefetchQueue []grits.BlobAddr
	queueMutex    sync.Mutex
	queueCond     *sync.Cond
	stopWorker    chan struct{}
	workerWg      sync.WaitGroup

	// Track cached blobs and their expiration
	cachedBlobs []*cachedBlobEntry
}

// cachedBlobEntry tracks a cached blob and when to release it
type cachedBlobEntry struct {
	file      grits.CachedFile
	expiresAt time.Time
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
	localCache, err := NewLocalVolume(localConfig, server, false, true, false)
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
		httpClient:    httpClient(),
		prefetchQueue: make([]grits.BlobAddr, 0, maxPrefetchQueueSize),
		stopWorker:    make(chan struct{}),
		cachedBlobs:   make([]*cachedBlobEntry, 0),
	}
	rv.queueCond = sync.NewCond(&rv.queueMutex)

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
	err := rv.localCache.Start()
	if err != nil {
		return err
	}

	// Start the prefetch workers
	for i := 0; i < numPrefetchWorkers; i++ {
		rv.workerWg.Add(1)
		go rv.prefetchWorker(i)
	}

	// Start the cleanup worker
	rv.workerWg.Add(1)
	go rv.cleanupWorker()

	return nil
}

// Stop implements Volume interface
func (rv *RemoteVolume) Stop() error {
	// Stop all workers
	close(rv.stopWorker)
	rv.queueCond.Broadcast() // Wake up all workers so they can exit
	rv.workerWg.Wait()

	err := rv.localCache.Stop()
	if err != nil {
		return err
	}

	return nil
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
	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode()")

	var err error
	if rv.isFresh() {
		grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode():  is fresh")

		// Try local cache first
		rv.cacheMutex.Lock()
		if rv.localCache.ns.GetRoot() != string(rv.localRootAddr) {
			grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode():  linking")
			// Need to link to the new root. Depending on prefetch progress, this might fail.
			err = rv.localCache.LinkByMetadata("", rv.localRootAddr)
		}
		rv.cacheMutex.Unlock()
		if err == nil {
			// Who knows, maybe we've prefetched enough by now to do the read locally
			grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode():  looking up")
			node, err := rv.localCache.LookupNode(path)
			if err == nil {
				// Cache hit!
				grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode():  cache hit!")

				for _, childAddr := range node.Children() {
					rv.enqueuePrefetch(childAddr)
				}
				grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode():  enqueued children")

				return node, nil
			}
		}

		grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode():  cache miss looking up %s, err is %v", path, err)
	}

	// If we get here, then it was a cache miss. Fine, fetch from remote.
	lookupResponse := &grits.LookupResponse{
		Paths: make([]*grits.PathNodePair, 0),
	}

	err = rv.lookupFromRemote(path, lookupResponse)
	if err != nil {
		return nil, err
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode():  looked up from remote\n")

	if lookupResponse.IsPartial {
		return nil, grits.ErrNotExist
	}
	if len(lookupResponse.Paths) <= 0 {
		return nil, fmt.Errorf("malformed lookup response, 0 paths")
	}

	metadataAddr := lookupResponse.Paths[len(lookupResponse.Paths)-1].Addr

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "LookupNode():  returning from cache miss\n")

	return rv.GetFileNode(metadataAddr)
}

// LookupFull implements Volume interface
func (rv *RemoteVolume) LookupFull(paths []string) (*grits.LookupResponse, error) {
	// FIXME - It would be better if this was atomic, but we don't have the HTTP API for it right now

	grits.DebugLogWithTime(grits.DebugHttpPerformance, paths[0], "LookupFull()\n")

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
	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(metadataAddr), "GetFileNode()\n")

	metadataBlobFile, err := rv.GetBlob(metadataAddr)
	if err != nil {
		return nil, err
	}
	defer metadataBlobFile.Release()

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(metadataAddr), "GetFileNode():  got blob\n")

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

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(metadataAddr), "GetFileNode():  getting file node\n")

	fileNode, err := rv.NewRemoteFileNode(metadataAddr)
	if err != nil {
		return nil, err
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(metadataAddr), "GetFileNode():  all done\n")

	return fileNode, nil
}

// CreateTreeNode implements Volume interface
func (rv *RemoteVolume) CreateTreeNode() (grits.FileNode, error) {
	return rv.localCache.CreateTreeNode()
}

// CreateBlobNode implements Volume interface
func (rv *RemoteVolume) CreateBlobNode(contentAddr grits.BlobAddr, size int64) (grits.FileNode, error) {
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
	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "GetBlob()\n")

	cf, _ := rv.server.BlobStore.ReadFile(addr)
	if cf != nil {
		grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "GetBlob():  cache hit, all done\n")
		return cf, nil
	}

	url := fmt.Sprintf("%s/grits/v1/blob/%s", rv.config.RemoteURL, addr)

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "GetBlob():  doing client get\n")

	resp, err := rv.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blob %s: %v", addr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch blob %s: status %d", addr, resp.StatusCode)
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "GetBlob():  reading\n")

	cf, err = rv.server.BlobStore.AddReader(resp.Body)
	if err != nil {
		return nil, err
	}
	if cf.GetAddress() != addr {
		cf.Release()
		return nil, fmt.Errorf("mismatch of blob addr! %s != %s", cf.GetAddress(), addr)
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, string(addr), "GetBlob():  all done with cache miss\n")

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

/////
// Helper methods

// isFresh checks if our local cache is still fresh based on configuration
func (rv *RemoteVolume) isFresh() bool {
	rv.cacheMutex.RLock()
	defer rv.cacheMutex.RUnlock()

	if rv.lastFetchTime.IsZero() {
		return false // Never fetched before
	}

	return time.Since(rv.lastFetchTime) < rv.config.FreshnessDuration
}

// httpClient returns a configured HTTP client for making requests
func httpClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        numHttpConnections, // Max idle connections total
			MaxIdleConnsPerHost: numHttpConnections, // Max idle per host (most important)
			MaxConnsPerHost:     0,                  // 0 = unlimited active connections
			IdleConnTimeout:     90 * time.Second,   // How long to keep idle connections
			DisableKeepAlives:   false,              // Ensure keepalive is enabled (default)
		},
	}
}

/////
// Proxy FileNodes

type RemoteFileNode struct {
	metadataBlob grits.CachedFile
	metadata     *grits.GNodeMetadata

	contentBlob grits.CachedFile // May be nil
	contentMtx  sync.Mutex
	contentErr  error
	children    map[string]grits.BlobAddr

	refCount int
	refMtx   sync.Mutex

	rv *RemoteVolume
}

var _ = (grits.FileNode)((*RemoteFileNode)(nil))

// Implementations for BlobNode

func (rv *RemoteVolume) NewRemoteFileNode(addr grits.BlobAddr) (*RemoteFileNode, error) {
	metadataCf, err := rv.GetBlob(addr)
	if err != nil {
		return nil, err
	}
	defer metadataCf.Release()

	metadataReader, err := metadataCf.Reader()
	if err != nil {
		return nil, err
	}
	defer metadataReader.Close()

	metadataBytes, err := io.ReadAll(metadataReader)
	if err != nil {
		return nil, err
	}

	var metadata grits.GNodeMetadata
	err = UnmarshalAndValidate(metadataBytes, &metadata)
	if err != nil {
		return nil, err
	}

	metadataCf.Take() // Will be released when we're done with the whole RemoteFileNode

	result := &RemoteFileNode{
		metadataBlob: metadataCf,
		metadata:     &metadata,
		refCount:     1,
		rv:           rv,
	}
	defer result.Release()

	if metadata.Type == grits.GNodeTypeDirectory {
		// Load the children map too. We could do this one on-demand also, except that Children() has no
		// error return so we need to make sure ahead of time that it succeeds.

		contentBlob, err := result.ExportedBlob()
		if err != nil {
			return nil, err
		}

		contentReader, err := contentBlob.Reader()
		if err != nil {
			return nil, err
		}
		defer contentReader.Close()

		contentBytes, err := io.ReadAll(contentReader)
		if err != nil {
			return nil, err
		}

		dirMap := make(map[string]string)
		if err := json.Unmarshal(contentBytes, &dirMap); err != nil {
			return nil, fmt.Errorf("error parsing directory: %v", err)
		}
		result.children = make(map[string]grits.BlobAddr)
		for name, childMetadataCID := range dirMap {
			result.children[name], err = grits.NewBlobAddrFromString(childMetadataCID)
			if err != nil {
				return nil, err
			}
		}
	}

	result.Take()
	return result, nil
}

func (bn *RemoteFileNode) ExportedBlob() (grits.CachedFile, error) {
	bn.contentMtx.Lock()
	defer bn.contentMtx.Unlock()

	if bn.contentBlob == nil && bn.contentErr == nil {
		bn.contentBlob, bn.contentErr = bn.rv.GetBlob(bn.metadata.ContentHash)
	}

	return bn.contentBlob, bn.contentErr
}

func (bn *RemoteFileNode) MetadataBlob() grits.CachedFile {
	return bn.metadataBlob
}

func (bn *RemoteFileNode) Metadata() *grits.GNodeMetadata {
	return bn.metadata
}

func (bn *RemoteFileNode) Children() map[string]grits.BlobAddr {
	return bn.children
}

func (bn *RemoteFileNode) Take() {
	bn.refMtx.Lock()
	defer bn.refMtx.Unlock()

	if bn.refCount < 0 {
		grits.PrintStack()
		log.Fatalf("Ref count for %s is < 0", bn.metadataBlob.GetAddress())
	}

	bn.refCount++
}

func (bn *RemoteFileNode) Release() {
	bn.refMtx.Lock()
	defer bn.refMtx.Unlock()

	bn.refCount--
	if bn.refCount < 0 {
		grits.PrintStack()
		log.Fatalf("Reduced ref count for %s to < 0", bn.metadataBlob.GetAddress())
	} else if bn.refCount == 0 {
		bn.metadataBlob.Release()
		bn.metadataBlob = nil

		bn.contentMtx.Lock()
		defer bn.contentMtx.Unlock()

		if bn.contentBlob != nil {
			bn.contentBlob.Release()
			bn.contentBlob = nil
		}
	}
}

func (bn *RemoteFileNode) RefCount() int {
	bn.refMtx.Lock()
	defer bn.refMtx.Unlock()

	return bn.refCount
}

/////
// Prefetch functionality

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
		cf, err := rv.GetBlob(entry)
		if err != nil {
			log.Printf("prefetch worker %d failed for %s: %v", id, entry, err)
			continue
		}

		grits.DebugLogWithTime(grits.DebugHttpPerformance, string(entry), "Prefetch: Got blob")

		// Store in our cache map with expiration time
		expiresAt := time.Now().Add(rv.config.CacheExpirationTime)

		// Need to lock when appending to cachedBlobs since multiple workers access it
		rv.queueMutex.Lock()
		rv.cachedBlobs = append(rv.cachedBlobs, &cachedBlobEntry{
			file:      cf,
			expiresAt: expiresAt,
		})
		rv.queueMutex.Unlock()

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
	rv.queueMutex.Lock()
	defer rv.queueMutex.Unlock()

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
// Network fetch functions

func (rv *RemoteVolume) lookupFromRemote(path string, lookupResponse *grits.LookupResponse) error {
	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup")

	url := fmt.Sprintf("%s/grits/v1/lookup/%s", rv.config.RemoteURL, rv.config.VolumeName)

	// Encode the path as JSON
	pathJSON, err := json.Marshal(path)
	if err != nil {
		return fmt.Errorf("failed to encode path: %v", err)
	}

	resp, err := rv.httpClient.Post(url, "application/json", bytes.NewReader(pathJSON))
	if err != nil {
		return fmt.Errorf("failed to lookup path %s: %v", path, err)
	}
	defer resp.Body.Close()

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup:  posted")

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

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup:  about to read")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup:  decoding")

	// The HTTP handler returns [][]any where each inner array contains:
	// [path (string), metadataHash (BlobAddr), contentHash (BlobAddr), contentSize (int64)]
	var pathData [][]interface{}
	err = json.Unmarshal(body, &pathData)
	if err != nil {
		return fmt.Errorf("failed to decode lookup response: %v", err)
	}

	for i, entry := range pathData {
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

		contentAddr, ok := entry[2].(string)
		if !ok {
			return fmt.Errorf("invalid content address type in response")
		}

		// Update the root of the local cache, when we hit it here
		// FIXME - this will break change notifications, we need to be more clever and handle them with
		// a complex lookup here, if there's going to be someone who cares
		// FIXME - we need to validate the stuff we're getting back, and handle errors
		// FIXME - we need to handle serial numbers, not do stuff out of order if we get out or order responses, really the
		// whole of the HTTP/in memory APIs need to be unified for this
		if i == 0 {
			if path != "" {
				log.Printf("  malformed response 1")
				return fmt.Errorf("malformed lookup response, path[0] is %s instead of empty", path)
			}

			grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup:  updating root: %s", metadataAddr)
			rv.cacheMutex.Lock()
			rv.localRootAddr = grits.BlobAddr(metadataAddr)
			rv.lastFetchTime = time.Now()
			rv.cacheMutex.Unlock()
		}

		// Enqueue metadata and content hashes for prefetching
		// Skip the content hash of the last entry (the target file/dir content)
		grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup:  enqueueing metadata prefetch: %s", metadataAddr)
		rv.enqueuePrefetch(grits.BlobAddr(metadataAddr))
		if i < len(pathData)-1 {
			// This is an intermediate directory, prefetch its content too
			grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup:  enqueueing content prefetch: %s", contentAddr)
			rv.enqueuePrefetch(grits.BlobAddr(contentAddr))
		}

		// We only need the path and metadata address for the PathNodePair
		lookupResponse.Paths = append(lookupResponse.Paths, &grits.PathNodePair{
			Path: path,
			Addr: grits.BlobAddr(metadataAddr),
		})
		grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup:  done, looping")
	}

	grits.DebugLogWithTime(grits.DebugHttpPerformance, path, "Remote lookup:  all done")

	return nil
}

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
