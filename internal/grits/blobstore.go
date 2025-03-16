package grits

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/mr-tron/base58"
	"github.com/multiformats/go-multihash"
)

/////
// LocalBlobStore

type LocalBlobStore struct {
	config     *Config
	files      map[string]*LocalCachedFile
	mtx        sync.RWMutex
	storageDir string
	lock       *FileLock
}

// Ensure that LocalBlobStore implements BlobStore
var _ BlobStore = &LocalBlobStore{}

func NewLocalBlobStore(config *Config) *LocalBlobStore {
	// Create lock path
	lockPath := config.ServerPath("var/lock/blobstore.lock")

	// Try to acquire the lock
	lock, err := AcquireLock(lockPath)
	if err != nil {
		log.Printf("Failed to acquire lock: %v\n", err)
		return nil
	}

	bs := &LocalBlobStore{
		config: config,
		files:  make(map[string]*LocalCachedFile),
		lock:   lock,
	}

	bs.storageDir = config.ServerPath("var/blobs")
	// Ensure storage directory exists
	if err := os.MkdirAll(bs.storageDir, 0755); err != nil {
		log.Printf("Failed to create storage directory: %v\n", err)
		return nil
	}

	// Initialize the LocalBlobStore by scanning the existing files in the storage path
	err = bs.scanAndLoadExistingFiles()
	if err != nil {
		log.Printf("Can't read existing LocalBlobStore files: %v\n", err)
		return nil
	}

	return bs
}

// Close releases the lock and performs any necessary cleanup
func (bs *LocalBlobStore) Close() error {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	if bs.lock != nil {
		return bs.lock.Release()
	}
	return nil
}

func (bs *LocalBlobStore) scanAndLoadExistingFiles() error {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	return filepath.Walk(bs.storageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // return error to stop the walk
		}
		if !info.IsDir() {
			relativePath, err := filepath.Rel(bs.storageDir, path)
			if err != nil {
				return fmt.Errorf("can't relativize %s: %v", path, err)
			}

			blobAddr, err := NewBlobAddrFromString(relativePath)
			if err != nil {
				log.Printf("File %s seems not to be a blob. Skipping...\n", path)
				return nil
			}

			if bs.config.ValidateBlobs {
				computedBlobAddr, err := ComputeBlobAddr(path)
				if err != nil {
					log.Printf("Error computing hash and size for file %s: %v\n", path, err)
					return err // continue scanning other files even if one fails
				}

				if computedBlobAddr.String() != relativePath {
					return fmt.Errorf("failure to verify %s", path)
				}
			}

			fileInfo, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("error obtaining file information: %s", err)
			}

			isHardLink := false

			// Type assertion to access the Sys() interface as *syscall.Stat_t
			stat, ok := fileInfo.Sys().(*syscall.Stat_t)
			if ok {
				isHardLink = (stat.Nlink > 1)
			}

			// Create a LocalCachedFile object and add it to the map
			bs.files[blobAddr.Hash] = &LocalCachedFile{
				Path:        path,
				Size:        stat.Size,
				RefCount:    0, // Initially, no references to the file
				Address:     blobAddr,
				LastTouched: info.ModTime(),
				IsHardLink:  isHardLink,
				blobStore:   bs,
			}
		}
		return nil
	})
}

func (bs *LocalBlobStore) ReadFile(blobAddr *BlobAddr) (CachedFile, error) {
	bs.mtx.RLock()
	defer bs.mtx.RUnlock()

	cachedFile, ok := bs.files[blobAddr.Hash]
	if !ok {
		return nil, fmt.Errorf("file with address %s not found in cache", blobAddr.String())
	}

	// Increment RefCount to reserve the file and protect it from cleanup
	cachedFile.RefCount++
	return cachedFile, nil
}

func (bs *LocalBlobStore) AddLocalFile(srcPath string) (CachedFile, error) {
	file, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", srcPath, err)
	}
	defer file.Close()

	return bs.AddReader(file)
}

