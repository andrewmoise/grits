package grits

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mr-tron/base58"
	"github.com/multiformats/go-multihash"
)

////////////////////////
// BlobAddr

type BlobAddr struct {
	Hash string // SHA-256 hash as an IPFS CID v0 string
}

// NewBlobAddr creates a new BlobAddr with a hash string and size
func NewBlobAddr(hash string) *BlobAddr {
	return &BlobAddr{Hash: hash}
}

// NewBlobAddrFromString creates a BlobAddr from a CID v0 string.
func NewBlobAddrFromString(cidStr string) (*BlobAddr, error) {
	// Verify that the CID starts with 'Qm'
	if !strings.HasPrefix(cidStr, "Qm") {
		return nil, fmt.Errorf("invalid CID v0 format - %s", cidStr)
	}
	return NewBlobAddr(cidStr), nil
}

// String returns the string representation of BlobAddr.
func (ba *BlobAddr) String() string {
	return ba.Hash
}

// Equals checks if two BlobAddr instances are equal.
func (ba *BlobAddr) Equals(other *BlobAddr) bool {
	return ba.Hash == other.Hash
}

func ComputeHash(data []byte) string {
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		panic(err) // or handle error gracefully
	}
	return base58.Encode(mh)
}

func ComputeHashFromReader(r io.Reader) (string, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return "", fmt.Errorf("error computing hash: %v", err)
	}
	mh, err := multihash.Sum(hasher.Sum(nil), multihash.SHA2_256, -1)
	if err != nil {
		return "", fmt.Errorf("error encoding multihash: %v", err)
	}
	return base58.Encode(mh), nil
}

// computeBlobAddr computes the SHA-256 hash, size, and file extension for an existing file,
// and returns a new BlobAddr instance based on these parameters.
func ComputeBlobAddr(path string) (*BlobAddr, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	cid := ComputeHash(data)
	return NewBlobAddr(cid), nil
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
	Size int64
	Type AddrType
}

// NewTypedFileAddr creates a new TypedFileAddr.
func NewTypedFileAddr(hash string, size int64, t AddrType) *TypedFileAddr {
	return &TypedFileAddr{
		BlobAddr: BlobAddr{
			Hash: hash,
		},
		Size: size,
		Type: t,
	}
}

// String returns a string representation of the TypedFileAddr, including its type.
func (tfa *TypedFileAddr) String() string {
	typePrefix := "blob"
	if tfa.Type == Tree {
		typePrefix = "tree"
	}
	return fmt.Sprintf("%s:%s-%d", typePrefix, tfa.BlobAddr.Hash, tfa.Size)
}

// NewTypedFileAddrFromString parses a string into a TypedFileAddr.
// The string format is expected to be "type:hash-size".

// FIXME - extension?

func NewTypedFileAddrFromString(s string) (*TypedFileAddr, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid format, expected 'type:hash-size', got %s", s)
	}

	// Identify type
	addrType := Blob // Default to Blob unless specified as Tree
	switch parts[0] {
	case "blob":
		addrType = Blob
	case "tree":
		addrType = Tree
	default:
		return nil, fmt.Errorf("unknown type prefix %s", parts[0])
	}

	hashSizeParts := strings.Split(parts[1], "-")
	if len(hashSizeParts) != 2 {
		return nil, fmt.Errorf("invalid format for hash-size in %s", parts[1])
	}

	hash := hashSizeParts[0]
	size, err := strconv.ParseInt(hashSizeParts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid size value: %v", err)
	}

	return &TypedFileAddr{
		BlobAddr: BlobAddr{
			Hash: hash,
		},
		Size: size,
		Type: addrType,
	}, nil
}

////////////////////////
// Blob storage interfaces

type BlobStore interface {
	ReadFile(blobAddr *BlobAddr) (CachedFile, error)
	AddLocalFile(srcPath string) (CachedFile, error)
	AddOpenFile(file *os.File) (CachedFile, error)
	AddDataBlock(data []byte) (CachedFile, error)
	DumpStats()
}

// CachedFile defines the interface for interacting with a cached file.
type CachedFile interface {
	GetAddress() *BlobAddr
	GetSize() int64

	Touch()

	Read(offset int64, length int64) ([]byte, error)
	Reader() (io.ReadSeekCloser, error)

	Release()
	Take()
	GetRefCount() int
}
