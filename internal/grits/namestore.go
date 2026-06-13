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
// LOCKING STRATEGY:
// - ns.mtx (RWMutex): Protects fileCache, rootAddr, serialNumber
//   Used with brief locks for cache access and root snapshots
// - ns.writeMtx (Mutex): Serializes all link/write operations
//   Held for the duration of link operations to ensure atomicity
// - Read operations (resolvePath, loadFileNode) are mostly lock-free:
//   They snapshot the root, take a reference, then traverse without locks
//
// Key structs:
// - FileNode / TreeNode / BlobNode: A file or directory
// - GNodeMetadata: Metadata for a file (including the file's content hash)
// - RefManager: Stops file trees from being garbage collected
// - FileTreeWatcher: Gets notifications when something changed
//
// - NameStore: Main class managing the namespace and operations, main functions:
//   - Lookup: Get all nodes along a path
//   - LookupNode: Get a node at a specific path
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
// Interface stuff

// fileCacheEntry tracks both the node and whether a fetch is in progress
type fileCacheEntry struct {
	node     FileNode
	inFlight bool
}

type NameStore struct {
	BlobStore BlobStore
	rootAddr  BlobAddr
	fileCache map[BlobAddr]*fileCacheEntry

	mtx       sync.Mutex   // Protects fileCache, rootAddr, serialNumber
	cacheCond *sync.Cond   // Broadcast when in-flight stuff in the cache gets updated
	writeMtx  sync.RWMutex // Serializes all link/write operations, as well as ref manager structure access

	watchers []FileTreeWatcher
	wmtx     sync.RWMutex   // Separate mutex for watchers list
	wgroup   sync.WaitGroup // Tracks in-flight notifications

	refManager RefManager

	serialNumber int64 // Increments on every root change

	fetchers      []BlobFetcher
	fetcherMtx    sync.RWMutex
	lastFetch     time.Time
	CacheDuration time.Duration

	lookupCallbacks []LookupCallback
	linkCallbacks   []LinkCallback

	DebugWhitelist []string // If non-empty and checkAccess is true, restricts visible paths
}

// BlobFetcher provides on-demand fetching of blobs not available locally
type BlobFetcher interface {
	// FetchBlob retrieves a single blob by address
	FetchBlob(addr BlobAddr) (CachedFile, error)

	// FetchPath retrieves path lookup information and all intermediate nodes
	// Returns the lookup response which includes all nodes along the path
	FetchPath(path string) (*LookupResponse, error)
}

func (ns *NameStore) GetRoot() string {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()
	return string(ns.rootAddr)
}

func (ns *NameStore) LookupAndOpen(name string) (CachedFile, error) {
	lookupResp, err := ns.resolvePath(name, nil)
	if err != nil {
		return nil, err
	}
	leaf := lookupResp.Leaf()
	if leaf == nil || leaf.Error != "" {
		return nil, ErrNotExist
	}

	node, err := ns.loadFileNode(leaf.Addr, true)
	if err != nil {
		return nil, err
	}

	cf, err := node.ExportedBlob()
	if err != nil {
		return nil, err
	}

	cf.Take()
	return cf, nil
}

func (ns *NameStore) LookupNode(path string, principal *Principal) (FileNode, error) {
	path = strings.TrimRight(path, "/") // FIXME
	resp, err := ns.Lookup([]string{path}, "", nil, principal)
	if err != nil {
		return nil, err
	}
	leaf := resp.Leaf()
	if leaf == nil {
		return nil, fmt.Errorf("LookupNode: no paths returned for %q", path)
	}
	if leaf.Error == "not_found" {
		return nil, ErrNotExist
	}
	if leaf.Error == "access_denied" {
		return nil, &ErrAccessDenied{Path: leaf.Path}
	}
	if leaf.Error != "" {
		return nil, fmt.Errorf("LookupNode: unexpected error at %q: %s", leaf.Path, leaf.Error)
	}
	node, err := ns.loadFileNode(leaf.Addr, true)
	if err != nil {
		return nil, err
	}
	node.Take()
	return node, nil
}

