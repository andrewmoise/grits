package proxy

import (
	"grits/internal/grits"
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type DirMonitor struct {
	watcher    *fsnotify.Watcher
	fileCache  *FileCache
	dirPath    string
	pathToFile map[string]*grits.CachedFile // Add this line
}

func NewDirMonitor(dirPath string, fileCache *FileCache) *DirMonitor {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Failed to create watcher:", err)
	}

	return &DirMonitor{
		watcher:    watcher,
		fileCache:  fileCache,
		dirPath:    dirPath,
		pathToFile: make(map[string]*grits.CachedFile), // Initialize the map
	}
}

func (dm *DirMonitor) Start() {
	go dm.watch()

	err := dm.watcher.Add(dm.dirPath)
	if err != nil {
		log.Fatal("Failed to add directory to watcher:", err)
	}

	dm.initialScan()
}

// New method for initial directory scan
func (dm *DirMonitor) initialScan() {
	filepath.Walk(dm.dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Println("Error accessing path:", path, "Error:", err)
			return err
		}
		if !info.IsDir() {
			dm.addOrUpdateFile(path)
		}
		return nil
	})
}

func (dm *DirMonitor) watch() {
	for {
		select {
		case event, ok := <-dm.watcher.Events:
			if !ok {
				return
			}
			dm.handleEvent(event)
		case err, ok := <-dm.watcher.Errors:
			if !ok {
				return
			}
			log.Println("Watcher error:", err)
		}
	}
}

func (dm *DirMonitor) handleEvent(event fsnotify.Event) {
	log.Println("Event:", event)
	if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
		dm.addOrUpdateFile(event.Name)
	} else if event.Op&fsnotify.Remove == fsnotify.Remove {
		dm.removeFile(event.Name)
	}
}

func (dm *DirMonitor) addOrUpdateFile(filePath string) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Println("Error stating file:", err)
		return
	}

	if fileInfo.IsDir() {
		return // Skip directories
	}

	// Check if we already have this file in cache and release it if we do
	if existing, ok := dm.pathToFile[filePath]; ok {
		dm.fileCache.Release(existing) // Assume Release decreases RefCount and possibly removes the file
	}

	// Add or update the file in the cache
	cachedFile, err := dm.fileCache.AddLocalFile(filePath)
	if err != nil {
		log.Println("Failed to add or update file in cache:", err)
		return
	}

	// Update the map with the new or updated file
	dm.pathToFile[filePath] = cachedFile

	log.Printf("File added/updated in cache: %s", cachedFile.Path)
}

func (dm *DirMonitor) removeFile(filePath string) {
	// Decrease RefCount or remove the file from the cache if it exists in our map
	if cachedFile, ok := dm.pathToFile[filePath]; ok {
		dm.fileCache.Release(cachedFile) // Assume Release decreases RefCount and possibly removes the file
		delete(dm.pathToFile, filePath)
	}

	log.Printf("File removed: %s", filePath)
}

func (dm *DirMonitor) Stop() {
	dm.watcher.Close()
}
