package grits

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

////////////////////////
// Node Types

type FileNode interface {
	ExportedBlob() *CachedFile
	Children() map[string]FileNode
	AddressString() string
	Address() *TypedFileAddr
	Inode() uint64

	Take()
	Release()
}

type TreeNode struct {
	blob        *CachedFile
	ChildrenMap map[string]FileNode
	inode       uint64 // Add inode number field
	refCount    int
	nameStore   *NameStore
}

type BlobNode struct {
	blob     *CachedFile
	inode    uint64 // Add inode number field
	refCount int
}

// Implementations for BlobNode

func (bn *BlobNode) ExportedBlob() *CachedFile {
	return bn.blob
}

func (bn *BlobNode) Children() map[string]FileNode {
	return nil
}

func (bn *BlobNode) AddressString() string {
	return "blob:" + bn.blob.Address.String()
}

func (bn *BlobNode) Address() *TypedFileAddr {
	return NewTypedFileAddr(bn.blob.Address.Hash, bn.blob.Address.Size, Blob)
}

func (bn *BlobNode) Inode() uint64 {
	return bn.inode
}

func (bn *BlobNode) Take() {
	bn.refCount++
}

func (bn *BlobNode) Release() {
	bn.refCount--
	if bn.refCount == 0 {
		bn.blob.Release()
	}
}

// Implementations for TreeNode

func (tn *TreeNode) ExportedBlob() *CachedFile {
	tn.ensureSerialized()
	return tn.blob
}

func (tn *TreeNode) Children() map[string]FileNode {
	return tn.ChildrenMap
}

func (tn *TreeNode) Address() *TypedFileAddr {
	tn.ensureSerialized()
	return NewTypedFileAddr(tn.blob.Address.Hash, tn.blob.Address.Size, Tree)
}

func (tn *TreeNode) AddressString() string {
	tn.ensureSerialized()
	return "tree:" + tn.blob.Address.String()
}

func (tn *TreeNode) Inode() uint64 {
	return tn.inode
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
		for _, child := range tn.ChildrenMap {
			child.Release()
		}
	}
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
	BlobStore  *BlobStore
	root       FileNode
	fileCache  map[string]FileNode
	inodeIndex uint64 // inode index to assign inode numbers sequentially
	mtx        sync.RWMutex
}

func (ns *NameStore) GetRoot() string {
	return ns.root.AddressString()
}

