package grits

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/rjeczalik/notify"
)

/////
// Base interface

// DirMirror defines the interface for directory mirroring operations
type Volume interface {
	Start() error
	Stop() error
	handleEvent(ei notify.EventInfo)
	initialScan() error
	isReadOnly() bool
}

type DirMirrorBase struct {
	eventChan chan notify.EventInfo // Channel for notify events
	srcPath   string
	mtx       sync.Mutex
}

func NewDirMirrorBase(srcPath string) *DirMirrorBase {
	return &DirMirrorBase{
		eventChan: make(chan notify.EventInfo, 10), // Buffer can be adjusted as needed
		srcPath:   srcPath,
	}
}

/////
// DirToBlobsMirror

type DirToBlobsMirror struct {
	*DirMirrorBase // Embedding DirMirrorBase
	bs             *BlobStore
	files          map[string]*CachedFile // File reference tracking
	destPath       string
}

func NewDirToBlobsMirror(srcPath, destPath string, blobStore *BlobStore) (*DirToBlobsMirror, error) {
	realSrcPath, err := filepath.EvalSymlinks(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate source path: %v", err)
	}

	err = os.MkdirAll(destPath, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %v", err)
	}

	realDestPath, err := filepath.EvalSymlinks(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate destination path: %v", err)
	}

	return &DirToBlobsMirror{
		DirMirrorBase: NewDirMirrorBase(realSrcPath),
		bs:            blobStore,
		destPath:      realDestPath,
		files:         make(map[string]*CachedFile),
	}, nil
}

func (db *DirToBlobsMirror) Start() error {
	log.Printf("Starting DirToBlobsMirror for %s -> %s\n", db.srcPath, db.destPath)

	// Setup recursive watch
	if err := notify.Watch(db.srcPath+"/...", db.eventChan, notify.All); err != nil {
		return fmt.Errorf("failed to watch directory: %v", err)
	}
	go db.watch()

	err := db.initialScan()
	if err != nil {
		return fmt.Errorf("failed to perform initial scan: %v", err)
	}

	return nil
}

