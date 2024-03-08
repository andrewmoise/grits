package grits

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
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
func (bf *BlobFinder) GetClosestPeers(blobAddr *BlobAddr, n int) ([]*Peer, error) {
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