// AddReader adds a blob to the store from an io.Reader
func (bs *LocalBlobStore) AddReader(reader io.Reader) (CachedFile, error) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	// Create the temp directory if it doesn't exist
	tmpDir := bs.config.ServerPath("var/tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create a temporary file
	tempFile, err := os.CreateTemp(tmpDir, "incoming-blob-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Calculate hash while copying to temp file
	hash, size, err := CopyAndHash(reader, tempFile)
	if err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to copy and hash data: %w", err)
	}

	// Close the temp file as we're done writing
	tempFile.Close()

	// Create the blob address
	blobAddr := NewBlobAddr(hash)

	// Check if we already have this blob
	if cachedFile, exists := bs.files[blobAddr.Hash]; exists {
		// We already have this blob, increment ref count and clean up temp file
		os.Remove(tempPath)
		cachedFile.RefCount++
		cachedFile.LastTouched = time.Now()
		return cachedFile, nil
	}

	// We don't have this blob, move temp file to final location
	destPath := filepath.Join(bs.storageDir, blobAddr.String())

	// Ensure the directory exists
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Move the file to its final location
	if err := os.Rename(tempPath, destPath); err != nil {
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to move temp file to final location: %w", err)
	}

	// Create a new CachedFile
	cachedFile := &LocalCachedFile{
		Path:        destPath,
		RefCount:    1,
		Address:     blobAddr,
		Size:        size,
		LastTouched: time.Now(),
		IsHardLink:  false,
		blobStore:   bs,
	}

	// Add to the files map
	bs.files[blobAddr.Hash] = cachedFile

	return cachedFile, nil
}

func ComputeHash(data []byte) string {
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		panic(err) // or handle error gracefully
	}
	return base58.Encode(mh)
}

// CopyAndHash reads from the source and writes to the destination, while calculating the hash
func CopyAndHash(src io.Reader, dst io.Writer) (string, int64, error) {
	// Create the hasher for SHA-256 (which multihash will use)
	hasher := sha256.New()

	// Use MultiWriter to simultaneously write to both the destination and hasher
	mw := io.MultiWriter(hasher, dst)

	// Copy data, writing to both hasher and destination
	size, err := io.Copy(mw, src)
	if err != nil {
		return "", 0, err
	}

	// Get the SHA-256 digest
	digest := hasher.Sum(nil)

	// Create the multihash encoding with the right prefix
	prefix := []byte{18, 32} // 18 = SHA-256, 32 = length in bytes
	mhash := append(prefix, digest...)

	// Base58 encode the multihash
	hash := base58.Encode(mhash)

	return hash, size, nil
}

func (bs *LocalBlobStore) AddDataBlock(data []byte) (CachedFile, error) {
	// Compute hash and size of the data
	hash := ComputeHash(data)

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	blobAddr := NewBlobAddr(hash)

	// Check if the data block already exists in the store
	if cachedFile, exists := bs.files[blobAddr.Hash]; exists {
		// Increment RefCount since the file is being accessed
		cachedFile.RefCount++
		return cachedFile, nil
	}

	// Since the data block does not exist, store it
	destPath := filepath.Join(bs.storageDir, blobAddr.String())

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return nil, fmt.Errorf("error writing data block to store: %v", err)
	}

	// Create a new CachedFile for the data block
	cachedFile := &LocalCachedFile{
		Path:        destPath,
		RefCount:    1, // Initialize RefCount to 1 for new data blocks
		Address:     blobAddr,
		Size:        int64(len(data)),
		LastTouched: time.Now(),
		blobStore:   bs,
	}

	// Add the new data block to the files map
	bs.files[blobAddr.Hash] = cachedFile

	return cachedFile, nil
}

func (bs *LocalBlobStore) Touch(cf *LocalCachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cf.LastTouched = time.Now()
	os.Chtimes(cf.Path, time.Now(), time.Now())
}

func (bs *LocalBlobStore) Take(cachedFile *LocalCachedFile) {
	bs.mtx.Lock()

	// Log the operation
	if DebugRefCounts {
		log.Printf("TAKE: %s (count: %d)",
			cachedFile.Address.Hash,
			cachedFile.RefCount)
		PrintStack()
	}

	// Perform the actual operation
	cachedFile.RefCount++
	bs.mtx.Unlock()
}