// initialScan walks through the directory initially to add existing files
func (db *DirToBlobsMirror) initialScan() error {
	os.MkdirAll(db.destPath, 0755)

	db.mtx.Lock()
	defer db.mtx.Unlock()

	// First, synchronize the destination directory with the source
	err := filepath.Walk(db.destPath, func(destPath string, info os.FileInfo, err error) error {
		if err != nil {
			log.Println("Error accessing path:", destPath, "Error:", err)
			return err
		}

		log.Printf("Checking file %s\n", destPath)

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
	if err != nil {
		return fmt.Errorf("error walking destination directory: %v", err)
	}

	// Then, ensure all files from the source are present in the destination
	err = filepath.Walk(db.srcPath, func(srcPath string, info os.FileInfo, err error) error {
		if err != nil {
			log.Println("Error accessing path:", srcPath, "Error:", err)
			return err
		}
		if !info.IsDir() {
			err := db.addOrUpdateFile(srcPath)
			if err != nil {
				log.Println("Error adding/updating file:", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking source directory: %v", err)
	}

	return nil
}

// watch listens for file system events and handles them
func (db *DirToBlobsMirror) watch() {
	log.Printf("Watching for DirToBlobsMirror for %s -> %s\n", db.srcPath, db.destPath)
	for ei := range db.eventChan {
		log.Printf("Event received: %s on path: %s\n", ei.Event(), ei.Path())
		db.handleEvent(ei)
	}
}

// handleEvent processes file creation, modification, and deletion events
func (db *DirToBlobsMirror) handleEvent(ei notify.EventInfo) {
	switch ei.Event() {
	case notify.Create, notify.Write:
		// For create and write events, check if the path is not a directory before proceeding
		if fileInfo, err := os.Stat(ei.Path()); err == nil && !fileInfo.IsDir() {
			log.Printf("File created or modified: %s\n", ei.Path())

			db.mtx.Lock()
			defer db.mtx.Unlock()
			if err := db.addOrUpdateFile(ei.Path()); err != nil {
				log.Printf("Error adding/updating file: %v", err)
			}
		}
	case notify.Remove:
		// For remove events, proceed without stat-ing since the file no longer exists
		log.Printf("File removed: %s\n", ei.Path())

		db.mtx.Lock()
		defer db.mtx.Unlock()
		if err := db.removeFile(ei.Path()); err != nil {
			log.Printf("Error removing file: %v", err)
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

	log.Printf("DB srcPath: %s, srcPath: %s, relPath: %s\n", db.srcPath, srcPath, relPath)

	// Copy the file to the destination directory
	destPath := filepath.Join(db.destPath, relPath)

	log.Printf("Copying file %s to destination %s\n", srcPath, destPath)

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
func (db *DirToBlobsMirror) Stop() error {
	notify.Stop(db.eventChan)
	return nil
}

func (db *DirToBlobsMirror) isReadOnly() bool {
	return true
}

/////
// DirToTreeMirror

type DirToTreeMirror struct {
	*DirMirrorBase // Embedding DirMirrorBase

	destPath string
	bs       *BlobStore
	ns       *NameStore
}

func NewDirToTreeMirror(srcPath string, destPath string, blobStore *BlobStore) (*DirToTreeMirror, error) {
	log.Printf("Creating DirToTreeMirror for %s -> %s\n", srcPath, destPath)

	realSrcPath, error := filepath.EvalSymlinks(srcPath)
	if error != nil {
		return nil, fmt.Errorf("failed to evaluate source path: %v", error)
	}

	err := os.MkdirAll(destPath, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %v", err)
	}

	realDestPath, error := filepath.EvalSymlinks(destPath)
	if error != nil {
		return nil, fmt.Errorf("failed to evaluate destination path: %v", error)
	}

	ns, err := EmptyNameStore(blobStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	return &DirToTreeMirror{
		DirMirrorBase: NewDirMirrorBase(realSrcPath),
		destPath:      realDestPath,
		bs:            blobStore,
		ns:            ns,
	}, nil
}

func (dt *DirToTreeMirror) Start() error {
	log.Printf("Starting DirToTreeMirror for %s -> %s\n", dt.srcPath, dt.destPath)

	if err := notify.Watch(dt.srcPath+"/...", dt.eventChan, notify.All); err != nil {
		return fmt.Errorf("failed to watch directory: %v", err)
	}
	go dt.watch()

	err := dt.initialScan()
	if err != nil {
		return fmt.Errorf("failed to perform initial scan: %v", err)
	}

	return nil
}

func (dt *DirToTreeMirror) initialScan() error {
	log.Printf("Initial scan for DirToTreeMirror for %s -> %s\n", dt.srcPath, dt.destPath)

	dt.mtx.Lock()
	defer dt.mtx.Unlock()

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
			defer dt.bs.Release(cf)

			// Compute the relative path from srcPath to this file
			relativePath, err := filepath.Rel(dt.srcPath, path)
			if err != nil {
				return err // Propagate the error upwards
			}

			// Link a node for the new file
			key := filepath.Join(dt.destPath, relativePath)
			dt.ns.LinkBlob(key, cf.Address)
		}

		return nil
	})

	return err
}

// watch listens for file system events and handles them
func (dt *DirToTreeMirror) watch() {
	log.Printf("Watching for DirToTreeMirror for %s -> %s\n", dt.srcPath, dt.destPath)

	for ei := range dt.eventChan {
		dt.handleEvent(ei)
	}
}

// handleEvent processes file creation, modification, and deletion events
func (dt *DirToTreeMirror) handleEvent(ei notify.EventInfo) {
	switch ei.Event() {
	case notify.Create, notify.Write:
		// Only proceed with stat for create and write events to check for a regular file
		if fileInfo, err := os.Stat(ei.Path()); err == nil && !fileInfo.IsDir() {
			log.Printf("File created or modified: %s\n", ei.Path())

			dt.mtx.Lock()
			defer dt.mtx.Unlock()
			if err := dt.addOrUpdateFile(ei.Path()); err != nil {
				log.Printf("Error adding/updating file: %v", err)
			}
		}
	case notify.Remove:
		// Skip stat for remove events as the file is already removed
		log.Printf("File removed: %s\n", ei.Path())

		dt.mtx.Lock()
		defer dt.mtx.Unlock()
		if err := dt.removeFile(ei.Path()); err != nil {
			log.Printf("Error removing file: %v", err)
		}
	}
}

func (dt *DirToTreeMirror) addOrUpdateFile(srcPath string) error {
	cf, err := dt.ns.blobStore.AddLocalFile(srcPath)
	if err != nil {
		return err
	}
	defer dt.ns.blobStore.Release(cf)

	relPath, err := filepath.Rel(dt.srcPath, srcPath)
	if err != nil {
		return fmt.Errorf("error calculating relative path: %v", err)
	}

	destPath := filepath.Join(dt.destPath, relPath)

	err = dt.ns.LinkBlob(destPath, cf.Address)
	if err != nil {
		return err
	}

	return nil
}

func (dt *DirToTreeMirror) removeFile(filePath string) error {
	relPath, err := filepath.Rel(dt.srcPath, filePath)
	if err != nil {
		return fmt.Errorf("error calculating relative path: %v", err)
	}

	destPath := filepath.Join(dt.destPath, relPath)

	err = dt.ns.LinkBlob(destPath, nil)
	if err != nil {
		return err
	}

	return nil
}

// Stop stops the directory monitoring
func (dt *DirToTreeMirror) Stop() error {
	notify.Stop(dt.eventChan)
	return nil
}

func (db *DirToTreeMirror) isReadOnly() bool {
	return true
}
