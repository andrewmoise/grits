package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"grits/internal/grits"
	"net"
	"strconv"
	"strings"
)

const FileHashSize = 32
const FileSizeSize = 8
const FileAddrSize = 40

// MessageType represents the type of a network message.
type MessageType int

const (
	HeartbeatMsgType MessageType = iota
	HeartbeatReplyType

	DataFetchMsgType
	DataFetchReplyOkType
	DataFetchReplyNoType

	DhtStoreMsgType
	DhtStoreReplyType

	DhtLookupMsgType
	DhtLookupReplyType
)

type MessageDecoderFunc func(data []byte) (Message, error)

// Make sure the size matches the number of message types.
var Decoders [9]MessageDecoderFunc = [9]MessageDecoderFunc{
	DecodeHeartbeatMsg,
	DecodeHeartbeatReply,
	DecodeDataFetchMsg,
	DecodeDataFetchReplyOk,
	DecodeDataFetchReplyNo,
	DecodeDhtStoreMsg,
	DecodeDhtStoreReply,
	DecodeDhtLookupMsg,
	DecodeDhtLookupReply,
}

// DecodeMessage decodes a message given its type and the data.
func DecodeMessage(messageType MessageType, data []byte) (Message, error) {
	if int(messageType) < 0 || int(messageType) >= len(Decoders) {
		return nil, fmt.Errorf("unknown message type: %d", messageType)
	}

	return Decoders[messageType](data)
}

// Message interface represents a network message.
type Message interface {
	Encode() []byte
	String() string
}

// HeartbeatMsg represents a heartbeat message.
type HeartbeatMsg struct{}

func NewHeartbeatMsg() *HeartbeatMsg {
	return &HeartbeatMsg{}
}

func DecodeHeartbeatMsg(data []byte) (Message, error) {
	return NewHeartbeatMsg(), nil
}

func (m *HeartbeatMsg) Encode() []byte {
	return []byte{}
}

func (m *HeartbeatMsg) String() string {
	return "HeartbeatMsg"
}

type HeartbeatReply struct {
	NodeMapAddress *grits.FileAddr
}

func NewHeartbeatReply(address *grits.FileAddr) *HeartbeatReply {
	return &HeartbeatReply{NodeMapAddress: address}
}

// DecodeHeartbeatReply decodes the given data into a HeartbeatReply.
func DecodeHeartbeatReply(data []byte) (Message, error) {
	if len(data) < FileAddrSize {
		return nil, fmt.Errorf("data slice is too short for decoding HeartbeatReply, expected at least %d bytes, got %d", FileAddrSize, len(data))
	}

	// Check if the data is just zero-filled, indicating a nil NodeMapAddress.
	isZeroFilled := bytes.Equal(data, make([]byte, FileAddrSize))
	if isZeroFilled {
		return NewHeartbeatReply(nil), nil
	}

	offset := 0

	hash := data[offset : offset+FileHashSize]
	offset += FileHashSize

	size := binary.BigEndian.Uint64(data[offset : offset+FileSizeSize])

	fileAddr := grits.NewFileAddr(hash, size)
	return NewHeartbeatReply(fileAddr), nil
}

func (m *HeartbeatReply) Encode() []byte {
	buf := make([]byte, FileAddrSize)
	offset := 0

	if m.NodeMapAddress == nil {
		copy(buf, make([]byte, FileAddrSize))
	} else {
		copy(buf[offset:], m.NodeMapAddress.Hash)
		offset += FileHashSize

		binary.BigEndian.PutUint64(buf[offset:], uint64(m.NodeMapAddress.Size))
	}

	return buf
}

func (m *HeartbeatReply) String() string {
	return "HeartbeatReply"
}

// DataFetchMsg

type DataFetchMsg struct {
	Address    *grits.FileAddr
	Offset     uint64
	Length     int32
	TransferId string
}

func DecodeDataFetchMsg(data []byte) (Message, error) {
	if len(data) < 60 {
		return nil, errors.New("insufficient data for DataFetchMsg")
	}

	hash := data[:FileHashSize]
	size := binary.BigEndian.Uint64(data[FileHashSize:FileAddrSize])
	fileAddr := grits.NewFileAddr(hash, size)

	messageOffset := uint64(binary.BigEndian.Uint64(data[40:48]))
	length := int32(binary.BigEndian.Uint32(data[48:52]))
	transferId := string(data[52:60])

	return &DataFetchMsg{
		Address:    fileAddr,
		Offset:     messageOffset,
		Length:     length,
		TransferId: transferId,
	}, nil
}

func (m *DataFetchMsg) Encode() []byte {
	buffer := make([]byte, 60)

	encodeFileAddr(m.Address, buffer)

	binary.BigEndian.PutUint64(buffer[40:], uint64(m.Offset))
	binary.BigEndian.PutUint32(buffer[48:], uint32(m.Length))
	copy(buffer[52:], m.TransferId)

	return buffer
}

