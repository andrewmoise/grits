package grits

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	files      map[BlobAddr]*LocalCachedFile
	mtx        sync.RWMutex
	storageDir string
	lock       *FileLock

	// Add fetcher support
	fetchers   []BlobFetcher
	fetcherMtx sync.RWMutex

	// Eviction worker
	stopWorker chan struct{}
	workerWg   sync.WaitGroup
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
		config:     config,
		files:      make(map[BlobAddr]*LocalCachedFile),
		lock:       lock,
		stopWorker: make(chan struct{}),
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

// Start begins the background eviction worker
func (bs *LocalBlobStore) Start() error {
	bs.workerWg.Add(1)
	go bs.evictionWorker()
	return nil
}

// Stop halts the background eviction worker
func (bs *LocalBlobStore) Stop() error {
	close(bs.stopWorker)
	bs.workerWg.Wait()
	return nil
}

// blobToPath converts a BlobAddr to its storage path with subdirectory sharding.
// e.g. QmVhvSFKf... -> storageDir/QmV/h/vSFKf...
func (bs *LocalBlobStore) blobToPath(addr BlobAddr) string {
	s := string(addr)
	// Expected format is "Qm" + at least 2 more chars; be defensive
	if len(s) < 5 {
		return filepath.Join(bs.storageDir, s)
	}
	// s[0:2] == "Qm", s[2] is first subdir char, s[3] is second subdir char
	dir1 := s[0:3] // "QmV"
	dir2 := s[3:4] // "h"
	rest := s[4:]  // remainder
	return filepath.Join(bs.storageDir, dir1, dir2, rest)
}

// tryRmdirParents attempts to remove shard subdirectories if they're now empty.
// Failures are silently ignored — the dirs will just persist until next cleanup.
func (bs *LocalBlobStore) tryRmdirParents(blobPath string) {
	dir := filepath.Dir(blobPath)
	for i := 0; i < 2; i++ { // at most 2 levels (the "h" and "QmV" dirs)
		if dir == bs.storageDir {
			break
		}
		if err := os.Remove(dir); err != nil {
			break // not empty, or some other error — stop
		}
		dir = filepath.Dir(dir)
	}
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

			// Reconstruct the blob address by stripping path separators
			blobAddrStr := strings.ReplaceAll(relativePath, string(filepath.Separator), "")
			blobAddr, err := NewBlobAddrFromString(blobAddrStr)
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

				if string(computedBlobAddr) != blobAddrStr {
					return fmt.Errorf("failure to verify %s", path)
				}
			}

			sysInfo := info.Sys()
			if sysInfo == nil {
				return fmt.Errorf("error obtaining file information: %s", path)
			}
			stat, ok := sysInfo.(*syscall.Stat_t)
			if !ok {
				return fmt.Errorf("error obtaining file information: %s", path)
			}

			// The mtime on disk is the expiration time (set by Touch).
			// If it's in the past, the blob is already eligible for eviction
			// but we load it anyway; the eviction worker will clean it up.
			expiresAt := info.ModTime()

			// Create a LocalCachedFile object and add it to the map
			bs.files[blobAddr] = &LocalCachedFile{
				Path:      path,
				Size:      stat.Size,
				RefCount:  0,
				Address:   blobAddr,
				ExpiresAt: expiresAt,
				blobStore: bs,
			}
		}
		return nil
	})
}

func (bs *LocalBlobStore) ReadFile(blobAddr BlobAddr) (CachedFile, error) {
	bs.mtx.Lock()
	cachedFile, ok := bs.files[blobAddr]
	if ok {
		cachedFile.RefCount++
		bs.mtx.Unlock()
		return cachedFile, nil
	}
	bs.mtx.Unlock()

	bs.fetcherMtx.RLock()
	fetchers := bs.fetchers
	bs.fetcherMtx.RUnlock()

	var lastErr error
	for _, fetcher := range fetchers {
		var fetchedFile CachedFile
		fetchedFile, lastErr = fetcher.FetchBlob(blobAddr) // TODO: Figure out errors a little better
		if fetchedFile != nil {
			return fetchedFile, nil
		}
	}

	if lastErr == nil {
		lastErr = &ErrBlobMissing{Addr: blobAddr}
	}

	return nil, lastErr
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

	blobAddr := BlobAddr(hash)

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	// Check if we already have this blob
	if cachedFile, exists := bs.files[blobAddr]; exists {
		// We already have this blob, increment ref count and clean up temp file
		os.Remove(tempPath)
		cachedFile.RefCount++
		return cachedFile, nil
	}

	// We don't have this blob, move temp file to final location
	destPath := bs.blobToPath(blobAddr)
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to create shard directories: %w", err)
	}

	if err := os.Rename(tempPath, destPath); err != nil {
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to move temp file to final location: %w", err)
	}

	// New blobs have no future expiry — zero ExpiresAt means delete immediately on release.
	cachedFile := &LocalCachedFile{
		Path:      destPath,
		RefCount:  1,
		Address:   blobAddr,
		Size:      size,
		ExpiresAt: time.Time{},
		blobStore: bs,
	}

	bs.files[blobAddr] = cachedFile
	return cachedFile, nil
}

