package grits

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
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
			fileAddr, err := ComputeFileAddr(path)
			if err != nil {
				log.Printf("Error computing hash and size for file %s: %v\n", path, err)
				return err // continue scanning other files even if one fails
			}

			relativePath, _ := filepath.Rel(bs.storageDir, path)

			if relativePath != fileAddr.String() {
				log.Printf("File %s seems not to be a blob %s != %s. Skipping...\n", path, relativePath, fileAddr.String())
				return nil
			}

			// Create a CachedFile object and add it to the map
			bs.files[relativePath] = &CachedFile{
				Path:        path,
				RefCount:    0, // Initially, no references to the file
				Address:     fileAddr,
				LastTouched: info.ModTime(),
			}
		}
		return nil
	})
}

func (bs *BlobStore) ReadFile(fileAddr *FileAddr) (*CachedFile, error) {
	bs.mtx.RLock()
	defer bs.mtx.RUnlock()

	cachedFile, ok := bs.files[fileAddr.String()]
	if !ok {
		return nil, fmt.Errorf("file with address %s not found in cache", fileAddr.String())
	}

	// Increment RefCount to reserve the file and protect it from cleanup
	cachedFile.RefCount++
	return cachedFile, nil
}

func (bs *BlobStore) AddLocalFile(srcPath string) (*CachedFile, error) {
	fileAddr, err := ComputeFileAddr(srcPath)
	if err != nil {
		return nil, err
	}

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	if cachedFile, exists := bs.files[fileAddr.String()]; exists {
		cachedFile.RefCount++
		return cachedFile, nil
	}

	destPath := filepath.Join(bs.storageDir, fileAddr.String())
	if err := copyFile(srcPath, destPath); err != nil {
		return nil, err
	}

	cachedFile := &CachedFile{
		Path:        destPath,
		RefCount:    1,
		Address:     fileAddr,
		LastTouched: time.Now(),
	}

	bs.files[fileAddr.String()] = cachedFile
	bs.currentSize += fileAddr.Size
	return cachedFile, nil
}

func (bs *BlobStore) AddDataBlock(data []byte, ext string) (*CachedFile, error) {
	// Compute hash and size of the data
	hash := ComputeSHA256(data)
	size := uint64(len(data))

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	fileAddr := NewFileAddr(hash, size, ext)

	// Check if the data block already exists in the store
	if cachedFile, exists := bs.files[fileAddr.String()]; exists {
		// Increment RefCount since the file is being accessed
		cachedFile.RefCount++
		return cachedFile, nil
	}

	// Since the data block does not exist, store it
	destPath := filepath.Join(bs.storageDir, fileAddr.String())

	fmt.Printf("Writing file for data block to store: %s\n", destPath)

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return nil, fmt.Errorf("error writing data block to store: %v", err)
	}

	// Create a new CachedFile for the data block
	cachedFile := &CachedFile{
		Path:        destPath,
		RefCount:    1, // Initialize RefCount to 1 for new data blocks
		Address:     fileAddr,
		LastTouched: time.Now(),
	}

	// Add the new data block to the files map
	bs.files[fileAddr.String()] = cachedFile
	bs.currentSize += size

	return cachedFile, nil
}

func (bs *BlobStore) Touch(cf *CachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cf.LastTouched = time.Now()
	os.Chtimes(cf.Path, time.Now(), time.Now())
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

		bs.currentSize -= file.Address.Size
		delete(bs.files, file.Address.String())

		err := os.Remove(file.Path)
		if err != nil {
			panic(fmt.Sprintf("Couldn't delete expired file %s from cache! %v", file.Path, err))
		}
	}
}

func copyFile(srcPath, destPath string) error {
	input, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer output.Close()

	_, err = io.Copy(output, input)
	return err
}
