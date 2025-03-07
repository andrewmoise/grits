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
)

// Notes for transitioning to on-demand serialization and general API cleanup:

// Link() can start to take a FileNode as the target, instead of an address. If you want to link
// by address, you need to fetch the FileNode for that address, then do your Link(), then release
// the ref count after.

// Same for MultiLink().

// LinkBlob() and LinkTree() should go away. Honestly? What that should look like instead is a
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

// LookupFull() should start to return nodes. We just need to take away the ".AddressString()"
// in it, and worry about reference counting.

// Likewise resolvePath() is already converted, nothing to do for now.

////////////////////////
// Node Types

type GNodeType int

const (
	GNodeTypeFile GNodeType = iota
	GNodeTypeDirectory
)

type GNodeMetadata struct {
	Type        GNodeType `json:"type"`
	Size        int64     `json:"size"`
	ContentAddr string    `json:"content_addr"` // CID of the actual content blob
}

type FileNode interface {
	ExportedBlob() CachedFile
	MetadataBlob() CachedFile
	Metadata() *GNodeMetadata
	Children() map[string]*BlobAddr
	AddressString() string
	Address() *TypedFileAddr

	Take()
	Release()
	RefCount() int
}

type TreeNode struct {
	blob         CachedFile
	metadataBlob CachedFile
	ChildrenMap  map[string]*BlobAddr
	refCount     int
	nameStore    *NameStore
	mtx          sync.Mutex
}

type BlobNode struct {
	blob         CachedFile
	metadataBlob CachedFile
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
	return &GNodeMetadata{
		Type:        GNodeTypeFile,
		Size:        bn.blob.GetSize(),
		ContentAddr: bn.blob.GetAddress().String(),
	}
}

func (bn *BlobNode) Children() map[string]*BlobAddr {
	return nil
}

// Still maintaining TypedFileAddr compatibility for external APIs
func (bn *BlobNode) AddressString() string {
	return fmt.Sprintf("blob:%s-%d", bn.blob.GetAddress().String(), bn.blob.GetSize())
}

func (bn *BlobNode) Address() *TypedFileAddr {
	return NewTypedFileAddr(bn.blob.GetAddress().Hash, bn.blob.GetSize(), Blob)
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

	// This is where we used to release the actual storage; now we're not doing that.
}

// FIXME - Maybe audit the callers of this, make sure they are synchronized WRT things that
// might cause take/release of references
func (bn *BlobNode) RefCount() int {
	return bn.refCount
}

// Implementations for TreeNode

func (tn *TreeNode) ExportedBlob() CachedFile {
	tn.ensureSerialized()
	return tn.blob
}

func (tn *TreeNode) MetadataBlob() CachedFile {
	return tn.metadataBlob
}

func (tn *TreeNode) Metadata() *GNodeMetadata {
	tn.ensureSerialized() // Need this to get the size
	return &GNodeMetadata{
		Type:        GNodeTypeDirectory,
		Size:        tn.blob.GetSize(),
		ContentAddr: tn.blob.GetAddress().String(),
	}
}

func (tn *TreeNode) Children() map[string]*BlobAddr {
	return tn.ChildrenMap
}

// Still maintaining TypedFileAddr compatibility for external APIs
func (tn *TreeNode) Address() *TypedFileAddr {
	tn.ensureSerialized()
	return NewTypedFileAddr(tn.blob.GetAddress().Hash, tn.blob.GetSize(), Tree)
}

func (tn *TreeNode) AddressString() string {
	tn.ensureSerialized()
	return fmt.Sprintf("tree:%s-%d", tn.blob.GetAddress().String(), tn.blob.GetSize())
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
			log.Panicf("couldn't load %s: %v", childAddr.String(), err)
		}
		ns.debugPrintTree(childNode, indent+"  ")
	}
}

////////////////////////
// Error sentinels

// Nonexistent files
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
// Pins

type Pin struct {
	path     string
	refCount map[string]int
}

func NewPin(path string) *Pin {
	return &Pin{
		path:     path,
		refCount: make(map[string]int),
	}
}

func (ns *NameStore) recursiveTake(pm *Pin, fn FileNode) error {
	if DebugRefCounts {
		log.Printf("Recursive take on %s %p: count %d/%d", fn.MetadataBlob().GetAddress(), fn, fn.RefCount(), pm.refCount[fn.AddressString()])
	}

	metadataHash := fn.MetadataBlob().GetAddress().String()

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
				return err
			}

			ns.recursiveTake(pm, childNode)
		}
	}

	return nil
}

