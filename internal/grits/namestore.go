package grits

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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
	return tn.blob
}

func (tn *TreeNode) Children() map[string]FileNode {
	return tn.ChildrenMap
}

func (tn *TreeNode) AddressString() string {
	return "tree:" + tn.blob.Address.String()
}

func (tn *TreeNode) Take() {
	tn.refCount++
}

func (tn *TreeNode) Release(bs *BlobStore) {
	tn.refCount--
	if tn.refCount == 0 {
		bs.Release(tn.blob)
		for _, child := range tn.ChildrenMap {
			child.Release(bs)
		}
	}
}

////////////////////////
// NameStore

type NameStore struct {
	blobStore *BlobStore
	root      FileNode
	fileCache map[string]FileNode
	mtx       sync.RWMutex
}

func (ns *NameStore) GetRoot() string {
	return ns.root.AddressString()
}

func (ns *NameStore) Lookup(name string) (*CachedFile, error) {
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
	ns.blobStore.Take(cf)

	return cf, nil
}

func (ns *NameStore) Link(name string, addr *TypedFileAddr) error {
	name = strings.TrimRight(name, "/")
	if name != "" && name[0] == '/' {
		return fmt.Errorf("name must be relative")
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
		ns.root.Release(ns.blobStore)
	}

	ns.root = newRoot
	return nil
}

func (ns *NameStore) LinkBlob(name string, addr *FileAddr) error {
	var tfa *TypedFileAddr

	if addr != nil {
		tfa = &TypedFileAddr{
			FileAddr: *addr,
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
			newValue, err = ns.FetchFileNode(addr)
			if err != nil {
				return nil, err
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
		log.Printf("  created new tree node %s\n", result.AddressString())
		if err != nil {
			for _, v := range oldChildren {
				v.Release(ns.blobStore)
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
		blobStore: bs,
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
		blobStore: bs,
		fileCache: make(map[string]FileNode),
	}

	dn, err := ns.FetchFileNode(rootAddr)
	if err != nil {
		return nil, fmt.Errorf("error fetching root node: %v", err)
	}

	ns.root = dn
	return ns, nil
}

// Getting file nodes from a NameStore

func (ns *NameStore) FetchFileNode(addr *TypedFileAddr) (FileNode, error) {
	cf, err := ns.blobStore.ReadFile(&addr.FileAddr)
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
		}

		dirFile, err := ns.blobStore.ReadFile(&addr.FileAddr)
		if err != nil {
			return nil, fmt.Errorf("error reading %s: %v", addr.String(), err)
		}
		defer func() {
			// FIXME - This actually isn't quite right
			if dn != nil {
				delete(ns.fileCache, addr.String())
				ns.blobStore.Release(dirFile)
				for _, child := range dn.ChildrenMap {
					ns.blobStore.Release(child.ExportedBlob())
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
			cachedFile, exists := ns.fileCache[addrStr]
			if exists {
				dn.ChildrenMap[name] = cachedFile
				continue
			}

			addr, err := NewTypedFileAddrFromString(addrStr)
			if err != nil {
				return nil, fmt.Errorf("error parsing address %s: %v", addrStr, err)
			}

			fn, err := ns.FetchFileNode(addr)
			if err != nil {
				return nil, fmt.Errorf("error fetching %s: %v", addrStr, err)
			}

			dn.ChildrenMap[name] = fn
		}

		resultDn := dn
		dn = nil
		return resultDn, nil
	}
}

func (ns *NameStore) CreateTreeNode(children map[string]FileNode) (*TreeNode, error) {
	dirMap := make(map[string]string)
	for name, child := range children {
		dirMap[name] = child.AddressString()
	}

	dirData, err := json.Marshal(dirMap)
	if err != nil {
		return nil, fmt.Errorf("error marshalling directory: %v", err)
	}

	cf, err := ns.blobStore.AddDataBlock(dirData, ".json")
	if err != nil {
		return nil, fmt.Errorf("error writing directory: %v", err)
	}

	return &TreeNode{
		blob:        cf,
		ChildrenMap: children,
	}, nil
}

func (ns *NameStore) CreateBlobNode(fa *FileAddr) (*BlobNode, error) {
	cf, err := ns.blobStore.ReadFile(fa)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", fa.String(), err)
	}

	return &BlobNode{blob: cf}, nil
}
