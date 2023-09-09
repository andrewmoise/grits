package grits

import (
	"bytes"
	"fmt"
	"os"
	"time"
)

type FileAddr struct {
	Hash []byte // This will hold the SHA-256 hash.
	Size uint64  // 64-bit size.
}

func NewFileAddr(hash []byte, size uint64) *FileAddr {
	return &FileAddr{Hash: hash, Size: size}
}

func (fa *FileAddr) Equals(other *FileAddr) bool {
    return bytes.Equal(fa.Hash, other.Hash) && fa.Size == other.Size
}

func (fa *FileAddr) String() string {
	return fmt.Sprintf("%x:%d", fa.Hash, fa.Size)
}

type CachedFile struct {
	Path        string
	Size        uint64
	RefCount    int
	Address     *FileAddr
	LastTouched time.Time
}

func NewCachedFile(path string, refCount int, fileAddr *FileAddr) *CachedFile {
	return &CachedFile{
		Path: path,
		Size: fileAddr.Size,
		RefCount: refCount,
		Address: fileAddr,
		LastTouched: time.Now(),
	}
}

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

func (c *CachedFile) Release() {
	c.RefCount -= 1
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
