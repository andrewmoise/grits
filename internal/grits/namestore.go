package grits

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
)

////////////////////////
// Node Types

type FileNode interface {
	ExportedBlob() *CachedFile
	Children() map[string]FileNode
	AddressString() string

	Take()
	Release(*BlobStore)
}

type BlobNode struct {
	blob     *CachedFile
	refCount int
}

type TreeNode struct {
	blob        *CachedFile
	ChildrenMap map[string]FileNode
	refCount    int

	nameStore *NameStore
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

func (bn *BlobNode) Take() {
	bn.refCount++
}

func (bn *BlobNode) Release(bs *BlobStore) {
	bn.refCount--
	if bn.refCount == 0 {
		bs.Release(bn.blob)
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

func (tn *TreeNode) AddressString() string {
	tn.ensureSerialized()
	return "tree:" + tn.blob.Address.String()
}

func (tn *TreeNode) Take() {
	tn.refCount++
}

func (tn *TreeNode) Release(bs *BlobStore) {
	tn.refCount--
	if tn.refCount == 0 {
		if tn.blob != nil {
			bs.Release(tn.blob)
		}
		for _, child := range tn.ChildrenMap {
			child.Release(bs)
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
	BlobStore *BlobStore
	root      FileNode
	fileCache map[string]FileNode
	mtx       sync.RWMutex
}

func (ns *NameStore) GetRoot() string {
	return ns.root.AddressString()
}

func (ns *NameStore) Lookup(name string) (*CachedFile, error) {
	name = strings.TrimRight(name, "/")
	if name != "" && name[0] == '/' {
		return nil, fmt.Errorf("name must be relative")
	}

	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	pos := ns.root
	parts := strings.Split(name, "/")

	for _, part := range parts {
		if pos == nil {
			return nil, fmt.Errorf("no such file or directory: %s", name)
		}

		if part == "" {
			continue
		}

		container := pos.Children()
		if container == nil {
			return nil, fmt.Errorf("non-directory in path %s", name)
		}

		nextNode, exists := container[part]
		if !exists {
			return nil, fmt.Errorf("no such file or directory: %s", part)
		}

		pos = nextNode
	}

	cf := pos.ExportedBlob()
	ns.BlobStore.Take(cf)

	return cf, nil
}

func (ns *NameStore) LookupFull(name string) ([][]string, error) {
	if name != "" && name[0] == '/' {
		return nil, fmt.Errorf("name must be relative")
	}

	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	pos := ns.root
	parts := strings.Split(name, "/")
	partialPath := ""

	response := make([][]string, 0, len(parts)+1) // +1 for the root
	response = append(response, []string{partialPath, pos.AddressString()})

	for _, part := range parts {
		if pos == nil {
			return nil, fmt.Errorf("no such file or directory: %s", name)
		}

		if part == "" {
			continue
		}

		container := pos.Children()
		if container == nil {
			return nil, fmt.Errorf("non-directory in path %s", name)
		}

		nextNode, exists := container[part]
		if !exists {
			return nil, fmt.Errorf("no such file or directory: %s", part)
		}

		pos = nextNode
		partialPath = path.Join(partialPath, part)
		response = append(response, []string{partialPath, pos.AddressString()})
	}

	return response, nil
}

func (ns *NameStore) Link(name string, addr *TypedFileAddr) error {
	name = strings.TrimRight(name, "/")
	if name != "" && name[0] == '/' {
		return fmt.Errorf("name must be relative")
	}

	if name == "." {
		name = ""
	}

	// Somewhat weird special case for empty tree... FIXME what about whitespace?
	if addr != nil &&
		addr.Type == Tree &&
		addr.Hash == "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" &&
		addr.Size == 2 {
		addr = nil
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
		ns.root.Release(ns.BlobStore)
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

func (ns *NameStore) recursiveLink(name string, addr *TypedFileAddr, oldParent FileNode) (FileNode, error) {
	parts := strings.SplitN(name, "/", 2)

	var oldChildren map[string]FileNode
	if oldParent != nil {
		oldChildren = oldParent.Children()
		if oldChildren == nil {
			return nil, fmt.Errorf("non-directory in path %s", name)
		}
	} else {
		oldChildren = make(map[string]FileNode)
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
			oldChild = nil
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

	var result FileNode
	if len(newChildren) > 0 {
		result, err = ns.CreateTreeNode(newChildren)
		if err != nil {
			for _, v := range newChildren {
				v.Release(ns.BlobStore)
			}

			return nil, err
		}
	} else {
		result = nil
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
					child.Release(ns.BlobStore)
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

func (ns *NameStore) CreateTreeNode(children map[string]FileNode) (*TreeNode, error) {
	return &TreeNode{
		ChildrenMap: children,
		nameStore:   ns,
	}, nil
}

func (ns *NameStore) CreateBlobNode(fa *BlobAddr) (*BlobNode, error) {
	cf, err := ns.BlobStore.ReadFile(fa)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", fa.String(), err)
	}

	return &BlobNode{blob: cf}, nil
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
