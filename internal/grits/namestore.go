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
// Basic structures

type GNode struct {
	IsDirectory bool            `json:"is_directory"`
	Contents    FileContentAddr `json:"contents,omitempty"` // Hash of the file content or directory

	ChildrenMap        *map[string]*GNode `json:"-"`
	ContentsCachedFile CachedFile         `json:"-"` // This field should not be serialized

	refCount       int        `json:"-"`
	metaCachedFile CachedFile `json:"-"` // This is handled manually

	blobStore BlobStore  `json:"-"`
	mtx       sync.Mutex `json:"-"`
}

type LinkRequest struct {
	Path     string    `json:"path"`
	Addr     GNodeAddr `json:"addr"`                // Address of the GNode
	PrevAddr GNodeAddr `json:"prev_addr,omitempty"` // Previous address of the GNode (for assertions)
	Assert   uint32    `json:"assert"`
}

type LinkResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

type Directory struct {
	Entries map[string]GNodeAddr `json:"entries"` // Map of name to GNode address
}

const (
	AssertPrevValueMatches = 1 << iota
	AssertIsBlob
	AssertIsTree
	AssertIsNonEmpty
)

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

func (gn *GNode) Address() GNodeAddr {
	return GNodeAddr(gn.metaCachedFile.GetAddress())
}

func (gn *GNode) Take() {
	gn.mtx.Lock()
	defer gn.mtx.Unlock()
	//log.Printf("  Taking reference: Before=%d, Address=%s\n", gn.refCount, gn.Address())
	gn.refCount++
	//log.Printf("  Taking reference: After=%d, Address=%s\n", gn.refCount, gn.Address())
}

func (gn *GNode) Release() {
	gn.mtx.Lock()
	defer gn.mtx.Unlock()
	//log.Printf("  Releasing reference: Before=%d, Address=%s\n", gn.refCount, gn.Address())
	gn.refCount--
	if gn.refCount == 0 {
		//log.Printf("  Reference count zero, cleaning up: Address=%s\n", gn.Address())
		if gn.metaCachedFile != nil {
			gn.metaCachedFile.Release()
		}
		if gn.ContentsCachedFile != nil {
			gn.ContentsCachedFile.Release()
		}
		if gn.ChildrenMap != nil {
			for _, child := range *gn.ChildrenMap {
				child.Release()
			}
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
	fileCache map[GNodeAddr]*GNode
	mtx       sync.RWMutex
}

func NewNameStore(bs BlobStore) (*NameStore, error) {
	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[GNodeAddr]*GNode),
	}

	rootGNode, err := ns.CreateEmptyTreeGNode()
	if err != nil {
		return nil, err
	}
	ns.root = rootGNode

	return ns, nil
}

func (ns *NameStore) GetRoot() GNodeAddr {
	return ns.root.Address()
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
	response = append(response, []string{"", string(nodes[0].Address())})
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
		response = append(response, []string{partialPath, string(nodes[index].Address())})
		index += 1
	}

	return response, nil
}

func (ns *NameStore) ResolvePath(path string) ([]*GNode, error) {
	path = strings.TrimRight(path, "/")
	if path != "" && path[0] == '/' {
		return nil, fmt.Errorf("paths must be relative")
	}

	parts := strings.Split(path, "/")
	node := ns.root

	response := make([]*GNode, 0, len(parts)+1)
	response = append(response, node)

	//log.Printf("Hello! We resolve %s\n", path)

	for _, part := range parts {
		//log.Printf("  part: %s\n", part)

		if part == "" {
			continue
		}

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

		childNode := children[part]
		node = childNode
		response = append(response, node)

		//log.Printf("    append: %v\n", node)
	}

	return response, nil
}