func (ns *NameStore) recursiveRelease(pm *Pin, fn FileNode) error {
	metadataHash := fn.MetadataBlob().GetAddress().String()
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
				return err
			}

			ns.recursiveRelease(pm, childNode)
		}

		fn.Release()
		delete(pm.refCount, metadataHash)
	} else {
		pm.refCount[metadataHash] = refCount - 1
	}

	return nil
}

////////////////////////
// NameStore

type NameStore struct {
	BlobStore BlobStore
	root      FileNode
	fileCache map[string]FileNode
	mtx       sync.RWMutex

	watchers []FileTreeWatcher
	wmtx     sync.RWMutex // Separate mutex for watchers list to avoid lock contention

	rootPin *Pin
}

func (ns *NameStore) GetRoot() string {
	return ns.root.AddressString()
}

func (ns *NameStore) LookupAndOpen(name string) (CachedFile, error) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	nodes, err := ns.resolvePath(name)
	if err != nil {
		return nil, err
	}

	node := nodes[len(nodes)-1]
	if node == nil {
		return nil, ErrNotExist
	}

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

	nodes, err := ns.resolvePath(path)
	if err != nil {
		return nil, err
	}

	node := nodes[len(nodes)-1]
	if node == nil {
		return nil, ErrNotExist
	}

	node.Take()
	return node, nil
}

// FIXME - clean up this API a little

func (ns *NameStore) LookupFull(name string) ([][]string, error) {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	name = strings.TrimRight(name, "/")
	nodes, err := ns.resolvePath(name)
	if err != nil {
		return nil, err
	}
	if len(nodes) > 0 && nodes[len(nodes)-1] == nil {
		return nil, ErrNotExist
	}

	parts := strings.Split(name, "/")
	partialPath := ""

	response := make([][]string, 0, len(parts)+1) // +1 for the root
	response = append(response, []string{"", nodes[0].AddressString()})
	index := 1

	for _, part := range parts {
		if part == "" {
			continue
		}

		partialPath = filepath.Join(partialPath, part)
		node := nodes[index]
		if node == nil {
			return nil, ErrNotExist
		}
		// FIXME - we crash if the last node is nil
		response = append(response, []string{partialPath, nodes[index].AddressString()})
		index += 1
	}

	return response, nil
}

// FIXME - Clean up this API a lot.

