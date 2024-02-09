package proxy

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"grits/internal/grits"
)

type BlobStore struct {
	config      *Config
	files       map[string]*grits.CachedFile // All files in the store
	mtx         sync.RWMutex                 // Mutex for thread-safe access
	currentSize uint64
	storageDir  string
}

func NewBlobStore(config *Config) *BlobStore {
	bs := &BlobStore{
		config: config,
		files:  make(map[string]*grits.CachedFile),
	}

	bs.storageDir = config.VarPath("blobs")
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
			hash, size, err := computeSHA256AndSize(path)
			if err != nil {
				log.Printf("Error computing hash and size for file %s: %v\n", path, err)
				return err // continue scanning other files even if one fails
			}

			// Construct the file address from its hash
			fileAddr := grits.NewFileAddr(hash, size)
			relativePath, _ := filepath.Rel(bs.storageDir, path)

			if relativePath != fileAddr.String() {
				log.Printf("File %s seems not to be a blob. Skipping...\n", path)
				return nil
			}

			// Create a CachedFile object and add it to the map
			bs.files[relativePath] = &grits.CachedFile{
				Path:        path,
				Size:        size,
				RefCount:    0, // Initially, no references to the file
				Address:     fileAddr,
				LastTouched: info.ModTime(),
			}
		}
		return nil
	})
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
	hashStr, size, err := computeSHA256AndSize(srcPath)
	if err != nil {
		return nil, err
	}
	fileAddr := grits.NewFileAddr(hashStr, size) // Assuming NewFileAddr now accepts a string hash

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

	cachedFile := &grits.CachedFile{
		Path:        destPath,
		Size:        size,
		RefCount:    1,
		Address:     fileAddr,
		LastTouched: time.Now(),
	}

	bs.files[fileAddr.String()] = cachedFile
	bs.currentSize += size
	return cachedFile, nil
}

func (bs *BlobStore) AddDataBlock(data []byte) (*grits.CachedFile, error) {
	// Compute hash and size of the data
	hash := computeSHA256(data)
	size := uint64(len(data))

	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	fileAddr := grits.NewFileAddr(hash, size)

	// Check if the data block already exists in the store
	if cachedFile, exists := bs.files[fileAddr.String()]; exists {
		// Increment RefCount since the file is being accessed
		cachedFile.RefCount++
		return cachedFile, nil
	}

	// Since the data block does not exist, store it
	destPath := filepath.Join(bs.storageDir, fileAddr.String())

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return nil, fmt.Errorf("error writing data block to store: %v", err)
	}

	// Create a new CachedFile for the data block
	cachedFile := &grits.CachedFile{
		Path:        destPath,
		Size:        size,
		RefCount:    1, // Initialize RefCount to 1 for new data blocks
		Address:     fileAddr,
		LastTouched: time.Now(),
	}

	// Add the new data block to the files map
	bs.files[fileAddr.String()] = cachedFile
	bs.currentSize += size

	return cachedFile, nil
}

func (bs *BlobStore) Touch(cf *grits.CachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cf.LastTouched = time.Now()
	os.Chtimes(cf.Path, time.Now(), time.Now())
}

func (bs *BlobStore) Release(cachedFile *grits.CachedFile) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()

	cachedFile.RefCount--
}

// computeSHA256 takes a byte slice and returns its SHA-256 hash as a lowercase hexadecimal string.
func computeSHA256(data []byte) string {
	hasher := sha256.New()
	hasher.Write(data)                        // Write data into the hasher
	return fmt.Sprintf("%x", hasher.Sum(nil)) // Compute the SHA-256 checksum and format it as a hex string
}

// computeSHA256AndSize computes the SHA-256 hash of the file's contents and its size, returning the hash as a hex string.
func computeSHA256AndSize(path string) (string, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), uint64(size), nil // Return hash as hex string
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
