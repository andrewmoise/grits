package grits

import (
	"encoding/json"
	"os"
	"sync"
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
