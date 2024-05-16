package grits

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
)

////////////////////////
// GNode

type GNode struct {
	IsDirectory bool             `json:"is_directory"`
	Contents    *FileContentAddr `json:"contents,omitempty"`

	ChildrenMap        *map[string]*GNode `json:"-"`
	ContentsCachedFile CachedFile         `json:"-"` // This field should not be serialized

	refCount       int        `json:"-"`
	metaCachedFile CachedFile `json:"-"` // This is handled manually

	blobStore BlobStore  `json:"-"`
	mtx       sync.Mutex `json:"-"`
}

func (gn *GNode) MetaBlob() (CachedFile, error) {
	err := gn.ensureSerialized()
	if err != nil {
		return nil, err
	}

	return gn.metaCachedFile, nil
}

func (gn *GNode) ContentsBlob() (CachedFile, error) {
	err := gn.ensureSerialized()
	if err != nil {
		return nil, err
	}

	return gn.ContentsCachedFile, nil
}

func (gn *GNode) Children() (map[string]*GNode, error) {
	if gn.IsDirectory {
		return *gn.ChildrenMap, nil
	} else {
		return nil, nil
	}
}

func (gn *GNode) Address() *GNodeAddr {
	return &GNodeAddr{BlobAddr: *gn.metaCachedFile.GetAddress()}
}

func (gn *GNode) Take() {
	gn.mtx.Lock()
	gn.refCount++
	gn.mtx.Unlock()
}

func (gn *GNode) Release() {
	gn.mtx.Lock()
	defer gn.mtx.Unlock()
	gn.refCount--
	if gn.refCount == 0 {
		if gn.metaCachedFile != nil {
			gn.metaCachedFile.Release()
		}
		for _, child := range *gn.ChildrenMap {
			child.Release()
		}
	}
}

func DebugPrintTree(node *GNode, indent string) {
	if node == nil {
		return
	}

	children, err := node.Children()
	if err != nil {
		log.Fatalf("Can't decode tree: %v", err)
	}
	if children == nil {
		return
	}

	for _, child := range children {
		DebugPrintTree(child, indent+"  ")
	}
}

////////////////////////
// NameStore

type NameStore struct {
	BlobStore BlobStore
	root      *GNode
	fileCache map[string]*GNode
	mtx       sync.RWMutex
}

func NewNameStore(bs BlobStore) (*NameStore, error) {
	rootChildren := make(map[string]*GNode)

	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[string]*GNode),
	}

	rootGNode, err := CreateTreeGNode(ns.BlobStore, rootChildren)
	if err != nil {
		return nil, err
	}
	ns.root = rootGNode

	return ns, nil
}

func (ns *NameStore) GetRoot() string {
	return ns.root.Address().String()
}

func (ns *NameStore) LookupAndOpen(name string) (CachedFile, error) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	gnode, err := ns.LookupNode(name)
	if err != nil {
		return nil, err
	}

	if gnode == nil {
		return nil, fmt.Errorf("%s not found", name)
	}

	gnode.Take()
	return gnode.ContentsCachedFile, nil
}

func (ns *NameStore) LookupNode(name string) (*GNode, error) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	nodes, err := ns.ResolvePath(name)
	if err != nil {
		return nil, err
	}

	node := nodes[len(nodes)-1]
	if node != nil {
		node.Take()
	}
	return node, nil
}

// FIXME - clean up this API a little

func (ns *NameStore) LookupFull(name string) ([][]string, error) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	name = strings.TrimRight(name, "/")
	nodes, err := ns.ResolvePath(name)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(name, "/")
	partialPath := ""

	response := make([][]string, 0, len(parts)+1) // +1 for the root
	response = append(response, []string{"", nodes[0].Address().String()})
	index := 1

	for _, part := range parts {
		if part == "" {
			continue
		}

		partialPath = filepath.Join(partialPath, part)
		node := nodes[index]
		if node == nil {
			return nil, fmt.Errorf("can't find %s", name)
		}
		// FIXME - we crash if the last node is nil
		response = append(response, []string{partialPath, nodes[index].Address().String()})
		index += 1
	}

	return response, nil
}

