package grits

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

/////
// Base interface

// DirMirror defines the overall interface for anything that's like a directory mirror
type Volume interface {
	Start() error
	Stop() error

	HandleScan(file string) error
	HandleScanTree(directory string) error

	isReadOnly() bool
}

type DirMirrorBase struct {
	srcPath  string
	destPath string

	bs         *BlobStore
	dirWatcher *DirWatcher
	mtx        sync.Mutex
}

func NewDirMirrorBase(bs *BlobStore, srcPath, destPath string) *DirMirrorBase {
	dmb := &DirMirrorBase{
		bs:       bs,
		srcPath:  srcPath,
		destPath: destPath,
	}

	return dmb
}

/////
// DirToBlobsMirror

type DirToBlobsMirror struct {
	bs       *BlobStore
	srcPath  string
	destPath string

	files map[string]*CachedFile // File reference tracking

	dirWatcher *DirWatcher
	mtx        sync.Mutex
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

	db := &DirToBlobsMirror{
		bs:       blobStore,
		srcPath:  realSrcPath,
		destPath: realDestPath,

		files: make(map[string]*CachedFile),
	}
	db.dirWatcher = NewDirWatcher(blobStore.config.DirWatcherPath, srcPath, db)

	return db, nil
}

func (db *DirToBlobsMirror) Start() error {
	log.Printf("Starting DirToBlobsMirror for %s -> %s\n", db.srcPath, db.destPath)

	// Setup recursive watch
	err := db.dirWatcher.Start()
	if err != nil {
		return fmt.Errorf("cannot start watcher: %v", err)
	}

	// We do this one synchronously, to get ready before we return
	err = db.HandleScanTree(db.srcPath)
	if err != nil {
		return fmt.Errorf("failed to perform initial scan: %v", err)
	}

	return nil
}

func (db *DirToBlobsMirror) HandleScan(filename string) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	f, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// If the file does not exist, remove it from the cache.
			return db.removeFile(filename)
		}
		// For other types of errors, return the error.
		return err
	}
	defer f.Close()

	// If the file exists, proceed to add or update it in the BlobStore using the file handle.
	return db.addOrUpdateFile(filename, f)
}

func (db *DirToBlobsMirror) HandleScanTree(directory string) error {
	// Keep track of files that we found in the source directory
	currentFiles := make(map[string]bool)

	// First, walk through the source directory and try to add or update every file
	err := filepath.Walk(db.srcPath, func(path string, info os.FileInfo, err error) error {
		db.mtx.Lock()
		defer db.mtx.Unlock()

		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, err := filepath.Rel(db.srcPath, path)
			if err != nil {
				return err
			}
			fullDestPath := filepath.Join(db.destPath, relPath)
			currentFiles[fullDestPath] = true

			file, err := os.Open(path)
			if err != nil {
				if os.IsNotExist(err) {
					err = db.removeFile(path)
					return err
				} else {
					return err
				}
			}
			defer file.Close()

			err = db.addOrUpdateFile(path, file)
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("error walking source directory: %v", err)
	}

	db.mtx.Lock()
	defer db.mtx.Unlock()

	// Then, walk through the destination directory to delete files that no longer exist in the source
	err = filepath.Walk(db.destPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && !currentFiles[path] {
			// This file wasn't encountered in the source directory walk, so it should be deleted.
			//if err := os.Remove(path); err != nil {
			//	return fmt.Errorf("error removing outdated file: %v", err)
			//}
			log.Printf("Wanted to remove outdated file from destination: %s", path)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("error cleaning destination directory: %v", err)
	}

	return nil
}

// addOrUpdateFile adds a new file to the BlobStore or updates an existing one
func (db *DirToBlobsMirror) addOrUpdateFile(srcPath string, file *os.File) error {
	cachedFile, err := db.bs.AddOpenFile(file)
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

	// Prepare the destination path
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
	err := db.dirWatcher.Stop()
	return err
}

func (db *DirToBlobsMirror) isReadOnly() bool {
	return true
}

/////
// DirToTreeMirror

type DirToTreeMirror struct {
	*DirMirrorBase
	ns *NameStore
}

func NewDirToTreeMirror(srcPath string, destPath string, blobStore *BlobStore) (*DirToTreeMirror, error) {
	log.Printf("Creating DirToTreeMirror for %s -> %s\n", srcPath, destPath)

	realSrcPath, error := filepath.EvalSymlinks(srcPath)
	if error != nil {
		return nil, fmt.Errorf("failed to evaluate source path: %v", error)
	}

	ns, err := EmptyNameStore(blobStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	dt := &DirToTreeMirror{
		DirMirrorBase: NewDirMirrorBase(blobStore, realSrcPath, destPath),
		ns:            ns,
	}
	dt.dirWatcher = NewDirWatcher(blobStore.config.DirWatcherPath, realSrcPath, dt)

	return dt, nil
}

func (dt *DirToTreeMirror) Start() error {
	log.Printf("Starting DirToTreeMirror for %s -> %s\n", dt.srcPath, dt.destPath)

	err := dt.dirWatcher.Start()
	if err != nil {
		return err
	}

	err = dt.HandleScanTree(dt.srcPath)
	if err != nil {
		return err
	}

	return nil
}

func (db *DirToTreeMirror) HandleScan(filename string) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	f, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// If the file does not exist, remove it from the cache.
			return db.removeFile(filename)
		}
		// For other types of errors, return the error.
		return err
	}
	defer f.Close()

	// If the file exists, proceed to add or update it in the BlobStore using the file handle.
	return db.addOrUpdateFile(filename, f)
}

func (dt *DirToTreeMirror) HandleScanTree(directory string) error {
	// Okay for this one we leverage some of the usefulness of our NameStore primitives

	newDirNs, err := EmptyNameStore(dt.bs)
	if err != nil {
		return err
	}

	// Walk through the source directory and put all files into newDirNs
	err = filepath.Walk(dt.srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		defer file.Close()

		cf, err := dt.bs.AddOpenFile(file)
		if err != nil {
			return err
		}
		defer dt.bs.Release(cf)

		relPath, err := filepath.Rel(dt.srcPath, path)
		if err != nil {
			return err
		}

		err = newDirNs.LinkBlob(relPath, cf.Address)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	dt.mtx.Lock()
	defer dt.mtx.Unlock()

	fileAddr, err := NewTypedFileAddrFromString(newDirNs.GetRoot())
	if err != nil {
		return err
	}

	dt.ns.Link(dt.destPath, fileAddr)
	newDirNs.Link("", nil)

	return nil
}

func (dt *DirToTreeMirror) addOrUpdateFile(srcPath string, file *os.File) error {
	cf, err := dt.ns.blobStore.AddOpenFile(file)
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
	err := dt.dirWatcher.Stop()
	return err
}

func (db *DirToTreeMirror) isReadOnly() bool {
	return true
}
