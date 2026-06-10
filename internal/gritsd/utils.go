package gritsd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"os"
	"path/filepath"
	"strings"
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

// ReadVolumeFile reads the full content of a file stored in a volume.
// Returns the file bytes, or an error if the file doesn't exist or can't be read.
// The caller is responsible for the returned bytes (the underlying CachedFile
// reference is released before returning).
func ReadVolumeFile(srv *Server, volumeName, path string) ([]byte, error) {
	volume := srv.FindVolumeByName(volumeName)
	if volume == nil {
		return nil, fmt.Errorf("ReadVolumeFile: volume %q not found", volumeName)
	}

	fileNode, err := volume.LookupNode(path, grits.BackendPrincipal)
	if err != nil {
		return nil, fmt.Errorf("ReadVolumeFile: lookup %q in %q: %w", path, volumeName, err)
	}
	defer fileNode.Release()

	blob, err := fileNode.ExportedBlob()
	if err != nil {
		return nil, fmt.Errorf("ReadVolumeFile: exported blob for %q: %w", path, err)
	}

	data, err := blob.Read(0, blob.GetSize())
	if err != nil {
		return nil, fmt.Errorf("ReadVolumeFile: reading %q: %w", path, err)
	}

	return data, nil
}

// ensureVolumeParentDirs creates any directories along parentPath that do
// not yet exist within the volume.
func ensureVolumeParentDirs(volume Volume, parentPath string) error {
	if parentPath == "" || parentPath == "." {
		return nil
	}
	// Walk the path segments, creating any missing directory.
	var acc string
	for _, seg := range strings.Split(parentPath, "/") {
		if seg == "" {
			continue
		}
		if acc == "" {
			acc = seg
		} else {
			acc = acc + "/" + seg
		}
		if _, err := volume.LookupNode(acc, grits.BackendPrincipal); err != nil {
			if !grits.IsNotExist(err) {
				return fmt.Errorf("ensuring parent dir %q: %w", acc, err)
			}
			dirNode, dirErr := volume.CreateTreeNode()
			if dirErr != nil {
				return fmt.Errorf("creating dir node for %q: %w", acc, dirErr)
			}
			defer dirNode.Release()
			if linkErr := volume.LinkByMetadata(acc, dirNode.MetadataBlob().GetAddress()); linkErr != nil {
				return fmt.Errorf("linking dir %q: %w", acc, linkErr)
			}
		}
	}
	return nil
}

// WriteVolumeFile stores data as a file at the given path within a volume,
// replacing any existing file at that path. Parent directories are created
// automatically if they don't exist.
func WriteVolumeFile(srv *Server, volumeName, path string, data []byte) error {
	volume := srv.FindVolumeByName(volumeName)
	if volume == nil {
		return fmt.Errorf("WriteVolumeFile: volume %q not found", volumeName)
	}

	// Ensure parent directories exist.
	parent := path
	if idx := strings.LastIndex(parent, "/"); idx >= 0 {
		parent = path[:idx]
	} else {
		parent = ""
	}
	if err := ensureVolumeParentDirs(volume, parent); err != nil {
		return fmt.Errorf("WriteVolumeFile: %w", err)
	}

	contentCf, err := volume.AddDataBlock(data)
	if err != nil {
		return fmt.Errorf("WriteVolumeFile: storing content for %q: %w", path, err)
	}
	defer contentCf.Release()

	metadataNode, err := volume.CreateBlobNode(contentCf.GetAddress(), contentCf.GetSize())
	if err != nil {
		return fmt.Errorf("WriteVolumeFile: creating metadata for %q: %w", path, err)
	}
	defer metadataNode.Release()

	if err := volume.LinkByMetadata(path, metadataNode.MetadataBlob().GetAddress()); err != nil {
		return fmt.Errorf("WriteVolumeFile: linking %q: %w", path, err)
	}

	return nil
}

// ReadJSONL reads a JSONL (JSON Lines) file from a volume and returns each
// non-empty line as a raw byte slice. Lines with only whitespace are omitted.
func ReadJSONL(srv *Server, volumeName, path string) ([][]byte, error) {
	data, err := ReadVolumeFile(srv, volumeName, path)
	if err != nil {
		return nil, err
	}

	var lines [][]byte
	for _, raw := range bytes.Split(data, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// WriteJSONL writes a set of records as a JSONL (JSON Lines) file to a volume.
// Each record is marshalled to JSON and written as one line.
func WriteJSONL(srv *Server, volumeName, path string, records []map[string]any) error {
	var buf bytes.Buffer
	for _, rec := range records {
		line, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("WriteJSONL: marshalling record: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return WriteVolumeFile(srv, volumeName, path, buf.Bytes())
}