func (bs *LocalBlobStore) Release(cachedFile *LocalCachedFile) {
	bs.mtx.Lock()

	if DebugRefCounts {
		log.Printf("RELEASE: %s (count: %d)",
			cachedFile.Address.Hash,
			cachedFile.RefCount)
		PrintStack()
	}

	// Decrement ref count
	cachedFile.RefCount--
	if cachedFile.RefCount < 0 {
		log.Fatalf("Reduced ref count for %s to < 0", cachedFile.GetAddress())
	}

	// Delete file, if we're done with it
	if !bs.config.DelayedEviction && cachedFile.RefCount == 0 {
		// Remove from the files map
		delete(bs.files, cachedFile.Address.Hash)

		// Delete the file from disk (without the lock, in case of slowness)
		bs.mtx.Unlock()
		err := os.Remove(cachedFile.Path)
		if err != nil {
			log.Printf("Warning: couldn't delete file %s from cache: %v", cachedFile.Path, err)
		}
		return
	}

	bs.mtx.Unlock()
}

// BlobStoreStat represents statistics for a specific reference count
type BlobStoreStat struct {
	RefCount   int
	FileCount  int
	TotalBytes int64
}

// CollectStats gathers statistics about the blob store, broken down by reference count

// Need to hold a lock while calling this
func (bs *LocalBlobStore) collectStats() []BlobStoreStat {
	// Map to store stats by reference count
	statsByRef := make(map[int]*BlobStoreStat)

	// Collect stats for each file
	for _, file := range bs.files {
		refCount := file.RefCount

		if _, exists := statsByRef[refCount]; !exists {
			statsByRef[refCount] = &BlobStoreStat{
				RefCount: refCount,
			}
		}

		// Update statistics for this reference count
		statsByRef[refCount].FileCount++
		statsByRef[refCount].TotalBytes += file.Size
	}

	// Convert to slice and sort by reference count (descending)
	stats := make([]BlobStoreStat, 0, len(statsByRef))
	for _, stat := range statsByRef {
		stats = append(stats, *stat)
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].RefCount > stats[j].RefCount
	})

	return stats
}

// PrintStats prints statistics about the blob store to the log

// Need to hold a lock while calling this
func (bs *LocalBlobStore) printStats(currentSize int64) {
	stats := bs.collectStats()

	log.Printf("=== BlobStore Statistics ===")
	log.Printf("Total files: %d", len(bs.files))
	log.Printf("Current size: %d bytes", currentSize)
	log.Printf("Storage capacity: %d bytes", bs.config.StorageSize)
	log.Printf("Target free size: %d bytes", bs.config.StorageFreeSize)

	log.Printf("\nFiles by reference count (descending):")
	for _, stat := range stats {
		log.Printf("  RefCount %d: %d files, %d bytes",
			stat.RefCount, stat.FileCount, stat.TotalBytes)
	}

	log.Printf("=== End BlobStore Statistics ===")
}

func (bs *LocalBlobStore) EvictOldFiles() {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	var currentSize int64
	for _, file := range bs.files {
		if !file.IsHardLink {
			currentSize += file.Size
		}
	}

	if DebugBlobStorage {
		log.Printf("Check stats:")
		bs.printStats(currentSize)
	}

	if !bs.config.DelayedEviction {
		// Nothing to do, we do it on demand
		return
	}

	if currentSize <= bs.config.StorageSize {
		log.Printf("Storage usage (%d bytes) within capacity (%d bytes), no eviction needed",
			currentSize, bs.config.StorageSize)
		return
	}

	// Collect files with zero reference count
	var evictionCandidates []*LocalCachedFile
	for _, file := range bs.files {
		if file.RefCount <= 0 {
			evictionCandidates = append(evictionCandidates, file)
		}
	}

	// Sort files by last touched time (oldest first)
	sort.Slice(evictionCandidates, func(i, j int) bool {
		return evictionCandidates[i].LastTouched.Before(evictionCandidates[j].LastTouched)
	})

	// Track eviction statistics
	var evictedCount int
	var evictedBytes int64

	// Evict files until we're under the target free size
	for _, file := range evictionCandidates {
		if currentSize-evictedBytes <= bs.config.StorageFreeSize {
			break
		}

		if !file.IsHardLink {
			evictedBytes += file.Size
		}

		delete(bs.files, file.Address.Hash)
		evictedCount++

		err := os.Remove(file.Path)
		if err != nil {
			log.Printf("Warning: couldn't delete expired file %s from cache: %v", file.Path, err)
		}
	}

	log.Printf("Evicted %d files, freed %d bytes, %d total now used", evictedCount, evictedBytes, currentSize-evictedBytes)
}

