package grits

import (
	"errors"
	"testing"
	"time"
)

func TestBlobCache_Get_CacheHit(t *testing.T) {
	// Setup
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create a test blob to be "already in the cache"
	content := []byte("test blob content")
	cachedFile, err := bs.AddDataBlock(content)
	if err != nil {
		t.Fatalf("Failed to add test blob: %v", err)
	}
	addr := cachedFile.GetAddress()

	// Create cache with no fetch function (we only test cache hits)
	cache := NewBlobCache(bs, 1024*1024, nil)

	// Manually add to cache
	cache.mutex.Lock()
	cache.cachedBlobs[addr.Hash] = &CacheEntry{
		file:         cachedFile,
		lastAccessed: time.Now(),
	}
	cache.mutex.Unlock()

	// Test cache hit
	result, err := cache.Get(addr)
	if err != nil {
		t.Fatalf("Failed to get from cache: %v", err)
	}
	if result.GetAddress().Hash != addr.Hash {
		t.Errorf("Got wrong blob, expected %s, got %s", addr.Hash, result.GetAddress().Hash)
	}

	// Check refcount increased
	if result.GetRefCount() <= 1 {
		t.Errorf("Expected refcount > 1, got %d", result.GetRefCount())
	}

	// Cleanup
	result.Release()
}

func TestBlobCache_Get_FetchNew(t *testing.T) {
	// Setup
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create test content we'll "fetch"
	content := []byte("fetched content")
	fetchedAddr := NewBlobAddr(ComputeHash(content))

	// Track if fetch was called
	fetchCalled := false

	// Mock fetch function
	mockFetch := func(addr *BlobAddr) (CachedFile, error) {
		fetchCalled = true
		// Actually add to blobstore
		return bs.AddDataBlock(content)
	}

	// Create cache with mock fetch function
	cache := NewBlobCache(bs, 1024*1024, mockFetch)

	// Test fetch
	result, err := cache.Get(fetchedAddr)
	if err != nil {
		t.Fatalf("Failed to fetch blob: %v", err)
	}

	// Verify fetch was called
	if !fetchCalled {
		t.Errorf("Fetch function was not called")
	}

	// Verify correct blob was fetched
	if result.GetAddress().Hash != fetchedAddr.Hash {
		t.Errorf("Got wrong blob, expected %s, got %s", fetchedAddr.Hash, result.GetAddress().Hash)
	}

	// Check blob is now in cache
	cache.mutex.Lock()
	entry, exists := cache.cachedBlobs[fetchedAddr.Hash]
	cache.mutex.Unlock()

	if !exists {
		t.Errorf("Blob not added to cache after fetch")
	} else if entry.file.GetAddress().Hash != fetchedAddr.Hash {
		t.Errorf("Wrong blob added to cache")
	}

	// Cleanup
	result.Release()
}

func TestBlobCache_Cleanup(t *testing.T) {
	// Setup
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create a very small cache
	cacheSize := int64(100) // Very small cache to force cleanup
	cache := NewBlobCache(bs, cacheSize, nil)

	// Add several blobs exceeding cache size
	blobs := []struct {
		content []byte
		file    CachedFile
	}{
		{content: []byte("blob 1 - this takes up space")},
		{content: []byte("blob 2 - this also takes space")},
		{content: []byte("blob 3 - even more content to push out the first blob")},
	}

	// Add blobs to cache manually
	for i := range blobs {
		var err error
		blobs[i].file, err = bs.AddDataBlock(blobs[i].content)
		if err != nil {
			t.Fatalf("Failed to add test blob %d: %v", i, err)
		}

		addr := blobs[i].file.GetAddress()

		// Manual addition to cache
		cache.mutex.Lock()
		cache.cachedBlobs[addr.Hash] = &CacheEntry{
			file:         blobs[i].file,
			lastAccessed: time.Now(),
		}
		cache.currentSize += blobs[i].file.GetSize()
		blobs[i].file.Take() // Extra ref for cache

		// If we're exceeding size, trigger cleanup
		if cache.currentSize > cache.maxSize {
			select {
			case cache.cleanupChan <- struct{}{}:
			default:
			}
		}
		cache.mutex.Unlock()

		// Small sleep to ensure different last accessed times
		time.Sleep(10 * time.Millisecond)
	}

	// Give cleanup goroutine time to run
	time.Sleep(100 * time.Millisecond)

	// Check that oldest entries were removed
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	// The first blob should be evicted since it's the oldest
	if _, exists := cache.cachedBlobs[blobs[0].file.GetAddress().Hash]; exists {
		t.Errorf("Expected oldest blob to be evicted, but it's still in cache")
	}

	// The newest blob should still be in cache
	if _, exists := cache.cachedBlobs[blobs[2].file.GetAddress().Hash]; !exists {
		t.Errorf("Expected newest blob to stay in cache, but it was evicted")
	}

	// Verify cache size is under limit
	if cache.currentSize > cache.maxSize {
		t.Errorf("Cache cleanup didn't reduce size below limit: %d > %d",
			cache.currentSize, cache.maxSize)
	}
}

