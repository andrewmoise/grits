package grits

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type FileAddr struct {
	Hash string // SHA-256 hash as a lowercase hexadecimal string.
	Size uint64 // 64-bit size.
}

// NewFileAddr creates a new FileAddr with a hash string and size.
func NewFileAddr(hash string, size uint64) *FileAddr {
	return &FileAddr{Hash: hash, Size: size}
}

func NewFileAddrFromString(addrStr string) (*FileAddr, error) {
	// Split the address into hash and size
	parts := strings.SplitN(addrStr, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("Invalid file address format - %s", addrStr)
	}
	hash, sizeStr := parts[0], parts[1]

	// Convert size from string to uint64
	size, err := strconv.ParseUint(sizeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("Invalid file size - %s", sizeStr)
	}

	// Create FileAddr from extracted hash and size
	fileAddr := &FileAddr{Hash: hash, Size: size}
	return fileAddr, nil
}

// Equals checks if two FileAddr instances are equal.
func (fa *FileAddr) Equals(other *FileAddr) bool {
	return fa.Hash == other.Hash && fa.Size == other.Size
}

// String returns the string representation of FileAddr.
func (fa *FileAddr) String() string {
	return fmt.Sprintf("%s:%d", fa.Hash, fa.Size)
}

type CachedFile struct {
	Path        string
	Size        uint64
	RefCount    int
	Address     *FileAddr
	LastTouched time.Time
}

// NewCachedFile creates a new CachedFile.
func NewCachedFile(path string, refCount int, fileAddr *FileAddr) *CachedFile {
	return &CachedFile{
		Path:        path,
		Size:        fileAddr.Size,
		RefCount:    refCount,
		Address:     fileAddr,
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

const DownloadChunkSize = 1400

type Peer struct {
	Ip            string
	Port          int
	LastSeen      time.Time
	DhtStoredData map[*CachedFile]time.Time
}

func NewPeer(ip string, port int) *Peer {
	return &Peer{ip, port, time.Time{}, make(map[*CachedFile]time.Time)}
}

func (p *Peer) UpdateLastSeen() {
	p.LastSeen = time.Now()
}

func (p *Peer) TimeSinceSeen() int {
	if p.LastSeen.IsZero() {
		return -1
	}
	currentTime := time.Now()
	return int(currentTime.Sub(p.LastSeen).Seconds())
}

type AllPeers struct {
	Peers map[string]*Peer
}

func NewAllPeers() *AllPeers {
	return &AllPeers{make(map[string]*Peer)}
}

func (a *AllPeers) GenerateNodeKey(ip string, port int) string {
	return fmt.Sprintf("%s:%d", ip, port)
}

func (a *AllPeers) GetPeer(ip string, port int) (*Peer, error) {
	key := a.GenerateNodeKey(ip, port)
	result, exists := a.Peers[key]
	if exists {
		return result, nil
	} else {
		return a.AddPeer(ip, port)
	}
}

func (a *AllPeers) AddPeer(ip string, port int) (*Peer, error) {
	key := a.GenerateNodeKey(ip, port)
	_, exists := a.Peers[key]
	if !exists {
		peerNode := NewPeer(ip, port)
		a.Peers[key] = peerNode
		return peerNode, nil
	} else {
		return nil, fmt.Errorf("Node with address %s and port %d already exists.", ip, port)
	}
}

func (a *AllPeers) RemovePeer(ip string, port int) {
	key := a.GenerateNodeKey(ip, port)
	delete(a.Peers, key)
}