func ComputeHash(data []byte) string {
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		panic(err)
	}
	return base58.Encode(mh)
}

// CopyAndHash reads from the source and writes to the destination, while calculating the hash
func CopyAndHash(src io.Reader, dst io.Writer) (string, int64, error) {
	hasher := sha256.New()
	mw := io.MultiWriter(hasher, dst)

	size, err := io.Copy(mw, src)
	if err != nil {
		return "", 0, err
	}

	digest := hasher.Sum(nil)
	prefix := []byte{18, 32} // 18 = SHA-256, 32 = length in bytes
	mhash := append(prefix, digest...)
	hash := base58.Encode(mhash)

	return hash, size, nil
}

func (bs *LocalBlobStore) AddDataBlock(data []byte) (CachedFile, error) {
	hash := ComputeHash(data)

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	blobAddr := BlobAddr(hash)

	if cachedFile, exists := bs.files[blobAddr]; exists {
		cachedFile.RefCount++
		return cachedFile, nil
	}

	destPath := bs.blobToPath(blobAddr)
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create shard directories: %w", err)
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return nil, fmt.Errorf("error writing data block to store: %v", err)
	}

	// New blobs have no future expiry — zero ExpiresAt means delete immediately on release.
	cachedFile := &LocalCachedFile{
		Path:      destPath,
		RefCount:  1,
		Address:   blobAddr,
		Size:      int64(len(data)),
		ExpiresAt: time.Time{},
		blobStore: bs,
	}

	bs.files[blobAddr] = cachedFile
	return cachedFile, nil
}

// Touch updates the in-memory expiry time and writes it to disk as the file's mtime.
func (bs *LocalBlobStore) Touch(cf *LocalCachedFile, duration time.Duration) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	now := time.Now()
	expiresAt := now.Add(duration)
	cf.ExpiresAt = expiresAt

	// Persist to disk so the expiry survives a restart.
	err := os.Chtimes(cf.Path, now, expiresAt)
	if err != nil {
		log.Printf("Warning: couldn't update timestamps for %s: %v", cf.Path, err)
	}
}

func (bs *LocalBlobStore) Take(cachedFile *LocalCachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	if DebugRefCounts {
		log.Printf("TAKE: %s (count: %d)", cachedFile.Address, cachedFile.RefCount)
		PrintStack()
	}

	cachedFile.RefCount++
}

func (bs *LocalBlobStore) Release(cachedFile *LocalCachedFile) {
	bs.mtx.Lock()

	if DebugRefCounts {
		log.Printf("RELEASE: %s (count: %d)", cachedFile.Address, cachedFile.RefCount)
		PrintStack()
	}

	cachedFile.RefCount--
	if cachedFile.RefCount < 0 {
		bs.mtx.Unlock()
		log.Fatalf("Reduced ref count for %s to < 0", cachedFile.GetAddress())
	}

	if cachedFile.RefCount > 0 {
		bs.mtx.Unlock()
		return
	}

	// RefCount just hit zero. Decide whether to delete now or defer to eviction worker.
	// All decisions and map mutation happen under the lock, eliminating the TOCTOU race.
	if time.Now().After(cachedFile.ExpiresAt) {
		// No future expiry — delete immediately.
		path := cachedFile.Path
		addr := cachedFile.Address
		delete(bs.files, addr)
		bs.mtx.Unlock()

		log.Printf("Deleting %s", path)
		if err := os.Remove(path); err != nil {
			log.Printf("Warning: couldn't delete file %s from cache: %v", path, err)
		}
		bs.tryRmdirParents(path)
		return
	}

	// Has a future expiry — keep in place and let the eviction worker handle it.
	bs.mtx.Unlock()
}

/////
// Fetcher stuff

func (bs *LocalBlobStore) RegisterFetcher(fetcher BlobFetcher) {
	bs.fetcherMtx.Lock()
	defer bs.fetcherMtx.Unlock()
	bs.fetchers = append(bs.fetchers, fetcher)
}

func (bs *LocalBlobStore) UnregisterFetcher(fetcher BlobFetcher) {
	bs.fetcherMtx.Lock()
	defer bs.fetcherMtx.Unlock()

	for i, f := range bs.fetchers {
		if f == fetcher {
			bs.fetchers[i] = bs.fetchers[len(bs.fetchers)-1]
			bs.fetchers = bs.fetchers[:len(bs.fetchers)-1]
			break
		}
	}
}