func (ns *NameStore) LookupAndOpen(name string) (*CachedFile, error) {
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
	log.Printf("We resolve path %s (root %v)\n", path, ns.root)

	path = strings.TrimRight(path, "/")
	if path != "" && path[0] == '/' {
		return nil, fmt.Errorf("paths must be relative")
	}

	parts := strings.Split(path, "/")
	node := ns.root

	response := make([]FileNode, 0, len(parts)+1) // +1 for the root
	response = append(response, node)

	for _, part := range parts {
		log.Printf("  part %s\n", part)

		if part == "" {
			continue
		}

		if node == nil {
			return nil, fmt.Errorf("%s not not found traversing %s", part, path)
		}

		// Only TreeNodes have children to traverse
		treeNode, isTreeNode := node.(*TreeNode)
		if !isTreeNode {
			return nil, fmt.Errorf("%s is not a directory, traversing %s", part, path)
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

	newRoot, error := ns.recursiveLink(name, addr, ns.root)
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

func (ns *NameStore) LinkBlob(name string, addr *BlobAddr) error {
	var tfa *TypedFileAddr

	if addr != nil {
		tfa = &TypedFileAddr{
			BlobAddr: *addr,
			Type:     Blob,
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

// We're linking in `addr` into place as `name` within `oldParent`, and return the
// modified version of `oldParent`.

func (ns *NameStore) recursiveLink(name string, addr *TypedFileAddr, oldParent FileNode) (FileNode, error) {
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
		if addr == nil {
			newValue = nil
		} else {
			var exists bool

			newValue, exists = ns.fileCache[addr.String()]
			if !exists {
				newValue, err = ns.LoadFileNode(addr)
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

func EmptyNameStore(bs *BlobStore) (*NameStore, error) {
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

func DeserializeNameStore(bs *BlobStore, rootAddr *TypedFileAddr) (*NameStore, error) {
	ns := &NameStore{
		BlobStore: bs,
		fileCache: make(map[string]FileNode),
	}

	dn, err := ns.LoadFileNode(rootAddr)
	if err != nil {
		return nil, fmt.Errorf("error loading root node: %v", err)
	}

	ns.root = dn
	ns.root.Take()

	return ns, nil
}

// Getting file nodes from a NameStore

func (ns *NameStore) LoadFileNode(addr *TypedFileAddr) (FileNode, error) {
	cf, err := ns.BlobStore.ReadFile(&addr.BlobAddr)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", addr.String(), err)
	}

	if addr.Type == Blob {
		bn := &BlobNode{blob: cf}
		ns.fileCache[addr.String()] = bn
		return bn, nil
	} else {
		ns.fileCache[addr.String()] = nil

		dn := &TreeNode{
			blob:        cf,
			ChildrenMap: make(map[string]FileNode),
			nameStore:   ns,
		}

		dirFile, err := ns.BlobStore.ReadFile(&addr.BlobAddr)
		if err != nil {
			delete(ns.fileCache, addr.String())
			return nil, fmt.Errorf("error reading %s: %v", addr.String(), err)
		}
		defer func() {
			if dn != nil {
				delete(ns.fileCache, addr.String())
				for _, child := range dn.ChildrenMap {
					child.Release()
				}
			}
		}()

		dirData, err := os.ReadFile(dirFile.Path)
		if err != nil {
			return nil, fmt.Errorf("error reading %s: %v", addr.String(), err)
		}

		dirMap := make(map[string]string)
		if err := json.Unmarshal(dirData, &dirMap); err != nil {
			return nil, fmt.Errorf("error unmarshalling %s: %v", addr.String(), err)
		}

		for name, addrStr := range dirMap {
			cachedChild, exists := ns.fileCache[addrStr]
			if exists {
				dn.ChildrenMap[name] = cachedChild
				cachedChild.Take()

				continue
			}

			addr, err := NewTypedFileAddrFromString(addrStr)
			if err != nil {
				return nil, fmt.Errorf("error parsing address %s: %v", addrStr, err)
			}

			fn, err := ns.LoadFileNode(addr)
			if err != nil {
				return nil, fmt.Errorf("error loading %s: %v", addrStr, err)
			}

			dn.ChildrenMap[name] = fn
			fn.Take()
		}

		ns.fileCache[addr.String()] = dn
		resultDn := dn

		dn = nil
		return resultDn, nil
	}
}

func (ns *NameStore) nextInode() uint64 {
	return atomic.AddUint64(&ns.inodeIndex, 1) // atomically increment the inode index
}

func (ns *NameStore) CreateTreeNode(children map[string]FileNode) (*TreeNode, error) {
	tn := &TreeNode{
		ChildrenMap: children,
		inode:       ns.nextInode(), // Assign an inode number
		nameStore:   ns,
	}
	return tn, nil
}

func (ns *NameStore) CreateBlobNode(fa *BlobAddr) (*BlobNode, error) {
	cf, err := ns.BlobStore.ReadFile(fa)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", fa.String(), err)
	}
	bn := &BlobNode{
		blob:     cf,
		inode:    ns.nextInode(), // Assign an inode number
		refCount: 1,              // Initially referenced
	}
	return bn, nil
}

func (tn *TreeNode) ensureSerialized() error {
	if tn.blob != nil {
		// Already serialized
		return nil
	}

	dirMap := make(map[string]string)
	for name, child := range tn.Children() {
		dirMap[name] = child.AddressString()
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
