package proxy

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"grits/internal/grits"
)

type BlobStore struct {
	config      *Config
	files       map[string]*grits.CachedFile
	mtx         sync.RWMutex
	currentSize uint64 // Add this line
}

func NewBlobStore(config *Config) *BlobStore {
	return &BlobStore{
		config: config,
		files:  make(map[string]*grits.CachedFile),
	}
}

func (bs *BlobStore) ReadFile(fileAddr *grits.FileAddr) (*grits.CachedFile, error) {
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

func (bs *BlobStore) AddLocalFile(srcPath string) (*grits.CachedFile, error) {
	hash, size, err := computeSHA256AndSize(srcPath)
	if err != nil {
		return nil, err
	}
	fileAddr := grits.NewFileAddr(hash, size)

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	// Check if file already exists in the store
	if cachedFile, exists := bs.files[fileAddr.String()]; exists {
		// Increment RefCount since the file is being accessed
		cachedFile.RefCount++
		return cachedFile, nil
	}

	// Create a new CachedFile for new files
	destPath := filepath.Join(bs.config.StorageDirectory, fileAddr.String())
	if err := copyFile(srcPath, destPath); err != nil {
		return nil, err
	}

	cachedFile := &grits.CachedFile{
		Path:        destPath,
		Size:        size,
		RefCount:    1, // Initialize RefCount to 1 for new files
		Address:     fileAddr,
		LastTouched: time.Now(),
	}

	bs.files[fileAddr.String()] = cachedFile
	bs.currentSize += size
	return cachedFile, nil
}

func (bs *BlobStore) Release(cachedFile *grits.CachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cachedFile.RefCount--
	if cachedFile.RefCount <= 0 {
		// Optional: Remove the file from the store if RefCount drops to 0
		delete(bs.files, cachedFile.Address.String())
		// Consider deleting the file from disk if no longer needed
		os.Remove(cachedFile.Path)
		bs.currentSize -= cachedFile.Size
	}
}

func computeSHA256AndSize(path string) ([]byte, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return nil, 0, err
	}

	return hasher.Sum(nil), uint64(size), nil
}

func (bs *BlobStore) evictOldFiles() {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	if bs.currentSize <= bs.config.StorageSize {
		return
	}

	var sortedFiles []*grits.CachedFile

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

		bs.currentSize -= file.Size
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