// Get a FileNode from a metadata address, either from cache or loaded on demand.
// Takes a reference to the node before returning it.
func (ns *NameStore) GetFileNode(metadataAddr *BlobAddr) (FileNode, error) {
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

func (ns *NameStore) resolvePath(path string) ([]FileNode, error) {
	if DebugNameStore {
		log.Printf("We resolve path %s (root %v)\n", path, ns.root)
	}

	path = strings.TrimRight(path, "/")
	if path != "" && path[0] == '/' {
		return nil, fmt.Errorf("paths must be relative")
	}

	parts := strings.Split(path, "/")
	node := ns.root

	response := make([]FileNode, 0, len(parts)+1) // +1 for the root
	response = append(response, node)

	for n, part := range parts {
		//log.Printf("  part %s\n", part)

		if part == "" {
			continue
		}

		if node == nil {
			return nil, ErrNotExist
		}

		// Only TreeNodes have children to traverse
		treeNode, isTreeNode := node.(*TreeNode)
		if !isTreeNode {
			return nil, ErrNotDir
		}

		childAddr, exists := treeNode.ChildrenMap[part]
		if !exists {
			if n == len(parts)-1 {
				// Special case, last part nil is permissible
				response = append(response, nil)
				break
			} else {
				// All other times, it's an error
				return nil, ErrNotExist
			}
		}

		childNode, err := ns.loadFileNode(childAddr, true)
		if err != nil {
			return nil, err
		}

		node = childNode // Move to the next node in the path
		response = append(response, node)
	}

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
	Path     string
	Addr     *TypedFileAddr // We'll create a new metadata blob for this
	PrevAddr *BlobAddr      // Must be the existing metadata addr
	Assert   uint32
}

func matchesAddr(a FileNode, b *BlobAddr) bool {
	if a == nil {
		return b == nil
	} else {
		return b != nil && a.MetadataBlob().GetAddress().Equals(b)
	}
}

func (ns *NameStore) MultiLink(requests []*LinkRequest) error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	for _, req := range requests {
		if DebugLinks {
			log.Printf("Checking assertion: %d", req.Assert)
		}

		if req.Assert == 0 {
			continue
		}

		nodes, err := ns.resolvePath(req.Path)
		if err != nil {
			return err
		}
		node := nodes[len(nodes)-1]

		if req.Assert&AssertPrevValueMatches != 0 {
			if DebugLinks {
				log.Printf("  Prev value matches")
			}
			if !matchesAddr(node, req.PrevAddr) {
				return ErrAssertionFailed
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
				return ErrAssertionFailed
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
				return ErrAssertionFailed
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
				return ErrAssertionFailed
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
			return fmt.Errorf("name must be relative")
		}

		if name == "." {
			name = ""
		}

		var linkAddr *BlobAddr
		if req.Addr != nil {
			linkMetadataBlob, err := ns.typeToMetadata(req.Addr)
			if err != nil {
				return err
			}
			defer linkMetadataBlob.Release()
			linkAddr = linkMetadataBlob.GetAddress()
		} else {
			linkAddr = nil
		}

		var err error
		newRoot, err = ns.recursiveLink("", name, linkAddr, newRoot)
		if err != nil {
			return err
		}
	}

	err := ns.notifyWatchers("", oldRoot, newRoot)
	if err != nil {
		return err
	}

	if newRoot != nil {
		//newRoot.Take()
		ns.recursiveTake(ns.rootPin, newRoot)
	}

	if ns.root != nil {
		//ns.root.Release()
		ns.recursiveRelease(ns.rootPin, ns.root)
	}

	ns.root = newRoot
	return nil
}

func (ns *NameStore) Link(name string, addr *TypedFileAddr) error {
	name = strings.TrimRight(name, "/")
	if name != "" && name[0] == '/' {
		return fmt.Errorf("name must be relative")
	}

	if name == "." {
		name = ""
	}

	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	var metadataAddr *BlobAddr
	if addr != nil {
		metadataBlob, err := ns.typeToMetadata(addr)
		if err != nil {
			return err
		}
		metadataAddr = metadataBlob.GetAddress()
		defer metadataBlob.Release()
	}

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
		ns.recursiveTake(ns.rootPin, newRoot)
	}
	if ns.root != nil {
		//ns.root.Release()
		ns.recursiveRelease(ns.rootPin, ns.root)
	}

	ns.root = newRoot
	return nil
}

func (ns *NameStore) LinkBlob(name string, addr *BlobAddr, size int64) error {
	var tfa *TypedFileAddr

	if addr != nil {
		tfa = &TypedFileAddr{
			BlobAddr: *addr,
			Type:     Blob,
			Size:     size,
		}
	} else {
		tfa = nil
	}

	return ns.Link(name, tfa)
}

func (ns *NameStore) LinkTree(name string, addr *BlobAddr) error {
	var tfa *TypedFileAddr

	if addr != nil {
		tfa = &TypedFileAddr{
			BlobAddr: *addr,
			Type:     Tree,
		}
	} else {
		tfa = nil
	}

	return ns.Link(name, tfa)
}

// Core link function helper.

// We link in `metadataAddr` into place as `name` within `oldParent`, and return the
// modified version of `oldParent`.

