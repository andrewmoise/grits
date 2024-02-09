package proxy

import (
	"grits/internal/grits"
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type DirBacking struct {
	watcher   *fsnotify.Watcher
	blobStore *BlobStore
	dirPath   string
	files     map[string]*grits.CachedFile // Map to track files
}

// Constructor to initialize DirBacking with the directory path and the BlobStore
func NewDirBacking(dirPath string, blobStore *BlobStore) *DirBacking {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Failed to create watcher:", err)
	}

	return &DirBacking{
		watcher:   watcher,
		blobStore: blobStore,
		dirPath:   dirPath,
		files:     make(map[string]*grits.CachedFile),
	}
}

// Start begins monitoring the directory for changes
func (db *DirBacking) Start() {
	go db.watch()

	err := db.watcher.Add(db.dirPath)
	if err != nil {
		log.Fatal("Failed to add directory to watcher:", err)
	}

	db.initialScan()
}

// initialScan walks through the directory initially to add existing files
func (db *DirBacking) initialScan() {
	filepath.Walk(db.dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Println("Error accessing path:", path, "Error:", err)
			return err
		}
		if !info.IsDir() {
			db.addOrUpdateFile(path)
		}
		return nil
	})
}

// watch listens for file system events and handles them
func (db *DirBacking) watch() {
	for {
		select {
		case event, ok := <-db.watcher.Events:
			if !ok {
				return
			}
			db.handleEvent(event)
		case err, ok := <-db.watcher.Errors:
			if !ok {
				return
			}
			log.Println("Watcher error:", err)
		}
	}
}

// handleEvent processes file creation, modification, and deletion events
func (db *DirBacking) handleEvent(event fsnotify.Event) {
	log.Println("Event:", event)
	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		db.addOrUpdateFile(event.Name)
	} else if event.Op&fsnotify.Remove == fsnotify.Remove {
		db.removeFile(event.Name)
	}
}

// addOrUpdateFile adds a new file to the BlobStore or updates an existing one
func (db *DirBacking) addOrUpdateFile(filePath string) {
	cachedFile, err := db.blobStore.AddLocalFile(filePath)
	if err != nil {
		log.Println("Failed to add or update file in BlobStore:", err)
		return
	}

	db.files[filePath] = cachedFile
	log.Printf("File %s added/updated in BlobStore", filePath)
}

// removeFile handles the removal of a file from the BlobStore and internal tracking
func (db *DirBacking) removeFile(filePath string) {
	if cachedFile, exists := db.files[filePath]; exists {
		db.blobStore.Release(cachedFile)
		delete(db.files, filePath)
		log.Printf("File %s removed from BlobStore", filePath)
	}
}

// Stop stops the directory monitoring
func (db *DirBacking) Stop() {
	db.watcher.Close()
}
