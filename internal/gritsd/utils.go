package gritsd

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"os"
	"path/filepath"
	"time"
)

// ImportLocalDir walks localPath on the real filesystem and links the resulting
// tree into volume at volumePath.
//
// The metadata (timestamps, mode) is derived from the real filesystem stat info,
// so re-importing an unchanged directory produces byte-for-byte identical blobs
// and therefore identical hashes — no spurious cache invalidation for clients.
func ImportLocalDir(volume Volume, localPath string, volumePath string) error {
	var h refHolder
	defer h.releaseAll()

	node, err := importNode(volume, localPath, &h)
	if err != nil {
		return err
	}

	return volume.LinkByMetadata(volumePath, node.MetadataBlob().GetAddress())
}

// refHolder keeps CachedFile and FileNode references alive until we're done
// building and linking the whole tree, preventing premature deletion of
// zero-refcount blobs by the eviction worker.
type refHolder struct {
	cfs   []grits.CachedFile
	nodes []grits.FileNode
}

func (h *refHolder) holdCF(cf grits.CachedFile) grits.CachedFile {
	h.cfs = append(h.cfs, cf)
	return cf
}

func (h *refHolder) holdNode(n grits.FileNode) grits.FileNode {
	h.nodes = append(h.nodes, n)
	return n
}

func (h *refHolder) releaseAll() {
	for _, cf := range h.cfs {
		cf.Release()
	}
	for _, n := range h.nodes {
		n.Release()
	}
}

func importNode(volume Volume, localPath string, h *refHolder) (grits.FileNode, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", localPath, err)
	}

	if info.IsDir() {
		return importDir(volume, localPath, info, h)
	}
	return importFile(volume, localPath, info, h)
}

func importFile(volume Volume, localPath string, info os.FileInfo, h *refHolder) (grits.FileNode, error) {
	contentCf, err := volume.AddBlob(localPath)
	if err != nil {
		return nil, fmt.Errorf("adding blob %s: %w", localPath, err)
	}
	h.holdCF(contentCf)

	metadata := &grits.GNodeMetadata{
		Type:        grits.GNodeTypeFile,
		Size:        info.Size(),
		ContentHash: contentCf.GetAddress(),
		Mode:        uint32(info.Mode().Perm()),
		Timestamp:   info.ModTime().UTC().Format(time.RFC3339),
	}

	metadataCf, err := volume.AddMetadataBlob(metadata)
	if err != nil {
		return nil, fmt.Errorf("adding metadata blob for %s: %w", localPath, err)
	}
	h.holdCF(metadataCf)

	node, err := volume.GetFileNode(metadataCf.GetAddress())
	if err != nil {
		return nil, fmt.Errorf("getting file node for %s: %w", localPath, err)
	}
	h.holdNode(node)

	return node, nil
}

func importDir(volume Volume, localPath string, info os.FileInfo, h *refHolder) (grits.FileNode, error) {
	entries, err := os.ReadDir(localPath)
	if err != nil {
		return nil, fmt.Errorf("reading dir %s: %w", localPath, err)
	}

	children := make(map[string]grits.BlobAddr, len(entries))
	for _, entry := range entries {
		childPath := filepath.Join(localPath, entry.Name())
		childNode, err := importNode(volume, childPath, h)
		if err != nil {
			return nil, fmt.Errorf("importing %s: %w", childPath, err)
		}
		children[entry.Name()] = childNode.MetadataBlob().GetAddress()
	}

	// Serialize the children map the same way CreateTreeNode does, so the
	// directory content hash is stable across re-imports.
	// Go's json.Marshal sorts map keys, which is load-bearing for hash stability.
	dirData, err := json.Marshal(children)
	if err != nil {
		return nil, fmt.Errorf("marshalling dir %s: %w", localPath, err)
	}

	contentCf, err := volume.AddDataBlock(dirData)
	if err != nil {
		return nil, fmt.Errorf("storing dir content %s: %w", localPath, err)
	}
	h.holdCF(contentCf)

	metadata := &grits.GNodeMetadata{
		Type:        grits.GNodeTypeDirectory,
		Size:        contentCf.GetSize(),
		ContentHash: contentCf.GetAddress(),
		Mode:        uint32(info.Mode().Perm()),
		Timestamp:   info.ModTime().UTC().Format(time.RFC3339),
	}

	metadataCf, err := volume.AddMetadataBlob(metadata)
	if err != nil {
		return nil, fmt.Errorf("adding metadata blob for dir %s: %w", localPath, err)
	}
	h.holdCF(metadataCf)

	node, err := volume.GetFileNode(metadataCf.GetAddress())
	if err != nil {
		return nil, fmt.Errorf("getting file node for dir %s: %w", localPath, err)
	}
	h.holdNode(node)

	return node, nil
}