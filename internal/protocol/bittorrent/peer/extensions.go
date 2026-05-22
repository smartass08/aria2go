package peer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/smartass08/aria2go/internal/bencode"
)

const (
	ExtensionUTMetadata = iota
	ExtensionUTPex
)

const (
	ExtensionHandshakeID    = 0
	ExtensionNameUTMetadata = "ut_metadata"
	ExtensionNameUTPex      = "ut_pex"
	utPexSeederFlag         = 0x02
	MetadataPieceSize       = 16 * 1024
	MaxMetadataSize         = 8 * 1024 * 1024
)

type ExtendedHandshake struct {
	ClientVersion string
	TCPPort       uint16
	MetadataSize  int
	Extensions    map[string]uint8
}

type ExtensionHandshake = ExtendedHandshake

type PEXPeer struct {
	IP     net.IP
	Port   uint16
	Seeder bool
}

type UTPexMessage struct {
	Added   []PEXPeer
	Dropped []PEXPeer
}

type UTMetadataMessageType int64

const (
	UTMetadataRequest UTMetadataMessageType = iota
	UTMetadataData
	UTMetadataReject
)

type UTMetadataMessage struct {
	MessageType UTMetadataMessageType
	Piece       int
	TotalSize   int
	Data        []byte
}

func EncodeExtendedHandshakeKeys(clientVersion string, port uint16, metadataSize int, extensions map[int]uint8) ([]byte, error) {
	nameMap := make(map[string]uint8, len(extensions))
	for key, id := range extensions {
		if id == 0 {
			continue
		}
		switch key {
		case ExtensionUTMetadata:
			nameMap[ExtensionNameUTMetadata] = id
		case ExtensionUTPex:
			nameMap[ExtensionNameUTPex] = id
		}
	}
	return EncodeExtendedHandshake(ExtendedHandshake{
		ClientVersion: clientVersion,
		TCPPort:       port,
		MetadataSize:  metadataSize,
		Extensions:    nameMap,
	})
}

func EncodeExtendedHandshake(hs ExtendedHandshake) ([]byte, error) {
	dict := bencode.NewDict()
	if hs.ClientVersion != "" {
		dict.Set("v", bencode.NewString(hs.ClientVersion))
	}
	if hs.TCPPort > 0 {
		dict.Set("p", bencode.NewInt(int64(hs.TCPPort)))
	}
	extDict := bencode.NewDict()
	for name, id := range hs.Extensions {
		if id == 0 {
			continue
		}
		extDict.Set(name, bencode.NewInt(int64(id)))
	}
	dict.Set("m", extDict)
	if hs.MetadataSize > 0 {
		dict.Set("metadata_size", bencode.NewInt(int64(hs.MetadataSize)))
	}
	return bencode.Marshal(dict)
}

func ParseExtendedHandshake(payload []byte) (ExtendedHandshake, error) {
	var raw bencode.Value
	if err := bencode.Unmarshal(payload, &raw); err != nil {
		return ExtendedHandshake{}, fmt.Errorf("peer: decode extension handshake: %w", err)
	}
	dict, ok := raw.(*bencode.DictVal)
	if !ok {
		return ExtendedHandshake{}, fmt.Errorf("%w: extended handshake payload must be dictionary", ErrProtocolViolation)
	}

	hs := ExtendedHandshake{Extensions: make(map[string]uint8)}
	if portVal, ok := dict.Get("p"); ok {
		if iv, ok := portVal.(bencode.IntVal); ok && iv.I > 0 && iv.I < 65536 {
			hs.TCPPort = uint16(iv.I)
		}
	}
	if versionVal, ok := dict.Get("v"); ok {
		if sv, ok := versionVal.(bencode.StringVal); ok {
			hs.ClientVersion = sv.S
		}
	}
	if metadataVal, ok := dict.Get("metadata_size"); ok {
		if iv, ok := metadataVal.(bencode.IntVal); ok && iv.I > 0 && iv.I <= MaxMetadataSize {
			hs.MetadataSize = int(iv.I)
		}
	}
	if extVal, ok := dict.Get("m"); ok {
		if extDict, ok := extVal.(*bencode.DictVal); ok {
			for _, key := range extDict.Keys {
				value, ok := extDict.Values[key].(bencode.IntVal)
				if !ok || value.I <= 0 || value.I > 255 {
					continue
				}
				hs.Extensions[key] = uint8(value.I)
			}
		}
	}
	return hs, nil
}

