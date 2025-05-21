package grits

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// namestore.go implements a content-addressable namespace with path-based lookup.
//
// Key structs:
// - FileNode / TreeNode / BlobNode: A file or directory
// - GNodeMetadata: Metadata for a file (including the file's content hash)
// - RefManager: Stops file trees from being garbage collected
// - FileTreeWatcher: Gets notifications when something changed
//
// - NameStore: Main class managing the namespace and operations, main functions:
//   - LookupNode: Get a node at a specific path
//   - LookupFull: Get all nodes along a path
//   - Link/LinkByMetadata: Add or update a path
//   - MultiLink: Update multiple paths atomically or only if conditions are met
//   - CreateTreeNode: Create a new directory node
//
//   - loadFileNode: Load node from storage (internal)
//   - recursiveLink: Core path update logic (internal)
//   - resolvePath: Core path resolution logic (internal)
//
// The system uses reference counting to track when objects can be released. Most
// nodes will have a reference count taken when they are returned, which you must
// release. This means that we can do a full writable "merkle tree" with frequent
// changes while still having safe access if you're holding an older copy of the
// tree, and also reasonable performance and disk consumption (roughly in the
// ballpark of what you'd expect from a normal filesystem, when it is mounted
// via FUSE).

////////////////////////
// NameStore

type NameStore struct {
	BlobStore BlobStore
	root      FileNode
	fileCache map[BlobAddr]FileNode
	mtx       sync.RWMutex

	watchers []FileTreeWatcher
	wmtx     sync.RWMutex // Separate mutex for watchers list

	refManager RefManager

	serialNumber int64 // Increments on every root change
}

func (ns *NameStore) GetRoot() string {
	return ns.root.AddressString()
}

func (ns *NameStore) LookupAndOpen(name string) (CachedFile, error) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	nodes, failureParts, err := ns.resolvePath(name)
	if err != nil {
		return nil, err
	}
	if failureParts != 0 {
		return nil, ErrNotExist
	}

	node := nodes[len(nodes)-1]

	cf := node.ExportedBlob()
	cf.Take()
	return cf, nil
}

func (ns *NameStore) LookupNode(path string) (FileNode, error) {
	if DebugNameStore {
		log.Printf("LookupNode(%s)", path)
	}

	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	nodes, failureParts, err := ns.resolvePath(path)
	if err != nil {
		return nil, err
	}
	if failureParts != 0 {
		return nil, ErrNotExist
	}

	node := nodes[len(nodes)-1]

	node.Take()
	return node, nil
}

// PathNodePair represents a path and its corresponding FileNode
type PathNodePair struct {
	Path string
	Node FileNode
}

// LookupFull returns a list of path and node pairs for a given path or paths

// Second part of the return value indicates whether we got a partial failure

func (ns *NameStore) LookupFull(names []string) ([]*PathNodePair, bool, error) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	seenPaths := make(map[string]bool)
	var response []*PathNodePair
	wasFailure := false

	for _, name := range names {
		name = strings.TrimRight(name, "/")
		nodes, failureParts, err := ns.resolvePath(name)
		if err != nil {
			return nil, false, err
		}
		if failureParts != 0 {
			wasFailure = true
		}
		if len(nodes) <= 0 {
			log.Printf("Can't happen! No nodes returned on lookup of %s", name)
			return nil, false, ErrNotExist
		}

		parts := strings.Split(name, "/")
		partialPath := ""

		if _, exists := seenPaths[""]; !exists {
			response = append(response, &PathNodePair{
				Path: "",
				Node: nodes[0],
			})
			seenPaths[""] = true
		}

		// Add each path component and its corresponding node
		index := 1
		for _, part := range parts {
			if part == "" {
				continue
			}

			if index >= len(nodes) {
				break // Safeguard against potential out-of-bounds
			}

			partialPath = filepath.Join(partialPath, part)
			node := nodes[index]

			if _, exists := seenPaths[partialPath]; !exists {
				response = append(response, &PathNodePair{
					Path: partialPath,
					Node: node,
				})
				seenPaths[partialPath] = true
			}

			index++
		}
	}

	// Take a reference to each node we're returning
	for _, pair := range response {
		pair.Node.Take()
	}

	return response, wasFailure, nil
}

// FIXME - Clean up this API a lot.

// Get a FileNode from a metadata address, either from cache or loaded on demand.
// Takes a reference to the node before returning it.
func (ns *NameStore) GetFileNode(metadataAddr BlobAddr) (FileNode, error) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	node, err := ns.loadFileNode(metadataAddr, true)
	if err != nil {
		return nil, err
	}

	node.Take()
	return node, nil
}

// Core lookup helper function.

// Result is (all FileNodes along path, number of unresolved entries at end, error)

// We can fail early, but still return the partial results. In that case the second part of the
// return value will be nonzero.

func (ns *NameStore) resolvePath(path string) ([]FileNode, int, error) {
	if DebugNameStore {
		log.Printf("We resolve path %s (root %v)\n", path, ns.root)
	}

	path = strings.TrimRight(path, "/")
	if path != "" && path[0] == '/' {
		return nil, -1, fmt.Errorf("paths must be relative")
	}

	parts := strings.Split(path, "/")
	node := ns.root

	if node == nil {
		return nil, -1, fmt.Errorf("looking up in nil root")
	}

	response := make([]FileNode, 0, len(parts)+1) // +1 for the root
	response = append(response, node)

	expectedResponseLen := len(parts) + 1

	for _, part := range parts {
		//log.Printf("  part %s\n", part)

		if part == "" {
			expectedResponseLen -= 1 // Highly unlikely that this is relevant...
			continue
		}

		// Only TreeNodes have children to traverse
		treeNode, isTreeNode := node.(*TreeNode)
		if !isTreeNode {
			return nil, -1, ErrNotDir
		}

		childAddr, exists := treeNode.ChildrenMap[part]
		if !exists {
			// Oop. Early return.
			return response, expectedResponseLen - len(response), nil
		}

		childNode, err := ns.loadFileNode(childAddr, true)
		if err != nil {
			return nil, expectedResponseLen - len(response), err
		}

		node = childNode // Move to the next node in the path
		response = append(response, node)
	}

	return response, 0, nil
}

/////
// Link stuff

const (
	AssertPrevValueMatches = 1 << iota
	AssertIsBlob
	AssertIsTree
	AssertIsNonEmpty
)

type LinkRequest struct {
	Path     string   `json:"path"`               // Path to link
	NewAddr  BlobAddr `json:"addr"`               // New metadata blob address
	PrevAddr BlobAddr `json:"prevAddr,omitempty"` // Optional previous address for assertions
	Assert   uint32   `json:"assert,omitempty"`   // Optional assertion flags
}

