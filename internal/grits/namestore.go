package grits

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
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
	log.Printf("  Release ref for blob node %s\n", bn.blob.Address.String())
	if bn.refCount == 0 {
		log.Printf("    and release blob %s\n", bn.blob.Address.String())
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
	log.Printf("  Release ref for tree node %s\n", tn.blob.Address.String())
	if tn.refCount == 0 {
		log.Printf("    and release blob %s\n", tn.blob.Address.String())
		bs.Release(tn.blob)
		for name, child := range tn.ChildrenMap {
			log.Printf("    and child %s\n", name)
			child.Release(bs)
		}
	}
}

func DebugPrintTree(node FileNode, indent string) {
	if node == nil {
		log.Printf("%snil\n", indent)
		return
	}

	children := node.Children()
	if children == nil {
		return
	}

	for name, child := range children {
		log.Printf("%s%s - %s\n", indent, name, child.AddressString())
		DebugPrintTree(child, indent+"  ")
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
	log.Printf("  Take ref for %s from Lookup\n", name)
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
		log.Printf("  Take reference for root - %s", newRoot.AddressString())
		newRoot.Take()
	}
	if ns.root != nil {
		log.Printf("  Release reference for old root - %s", ns.root.AddressString())
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
	addrStr := "null"
	if addr != nil {
		addrStr = addr.String()
	}

	var oldParentStr string
	if oldParent == nil || reflect.ValueOf(oldParent).IsNil() {
		oldParentStr = "null"
	} else {
		oldParentStr = oldParent.AddressString()
	}

	log.Printf("recursiveLink(%s, %s, %s)\n", name, addrStr, oldParentStr)

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
			log.Printf("  Take ref for sibling %s from recursiveLink\n", k)
			v.Take()
		}
	}

	if newValue != nil {
		newChildren[parts[0]] = newValue
		log.Printf("  Take ref for new child %s from recursiveLink\n", parts[0])
		newValue.Take()
	} else {
		delete(newChildren, parts[0])
	}

	var result FileNode
	if len(newChildren) > 0 {
		result, err = ns.CreateTreeNode(newChildren)
		log.Printf("  created new tree node %s\n", result.AddressString())
		if err != nil {
			for _, v := range newChildren {
				log.Printf("  Release ref for sibling %s from recursiveLink\n", v.AddressString())
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
			delete(ns.fileCache, addr.String())
			return nil, fmt.Errorf("error reading %s: %v", addr.String(), err)
		}
		defer func() {
			if dn != nil {
				delete(ns.fileCache, addr.String())
				for name, child := range dn.ChildrenMap {
					log.Printf("  iter child %s", name)
					child.Release(ns.blobStore)
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
