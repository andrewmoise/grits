package server

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"sort"
	"sync"
	"time"
)

type DhtModuleConfig struct {

	// Nitty-gritty DHT tuning
	DhtNotifyNumber     int `json:"DhtNotifyNumber"`
	DhtNotifyPeriod     int `json:"DhtNotifyPeriod"`
	DhtMaxResponseNodes int `json:"DhtMaxResponseNodes"`
	DhtRefreshTime      int `json:"DhtRefreshTime"`
	DhtExpiryTime       int `json:"DhtExpiryTime"`

	MaxProxyMapAge           int `json:"MaxProxyMapAge"`
	ProxyMapCleanupPeriod    int `json:"ProxyMapCleanupPeriod"`
	ProxyHeartbeatPeriod     int `json:"ProxyHeartbeatPeriod"`
	RootUpdatePeerListPeriod int `json:"RootUpdatePeerListPeriod"`
	RootProxyDropTimeout     int `json:"RootProxyDropTimeout"`
}

func NewDhtModuleConfig() *DhtModuleConfig {
	return &DhtModuleConfig{
		DhtNotifyNumber:          5,  // # of peers to notify in the DHT
		DhtNotifyPeriod:          20, // # of seconds between DHT notifications
		DhtMaxResponseNodes:      10, // Max # of nodes to return in a DHT response
		DhtRefreshTime:           8 * 60 * 60,
		DhtExpiryTime:            24 * 60 * 60,
		MaxProxyMapAge:           24 * 60 * 60,
		ProxyMapCleanupPeriod:    60 * 60,
		ProxyHeartbeatPeriod:     5 * 60, // # of seconds between proxy heartbeats
		RootUpdatePeerListPeriod: 6 * 60, // ?
		RootProxyDropTimeout:     6 * 60, // # of seconds before root drops a proxy
	}
}

////////////////////////
// Peer

type Peer struct {
	IPv4Address string    `json:"ipv4Address"`
	IPv6Address string    `json:"ipv6Address"`
	Port        int       `json:"port"`
	IsNAT       bool      `json:"IsNAT"`
	Token       string    `json:"token"`
	LastSeen    time.Time `json:"-"`
}

func NewPeer(ipv4Address, ipv6Address string, port int, IsNAT bool) *Peer {
	return &Peer{
		IPv4Address: ipv4Address,
		IPv6Address: ipv6Address,
		Port:        port,
		IsNAT:       IsNAT,
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

// BlobFinder finds the closest peers for a given file address.
type BlobFinder struct {
	SortedPeers []struct {
		Index uint64
		Peer  *Peer
	}
}

// NewBlobFinder creates a new BlobFinder instance.
func NewBlobFinder() *BlobFinder {
	return &BlobFinder{}
}

// convertPeerToInteger converts a PeerNode into an integer by hashing its IP and port.
func convertPeerToInteger(peer *Peer) uint64 {
	hasher := sha256.New()
	hasher.Write([]byte(fmt.Sprintf("%s:evenly distribute hash, please", peer.Token)))
	hash := hasher.Sum(nil)
	// Take first 8 characters (4 bytes) of the hash and convert to uint64
	intValue, _ := hex.DecodeString(hex.EncodeToString(hash[:4]))
	return binary.BigEndian.Uint64(intValue)
}

// convertBlobAddrToInteger converts a file address to an integer.
func convertBlobAddrToInteger(blobAddr string) uint64 {
	intValue, _ := hex.DecodeString(blobAddr[:8])
	return binary.BigEndian.Uint64(intValue)
}

// UpdatePeers updates the internal sorted list of peers.
func (bf *BlobFinder) UpdatePeers(allPeers *AllPeers) {
	bf.SortedPeers = make([]struct {
		Index uint64
		Peer  *Peer
	}, 0, len(allPeers.Peers))

	for _, peer := range allPeers.Peers {
		bf.SortedPeers = append(bf.SortedPeers, struct {
			Index uint64
			Peer  *Peer
		}{
			Index: convertPeerToInteger(peer),
			Peer:  peer,
		})
	}

	sort.Slice(bf.SortedPeers, func(i, j int) bool {
		return bf.SortedPeers[i].Index < bf.SortedPeers[j].Index
	})
}

// GetClosestPeers finds the n closest peers to a given file address.
func (bf *BlobFinder) GetClosestPeers(blobAddr *grits.BlobAddr, n int) ([]*Peer, error) {
	if len(bf.SortedPeers) == 0 {
		return nil, fmt.Errorf("no peers")
	}

	target := convertBlobAddrToInteger(blobAddr.Hash)
	left, right := 0, len(bf.SortedPeers)-1

	for right-left > 1 {
		mid := (left + right) / 2
		if bf.SortedPeers[mid].Index <= target {
			left = mid
		} else {
			right = mid
		}
	}

	var result []*Peer
	i, j := left, 0
	for j < n {
		index := i % len(bf.SortedPeers)
		peer := bf.SortedPeers[index].Peer

		if !containsPeer(result, peer) {
			result = append(result, peer)
		} else {
			break
		}

		i++
		j++
	}

	return result, nil
}

// containsPeer checks if a slice of *Peer contains a specific *Peer.
func containsPeer(slice []*Peer, item *Peer) bool {
	for _, a := range slice {
		if a == item {
			return true
		}
	}
	return false
}

// We need to add this to the DHT module once it's all enabled and worked on again:

// DHT routes

// Using the middleware directly with HandleFunc for specific routes
//	if s.Server.Config.IsRootNode {
//		s.Mux.HandleFunc("/grits/v1/heartbeat", s.handleHeartbeat())
//	}
//	s.Mux.HandleFunc("/grits/v1/announce", s.handleAnnounce())

//func (s *HttpModule) handleHeartbeat() http.HandlerFunc {
//	return func(w http.ResponseWriter, r *http.Request) {
//		token := r.Header.Get("Authorization")
//		peer, exists := s.Peers.GetPeer(token)
//		if !exists {
//			http.Error(w, "Unauthorized: Unknown or invalid token", http.StatusUnauthorized)
//			return
//		}
//
//		peer.UpdateLastSeen()
//
//		peerList, error := s.Peers.Serialize()
//		if error != nil {
//			http.Error(w, "Internal server error", http.StatusInternalServerError)
//			return
//		}
//
//		w.Write(peerList)
//	}
//}
//
//func (s *HttpModule) handleAnnounce() http.HandlerFunc {
//	return func(w http.ResponseWriter, r *http.Request) {
//		token := r.Header.Get("Authorization")
//		peer, exists := s.Peers.GetPeer(token)
//		if !exists {
//			http.Error(w, "Unauthorized: Unknown or invalid token", http.StatusUnauthorized)
//			return
//		}
//
//		peer.UpdateLastSeen()
//		// Process the announcement...
//	}
//}