func matchesAddr(a FileNode, b BlobAddr) bool {
	if a == nil {
		return b == ""
	} else {
		return b != "" && a.MetadataBlob().GetAddress() == b
	}
}

func (ns *NameStore) MultiLink(requests []*LinkRequest, returnResults bool) ([]*PathNodePair, error) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	for _, req := range requests {
		if DebugLinks {
			log.Printf("Checking assertion: %d", req.Assert)
		}

		if req.Assert == 0 {
			continue
		}

		var node FileNode
		nodes, failureParts, err := ns.resolvePath(req.Path)
		if err != nil {
			return nil, err
		} else if failureParts > 1 {
			// We failed before we got to the dir we're trying to link into
			return nil, ErrNotExist
		} else if failureParts == 1 {
			// We found the parent, but not the requested file, so create it
			node = nil
		} else if failureParts == 0 {
			// We found a previous value, so replace it
			node = nodes[len(nodes)-1]
		} else {
			// Can't happen
			log.Panicf("Weird failure on lookup of %s: %d, %v", req.Path, failureParts, err)
		}

		if req.Assert&AssertPrevValueMatches != 0 {
			if DebugLinks {
				log.Printf("  Prev value matches")
			}
			if !matchesAddr(node, req.PrevAddr) {
				return nil, ErrAssertionFailed
			}
			if DebugLinks {
				log.Printf("  pass")
			}
		}

		if req.Assert&AssertIsBlob != 0 {
			if DebugLinks {
				log.Printf("  Is blob")
			}
			if node == nil || node.Address().Type != Blob {
				return nil, ErrAssertionFailed
			}
			if DebugLinks {
				log.Printf("  pass")
			}
		}

		if req.Assert&AssertIsTree != 0 {
			if DebugLinks {
				log.Printf("  Is tree")
			}
			if node == nil || node.Address().Type != Tree {
				return nil, ErrAssertionFailed
			}
			if DebugLinks {
				log.Printf("  pass")
			}
		}

		if req.Assert&AssertIsNonEmpty != 0 {
			if DebugLinks {
				log.Printf("  Is nonempty")
			}
			if node == nil {
				return nil, ErrAssertionFailed
			}
			if DebugLinks {
				log.Printf("  pass")
			}
		}
	}

	oldRoot := ns.root
	newRoot := ns.root

	for _, req := range requests {
		name := strings.TrimRight(req.Path, "/")
		if name != "" && name[0] == '/' {
			return nil, fmt.Errorf("name must be relative")
		}

		if name == "." {
			name = ""
		}

		var err error
		newRoot, err = ns.recursiveLink("", name, req.NewAddr, newRoot)
		if err != nil {
			return nil, err
		}
	}

	err := ns.notifyWatchers("", oldRoot, newRoot)
	if err != nil {
		return nil, err
	}

	if newRoot != nil {
		//newRoot.Take()
		ns.refManager.recursiveTake(ns, newRoot)
	}

	if ns.root != nil {
		//ns.root.Release()
		ns.refManager.recursiveRelease(ns, ns.root)
	}

	ns.root = newRoot
	ns.serialNumber++

	var response []*PathNodePair
	if returnResults {
		// Now build up the lookup results
		seenPaths := make(map[string]bool)

		// Gather all paths we need to look up
		for _, req := range requests {
			path := strings.TrimRight(req.Path, "/")

			// Skip paths we've already processed
			if _, exists := seenPaths[path]; exists {
				continue
			}

			// Look up this path
			nodes, failureParts, err := ns.resolvePath(path)
			if err != nil {
				return nil, err
			}
			if failureParts != 0 {
				// We don't care about failureParts; it's okay for it to happen but the
				// only way it can happen without some kind of internal error is if one
				// part of the link overwrites an earlier part with nil. It's a litle
				// weird, so we log a warning.
				log.Printf("Found failureParts in MultiLink for %s", path)
			}

			if len(nodes) <= 0 {
				log.Printf("Found <= 0 nodes in MultiLink")
				continue // Path not found... including the root. This definitely seems wrong.
			}

			// Process all path components including parent directories
			parts := strings.Split(path, "/")
			partialPath := ""

			// Add root if we haven't seen it
			if _, exists := seenPaths[""]; !exists {
				response = append(response, &PathNodePair{
					Path: "",
					Node: nodes[0],
				})
				seenPaths[""] = true
			}

			// Add each path component
			index := 1
			for _, part := range parts {
				if part == "" {
					continue
				}

				if index >= len(nodes) {
					break
				}

				partialPath = filepath.Join(partialPath, part)
				node := nodes[index]

				if _, exists := seenPaths[partialPath]; !exists {
					response = append(response, &PathNodePair{
						Path: partialPath,
						Node: node,
					})
					seenPaths[partialPath] = true
				}

				index++
			}
		}

		// Take a reference to each node we're returning
		for _, pair := range response {
			pair.Node.Take()
		}
	} else {
		response = nil
	}

	return response, nil
}

func (ns *NameStore) Link(name string, addr *TypedFileAddr) error {
	if addr == nil {
		return ns.LinkByMetadata(name, "")
	}

	metadataBlob, err := ns.typeToMetadata(addr)
	if err != nil {
		return err
	}
	defer metadataBlob.Release()

	return ns.LinkByMetadata(name, metadataBlob.GetAddress())
}

// FIXME - This is the one we should actually be using, for everything
func (ns *NameStore) LinkByMetadata(name string, metadataAddr BlobAddr) error {
	name = strings.TrimRight(name, "/")
	if name != "" && name[0] == '/' {
		return fmt.Errorf("name must be relative")
	}

	if name == "." {
		name = ""
	}

	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	newRoot, err := ns.recursiveLink("", name, metadataAddr, ns.root)
	if err != nil {
		return err
	}

	err = ns.notifyWatchers("", ns.root, newRoot)
	if err != nil {
		return err
	}

	if newRoot != nil {
		//newRoot.Take()
		ns.refManager.recursiveTake(ns, newRoot)
	}
	if ns.root != nil {
		//ns.root.Release()
		ns.refManager.recursiveRelease(ns, ns.root)
	}

	ns.serialNumber++
	ns.root = newRoot
	return nil
}

// linkBlob creates metadata for a blob and links it into the path
func (ns *NameStore) linkBlob(name string, addr BlobAddr, size int64) error {
	if addr == "" {
		// If addr is "", we're unlinking
		return ns.LinkByMetadata(name, "")
	}

	// Create metadata for the blob
	_, metadataBlob, err := ns.createMetadataBlob(addr, size, false, 0)
	if err != nil {
		return fmt.Errorf("failed to create blob metadata: %v", err)
	}
	defer metadataBlob.Release()

	// Link using the metadata address
	return ns.LinkByMetadata(name, metadataBlob.GetAddress())
}

