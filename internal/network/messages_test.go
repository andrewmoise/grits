package network

import (
	"bytes"
	"grits/internal/grits"
	"reflect"
	"testing"
)

func TestHeartbeatMsg(t *testing.T) {
	// Create and encode
	msg := NewHeartbeatMsg()
	encodedData := msg.Encode()

	// Test if the encoding is a 0-byte array instead of nil
	if len(encodedData) != 0 {
		t.Fatal("HeartbeatMsg encoding should return 0-byte data")
	}

	// Decode the message
	decodedMsg, err := DecodeHeartbeatMsg(encodedData)
	if err != nil {
		t.Fatalf("Failed to decode HeartbeatMsg: %v", err)
	}

	// Verify that we've decoded to a HeartbeatMsg
	_, ok := decodedMsg.(*HeartbeatMsg)
	if !ok {
		t.Fatal("Decoded message is not of type *HeartbeatMsg")
	}
}

func TestHeartbeatReply(t *testing.T) {
	// Create and encode
	fileAddr := grits.FileAddr{
		Hash: []byte("somehashthatshouldbe32byteslong!"),
		Size: 12345,
	}
	reply := NewHeartbeatReply(&fileAddr)
	encodedData := reply.Encode()

	// Decode
	rawDecodedReply, err := DecodeHeartbeatReply(encodedData)
	if err != nil {
		t.Fatalf("failed to decode HeartbeatReply: %v", err)
	}

	// Cast
	decodedReply, ok := rawDecodedReply.(*HeartbeatReply)
	if !ok {
		t.Fatal("Decoded message is not a HeartbeatReply")
	}

	// Compare
	if !bytes.Equal(decodedReply.NodeMapAddress.Hash, reply.NodeMapAddress.Hash) ||
		decodedReply.NodeMapAddress.Size != reply.NodeMapAddress.Size {
		t.Fatal("decoded HeartbeatReply does not match original")
	}
}

func TestEncodeDecodeDataFetchMsg(t *testing.T) {
	originalMsg := &DataFetchMsg{
		Address:    grits.NewFileAddr([]byte("01234567890123456789012345678901"), 12345),
		Offset:     32,
		Length:     64,
		TransferId: "trans123",
	}

	encodedData := originalMsg.Encode()
	decodedMsg, err := DecodeDataFetchMsg(encodedData)

	if err != nil {
		t.Errorf("Error decoding DataFetchMsg: %v", err)
	}

	if !reflect.DeepEqual(originalMsg, decodedMsg) {
		t.Errorf("Original and Decoded messages don't match. Original: %v, Decoded: %v", originalMsg, decodedMsg)
	}
}

func TestEncodeDecodeDataFetchReplyOk(t *testing.T) {
	originalMsg := &DataFetchReplyOk{
		Address: grits.NewFileAddr([]byte("01234567890123456789012345678901"), 12345),
		Offset:  32,
		Length:  12,
		Data:    []byte("012345678901"),
	}

	encodedData := originalMsg.Encode()
	decodedMsg, err := DecodeDataFetchReplyOk(encodedData)

	if err != nil {
		t.Errorf("Error decoding DataFetchReplyOk: %v", err)
	}

	if !reflect.DeepEqual(originalMsg, decodedMsg) {
		t.Errorf("Original and Decoded messages don't match. Original: %v, Decoded: %v", originalMsg, decodedMsg)
	}
}

func TestEncodeDecodeDataFetchReplyNo(t *testing.T) {
	originalMsg := &DataFetchReplyNo{
		Address: grits.NewFileAddr([]byte("01234567890123456789012345678901"), 12345),
	}

	encodedData := originalMsg.Encode()
	decodedMsg, err := DecodeDataFetchReplyNo(encodedData)

	if err != nil {
		t.Errorf("Error decoding DataFetchReplyNo: %v", err)
	}

	if !reflect.DeepEqual(originalMsg, decodedMsg) {
		t.Errorf("Original and Decoded messages don't match. Original: %v, Decoded: %v", originalMsg, decodedMsg)
	}
}

// Test for DhtStoreMsg
func TestEncodeDecodeDhtStoreMsg(t *testing.T) {
	fileAddr := grits.NewFileAddr([]byte("01234567890123456789012345678901"), 12345)
	originalMsg := NewDhtStoreMsg(fileAddr)

	encodedData := originalMsg.Encode()
	decodedMsg, err := DecodeDhtStoreMsg(encodedData)

	if err != nil {
		t.Errorf("Error decoding DhtStoreMsg: %v", err)
	}

	if !reflect.DeepEqual(originalMsg, decodedMsg) {
		t.Errorf("Original and Decoded messages don't match. Original: %+v, Decoded: %+v", originalMsg, decodedMsg)
	}
}

// Test for DhtStoreReply
func TestEncodeDecodeDhtStoreReply(t *testing.T) {
	address := grits.NewFileAddr([]byte("01234567890123456789012345678901"), 12345)
	originalMsg := NewDhtStoreReply(address)

	encodedData := originalMsg.Encode()
	decodedMsg, err := DecodeDhtStoreReply(encodedData)

	if err != nil {
		t.Errorf("Error decoding DhtStoreReply: %v", err)
	}

	if !reflect.DeepEqual(originalMsg, decodedMsg) {
		t.Errorf("Original and Decoded messages don't match. Original: %+v, Decoded: %+v", originalMsg, decodedMsg)
	}
}

// Test for DhtLookupMsg
func TestEncodeDecodeDhtLookupMsg(t *testing.T) {
	address := grits.NewFileAddr([]byte("01234567890123456789012345678901"), 12345)
	originalMsg := NewDhtLookupMsg(address, "transfer")

	encodedData := originalMsg.Encode()
	decodedMsg, err := DecodeDhtLookupMsg(encodedData)

	if err != nil {
		t.Errorf("Error decoding DhtLookupMsg: %v", err)
	}

	if !reflect.DeepEqual(originalMsg, decodedMsg) {
		t.Errorf("Original and Decoded messages don't match. Original: %+v, Decoded: %+v", originalMsg, decodedMsg)
	}
}

// Test for DhtLookupReply
func TestEncodeDecodeDhtLookupReply(t *testing.T) {
	fileAddr := grits.NewFileAddr([]byte("01234567890123456789012345678901"), 12345)
	peers := []grits.Peer{
		{Ip: "192.168.1.1", Port: 8080},
		{Ip: "192.168.1.2", Port: 8081},
	}
	originalMsg := NewDhtLookupReply(fileAddr, peers)

	encodedData := originalMsg.Encode()
	decodedMsg, err := DecodeDhtLookupReply(encodedData)

	if err != nil {
		t.Errorf("Error decoding DhtLookupReply: %v", err)
	}

	if !reflect.DeepEqual(originalMsg, decodedMsg) {
		t.Errorf("Original and Decoded messages don't match. Original: %+v, Decoded: %+v", originalMsg, decodedMsg)
	}
}
