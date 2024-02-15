package grits

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
)

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

// convertFileAddrToInteger converts a file address to an integer.
func convertFileAddrToInteger(fileAddr string) uint64 {
	intValue, _ := hex.DecodeString(fileAddr[:8])
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
func (bf *BlobFinder) GetClosestPeers(fileAddr *FileAddr, n int) ([]*Peer, error) {
	if len(bf.SortedPeers) == 0 {
		return nil, fmt.Errorf("no peers")
	}

	target := convertFileAddrToInteger(fileAddr.Hash)
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
