package grits

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
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

////////////////////////
// Peer

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

////////////////////////
// BlobReference

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

////////////////////////
// AllData

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
func (ad *AllData) AddData(BlobAddr string, peer *Peer) {
	ad.lock.Lock()
	defer ad.lock.Unlock()

	// If the file address is not yet known, initialize its BlobReference.
	if _, exists := ad.Data[BlobAddr]; !exists {
		ad.Data[BlobAddr] = NewBlobReference()
	}
	ad.Data[BlobAddr].AddPeer(peer)
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