// Core link function helper
func (ns *NameStore) recursiveLink(prevPath string, name string, metadataAddr *BlobAddr, oldParent FileNode) (FileNode, error) {
	if DebugNameStore {
		if metadataAddr != nil {
			log.Printf("We're trying to link %s under path %s /// %s\n", metadataAddr.String(), prevPath, name)
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

	var oldChildren map[string]*BlobAddr
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
			return nil, fmt.Errorf("can't load %s: %v", oldChildAddr.String(), err)
		}
	} else if len(parts) > 1 {
		// Directory doesn't exist while searching down in the path while doing a link
		return nil, ErrNotExist
	}

	if len(parts) == 1 {
		if metadataAddr == nil {
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

	newChildren := make(map[string]*BlobAddr)
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
		BlobStore: bs,
		fileCache: make(map[string]FileNode),
		rootPin:   NewPin(""),
	}

	rootNodeMap := make(map[string]*BlobAddr)
	root, err := ns.CreateTreeNode(rootNodeMap)
	if err != nil {
		return nil, err
	}

	//root.Take()
	ns.recursiveTake(ns.rootPin, root)

	log.Printf("Done setting up root (%s). Ref count: %d / %d",
		root.AddressString(), root.refCount, ns.rootPin.refCount[root.AddressString()])

	ns.root = root
	return ns, nil
}

func DeserializeNameStore(bs BlobStore, rootAddr *TypedFileAddr) (*NameStore, error) {
	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[string]FileNode),
		rootPin:   NewPin(""),
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
	ns.recursiveTake(ns.rootPin, root)

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
			ContentAddr: addr.BlobAddr.String(),
		}
	} else {
		metadata = GNodeMetadata{
			Type:        GNodeTypeDirectory,
			Size:        addr.Size,
			ContentAddr: addr.BlobAddr.String(),
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

// This metadataAddr that is the index, it is

func (ns *NameStore) loadFileNode(metadataAddr *BlobAddr, printDebug bool) (FileNode, error) {
	if printDebug && DebugFileCache {
		log.Printf("We try to chase down %s\n", metadataAddr.String())
	}

	cachedNode, exists := ns.fileCache[metadataAddr.String()]
	if exists {
		if printDebug && DebugFileCache {
			log.Printf("  found: %p", cachedNode)
		}
		return cachedNode, nil
	}

	// If not, we have to load it. First load and parse the metadata.
	metadataCf, err := ns.BlobStore.ReadFile(metadataAddr)
	if err != nil {
		return nil, fmt.Errorf("error reading metadata %s: %v", metadataAddr.String(), err)
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
	contentAddr := NewBlobAddr(metadata.ContentAddr)
	contentCf, err := ns.BlobStore.ReadFile(contentAddr)
	if err != nil {
		metadataCf.Release()
		return nil, fmt.Errorf("error reading content: %v", err)
	}

	//log.Printf("We got it. The content addr is %s\n", contentAddr.String())

	if metadata.Type == GNodeTypeFile {
		bn := &BlobNode{
			blob:         contentCf,
			metadataBlob: metadataCf,
			refCount:     0,
		}
		if printDebug && DebugFileCache {
			log.Printf("  created blob: %p", bn)
		}

		ns.fileCache[metadataAddr.String()] = bn // Now using metadata addr as cache key
		return bn, nil
	} else {
		ns.fileCache[metadataAddr.String()] = nil

		dn := &TreeNode{
			blob:         contentCf,
			metadataBlob: metadataCf,
			ChildrenMap:  make(map[string]*BlobAddr),
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
				delete(ns.fileCache, metadataAddr.String())
			}
		}()

		dirData, err := contentCf.Read(0, contentCf.GetSize())
		if err != nil {
			return nil, fmt.Errorf("error reading directory data: %v", err)
		}

		//log.Printf("We check contents of %s: %s\n", contentAddr.String(), string(dirData))

		dirMap := make(map[string]string)
		if err := json.Unmarshal(dirData, &dirMap); err != nil {
			return nil, fmt.Errorf("error parsing directory: %v", err)
		}

		for name, childMetadataCID := range dirMap {
			dn.ChildrenMap[name] = &BlobAddr{Hash: childMetadataCID}
		}

		ns.fileCache[metadataAddr.String()] = dn
		if printDebug && DebugFileCache {
			log.Printf("  putting in file cache at %s", metadataAddr.String())
		}
		resultDn := dn
		dn = nil // Prevent deferred cleanup + removal from file cache
		return resultDn, nil
	}
}

func (ns *NameStore) CreateTreeNode(children map[string]*BlobAddr) (*TreeNode, error) {
	return ns.createTreeNode(children, true)
}

// Same as CreateTreeNode() but with no locking
func (ns *NameStore) createTreeNode(children map[string]*BlobAddr, takeLock bool) (*TreeNode, error) {
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
	dirMap := make(map[string]string)
	for name, childAddr := range children {
		dirMap[name] = childAddr.String()
	}

	dirData, err := json.Marshal(dirMap)
	if err != nil {
		return nil, fmt.Errorf("error marshalling directory: %v", err)
	}

	contentBlob, err := ns.BlobStore.AddDataBlock(dirData)
	if err != nil {
		return nil, fmt.Errorf("error writing directory: %v", err)
	}

	//log.Printf("Added content blob, addr %s, rc %d", contentBlob.GetAddress().String(), contentBlob.GetRefCount())

	//ns.BlobStore.DumpStats()

	// Create metadata
	metadata := &GNodeMetadata{
		Type:        GNodeTypeDirectory,
		Size:        int64(len(dirData)),
		ContentAddr: contentBlob.GetAddress().String(),
	}

	metadataData, err := json.Marshal(metadata)
	if err != nil {
		contentBlob.Release()
		return nil, fmt.Errorf("error marshalling metadata: %v", err)
	}

	metadataBlob, err := ns.BlobStore.AddDataBlock(metadataData)
	if err != nil {
		contentBlob.Release()
		return nil, fmt.Errorf("error writing metadata: %v", err)
	}

	//log.Printf("Added metadata blob, addr %s, rc %d", metadataBlob.GetAddress().String(), metadataBlob.GetRefCount())

	tn.blob = contentBlob
	tn.metadataBlob = metadataBlob

	if takeLock {
		ns.mtx.Lock()
		defer ns.mtx.Unlock()
	}

	existingNode, exists := ns.fileCache[tn.metadataBlob.GetAddress().String()]
	if exists {
		contentBlob.Release()
		metadataBlob.Release()

		existingTreeNode, ok := existingNode.(*TreeNode)
		if !ok {
			return nil, fmt.Errorf("non tree node found in file cache for tree node %s", tn.metadataBlob.GetAddress())
		}

		return existingTreeNode, nil
	} else {
		ns.fileCache[tn.metadataBlob.GetAddress().String()] = tn
		return tn, nil
	}
}

func (ns *NameStore) CreateBlobNode(ba *BlobAddr) (*BlobNode, error) {
	return ns.createBlobNode(ba, true)
}

func (ns *NameStore) createBlobNode(fa *BlobAddr, takeLock bool) (*BlobNode, error) {
	contentBlob, err := ns.BlobStore.ReadFile(fa)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", fa.String(), err)
	}

	metadata := &GNodeMetadata{
		Type:        GNodeTypeFile,
		Size:        contentBlob.GetSize(),
		ContentAddr: contentBlob.GetAddress().String(),
	}

	metadataData, err := json.Marshal(metadata)
	if err != nil {
		contentBlob.Release()
		return nil, fmt.Errorf("error marshalling metadata: %v", err)
	}

	metadataBlob, err := ns.BlobStore.AddDataBlock(metadataData)
	if err != nil {
		contentBlob.Release()
		return nil, fmt.Errorf("error writing metadata: %v", err)
	}

	if takeLock {
		ns.mtx.Lock()
		defer ns.mtx.Unlock()
	}

	existingNode, exists := ns.fileCache[metadataBlob.GetAddress().String()]
	if exists {
		contentBlob.Release()
		metadataBlob.Release()

		existingBlobNode, ok := existingNode.(*BlobNode)
		if !ok {
			return nil, fmt.Errorf("non blob node found in file cache for blob node %s", metadataBlob.GetAddress())
		}

		return existingBlobNode, nil
	}

	bn := &BlobNode{
		blob:         contentBlob,
		metadataBlob: metadataBlob,
		refCount:     1,
	}
	return bn, nil
}

func (tn *TreeNode) ensureSerialized() error {
	if tn.blob != nil {
		return nil
	}

	// Directory format is now filename => metadata CID
	dirMap := make(map[string]string)
	for name, childAddr := range tn.Children() {
		dirMap[name] = childAddr.String()
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
				log.Panicf("couldn't load %s: %v", childAddr.String(), err)
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
	seenBlobs := make(map[string]bool)

	fmt.Println("=== Reference Count Debugging ===")
	fmt.Println("Root node:", ns.GetRoot())

	// Start recursive walk from root
	ns.debugRefCountsWalk("", ns.root, seenBlobs)

	// Now check for orphaned blobs
	fmt.Println("\n=== Orphaned Blobs ===")

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
					fmt.Printf("Orphaned blob: %s\n", hash)
					fmt.Printf("  Size: %d bytes\n", file.Size)
					fmt.Printf("  RefCount: %d\n", file.RefCount)
					fmt.Printf("  Path: %s\n", file.Path)

					// For small blobs, print content for debugging
					if file.Size <= 200 {
						data, err := file.Read(0, file.Size)
						if err != nil {
							fmt.Printf("  Contents: <error reading: %v>\n", err)
						} else {
							fmt.Printf("  Contents: %s\n", string(data))
						}
					}
					fmt.Println()
				}
			}
		}

		if orphanCount >= maxOrphans {
			fmt.Printf("  (%d orphans not shown)", orphanCount-maxOrphans+1)
		}
		fmt.Printf("Total orphaned blobs: %d (%.2f MB)\n", orphanCount, float64(totalSize)/1024/1024)
	} else {
		fmt.Println("BlobStore is not a LocalBlobStore, cannot check for orphaned blobs")
	}

	fmt.Println("=== End Reference Count Debugging ===")

	return nil
}