// BlobStoreStat represents statistics for a specific reference count
type BlobStoreStat struct {
	RefCount   int
	FileCount  int
	TotalBytes int64
}

// Need to hold a lock while calling this
func (bs *LocalBlobStore) collectStats() []BlobStoreStat {
	statsByRef := make(map[int]*BlobStoreStat)

	for _, file := range bs.files {
		refCount := file.RefCount
		if _, exists := statsByRef[refCount]; !exists {
			statsByRef[refCount] = &BlobStoreStat{RefCount: refCount}
		}
		statsByRef[refCount].FileCount++
		statsByRef[refCount].TotalBytes += file.Size
	}

	stats := make([]BlobStoreStat, 0, len(statsByRef))
	for _, stat := range statsByRef {
		stats = append(stats, *stat)
	}

	sort.Slice(stats, func(i, j int) bool {
		return stats[i].RefCount > stats[j].RefCount
	})

	return stats
}

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

// EvictOldFiles scans the in-memory map and removes blobs that are expired and unreferenced.
// All eviction decisions, map removal, and disk deletion happen under the lock to prevent
// any race between eviction and concurrent blob creation for the same address.
func (bs *LocalBlobStore) EvictOldFiles() {
	if !strings.Contains(bs.storageDir, "var/blobs") {
		log.Printf("cannot do blob cleanup! directory %s looks unsafe", bs.storageDir)
		return
	}

	now := time.Now()
	var evictedCount int
	var evictedBytes int64

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	for addr, cf := range bs.files {
		if cf.RefCount > 0 {
			continue
		}
		if now.After(cf.ExpiresAt) {
			delete(bs.files, addr)
			evictedCount++
			evictedBytes += cf.Size
			if err := os.Remove(cf.Path); err != nil {
				log.Printf("Warning: couldn't delete expired file %s: %v", cf.Path, err)
			}
			bs.tryRmdirParents(cf.Path)
		}
	}

	if evictedCount > 0 {
		log.Printf("Evicted %d expired blobs, freed %d bytes", evictedCount, evictedBytes)
	}
}

// evictionWorker runs periodically to clean up expired blobs
func (bs *LocalBlobStore) evictionWorker() {
	defer bs.workerWg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-bs.stopWorker:
			bs.EvictOldFiles()
			return
		case <-ticker.C:
			bs.EvictOldFiles()
		}
	}
}

/////
// LocalCachedFile

type LocalCachedFile struct {
	Path      string
	RefCount  int
	Address   BlobAddr
	Size      int64
	ExpiresAt time.Time // Zero value means "expire immediately when ref count hits 0"
	blobStore *LocalBlobStore
}

// Ensure that LocalCachedFile implements CachedFile
var _ CachedFile = &LocalCachedFile{}

func (c *LocalCachedFile) GetAddress() BlobAddr {
	return c.Address
}

func (c *LocalCachedFile) GetSize() int64 {
	return c.Size
}

func (c *LocalCachedFile) GetPath() string {
	return c.Path
}

// Touch sets the expiration time for this blob.
// duration specifies how long to keep the blob cached from now.
func (c *LocalCachedFile) Touch(duration time.Duration) {
	c.blobStore.Touch(c, duration)
}

func (c *LocalCachedFile) Reader() (io.ReadSeekCloser, error) {
	return os.Open(c.Path)
}

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

	hashes := make([]string, 0, len(bs.files))
	for hash := range bs.files {
		hashes = append(hashes, string(hash))
	}
	sort.Strings(hashes)

	for _, hash := range hashes {
		file := bs.files[BlobAddr(hash)]
		if file.RefCount <= 0 {
			continue
		}

		log.Printf("File: %s", file.Address)
		log.Printf("  Size: %d bytes", file.Size)
		log.Printf("  RefCount: %d", file.RefCount)
		log.Printf("  Path: %s", file.Path)
		log.Printf("  ExpiresAt: %v", file.ExpiresAt)

		if file.Size <= 200 {
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

type FileLock struct {
	file *os.File
	path string
}

func AcquireLock(lockPath string) (*FileLock, error) {
	lockDir := filepath.Dir(lockPath)
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("cannot open lock file: %w", err)
	}

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("cannot acquire lock, another process may be running: %w", err)
	}

	pid := os.Getpid()
	if _, err := fmt.Fprintf(file, "%d\n", pid); err != nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
		return nil, fmt.Errorf("cannot write PID to lock file: %w", err)
	}

	return &FileLock{file: file, path: lockPath}, nil
}

func (l *FileLock) Release() error {
	if l.file == nil {
		return nil
	}

	err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	if err != nil {
		l.file.Close()
		return fmt.Errorf("failed to unlock file: %w", err)
	}

	if err := l.file.Close(); err != nil {
		return fmt.Errorf("failed to close lock file: %w", err)
	}

	l.file = nil
	return nil
}