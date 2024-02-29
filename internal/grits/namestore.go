package grits

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

type NameStore struct {
	blobStore *BlobStore
	root      FileNode
	fileCache map[string]FileNode
	mtx       sync.RWMutex
}

func (ns *NameStore) GetRoot() string {
	return ns.root.AddressString()
}

func (ns *NameStore) Lookup(name string) (FileNode, error) {
	ns.mtx.RLock()
	defer ns.mtx.RUnlock()

	pos := ns.root
	log.Printf("Lookup: %s in %s\n", name, pos.AddressString())
	parts := strings.Split(name, "/")

	for _, part := range parts {
		if part == "" {
			continue
		}

		log.Printf("  iter %s in %s\n", part, pos.AddressString())

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

	return pos, nil
}

func (ns *NameStore) LinkBlob(name string, addr *FileAddr) error {
	ns.mtx.Lock()
	defer ns.mtx.Unlock()

	var tfa *TypedFileAddr

	if addr != nil {
		tfa = &TypedFileAddr{
			FileAddr: *addr,
			Type:     Blob,
		}
	} else {
		tfa = nil
	}

	newRoot, error := ns.recursiveLink(name, tfa, ns.root)
	if error != nil {
		return error
	}

	log.Printf("  new root: %s\n", newRoot.AddressString())

	ns.root = newRoot
	return nil
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
	}

	if newValue != nil {
		newChildren[parts[0]] = newValue
	} else {
		delete(newChildren, parts[0])
	}

	var result FileNode
	if len(newChildren) > 0 {
		result, err = ns.CreateTreeNode(newChildren)
		log.Printf("  created new tree node %s\n", result.AddressString())
		if err != nil {
			// FIXME - release newChild recursively
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
