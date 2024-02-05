package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"grits/internal/grits"
)

type WikiBacking struct {
	blobStore  *BlobStore
	currentRev *RevNode // Current revision of the file system
	mutex      sync.RWMutex
	dataFile   string // File path for persisting the WikiBacking data
}

func NewWikiBacking(blobStore *BlobStore, dataFile string) *WikiBacking {
	return &WikiBacking{
		blobStore:  blobStore,
		currentRev: nil, // Start with no revision
		dataFile:   dataFile,
	}
}

// LoadData loads the WikiBacking data from the persisted JSON file.
func (wb *WikiBacking) LoadData() error {
	wb.mutex.Lock()
	defer wb.mutex.Unlock()

	file, err := os.Open(wb.dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // If the file doesn't exist, it's not an error; start with an empty revision.
		}
		return err
	}
	defer file.Close()

	return json.NewDecoder(file).Decode(&wb.currentRev)
}

// SaveData persists the current state of WikiBacking to the JSON file.
func (wb *WikiBacking) SaveData() error {
	wb.mutex.Lock()
	defer wb.mutex.Unlock()

	file, err := os.Create(wb.dataFile)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(wb.currentRev)
}

// CommitChange commits a series of file changes to the WikiBacking, creating a new revision.
// Changes is a map where key is the file name, and value is the path to the new file content
// or nil to indicate deletion.
func (wb *WikiBacking) CommitChange(changes map[string]*grits.CachedFile) error {
	wb.mutex.Lock()
	defer wb.mutex.Unlock()

	// Clone the current tree to a new tree
	newTree := wb.cloneCurrentTree()

	// Apply changes to the new tree
	for name, cachedFile := range changes {
		if cachedFile == nil {
			// Handle deletion
			newTree.RemoveChild(name)
		} else {
			// Handle addition or update
			newNode := NewFileNode(NodeTypeBlob, cachedFile.Address)
			newTree.AddChild(name, newNode)
		}
	}

	// Create a new revision
	newRev := NewRevNode(newTree, wb.currentRev)
	wb.currentRev = newRev

	return wb.SaveData() // Persist changes after modification.
}

// RetrieveFileNode retrieves the FileNode for a given name at the current revision.
func (wb *WikiBacking) RetrieveFileNode(name string) (*FileNode, error) {
	wb.mutex.RLock()
	defer wb.mutex.RUnlock()

	if wb.currentRev == nil {
		return nil, fmt.Errorf("no current revision exists")
	}

	node, exists := wb.currentRev.Tree.GetChild(name)
	if !exists {
		return nil, fmt.Errorf("file with name %s does not exist", name)
	}

	return node, nil
}
