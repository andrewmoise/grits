package grits

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/mr-tron/base58"
	"github.com/multiformats/go-multihash"
)

////////////////////////
// Type definitions for clarity

type BlobAddr string

type GNodeAddr BlobAddr
type FileContentAddr BlobAddr

////////////////////////
// BlobAddr

func ComputeBlobAddrFromData(data []byte) (BlobAddr, error) {
	mh, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		return "", fmt.Errorf("error computing hash: %v", err)
	}

	return BlobAddr(base58.Encode(mh)), nil
}

func ComputeBlobAddrFromReader(r io.Reader) (BlobAddr, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return "", fmt.Errorf("error computing hash: %v", err)
	}
	return ComputeBlobAddrFromData(hasher.Sum(nil))
}

func ComputeBlobAddrFromFile(path string) (BlobAddr, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return ComputeBlobAddrFromReader(file)
}

////////////////////////
// Blob storage interfaces

type BlobStore interface {
	ReadFile(blobAddr BlobAddr) (CachedFile, error)
	AddLocalFile(srcPath string) (CachedFile, error)
	AddOpenFile(file *os.File) (CachedFile, error)
	AddDataBlock(data []byte) (CachedFile, error)
}

type CachedFile interface {
	GetAddress() BlobAddr
	GetSize() int64
	Touch()
	Read(offset int64, length int64) ([]byte, error)
	Reader() (io.ReadSeekCloser, error)
	Release()
	Take()
}
