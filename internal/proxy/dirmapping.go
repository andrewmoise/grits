package proxy

import (
	"grits/internal/grits"
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type DirMapping struct {
	watcher   *fsnotify.Watcher
	blobStore *BlobStore
	dirPath   string
	files     map[string]*grits.CachedFile // Map to track files
}

// Constructor to initialize DirMapping with the directory path and the BlobStore
func NewDirMapping(dirPath string, blobStore *BlobStore) *DirMapping {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Failed to create watcher:", err)
	}

	return &DirMapping{
		watcher:   watcher,
		blobStore: blobStore,
		dirPath:   dirPath,
		files:     make(map[string]*grits.CachedFile),
	}
}

// Start begins monitoring the directory for changes
func (dm *DirMapping) Start() {
	go dm.watch()

	err := dm.watcher.Add(dm.dirPath)
	if err != nil {
		log.Fatal("Failed to add directory to watcher:", err)
	}

	dm.initialScan()
}

// initialScan walks through the directory initially to add existing files
func (dm *DirMapping) initialScan() {
	filepath.Walk(dm.dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Println("Error accessing path:", path, "Error:", err)
			return err
		}
		if !info.IsDir() {
			dm.addOrUpdateFile(path)
		}
		return nil
	})
}

// watch listens for file system events and handles them
func (dm *DirMapping) watch() {
	for {
		select {
		case event, ok := <-dm.watcher.Events:
			if !ok {
				return
			}
			dm.handleEvent(event)
		case err, ok := <-dm.watcher.Errors:
			if !ok {
				return
			}
			log.Println("Watcher error:", err)
		}
	}
}

// handleEvent processes file creation, modification, and deletion events
func (dm *DirMapping) handleEvent(event fsnotify.Event) {
	log.Println("Event:", event)
	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		dm.addOrUpdateFile(event.Name)
	} else if event.Op&fsnotify.Remove == fsnotify.Remove {
		dm.removeFile(event.Name)
	}
}

// addOrUpdateFile adds a new file to the BlobStore or updates an existing one
func (dm *DirMapping) addOrUpdateFile(filePath string) {
	cachedFile, err := dm.blobStore.AddLocalFile(filePath)
	if err != nil {
		log.Println("Failed to add or update file in BlobStore:", err)
		return
	}

	dm.files[filePath] = cachedFile
	log.Printf("File %s added/updated in BlobStore", filePath)
}

// removeFile handles the removal of a file from the BlobStore and internal tracking
func (dm *DirMapping) removeFile(filePath string) {
	if cachedFile, exists := dm.files[filePath]; exists {
		dm.blobStore.Release(cachedFile)
		delete(dm.files, filePath)
		log.Printf("File %s removed from BlobStore", filePath)
	}
}

// Stop stops the directory monitoring
func (dm *DirMapping) Stop() {
	dm.watcher.Close()
}