// Core lookup helper function.

func (ns *NameStore) ResolvePath(path string) ([]*GNode, error) {
	//log.Printf("We resolve path %s (root %v)\n", path, ns.root)

	path = strings.TrimRight(path, "/")
	if path != "" && path[0] == '/' {
		return nil, fmt.Errorf("paths must be relative")
	}

	parts := strings.Split(path, "/")
	node := ns.root

	response := make([]*GNode, 0, len(parts)+1)
	response = append(response, node)

	for _, part := range parts {
		if part == "" {
			continue
		}

		// If we hit a "does not exist" but we still have more of
		// the path to go, then it's an error
		if node == nil {
			return nil, fmt.Errorf("%s not found traversing %s", part, path)
		}

		if !node.IsDirectory {
			return nil, fmt.Errorf("%s is not a directory, traversing %s", part, path)
		}

		children, err := node.Children()
		if err != nil {
			return nil, err
		}

		childNode := children[part] // May not exist
		node = childNode
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
	Addr     *GNodeAddr
	PrevAddr *GNodeAddr
	Assert   uint32
}

func matchesAddr(a *GNode, b *GNodeAddr) bool {
	if a == nil {
		return b == nil
	} else {
		return b != nil && a.Address().String() == b.Hash
	}
}

func (ns *NameStore) MultiLink(requests []*LinkRequest) error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	for _, req := range requests {
		if req.Assert == 0 {
			continue
		}

		nodes, err := ns.ResolvePath(req.Path)
		if err != nil {
			return err
		}
		node := nodes[len(nodes)-1]

		if req.Assert&AssertPrevValueMatches != 0 {
			if !matchesAddr(node, req.PrevAddr) {
				return fmt.Errorf("prev value for %s is not %v", req.Path, req.PrevAddr)
			}
		}

		if req.Assert&AssertIsBlob != 0 {
			if node == nil || node.IsDirectory {
				return fmt.Errorf("prev value for %s isn't blob", req.Path)
			}
		}

		if req.Assert&AssertIsTree != 0 {
			if node == nil || !node.IsDirectory {
				return fmt.Errorf("prev value for %s isn't tree", req.Path)
			}
		}

		if req.Assert&AssertIsNonEmpty != 0 {
			if node == nil {
				return fmt.Errorf("prev value for %s is nil", req.Path)
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

		var err error
		newRoot, err = ns.recursiveLink(name, req.Addr, newRoot)
		if err != nil {
			return err
		}
	}

	if newRoot != nil {
		newRoot.Take()
	}
	if oldRoot != nil {
		oldRoot.Release()
	}

	ns.root = newRoot
	return nil
}

func (ns *NameStore) LinkGNode(name string, gnodeAddr *GNodeAddr) error {
	name = strings.TrimRight(name, "/")
	if name != "" && name[0] == '/' {
		return fmt.Errorf("name must be relative")
	}

	if name == "." {
		name = ""
	}

	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	newRoot, error := ns.recursiveLink(name, gnodeAddr, ns.root)
	if error != nil {
		return error
	}

	if newRoot != nil {
		newRoot.Take()
	}
	if ns.root != nil {
		ns.root.Release()
	}

	ns.root = newRoot
	return nil
}

func CreateBlobGNode(bs BlobStore, addr *FileContentAddr) (*GNode, error) {
	gnode := &GNode{
		IsDirectory: false,
		Contents:    addr,
		blobStore:   bs,
	}

	var err error
	gnode.ContentsCachedFile, err = bs.ReadFile(&addr.BlobAddr)
	if err != nil {
		return nil, err
	}

	serializedGNode, err := json.Marshal(gnode)
	if err != nil {
		return nil, fmt.Errorf("error serializing GNode: %v", err)
	}

	serializedGNodeCF, err := bs.AddDataBlock(serializedGNode)
	if err != nil {
		return nil, fmt.Errorf("error storing serialized GNode: %v", err)
	}

	gnode.metaCachedFile = serializedGNodeCF
	return gnode, nil
}

func CreateTreeGNode(bs BlobStore, children map[string]*GNode) (*GNode, error) {
	gnode := &GNode{
		IsDirectory: true,
		ChildrenMap: &children,
		blobStore:   bs,
	}

	// Translate children into a map of strings to strings
	childMap := make(map[string]string)
	for name, childNode := range children {
		if childNode.metaCachedFile == nil {
			return nil, fmt.Errorf("child node %s does not have a metadata cache file", name)
		}
		childMap[name] = childNode.metaCachedFile.GetAddress().Hash
	}

	// Serialize the map of children
	serializedChildMap, err := json.Marshal(childMap)
	if err != nil {
		return nil, fmt.Errorf("error serializing children map: %v", err)
	}

	// Store the serialized children map as a new data block
	contentsCachedFile, err := bs.AddDataBlock(serializedChildMap)
	if err != nil {
		return nil, fmt.Errorf("error storing serialized children map: %v", err)
	}

	// Set .Contents and .ContentsCachedFile based on the created data block
	gnode.ContentsCachedFile = contentsCachedFile
	gnode.Contents = &FileContentAddr{BlobAddr: *contentsCachedFile.GetAddress()}

	// Serialize the GNode itself now that it includes the contents address
	serializedGNode, err := json.Marshal(gnode)
	if err != nil {
		return nil, fmt.Errorf("error serializing GNode: %v", err)
	}

	// Store the serialized GNode as a new data block
	metaCachedFile, err := bs.AddDataBlock(serializedGNode)
	if err != nil {
		return nil, fmt.Errorf("error storing serialized GNode: %v", err)
	}
	gnode.metaCachedFile = metaCachedFile

	return gnode, nil
}

func (ns *NameStore) recursiveLink(name string, addr *GNodeAddr, oldParent *GNode) (*GNode, error) {
	parts := strings.SplitN(name, "/", 2)

	var oldChildren map[string]*GNode
	var err error
	if oldParent != nil {
		oldChildren, err = oldParent.Children()
		if err != nil {
			return nil, fmt.Errorf("couldn't get children for %s", name)
		}
		if oldChildren == nil {
			return nil, fmt.Errorf("non-directory in path %s", name)
		}
	} else {
		return nil, fmt.Errorf("attempting to link in nonexistent directory")
	}

	var newValue *GNode

	if len(parts) == 1 {
		if addr == nil {
			newValue = nil
		} else {
			var exists bool

			newValue, exists = ns.fileCache[addr.String()]
			if !exists {
				newValue, err = ns.LoadGNode(addr)
				if err != nil {
					return nil, err
				}
			}
		}

		if parts[0] == "" {
			return newValue, nil
		}
	} else {
		oldChild, exists := oldChildren[parts[0]]
		if !exists {
			return nil, fmt.Errorf("no such directory %s in path", parts[0])
		}

		newValue, err = ns.recursiveLink(parts[1], addr, oldChild)
		if err != nil {
			return nil, err
		}
	}

	newChildren := make(map[string]*GNode)
	for k, v := range oldChildren {
		newChildren[k] = v
		if k != parts[0] {
			v.Take()
		}
	}

	if newValue != nil {
		newChildren[parts[0]] = newValue
		newValue.Take()
	} else {
		delete(newChildren, parts[0])
	}

	result, err := CreateTreeGNode(ns.BlobStore, newChildren)
	if err != nil {
		for _, v := range newChildren {
			v.Release()
		}

		return nil, err
	}
	return result, nil
}

func EmptyNameStore(bs BlobStore) (*NameStore, error) {
	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[string]*GNode),
	}

	rootNodeMap := make(map[string]*GNode)
	dn, err := CreateTreeGNode(ns.BlobStore, rootNodeMap)
	if err != nil {
		return nil, err
	}

	ns.root = dn
	return ns, nil
}