// Helper function to recursively walk the tree
func (ns *NameStore) debugRefCountsWalk(path string, node FileNode, seenBlobs map[string]bool) {
	if node == nil {
		// ??? Can't happen
		log.Printf("%s: <nil>\n", path)
		return
	}

	contentBlob := node.ExportedBlob()
	metadataBlob := node.MetadataBlob()

	// Mark these blobs as seen
	seenBlobs[contentBlob.GetAddress().Hash] = true
	seenBlobs[metadataBlob.GetAddress().Hash] = true

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
		log.Printf("  Content Blob Hash: %s\n", contentBlob.GetAddress().Hash)
		log.Printf("  Content Blob RefCount: %d\n", contentRefCount)
		log.Printf("  Metadata Blob Hash: %s\n", metadataBlob.GetAddress().Hash)
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
				fmt.Printf("  ERROR loading child %s: %v\n", childName, err)
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

	fmt.Println("=== NAMESPACE DUMP ===")
	fmt.Printf("Root node: %s (%p)\n\n", ns.GetRoot(), ns.root)

	if ns.root == nil {
		fmt.Println("Root is nil, nothing to dump")
		return
	}

	// Start recursive DFS from the root
	ns.debugDumpNode("", ns.root)

	fmt.Println("=== END NAMESPACE DUMP ===")
}

