package server

import (
	"fmt"
	"grits/internal/grits"
	"log"
	"sync"
)

// PinConfig represents the configuration for a pin module
type PinConfig struct {
	Volume string `json:"Volume"`
	Path   string `json:"Path"`
}

// PinModule keeps FileNodes in memory by maintaining references to them
type PinModule struct {
	Config *PinConfig
	Server *Server
	Volume Volume

	refCount map[string]int
	mtx      sync.RWMutex

	updateQueue chan updateTask
	quitChan    chan struct{}
	wg          sync.WaitGroup
}

type updateTask struct {
	path     string
	oldValue grits.FileNode
	newValue grits.FileNode
}

// NewPinModule creates a new instance of PinModule
func NewPinModule(server *Server, config *PinConfig) *PinModule {
	pm := &PinModule{
		Config:      config,
		Server:      server,
		refCount:    make(map[string]int),
		updateQueue: make(chan updateTask, 100), // Buffer size can be adjusted
		quitChan:    make(chan struct{}),
	}

	server.AddModuleHook(pm.moduleHook)

	// Add to waitgroup before starting goroutine
	pm.wg.Add(1)

	// Start the worker goroutine
	go pm.processUpdates()

	return pm
}

func (pm *PinModule) processUpdates() {
	defer pm.wg.Done()

	for {
		select {
		case task := <-pm.updateQueue:
			pm.processUpdate(task)
		case <-pm.quitChan:
			return
		}
	}
}

func (pm *PinModule) processUpdate(task updateTask) {
	// Check if we even care about this path
	if task.path != pm.Config.Path {
		return
	}

	pm.mtx.Lock()
	defer pm.mtx.Unlock()

	if task.newValue != nil {
		err := pm.recursiveTake(task.newValue)
		if err != nil {
			log.Printf("Error taking reference to %s: %v", task.path, err)
		}
	}

	if task.oldValue != nil {
		err := pm.recursiveRelease(task.oldValue)
		if err != nil {
			log.Printf("Error releasing reference to %s: %v", task.path, err)
		}
	}
}

// FIXME - we need error returns from the whole module hook thing
func (pm *PinModule) moduleHook(module Module) {
	// Check if it's a volume
	if vol, ok := module.(Volume); ok {
		if vol.GetVolumeName() == pm.Config.Volume {
			vol.RegisterWatcher(pm)
			pm.Volume = vol

			newNode, err := vol.LookupNode(pm.Config.Path)
			if err != nil {
				log.Fatalf("error return from module init: %v", err)
			}
			defer newNode.Release()

			err = pm.recursiveTake(newNode)
			if err != nil {
				log.Fatalf("error taking ref to %s: %v", pm.Config.Path, err)
			}
		}
	}
}

// OnFileTreeChange implements the FileTreeWatcher interface

// The invariant is: refCount[x] equals the number of other nodes also in .refCount that
// have x as a child
//
// Plus one for the root

func (pm *PinModule) OnFileTreeChange(path string, oldValue grits.FileNode, newValue grits.FileNode) error {
	// For debugging purposes, log the notification
	log.Printf("PinModule: Tree change notification for path %s", path)

	if path != pm.Config.Path {
		return nil
	}

	// Enqueue the task
	pm.updateQueue <- updateTask{
		path:     path,
		oldValue: oldValue,
		newValue: newValue,
	}

	return nil
}

func (pm *PinModule) recursiveTake(fn grits.FileNode) error {
	metadataHash := fn.AddressString() // FIXME - transition to metadata addr as we fix the rest

	refCount, exists := pm.refCount[metadataHash]

	if exists {
		if refCount <= 0 {
			return fmt.Errorf("ref count for %s is nonpositive", metadataHash)
		}

		pm.refCount[metadataHash] = refCount + 1
	} else {
		fn.Take()
		pm.refCount[metadataHash] = 1

		children := fn.Children()
		if children == nil {
			return nil
		}

		for _, childMetadataAddr := range children {
			childNode, err := pm.Volume.GetFileNode(childMetadataAddr)
			if err != nil {
				return err
			}
			defer childNode.Release()

			pm.recursiveTake(childNode)
		}
	}

	return nil
}

func (pm *PinModule) recursiveRelease(fn grits.FileNode) error {
	metadataHash := fn.AddressString()

	refCount, exists := pm.refCount[metadataHash]
	if !exists {
		return fmt.Errorf("can't find %s in ref count to release", metadataHash)
	}

	if refCount <= 1 {
		children := fn.Children()
		for _, childMetadataAddr := range children {
			childNode, err := pm.Volume.GetFileNode(childMetadataAddr)
			if err != nil {
				return err
			}
			defer childNode.Release()

			pm.recursiveRelease(childNode)
		}

		fn.Release()
		delete(pm.refCount, metadataHash)
	} else {
		pm.refCount[metadataHash] = refCount - 1
	}

	return nil
}

// Start implements the Module interface
func (pm *PinModule) Start() error {
	log.Printf("PinModule: Starting")
	// Initial setup was done in constructor
	return nil
}

func (pm *PinModule) Stop() error {
	log.Printf("PinModule: Stopping and releasing all pinned nodes")

	// Signal the worker goroutine to exit
	close(pm.quitChan)

	// Wait for the goroutine to actually finish
	pm.wg.Wait()

	// Now proceed with cleanup, knowing the goroutine is done
	pm.mtx.Lock()
	defer pm.mtx.Unlock()

	for metadataHash := range pm.refCount {
		node, err := pm.Volume.GetFileNode(grits.NewBlobAddr(metadataHash))
		if err != nil {
			log.Printf("Error getting node for cleanup: %v", err)
			continue // Skip but continue with others
		}
		node.Release()
	}

	// Clear the map
	pm.refCount = make(map[string]int)
	return nil
}

// GetModuleName implements the Module interface
func (pm *PinModule) GetModuleName() string {
	return "pin"
}