func TestBlobCache_ConcurrentFetch(t *testing.T) {
	// Setup
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create test content we'll "fetch"
	content := []byte("concurrent fetch content")
	addr := NewBlobAddr(ComputeHash(content))

	// Use a channel to control when fetch completes
	fetchStarted := make(chan struct{})
	fetchComplete := make(chan struct{})
	fetchCount := 0

	// Mock fetch function that pauses
	mockFetch := func(a *BlobAddr) (CachedFile, error) {
		fetchCount++
		fetchStarted <- struct{}{}
		<-fetchComplete // Wait for signal to complete
		return bs.AddDataBlock(content)
	}

	// Create cache with controlled fetch
	cache := NewBlobCache(bs, 1024*1024, mockFetch)

	// Start two concurrent fetches for the same blob
	results := make(chan struct {
		file CachedFile
		err  error
	}, 2)

	for i := 0; i < 2; i++ {
		go func() {
			file, err := cache.Get(addr)
			results <- struct {
				file CachedFile
				err  error
			}{file, err}
		}()
	}

	// Wait for first fetch to start
	<-fetchStarted

	// Let fetch complete
	fetchComplete <- struct{}{}

	// Get both results
	result1 := <-results
	result2 := <-results

	// Verify only one fetch occurred
	if fetchCount != 1 {
		t.Errorf("Expected exactly one fetch, got %d", fetchCount)
	}

	// Both results should be successful
	if result1.err != nil {
		t.Errorf("First result failed: %v", result1.err)
	}
	if result2.err != nil {
		t.Errorf("Second result failed: %v", result2.err)
	}

	// Both should have same blob
	if result1.file.GetAddress().Hash != addr.Hash {
		t.Errorf("First result has wrong blob")
	}
	if result2.file.GetAddress().Hash != addr.Hash {
		t.Errorf("Second result has wrong blob")
	}

	// Cleanup
	result1.file.Release()
	result2.file.Release()
}

func TestBlobCache_FetchError(t *testing.T) {
	// Setup
	bs, cleanup := setupBlobStore(t)
	defer cleanup()

	// Create a mock address
	addr := NewBlobAddr("QmTest123456")

	// Mock fetch function that always errors
	mockError := errors.New("simulated fetch error")
	mockFetch := func(a *BlobAddr) (CachedFile, error) {
		return nil, mockError
	}

	// Create cache with error fetch
	cache := NewBlobCache(bs, 1024*1024, mockFetch)

	// Test fetch error propagation
	_, err := cache.Get(addr)
	if err == nil {
		t.Errorf("Expected error from fetch, got nil")
	} else if !errors.Is(err, mockError) {
		t.Errorf("Got wrong error: %v", err)
	}

	// Ensure failed blob isn't in cache
	cache.mutex.Lock()
	_, exists := cache.cachedBlobs[addr.Hash]
	cache.mutex.Unlock()

	if exists {
		t.Errorf("Failed blob was erroneously added to cache")
	}
}