// PathNodePair represents a path and its corresponding address, or an error
// for that path. Addr/ContentHash/Size are populated on success; Error is
// populated (and Addr is empty) when the path could not be resolved.
//
// Error values: "not_found", "not_dir", "access_denied", "internal"
type PathNodePair struct {
	Path        string   `json:"path"`
	Addr        BlobAddr `json:"addr,omitempty"`
	ContentHash BlobAddr `json:"contentHash,omitempty"`
	Size        int64    `json:"size,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// LookupResponse represents a full response to a lookup request.
// Every requested path has an entry in Paths, either with Addr set
// (success) or Error set (failure). Ancestor entries always have Addr set.
type LookupResponse struct {
	Paths        []*PathNodePair `json:"paths"`
	SerialNumber int64           `json:"serialNumber"`
}

// Leaf returns the last entry in Paths, which corresponds to the deepest
// path that was resolved. Returns nil if Paths is empty.
func (r *LookupResponse) Leaf() *PathNodePair {
	if len(r.Paths) == 0 {
		return nil
	}
	return r.Paths[len(r.Paths)-1]
}

// RefHoldFunc is an optional callback passed to Lookup. If non-nil it is called
// with the metadata address of the root node that was actually used for the
// lookup (the volume root when startAddr is "", otherwise startAddr itself).
// The call is made before Lookup returns, while the node is still reachable,
// so the callback can safely snag a timed reference via a refHolder.
type RefHoldFunc func(addr BlobAddr)

// Principal represents the entity making a request.
// BackendPrincipal bypasses all permission checks, AnonPrincipal
// follows permission whitelists. Origin is set from the HTTP
// Origin header.

type Principal struct {
	User   string // "" means unauthenticated
	Origin string // "" means direct navigation (user at keyboard)
}

var BackendPrincipal = &Principal{User: "backend", Origin: ""}
var AnonPrincipal = &Principal{}

// LookupRequest captures the original parameters that were passed to a Lookup call.
type LookupRequest struct {
	Paths     []string
	StartAddr BlobAddr
	Principal *Principal
}

// LookupCallback is called just before Lookup returns its response.
// resp is the proposed response (paths resolved).
// req is the original request parameters.
// root is the FileNode used as the volume root for this lookup.
// It may prune paths or return an error to reject the entire lookup.
// Called with no locks held.
type LookupCallback func(resp *LookupResponse, req *LookupRequest, root FileNode) (*LookupResponse, error)

func (ns *NameStore) AddLookupCallback(cb LookupCallback) {
	ns.lookupCallbacks = append(ns.lookupCallbacks, cb)
}

// LinkCallback is called after all recursiveLink calls succeed but before
// references are committed. Both oldRoot and newRoot are live at this point.
// writeMtx IS held; ns.mtx is NOT held (so the callback may safely read the tree).
// Return a non-nil error to abort the commit with no tree changes.
type LinkCallback func(oldRoot, newRoot FileNode, requests []*LinkRequest, principal *Principal) error

func (ns *NameStore) AddLinkCallback(cb LinkCallback) {
	ns.linkCallbacks = append(ns.linkCallbacks, cb)
}

// Lookup returns a LookupResponse for one or more paths.

// If startAddr is empty all paths are resolved from the current volume root

// If startAddr is non-empty all paths are resolved relative to that node

// If holdRef is non-nil it is called with the address of the starting node
// before Lookup returns.

func (ns *NameStore) Lookup(paths []string, startAddr BlobAddr, holdRef RefHoldFunc, principal *Principal) (*LookupResponse, error) {
	ns.mtx.Lock()
	serialNumber := ns.serialNumber
	ns.mtx.Unlock()

	// Resolve the starting node.
	var startNode FileNode
	var usedAddr BlobAddr

	if startAddr != "" {
		var err error
		startNode, err = ns.loadFileNode(startAddr, false)
		if err != nil {
			return nil, fmt.Errorf("lookup: cannot load start node %s: %w", startAddr, err)
		}
		usedAddr = startAddr
	} else {
		ns.mtx.Lock()
		usedAddr = ns.rootAddr
		ns.mtx.Unlock()
		// startNode stays nil — resolvePath will load from root itself.
	}

	if holdRef != nil {
		holdRef(usedAddr)
	}

	// Snapshot the volume root for callbacks, so they resolve against the same
	// tree the paths were resolved from.
	var cbRoot FileNode
	if len(ns.lookupCallbacks) > 0 {
		ns.mtx.Lock()
		rootAddr := ns.rootAddr
		ns.mtx.Unlock()
		if rootAddr != "" {
			var err error
			cbRoot, err = ns.loadFileNode(rootAddr, false)
			if err == nil && cbRoot != nil {
				cbRoot.Take()
				defer cbRoot.Release()
			}
		}
	}

	seenPaths := make(map[string]bool)
	response := &LookupResponse{
		Paths:        make([]*PathNodePair, 0),
		SerialNumber: serialNumber,
	}

	for _, path := range paths {
		name := strings.TrimRight(path, "/")
		lookupResp, err := ns.resolvePath(name, startNode)
		if err != nil {
			return nil, err
		}
		for _, pair := range lookupResp.Paths {
			if !seenPaths[pair.Path] {
				response.Paths = append(response.Paths, pair)
				seenPaths[pair.Path] = true
			}
		}
	}

	req := &LookupRequest{
		Paths:     paths,
		StartAddr: startAddr,
		Principal: principal,
	}
	for _, cb := range ns.lookupCallbacks {
		var err error
		response, err = cb(response, req, cbRoot)
		if err != nil {
			return nil, err
		}
	}

	return response, nil
}

// LookupFromRoot resolves a single path relative to a specific root node.
// Unlike Lookup, it does not invoke any callbacks and does not consult remote
// fetchers. rootNode must be non-nil.
func (ns *NameStore) LookupFromRoot(path string, rootNode FileNode) (*LookupResponse, error) {
	if rootNode == nil {
		return nil, fmt.Errorf("LookupFromRoot: rootNode must be non-nil")
	}
	return ns.resolvePath(path, rootNode)
}

// FIXME - Clean up this API a lot.

// Get a FileNode from a metadata address, either from cache or loaded on demand.
// Takes a reference to the node before returning it.
func (ns *NameStore) GetFileNode(metadataAddr BlobAddr) (FileNode, error) {
	node, err := ns.loadFileNode(metadataAddr, true)
	if err != nil {
		return nil, err
	}

	node.Take()
	return node, nil
}

// resolvePath resolves a path against the namestore.
// If startNode is nil the current root is used (root-relative lookup).
// If startNode is non-nil the traversal begins from that node
// (CID-relative lookup); fetchers are not consulted.
//
// Unlike old callers, this never returns a Go error for "not found" or
// "not a directory" — those conditions are encoded as PathNodePair.Error
// entries in the returned LookupResponse. A non-nil Go error means
// something structural failed (e.g. blob store I/O error).
func (ns *NameStore) resolvePath(path string, startNode FileNode) (*LookupResponse, error) {
	DebugLog(DebugNameStore, "We resolve path '%s'\n", path)

	if startNode == nil {
		ns.mtx.Lock()
		forceFetch := len(ns.fetchers) > 0 && time.Since(ns.lastFetch) > ns.CacheDuration
		ns.mtx.Unlock()

		if forceFetch {
			DebugLog(DebugNameStore, "  from fetchers (forced)")
			return ns.resolveFromFetchers(path, nil, nil)
		}
	}

	DebugLog(DebugNameStore, "  from local")
	resp, err := ns.resolveFromLocal(path, startNode)
	if err == nil {
		DebugLog(DebugNameStore, "  we return %v %v from local", resp, err)
		return resp, nil
	}
	DebugLog(DebugNameStore, "  local lookup failed: %v", err)

	if startNode != nil {
		return nil, err
	}

	DebugLog(DebugNameStore, "  from fetchers")
	resp, err = ns.resolveFromFetchers(path, resp, err)
	DebugLog(DebugNameStore, "  we return %v %v from fetcher", resp, err)
	return resp, err
}

// resolveFromLocal resolves a path against the namestore.
// If startNode is nil the current root is used (root-relative lookup).
// If startNode is non-nil the traversal begins from that node.
//
// Always returns one PathNodePair per path component, including the root ("").
// If the walk stops early (not_found, not_dir, internal error), subsequent
// components are still emitted with Error: "not_found". The shape of the
// response is always the same regardless of where the walk stopped.
func (ns *NameStore) resolveFromLocal(path string, startNode FileNode) (*LookupResponse, error) {
	path = strings.TrimRight(path, "/")
	if path != "" && path[0] == '/' {
		return nil, fmt.Errorf("paths must be relative")
	}

	ns.mtx.Lock()
	serialNumber := ns.serialNumber
	ns.mtx.Unlock()

	var rootAddr BlobAddr
	var node FileNode

	if startNode != nil {
		node = startNode
		rootAddr = startNode.MetadataBlob().GetAddress()
	} else {
		ns.mtx.Lock()
		rootAddr = ns.rootAddr
		ns.mtx.Unlock()

		var err error
		node, err = ns.loadFileNode(rootAddr, false)
		if err != nil {
			DebugLog(DebugNameStore, "  couldn't load root node")
			return nil, err
		}
		if node == nil {
			DebugLog(DebugNameStore, "  nil root")
			return nil, fmt.Errorf("looking up in nil root")
		}
	}

	node.Take()
	defer node.Release()

	parts := strings.Split(path, "/")

	response := &LookupResponse{
		Paths:        make([]*PathNodePair, 0, len(parts)+1),
		SerialNumber: serialNumber,
	}
	response.Paths = append(response.Paths, &PathNodePair{
		Path:        "",
		Addr:        rootAddr,
		ContentHash: node.Metadata().ContentHash,
		Size:        node.Metadata().Size,
	})

	// current tracks the live node as we walk down.
	// Once set to nil, we've lost the walk and emit not_found for all remaining parts.
	current := node
	currentPath := ""

	for _, part := range parts {
		if part == "" {
			continue
		}

		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = filepath.Join(currentPath, part)
		}

		// If we've already lost the walk, emit not_found for remaining parts.
		if current == nil {
			response.Paths = append(response.Paths, &PathNodePair{
				Path:  currentPath,
				Error: "not_found",
			})
			continue
		}

		treeNode, isTreeNode := current.(*TreeNode)
		if !isTreeNode {
			DebugLog(DebugNameStore, "    isn't tree!")
			response.Paths = append(response.Paths, &PathNodePair{
				Path:  currentPath,
				Error: "not_dir",
			})
			current = nil
			continue
		}

		children, err := treeNode.Children()
		if err != nil {
			log.Printf("resolveFromLocal: error loading children of %s: %v", treeNode.MetadataBlob().GetAddress(), err)
			response.Paths = append(response.Paths, &PathNodePair{
				Path:  currentPath,
				Error: "internal",
			})
			current = nil
			continue
		}

		childAddr, exists := children[part]
		if !exists {
			DebugLog(DebugNameStore, "    not found")
			response.Paths = append(response.Paths, &PathNodePair{
				Path:  currentPath,
				Error: "not_found",
			})
			current = nil
			continue
		}

		childNode, err := ns.loadFileNode(childAddr, true)
		if err != nil {
			log.Printf("resolveFromLocal: error loading child node %s: %v", childAddr, err)
			response.Paths = append(response.Paths, &PathNodePair{
				Path:  currentPath,
				Error: "internal",
			})
			current = nil
			continue
		}

		response.Paths = append(response.Paths, &PathNodePair{
			Path:        currentPath,
			Addr:        childAddr,
			ContentHash: childNode.Metadata().ContentHash,
			Size:        childNode.Metadata().Size,
		})
		current = childNode
	}

	DebugLog(DebugNameStore, "  all done")
	return response, nil
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

func extractPaths(requests []*LinkRequest) []string {
	paths := make([]string, 0, len(requests))
	seen := make(map[string]bool)
	for _, req := range requests {
		p := strings.TrimRight(req.Path, "/")
		if !seen[p] {
			paths = append(paths, p)
			seen[p] = true
		}
	}
	return paths
}

func (ns *NameStore) MultiLink(requests []*LinkRequest, returnResults bool, principal *Principal) (*LookupResponse, error) {
	DebugLog(DebugLinks, "MultiLink - %d elements", len(requests))

	// Serialize all link operations
	ns.writeMtx.Lock()
	defer ns.writeMtx.Unlock()

	// Phase 1: Pre-fetch and validate assertions
	// We do this without holding the main lock (except for brief periods)
	// to allow network fetches to proceed in parallel with reads

	for _, req := range requests {
		if req.Path == "" || req.Path == "." {
			if req.NewAddr == "" {
				return nil, fmt.Errorf("cannot unlink root")
			}
		}

		if req.Assert == 0 {
			continue
		}

		DebugLog(DebugLinks, "Checking assertion: %d", req.Assert)

		var node FileNode
		lookupResp, err := ns.resolvePath(req.Path, nil)
		if err != nil {
			DebugLog(DebugLinks, "  error resolving path: %v", err)
			return nil, err
		}
		if len(lookupResp.Paths) <= 0 {
			return nil, fmt.Errorf("empty return from resolvePath()")
		}

		leaf := lookupResp.Leaf()
		if leaf.Error == "not_found" {
			// Check if the parent was actually reachable
			parentReachable := false
			if len(lookupResp.Paths) >= 2 {
				parent := lookupResp.Paths[len(lookupResp.Paths)-2]
				parentReachable = parent.Error == ""
			} else if len(lookupResp.Paths) == 1 {
				// Only the root entry — root is the parent for a top-level path
				parentReachable = lookupResp.Paths[0].Error == ""
			}
			if !parentReachable {
				return nil, ErrNotExist
			}
			node = nil
		} else if leaf.Error != "" {
			return nil, ErrNotExist
		} else {
			var loadErr error
			node, loadErr = ns.loadFileNode(leaf.Addr, true)
			if loadErr != nil {
				return nil, loadErr
			}
		}

		if req.Assert&AssertPrevValueMatches != 0 {
			DebugLog(DebugLinks, "  prev value matches")
			if !matchesAddr(node, req.PrevAddr) {
				return nil, ErrAssertionFailed
			}
			DebugLog(DebugLinks, "    pass")
		}

		if req.Assert&AssertIsBlob != 0 {
			DebugLog(DebugLinks, "  is blob")
			if node == nil || node.Metadata().Type != GNodeTypeFile {
				return nil, ErrAssertionFailed
			}
			DebugLog(DebugLinks, "    pass")
		}

		if req.Assert&AssertIsTree != 0 {
			DebugLog(DebugLinks, "  is tree")
			if node == nil || node.Metadata().Type != GNodeTypeDirectory {
				return nil, ErrAssertionFailed
			}
			DebugLog(DebugLinks, "    pass")
		}

		if req.Assert&AssertIsNonEmpty != 0 {
			DebugLog(DebugLinks, "  is nonempty")
			if node == nil {
				return nil, ErrAssertionFailed
			}
			DebugLog(DebugLinks, "    pass")
		}
	}

	// Phase 2: Build new tree

	ns.mtx.Lock()
	oldRootAddr := ns.rootAddr
	newSerialNumber := ns.serialNumber + 1
	ns.mtx.Unlock()

	oldRoot, err := ns.loadFileNode(oldRootAddr, false)
	if err != nil {
		return nil, err
	}
	newRoot := oldRoot

	for _, req := range requests {
		name := strings.TrimRight(req.Path, "/")
		if name != "" && name[0] == '/' {
			return nil, fmt.Errorf("name must be relative")
		}
		if name == "." {
			name = ""
		}

		newRoot, err = ns.recursiveLink("", name, req.NewAddr, newRoot)
		if err != nil {
			return nil, err
		}
	}

	// Phase 3: Link callback — veto the write before anything that'd be hard to undo.
	// Both oldRoot and newRoot have refs held. writeMtx held, ns.mtx NOT held.

	for _, cb := range ns.linkCallbacks {
		if err := cb(oldRoot, newRoot, requests, principal); err != nil {
			return nil, err
		}
	}

	// Phase 4: Build response against newRoot (before swapping, so we pass
	// newRoot explicitly as the start node rather than relying on the current root).

	var response *LookupResponse
	if returnResults {
		seenPaths := make(map[string]bool)
		response = &LookupResponse{
			Paths:        make([]*PathNodePair, 0),
			SerialNumber: newSerialNumber,
		}

		for _, req := range requests {
			path := strings.TrimRight(req.Path, "/")
			if _, exists := seenPaths[path]; exists {
				continue
			}

			lookupResp, err := ns.resolvePath(path, newRoot)
			if err != nil {
				return nil, err
			}

			if leaf := lookupResp.Leaf(); leaf != nil && leaf.Error == "not_found" {
				// Path was deleted — emit a clean nil entry, not an error.
				if !seenPaths[path] {
					response.Paths = append(response.Paths, &PathNodePair{Path: path})
					seenPaths[path] = true
				}
				continue
			} else if leaf != nil && leaf.Error != "" {
				log.Printf("Unexpected error leaf in MultiLink result for %s: %s", path, leaf.Error)
			}

			for _, pair := range lookupResp.Paths {
				if _, exists := seenPaths[pair.Path]; !exists {
					response.Paths = append(response.Paths, pair)
					seenPaths[pair.Path] = true
				}
			}
		}

		// Lookup callback — veto or prune the response before committing.
		req := &LookupRequest{
			Paths:     extractPaths(requests),
			Principal: principal,
		}
		for _, cb := range ns.lookupCallbacks {
			response, err = cb(response, req, newRoot)
			if err != nil {
				return nil, err
			}
		}
	}

	// Phase 5: Commit — expensive and hard to undo, so we only reach here
	// after both callbacks have approved.

	err = ns.refManager.recursiveTake(ns, newRoot, nil)
	if err != nil {
		return nil, err
	}

	err = ns.refManager.recursiveRelease(ns, oldRoot)
	if err != nil {
		if newRoot != nil {
			err2 := ns.refManager.recursiveRelease(ns, newRoot)
			if err2 != nil {
				log.Fatalf("Can't re-release new root when cancelling: %v (original error: %v)", err2, err)
			}
		}
		return nil, err
	}

	ns.mtx.Lock()
	ns.rootAddr = newRoot.MetadataBlob().GetAddress()
	ns.serialNumber++
	ns.mtx.Unlock()

	return response, nil
}

func (ns *NameStore) LinkByMetadata(name string, metadataAddr BlobAddr, principal *Principal) error {
	name = strings.TrimRight(name, "/")
	if name != "" && name[0] == '/' {
		return fmt.Errorf("name must be relative")
	}
	if name == "." {
		name = ""
	}

	_, err := ns.MultiLink([]*LinkRequest{{
		Path:    name,
		NewAddr: metadataAddr,
	}}, false, principal)
	return err
}

// linkBlob creates metadata for a blob and links it into the path
func (ns *NameStore) linkBlob(name string, addr BlobAddr, size int64) error {
	if addr == "" {
		// If addr is "", we're unlinking
		return ns.LinkByMetadata(name, "", BackendPrincipal)
	}

	// Create metadata for the blob
	_, metadataBlob, err := ns.createMetadataBlob(addr, size, false, 0)
	if err != nil {
		return fmt.Errorf("failed to create blob metadata: %v", err)
	}
	defer metadataBlob.Release()

	// Link using the metadata address
	return ns.LinkByMetadata(name, metadataBlob.GetAddress(), BackendPrincipal)
}

// linkTree creates metadata for a tree and links it into the path
func (ns *NameStore) linkTree(name string, addr BlobAddr) error {
	if addr == "" {
		// If addr is nil, we're unlinking
		return ns.LinkByMetadata(name, "", BackendPrincipal)
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
	return ns.LinkByMetadata(name, metadataBlob.GetAddress(), BackendPrincipal)
}

// Core link function helper.

// We link in `metadataAddr` into place as `name` within `oldParent`, and return the
// modified version of `oldParent`.

// Core link function helper
// This doesn't need locking because it's working on immutable tree nodes
func (ns *NameStore) recursiveLink(prevPath string, name string, metadataAddr BlobAddr, oldParent FileNode) (FileNode, error) {
	if metadataAddr != "" {
		DebugLog(DebugNameStore, "We're trying to link %s under path %s /// %s\n", metadataAddr, prevPath, name)
	} else {
		DebugLog(DebugNameStore, "We're trying to link nil under path %s /// %s\n", prevPath, name)
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
		var err error
		oldChildren, err = oldParent.Children()
		if err != nil {
			return nil, err
		}
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

		err = ns.notifyWatchers(currPath, oldChild, newValue)
		if err != nil {
			return nil, err
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

	result, err := ns.CreateTreeNode(newChildren)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func EmptyNameStore(bs BlobStore, sparse bool) (*NameStore, error) {
	var refManager RefManager
	if sparse {
		refManager = NewSparseRefManager(time.Second * 60) // FIXME
	} else {
		refManager = NewDenseRefManager("")
	}

	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[BlobAddr]*fileCacheEntry),

		refManager:   refManager,
		serialNumber: 1,
	}
	ns.cacheCond = sync.NewCond(&ns.mtx)

	// For access control debugging
	// ns.DebugWhitelist = []string{"one", "two", "lib", "tmp", "home"}
	ns.DebugWhitelist = []string{}

	rootNodeMap := make(map[string]BlobAddr)
	root, err := ns.CreateTreeNode(rootNodeMap)
	if err != nil {
		return nil, err
	}

	err = ns.refManager.recursiveTake(ns, root, nil)
	if err != nil {
		return nil, err
	}

	ns.serialNumber = 0
	ns.rootAddr = root.MetadataBlob().GetAddress()
	return ns, nil
}

func (ns *NameStore) GetSerialNumber() int64 {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()
	return ns.serialNumber
}

func (ns *NameStore) DeserializeNameStore(rootAddr BlobAddr, serialNumber int64, cacheDuration time.Duration) error {
	// Take write lock since we're updating the root
	ns.writeMtx.Lock()
	defer ns.writeMtx.Unlock()

	root, err := ns.loadFileNode(rootAddr, true)
	if err != nil {
		return err
	}

	//ns.refManager.recursiveRelease might be nice here, in case the thing was somehow non-empty when we started

	err = ns.refManager.recursiveTake(ns, root, nil)
	if err != nil {
		return err
	}

	ns.mtx.Lock()
	ns.rootAddr = root.MetadataBlob().GetAddress()
	ns.serialNumber = serialNumber
	ns.CacheDuration = cacheDuration
	ns.mtx.Unlock()

	return nil
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

// Get a file node, return it
// This is now lock-free except for brief fileCache access
func (ns *NameStore) loadFileNode(metadataAddr BlobAddr, printDebug bool) (FileNode, error) {
	DebugLog(DebugFileCache && printDebug, "We try to chase down %s\n", metadataAddr)

	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	for {
		entry, exists := ns.fileCache[metadataAddr]

		if exists && !entry.inFlight {
			// Found it and it's ready
			return entry.node, nil
		}

		if !exists {
			// We're first - claim it
			entry = &fileCacheEntry{
				node:     nil,
				inFlight: true,
			}
			ns.fileCache[metadataAddr] = entry

			// Unlock to do the fetch
			ns.mtx.Unlock()
			node, err := ns.actuallyLoadFileNode(metadataAddr, printDebug)
			ns.mtx.Lock()

			if err != nil {
				// Don't cache failures — remove the entry so the next caller retries
				delete(ns.fileCache, metadataAddr)
				entry.inFlight = false
				ns.cacheCond.Broadcast()
				return nil, err
			}

			// Update and broadcast
			entry.node = node
			entry.inFlight = false
			ns.cacheCond.Broadcast() // Wake all waiters

			return node, err
		}

		// Entry exists but is in flight - wait
		ns.cacheCond.Wait() // Unlocks, waits, re-locks
	}
}

// actuallyLoadFileNode does the actual work of loading a node from storage
// This is called without locks held
func (ns *NameStore) actuallyLoadFileNode(metadataAddr BlobAddr, printDebug bool) (FileNode, error) {
	metadataCf, err := ns.BlobStore.ReadFile(metadataAddr)
	if err != nil {
		return nil, err
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

	DebugLog(DebugNameStore, "Creating node for metadata %s (content will be lazy-loaded)\n", metadataAddr)

	var blob CachedFile
	var blobErr error

	// We force an actual load, if we're in a dense NameStore
	// Otherwise, we might wind up GCing the content blob because
	// nothing has a reference to it.

	// FIXME this is a bit of a hack
	if _, ok := ns.refManager.(*DenseRefManager); ok {
		blob, blobErr = ns.BlobStore.ReadFile(metadata.ContentHash)
	}

	if metadata.Type == GNodeTypeFile {
		bn := &BlobNode{
			blob:         blob,
			blobErr:      blobErr,
			metadataBlob: metadataCf,
			metadata:     &metadata,
			refCount:     0,
			nameStore:    ns,
		}
		DebugLog(DebugFileCache && printDebug, "  created blob: %p", bn)

		return bn, nil

	} else if metadata.Type == GNodeTypeDirectory {
		dn := &TreeNode{
			blob:         blob,
			blobErr:      blobErr,
			metadataBlob: metadataCf,
			metadata:     &metadata,
			ChildrenMap:  nil, // Will be loaded when blob is loaded
			nameStore:    ns,
		}

		DebugLog(DebugFileCache && printDebug, "  created tree: %p", dn)
		return dn, nil

	} else {
		metadataCf.Release()
		return nil, fmt.Errorf("unknown node type: %d", int(metadata.Type))
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
	return ns.createMetadataBlob(contentHash, size, isDir, mode)
}

// Create a metadata node with proper mode and timestamps
// No longer needs to lock since it's just creating blobs
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
	DebugLog(DebugNameStore, "Creating tree node for map with %d children", len(children))

	tn := &TreeNode{
		ChildrenMap: children,
		nameStore:   ns,
	}

	if DebugRefCounts {
		DebugLog(DebugRefCounts, "CreateTreeNode() Creating tree node %p", tn)
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

	DebugLog(DebugNameStore, "Added metadata blob, addr %s, rc %d", metadataBlob.GetAddress(), metadataBlob.GetRefCount())

	tn.blob = contentBlob
	tn.metadata = metadata
	tn.metadataBlob = metadataBlob

	// Check if this tree node already exists in cache
	// Use brief lock for cache check/update
	ns.mtx.Lock()
	existingEntry, exists := ns.fileCache[tn.metadataBlob.GetAddress()]
	if exists && existingEntry.node != nil {
		ns.mtx.Unlock()

		contentBlob.Release()
		metadataBlob.Release()

		existingTreeNode, ok := existingEntry.node.(*TreeNode)
		if !ok {
			return nil, fmt.Errorf("non tree node found in file cache for tree node %s", tn.metadataBlob.GetAddress())
		}

		return existingTreeNode, nil
	} else {
		// Add to cache
		ns.fileCache[tn.metadataBlob.GetAddress()] = &fileCacheEntry{
			node:     tn,
			inFlight: false,
		}
		ns.mtx.Unlock()
		return tn, nil
	}

	// We do not release the cachedFiles for the content or metadata. Ownership of them has been
	// taken over by the FileNode, at this point.
}

func (ns *NameStore) DumpFileCache() error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	log.Printf("=== File Cache Contents ===")
	for cid, entry := range ns.fileCache {
		log.Printf("CID: %s", cid)
		if entry == nil || entry.node == nil {
			log.Printf("  Value: nil")
			continue
		}

		node := entry.node

		// Get the content blob
		blob, err := node.ExportedBlob()
		if err != nil {
			log.Printf("  Error! %v", err)
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
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	log.Printf("=== Name Store Tree Dump ===")
	ns.dumpTreeNode("", ns.rootAddr, "(root)")
	log.Printf("=== End Tree Dump ===")
}

func (ns *NameStore) dumpTreeNode(indent string, nodeAddr BlobAddr, name string) {
	node, err := ns.loadFileNode(nodeAddr, false)
	if err != nil {
		log.Printf("Couldn't load %s", nodeAddr)
		return
	}

	if node == nil {
		log.Printf("%s%s: <nil>", indent, name)
		return
	}

	contentBlob, err := node.ExportedBlob()
	if err != nil {
		log.Printf("Couldn't load content for %s: %v", nodeAddr, err)
		return
	}
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
	children, err := node.Children()
	if err != nil {
		log.Printf("Error trying to load children: %v", err)
		return
	}
	if children != nil {
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

			ns.dumpTreeNode(indent+"  ", childNode.MetadataBlob().GetAddress(), childName)
		}
	}
}

/////
// Debug stuff

// DebugReferenceCountsRecursive walks the entire namespace tree and prints reference count information
// for all nodes, while also identifying orphaned blobs
func (ns *NameStore) PrintBlobStorageDebugging() error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	// Map to track which blobs we've seen
	seenBlobs := make(map[BlobAddr]bool)

	log.Println("=== Reference Count Debugging ===")
	log.Println("Root node:", ns.GetRoot())

	// Start recursive walk from root
	ns.debugRefCountsWalk("", ns.rootAddr, seenBlobs)

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
func (ns *NameStore) debugRefCountsWalk(path string, nodeAddr BlobAddr, seenBlobs map[BlobAddr]bool) {
	node, err := ns.loadFileNode(nodeAddr, false)
	if node == nil {
		// ??? Can't happen
		log.Printf("%s: <nil> %v\n", path, err)
		return
	}

	contentBlob, err := node.ExportedBlob()
	if err != nil {
		log.Printf("Can't read content for %s: %v", nodeAddr, err)
		return
	}
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
	children, err := node.Children()
	if err != nil {
		log.Printf("Error trying to get children: %v", err)
		return
	}
	if children != nil {
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

			ns.debugRefCountsWalk(childPath, childNode.MetadataBlob().GetAddress(), seenBlobs)
		}
	}
}

func (ns *NameStore) DebugDumpNamespace() {
	log.Println("=== NAMESPACE DUMP ===")
	log.Printf("Root node: %s\n\n", ns.rootAddr)

	if ns.rootAddr == "" {
		log.Println("Root is nil, nothing to dump")
		return
	}

	// Start recursive DFS from the root
	ns.debugDumpNode("", ns.rootAddr)

	log.Println("=== END NAMESPACE DUMP ===")
}

func (ns *NameStore) debugDumpNode(path string, nodeAddr BlobAddr) {
	node, err := ns.loadFileNode(nodeAddr, false)
	if node == nil {
		log.Printf("%s: <nil> %v\n", path, err)
		return
	}

	// Get blobs and metadata
	contentBlob, err := node.ExportedBlob()
	if err != nil {
		log.Printf("Can't load content for %s: %v", nodeAddr, err)
		return
	}
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
	children, err := node.Children()
	if err != nil {
		log.Printf("Couldn't load children: %v", err)
		return
	}
	if children != nil {
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

			ns.debugDumpNode(childPath, childNode.MetadataBlob().GetAddress())
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
	ExportedBlob() (CachedFile, error)
	MetadataBlob() CachedFile
	Metadata() *GNodeMetadata
	Children() (map[string]BlobAddr, error)

	Take()
	Release()
	RefCount() int
}

type BlobNode struct {
	blob    CachedFile // May be nil until first access, or on error
	blobErr error

	metadataBlob CachedFile
	metadata     *GNodeMetadata

	refCount int
	mtx      sync.Mutex

	nameStore *NameStore
}

var _ = (FileNode)((*BlobNode)(nil))

// Implementations for BlobNode

func (bn *BlobNode) ExportedBlob() (CachedFile, error) {
	bn.mtx.Lock()
	defer bn.mtx.Unlock()

	// TODO: Handle transient errors better - currently we cache errors forever
	if bn.blob != nil || bn.blobErr != nil {
		return bn.blob, bn.blobErr
	}

	contentHash := bn.metadata.ContentHash
	bn.blob, bn.blobErr = bn.nameStore.BlobStore.ReadFile(contentHash)
	return bn.blob, bn.blobErr
}

func (bn *BlobNode) MetadataBlob() CachedFile {
	return bn.metadataBlob
}

func (bn *BlobNode) Metadata() *GNodeMetadata {
	return bn.metadata
}

func (bn *BlobNode) Children() (map[string]BlobAddr, error) {
	return nil, nil
}

func (bn *BlobNode) Take() {
	bn.mtx.Lock()
	defer bn.mtx.Unlock()

	if DebugRefCounts {
		log.Printf("TAKE: %s->%s %p (count: %d)",
			bn.metadataBlob.GetAddress(),
			bn.metadata.ContentHash,
			bn,
			bn.refCount+1)
		PrintStack()
	}

	if bn.refCount < 0 {
		PrintStack()
		log.Fatalf("Ref count for %s is < 0", bn.metadataBlob.GetAddress())
	} else if bn.refCount == 0 {
		bn.nameStore.refManager.flatTake(bn.nameStore, bn)
	}

	bn.refCount++
}

func (bn *BlobNode) Release() {
	bn.mtx.Lock()
	defer bn.mtx.Unlock()

	if DebugRefCounts {
		log.Printf("RELEASE: %s->%s %p (count: %d)",
			bn.metadataBlob.GetAddress(),
			bn.metadata.ContentHash,
			bn,
			bn.refCount-1)
		PrintStack()
	}

	bn.refCount--
	if bn.refCount < 0 {
		PrintStack()
		log.Fatalf("Reduced ref count for %s to < 0", bn.metadataBlob.GetAddress())
	} else if bn.refCount == 0 {
		bn.nameStore.refManager.flatRelease(bn.nameStore, bn)
	}

	// This is where we used to release the actual storage; now we're not doing that until
	// deferred cleanup
}

// FIXME - Maybe audit the callers of this, make sure they are synchronized WRT things that
// might cause take/release of references
func (bn *BlobNode) RefCount() int {
	return bn.refCount
}

// Implementation for TreeNode

type TreeNode struct {
	blob    CachedFile // May be nil until first access, or on error
	blobErr error

	metadataBlob CachedFile
	metadata     *GNodeMetadata

	ChildrenMap map[string]BlobAddr
	refCount    int
	mtx         sync.Mutex

	nameStore *NameStore
}

var _ = (FileNode)((*TreeNode)(nil))

func (tn *TreeNode) ExportedBlob() (CachedFile, error) {
	tn.mtx.Lock()
	defer tn.mtx.Unlock()

	// TODO: Handle transient errors better - currently we cache errors forever
	if tn.blob != nil || tn.blobErr != nil {
		return tn.blob, tn.blobErr
	}

	// We need metadata to know the content hash
	if tn.metadata == nil {
		tn.blobErr = fmt.Errorf("no metadata available")
		return nil, tn.blobErr
	}

	contentHash := tn.metadata.ContentHash

	tn.blob, tn.blobErr = tn.nameStore.BlobStore.ReadFile(contentHash)
	return tn.blob, tn.blobErr
}

func (tn *TreeNode) MetadataBlob() CachedFile {
	return tn.metadataBlob
}

func (tn *TreeNode) Metadata() *GNodeMetadata {
	return tn.metadata
}

// Children lazy-loads and parses the directory structure
func (tn *TreeNode) Children() (map[string]BlobAddr, error) {
	// First get the blob without holding the lock
	blob, err := tn.ExportedBlob()
	if err != nil {
		return nil, err
	}

	// Now grab our lock to check/update ChildrenMap
	tn.mtx.Lock()
	defer tn.mtx.Unlock()

	// TODO: Handle transient errors better - currently we cache errors forever
	if tn.ChildrenMap != nil || tn.blobErr != nil {
		return tn.ChildrenMap, tn.blobErr
	}

	// Parse the directory structure from the blob
	dirData, err := blob.Read(0, blob.GetSize())
	if err != nil {
		tn.blobErr = fmt.Errorf("error reading directory data: %v", err)
		return nil, tn.blobErr
	}

	dirMap := make(map[string]string)
	if err := json.Unmarshal(dirData, &dirMap); err != nil {
		tn.blobErr = fmt.Errorf("error parsing directory: %v", err)
		return nil, tn.blobErr
	}

	tn.ChildrenMap = make(map[string]BlobAddr)
	for name, childMetadataCID := range dirMap {
		tn.ChildrenMap[name], err = NewBlobAddrFromString(childMetadataCID)
		if err != nil {
			tn.blobErr = err
			return nil, err
		}
	}

	return tn.ChildrenMap, nil
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

	if tn.refCount < 0 {
		PrintStack()
		log.Fatalf("Ref count for %s is < 0", tn.metadataBlob.GetAddress())
	} else if tn.refCount == 0 {
		tn.nameStore.refManager.flatTake(tn.nameStore, tn)
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
	} else if tn.refCount == 0 {
		tn.nameStore.refManager.flatRelease(tn.nameStore, tn)
	}

	// This is where we used to release the actual storage; now we do not do that.
}

// FIXME - Maybe audit the callers of this, make sure they are synchronized WRT things that
// might cause take/release of references
func (tn *TreeNode) RefCount() int {
	return tn.refCount
}

func (ns *NameStore) DebugPrintTree(node FileNode) {
	ns.debugPrintTree(node, "")
}

func (ns *NameStore) debugPrintTree(node FileNode, indent string) {
	if node == nil {
		return
	}

	children, err := node.Children()
	if err != nil {
		log.Printf("Error fetching children")
		return
	}
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

// ErrBlobMissing is returned when a link operation needs a blob not in the local store.
// Addr is the blob address the client should upload before retrying.
type ErrBlobMissing struct {
	Addr BlobAddr
}

func (e *ErrBlobMissing) Error() string {
	return fmt.Sprintf("blob not in store: %s", e.Addr)
}

func IsBlobMissing(err error) (*ErrBlobMissing, bool) {
	var e *ErrBlobMissing
	return e, errors.As(err, &e)
}

// ErrAccessDenied is returned when a path is outside what's permitted.
type ErrAccessDenied struct {
	Path string
}

func (e *ErrAccessDenied) Error() string {
	return fmt.Sprintf("access denied: %s", e.Path)
}

func IsAccessDenied(err error) (*ErrAccessDenied, bool) {
	var e *ErrAccessDenied
	return e, errors.As(err, &e)
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

	for i, w := range ns.watchers {
		if w == watcher {
			// Remove by replacing with last element and truncating
			ns.watchers[i] = ns.watchers[len(ns.watchers)-1]
			ns.watchers = ns.watchers[:len(ns.watchers)-1]
			break
		}
	}

	ns.wmtx.Unlock()
	ns.wgroup.Wait() // Wait until callbacks complete before returning
}

// notifyWatchers sends event to all registered watchers
func (ns *NameStore) notifyWatchers(path string, oldValue FileNode, newValue FileNode) error {
	ns.wmtx.RLock()
	watchers := make([]FileTreeWatcher, len(ns.watchers))
	copy(watchers, ns.watchers) // Copy to avoid holding lock during callbacks
	ns.wgroup.Add(len(watchers))
	ns.wmtx.RUnlock()

	// Notify each watcher
	var result error
	for _, watcher := range watchers {
		err := watcher.OnFileTreeChange(path, oldValue, newValue)
		if err != nil {
			result = err
		}
		ns.wgroup.Done()
	}
	return result
}

/////
// Fetchers

// RegisterFetcher adds a fetcher to be tried when blobs are not found locally
func (ns *NameStore) RegisterFetcher(fetcher BlobFetcher) {
	ns.fetcherMtx.Lock()
	defer ns.fetcherMtx.Unlock()

	ns.fetchers = append(ns.fetchers, fetcher)
}

// UnregisterFetcher removes a fetcher from the list
func (ns *NameStore) UnregisterFetcher(fetcher BlobFetcher) {
	ns.fetcherMtx.Lock()
	defer ns.fetcherMtx.Unlock()

	for i, f := range ns.fetchers {
		if f == fetcher {
			ns.fetchers[i] = ns.fetchers[len(ns.fetchers)-1]
			ns.fetchers = ns.fetchers[:len(ns.fetchers)-1]
			break
		}
	}
}

// resolveFromFetchers uses registered fetchers to resolve a path
// This now takes the write lock when updating the root to ensure atomicity
func (ns *NameStore) resolveFromFetchers(path string, _ *LookupResponse, origErr error) (*LookupResponse, error) {
	DebugLog(DebugNameStore, "resolveFromFetchers(%s)\n", path)

	ns.fetcherMtx.RLock()
	fetchers := ns.fetchers
	ns.fetcherMtx.RUnlock()

	// Try each fetcher (no locks held during network I/O)
	lastErr := origErr
	for _, fetcher := range fetchers {
		lookupResp, err := fetcher.FetchPath(path)
		if err != nil {
			lastErr = err
			continue
		}

		if lookupResp == nil || len(lookupResp.Paths) == 0 {
			lastErr = fmt.Errorf("empty lookup response")
			continue
		}

		// Take write lock to update root atomically
		ns.writeMtx.Lock()
		defer ns.writeMtx.Unlock() // Note, we're all done at this point with anything that might loop again

		ns.mtx.Lock()
		currentSerialNumber := ns.serialNumber
		ns.mtx.Unlock()

		if lookupResp.SerialNumber < currentSerialNumber {
			log.Printf("Warning! Out of order lookup response")
			// Still return the data, but don't update our root
			return lookupResp, nil
		}

		// The first entry should always be the root (path "")
		if lookupResp.Paths[0].Path != "" {
			return nil, fmt.Errorf("0 path entry is for %s, not root", lookupResp.Paths[0].Path)
		}

		// Update our root with the fetched data
		newRoot, err := ns.loadFileNode(lookupResp.Paths[0].Addr, false)
		if err != nil {
			return nil, err
		}

		err = ns.refManager.recursiveTake(ns, newRoot, nil)
		if err != nil {
			log.Printf("Can't update with new data! Looking up %s", path)
			return nil, err
		}

		ns.mtx.Lock()
		oldRootAddr := ns.rootAddr
		ns.lastFetch = time.Now()
		ns.mtx.Unlock()

		if oldRootAddr != "" {
			oldRoot, err := ns.loadFileNode(oldRootAddr, false)
			if err != nil {
				log.Printf("Can't load old root addr! Leaking refs")
			} else {
				err = ns.refManager.recursiveRelease(ns, oldRoot)
				if err != nil {
					log.Printf("Can't release ref after looking up %s! We will leak refs", path)
				}
			}
		}

		ns.mtx.Lock()
		log.Printf("We're updating the root now. Testing %d > %d", lookupResp.SerialNumber, ns.serialNumber)
		if lookupResp.SerialNumber > ns.serialNumber {
			ns.rootAddr = lookupResp.Paths[0].Addr
			ns.serialNumber = lookupResp.SerialNumber
		}
		ns.mtx.Unlock()

		// Return the lookup response directly
		return lookupResp, nil
	}

	if len(fetchers) > 1 {
		return nil, fmt.Errorf("all fetchers failed: %w", lastErr)
	} else {
		return nil, lastErr
	}
}

/////
// RefManagers

type RefManager interface {
	recursiveTake(ns *NameStore, fn FileNode, taken *[]BlobAddr) error
	recursiveRelease(ns *NameStore, fn FileNode) error
	flatTake(ns *NameStore, fn FileNode) error
	flatRelease(ns *NameStore, fn FileNode) error
	cleanup(ns *NameStore)
}

type SparseRefManager struct {
	nodeDropTimes map[BlobAddr]time.Time
	timeout       time.Duration
	// mtx protected under NameStore.writeMtx
}

func NewSparseRefManager(timeout time.Duration) *SparseRefManager {
	return &SparseRefManager{
		nodeDropTimes: make(map[BlobAddr]time.Time),
		timeout:       timeout,
	}
}

func (rm *SparseRefManager) recursiveTake(ns *NameStore, fn FileNode, taken *[]BlobAddr) error {
	// No-op for SparseRefManager
	return nil
}

func (rm *SparseRefManager) recursiveRelease(ns *NameStore, fn FileNode) error {
	// No-op for SparseRefManager
	return nil
}

func (rm *SparseRefManager) flatTake(ns *NameStore, fn FileNode) error {
	// No-op when taking a reference
	return nil
}

func (rm *SparseRefManager) flatRelease(ns *NameStore, fn FileNode) error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	metadataAddr := fn.MetadataBlob().GetAddress()
	if fn.RefCount() == 0 {
		rm.nodeDropTimes[metadataAddr] = time.Now()
	}

	return nil
}

func (rm *SparseRefManager) cleanup(ns *NameStore) {
	// Brief lock to get entries to clean up
	ns.mtx.Lock()

	now := time.Now()
	entriesToClean := make(map[BlobAddr]*fileCacheEntry)
	for metadataAddr, dropTime := range rm.nodeDropTimes {
		if now.Sub(dropTime) > rm.timeout {
			if entry, exists := ns.fileCache[metadataAddr]; exists {
				entriesToClean[metadataAddr] = entry
				delete(ns.fileCache, metadataAddr)
			}
		}
	}
	for metadataAddr, _ := range entriesToClean {
		delete(rm.nodeDropTimes, metadataAddr)
	}

	ns.mtx.Unlock()

	// Clean up without holding lock
	for metadataAddr, entry := range entriesToClean {
		if entry.node != nil {
			metadataBlob := entry.node.MetadataBlob()
			if metadataBlob != nil {
				metadataBlob.Release()
			}

			contentBlob, err := entry.node.ExportedBlob()
			if err != nil {
				log.Printf("Problem cleaning up %s: %v", metadataAddr, err)
			} else if contentBlob != nil {
				contentBlob.Release()
			}
		}
	}
}

// Dense ref manager

type DenseRefManager struct {
	path     string
	refCount map[BlobAddr]int
	// mtx protected under NameStore.writeMtx
}

func NewDenseRefManager(path string) *DenseRefManager {
	return &DenseRefManager{
		path:     path,
		refCount: make(map[BlobAddr]int),
	}
}

func (rm *DenseRefManager) recursiveTake(ns *NameStore, fn FileNode, taken *[]BlobAddr) error {
	if fn == nil {
		DebugLog(DebugRefCounts, "Recursive take on nil")
		return nil
	}
	if taken == nil {
		t := make([]BlobAddr, 0) // Happens only on topmost level, then everything else uses the one
		taken = &t
	}

	DebugLog(DebugRefCounts, "Recursive take on %s %p: count %d/%d", fn.MetadataBlob().GetAddress(), fn, fn.RefCount(), rm.refCount[BlobAddr(fn.MetadataBlob().GetAddress())])

	metadataHash := fn.MetadataBlob().GetAddress()
	refCount, exists := rm.refCount[metadataHash]

	if exists {
		DebugLog(DebugRefCounts, "  already exists")
		if refCount <= 0 {
			log.Fatalf("ref count for %s is nonpositive", metadataHash)
		}
		rm.refCount[metadataHash] = refCount + 1
		*taken = append(*taken, metadataHash)
		DebugLog(DebugRefCounts, "Increment count! For %s, we go to %d", metadataHash, refCount+1)
	} else {
		DebugLog(DebugRefCounts, "  doesn't exist")

		children, err := fn.Children()
		if err != nil {
			rm.releaseTaken(ns, taken)
			return err
		}
		for _, childMetadataAddr := range children {
			childNode, err := ns.loadFileNode(childMetadataAddr, true)
			if err != nil {
				rm.releaseTaken(ns, taken)
				return err
			}
			err = rm.recursiveTake(ns, childNode, taken)
			if err != nil {
				rm.releaseTaken(ns, taken)
				return err
			}
		}
		rm.refCount[metadataHash] = 1
		*taken = append(*taken, metadataHash)
		fn.Take()
	}

	return nil
}

func (rm *DenseRefManager) releaseTaken(ns *NameStore, taken *[]BlobAddr) {
	for _, addr := range *taken {
		count := rm.refCount[addr]
		if count <= 1 {
			delete(rm.refCount, addr)
			ns.mtx.Lock()
			if entry, exists := ns.fileCache[addr]; exists && entry.node != nil {
				entry.node.Release()
			}
			ns.mtx.Unlock()
		} else {
			rm.refCount[addr] = count - 1
		}
	}
	*taken = (*taken)[:0]
}

func (rm *DenseRefManager) recursiveRelease(ns *NameStore, fn FileNode) error {
	if fn == nil {
		DebugLog(DebugRefCounts, "Recursive release on nil")
		return nil
	}

	metadataHash := fn.MetadataBlob().GetAddress()
	DebugLog(DebugRefCounts, "Recursive release on %s: count %d/%d", fn.MetadataBlob().GetAddress(), fn.RefCount(), rm.refCount[metadataHash])

	refCount, exists := rm.refCount[metadataHash]
	if !exists {
		log.Fatalf("can't find %s in ref count to release", metadataHash)
	}

	if refCount <= 0 {
		log.Fatalf("Releasing 0-reference node")
	} else if refCount == 1 {
		children, err := fn.Children()
		if err != nil {
			return err
		}
		for _, childMetadataAddr := range children {
			childNode, err := ns.loadFileNode(childMetadataAddr, true)
			if err != nil {
				log.Printf("Can't happen! Can't load %s in dense ref manager.", childMetadataAddr)
				return err
			}

			err = rm.recursiveRelease(ns, childNode)
			if err != nil {
				return err
			}
		}

		fn.Release()
		delete(rm.refCount, metadataHash)
	} else {
		rm.refCount[metadataHash] = refCount - 1
	}

	return nil
}

func (rm *DenseRefManager) flatTake(ns *NameStore, fn FileNode) error {
	return nil
}

func (rm *DenseRefManager) flatRelease(ns *NameStore, fn FileNode) error {
	return nil
}

func (rm *DenseRefManager) cleanup(ns *NameStore) {
	log.Printf("DRM cleanup")

	ns.mtx.Lock()

	// First, identify all nodes with zero references
	nodesToRemove := make([]BlobAddr, 0)
	for metadataAddr, entry := range ns.fileCache {
		if entry.node == nil {
			continue
		}

		// Check if the node has zero references
		if entry.node.RefCount() == 0 {
			// Double check it's not in the RefManager structure
			metadataHash := entry.node.MetadataBlob().GetAddress()

			_, exists := rm.refCount[metadataHash]

			if exists {
				log.Panicf("Node %s has 0 refCount but exists in RefManager structure", metadataHash)
				continue
			}

			nodesToRemove = append(nodesToRemove, metadataAddr)
		}
	}
	ns.mtx.Unlock()

	// Now remove the nodes and release their storage (without holding the main lock)
	for _, metadataAddr := range nodesToRemove {
		ns.mtx.Lock()
		entry, exists := ns.fileCache[metadataAddr]
		ns.mtx.Unlock()

		if !exists || entry.node == nil {
			continue
		}

		node := entry.node

		metadataBlob := node.MetadataBlob()
		if metadataBlob != nil {
			metadataBlob.Release()
		}

		contentBlob, err := node.ExportedBlob()
		if err != nil {
			log.Printf("Couldn't load content for %s: %v", metadataAddr, err)
		} else if contentBlob != nil {
			contentBlob.Release()
		}

		// Remove from cache
		ns.mtx.Lock()
		delete(ns.fileCache, metadataAddr)
		ns.mtx.Unlock()
	}

	DebugLog(DebugBlobStorage, "NS cleanup complete. Removed %d unreferenced nodes", len(nodesToRemove))
}
