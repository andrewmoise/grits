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

func (dt *DirToTreeMirror) Lookup(path string) (*grits.TypedFileAddr, error) {
	node, err := dt.ns.LookupNode(path)
	if err != nil {
		return nil, err
	}

	if node == nil {
		return nil, nil
	} else {
		return node.Address(), nil
	}
}

func (dt *DirToTreeMirror) LookupFull(path string) ([][]string, error) {
	return dt.ns.LookupFull(path)
}

func (dt *DirToTreeMirror) LookupNode(path string) (grits.FileNode, error) {
	return dt.ns.LookupNode(path)
}

func (dt *DirToTreeMirror) Link(path string, addr *grits.TypedFileAddr) error {
	return dt.ns.Link(path, addr)
}

func (dt *DirToTreeMirror) MultiLink(req []*grits.LinkRequest) error {
	return dt.ns.MultiLink(req)
}

func (dt *DirToTreeMirror) ReadFile(addr *grits.TypedFileAddr) (grits.CachedFile, error) {
	return dt.ns.BlobStore.ReadFile(&addr.BlobAddr)
}

func (dt *DirToTreeMirror) AddBlob(path string) (grits.CachedFile, error) {
	return dt.ns.BlobStore.AddLocalFile(path)
}

func (dt *DirToTreeMirror) AddOpenBlob(file *os.File) (grits.CachedFile, error) {
	return dt.ns.BlobStore.AddOpenFile(file)
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
		totalFiles++
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

	emptyDir, err := dt.server.BlobStore.AddDataBlock([]byte("{}"))
	if err != nil {
		return err
	}
	defer emptyDir.Release()

	// Walk through the source directory and put all files into newDirNs
	err = filepath.Walk(scanPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		processedFiles++
		completion := float32(processedFiles) / float32(totalFiles)
		job.SetStage(path)
		job.SetCompletion(completion)

		relPath, err := filepath.Rel(scanPath, path)
		if err != nil {
			return err
		}

		if info.IsDir() {
			//log.Printf("Adding empty dir: %s\n", relPath)

			err = newDirNs.LinkTree(relPath, emptyDir.GetAddress())
			return err
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
		defer cf.Release()

		//log.Printf("Adding relative path: %s -> %s is %s", scanPath, path, relPath)

		err = newDirNs.LinkBlob(relPath, cf.GetAddress(), cf.GetSize())
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error in main scan of %s: %v", scanPath, err)
	}

	var fileAddr *grits.TypedFileAddr
	if processedFiles != 0 {
		fileAddr, err = grits.NewTypedFileAddrFromString(newDirNs.GetRoot())
		if err != nil {
			return err
		}
	} else {
		// If we found *nothing*, then don't even do the empty dir (e.g. deleting a regular file
		//   triggers this branch)
		fileAddr = nil
	}

	log.Printf("We link %s to %s\n", relPath, fileAddr)
	log.Printf("Current root is %s\n", dt.ns.GetRoot())

	dt.mtx.Lock()
	defer dt.mtx.Unlock()

	err = dt.ns.Link(relPath, fileAddr)
	if err != nil {
		return fmt.Errorf("can't do final link for %s: %v", relPath, err)
	}

	return nil
}
