package server

import (
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Volume defines the interface for directory mirroring operations
type Volume interface {
	Start() error
	Stop() error
	HandleScan(file string) error
	HandleScanTree(directory string) error
	isReadOnly() bool
}

// DirToTreeMirror is responsible for mirroring a directory structure to a tree structure in the blob store.
type DirToTreeMirror struct {
	srcPath    string
	destPath   string
	server     *Server
	ns         *grits.NameStore
	dirWatcher *DirWatcher
	mtx        sync.Mutex
}

func (*DirToTreeMirror) Name() string {
	return "dirmirror"
}

type DirToTreeMirrorConfig struct {
	SourceDir      string `json:"SourceDir"`
	DestPath       string `json:"DestPath"`
	DirWatcherPath string `json:"DirWatcherPath"`
}

// General bookkeeping functions

func NewDirToTreeMirror(srcPath string, destPath string, server *Server, dirWatcherPath string, shutdownFunc func()) (*DirToTreeMirror, error) {
	log.Printf("Creating DirToTreeMirror for %s -> %s\n", srcPath, destPath)

	realSrcPath, err := filepath.EvalSymlinks(srcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate source path: %v", err)
	}

	ns, err := grits.EmptyNameStore(server.BlobStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	dt := &DirToTreeMirror{
		server:   server,
		srcPath:  realSrcPath,
		destPath: destPath,
		ns:       ns,
	}
	dt.dirWatcher = NewDirWatcher(dirWatcherPath, realSrcPath, dt, shutdownFunc)

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

func (dt *DirToTreeMirror) Stop() error {
	err := dt.dirWatcher.Stop()
	return err
}

func (dt *DirToTreeMirror) isReadOnly() bool {
	return true
}

// HandleScan processes an individual file update or addition.
func (dt *DirToTreeMirror) HandleScan(filename string) error {
	dt.mtx.Lock()
	defer dt.mtx.Unlock()

	f, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return dt.removeFile(filename)
		}
		return err
	}
	defer f.Close()

	return dt.addOrUpdateFile(filename, f)
}

func (dt *DirToTreeMirror) HandleScanTree(directory string) error {
	// Okay for this one we leverage some of the usefulness of our NameStore primitives

	newDirNs, err := grits.EmptyNameStore(dt.server.BlobStore)
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

		cf, err := dt.server.BlobStore.AddOpenFile(file)
		if err != nil {
			return err
		}
		defer dt.server.BlobStore.Release(cf)

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

	fileAddr, err := grits.NewTypedFileAddrFromString(newDirNs.GetRoot())
	if err != nil {
		return err
	}

	dt.ns.Link(dt.destPath, fileAddr)
	newDirNs.Link("", nil)

	return nil
}

// Interface to NameStore, to make changes to the tree when we need to

func (dt *DirToTreeMirror) addOrUpdateFile(srcPath string, file *os.File) error {
	cf, err := dt.ns.BlobStore.AddOpenFile(file)
	if err != nil {
		return err
	}
	defer dt.ns.BlobStore.Release(cf)

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