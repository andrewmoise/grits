package proxy

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"sync"
	"time"
	"grits/internal/grits"
)

type FileCache struct {
	config      *Config
	currentSize uint64
	files       map[string]*grits.CachedFile
	mtx         sync.RWMutex
}

func NewFileCache(config *Config) *FileCache {
	if config.StorageFreeSize > config.StorageSize {
		panic("StorageFreeSize is bigger than StorageSize!")
	}

	return &FileCache{
		config:     config,
		currentSize: 0,
		files:      make(map[string]*grits.CachedFile),
	}
}

// ReadFile retrieves a file from the cache.
func (fc *FileCache) ReadFile(fileAddr *grits.FileAddr) (*grits.CachedFile, error) {
	fc.mtx.RLock()
	defer fc.mtx.RUnlock()
    
	cachedFile, ok := fc.files[fileAddr.String()]
	if !ok {
		return nil, fmt.Errorf("file with address %s not found in cache", fileAddr.String())
	}

	cachedFile.RefCount++
	return cachedFile, nil
}


func (fc *FileCache) CreateTempFile(fileAddr *grits.FileAddr) (*os.File, error) {
	filePath := "unknown"
	if fileAddr != nil {
		filePath = fileAddr.String()
	}
	
	return ioutil.TempFile(fc.config.StorageDirectory, filePath)
}

func (fc *FileCache) FinalizeFile(tempFile *os.File) (*grits.CachedFile, error) {
	// Close the file if not already closed by caller
	tempFile.Close()
	
	hash, size, err := computeSHA256AndSize(tempFile.Name())
	if err != nil {
		return nil, err
	}
	fileAddr := grits.NewFileAddr(hash, size)

	fc.mtx.Lock()
	defer fc.mtx.Unlock()

	_, exists := fc.files[fileAddr.String()]
	if exists {
		os.Remove(tempFile.Name())
		return nil, fmt.Errorf("file with address %s already in cache", fileAddr.String())
	}

	cachedFile := grits.NewCachedFile(tempFile.Name(), 1, fileAddr)

	fc.files[fileAddr.String()] = cachedFile
	fc.currentSize += uint64(fileAddr.Size)
	if fc.currentSize > fc.config.StorageSize {
		go fc.evictOldFiles()
	}
	
	return cachedFile, nil
}

func (fc *FileCache) AddLocalFile(srcPath string) (*grits.CachedFile, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return nil, err
	}
	defer srcFile.Close()

	destFile, err := fc.CreateTempFile(nil)
	if err != nil {
		return nil, err
	}
	
	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		destFile.Close()
		return nil, err
	}

	cachedFile, err := fc.FinalizeFile(destFile)
	if err != nil {
		return nil, err
	}

	return cachedFile, nil
}

func (fc *FileCache) Touch(cachedFile *grits.CachedFile) error {
	fc.mtx.RLock()
	defer fc.mtx.RUnlock()

	cachedFile.LastTouched = time.Now()
	return nil
}

func (fc *FileCache) Release(cachedFile *grits.CachedFile) error {
	fc.mtx.Lock()
	defer fc.mtx.Unlock()

	cachedFile.RefCount--
	return nil
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

func (fc *FileCache) evictOldFiles() {
	fc.mtx.Lock()
	defer fc.mtx.Unlock()

	if fc.currentSize <= fc.config.StorageSize {
		return
	}
	
	var sortedFiles []*grits.CachedFile

	for _, file := range fc.files {
		sortedFiles = append(sortedFiles, file)
	}

	sort.Slice(sortedFiles, func(i, j int) bool {
		return sortedFiles[i].LastTouched.Before(sortedFiles[j].LastTouched)
	})

	for _, file := range sortedFiles {
		if fc.currentSize <= fc.config.StorageFreeSize {
			break
		}
		if file.RefCount > 0 {
			continue
		}

		fc.currentSize -= file.Size
		delete(fc.files, file.Address.String())

		err := os.Remove(file.Path)
		if (err != nil) {
			panic(fmt.Sprintf("Couldn't delete expired file %s from cache! %v", file.Path, err))
		}
	}
}
	