func ParseExtensionHandshake(msg Message) (ExtendedHandshake, error) {
	id, payload, err := UnmarshalExtended(msg)
	if err != nil {
		return ExtendedHandshake{}, err
	}
	if id != ExtensionHandshakeID {
		return ExtendedHandshake{}, fmt.Errorf("%w: expected extended handshake id 0, got %d", ErrProtocolViolation, id)
	}
	return ParseExtendedHandshake(payload)
}

func MarshalExtensionHandshake(extensions map[string]uint8, port uint16) ([]byte, error) {
	payload, err := EncodeExtendedHandshake(ExtendedHandshake{
		TCPPort:    port,
		Extensions: extensions,
	})
	if err != nil {
		return nil, err
	}
	return MarshalExtended(ExtensionHandshakeID, payload), nil
}

func MarshalExtended(id uint8, payload []byte) []byte {
	return NewMessage(MsgExtended, append([]byte{id}, payload...)).Encode()
}

func UnmarshalExtended(msg Message) (uint8, []byte, error) {
	if msg.ID != MsgExtended {
		return 0, nil, fmt.Errorf("peer: expected extended message, got %d", msg.ID)
	}
	if len(msg.Payload) < 1 {
		return 0, nil, fmt.Errorf("%w: extended message missing extension id", ErrProtocolViolation)
	}
	return msg.Payload[0], cloneBytes(msg.Payload[1:]), nil
}

func MarshalUTPex(extID uint8, added, dropped []PEXPeer) ([]byte, error) {
	payload, err := MarshalUTPexPayload(added, dropped)
	if err != nil {
		return nil, err
	}
	return MarshalExtended(extID, payload), nil
}