func matchesAddr(a *GNode, b GNodeAddr) bool {
	if a == nil {
		return b == ""
	} else {
		return b != "" && a.Address() == b
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

func (ns *NameStore) LinkGNode(name string, gnode *GNode) error {
	name = strings.TrimRight(name, "/")
	if name != "" && name[0] == '/' {
		return fmt.Errorf("name must be relative")
	}

	if name == "." {
		name = ""
	}

	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	var addr GNodeAddr
	if gnode != nil {
		addr = gnode.Address()
	} else {
		addr = ""
	}

	log.Printf("Okay we're linking %s\n", name)

	if gnode != nil {
		log.Printf("  refCount for %s before: %d\n", name, gnode.refCount)
	}

	newRoot, error := ns.recursiveLink(name, addr, ns.root)
	if error != nil {
		return error
	}

	if gnode != nil {
		log.Printf("  refCount for %s after: %d\n", name, gnode.refCount)
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

func (ns *NameStore) CreateBlobGNode(addr FileContentAddr) (*GNode, error) {
	gnode := &GNode{
		IsDirectory: false,
		Contents:    addr,
		blobStore:   ns.BlobStore,
		refCount:    1,
	}

	var err error
	cf, err := ns.BlobStore.ReadFile(BlobAddr(addr))
	if err != nil {
		return nil, err
	}
	defer cf.Release()

	serializedGNode, err := json.Marshal(gnode)
	if err != nil {
		return nil, fmt.Errorf("error serializing GNode: %v", err)
	}

	serializedGNodeCF, err := ns.BlobStore.AddDataBlock(serializedGNode)
	if err != nil {
		return nil, fmt.Errorf("error storing serialized GNode: %v", err)
	}

	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	existingGNode, ok := ns.fileCache[GNodeAddr(serializedGNodeCF.GetAddress())]
	if ok {
		serializedGNodeCF.Release()
		existingGNode.Take()
		return existingGNode, nil
	}
	ns.fileCache[GNodeAddr(serializedGNodeCF.GetAddress())] = gnode

	gnode.metaCachedFile = serializedGNodeCF
	gnode.ContentsCachedFile = cf
	cf = nil // Prevent cleanup

	return gnode, nil
}

func (ns *NameStore) CreateEmptyTreeGNode() (*GNode, error) {
	gnode := &GNode{
		IsDirectory: true,
		blobStore:   ns.BlobStore,
		refCount:    1,
	}

	// Translate children into a map of strings to strings
	childMap := make(map[string]*GNode)
	gnode.ChildrenMap = &childMap

	// Serialize the map of children
	serializedChildMap, err := json.Marshal(childMap)
	if err != nil {
		return nil, fmt.Errorf("error serializing children map: %v", err)
	}

	// Store the serialized children map as a new data block
	contentsCachedFile, err := ns.BlobStore.AddDataBlock(serializedChildMap)
	if err != nil {
		return nil, fmt.Errorf("error storing serialized children map: %v", err)
	}
	defer contentsCachedFile.Release()

	gnode.Contents = FileContentAddr(contentsCachedFile.GetAddress())

	// Serialize the GNode itself now that it includes the contents address
	serializedGNode, err := json.Marshal(gnode)
	if err != nil {
		return nil, fmt.Errorf("error serializing GNode: %v", err)
	}

	// Store the serialized GNode as a new data block
	metaCachedFile, err := ns.BlobStore.AddDataBlock(serializedGNode)
	if err != nil {
		return nil, fmt.Errorf("error storing serialized GNode: %v", err)
	}
	gnode.metaCachedFile = metaCachedFile

	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	previousGNode, ok := ns.fileCache[GNodeAddr(metaCachedFile.GetAddress())]
	if ok {
		metaCachedFile.Release()
		previousGNode.Take()
		return previousGNode, nil
	}
	ns.fileCache[GNodeAddr(metaCachedFile.GetAddress())] = gnode

	gnode.ContentsCachedFile = contentsCachedFile
	contentsCachedFile = nil // Prevent cleanup

	//log.Printf("Returning gnode!")

	return gnode, nil
}

func (ns *NameStore) createTreeGNode(children map[string]*GNode) (*GNode, error) {
	gnode := &GNode{
		IsDirectory: true,
		ChildrenMap: &children,
		blobStore:   ns.BlobStore,
		refCount:    1,
	}

	// Translate children into a map of strings to strings
	childMap := make(map[string]GNodeAddr)
	for name, childNode := range children {
		//log.Printf("-- Add child: %s\n", name)
		if childNode.metaCachedFile == nil {
			return nil, fmt.Errorf("child node %s does not have a metadata cache file", name)
		}
		childMap[name] = GNodeAddr(childNode.metaCachedFile.GetAddress())
	}

	// Serialize the map of children
	serializedChildMap, err := json.Marshal(childMap)
	if err != nil {
		return nil, fmt.Errorf("error serializing children map: %v", err)
	}

	// Store the serialized children map as a new data block
	contentsCachedFile, err := ns.BlobStore.AddDataBlock(serializedChildMap)
	if err != nil {
		return nil, fmt.Errorf("error storing serialized children map: %v", err)
	}
	defer contentsCachedFile.Release()

	gnode.Contents = FileContentAddr(contentsCachedFile.GetAddress())

	// Serialize the GNode itself now that it includes the contents address
	serializedGNode, err := json.Marshal(gnode)
	if err != nil {
		return nil, fmt.Errorf("error serializing GNode: %v", err)
	}

	// Store the serialized GNode as a new data block
	metaCachedFile, err := ns.BlobStore.AddDataBlock(serializedGNode)
	if err != nil {
		return nil, fmt.Errorf("error storing serialized GNode: %v", err)
	}
	gnode.metaCachedFile = metaCachedFile

	previousGNode, ok := ns.fileCache[GNodeAddr(metaCachedFile.GetAddress())]
	if ok {
		metaCachedFile.Release()
		previousGNode.Take()
		return previousGNode, nil
	}
	ns.fileCache[GNodeAddr(metaCachedFile.GetAddress())] = gnode

	for _, childNode := range children {
		childNode.Take()
	}

	gnode.ContentsCachedFile = contentsCachedFile
	contentsCachedFile = nil // Prevent cleanup

	//log.Printf("Returning gnode!")

	return gnode, nil
}

func (ns *NameStore) recursiveLink(name string, addr GNodeAddr, oldParent *GNode) (*GNode, error) {
	parts := strings.SplitN(name, "/", 2)

	var oldChildren map[string]*GNode
	var err error
	if oldParent != nil {
		oldChildren, err = oldParent.Children()
		if err != nil {
			return nil, fmt.Errorf("couldn't get children for %s: %s", name, err)
		}
		if oldChildren == nil {
			return nil, fmt.Errorf("non-directory in path %s", name)
		}
	} else {
		return nil, fmt.Errorf("attempting to link in nonexistent directory")
	}

	var newValue *GNode

	if len(parts) == 1 {
		if addr == "" {
			newValue = nil
		} else {
			var exists bool
			log.Printf("    Searching in fileCache for %s; has %d entries\n", addr, len(ns.fileCache))
			newValue, exists = ns.fileCache[addr]
			if exists {
				newValue.Take()
			} else {
				log.Printf("  failed to find\n")
				log.Printf("Loading GNode: %s\n", addr)
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
		//log.Printf("Recursing into: %s\n", parts[0])
		newValue, err = ns.recursiveLink(parts[1], addr, oldChild)
		if err != nil {
			return nil, err
		}
	}

	newChildren := make(map[string]*GNode)
	for k, v := range oldChildren {
		newChildren[k] = v
		if k != parts[0] {
			//log.Printf("Taking reference for existing child %s\n", k)
			v.Take()
		}
	}

	if newValue != nil {
		newChildren[parts[0]] = newValue
		//log.Printf("Linking new value for %s\n", parts[0])
		newValue.Take()
	} else {
		//log.Printf("Removing child: %s\n", parts[0])
		delete(newChildren, parts[0])
	}

	result, err := ns.createTreeGNode(newChildren)
	if err != nil {
		for _, v := range newChildren {
			log.Printf("Releasing reference due to error for %s\n", v.Address())
			v.Release()
		}

		return nil, err
	}
	return result, nil
}

func EmptyNameStore(bs BlobStore) (*NameStore, error) {
	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[GNodeAddr]*GNode),
	}

	dn, err := ns.CreateEmptyTreeGNode()
	if err != nil {
		return nil, err
	}

	ns.root = dn
	return ns, nil
}

func DeserializeNameStore(bs BlobStore, rootAddr GNodeAddr) (*NameStore, error) {
	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[GNodeAddr]*GNode),
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

func (ns *NameStore) LoadGNode(addr GNodeAddr) (*GNode, error) {
	cf, err := ns.BlobStore.ReadFile(BlobAddr(addr))
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", addr, err)
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
		return nil, fmt.Errorf("error unmarshalling %s: %v", addr, err)
	}

	contentCf, err := ns.BlobStore.ReadFile(BlobAddr(gnode.Contents))
	if err != nil {
		return nil, err
	}
	defer contentCf.Release()
	gnode.ContentsCachedFile = contentCf

	gnode.metaCachedFile = cf
	gnode.refCount = 1

	if !gnode.IsDirectory {
		// Simple case, just a file blob

		ns.fileCache[addr] = gnode
		log.Printf("Added to fileCache blob %s; has %d entries\n", addr, len(ns.fileCache))

		cf = nil
		contentCf = nil // Prevent release
		return gnode, nil
	} else {
		// Complex case, recursive directory load

		ns.fileCache[addr] = nil

		childMap := make(map[string]*GNode, 0)
		gnode.ChildrenMap = &childMap

		defer func() {
			if gnode != nil {
				delete(ns.fileCache, addr)
				for _, child := range *gnode.ChildrenMap {
					child.Release()
				}
			}
		}()

		// Use the new Read() method to read directory data
		dirData, err := gnode.ContentsCachedFile.Read(0, gnode.ContentsCachedFile.GetSize())
		if err != nil {
			return nil, fmt.Errorf("error reading directory data from %s: %v", addr, err)
		}

		dirMap := make(map[string]GNodeAddr)
		if err := json.Unmarshal(dirData, &dirMap); err != nil {
			return nil, fmt.Errorf("error unmarshalling %s: %v", addr, err)
		}

		for name, addrStr := range dirMap {
			cachedChild, exists := ns.fileCache[addrStr]
			if exists {
				(*gnode.ChildrenMap)[name] = cachedChild
				cachedChild.Take()
				continue
			}

			childAddr := addrStr

			gn, err := ns.LoadGNode(childAddr)
			if err != nil {
				return nil, fmt.Errorf("error loading %s: %v", addrStr, err)
			}

			(*gnode.ChildrenMap)[name] = gn
			gn.Take()
		}

		ns.fileCache[addr] = gnode
		log.Printf("Finalized add to fileCache for tree %s; has %d entries\n", addr, len(ns.fileCache))

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

	dirMap := make(map[string]GNodeAddr)
	for name, child := range *gn.ChildrenMap {
		dirMap[name] = GNodeAddr(child.metaCachedFile.GetAddress())
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
