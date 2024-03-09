package grits

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

////////////////////////
// BlobAddr

type BlobAddr struct {
	Hash string // SHA-256 hash as a lowercase hexadecimal string.
	Size uint64 // 64-bit size.
}

// NewBlobAddr creates a new BlobAddr with a hash string and size
func NewBlobAddr(hash string, size uint64) *BlobAddr {
	return &BlobAddr{Hash: hash, Size: size}
}

// NewBlobAddrFromString creates a BlobAddr from a string format "hash-size"
func NewBlobAddrFromString(addrStr string) (*BlobAddr, error) {
	parts := strings.SplitN(addrStr, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid file address format - %s", addrStr)
	}

	hash := parts[0]

	size, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid file size - %s", parts[1])
	}

	return NewBlobAddr(hash, size), nil
}

// ComputeSHA256 takes a byte slice and returns its SHA-256 hash as a lowercase hexadecimal string.
func ComputeSHA256(data []byte) string {
	hasher := sha256.New()
	hasher.Write(data)                        // Write data into the hasher
	return fmt.Sprintf("%x", hasher.Sum(nil)) // Compute the SHA-256 checksum and format it as a hex string
}

// String returns the string representation of BlobAddr, including the extension if present.
func (fa *BlobAddr) String() string {
	return fmt.Sprintf("%s-%d", fa.Hash, fa.Size)
}

// Equals checks if two BlobAddr instances are equal
func (fa *BlobAddr) Equals(other *BlobAddr) bool {
	return fa.Hash == other.Hash && fa.Size == other.Size
}

// computeBlobAddr computes the SHA-256 hash, size, and file extension for an existing file,
// and returns a new BlobAddr instance based on these parameters.
func ComputeBlobAddr(path string) (*BlobAddr, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return nil, err
	}

	return NewBlobAddr(fmt.Sprintf("%x", hasher.Sum(nil)), uint64(size)), nil
}

////////////////////////
// TypedFileAddr

// AddrType distinguishes between different types of addresses (blob or tree).
type AddrType int

const (
	Blob AddrType = iota // 0
	Tree                 // 1
)

// TypedFileAddr embeds FileAddr and adds a type (blob or tree).
type TypedFileAddr struct {
	BlobAddr
	Type AddrType
}

// NewTypedFileAddr creates a new TypedFileAddr.
func NewTypedFileAddr(hash string, size uint64, t AddrType) *TypedFileAddr {
	return &TypedFileAddr{
		BlobAddr: BlobAddr{
			Hash: hash,
			Size: size,
		},
		Type: t,
	}
}

// String returns a string representation of the TypedFileAddr, including its type.
func (tfa *TypedFileAddr) String() string {
	typePrefix := "blob"
	if tfa.Type == Tree {
		typePrefix = "tree"
	}
	return fmt.Sprintf("%s:%s", typePrefix, tfa.BlobAddr.String())
}

// NewTypedFileAddrFromString parses a string into a TypedFileAddr.
// The string format is expected to be "type:hash:size.extension".
func NewTypedFileAddrFromString(s string) (*TypedFileAddr, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid format, expected 'type:hash-size', got %s", s)
	}

	// Identify type
	addrType := Blob // Could be "unknown" or something
	switch parts[0] {
	case "blob":
		addrType = Blob
	case "tree":
		addrType = Tree
	default:
		return nil, fmt.Errorf("unknown type prefix %s", parts[0])
	}

	// Assuming the extension is the last part and size is just before the extension, concatenated with the hash
	hashSizeParts := strings.Split(parts[1], "-")
	if len(hashSizeParts) != 2 {
		return nil, fmt.Errorf("invalid format for hash:size in %s", parts[1])
	}

	hash := hashSizeParts[0]
	size, err := strconv.ParseUint(hashSizeParts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid size value: %v", err)
	}

	return &TypedFileAddr{
		BlobAddr: BlobAddr{
			Hash: hash,
			Size: size,
		},
		Type: addrType,
	}, nil
}

////////////////////////
// CachedFile

type CachedFile struct {
	Path        string
	RefCount    int
	Address     *BlobAddr
	LastTouched time.Time
	IsHardLink  bool
}

// NewCachedFile creates a new CachedFile.
func NewCachedFile(path string, refCount int, blobAddr *BlobAddr) *CachedFile {
	return &CachedFile{
		Path:        path,
		RefCount:    refCount,
		Address:     blobAddr,
		LastTouched: time.Now(),
	}
}

// Read reads a portion of the file.
func (c *CachedFile) Read(offset int, length int) ([]byte, error) {
	file, err := os.Open(c.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	buffer := make([]byte, length)
	_, err = file.ReadAt(buffer, int64(offset))
	if err != nil {
		return nil, err
	}
	return buffer, nil
}
