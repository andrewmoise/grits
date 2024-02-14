package grits

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
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

type Peer struct {
	IPv4Address string    `json:"ipv4Address"`
	IPv6Address string    `json:"ipv6Address"`
	Port        int       `json:"port"`
	IsNat       bool      `json:"isNat"`
	Token       string    `json:"token"`
	LastSeen    time.Time `json:"-"`
}

func NewPeer(ipv4Address, ipv6Address string, port int, isNat bool) *Peer {
	return &Peer{
		IPv4Address: ipv4Address,
		IPv6Address: ipv6Address,
		Port:        port,
		IsNat:       isNat,
		LastSeen:    time.Now(),
	}
}

func (p *Peer) UpdateLastSeen() {
	p.LastSeen = time.Now()
}

func (p *Peer) TimeSinceSeen() time.Duration {
	if p.LastSeen.IsZero() {
		return -1
	}
	return time.Since(p.LastSeen)
}

// AllPeers holds mappings from IP addresses to peers.
type AllPeers struct {
	Peers map[string]*Peer
	lock  sync.RWMutex
}

// NewAllPeers creates a new AllPeers instance.
func NewAllPeers() *AllPeers {
	return &AllPeers{
		Peers: make(map[string]*Peer),
	}
}

// AddPeer adds a peer to the appropriate IP address maps.
func (ap *AllPeers) AddPeer(peer *Peer) {
	ap.lock.Lock()
	defer ap.lock.Unlock()

	ap.Peers[peer.Token] = peer
}

// GetPeerByIPv4 returns a peer by its IPv4 address.
func (ap *AllPeers) GetPeer(token string) (*Peer, bool) {
	ap.lock.RLock()
	defer ap.lock.RUnlock()

	peer, exists := ap.Peers[token]
	return peer, exists
}

// RemovePeer removes a peer from the IP address maps.
func (ap *AllPeers) RemovePeer(peer *Peer) {
	ap.lock.Lock()
	defer ap.lock.Unlock()

	delete(ap.Peers, peer.Token)
}

// Serialize serializes the list of peers to JSON.
func (ap *AllPeers) Serialize() ([]byte, error) {
	ap.lock.Lock()
	defer ap.lock.Unlock()

	peersList := make([]*Peer, 0, len(ap.Peers))
	for _, peer := range ap.Peers {
		peersList = append(peersList, peer)
	}

	return json.Marshal(peersList)
}

// Deserialize loads the list of peers from JSON.
func (ap *AllPeers) Deserialize(data []byte) error {
	ap.lock.Lock()
	defer ap.lock.Unlock()

	var peersList []*Peer
	if err := json.Unmarshal(data, &peersList); err != nil {
		return err
	}

	// Clear existing peers map to avoid duplicates
	ap.Peers = make(map[string]*Peer)

	// Re-populate the map with deserialized peer data
	for _, peer := range peersList {
		if peer.IPv4Address != "" {
			ap.Peers[peer.IPv4Address] = peer
		}
		if peer.IPv6Address != "" {
			ap.Peers[peer.IPv6Address] = peer
		}
	}

	return nil
}

type BlobReference struct {
	Peers map[*Peer]time.Time // Maps Peer URL to the timestamp of the latest announcement.
}

// NewBlobReference creates a new BlobReference instance.
func NewBlobReference() *BlobReference {
	return &BlobReference{
		Peers: make(map[*Peer]time.Time),
	}
}

// AddPeer adds or updates a peer and its announcement timestamp in the BlobReference.
func (br *BlobReference) AddPeer(peer *Peer) {
	br.Peers[peer] = time.Now()
}

// RemovePeer removes a peer from the BlobReference.
func (br *BlobReference) RemovePeer(peer *Peer) {
	delete(br.Peers, peer)
}

// AllData manages all data known to the network, mapping data addresses to BlobReferences.
type AllData struct {
	Data map[string]*BlobReference
	lock sync.Mutex
}

// NewAllData creates a new AllData instance.
func NewAllData() *AllData {
	return &AllData{
		Data: make(map[string]*BlobReference),
	}
}

// AddData adds or updates a data entry with a peer URL.
func (ad *AllData) AddData(fileAddr string, peer *Peer) {
	ad.lock.Lock()
	defer ad.lock.Unlock()

	// If the file address is not yet known, initialize its BlobReference.
	if _, exists := ad.Data[fileAddr]; !exists {
		ad.Data[fileAddr] = NewBlobReference()
	}
	ad.Data[fileAddr].AddPeer(peer)
}

// RemovePeerFromData removes a peer from all data entries where it's listed.
func (ad *AllData) RemovePeerFromData(peer *Peer) {
	ad.lock.Lock()
	defer ad.lock.Unlock()

	// Iterate over all data entries and remove the peer URL from each.
	for _, blobRef := range ad.Data {
		blobRef.RemovePeer(peer)
	}
}