func (m *DataFetchMsg) String() string {
	return fmt.Sprintf("DataFetchMsg { Address: { Hash: %x, Size: %d }, Offset: %d, Length: %d, TransferId: %s }",
		m.Address.Hash, m.Address.Size, m.Offset, m.Length, m.TransferId)
}

// DataFetchReplyOk

type DataFetchReplyOk struct {
	Address *grits.FileAddr
	Offset  uint64
	Length  int32
	Data    []byte
}

func DecodeDataFetchReplyOk(data []byte) (Message, error) {
	if len(data) < 52 {
		return nil, errors.New("insufficient data for DataFetchReplyOk")
	}

	fileAddr := decodeFileAddr(data)
	messageOffset := binary.BigEndian.Uint64(data[40:48]) // uint64 here
	length := int32(binary.BigEndian.Uint32(data[48:52]))
	dataBytes := data[52:]

	if length != int32(len(dataBytes)) {
		return nil, errors.New("data length mismatch")
	}

	return &DataFetchReplyOk{
		Address: fileAddr,
		Offset:  messageOffset,
		Length:  length,
		Data:    dataBytes,
	}, nil
}

func (m *DataFetchReplyOk) Encode() []byte {
	headerBuffer := make([]byte, 52)

	encodeFileAddr(m.Address, headerBuffer)

	binary.BigEndian.PutUint64(headerBuffer[40:], m.Offset)
	binary.BigEndian.PutUint32(headerBuffer[48:], uint32(m.Length))

	return append(headerBuffer, m.Data...)
}

func (m *DataFetchReplyOk) String() string {
	return fmt.Sprintf("DataFetchReplyOk { Address: %v, Offset: %d, Length: %d, Data Length: %d }",
		m.Address, m.Offset, m.Length, len(m.Data))
}

// DataFetchReplyNo

type DataFetchReplyNo struct {
	Address *grits.FileAddr
}

func DecodeDataFetchReplyNo(data []byte) (Message, error) {
	if len(data) < FileAddrSize {
		return nil, errors.New("insufficient data for DataFetchReplyNo")
	}

	fileAddr := decodeFileAddr(data)

	return &DataFetchReplyNo{
		Address: fileAddr,
	}, nil
}

func (m *DataFetchReplyNo) Encode() []byte {
	buffer := make([]byte, FileAddrSize)

	encodeFileAddr(m.Address, buffer)

	return buffer
}

func (m *DataFetchReplyNo) String() string {
	return fmt.Sprintf("DataFetchReplyNo { Address: %v }", m.Address)
}

// DhtStoreMsg represents a request to store into the DHT
type DhtStoreMsg struct {
	Address *grits.FileAddr
}

func NewDhtStoreMsg(fileAddr *grits.FileAddr) *DhtStoreMsg {
	return &DhtStoreMsg{Address: fileAddr}
}

func (m *DhtStoreMsg) Encode() []byte {
	buf := make([]byte, FileAddrSize)

	copy(buf[:FileHashSize], m.Address.Hash)
	binary.BigEndian.PutUint64(buf[FileHashSize:], m.Address.Size)

	return buf
}

func DecodeDhtStoreMsg(data []byte) (Message, error) {
	if len(data) < FileAddrSize {
		return nil, fmt.Errorf("data slice is too short for decoding DhtStoreMsg, expected at least %d bytes, got %d", FileAddrSize, len(data))
	}

	hash := data[:FileHashSize]
	size := binary.BigEndian.Uint64(data[FileHashSize:FileAddrSize])

	fileAddr := grits.NewFileAddr(hash, size)

	return NewDhtStoreMsg(fileAddr), nil
}

func (m *DhtStoreMsg) String() string {
	return fmt.Sprintf("DhtStoreMsg { Address: { Hash: %x, Size: %d } }", m.Address.Hash, m.Address.Size)
}

type DhtStoreReply struct {
	Address *grits.FileAddr
}

func NewDhtStoreReply(address *grits.FileAddr) *DhtStoreReply {
	return &DhtStoreReply{Address: address}
}

func (r *DhtStoreReply) Encode() []byte {
	buf := make([]byte, FileAddrSize)

	copy(buf[:FileHashSize], r.Address.Hash)
	binary.BigEndian.PutUint64(buf[FileHashSize:], r.Address.Size)

	return buf
}

func DecodeDhtStoreReply(data []byte) (Message, error) {
	if len(data) < FileAddrSize {
		return nil, fmt.Errorf("data slice is too short for decoding DhtStoreReply, expected at least %d bytes, got %d", FileAddrSize, len(data))
	}

	hash := data[:FileHashSize]
	size := binary.BigEndian.Uint64(data[FileHashSize:FileAddrSize])

	address := grits.NewFileAddr(hash, size)

	return NewDhtStoreReply(address), nil
}

func (r *DhtStoreReply) String() string {
	return fmt.Sprintf("DhtStoreReply { Address: { Hash: %x, Size: %d } }", r.Address.Hash, r.Address.Size)
}

