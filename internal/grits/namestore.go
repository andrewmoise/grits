// This is the new file, after revisions.

package grits

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

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
	Children() map[string]FileNode
	AddressString() string
	Address() *TypedFileAddr

	Take()
	Release()
	RefCount() int
}

type TreeNode struct {
	blob         CachedFile
	metadataBlob CachedFile
	ChildrenMap  map[string]FileNode
	refCount     int
	nameStore    *NameStore
}

type BlobNode struct {
	blob         CachedFile
	metadataBlob CachedFile
	refCount     int
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

func (bn *BlobNode) Children() map[string]FileNode {
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
	bn.refCount++
}

func (bn *BlobNode) Release() {
	bn.refCount--
	if bn.refCount == 0 {
		if bn.blob != nil {
			bn.blob.Release()
		}
		if bn.metadataBlob != nil {
			bn.metadataBlob.Release()
		}
	}
}

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

func (tn *TreeNode) Children() map[string]FileNode {
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
	tn.refCount++
}

func (tn *TreeNode) Release() {
	tn.refCount--
	if tn.refCount == 0 {
		if tn.blob != nil {
			tn.blob.Release()
		}
		if tn.metadataBlob != nil {
			tn.metadataBlob.Release()
		}
		for _, child := range tn.ChildrenMap {
			child.Release()
		}
	}
}

func (tn *TreeNode) RefCount() int {
	return tn.refCount
}

func DebugPrintTree(node FileNode, indent string) {
	if node == nil {
		return
	}

	children := node.Children()
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
	root      FileNode
	fileCache map[string]FileNode
	mtx       sync.RWMutex
}

func (ns *NameStore) GetRoot() string {
	return ns.root.AddressString()
}

func (ns *NameStore) LookupAndOpen(name string) (CachedFile, error) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	nodes, err := ns.resolvePath(name)
	if err != nil {
		return nil, err
	}

	node := nodes[len(nodes)-1]
	if node == nil {
		return nil, fmt.Errorf("%s not found", name)
	}

	cf := node.ExportedBlob()
	cf.Take()
	return cf, nil
}

func (ns *NameStore) LookupNode(name string) (FileNode, error) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	nodes, err := ns.resolvePath(name)
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
	nodes, err := ns.resolvePath(name)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("can't find %s", name)
		}
		// FIXME - we crash if the last node is nil
		response = append(response, []string{partialPath, nodes[index].AddressString()})
		index += 1
	}

	return response, nil
}

// Core lookup helper function.

func (ns *NameStore) resolvePath(path string) ([]FileNode, error) {
	//log.Printf("We resolve path %s (root %v)\n", path, ns.root)

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
			return nil, fmt.Errorf("%s not not found traversing %s", part, path)
		}

		// Only TreeNodes have children to traverse
		treeNode, isTreeNode := node.(*TreeNode)
		if !isTreeNode {
			if n > 0 {
				return nil, fmt.Errorf("%s is not a directory, traversing %s", parts[n-1], path)
			} else {
				return nil, fmt.Errorf("%s is not a directory, traversing %s", "(root)", path)
			}
		}

		childNode := treeNode.ChildrenMap[part]
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
	Addr     *TypedFileAddr
	PrevAddr *TypedFileAddr
	Assert   uint32
}

func matchesAddr(a FileNode, b *TypedFileAddr) bool {
	if a == nil {
		return b == nil
	} else {
		return b != nil && a.Address().Equals(&b.BlobAddr)
	}
}

