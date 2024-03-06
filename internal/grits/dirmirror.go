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
// DirToTreeMirror

type DirToTreeMirror struct {
	*DirMirrorBase
	ns *NameStore
}

func NewDirToTreeMirror(srcPath string, destPath string, blobStore *BlobStore, shutdownFunc func()) (*DirToTreeMirror, error) {
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
	dt.dirWatcher = NewDirWatcher(blobStore.config.DirWatcherPath, realSrcPath, dt, shutdownFunc)

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