/////
// LocalCachedFile

type LocalCachedFile struct {
	Path        string
	RefCount    int
	Address     *BlobAddr
	Size        int64
	LastTouched time.Time
	IsHardLink  bool
	blobStore   *LocalBlobStore
}

// Ensure that LocalCachedFile implements CachedFile
var _ CachedFile = &LocalCachedFile{}

func (c *LocalCachedFile) GetAddress() *BlobAddr {
	return c.Address
}

func (c *LocalCachedFile) GetSize() int64 {
	return c.Size
}

func (c *LocalCachedFile) GetPath() string {
	return c.Path
}

func (c *LocalCachedFile) Touch() {
	c.blobStore.mtx.Lock()
	defer c.blobStore.mtx.Unlock()

	c.LastTouched = time.Now()
}

func (c *LocalCachedFile) Reader() (io.ReadSeekCloser, error) {
	return os.Open(c.Path) // This opens the file and returns the file handle as an io.Reader
}

// Read reads a portion of the file.
func (c *LocalCachedFile) Read(offset, length int64) ([]byte, error) {
	file, err := os.Open(c.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buffer := make([]byte, length)
	_, err = file.ReadAt(buffer, offset)
	if err != nil {
		return nil, err
	}
	return buffer, nil
}

func (cf *LocalCachedFile) Release() {
	if cf != nil {
		cf.blobStore.Release(cf)
	}
}

func (cf *LocalCachedFile) Take() {
	cf.blobStore.Take(cf)
}

func (cf *LocalCachedFile) GetRefCount() int {
	return cf.RefCount
}

func (bs *LocalBlobStore) DumpStats() {
	bs.mtx.RLock()
	defer bs.mtx.RUnlock()

	log.Printf("=== BlobStore Stats ===")
	log.Printf("Total files: %d", len(bs.files))

	// Sort files by hash for consistent output
	hashes := make([]string, 0, len(bs.files))
	for hash := range bs.files {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)

	for _, hash := range hashes {
		file := bs.files[hash]
		if file.RefCount <= 0 {
			continue
		}

		log.Printf("File: %s", file.Address.String())
		log.Printf("  Size: %d bytes", file.Size)
		log.Printf("  RefCount: %d", file.RefCount)
		log.Printf("  Path: %s", file.Path)
		log.Printf("  IsHardlink: %v", file.IsHardLink)

		if file.Size <= 200 {
			// Read and print contents for small files
			data, err := file.Read(0, file.Size)
			if err != nil {
				log.Printf("  Contents: <error reading: %v>", err)
			} else {
				log.Printf("  Contents: %s", string(data))
			}
		}
		log.Printf("  ---")
	}
	log.Printf("=== End BlobStore Stats ===")
}

/////
// Prevent concurrent access

// File-based lock implementation
type FileLock struct {
	file *os.File
	path string
}

// AcquireLock attempts to acquire a lock file
func AcquireLock(lockPath string) (*FileLock, error) {
	// Ensure directory exists
	lockDir := filepath.Dir(lockPath)
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	// Open file with O_EXCL to ensure we create it or fail
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("cannot open lock file: %w", err)
	}

	// Use syscall to place an exclusive lock
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("cannot acquire lock, another process may be running: %w", err)
	}

	// Write PID to the file for diagnostics
	pid := os.Getpid()
	if _, err := fmt.Fprintf(file, "%d\n", pid); err != nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
		return nil, fmt.Errorf("cannot write PID to lock file: %w", err)
	}

	return &FileLock{file: file, path: lockPath}, nil
}

// Release unlocks and removes the lock file
func (l *FileLock) Release() error {
	if l.file == nil {
		return nil
	}

	// First unlock the file
	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	if err != nil {
		l.file.Close()
		return fmt.Errorf("failed to unlock file: %w", err)
	}

	// Then close it
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("failed to close lock file: %w", err)
	}

	l.file = nil
	return nil
}