// linkTree creates metadata for a tree and links it into the path
func (ns *NameStore) linkTree(name string, addr BlobAddr) error {
	if addr == "" {
		// If addr is nil, we're unlinking
		return ns.LinkByMetadata(name, "")
	}

	// For a tree node, we don't have size information readily available
	// We'll need to read the file to get its size
	contentCf, err := ns.BlobStore.ReadFile(addr)
	if err != nil {
		return fmt.Errorf("failed to read tree content: %v", err)
	}
	defer contentCf.Release()

	size := contentCf.GetSize()

	// Create metadata for the tree
	_, metadataBlob, err := ns.createMetadataBlob(addr, size, true, 0)
	if err != nil {
		return fmt.Errorf("failed to create tree metadata: %v", err)
	}
	defer metadataBlob.Release()

	// Link using the metadata address
	return ns.LinkByMetadata(name, metadataBlob.GetAddress())
}

// Core link function helper.

// We link in `metadataAddr` into place as `name` within `oldParent`, and return the
// modified version of `oldParent`.

// Core link function helper
func (ns *NameStore) recursiveLink(prevPath string, name string, metadataAddr BlobAddr, oldParent FileNode) (FileNode, error) {
	if DebugNameStore {
		if metadataAddr != "" {
			log.Printf("We're trying to link %s under path %s /// %s\n", metadataAddr, prevPath, name)
		} else {
			log.Printf("We're trying to link nil under path %s /// %s\n", prevPath, name)
		}
	}

	parts := strings.SplitN(name, "/", 2)

	var currPath string
	if prevPath == "" {
		currPath = parts[0]
	} else {
		currPath = fmt.Sprintf("%s/%s", prevPath, parts[0])
	}

	var oldChildren map[string]BlobAddr
	if oldParent != nil {
		oldChildren = oldParent.Children()
		if oldChildren == nil {
			return nil, ErrNotDir
		}
	} else {
		return nil, ErrNotExist
	}

	var newValue FileNode
	var err error

	oldChildAddr, exists := oldChildren[parts[0]]
	var oldChild FileNode
	if exists {
		oldChild, err = ns.loadFileNode(oldChildAddr, true)
		if err != nil {
			return nil, err
		}
	} else if len(parts) > 1 {
		// Directory doesn't exist while searching down in the path while doing a link
		return nil, ErrNotExist
	}

	if len(parts) == 1 {
		if metadataAddr == "" {
			newValue = nil
		} else {
			newValue, err = ns.loadFileNode(metadataAddr, true)
			if err != nil {
				return nil, err
			}
		}

		if parts[0] == "" {
			return newValue, nil
		}
	} else {
		newValue, err = ns.recursiveLink(currPath, parts[1], metadataAddr, oldChild)
		if err != nil {
			return nil, err
		}
	}

	newChildren := make(map[string]BlobAddr)
	for k, v := range oldChildren {
		newChildren[k] = v
	}

	if newValue != nil {
		newChildren[parts[0]] = newValue.MetadataBlob().GetAddress()
	} else {
		delete(newChildren, parts[0])
	}

	ns.notifyWatchers(currPath, oldChild, newValue)

	result, err := ns.createTreeNode(newChildren, false)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func EmptyNameStore(bs BlobStore) (*NameStore, error) {
	ns := &NameStore{
		BlobStore:  bs,
		fileCache:  make(map[BlobAddr]FileNode),
		refManager: NewDenseRefManager(""),
	}

	rootNodeMap := make(map[string]BlobAddr)
	root, err := ns.CreateTreeNode(rootNodeMap)
	if err != nil {
		return nil, err
	}

	//root.Take()
	ns.refManager.recursiveTake(ns, root)

	//log.Printf("Done setting up root (%s). Ref count: %d / %d",
	//	root.AddressString(), root.refCount, ns.refManager.refCount[BlobAddr(root.AddressString())])

	ns.serialNumber = 0
	ns.root = root
	return ns, nil
}

func (ns *NameStore) GetSerialNumber() int64 {
	return ns.serialNumber
}

func DeserializeNameStore(bs BlobStore, rootAddr *TypedFileAddr, serialNumber int64) (*NameStore, error) {
	ns := &NameStore{
		BlobStore:    bs,
		fileCache:    make(map[BlobAddr]FileNode),
		refManager:   NewDenseRefManager(""),
		serialNumber: serialNumber,
	}

	rootCf, err := ns.typeToMetadata(rootAddr)
	if err != nil {
		return nil, fmt.Errorf("error loading root node: %v", err)
	}
	defer rootCf.Release()

	root, err := ns.loadFileNode(rootCf.GetAddress(), true)
	if err != nil {
		return nil, err
	}

	//root.Take()
	ns.refManager.recursiveTake(ns, root)

	ns.root = root
	return ns, nil
}

// Getting file nodes from a NameStore

// Helper function to create or get a metadata node for a TypedFileAddr
func (ns *NameStore) typeToMetadata(addr *TypedFileAddr) (CachedFile, error) {
	// Create a temporary metadata node for this TypedFileAddr
	var metadata GNodeMetadata
	if addr.Type == Blob {
		metadata = GNodeMetadata{
			Type:        GNodeTypeFile,
			Size:        addr.Size,
			ContentHash: addr.BlobAddr,
		}
	} else {
		metadata = GNodeMetadata{
			Type:        GNodeTypeDirectory,
			Size:        addr.Size,
			ContentHash: addr.BlobAddr,
		}
	}

	// Serialize and store the metadata
	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("error marshalling metadata: %v", err)
	}

	metadataCf, err := ns.BlobStore.AddDataBlock(metadataData)
	if err != nil {
		return nil, fmt.Errorf("error writing metadata: %v", err)
	}
	return metadataCf, nil
}

// Get a file node, return it (same as GetFileNode(), but no reference or lock taken)