func (ns *NameStore) debugDumpNode(path string, node FileNode) {
	if node == nil {
		fmt.Printf("%s: <nil>\n", path)
		return
	}

	// Get blobs and metadata
	contentBlob := node.ExportedBlob()
	metadataBlob := node.MetadataBlob()

	// Print node details
	fmt.Printf("%s\n", path)
	fmt.Printf("  Memory address: %p\n", node)
	fmt.Printf("  Node type: %T\n", node)

	// Print reference counts
	nodeRefCount := node.RefCount()
	rootPinRefCount := 0
	if metadataHash := metadataBlob.GetAddress().String(); metadataHash != "" {
		rootPinRefCount = ns.rootPin.refCount[metadataHash]
	}

	fmt.Printf("  Reference count: %d (object) / %d (rootPin)\n", nodeRefCount, rootPinRefCount)

	// Print blob hashes
	fmt.Printf("  Content blob hash: %s\n", contentBlob.GetAddress().String())
	fmt.Printf("  Metadata blob hash: %s\n", metadataBlob.GetAddress().String())

	// For TreeNodes, print child count
	if children := node.Children(); children != nil {
		fmt.Printf("  Child count: %d\n", len(children))

		// Sort children names for consistent output
		names := make([]string, 0, len(children))
		for name := range children {
			names = append(names, name)
		}
		sort.Strings(names)

		// Print child names
		if len(names) > 0 {
			fmt.Println("  Children:")
			for _, childName := range names {
				fmt.Printf("  - %s\n", childName)
			}
		}

		// Recursively process each child
		for _, childName := range names {
			childAddr := children[childName]
			childNode, err := ns.loadFileNode(childAddr, false)
			if err != nil {
				fmt.Printf("  ERROR loading child %s: %v\n", childName, err)
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
				fmt.Printf("  Content preview: <error reading: %v>\n", err)
			} else {
				fmt.Printf("  Content preview: %s\n", string(data))
			}
		} else {
			fmt.Printf("  Content too large to preview (%d bytes)\n", contentBlob.GetSize())
		}
	}
}

// CleanupUnreferencedNodes removes all zero-reference nodes from the fileCache
// and releases their underlying storage.
func (ns *NameStore) CleanupUnreferencedNodes() {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	//log.Printf("Starting cleanup of unreferenced nodes")
	nodesToRemove := make([]string, 0)

	// First, identify all nodes with zero references
	for metadataAddr, node := range ns.fileCache {
		// Check if the node has zero references
		if node.RefCount() == 0 {
			// Double check it's not in the pin structure
			metadataHash := node.MetadataBlob().GetAddress().String()
			_, exists := ns.rootPin.refCount[metadataHash]
			if exists {
				log.Panicf("Node %s has 0 refCount but exists in pin structure", metadataHash)
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
