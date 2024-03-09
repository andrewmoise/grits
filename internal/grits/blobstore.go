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
)

type BlobStore struct {
	config      *Config
	files       map[string]*CachedFile // All files in the store
	mtx         sync.RWMutex           // Mutex for thread-safe access
	currentSize uint64
	storageDir  string
}

func NewBlobStore(config *Config) *BlobStore {
	bs := &BlobStore{
		config: config,
		files:  make(map[string]*CachedFile),
	}

	bs.storageDir = config.ServerPath("var/blobs")
	// Ensure storage directory exists
	if err := os.MkdirAll(bs.storageDir, 0755); err != nil {
		log.Printf("Failed to create storage directory: %v\n", err)
		return nil
	}

	// Initialize the BlobStore by scanning the existing files in the storage path
	err := bs.scanAndLoadExistingFiles()
	if err != nil {
		log.Printf("Can't read existing BlobStore files: %v\n", err)
		return nil
	}

	return bs
}

func (bs *BlobStore) scanAndLoadExistingFiles() error {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	return filepath.Walk(bs.storageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err // return error to stop the walk
		}
		if !info.IsDir() {
			relativePath, err := filepath.Rel(bs.storageDir, path)
			if err != nil {
				return fmt.Errorf("Can't relativize %s: %v", path, err)
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

			// Create a CachedFile object and add it to the map
			bs.files[blobAddr.Hash] = &CachedFile{
				Path:        path,
				RefCount:    0, // Initially, no references to the file
				Address:     blobAddr,
				LastTouched: info.ModTime(),
				IsHardLink:  isHardLink,
			}
		}
		return nil
	})
}

func (bs *BlobStore) ReadFile(blobAddr *BlobAddr) (*CachedFile, error) {
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

func (bs *BlobStore) AddLocalFile(srcPath string) (*CachedFile, error) {
	file, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return bs.AddOpenFile(file)
}

// AddOpenFile takes an already-opened file, computes its SHA-256 hash and size,
// and adds it to the blob store if necessary.
func (bs *BlobStore) AddOpenFile(file *os.File) (*CachedFile, error) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	// Compute the SHA-256 hash and size of the file.
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return nil, err
	}

	blobAddr := NewBlobAddr(fmt.Sprintf("%x", hasher.Sum(nil)), uint64(size))

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
			return nil, fmt.Errorf("failed to create hard link: %v", err)
		}

		isHardLink = true
	} else {
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

	cachedFile := &CachedFile{
		Path:        destPath,
		RefCount:    1,
		Address:     blobAddr,
		LastTouched: time.Now(),
		IsHardLink:  isHardLink,
	}

	bs.files[blobAddr.Hash] = cachedFile
	if !isHardLink {
		bs.currentSize += blobAddr.Size
	}

	return cachedFile, nil
}

func (bs *BlobStore) AddDataBlock(data []byte) (*CachedFile, error) {
	// Compute hash and size of the data
	hash := ComputeSHA256(data)
	size := uint64(len(data))

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	blobAddr := NewBlobAddr(hash, size)

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
	cachedFile := &CachedFile{
		Path:        destPath,
		RefCount:    1, // Initialize RefCount to 1 for new data blocks
		Address:     blobAddr,
		LastTouched: time.Now(),
	}

	// Add the new data block to the files map
	bs.files[blobAddr.Hash] = cachedFile
	bs.currentSize += size

	return cachedFile, nil
}

func (bs *BlobStore) Touch(cf *CachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cf.LastTouched = time.Now()
	os.Chtimes(cf.Path, time.Now(), time.Now())
}

func (bs *BlobStore) Take(cachedFile *CachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cachedFile.RefCount++
}

func (bs *BlobStore) Release(cachedFile *CachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cachedFile.RefCount--
}

func (bs *BlobStore) evictOldFiles() {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	if bs.currentSize <= bs.config.StorageSize {
		return
	}

	var sortedFiles []*CachedFile

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
			bs.currentSize -= file.Address.Size
		}
		delete(bs.files, file.Address.Hash)

		err := os.Remove(file.Path)
		if err != nil {
			panic(fmt.Sprintf("Couldn't delete expired file %s from cache! %v", file.Path, err))
		}
	}
}
