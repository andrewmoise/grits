package grits

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// DirMirror defines the interface for directory mirroring operations
type DirMirror interface {
	Start()
	Stop()
	handleEvent(event fsnotify.Event)
	initialScan()
}

type DirToBlobsMirror struct {
	watcher   *fsnotify.Watcher
	blobStore *BlobStore
	srcPath   string
	destPath  string
	files     map[string]*CachedFile // Map to track files
	mtx       sync.Mutex
}

// Constructor to initialize DirToBlobsMirror with the directory path and the BlobStore
func NewDirToBlobsMirror(srcPath string, destPath string, blobStore *BlobStore) *DirToBlobsMirror {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Failed to create watcher:", err)
	}

	return &DirToBlobsMirror{
		watcher:   watcher,
		blobStore: blobStore,
		srcPath:   srcPath,
		destPath:  destPath,
		files:     make(map[string]*CachedFile),
	}
}

func (db *DirToBlobsMirror) Start() {
	go db.watch()

	err := db.watcher.Add(db.srcPath)
	if err != nil {
		log.Fatal("Failed to add directory to watcher:", err)
	}

	db.initialScan()
}

// initialScan walks through the directory initially to add existing files
func (db *DirToBlobsMirror) initialScan() {
	os.MkdirAll(db.destPath, 0755)

	db.mtx.Lock()
	defer db.mtx.Unlock()

	// First, synchronize the destination directory with the source
	filepath.Walk(db.destPath, func(destPath string, info os.FileInfo, err error) error {
		if err != nil {
			log.Println("Error accessing path:", destPath, "Error:", err)
			return err
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(db.destPath, destPath)
			if err != nil {
				log.Println("Error calculating relative path:", err)
				return err
			}
			srcPath := filepath.Join(db.srcPath, relPath)

			destInfo, err := os.Stat(destPath)
			if err != nil {
				log.Println("Error accessing path:", destPath, "Error:", err)
				return err
			}

			srcInfo, err := os.Stat(srcPath)
			if err != nil && err != os.ErrNotExist {
				log.Println("Error accessing path:", srcPath, "Error:", err)
				return err
			}

			if os.IsNotExist(err) || destInfo.ModTime().Before(srcInfo.ModTime()) {
				// Here you might delete the file in destPath or mark it for deletion
				log.Printf("Want to delete outdated file in destination: %s\n", destPath)
			}
		}
		return nil
	})

	// Then, ensure all files from the source are present in the destination
	filepath.Walk(db.srcPath, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			log.Println("Error accessing path:", srcPath, "Error:", err)
			return err
		}
		if !info.IsDir() {
			db.addOrUpdateFile(srcPath)
		}
		return nil
	})
}

// watch listens for file system events and handles them
func (db *DirToBlobsMirror) watch() {
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
func (db *DirToBlobsMirror) handleEvent(event fsnotify.Event) {
	log.Println("Event:", event)
	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		db.mtx.Lock()
		defer db.mtx.Unlock()

		err := db.addOrUpdateFile(event.Name)
		if err != nil {
			fmt.Println("Error adding/updating file:", err)
		}
	} else if event.Op&fsnotify.Remove == fsnotify.Remove {
		db.mtx.Lock()
		defer db.mtx.Unlock()

		err := db.removeFile(event.Name)
		if err != nil {
			fmt.Println("Error removing file:", err)
		}
	}
}

// addOrUpdateFile adds a new file to the BlobStore or updates an existing one
func (db *DirToBlobsMirror) addOrUpdateFile(srcPath string) error {
	cachedFile, err := db.blobStore.AddLocalFile(srcPath)
	if err != nil {
		return err
	}

	prevFile, exists := db.files[srcPath]
	if exists {
		db.blobStore.Release(prevFile)
	}

	db.files[srcPath] = cachedFile
	log.Printf("File %s added/updated in BlobStore", srcPath)

	relPath, err := filepath.Rel(db.srcPath, srcPath)
	if err != nil {
		return fmt.Errorf("error calculating relative path: %v", err)
	}

	// Copy the file to the destination directory
	destPath := filepath.Join(db.destPath, relPath)
	err = os.WriteFile(destPath, []byte(cachedFile.Address.String()), 0644)
	if err != nil {
		return fmt.Errorf("error copying file to destination: %v", err)
	}

	log.Printf("File %s copied to destination\n", destPath)
	return nil
}

// removeFile handles the removal of a file from the BlobStore and internal tracking
func (db *DirToBlobsMirror) removeFile(filePath string) error {
	if cachedFile, exists := db.files[filePath]; exists {
		db.blobStore.Release(cachedFile)
		delete(db.files, filePath)
		log.Printf("File %s removed from BlobStore", filePath)

		relPath, err := filepath.Rel(db.srcPath, filePath)
		if err != nil {
			return fmt.Errorf("error calculating relative path: %v", err)
		}

		// Remove the file from the destination directory
		destPath := filepath.Join(db.destPath, relPath)
		err = os.Remove(destPath)
		if err != nil {
			return fmt.Errorf("error removing file from destination: %v", err)
		}
	}

	return nil
}

// Stop stops the directory monitoring
func (db *DirToBlobsMirror) Stop() {
	db.watcher.Close()
}
