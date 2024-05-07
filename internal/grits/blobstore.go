package grits

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

/////
// LocalBlobStore

type LocalBlobStore struct {
	config      *Config
	files       map[string]*LocalCachedFile // All files in the store
	mtx         sync.RWMutex                // Mutex for thread-safe access
	currentSize uint64
	storageDir  string
}

// Ensure that LocalBlobStore implements BlobStore
var _ BlobStore = &LocalBlobStore{}

func NewLocalBlobStore(config *Config) *LocalBlobStore {
	bs := &LocalBlobStore{
		config: config,
		files:  make(map[string]*LocalCachedFile),
	}

	bs.storageDir = config.ServerPath("var/blobs")
	// Ensure storage directory exists
	if err := os.MkdirAll(bs.storageDir, 0755); err != nil {
		log.Printf("Failed to create storage directory: %v\n", err)
		return nil
	}

	// Initialize the LocalBlobStore by scanning the existing files in the storage path
	err := bs.scanAndLoadExistingFiles()
	if err != nil {
		log.Printf("Can't read existing LocalBlobStore files: %v\n", err)
		return nil
	}

	// Add empty directory as a permanent blob never to be released
	_, err = bs.AddDataBlock([]byte("{}"))
	if err != nil {
		log.Printf("Can't create empty directory cachedFile")
		return nil
	}

	return bs
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
				log.Printf("File %s seems not to be a blob %s != %s. Skipping...\n", path, relativePath, blobAddr.String())
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
		return nil, err
	}
	defer file.Close()

	return bs.AddOpenFile(file)
}

// AddOpenFile takes an already-opened file, computes its SHA-256 hash and size,
// and adds it to the blob store if necessary.
func (bs *LocalBlobStore) AddOpenFile(file *os.File) (CachedFile, error) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	// Reset file pointer to ensure accurate size reading
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek file: %v", err)
	}

	blobHash, err := ComputeHashFromReader(file)
	if err != nil {
		return nil, err
	}

	blobAddr := NewBlobAddr(blobHash)

	if cachedFile, exists := bs.files[blobAddr.Hash]; exists {
		cachedFile.RefCount++
		cachedFile.LastTouched = time.Now() // Update the last touched time
		return cachedFile, nil
	}

	// If the blob doesn't exist in the store, copy it.
	destPath := filepath.Join(bs.storageDir, blobAddr.String())

	isHardLink := false

	if bs.config.HardLinkBlobs {
		// Attempt to create a hard link
		originalFilePath, err := filepath.Abs(file.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for original file: %v", err)
		}

		if err := os.Link(originalFilePath, destPath); err != nil {
			log.Printf("Failed to create hard link for %s: %v", file.Name(), err)
		} else {
			isHardLink = true
		}
	}

	if !isHardLink {
		// Seek back to the beginning of the file.
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("failed to seek file: %v", err)
		}

		destFile, err := os.Create(destPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create blob file: %v", err)
		}
		defer destFile.Close()

		if _, err = io.Copy(destFile, file); err != nil {
			return nil, fmt.Errorf("failed to copy file to blob store: %v", err)
		}
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %v", err)
	}
	size := uint64(fileInfo.Size())

	cachedFile := &LocalCachedFile{
		Path:        destPath,
		RefCount:    1,
		Address:     blobAddr,
		Size:        size,
		LastTouched: time.Now(),
		IsHardLink:  isHardLink,
		blobStore:   bs,
	}

	bs.files[blobAddr.Hash] = cachedFile
	if !isHardLink {
		bs.currentSize += size
	}

	return cachedFile, nil
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
		Size:        uint64(len(data)),
		LastTouched: time.Now(),
		blobStore:   bs,
	}

	// Add the new data block to the files map
	bs.files[blobAddr.Hash] = cachedFile
	bs.currentSize += uint64(len(data))

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
	defer bs.mtx.Unlock()

	cachedFile.RefCount++
}

func (bs *LocalBlobStore) Release(cachedFile *LocalCachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cachedFile.RefCount--
}

func (bs *LocalBlobStore) evictOldFiles() {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	if bs.currentSize <= bs.config.StorageSize {
		return
	}

	var sortedFiles []*LocalCachedFile

	for _, file := range bs.files {
		sortedFiles = append(sortedFiles, file)
	}

	sort.Slice(sortedFiles, func(i, j int) bool {
		return sortedFiles[i].LastTouched.Before(sortedFiles[j].LastTouched)
	})

	for _, file := range sortedFiles {
		if bs.currentSize <= bs.config.StorageFreeSize {
			break
		}
		if file.RefCount > 0 {
			continue
		}

		if !file.IsHardLink {
			bs.currentSize -= file.Size
		}
		delete(bs.files, file.Address.Hash)

		err := os.Remove(file.Path)
		if err != nil {
			panic(fmt.Sprintf("Couldn't delete expired file %s from cache! %v", file.Path, err))
		}
	}
}

/////
// LocalCachedFile

type LocalCachedFile struct {
	Path        string
	RefCount    int
	Address     *BlobAddr
	Size        uint64
	LastTouched time.Time
	IsHardLink  bool
	blobStore   *LocalBlobStore
}

// Ensure that LocalCachedFile implements CachedFile
var _ CachedFile = &LocalCachedFile{}

func (c *LocalCachedFile) GetAddress() *BlobAddr {
	return c.Address
}

func (c *LocalCachedFile) GetSize() uint64 {
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

// Read reads a portion of the file.
func (c *LocalCachedFile) Read(offset int, length int) ([]byte, error) {
	file, err := os.Open(c.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buffer := make([]byte, length)
	_, err = file.ReadAt(buffer, int64(offset))
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
