package grits

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mr-tron/base58"
	"github.com/multiformats/go-multihash"
)

////////////////////////
// BlobAddr

type BlobAddr struct {
	Hash string // SHA-256 hash as an IPFS CID v0 string
}

func NewBlobAddr(hash string) *BlobAddr {
	return &BlobAddr{Hash: hash}
}

func NewBlobAddrFromString(cidStr string) (*BlobAddr, error) {
	if !strings.HasPrefix(cidStr, "Qm") {
		return nil, fmt.Errorf("invalid CID v0 format - %s", cidStr)
	}
	return NewBlobAddr(cidStr), nil
}

func (ba *BlobAddr) String() string {
	return ba.Hash
}

func (ba *BlobAddr) Equals(other *BlobAddr) bool {
	return ba.Hash == other.Hash
}

func ComputeBlobAddrFromData(data []byte) (*BlobAddr, error) {
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		return nil, fmt.Errorf("error computing hash: %v", err)
	}
	return NewBlobAddr(base58.Encode(mh)), nil
}

func ComputeBlobAddrFromReader(r io.Reader) (*BlobAddr, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return nil, fmt.Errorf("error computing hash: %v", err)
	}
	return ComputeBlobAddrFromData(hasher.Sum(nil))
}

func ComputeBlobAddrFromFile(path string) (*BlobAddr, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ComputeBlobAddrFromReader(file)
}

////////////////////////
// Specific Blob Address Types

type GNodeAddr struct {
	BlobAddr
}

func NewGNodeAddr(hash string) *GNodeAddr {
	return &GNodeAddr{*NewBlobAddr(hash)}
}

type FileContentAddr struct {
	BlobAddr
}

func NewFileContentAddr(hash string) *FileContentAddr {
	return &FileContentAddr{*NewBlobAddr(hash)}
}

////////////////////////
// Blob storage interfaces

type BlobStore interface {
	ReadFile(blobAddr *BlobAddr) (CachedFile, error)
	AddLocalFile(srcPath string) (CachedFile, error)
	AddOpenFile(file *os.File) (CachedFile, error)
	AddDataBlock(data []byte) (CachedFile, error)
}

type CachedFile interface {
	GetAddress() *BlobAddr
	GetSize() int64
	Touch()
	Read(offset int64, length int64) ([]byte, error)
	Reader() (io.ReadSeekCloser, error)
	Release()
	Take()
}