func (ns *NameStore) loadFileNode(metadataAddr BlobAddr, printDebug bool) (FileNode, error) {
	if printDebug && DebugFileCache {
		log.Printf("We try to chase down %s\n", metadataAddr)
	}

	cachedNode, exists := ns.fileCache[metadataAddr]
	if exists {
		if printDebug && DebugFileCache {
			log.Printf("  found: %p", cachedNode)
		}
		return cachedNode, nil
	}

	// If not, we have to load it. First load and parse the metadata.

	// FIXME: In a perfect world, we would make sure it's actually a "file not found" error
	// before we report ErrNotInStore
	metadataCf, err := ns.BlobStore.ReadFile(metadataAddr)
	if err != nil {
		return nil, ErrNotInStore
	}

	metadataData, err := metadataCf.Read(0, metadataCf.GetSize())
	if err != nil {
		metadataCf.Release()
		return nil, fmt.Errorf("error reading metadata content: %v", err)
	}

	var metadata GNodeMetadata
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		metadataCf.Release()
		return nil, fmt.Errorf("error parsing metadata: %v", err)
	}

	// Load the content blob
	contentHash := BlobAddr(metadata.ContentHash)
	contentCf, err := ns.BlobStore.ReadFile(contentHash)
	if err != nil {
		metadataCf.Release()
		return nil, ErrNotInStore
	}

	//log.Printf("We got it. The content addr is %s\n", contentHash)

	if metadata.Type == GNodeTypeFile {
		bn := &BlobNode{
			blob:         contentCf,
			metadataBlob: metadataCf,
			metadata:     &metadata,
			refCount:     0,
		}
		if printDebug && DebugFileCache {
			log.Printf("  created blob: %p", bn)
		}

		ns.fileCache[metadataAddr] = bn // Now using metadata addr as cache key
		return bn, nil
	} else {
		ns.fileCache[metadataAddr] = nil

		dn := &TreeNode{
			blob:         contentCf,
			metadataBlob: metadataCf,
			metadata:     &metadata,
			ChildrenMap:  make(map[string]BlobAddr),
			nameStore:    ns,
		}

		if printDebug && DebugFileCache {
			log.Printf("  created tree: %p", dn)
		}

		if DebugRefCounts {
			if printDebug {
				log.Printf("loadFile() creating tree node %p", dn)
			}
			PrintStack()
		}

		defer func() { // In case of error
			if dn != nil {
				delete(ns.fileCache, metadataAddr)
			}
		}()

		dirData, err := contentCf.Read(0, contentCf.GetSize())
		if err != nil {
			return nil, fmt.Errorf("error reading directory data: %v", err)
		}

		//log.Printf("We check contents of %s: %s\n", contentHash, string(dirData))

		dirMap := make(map[string]string)
		if err := json.Unmarshal(dirData, &dirMap); err != nil {
			return nil, fmt.Errorf("error parsing directory: %v", err)
		}

		for name, childMetadataCID := range dirMap {
			dn.ChildrenMap[name] = BlobAddr(childMetadataCID)
		}

		ns.fileCache[metadataAddr] = dn
		if printDebug && DebugFileCache {
			log.Printf("  putting in file cache at %s", metadataAddr)
		}
		resultDn := dn
		dn = nil // Prevent deferred cleanup + removal from file cache
		return resultDn, nil
	}
}

// Helper function to create a timestamp in ISO 8601 format in UTC
func CreateTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// A word about ref counting: This will return a GNodeMetadata, which is not ref-counted, alongside
// a CachedFile with a reference taken. Most commonly, that file will wind up getting assigned to a
// newly-created FileNode of some sort, which will then hold that reference for as long as *it*
// remains un-garbage-collected.

func (ns *NameStore) CreateMetadataBlob(contentHash BlobAddr, size int64, isDir bool, mode uint32) (*GNodeMetadata, CachedFile, error) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	return ns.createMetadataBlob(contentHash, size, isDir, mode)
}

// Create a metadata node with proper mode and timestamps
func (ns *NameStore) createMetadataBlob(contentHash BlobAddr, size int64, isDir bool, mode uint32) (*GNodeMetadata, CachedFile, error) {
	// If mode is 0, set default modes
	if mode == 0 {
		if isDir {
			mode = 0755
		} else {
			mode = 0644
		}
	}

	timestamp := CreateTimestamp()

	var metadata GNodeMetadata
	if isDir {
		metadata = GNodeMetadata{
			Type:        GNodeTypeDirectory,
			Size:        size,
			ContentHash: contentHash,
			Mode:        mode,
			Timestamp:   timestamp,
		}
	} else {
		metadata = GNodeMetadata{
			Type:        GNodeTypeFile,
			Size:        size,
			ContentHash: contentHash,
			Mode:        mode,
			Timestamp:   timestamp,
		}
	}

	// Serialize and store the metadata
	metadataData, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, fmt.Errorf("error marshalling metadata: %v", err)
	}

	metadataCf, err := ns.BlobStore.AddDataBlock(metadataData)
	if err != nil {
		return nil, nil, fmt.Errorf("error writing metadata: %v", err)
	}

	return &metadata, metadataCf, nil
}

func (ns *NameStore) CreateTreeNode(children map[string]BlobAddr) (*TreeNode, error) {
	return ns.createTreeNode(children, true)
}

// Same as CreateTreeNode() but with no locking
func (ns *NameStore) createTreeNode(children map[string]BlobAddr, takeLock bool) (*TreeNode, error) {
	//log.Printf("Creating tree node for map with %d children", len(children))

	//ns.BlobStore.DumpStats()

	tn := &TreeNode{
		ChildrenMap: children,
		nameStore:   ns,
	}

	if DebugRefCounts {
		log.Printf("createTreeNode() Creating tree node %p", tn)
		PrintStack()
	}

	// Create and serialize the directory listing
	dirMap := make(map[string]BlobAddr)
	for name, childAddr := range children {
		dirMap[name] = childAddr
	}

	dirData, err := json.Marshal(dirMap)
	if err != nil {
		return nil, fmt.Errorf("error marshalling directory: %v", err)
	}

	contentBlob, err := ns.BlobStore.AddDataBlock(dirData)
	if err != nil {
		return nil, fmt.Errorf("error writing directory: %v", err)
	}

	metadata, metadataBlob, err := ns.createMetadataBlob(contentBlob.GetAddress(), contentBlob.GetSize(), true, 0)
	if err != nil {
		contentBlob.Release()
		return nil, fmt.Errorf("error creating metadata: %v", err)
	}

	//log.Printf("Added metadata blob, addr %s, rc %d", metadataBlob.GetAddress(), metadataBlob.GetRefCount())

	tn.blob = contentBlob
	tn.metadata = metadata
	tn.metadataBlob = metadataBlob

	if takeLock {
		ns.mtx.Lock()
		defer ns.mtx.Unlock()
	}

	existingNode, exists := ns.fileCache[tn.metadataBlob.GetAddress()]
	if exists {
		contentBlob.Release()
		metadataBlob.Release()

		existingTreeNode, ok := existingNode.(*TreeNode)
		if !ok {
			return nil, fmt.Errorf("non tree node found in file cache for tree node %s", tn.metadataBlob.GetAddress())
		}

		return existingTreeNode, nil
	} else {
		ns.fileCache[tn.metadataBlob.GetAddress()] = tn
		return tn, nil
	}

	// We do not release the cachedFiles for the content or metadata. Ownership of them has been
	// taken over by the FileNode, at this point.
}