const TransferIdSize = 8

// DhtLookupMsg represents the message for looking up data in the DHT.
type DhtLookupMsg struct {
	Address    *grits.FileAddr
	TransferId string
}

func NewDhtLookupMsg(address *grits.FileAddr, transferId string) *DhtLookupMsg {
	return &DhtLookupMsg{Address: address, TransferId: transferId}
}

func (m *DhtLookupMsg) Encode() []byte {
	buf := make([]byte, FileAddrSize+TransferIdSize)

	// Use helper function to encode the FileAddr
	encodeFileAddr(m.Address, buf[:FileAddrSize])

	// Add TransferId to the buffer
	copy(buf[FileAddrSize:], m.TransferId)

	return buf
}

func DecodeDhtLookupMsg(data []byte) (Message, error) {
	if len(data) < FileAddrSize+TransferIdSize {
		return nil, fmt.Errorf("data slice is too short for decoding DhtLookupMsg, expected at least %d bytes, got %d", FileAddrSize+TransferIdSize, len(data))
	}

	// Use helper function to decode the FileAddr
	address := decodeFileAddr(data[:FileAddrSize])

	// Extract the TransferId
	transferId := string(data[FileAddrSize : FileAddrSize+TransferIdSize])

	return NewDhtLookupMsg(address, transferId), nil
}

func (m *DhtLookupMsg) String() string {
	return fmt.Sprintf("DhtLookupMsg { Address: { Hash: %x, Size: %d }, TransferId: %s }", m.Address.Hash, m.Address.Size, m.TransferId)
}

// DhtLookupReply represents the response for looking up data in the DHT.

const (
	ProtocolEndSignal  = 97
	ProtocolIPv4Signal = 98
)

type DhtLookupReply struct {
	Address  *grits.FileAddr
	Location []grits.Peer
}

func NewDhtLookupReply(address *grits.FileAddr, location []grits.Peer) *DhtLookupReply {
	return &DhtLookupReply{Address: address, Location: location}
}

func (r *DhtLookupReply) Encode() []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 40+len(r.Location)*8+1))

	// Encode Address using helper function
	addrBuf := make([]byte, FileAddrSize)
	encodeFileAddr(r.Address, addrBuf)
	buf.Write(addrBuf)

	// Encode nodeInfo
	for _, peer := range r.Location {
		ipParts := strings.Split(peer.Ip, ".")
		buf.WriteByte(98) // Protocol type
		buf.WriteByte(6)  // Node size

		for _, part := range ipParts {
			octet, _ := strconv.Atoi(part)
			buf.WriteByte(byte(octet))
		}

		portBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(portBuf, uint16(peer.Port))
		buf.Write(portBuf)
	}

	buf.WriteByte(97)                                 // End signal
	return buf.Bytes()
}

func DecodeDhtLookupReply(data []byte) (Message, error) {
	if len(data) < 40 {
		return nil, errors.New("data slice is too short for decoding DhtLookupReply")
	}

	address := decodeFileAddr(data[:40])

	var nodeInfo []grits.Peer
	offset := 40
	for offset < len(data) {
		protocolType := data[offset]
		offset++

		switch protocolType {
		case ProtocolEndSignal: // End signal
			return NewDhtLookupReply(address, nodeInfo), nil
		case ProtocolIPv4Signal:
			blockBytes := data[offset]
			offset++
			
			if blockBytes < 6 || len(data) < offset+6 {
				return nil, errors.New("malformed data in DhtLookupReply")
			}
			ipBytes := data[offset : offset+4]
			ip := fmt.Sprintf("%d.%d.%d.%d", ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3])
			port := binary.BigEndian.Uint16(data[offset+4 : offset+6])
			nodeInfo = append(nodeInfo, grits.Peer{Ip: net.ParseIP(ip).String(), Port: int(port)})
			offset += 6
		default:
			nodeSize := int(data[offset])
			offset += nodeSize
			if offset > len(data) {
				return nil, errors.New("malformed data in DhtLookupReply")
			}
		}
	}
	return nil, errors.New("unexpected end of data in DhtLookupReply without end signal")
}

func (r *DhtLookupReply) String() string {
	return "DhtLookupReply"
}

// Helper functions

func encodeFileAddr(address *grits.FileAddr, buffer []byte) {
	if len(buffer) < 40 {
		panic("buffer must be at least 40 bytes long")
	}

	// Copying the hash
	copy(buffer[:32], address.Hash)

	// Encoding the size
	binary.BigEndian.PutUint64(buffer[32:], address.Size)
}

func decodeFileAddr(data []byte) *grits.FileAddr {
	if len(data) < 40 {
		panic("data slice is too short for decoding FileAddr")
	}

	// Extract the hash and size
	hash := data[:32]
	size := binary.BigEndian.Uint64(data[32:40])

	return &grits.FileAddr{
		Hash: hash,
		Size: size,
	}
}
