package server

import (
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// DirToTreeMirror is responsible for mirroring a directory structure to a tree structure in the blob store.
type DirToTreeMirror struct {
	volumeName string
	srcPath    string
	server     *Server
	ns         *grits.NameStore
	dirWatcher *DirWatcher
	mtx        sync.Mutex
}

func (*DirToTreeMirror) GetModuleName() string {
	return "dirmirror"
}

func (dt *DirToTreeMirror) GetVolumeName() string {
	return dt.volumeName
}

func (dt *DirToTreeMirror) GetNameStore() *grits.NameStore {
	return dt.ns
}

type DirToTreeMirrorConfig struct {
	VolumeName string `json:"VolumeName"`
	SourceDir  string `json:"SourceDir"`
}

// General bookkeeping functions

func NewDirToTreeMirror(config *DirToTreeMirrorConfig, server *Server, shutdownFunc func()) (*DirToTreeMirror, error) {
	if len(config.SourceDir) <= 0 {
		return nil, fmt.Errorf("must specify SourceDir for dirmirror %s", config.VolumeName)
	}

	sourceDir := config.SourceDir
	if sourceDir[0] != '/' {
		sourceDir = server.Config.ServerPath(sourceDir)
	}

	log.Printf("Creating DirToTreeMirror for %s -> %s\n", sourceDir, config.VolumeName)

	sourceDir, err := filepath.EvalSymlinks(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate %s: %v", config.SourceDir, err)
	}

	ns, err := grits.EmptyNameStore(server.BlobStore)
	if err != nil {
		return nil, fmt.Errorf("failed to create NameStore: %v", err)
	}

	dt := &DirToTreeMirror{
		volumeName: config.VolumeName,
		srcPath:    sourceDir,
		server:     server,
		ns:         ns,
	}

	dt.dirWatcher = NewDirWatcher(server.Config.DirWatcherPath, sourceDir, dt, shutdownFunc)

	return dt, nil
}

func (dt *DirToTreeMirror) Start() error {
	log.Printf("Starting DirToTreeMirror for %s -> %s\n", dt.srcPath, dt.volumeName)

	err := dt.dirWatcher.Start()
	if err != nil {
		return err
	}

	err = dt.HandleScan(dt.srcPath)
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

func (dt *DirToTreeMirror) Checkpoint() error {
	return nil
}

func (dt *DirToTreeMirror) HandleScan(scanPath string) error {
	log.Printf("HandleScan %s\n", scanPath)

	relPath, err := filepath.Rel(dt.srcPath, scanPath)
	if err != nil {
		return fmt.Errorf("cannot relativize %s: %v", scanPath, err)
	}

	log.Printf("  1 HandleScan %s\n", scanPath)

	// Set up job info, just in case things are complex.

	job := dt.server.CreateJobDescriptor("HandleScan " + scanPath)
	job.SetStage("Initializing")
	defer dt.server.Done(job)
	totalFiles := 0
	processedFiles := 0

	// First pass to count files (simplified for brevity)
	err = filepath.Walk(scanPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalFiles++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error in initial scan of %s: %v", scanPath, err)
	}

	log.Printf("  2 HandleScan %s\n", scanPath)

	newDirNs, err := grits.EmptyNameStore(dt.server.BlobStore)
	if err != nil {
		return err
	}
	defer newDirNs.Link("", nil) // Release references from temp NameStore

	// Walk through the source directory and put all files into newDirNs
	err = filepath.Walk(scanPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		processedFiles++
		completion := float32(processedFiles) / float32(totalFiles)
		job.SetStage(path)
		job.SetCompletion(completion)

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

		relPath, err := filepath.Rel(scanPath, path)
		if err != nil {
			return err
		}

		log.Printf("Adding relative path: %s -> %s is %s", dt.srcPath, path, relPath)

		err = newDirNs.LinkBlob(relPath, cf.Address)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error in main scan of %s: %v", scanPath, err)
	}

	dt.mtx.Lock()
	defer dt.mtx.Unlock()

	fileAddr, err := grits.NewTypedFileAddrFromString(newDirNs.GetRoot())
	if err != nil {
		return err
	}

	log.Printf("We link %s to %s\n", relPath, fileAddr.String())

	err = dt.ns.Link(relPath, fileAddr)
	if err != nil {
		return fmt.Errorf("Can't do final link for %s: %v", scanPath, err)
	}

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

	err = dt.ns.LinkBlob(relPath, cf.Address)
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

	err = dt.ns.LinkBlob(relPath, nil)
	if err != nil {
		return err
	}

	return nil
}