func (tn *TreeNode) ensureSerialized() error {
	if tn.blob != nil {
		return nil
	}

	// Directory format is now filename => metadata CID
	dirMap := make(map[string]BlobAddr)
	for name, childAddr := range tn.Children() {
		dirMap[name] = childAddr
	}

	dirData, err := json.Marshal(dirMap)
	if err != nil {
		return fmt.Errorf("error marshalling directory: %v", err)
	}

	cf, err := tn.nameStore.BlobStore.AddDataBlock(dirData)
	if err != nil {
		return fmt.Errorf("error writing directory: %v", err)
	}

	tn.blob = cf
	return nil
}

func (ns *NameStore) DumpFileCache() error {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	log.Printf("=== File Cache Contents ===")
	for cid, node := range ns.fileCache {
		log.Printf("CID: %s", cid)
		if node == nil {
			log.Printf("  Value: nil")
			continue
		}

		// Get the content blob
		blob := node.ExportedBlob()
		if blob == nil {
			log.Printf("  Value: <no blob>")
			continue
		}

		// Read and print the content
		data, err := blob.Read(0, blob.GetSize())
		if err != nil {
			return fmt.Errorf("error reading blob for %s: %v", cid, err)
		}
		log.Printf("  Value: %s", string(data))

		log.Printf("  Node RefCount: %d", node.RefCount())

		// Print ref counts by casting to LocalCachedFile
		if lcf, ok := blob.(*LocalCachedFile); ok {
			log.Printf("  Blob RefCount: %d", lcf.RefCount)
		}

		if metadata := node.MetadataBlob(); metadata != nil {
			if lcf, ok := metadata.(*LocalCachedFile); ok {
				log.Printf("  Metadata RefCount: %d", lcf.RefCount)
			}
		}
	}
	log.Printf("=== End File Cache ===")
	return nil
}

func (ns *NameStore) DumpTree() {
	log.Printf("=== Name Store Tree Dump ===")
	ns.dumpTreeNode("", ns.root, "(root)")
	log.Printf("=== End Tree Dump ===")
}

