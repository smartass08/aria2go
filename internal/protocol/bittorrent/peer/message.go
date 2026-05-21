package peer

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
)

type MessageID uint8

const (
	MsgChoke         MessageID = 0
	MsgUnchoke       MessageID = 1
	MsgInterested    MessageID = 2
	MsgNotInterested MessageID = 3
	MsgHave          MessageID = 4
	MsgBitfield      MessageID = 5
	MsgRequest       MessageID = 6
	MsgPiece         MessageID = 7
	MsgCancel        MessageID = 8
	MsgPort          MessageID = 9
	MsgSuggest       MessageID = 13
	MsgHaveAll       MessageID = 14
	MsgHaveNone      MessageID = 15
	MsgReject        MessageID = 16
	MsgAllowedFast   MessageID = 17
	MsgExtended      MessageID = 20
)

const (
	maxBlockLength    = 64 * 1024
	maxBufferCapacity = maxBlockLength + 128
)

type Message struct {
	ID      MessageID
	Payload []byte
}

func NewMessage(id MessageID, payload []byte) Message {
	return Message{ID: id, Payload: payload}
}

func (m Message) Encode() []byte {
	payloadLen := 1 + len(m.Payload)
	buf := make([]byte, 4+payloadLen)
	binary.BigEndian.PutUint32(buf[:4], uint32(payloadLen))
	buf[4] = byte(m.ID)
	copy(buf[5:], m.Payload)
	return buf
}

func KeepAlive() []byte {
	return []byte{0, 0, 0, 0}
}

func DecodeMessage(data []byte) (Message, error) {
	if len(data) < 4 {
		return Message{}, fmt.Errorf("peer: message too short: %d bytes", len(data))
	}
	length := binary.BigEndian.Uint32(data[:4])
	if length == 0 {
		return Message{}, fmt.Errorf("peer: keep-alive has no message ID")
	}
	if uint32(len(data)) < 4+length {
		return Message{}, fmt.Errorf("peer: incomplete message: have %d, need %d", len(data), 4+length)
	}
	if length > uint32(maxBufferCapacity) {
		return Message{}, fmt.Errorf("peer: message too long: %d", length)
	}
	msg := Message{
		ID:      MessageID(data[4]),
		Payload: cloneBytes(data[5 : 4+length]),
	}
	if err := validatePayloadShape(msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func validatePayloadShape(msg Message) error {
	switch msg.ID {
	case MsgChoke, MsgUnchoke, MsgInterested, MsgNotInterested, MsgHaveAll, MsgHaveNone:
		if len(msg.Payload) != 0 {
			return fmt.Errorf("%w: message %d must not have payload", ErrProtocolViolation, msg.ID)
		}
	case MsgHave, MsgSuggest, MsgAllowedFast:
		if len(msg.Payload) != 4 {
			return fmt.Errorf("%w: message %d payload length = %d, want 4", ErrProtocolViolation, msg.ID, len(msg.Payload))
		}
	case MsgRequest, MsgCancel, MsgReject:
		if len(msg.Payload) != 12 {
			return fmt.Errorf("%w: message %d payload length = %d, want 12", ErrProtocolViolation, msg.ID, len(msg.Payload))
		}
	case MsgPiece:
		if len(msg.Payload) <= 8 {
			return fmt.Errorf("%w: piece payload too short: %d bytes", ErrProtocolViolation, len(msg.Payload))
		}
		if len(msg.Payload)-8 > maxBlockLength {
			return fmt.Errorf("%w: piece block too long: %d", ErrProtocolViolation, len(msg.Payload)-8)
		}
	case MsgPort:
		if len(msg.Payload) != 2 {
			return fmt.Errorf("%w: port payload length = %d, want 2", ErrProtocolViolation, len(msg.Payload))
		}
	case MsgExtended:
		if len(msg.Payload) == 0 {
			return fmt.Errorf("%w: extended message missing extension id", ErrProtocolViolation)
		}
	}
	return nil
}

func validatePieceIndex(index, numPieces int) error {
	if numPieces > 0 && (index < 0 || index >= numPieces) {
		return fmt.Errorf("%w: piece index %d out of range [0,%d)", ErrProtocolViolation, index, numPieces)
	}
	return nil
}

func validateBlockRange(piece, offset, length, numPieces int, pieceLength int64) error {
	if err := validatePieceIndex(piece, numPieces); err != nil {
		return err
	}
	if offset < 0 {
		return fmt.Errorf("%w: negative block offset %d", ErrProtocolViolation, offset)
	}
	if length <= 0 {
		return fmt.Errorf("%w: invalid block length %d", ErrProtocolViolation, length)
	}
	if length > maxBlockLength {
		return fmt.Errorf("%w: block length too long: %d", ErrProtocolViolation, length)
	}
	if pieceLength > 0 {
		if int64(offset) >= pieceLength {
			return fmt.Errorf("%w: block offset %d out of piece length %d", ErrProtocolViolation, offset, pieceLength)
		}
		if int64(offset)+int64(length) > pieceLength {
			return fmt.Errorf("%w: block range exceeds piece length", ErrProtocolViolation)
		}
	}
	return nil
}

func validateBitfieldPayload(bf []byte, numPieces int) error {
	if numPieces < 0 {
		return fmt.Errorf("%w: negative piece count %d", ErrProtocolViolation, numPieces)
	}
	want := (numPieces + 7) / 8
	if len(bf) != want {
		return fmt.Errorf("%w: bitfield length = %d, want %d", ErrProtocolViolation, len(bf), want)
	}
	if len(bf) == 0 {
		return nil
	}
	unused := uint(numPieces % 8)
	if unused == 0 {
		return nil
	}
	mask := byte(0xff << (8 - unused))
	if bf[len(bf)-1]&^mask != 0 {
		return fmt.Errorf("%w: bitfield has spare bits set", ErrProtocolViolation)
	}
	return nil
}

func MarshalHave(piece int) []byte {
	bufp := bitfieldPayloadPool.Get().(*[]byte)
	buf := (*bufp)[:4]
	binary.BigEndian.PutUint32(buf, uint32(piece))
	data := NewMessage(MsgHave, buf).Encode()
	bitfieldPayloadPool.Put(bufp)
	return data
}

func MarshalBitfield(bf []byte) []byte {
	return NewMessage(MsgBitfield, cloneBytes(bf)).Encode()
}

func MarshalRequest(piece, offset, length int) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint32(buf[0:4], uint32(piece))
	binary.BigEndian.PutUint32(buf[4:8], uint32(offset))
	binary.BigEndian.PutUint32(buf[8:12], uint32(length))
	return NewMessage(MsgRequest, buf).Encode()
}

func MarshalPiece(piece, offset int, data []byte) []byte {
	buf := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(buf[0:4], uint32(piece))
	binary.BigEndian.PutUint32(buf[4:8], uint32(offset))
	copy(buf[8:], data)
	return NewMessage(MsgPiece, buf).Encode()
}

func MarshalCancel(piece, offset, length int) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint32(buf[0:4], uint32(piece))
	binary.BigEndian.PutUint32(buf[4:8], uint32(offset))
	binary.BigEndian.PutUint32(buf[8:12], uint32(length))
	return NewMessage(MsgCancel, buf).Encode()
}