func MarshalUTPexPayload(added, dropped []PEXPeer) ([]byte, error) {
	dict := bencode.NewDict()
	if v4, flags, v6, flags6 := encodePEXPeers(added); len(v4) > 0 || len(v6) > 0 {
		if len(v4) > 0 {
			dict.Set("added", bencode.NewString(string(v4)))
			dict.Set("added.f", bencode.NewString(string(flags)))
		}
		if len(v6) > 0 {
			dict.Set("added6", bencode.NewString(string(v6)))
			dict.Set("added6.f", bencode.NewString(string(flags6)))
		}
	}
	if v4, _, v6, _ := encodePEXPeers(dropped); len(v4) > 0 || len(v6) > 0 {
		if len(v4) > 0 {
			dict.Set("dropped", bencode.NewString(string(v4)))
		}
		if len(v6) > 0 {
			dict.Set("dropped6", bencode.NewString(string(v6)))
		}
	}
	data, err := bencode.Marshal(dict)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func ParseUTPex(msg Message) (uint8, UTPexMessage, error) {
	extID, payload, err := UnmarshalExtended(msg)
	if err != nil {
		return 0, UTPexMessage{}, err
	}
	var raw bencode.Value
	if err := bencode.Unmarshal(payload, &raw); err != nil {
		return 0, UTPexMessage{}, fmt.Errorf("peer: decode ut_pex: %w", err)
	}
	dict, ok := raw.(*bencode.DictVal)
	if !ok {
		return 0, UTPexMessage{}, fmt.Errorf("%w: ut_pex payload must be dictionary", ErrProtocolViolation)
	}

	var out UTPexMessage
	out.Added = append(out.Added, decodePEXCompact(dict, "added", "added.f", false)...)
	out.Added = append(out.Added, decodePEXCompact(dict, "added6", "added6.f", true)...)
	out.Dropped = append(out.Dropped, decodePEXCompact(dict, "dropped", "", false)...)
	out.Dropped = append(out.Dropped, decodePEXCompact(dict, "dropped6", "", true)...)
	return extID, out, nil
}

func UnmarshalPort(msg Message) (uint16, error) {
	if msg.ID != MsgPort {
		return 0, fmt.Errorf("peer: expected port message, got %d", msg.ID)
	}
	if len(msg.Payload) != 2 {
		return 0, fmt.Errorf("%w: port payload length = %d, want 2", ErrProtocolViolation, len(msg.Payload))
	}
	return binary.BigEndian.Uint16(msg.Payload), nil
}

func EncodeUTMetadataRequest(piece int) ([]byte, error) {
	dict := bencode.NewDict()
	dict.Set("msg_type", bencode.NewInt(int64(UTMetadataRequest)))
	dict.Set("piece", bencode.NewInt(int64(piece)))
	return bencode.Marshal(dict)
}

func EncodeUTMetadataData(piece int, totalSize int, data []byte) ([]byte, error) {
	dict := bencode.NewDict()
	dict.Set("msg_type", bencode.NewInt(int64(UTMetadataData)))
	dict.Set("piece", bencode.NewInt(int64(piece)))
	dict.Set("total_size", bencode.NewInt(int64(totalSize)))
	header, err := bencode.Marshal(dict)
	if err != nil {
		return nil, err
	}
	return append(header, data...), nil
}

func EncodeUTMetadataReject(piece int) ([]byte, error) {
	dict := bencode.NewDict()
	dict.Set("msg_type", bencode.NewInt(int64(UTMetadataReject)))
	dict.Set("piece", bencode.NewInt(int64(piece)))
	return bencode.Marshal(dict)
}

func ParseUTMetadata(payload []byte) (UTMetadataMessage, error) {
	dictEnd, err := scanBencodeValue(payload, 0)
	if err != nil {
		return UTMetadataMessage{}, fmt.Errorf("peer: decode ut_metadata: %w", err)
	}

	var raw bencode.Value
	if err := bencode.Unmarshal(payload[:dictEnd], &raw); err != nil {
		return UTMetadataMessage{}, fmt.Errorf("peer: decode ut_metadata: %w", err)
	}
	dict, ok := raw.(*bencode.DictVal)
	if !ok {
		return UTMetadataMessage{}, fmt.Errorf("%w: ut_metadata payload must be dictionary", ErrProtocolViolation)
	}
	msgTypeVal, ok := dict.Get("msg_type")
	if !ok {
		return UTMetadataMessage{}, fmt.Errorf("%w: ut_metadata msg_type missing", ErrProtocolViolation)
	}
	msgTypeInt, ok := msgTypeVal.(bencode.IntVal)
	if !ok {
		return UTMetadataMessage{}, fmt.Errorf("%w: ut_metadata msg_type invalid", ErrProtocolViolation)
	}
	pieceVal, ok := dict.Get("piece")
	if !ok {
		return UTMetadataMessage{}, fmt.Errorf("%w: ut_metadata piece missing", ErrProtocolViolation)
	}
	pieceInt, ok := pieceVal.(bencode.IntVal)
	if !ok || pieceInt.I < 0 {
		return UTMetadataMessage{}, fmt.Errorf("%w: ut_metadata piece invalid", ErrProtocolViolation)
	}

	msg := UTMetadataMessage{
		MessageType: UTMetadataMessageType(msgTypeInt.I),
		Piece:       int(pieceInt.I),
	}
	if totalSizeVal, ok := dict.Get("total_size"); ok {
		if totalSizeInt, ok := totalSizeVal.(bencode.IntVal); ok && totalSizeInt.I >= 0 {
			msg.TotalSize = int(totalSizeInt.I)
		}
	}
	if msg.MessageType == UTMetadataData {
		msg.Data = cloneBytes(payload[dictEnd:])
	}
	return msg, nil
}

func scanBencodeValue(data []byte, pos int) (int, error) {
	if pos >= len(data) {
		return 0, errors.New("unexpected end of data")
	}

	switch {
	case data[pos] == 'i':
		eIdx := bytes.IndexByte(data[pos+1:], 'e')
		if eIdx < 0 {
			return 0, errors.New("unterminated integer")
		}
		return pos + 1 + eIdx + 1, nil

	case data[pos] >= '0' && data[pos] <= '9':
		colonIdx := bytes.IndexByte(data[pos:], ':')
		if colonIdx < 0 {
			return 0, errors.New("invalid string")
		}
		length, err := parseBencodeDecimal(data[pos : pos+colonIdx])
		if err != nil {
			return 0, err
		}
		end := pos + colonIdx + 1 + length
		if end > len(data) {
			return 0, errors.New("string exceeds data bounds")
		}
		return end, nil

	case data[pos] == 'l' || data[pos] == 'd':
		depth := 1
		pos++
		for depth > 0 && pos < len(data) {
			switch {
			case data[pos] == 'l' || data[pos] == 'd':
				depth++
				pos++
			case data[pos] == 'e':
				depth--
				pos++
			case data[pos] == 'i':
				eIdx := bytes.IndexByte(data[pos+1:], 'e')
				if eIdx < 0 {
					return 0, errors.New("unterminated integer in nested")
				}
				pos += eIdx + 2
			case data[pos] >= '0' && data[pos] <= '9':
				colonIdx := bytes.IndexByte(data[pos:], ':')
				if colonIdx < 0 {
					return 0, errors.New("invalid string in nested")
				}
				length, err := parseBencodeDecimal(data[pos : pos+colonIdx])
				if err != nil {
					return 0, err
				}
				next := pos + colonIdx + 1 + length
				if next > len(data) {
					return 0, errors.New("string exceeds data bounds")
				}
				pos = next
			default:
				return 0, fmt.Errorf("unexpected byte 0x%02x", data[pos])
			}
		}
		if depth != 0 {
			return 0, errors.New("unterminated list/dict")
		}
		return pos, nil

	default:
		return 0, fmt.Errorf("unexpected byte 0x%02x", data[pos])
	}
}

func parseBencodeDecimal(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, errors.New("empty decimal")
	}
	i, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, err
	}
	if i > int64(int(^uint(0)>>1)) {
		return 0, errors.New("decimal exceeds int range")
	}
	return int(i), nil
}