func (ns *NameStore) dumpTreeNode(indent string, node FileNode, name string) {
	if node == nil {
		log.Printf("%s%s: <nil>", indent, name)
		return
	}

	contentBlob := node.ExportedBlob()
	metadataBlob := node.MetadataBlob()

	contentLcf, _ := contentBlob.(*LocalCachedFile)
	metadataLcf, _ := metadataBlob.(*LocalCachedFile)

	var contentStr string
	if contentBlob.GetSize() <= 200 {
		data, err := contentBlob.Read(0, contentBlob.GetSize())
		if err != nil {
			contentStr = fmt.Sprintf("<error reading: %v>", err)
		} else {
			contentStr = string(data)
		}
	} else {
		contentStr = fmt.Sprintf("<data length: %d>", contentBlob.GetSize())
	}

	log.Printf("%s%s[%d][%d]: %s",
		indent,
		name,
		metadataLcf.RefCount,
		contentLcf.RefCount,
		contentStr)

	// If this is a tree node, recursively print children
	if children := node.Children(); children != nil {
		// Sort children names for consistent output
		names := make([]string, 0, len(children))
		for name := range children {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, childName := range names {
			childAddr, exists := children[childName]
			if !exists {
				log.Panicf("couldn't find %s", childName)
			}

			childNode, err := ns.loadFileNode(childAddr, true)
			if err != nil {
				log.Printf("couldn't load %s: %v", childAddr, err)
				continue
			}

			ns.dumpTreeNode(indent+"  ", childNode, childName)
		}
	}
}

/////
// Debug stuff

// DebugReferenceCountsRecursive walks the entire namespace tree and prints reference count information
// for all nodes, while also identifying orphaned blobs
func (ns *NameStore) PrintBlobStorageDebugging() error {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	// Map to track which blobs we've seen
	seenBlobs := make(map[BlobAddr]bool)

	log.Println("=== Reference Count Debugging ===")
	log.Println("Root node:", ns.GetRoot())

	// Start recursive walk from root
	ns.debugRefCountsWalk("", ns.root, seenBlobs)

	// Now check for orphaned blobs
	log.Println("\n=== Orphaned Blobs ===")

	// Get all blobs from BlobStore
	if localBS, ok := ns.BlobStore.(*LocalBlobStore); ok {
		localBS.mtx.RLock()
		defer localBS.mtx.RUnlock()

		orphanCount := 0
		maxOrphans := 10
		totalSize := int64(0)

		for hash, file := range localBS.files {
			if !seenBlobs[hash] {
				orphanCount++
				totalSize += file.Size
				if orphanCount >= maxOrphans {
					continue
				}

				if true || VerboseDebugBlobStorage {
					log.Printf("Orphaned blob: %s\n", hash)
					log.Printf("  Size: %d bytes\n", file.Size)
					log.Printf("  RefCount: %d\n", file.RefCount)
					log.Printf("  Path: %s\n", file.Path)

					// For small blobs, print content for debugging
					if file.Size <= 200 {
						data, err := file.Read(0, file.Size)
						if err != nil {
							log.Printf("  Contents: <error reading: %v>\n", err)
						} else {
							log.Printf("  Contents: %s\n", string(data))
						}
					}
					log.Println()
				}
			}
		}

		if orphanCount >= maxOrphans {
			log.Printf("  (%d orphans not shown)", orphanCount-maxOrphans+1)
		}
		log.Printf("Total orphaned blobs: %d (%.2f MB)\n", orphanCount, float64(totalSize)/1024/1024)
	} else {
		log.Println("BlobStore is not a LocalBlobStore, cannot check for orphaned blobs")
	}

	log.Println("=== End Reference Count Debugging ===")

	return nil
}

// Helper function to recursively walk the tree
func (ns *NameStore) debugRefCountsWalk(path string, node FileNode, seenBlobs map[BlobAddr]bool) {
	if node == nil {
		// ??? Can't happen
		log.Printf("%s: <nil>\n", path)
		return
	}

	contentBlob := node.ExportedBlob()
	metadataBlob := node.MetadataBlob()

	// Mark these blobs as seen
	seenBlobs[contentBlob.GetAddress()] = true
	seenBlobs[metadataBlob.GetAddress()] = true

	// Get reference counts
	var contentRefCount, metadataRefCount int

	if lcf, ok := contentBlob.(*LocalCachedFile); ok {
		contentRefCount = lcf.RefCount
	}

	if lcf, ok := metadataBlob.(*LocalCachedFile); ok {
		metadataRefCount = lcf.RefCount
	}

	// Print node info
	if VerboseDebugBlobStorage {
		log.Printf("Path: %s\n", path)
		log.Printf("  Node Type: %T\n", node)
		log.Printf("  Node RefCount: %d\n", node.RefCount())
		log.Printf("  Content Blob Hash: %s\n", contentBlob.GetAddress())
		log.Printf("  Content Blob RefCount: %d\n", contentRefCount)
		log.Printf("  Metadata Blob Hash: %s\n", metadataBlob.GetAddress())
		log.Printf("  Metadata Blob RefCount: %d\n", metadataRefCount)

		// Print small blob contents
		if contentBlob.GetSize() <= 200 {
			data, err := contentBlob.Read(0, contentBlob.GetSize())
			if err != nil {
				log.Printf("  Contents: <error reading: %v>\n", err)
			} else {
				log.Printf("  Contents: %s\n", string(data))
			}
		}

		log.Println()
	}

	// Recursively process children if this is a directory
	if children := node.Children(); children != nil {
		// Sort children names for consistent output
		names := make([]string, 0, len(children))
		for name := range children {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, childName := range names {
			childAddr := children[childName]
			childNode, err := ns.loadFileNode(childAddr, false)
			if err != nil {
				log.Printf("  ERROR loading child %s: %v\n", childName, err)
				continue
			}

			childPath := path
			if childPath == "" {
				childPath = childName
			} else {
				childPath = childPath + "/" + childName
			}

			ns.debugRefCountsWalk(childPath, childNode, seenBlobs)
		}
	}
}

func (ns *NameStore) DebugDumpNamespace() {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	log.Println("=== NAMESPACE DUMP ===")
	log.Printf("Root node: %s (%p)\n\n", ns.GetRoot(), ns.root)

	if ns.root == nil {
		log.Println("Root is nil, nothing to dump")
		return
	}

	// Start recursive DFS from the root
	ns.debugDumpNode("", ns.root)

	log.Println("=== END NAMESPACE DUMP ===")
}

func (ns *NameStore) debugDumpNode(path string, node FileNode) {
	if node == nil {
		log.Printf("%s: <nil>\n", path)
		return
	}

	// Get blobs and metadata
	contentBlob := node.ExportedBlob()
	metadataBlob := node.MetadataBlob()

	// Print node details
	log.Printf("%s\n", path)
	log.Printf("  Memory address: %p\n", node)
	log.Printf("  Node type: %T\n", node)

	// Print reference counts
	//nodeRefCount := node.RefCount()
	//refManagerRefCount := 0
	//if metadataHash := metadataBlob.GetAddress(); metadataHash != "" {
	//	refManagerRefCount = ns.refManager.refCount[metadataHash]
	//}
	//log.Printf("  Reference count: %d (object) / %d (refManager)\n", nodeRefCount, refManagerRefCount)

	// Print blob hashes
	log.Printf("  Content blob hash: %s\n", contentBlob.GetAddress())
	log.Printf("  Metadata blob hash: %s\n", metadataBlob.GetAddress())

	// For TreeNodes, print child count
	if children := node.Children(); children != nil {
		log.Printf("  Child count: %d\n", len(children))

		// Sort children names for consistent output
		names := make([]string, 0, len(children))
		for name := range children {
			names = append(names, name)
		}
		sort.Strings(names)

		// Print child names
		if len(names) > 0 {
			log.Println("  Children:")
			for _, childName := range names {
				log.Printf("  - %s\n", childName)
			}
		}

		// Recursively process each child
		for _, childName := range names {
			childAddr := children[childName]
			childNode, err := ns.loadFileNode(childAddr, false)
			if err != nil {
				log.Printf("  ERROR loading child %s: %v\n", childName, err)
				continue
			}

			childPath := path
			if childPath == "" {
				childPath = childName
			} else {
				childPath = childPath + "/" + childName
			}

			ns.debugDumpNode(childPath, childNode)
		}
	} else {
		// For BlobNodes, print a bit of content for small files
		if contentBlob.GetSize() <= 200 {
			data, err := contentBlob.Read(0, contentBlob.GetSize())
			if err != nil {
				log.Printf("  Content preview: <error reading: %v>\n", err)
			} else {
				log.Printf("  Content preview: %s\n", string(data))
			}
		} else {
			log.Printf("  Content too large to preview (%d bytes)\n", contentBlob.GetSize())
		}
	}
}

// CleanupUnreferencedNodes removes all zero-reference nodes from the fileCache
// and releases their underlying storage.
func (ns *NameStore) CleanupUnreferencedNodes() {
	ns.refManager.cleanup(ns)
}

//////////////////////////////////////////////
// Various non-NameStore-struct things, support structures
//////////////////////////////////////////////

////////////////////////
// Node Types

type GNodeType int

const (
	GNodeTypeFile GNodeType = iota
	GNodeTypeDirectory
)

// MarshalJSON converts a GNodeType to a JSON string
func (t GNodeType) MarshalJSON() ([]byte, error) {
	switch t {
	case GNodeTypeFile:
		return []byte(`"blob"`), nil
	case GNodeTypeDirectory:
		return []byte(`"dir"`), nil
	default:
		return nil, fmt.Errorf("unknown GNodeType: %d", t)
	}
}

// UnmarshalJSON converts a JSON string to a GNodeType
func (t *GNodeType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Try as a number for backward compatibility
		var i int
		if err := json.Unmarshal(data, &i); err != nil {
			return err
		}
		*t = GNodeType(i)
		return nil
	}

	switch s {
	case "blob":
		*t = GNodeTypeFile
	case "dir":
		*t = GNodeTypeDirectory
	default:
		return fmt.Errorf("unknown GNodeType string: %s", s)
	}
	return nil
}

type GNodeMetadata struct {
	Type        GNodeType `json:"type"`
	Size        int64     `json:"size"`
	ContentHash BlobAddr  `json:"contentHash"`
	Mode        uint32    `json:"mode,omitempty"`      // File mode (permissions)
	Timestamp   string    `json:"timestamp,omitempty"` // Last modification time (UTC ISO format)
}

type FileNode interface {
	ExportedBlob() CachedFile
	MetadataBlob() CachedFile
	Metadata() *GNodeMetadata
	Children() map[string]BlobAddr
	AddressString() string
	Address() *TypedFileAddr

	Take()
	Release()
	RefCount() int
}

type TreeNode struct {
	blob         CachedFile
	metadataBlob CachedFile
	metadata     *GNodeMetadata
	ChildrenMap  map[string]BlobAddr
	refCount     int
	nameStore    *NameStore
	mtx          sync.Mutex
}

type BlobNode struct {
	blob         CachedFile
	metadataBlob CachedFile
	metadata     *GNodeMetadata
	refCount     int
	mtx          sync.Mutex
}

// Implementations for BlobNode

func (bn *BlobNode) ExportedBlob() CachedFile {
	return bn.blob
}

func (bn *BlobNode) MetadataBlob() CachedFile {
	return bn.metadataBlob
}

func (bn *BlobNode) Metadata() *GNodeMetadata {
	return bn.metadata
}

func (bn *BlobNode) Children() map[string]BlobAddr {
	return nil
}

// Still maintaining TypedFileAddr compatibility for external APIs
func (bn *BlobNode) AddressString() string {
	return fmt.Sprintf("blob:%s-%d", bn.blob.GetAddress(), bn.blob.GetSize())
}

func (bn *BlobNode) Address() *TypedFileAddr {
	return NewTypedFileAddr(bn.blob.GetAddress(), bn.blob.GetSize(), Blob)
}

func (bn *BlobNode) Take() {
	bn.mtx.Lock()
	defer bn.mtx.Unlock()

	if DebugRefCounts {
		log.Printf("TAKE: %s->%s %p (count: %d)",
			bn.metadataBlob.GetAddress(),
			bn.AddressString(),
			bn,
			bn.refCount+1)
		PrintStack()
	}

	bn.refCount++
}

func (bn *BlobNode) Release() {
	bn.mtx.Lock()
	defer bn.mtx.Unlock()

	if DebugRefCounts {
		log.Printf("RELEASE: %s->%s %p (count: %d)",
			bn.metadataBlob.GetAddress(),
			bn.AddressString(),
			bn,
			bn.refCount-1)
		PrintStack()
	}

	bn.refCount--
	if bn.refCount < 0 {
		PrintStack()
		log.Fatalf("Reduced ref count for %s to < 0", bn.metadataBlob.GetAddress())
	}

	// This is where we used to release the actual storage; now we're not doing that until
	// deferred cleanup
}

// FIXME - Maybe audit the callers of this, make sure they are synchronized WRT things that
// might cause take/release of references
func (bn *BlobNode) RefCount() int {
	return bn.refCount
}

// Implementations for TreeNode

func (tn *TreeNode) ExportedBlob() CachedFile {
	err := tn.ensureSerialized()
	if err != nil {
		// FIXME -- need better handling
		log.Printf("Error! Deserializing %p, got %v", tn, err)
		return nil
	}

	return tn.blob
}

func (tn *TreeNode) MetadataBlob() CachedFile {
	return tn.metadataBlob
}

func (tn *TreeNode) Metadata() *GNodeMetadata {
	tn.ensureSerialized()
	return tn.metadata
}

func (tn *TreeNode) Children() map[string]BlobAddr {
	return tn.ChildrenMap
}

// Still maintaining TypedFileAddr compatibility for external APIs
func (tn *TreeNode) Address() *TypedFileAddr {
	tn.ensureSerialized()
	return NewTypedFileAddr(tn.blob.GetAddress(), tn.blob.GetSize(), Tree)
}

func (tn *TreeNode) AddressString() string {
	tn.ensureSerialized()
	return fmt.Sprintf("tree:%s-%d", tn.blob.GetAddress(), tn.blob.GetSize())
}

func (tn *TreeNode) Take() {
	tn.mtx.Lock()
	defer tn.mtx.Unlock()

	if DebugRefCounts {
		log.Printf("TAKE: %s %p (count: %d)",
			tn.metadataBlob.GetAddress(),
			tn,
			tn.refCount+1)
		PrintStack()
	}

	tn.refCount++
}

func (tn *TreeNode) Release() {
	tn.mtx.Lock()
	defer tn.mtx.Unlock()

	if DebugRefCounts {
		log.Printf("RELEASE: %s %p (count: %d)",
			tn.metadataBlob.GetAddress(),
			tn,
			tn.refCount-1)
		PrintStack()
	}

	tn.refCount--
	if tn.refCount < 0 {
		log.Fatalf("Reduced ref count for %s to < 0", tn.metadataBlob.GetAddress())
	}

	// This is where we used to release the actual storage; now we do not do that.
}

// FIXME - Maybe audit the callers of this, make sure they are synchronized WRT things that
// might cause take/release of references
func (tn *TreeNode) RefCount() int {
	return tn.refCount
}

func (ns *NameStore) DebugPrintTree(node FileNode) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	ns.debugPrintTree(node, "")
}

