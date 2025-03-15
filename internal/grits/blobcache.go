package grits

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

// BlobCache manages a time-based LRU cache of blobs with size limits
type BlobCache struct {
	blobStore   *LocalBlobStore        // The underlying blob store
	fetchBlob   FetchBlobFunc          // Function to fetch blobs not in cache
	cachedBlobs map[string]*CacheEntry // Hash -> cache entry mapping
	maxSize     int64                  // Maximum cache size in bytes
	currentSize int64                  // Current size of all cached blobs
	mutex       sync.Mutex             // Protects concurrent access
	cleanupChan chan struct{}          // Signal channel for cleanup

	// Requests for blobs that we're in the middle of fetching
	inFlight map[string]*InFlightRequest
}

// FetchBlobFunc defines a function that can retrieve a blob by its address
type FetchBlobFunc func(addr *BlobAddr) (CachedFile, error)

// CacheEntry holds metadata about a cached blob
type CacheEntry struct {
	file         CachedFile // The actual blob file
	lastAccessed time.Time  // When it was last accessed
}

// InFlightRequest tracks an ongoing blob fetch operation
type InFlightRequest struct {
	result CachedFile // The result of the fetch (nil if error)
	err    error      // Any error that occurred during fetch
	cond   *sync.Cond // Condition variable for waiting
}

// NewBlobCache creates a new blob cache with the given parameters
func NewBlobCache(blobStore *LocalBlobStore, maxSize int64, fetchFunc FetchBlobFunc) *BlobCache {
	cache := &BlobCache{
		blobStore:   blobStore,
		fetchBlob:   fetchFunc,
		cachedBlobs: make(map[string]*CacheEntry),
		inFlight:    make(map[string]*InFlightRequest),
		maxSize:     maxSize,
		cleanupChan: make(chan struct{}, 1),
	}

	// Start cleanup goroutine
	go cache.cleanupWorker()

	return cache
}

// Get retrieves a blob from the cache, fetching it if necessary
func (c *BlobCache) Get(addr *BlobAddr) (CachedFile, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// First check if it's in the cache
	if entry, exists := c.cachedBlobs[addr.Hash]; exists {
		entry.file.Take()
		entry.lastAccessed = time.Now()
		return entry.file, nil
	}

	// If not, check if it's already in the blob store
	if cachedFile, err := c.blobStore.ReadFile(addr); err == nil {
		entry := &CacheEntry{
			file:         cachedFile,
			lastAccessed: time.Now(),
		}
		c.cachedBlobs[addr.Hash] = entry
		cachedFile.Take() // We take *two* refs total, one for returning it, one for putting it in the store
		return cachedFile, nil
	}

	// If not, check if we're already fetching it (FIXME -- this is better at the blob store layer, I guess)
	if req, fetching := c.inFlight[addr.Hash]; fetching {
		// Wait until the in-flight request completes
		req.cond.Wait()
		if req.err != nil {
			return nil, req.err
		}

		// Retry the request, presumably it'll be in cache now
		return c.Get(addr)
	}

	// Not in cache and not being fetched, so we have to do a new fetch
	req := &InFlightRequest{
		cond: sync.NewCond(&c.mutex),
	}
	c.inFlight[addr.Hash] = req

	// Temporarily unlock while fetching
	c.mutex.Unlock()
	file, err := c.fetchAndCache(addr)
	c.mutex.Lock()

	// Update request state
	req.result = file
	req.err = err

	// Remove from in-flight
	delete(c.inFlight, addr.Hash)

	// Signal all waiters that something has changed
	req.cond.Broadcast()

	return file, err
}

// fetchAndCache fetches a blob and adds it to the cache
func (c *BlobCache) fetchAndCache(addr *BlobAddr) (CachedFile, error) {
	// Fetch the blob
	file, err := c.fetchBlob(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blob %s: %w", addr.Hash, err)
	}

	// Take an extra reference for the cache
	file.Take()

	// Add to cache
	c.mutex.Lock()
	c.cachedBlobs[addr.Hash] = &CacheEntry{
		file:         file,
		lastAccessed: time.Now(),
	}

	// Update size tracking
	c.currentSize += file.GetSize()

	// Signal cleanup if needed
	if c.currentSize > c.maxSize {
		select {
		case c.cleanupChan <- struct{}{}:
			// Signal sent
		default:
			// Cleanup already pending
		}
	}
	c.mutex.Unlock()

	return file, nil
}

// cleanupWorker runs in the background to handle cache size management
func (c *BlobCache) cleanupWorker() {
	for range c.cleanupChan {
		c.performCleanup()
	}
}

// performCleanup removes least recently used entries to bring size under limit
func (c *BlobCache) performCleanup() {
	targetSize := int64(float64(c.maxSize) * 0.9) // Target 90%

	// Create a list of entries ordered by access time
	var entries []*struct {
		hash  string
		entry *CacheEntry
	}

	c.mutex.Lock()
	for hash, entry := range c.cachedBlobs {
		entries = append(entries, &struct {
			hash  string
			entry *CacheEntry
		}{hash, entry})
	}
	c.mutex.Unlock()

	// Sort by last accessed (oldest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].entry.lastAccessed.Before(entries[j].entry.lastAccessed)
	})

	// Collect entries to remove
	var toRemove []CachedFile
	var freedSize int64

	c.mutex.Lock()
	currentSize := c.currentSize // Local copy to work with

	for _, e := range entries {
		// Stop if we've freed enough space
		if currentSize <= targetSize {
			break
		}

		entry := e.entry
		file := entry.file.(*LocalCachedFile) // Type assertion

		// Remove from cache and track for cleanup
		delete(c.cachedBlobs, e.hash)
		toRemove = append(toRemove, entry.file)

		freedSize += file.Size
		currentSize -= file.Size
	}

	// Update current size
	c.currentSize = currentSize
	c.mutex.Unlock()

	// Release references outside of lock
	for _, file := range toRemove {
		file.Release()
	}

	if len(toRemove) > 0 {
		log.Printf("BlobCache cleanup: removed %d items, freed %d bytes, current size: %d",
			len(toRemove), freedSize, currentSize)
	}
}