func (ns *NameStore) MultiLink(requests []*LinkRequest) error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	for _, req := range requests {
		if req.Assert == 0 {
			continue
		}

		nodes, err := ns.resolvePath(req.Path)
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
			if node == nil || node.Address().Type != Blob {
				return fmt.Errorf("prev value for %s isn't blob", req.Path)
			}
		}

		if req.Assert&AssertIsTree != 0 {
			if node == nil || node.Address().Type != Tree {
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

	// FIXME - We need to defer a newRoot release here, in case of error

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
		newRoot, err = ns.recursiveLink(name, linkAddr, newRoot)
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

	newRoot, err := ns.recursiveLink(name, metadataAddr, ns.root)
	if err != nil {
		return err
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

// We link in `addr` into place as `name` within `oldParent`, and return the
// modified version of `oldParent`.

// Core link function helper
func (ns *NameStore) recursiveLink(name string, metadataAddr *BlobAddr, oldParent FileNode) (FileNode, error) {
	//if metadataAddr != nil {
	//	log.Printf("We're trying to link %s under path %s\n", metadataAddr.String(), name)
	//} else {
	//	log.Printf("We're trying to link nil under path %s\n", name)
	//}

	parts := strings.SplitN(name, "/", 2)

	var oldChildren map[string]FileNode
	if oldParent != nil {
		oldChildren = oldParent.Children()
		if oldChildren == nil {
			return nil, fmt.Errorf("non-directory in path %s", name)
		}
	} else {
		return nil, fmt.Errorf("attempting to link in nonexistent directory")
	}

	var newValue FileNode
	var err error

	if len(parts) == 1 {
		if metadataAddr == nil {
			newValue = nil
		} else {
			var exists bool
			newValue, exists = ns.fileCache[metadataAddr.String()]
			if !exists {
				newValue, err = ns.loadFileNode(metadataAddr)
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

		newValue, err = ns.recursiveLink(parts[1], metadataAddr, oldChild)
		if err != nil {
			return nil, err
		}
	}

	newChildren := make(map[string]FileNode)
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

	result, err := ns.CreateTreeNode(newChildren)
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
		fileCache: make(map[string]FileNode),
	}

	rootNodeMap := make(map[string]FileNode)
	dn, err := ns.CreateTreeNode(rootNodeMap)
	if err != nil {
		return nil, err
	}

	ns.root = dn
	return ns, nil
}

func DeserializeNameStore(bs BlobStore, rootAddr *TypedFileAddr) (*NameStore, error) {
	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[string]FileNode),
	}

	rootCf, err := ns.typeToMetadata(rootAddr)
	if err != nil {
		return nil, fmt.Errorf("error loading root node: %v", err)
	}
	defer rootCf.Release()

	// Check if we already have this node cached
	if root, exists := ns.fileCache[rootCf.GetAddress().String()]; exists {
		ns.root = root
		return ns, nil
	}

	// If not, load and set it up
	root, err := ns.loadFileNode(rootCf.GetAddress())
	if err != nil {
		return nil, err
	}

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

// Convert to private function that takes a metadata blob address
func (ns *NameStore) loadFileNode(metadataAddr *BlobAddr) (FileNode, error) {
	//log.Printf("We try to chase down %s\n", metadataAddr.String())

	// First load and parse the metadata
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
		ns.fileCache[metadataAddr.String()] = bn // Now using metadata addr as cache key
		return bn, nil
	} else {
		ns.fileCache[metadataAddr.String()] = nil

		dn := &TreeNode{
			blob:         contentCf,
			metadataBlob: metadataCf,
			ChildrenMap:  make(map[string]FileNode),
			nameStore:    ns,
		}

		defer func() { // In case of error
			if dn != nil {
				delete(ns.fileCache, metadataAddr.String())
				for _, child := range dn.ChildrenMap {
					child.Release()
				}
			}
		}()

		// Rest stays mostly the same, but no need to create temp TypedFileAddr
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
			cachedChild, exists := ns.fileCache[childMetadataCID]
			if exists {
				dn.ChildrenMap[name] = cachedChild
				cachedChild.Take()
				continue
			}

			childMetadataAddr := NewBlobAddr(childMetadataCID)
			fn, err := ns.loadFileNode(childMetadataAddr)
			if err != nil {
				return nil, fmt.Errorf("error loading child node: %v", err)
			}

			dn.ChildrenMap[name] = fn
			fn.Take()
		}

		ns.fileCache[metadataAddr.String()] = dn
		resultDn := dn
		dn = nil // Prevent deferred cleanup / release
		return resultDn, nil
	}
}

func (ns *NameStore) CreateTreeNode(children map[string]FileNode) (*TreeNode, error) {
	//log.Printf("Creating tree node for map with %d children", len(children))

	//ns.BlobStore.DumpStats()

	tn := &TreeNode{
		ChildrenMap: children,
		nameStore:   ns,
	}

	// Create and serialize the directory listing
	dirMap := make(map[string]string)
	for name, child := range children {
		dirMap[name] = child.MetadataBlob().GetAddress().String()
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
	return tn, nil
}

func (ns *NameStore) CreateBlobNode(fa *BlobAddr) (*BlobNode, error) {
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
	for name, child := range tn.Children() {
		dirMap[name] = child.MetadataBlob().GetAddress().String()
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
			ns.dumpTreeNode(indent+"  ", children[childName], childName)
		}
	}
}