func (ns *NameStore) debugPrintTree(node FileNode, indent string) {
	if node == nil {
		return
	}

	children := node.Children()
	if children == nil {
		return
	}

	for _, childAddr := range children {
		childNode, err := ns.loadFileNode(childAddr, true)
		if err != nil {
			log.Printf("couldn't load %s: %v", childAddr, err)
			continue
		}
		ns.debugPrintTree(childNode, indent+"  ")
	}
}

////////////////////////
// Error sentinels

// Nonexistent file
var ErrNotExist = errors.New("file does not exist")

func IsNotExist(err error) bool {
	return errors.Is(err, ErrNotExist)
}

// Assertion failures in MultiLink operations
var ErrAssertionFailed = errors.New("assertion conditions not satisfied")

func IsAssertionFailed(err error) bool {
	return errors.Is(err, ErrAssertionFailed)
}

// Path traversal hits a non-directory component
var ErrNotDir = errors.New("path component is not a directory")

func IsNotDir(err error) bool {
	return errors.Is(err, ErrNotDir)
}

// Blob to support this data is not found locally
var ErrNotInStore = errors.New("blob not found in local store")

func IsNotInStore(err error) bool {
	return errors.Is(err, ErrNotInStore)
}

/////
// Watch and notification interface

type FileTreeWatcher interface {
	// OnFileTreeChange is called whenever a path in the tree changes
	OnFileTreeChange(path string, oldValue FileNode, newValue FileNode) error
}

// RegisterWatcher adds a watcher to be notified of tree changes
func (ns *NameStore) RegisterWatcher(watcher FileTreeWatcher) {
	ns.wmtx.Lock()
	defer ns.wmtx.Unlock()

	for _, w := range ns.watchers {
		if w == watcher {
			return
		}
	}

	ns.watchers = append(ns.watchers, watcher)
}

