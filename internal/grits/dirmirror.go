package grits

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

/////
// Base interface

// DirMirror defines the interface for directory mirroring operations
type DirMirror interface {
	Start()
	Stop()
	handleEvent(event fsnotify.Event)
	initialScan()
}

type DirMirrorBase struct {
	watcher *fsnotify.Watcher
	srcPath string
	files   map[string]*CachedFile // File reference tracking
	mtx     sync.Mutex
}

func NewDirMirrorBase(srcPath string) *DirMirrorBase {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Failed to create watcher:", err)
	}

	return &DirMirrorBase{
		watcher: watcher,
		srcPath: srcPath,
		files:   make(map[string]*CachedFile),
	}
}

/////
// DirToBlobsMirror

type DirToBlobsMirror struct {
	*DirMirrorBase // Embedding DirMirrorBase
	bs             *BlobStore
	destPath       string
}

func NewDirToBlobsMirror(srcPath, destPath string, blobStore *BlobStore) *DirToBlobsMirror {
	return &DirToBlobsMirror{
		DirMirrorBase: NewDirMirrorBase(srcPath),
		bs:            blobStore,
		destPath:      destPath,
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
	cachedFile, err := db.bs.AddLocalFile(srcPath)
	if err != nil {
		return err
	}

	prevFile, exists := db.files[srcPath]
	if exists {
		db.bs.Release(prevFile)
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
		db.bs.Release(cachedFile)
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

/////
// DirToTreeMirror

type DirToTreeMirror struct {
	*DirMirrorBase // Embedding DirMirrorBase

	destPath string
	bs       *BlobStore
	ns       *NameStore
}

func NewDirToTreeMirror(srcPath string, destPath string, blobStore *BlobStore, nameStore *NameStore) *DirToTreeMirror {
	log.Printf("Creating DirToTreeMirror for %s -> %s\n", srcPath, destPath)

	return &DirToTreeMirror{
		DirMirrorBase: NewDirMirrorBase(srcPath),
		destPath:      destPath,
		bs:            blobStore,
		ns:            nameStore,
	}
}

func (dt *DirToTreeMirror) Start() {
	log.Printf("Starting DirToTreeMirror for %s -> %s\n", dt.srcPath, dt.destPath)

	go dt.watch()

	err := dt.watcher.Add(dt.srcPath)
	if err != nil {
		log.Fatal("Failed to add directory to watcher:", err)
	}

	dt.initialScan()
}

// initialScan walks through the directory initially to add existing files
func (dt *DirToTreeMirror) initialScan() {
	log.Printf("Initial scan for DirToTreeMirror for %s -> %s\n", dt.srcPath, dt.destPath)

	dt.mtx.Lock()
	defer dt.mtx.Unlock()

	dt.ns.ReviseRoot(dt.bs, func(m map[string]*FileAddr) error {
		// Delete keys with prefix dt.destPath
		for key := range m {
			if strings.HasPrefix(key, dt.destPath) {
				log.Printf("  Deleting key %s\n", key)

				delete(m, key)
			}
		}

		// Walk through the filesystem starting at dt.srcPath
		err := filepath.Walk(dt.srcPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err // Propagate the error upwards
			}

			if !info.IsDir() {
				log.Printf("  Adding file %s\n", path)

				// Process the file
				cf, err := dt.bs.AddLocalFile(path)
				if err != nil {
					return err // Propagate the error upwards
				}

				// Compute the relative path from srcPath to this file
				relativePath, err := filepath.Rel(dt.srcPath, path)
				if err != nil {
					dt.bs.Release(cf)
					return err // Propagate the error upwards
				}

				// Construct the key for the map m
				key := filepath.Join(dt.destPath, relativePath)

				// Update the map with the new file address
				m[key] = cf.Address
				dt.files[path] = cf
			}

			return nil // Continue walking
		})

		return err // Return any error encountered during filepath.Walk
	})
}

// watch listens for file system events and handles them
func (dt *DirToTreeMirror) watch() {
	for {
		select {
		case event, ok := <-dt.watcher.Events:
			if !ok {
				return
			}
			dt.handleEvent(event)
		case err, ok := <-dt.watcher.Errors:
			if !ok {
				return
			}
			log.Println("Watcher error:", err)
		}
	}
}

// handleEvent processes file creation, modification, and deletion events
func (dt *DirToTreeMirror) handleEvent(event fsnotify.Event) {
	log.Println("DTM Event:", event)
	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		dt.mtx.Lock()
		defer dt.mtx.Unlock()

		err := dt.addOrUpdateFile(event.Name)
		if err != nil {
			fmt.Println("Error adding/updating file:", err)
		}
	} else if event.Op&fsnotify.Remove == fsnotify.Remove {
		dt.mtx.Lock()
		defer dt.mtx.Unlock()

		err := dt.removeFile(event.Name)
		if err != nil {
			fmt.Println("Error removing file:", err)
		}
	}
}

// addOrUpdateFile adds a new file to the BlobStore or updates an existing one
func (dt *DirToTreeMirror) addOrUpdateFile(srcPath string) error {
	cachedFile, err := dt.bs.AddLocalFile(srcPath)
	if err != nil {
		return err
	}

	prevFile, exists := dt.files[srcPath]
	if exists {
		dt.bs.Release(prevFile)
	}

	dt.ns.ReviseRoot(dt.bs, func(m map[string]*FileAddr) error {
		relPath, err := filepath.Rel(dt.srcPath, srcPath)
		if err != nil {
			return fmt.Errorf("error calculating relative path: %v", err)
		}

		// Construct the key for the map m
		key := filepath.Join(dt.destPath, relPath)
		m[key] = cachedFile.Address
		return nil
	})

	dt.files[srcPath] = cachedFile
	log.Printf("File %s added/updated in BlobStore", srcPath)
	return nil
}

// removeFile handles the removal of a file from the BlobStore and internal tracking
func (dt *DirToTreeMirror) removeFile(filePath string) error {
	if cachedFile, exists := dt.files[filePath]; exists {
		dt.bs.Release(cachedFile)
		delete(dt.files, filePath)
		log.Printf("File %s removed from BlobStore", filePath)

		relPath, err := filepath.Rel(dt.srcPath, filePath)
		if err != nil {
			return fmt.Errorf("error calculating relative path: %v", err)
		}

		dt.ns.ReviseRoot(dt.bs, func(m map[string]*FileAddr) error {
			// Construct the key for the map m
			key := filepath.Join(dt.destPath, relPath)
			delete(m, key)
			return nil
		})
	}

	return nil
}

// Stop stops the directory monitoring
func (dt *DirToTreeMirror) Stop() {
	dt.watcher.Close()
}