func MarshalPort(port uint16) []byte {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, port)
	return NewMessage(MsgPort, buf).Encode()
}

func UnmarshalPiece(msg Message) (piece int, offset int, data []byte, err error) {
	if msg.ID != MsgPiece {
		return 0, 0, nil, fmt.Errorf("peer: expected piece message, got %d", msg.ID)
	}
	if len(msg.Payload) <= 8 {
		return 0, 0, nil, fmt.Errorf("peer: piece payload too short: %d bytes", len(msg.Payload))
	}
	piece = int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	offset = int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	data = cloneBytes(msg.Payload[8:])
	return piece, offset, data, nil
}

func UnmarshalHave(msg Message) (int, error) {
	if msg.ID != MsgHave {
		return 0, fmt.Errorf("peer: expected have message, got %d", msg.ID)
	}
	if len(msg.Payload) != 4 {
		return 0, fmt.Errorf("peer: have payload too short")
	}
	return int(binary.BigEndian.Uint32(msg.Payload[0:4])), nil
}

func UnmarshalRequest(msg Message) (piece, offset, length int, err error) {
	if msg.ID != MsgRequest {
		return 0, 0, 0, fmt.Errorf("peer: expected request message, got %d", msg.ID)
	}
	if len(msg.Payload) != 12 {
		return 0, 0, 0, fmt.Errorf("peer: request payload too short")
	}
	piece = int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	offset = int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	length = int(binary.BigEndian.Uint32(msg.Payload[8:12]))
	return piece, offset, length, nil
}

func UnmarshalCancel(msg Message) (piece, offset, length int, err error) {
	if msg.ID != MsgCancel {
		return 0, 0, 0, fmt.Errorf("peer: expected cancel message, got %d", msg.ID)
	}
	if len(msg.Payload) != 12 {
		return 0, 0, 0, fmt.Errorf("peer: cancel payload too short")
	}
	piece = int(binary.BigEndian.Uint32(msg.Payload[0:4]))
	offset = int(binary.BigEndian.Uint32(msg.Payload[4:8]))
	length = int(binary.BigEndian.Uint32(msg.Payload[8:12]))
	return piece, offset, length, nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

func ComputeAllowedFast(peerIP net.IP, numPieces int, infoHash [20]byte, fastSetSize int) []int {
	ip4 := peerIP.To4()
	if ip4 == nil {
		return nil
	}
	if numPieces <= 0 {
		return nil
	}
	if fastSetSize <= 0 {
		fastSetSize = 10
	}
	if numPieces < fastSetSize {
		fastSetSize = numPieces
	}
	tx := make([]byte, 24)
	copy(tx[:4], ip4)
	if (tx[0]&0x80) == 0 || (tx[0]&0x40) == 0 {
		tx[2] = 0x00
		tx[3] = 0x00
	} else {
		tx[3] = 0x00
	}
	copy(tx[4:24], infoHash[:])

	x := make([]byte, 20)
	h := sha1.New()
	h.Write(tx)
	h.Sum(x[:0])

	seen := make(map[int]bool, fastSetSize)
	result := make([]int, 0, fastSetSize)

	for len(result) < fastSetSize {
		for i := 0; i < 5 && len(result) < fastSetSize; i++ {
			y := binary.BigEndian.Uint32(x[i*4 : (i+1)*4])
			idx := int(y % uint32(numPieces))
			if !seen[idx] {
				seen[idx] = true
				result = append(result, idx)
			}
		}
		if len(result) >= fastSetSize {
			break
		}
		h.Reset()
		h.Write(x)
		x = x[:0]
		x = h.Sum(x)
	}
	return result
}