// UnregisterWatcher removes a watcher from notification list
func (ns *NameStore) UnregisterWatcher(watcher FileTreeWatcher) {
	ns.wmtx.Lock()
	defer ns.wmtx.Unlock()

	for i, w := range ns.watchers {
		if w == watcher {
			// Remove by replacing with last element and truncating
			ns.watchers[i] = ns.watchers[len(ns.watchers)-1]
			ns.watchers = ns.watchers[:len(ns.watchers)-1]
			break
		}
	}
}

// notifyWatchers sends event to all registered watchers
func (ns *NameStore) notifyWatchers(path string, oldValue FileNode, newValue FileNode) error {
	ns.wmtx.RLock()
	watchers := make([]FileTreeWatcher, len(ns.watchers))
	copy(watchers, ns.watchers) // Copy to avoid holding lock during callbacks
	ns.wmtx.RUnlock()

	// Notify each watcher
	for _, watcher := range watchers {
		err := watcher.OnFileTreeChange(path, oldValue, newValue)
		if err != nil {
			return err
		}
	}

	return nil
}

/////
// RefManagers

type RefManager interface {
	recursiveTake(ns *NameStore, fn FileNode) error
	recursiveRelease(ns *NameStore, fn FileNode) error
	cleanup(ns *NameStore)
}

type DenseRefManager struct {
	path     string
	refCount map[BlobAddr]int
}

func NewDenseRefManager(path string) *DenseRefManager {
	return &DenseRefManager{
		path:     path,
		refCount: make(map[BlobAddr]int),
	}
}

func (pm *DenseRefManager) recursiveTake(ns *NameStore, fn FileNode) error {
	if DebugRefCounts {
		log.Printf("Recursive take on %s %p: count %d/%d", fn.MetadataBlob().GetAddress(), fn, fn.RefCount(), pm.refCount[BlobAddr(fn.AddressString())])
	}

	metadataHash := fn.MetadataBlob().GetAddress()

	refCount, exists := pm.refCount[metadataHash]

	if exists {
		if DebugRefCounts {
			log.Printf("  already exists")
		}
		if refCount <= 0 {
			log.Fatalf("ref count for %s is nonpositive", metadataHash)
		}

		pm.refCount[metadataHash] = refCount + 1
		if DebugRefCounts {
			log.Printf("Increment count! For %s, we go to %d", metadataHash, refCount+1)
		}
	} else {
		if DebugRefCounts {
			log.Printf("  doesn't exist")
		}

		fn.Take()
		pm.refCount[metadataHash] = 1

		children := fn.Children()
		if children == nil {
			return nil
		}

		for _, childMetadataAddr := range children {
			childNode, err := ns.loadFileNode(childMetadataAddr, true)
			if err != nil {
				log.Printf("Can't happen! Can't load %s in dense ref manager.", childMetadataAddr)
				return err
			}

			pm.recursiveTake(ns, childNode)
		}
	}

	return nil
}

func (pm *DenseRefManager) recursiveRelease(ns *NameStore, fn FileNode) error {
	metadataHash := fn.MetadataBlob().GetAddress()
	if DebugRefCounts {
		log.Printf("Recursive release on %s: count %d/%d", fn.AddressString(), fn.RefCount(), pm.refCount[metadataHash])
	}

	refCount, exists := pm.refCount[metadataHash]
	if !exists {
		log.Fatalf("can't find %s in ref count to release", metadataHash)
	}

	if refCount <= 0 {
		log.Fatalf("Releasing 0-reference node")
	} else if refCount == 1 {
		children := fn.Children()
		for _, childMetadataAddr := range children {
			childNode, err := ns.loadFileNode(childMetadataAddr, true)
			if err != nil {
				log.Printf("Can't happen! Can't load %s in dense ref manager.", childMetadataAddr)
				return err
			}

			pm.recursiveRelease(ns, childNode)
		}

		fn.Release()
		delete(pm.refCount, metadataHash)
	} else {
		pm.refCount[metadataHash] = refCount - 1
	}

	return nil
}

func (rm *DenseRefManager) cleanup(ns *NameStore) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	//log.Printf("Starting cleanup of unreferenced nodes")
	nodesToRemove := make([]BlobAddr, 0)

	// First, identify all nodes with zero references
	for metadataAddr, node := range ns.fileCache {
		// Check if the node has zero references
		if node.RefCount() == 0 {
			// Double check it's not in the RefManager structure
			metadataHash := node.MetadataBlob().GetAddress()
			_, exists := rm.refCount[metadataHash]
			if exists {
				log.Panicf("Node %s has 0 refCount but exists in RefManager structure", metadataHash)
				continue
			}

			nodesToRemove = append(nodesToRemove, metadataAddr)
		}
	}

	// Now remove the nodes and release their storage
	for _, metadataAddr := range nodesToRemove {
		node := ns.fileCache[metadataAddr]
		//log.Printf("Removing unreferenced node %p with metadata %s", node, metadataAddr)

		// Release the underlying storage
		contentBlob := node.ExportedBlob()
		metadataBlob := node.MetadataBlob()

		if contentBlob != nil {
			contentBlob.Release()
		}

		if metadataBlob != nil {
			metadataBlob.Release()
		}

		// Remove from cache
		delete(ns.fileCache, metadataAddr)
	}

	if DebugBlobStorage {
		log.Printf("NS cleanup complete. Removed %d unreferenced nodes", len(nodesToRemove))
	}
}

////////////////////////
// Internal notes for API transition / cleanup:

// Link() can start to take a FileNode as the target, instead of an address. If you want to link
// by address, you need to fetch the FileNode for that address, then do your Link(), then release
// the ref count after.

// Same for MultiLink().

// LinkBlob() and LinkTree() should go away. What that should look like instead is a
// helper method that constructs a metadata node for a given blob or tree, and then another thing
// that gives you the FileNode for the metadata you constructed. This stuff shouldn't really be
// needed but there are places where I think we're doing it for compatibility. (Ugh - we don't even
// use it outside of tests. Okay, whatever, it can stay, maybe uncapitalized, and help keep the
// tests running but be deprecated for everything else, maybe even become helper methods within
// the test scaffold. It shouldn't be a main interface.)
//
// recursiveLink() can work in exactly the same fashion with mutable nodes as with immutable
// ones. It's just going to be winding up making mutable copies of any immutable stuff it finds,
// or modifying in-place any mutable stuff it finds and then returning it unchanged. We just have
// to keep our invariant that if a mutable node ever gets a reference count taken (taking its
// refCount to 2), it needs to become immutable before returning. That means it's being linked
// in two places and the second one shouldn't change because the first did.

// LookupAndOpen() should go away I think. We should be able to Open() and do I/O on the file
// nodes directly, since they are getting more capable and stateful now.

// LookupNode() is perfect, no change

// Likewise resolvePath() is already converted, nothing to do for now.