func DeserializeNameStore(bs BlobStore, rootAddr *GNodeAddr) (*NameStore, error) {
	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[string]*GNode),
	}

	dn, err := ns.LoadGNode(rootAddr)
	if err != nil {
		return nil, fmt.Errorf("error loading root node: %v", err)
	}

	ns.root = dn
	ns.root.Take()

	return ns, nil
}

// Getting file nodes from a NameStore

func (ns *NameStore) LoadGNode(addr *GNodeAddr) (*GNode, error) {
	cf, err := ns.BlobStore.ReadFile(&addr.BlobAddr)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", addr.String(), err)
	}
	defer cf.Release()

	nodeData, err := cf.Read(0, cf.GetSize())
	if err != nil {
		return nil, err
	}

	gnode := &GNode{
		blobStore: ns.BlobStore,
	}

	if err := json.Unmarshal(nodeData, &gnode); err != nil {
		return nil, fmt.Errorf("error unmarshalling %s: %v", addr.String(), err)
	}

	contentCf, err := ns.BlobStore.ReadFile(&gnode.Contents.BlobAddr)
	if err != nil {
		return nil, err
	}
	defer contentCf.Release()
	gnode.ContentsCachedFile = contentCf

	gnode.metaCachedFile = cf
	gnode.refCount = 1

	if !gnode.IsDirectory {
		// Simple case, just a file blob

		ns.fileCache[addr.Hash] = gnode
		cf = nil
		contentCf = nil // Prevent release
		return gnode, nil
	} else {
		// Complex case, recursive directory load

		ns.fileCache[addr.Hash] = nil

		childMap := make(map[string]*GNode, 0)
		gnode.ChildrenMap = &childMap

		defer func() {
			if gnode != nil {
				delete(ns.fileCache, addr.String())
				for _, child := range *gnode.ChildrenMap {
					child.Release()
				}
			}
		}()

		// Use the new Read() method to read directory data
		dirData, err := gnode.ContentsCachedFile.Read(0, gnode.ContentsCachedFile.GetSize())
		if err != nil {
			return nil, fmt.Errorf("error reading directory data from %s: %v", addr.String(), err)
		}

		dirMap := make(map[string]string)
		if err := json.Unmarshal(dirData, &dirMap); err != nil {
			return nil, fmt.Errorf("error unmarshalling %s: %v", addr.String(), err)
		}

		for name, addrStr := range dirMap {
			cachedChild, exists := ns.fileCache[addrStr]
			if exists {
				(*gnode.ChildrenMap)[name] = cachedChild
				cachedChild.Take()
				continue
			}

			childAddr := NewGNodeAddr(addrStr)

			gn, err := ns.LoadGNode(childAddr)
			if err != nil {
				return nil, fmt.Errorf("error loading %s: %v", addrStr, err)
			}

			(*gnode.ChildrenMap)[name] = gn
			gn.Take()
		}

		ns.fileCache[addr.String()] = gnode
		resultGNode := gnode

		gnode = nil // Prevent cleanup
		cf = nil
		contentCf = nil

		return resultGNode, nil
	}
}

func (gn *GNode) ensureSerialized() error {
	if gn.ContentsCachedFile != nil {
		// Already serialized
		return nil
	}

	dirMap := make(map[string]string)
	for name, child := range *gn.ChildrenMap {
		dirMap[name] = child.Contents.Hash
	}

	dirData, err := json.Marshal(dirMap)
	if err != nil {
		return fmt.Errorf("error marshalling directory: %v", err)
	}

	cf, err := gn.blobStore.AddDataBlock(dirData)
	if err != nil {
		return fmt.Errorf("error writing directory: %v", err)
	}

	gn.ContentsCachedFile = cf

	return nil
}