func encodePEXPeers(peers []PEXPeer) (v4 []byte, flags4 []byte, v6 []byte, flags6 []byte) {
	for _, peer := range peers {
		if ip4 := peer.IP.To4(); ip4 != nil {
			v4 = append(v4, ip4...)
			var port [2]byte
			binary.BigEndian.PutUint16(port[:], peer.Port)
			v4 = append(v4, port[:]...)
			if peer.Seeder {
				flags4 = append(flags4, utPexSeederFlag)
			} else {
				flags4 = append(flags4, 0)
			}
			continue
		}
		ip6 := peer.IP.To16()
		if ip6 == nil {
			continue
		}
		v6 = append(v6, ip6...)
		var port [2]byte
		binary.BigEndian.PutUint16(port[:], peer.Port)
		v6 = append(v6, port[:]...)
		if peer.Seeder {
			flags6 = append(flags6, utPexSeederFlag)
		} else {
			flags6 = append(flags6, 0)
		}
	}
	return v4, flags4, v6, flags6
}

func decodePEXCompact(dict *bencode.DictVal, peerKey, flagKey string, ipv6 bool) []PEXPeer {
	value, ok := dict.Get(peerKey)
	if !ok {
		return nil
	}
	peerStr, ok := value.(bencode.StringVal)
	if !ok {
		return nil
	}
	flags := []byte(nil)
	if flagKey != "" {
		if flagVal, ok := dict.Get(flagKey); ok {
			if flagStr, ok := flagVal.(bencode.StringVal); ok {
				flags = []byte(flagStr.S)
			}
		}
	}
	return decodePEXPeers([]byte(peerStr.S), flags, ipv6)
}

func decodePEXPeers(data, flags []byte, ipv6 bool) []PEXPeer {
	unit := 6
	ipLen := 4
	if ipv6 {
		unit = 18
		ipLen = 16
	}
	if len(data) == 0 || len(data)%unit != 0 {
		return nil
	}
	peers := make([]PEXPeer, 0, len(data)/unit)
	for off := 0; off < len(data); off += unit {
		var ip net.IP
		if ipv6 {
			ip = make(net.IP, net.IPv6len)
		} else {
			ip = make(net.IP, net.IPv4len)
		}
		copy(ip, data[off:off+ipLen])
		port := binary.BigEndian.Uint16(data[off+ipLen : off+unit])
		seeder := false
		if idx := off / unit; idx < len(flags) {
			seeder = flags[idx]&utPexSeederFlag != 0
		}
		peers = append(peers, PEXPeer{IP: ip, Port: port, Seeder: seeder})
	}
	return peers
}
