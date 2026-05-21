package dht

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/smartass08/aria2go/internal/bencode"
)

// NodeID is a 20-byte DHT node identifier per BEP 5.
type NodeID [20]byte

// NodeIDLength is the standard DHT node ID size in bytes.
const NodeIDLength = 20

// Message is a KRPC message per BEP 5, with aria2-specific extensions.
//
// The V field carries the client version string (aria2 uses "A2" + htons(DHT_VERSION), 4 bytes).
// Other clients may omit it. When marshalling, V is included only when non-empty.
type Message struct {
	T string           // transaction ID (bencoded string)
	Y string           // "q"=query, "r"=response, "e"=error
	Q string           // query method (ping, find_node, get_peers, announce_peer)
	A *bencode.DictVal // query arguments
	R *bencode.DictVal // response values
	E [2]interface{}   // error: [code int64, message string]
	V string           // client version (4 bytes per aria2: "A2\x00\x03"). Always set; empty means unknown.
}

// ValidateID checks that an ID byte slice is exactly NodeIDLength bytes.
func ValidateID(id []byte) error {
	if len(id) != NodeIDLength {
		return fmt.Errorf("dht: invalid ID length: expected %d, got %d", NodeIDLength, len(id))
	}
	return nil
}

// ValidatePort checks that a port is in the valid range (0 < port < UINT16_MAX).
func ValidatePort(port uint16) error {
	if port == 0 || port == 65535 {
		return fmt.Errorf("dht: invalid port %d", port)
	}
	return nil
}

// Query names
const (
	QPing         = "ping"
	QFindNode     = "find_node"
	QGetPeers     = "get_peers"
	QAnnouncePeer = "announce_peer"
)

// CompactLenIPv4 and CompactLenIPv6 are the number of bytes for address+port in
// compact node encoding (not including the 20-byte node ID).
const (
	CompactLenIPv4 = 6
	CompactLenIPv6 = 18
)

// NodeInfo is a compact node contact: 20-byte ID + 4-byte IP + 2-byte port (IPv4).
type NodeInfo struct {
	ID   NodeID
	IP   [4]byte
	Port uint16
}

// Compact encodes NodeInfo into the 26-byte compact format (network byte order).
func (n NodeInfo) Compact() [26]byte {
	var out [26]byte
	copy(out[:], n.ID[:])
	copy(out[20:24], n.IP[:])
	binary.BigEndian.PutUint16(out[24:], n.Port)
	return out
}

// DecodeCompactNodeInfo decodes 26 compact bytes into a NodeInfo.
func DecodeCompactNodeInfo(data [26]byte) NodeInfo {
	return NodeInfo{
		ID:   *(*NodeID)(data[:20]),
		IP:   [4]byte(data[20:24]),
		Port: binary.BigEndian.Uint16(data[24:26]),
	}
}

// CompactNodes encodes []NodeInfo into a compact byte string (N * 26 bytes).
func CompactNodes(nodes []NodeInfo) []byte {
	n := len(nodes) * 26
	out := make([]byte, n)
	for i, node := range nodes {
		off := i * 26
		copy(out[off:off+20], node.ID[:])
		copy(out[off+20:off+24], node.IP[:])
		binary.BigEndian.PutUint16(out[off+24:], node.Port)
	}
	return out
}

// DecodeCompactNodes decodes a compact node string into []NodeInfo.
func DecodeCompactNodes(data []byte) ([]NodeInfo, error) {
	if len(data)%26 != 0 {
		return nil, fmt.Errorf("dht: compact nodes data length %d not multiple of 26", len(data))
	}
	if len(data) == 0 {
		return nil, nil
	}
	nodes := make([]NodeInfo, 0, len(data)/26)
	var zeroIP [4]byte
	for i := 0; i < len(data); i += 26 {
		chunk := data[i : i+26]
		ip := *(*[4]byte)(chunk[20:24])
		if ip == zeroIP {
			continue
		}
		nodes = append(nodes, NodeInfo{
			ID:   *(*NodeID)(chunk[:20]),
			IP:   ip,
			Port: binary.BigEndian.Uint16(chunk[24:26]),
		})
	}
	return nodes, nil
}

// V6NodeInfo is an IPv6 compact node contact: 20-byte ID + 16-byte IP + 2-byte port.
type V6NodeInfo struct {
	ID   NodeID
	IP   [16]byte
	Port uint16
}

// Compact encodes V6NodeInfo into the 38-byte compact format (network byte order).
func (n V6NodeInfo) Compact() [38]byte {
	var out [38]byte
	copy(out[:], n.ID[:])
	copy(out[20:36], n.IP[:])
	binary.BigEndian.PutUint16(out[36:], n.Port)
	return out
}

// DecodeCompactV6NodeInfo decodes 38 compact bytes into a V6NodeInfo.
func DecodeCompactV6NodeInfo(data [38]byte) V6NodeInfo {
	return V6NodeInfo{
		ID:   *(*NodeID)(data[:20]),
		IP:   [16]byte(data[20:36]),
		Port: binary.BigEndian.Uint16(data[36:38]),
	}
}

// CompactV6Nodes encodes []V6NodeInfo into a compact byte string (N * 38 bytes).
func CompactV6Nodes(nodes []V6NodeInfo) []byte {
	n := len(nodes) * 38
	out := make([]byte, n)
	for i, node := range nodes {
		off := i * 38
		copy(out[off:off+20], node.ID[:])
		copy(out[off+20:off+36], node.IP[:])
		binary.BigEndian.PutUint16(out[off+36:], node.Port)
	}
	return out
}

// DecodeCompactV6Nodes decodes a compact node string into []V6NodeInfo.
func DecodeCompactV6Nodes(data []byte) ([]V6NodeInfo, error) {
	if len(data)%38 != 0 {
		return nil, fmt.Errorf("dht: compact v6 nodes data length %d not multiple of 38", len(data))
	}
	if len(data) == 0 {
		return nil, nil
	}
	nodes := make([]V6NodeInfo, 0, len(data)/38)
	var zeroIP [16]byte
	for i := 0; i < len(data); i += 38 {
		chunk := data[i : i+38]
		ip := *(*[16]byte)(chunk[20:36])
		if ip == zeroIP {
			continue
		}
		nodes = append(nodes, V6NodeInfo{
			ID:   *(*NodeID)(chunk[:20]),
			IP:   ip,
			Port: binary.BigEndian.Uint16(chunk[36:38]),
		})
	}
	return nodes, nil
}

// NewQuery creates a query message.
func NewQuery(method string, args *bencode.DictVal) *Message {
	if args == nil {
		args = bencode.NewDict()
	}
	return &Message{
		Y: "q",
		Q: method,
		A: args,
	}
}

// NewResponse creates a response message.
func NewResponse(t string, r *bencode.DictVal) *Message {
	if r == nil {
		r = bencode.NewDict()
	}
	return &Message{
		T: t,
		Y: "r",
		R: r,
	}
}

// NewError creates an error message.
func NewError(t string, code int64, msg string) *Message {
	return &Message{
		T: t,
		Y: "e",
		E: [2]interface{}{code, msg},
	}
}

// Marshal encodes a Message to bencode bytes.
func (m *Message) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	if err := m.MarshalTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MarshalTo writes the bencode representation of m directly to buf, avoiding
// intermediate DictVal/String allocations for the outer dict envelope.
// Keys are emitted in sorted order per the bencode spec.
func (m *Message) MarshalTo(buf *bytes.Buffer) error {
	buf.WriteByte('d')

	switch m.Y {
	case "q":
		aEnc := "de"
		if m.A != nil {
			aEnc = m.A.String()
		}
		writeBencodeRaw(buf, "a", aEnc)
		writeBencodeString(buf, "q", m.Q)
		writeBencodeString(buf, "t", m.T)
		writeBencodeString(buf, "v", m.V)
		writeBencodeString(buf, "y", m.Y)
	case "r":
		rEnc := "de"
		if m.R != nil {
			rEnc = m.R.String()
		}
		writeBencodeRaw(buf, "r", rEnc)
		writeBencodeString(buf, "t", m.T)
		writeBencodeString(buf, "v", m.V)
		writeBencodeString(buf, "y", m.Y)
	case "e":
		buf.WriteString("1:el")
		fmt.Fprintf(buf, "i%de", m.E[0].(int64))
		escaped := m.E[1].(string)
		fmt.Fprintf(buf, "%d:%s", len(escaped), escaped)
		buf.WriteByte('e')
		writeBencodeString(buf, "t", m.T)
		writeBencodeString(buf, "v", m.V)
		writeBencodeString(buf, "y", m.Y)
	default:
		return fmt.Errorf("dht: unknown message type %q", m.Y)
	}

	buf.WriteByte('e')
	return nil
}

func writeBencodeString(buf *bytes.Buffer, key, value string) {
	fmt.Fprintf(buf, "%d:%s%d:%s", len(key), key, len(value), value)
}

func writeBencodeRaw(buf *bytes.Buffer, key, raw string) {
	fmt.Fprintf(buf, "%d:%s%s", len(key), key, raw)
}

// ClientVersion returns the aria2-compatible DHT client version string.
// Format: "A2" + htons(DHT_VERSION) per BEP 5 + aria2 DHTConstants.h.
func ClientVersion() string {
	buf := make([]byte, 4)
	copy(buf[:2], "A2")
	binary.BigEndian.PutUint16(buf[2:], dhtVersion)
	return string(buf)
}

var clientVersion = ClientVersion()

const dhtVersion = 3

// ErrInvalidMessage is returned when a KRPC message fails structural validation.
var ErrInvalidMessage = errors.New("dht: invalid message")

// Unmarshal decodes bencode bytes into a Message.
func Unmarshal(data []byte) (*Message, error) {
	v, err := beDecode(data)
	if err != nil {
		return nil, fmt.Errorf("dht: %w", err)
	}
	d, ok := v.(*bencode.DictVal)
	if !ok {
		return nil, fmt.Errorf("dht: expected dict at top level, got %s", v.Kind())
	}

	var m Message

	tv, ok := d.Get("t")
	if !ok {
		return nil, fmt.Errorf("dht: %w: missing 't' key", ErrInvalidMessage)
	}
	tsv, ok := tv.(bencode.StringVal)
	if !ok {
		return nil, fmt.Errorf("dht: %w: 't' must be string, got %s", ErrInvalidMessage, tv.Kind())
	}
	m.T = tsv.S

	yv, ok := d.Get("y")
	if !ok {
		return nil, fmt.Errorf("dht: %w: missing 'y' key", ErrInvalidMessage)
	}
	ysv, ok := yv.(bencode.StringVal)
	if !ok {
		return nil, fmt.Errorf("dht: %w: 'y' must be string, got %s", ErrInvalidMessage, yv.Kind())
	}
	m.Y = ysv.S

	// Optional: client version string (aria2 sends "A2" + htons(DHT_VERSION))
	if vv, ok := d.Get("v"); ok {
		if vsv, ok := vv.(bencode.StringVal); ok {
			m.V = vsv.S
		}
	}

	switch m.Y {
	case "q":
		qv, ok := d.Get("q")
		if !ok {
			return nil, fmt.Errorf("dht: %w: missing 'q' key in query", ErrInvalidMessage)
		}
		qsv, ok := qv.(bencode.StringVal)
		if !ok {
			return nil, fmt.Errorf("dht: %w: 'q' must be string, got %s", ErrInvalidMessage, qv.Kind())
		}
		m.Q = qsv.S

		av, ok := d.Get("a")
		if !ok {
			return nil, fmt.Errorf("dht: %w: missing 'a' key in query", ErrInvalidMessage)
		}
		adv, ok := av.(*bencode.DictVal)
		if !ok {
			return nil, fmt.Errorf("dht: %w: 'a' must be dict, got %s", ErrInvalidMessage, av.Kind())
		}
		m.A = adv
		if err := validateDictNodeID("a", m.A); err != nil {
			return nil, err
		}

	case "r":
		rv, ok := d.Get("r")
		if !ok {
			return nil, fmt.Errorf("dht: %w: missing 'r' key in response", ErrInvalidMessage)
		}
		rdv, ok := rv.(*bencode.DictVal)
		if !ok {
			return nil, fmt.Errorf("dht: %w: 'r' must be dict, got %s", ErrInvalidMessage, rv.Kind())
		}
		m.R = rdv
		if err := validateDictNodeID("r", m.R); err != nil {
			return nil, err
		}

	case "e":
		ev, ok := d.Get("e")
		if !ok {
			return nil, fmt.Errorf("dht: %w: missing 'e' key in error", ErrInvalidMessage)
		}
		elv, ok := ev.(bencode.ListVal)
		if !ok {
			return nil, fmt.Errorf("dht: %w: 'e' must be list, got %s", ErrInvalidMessage, ev.Kind())
		}
		if len(elv.L) != 2 {
			return nil, fmt.Errorf("dht: %w: error list must have 2 elements, got %d", ErrInvalidMessage, len(elv.L))
		}
		eiv, ok := elv.L[0].(bencode.IntVal)
		if !ok {
			return nil, fmt.Errorf("dht: %w: error code must be int, got %s", ErrInvalidMessage, elv.L[0].Kind())
		}
		esv, ok := elv.L[1].(bencode.StringVal)
		if !ok {
			return nil, fmt.Errorf("dht: %w: error message must be string, got %s", ErrInvalidMessage, elv.L[1].Kind())
		}
		m.E = [2]interface{}{eiv.I, esv.S}

	default:
		return nil, fmt.Errorf("dht: %w: unknown message type %q", ErrInvalidMessage, m.Y)
	}

	return &m, nil
}

func validateDictNodeID(dictName string, d *bencode.DictVal) error {
	idv, ok := d.Get("id")
	if !ok {
		return fmt.Errorf("dht: %w: missing 'id' key in %s", ErrInvalidMessage, dictName)
	}
	idsv, ok := idv.(bencode.StringVal)
	if !ok {
		return fmt.Errorf("dht: %w: %s.id must be string, got %s", ErrInvalidMessage, dictName, idv.Kind())
	}
	if len(idsv.S) != NodeIDLength {
		return fmt.Errorf("dht: %w: %s.id must be %d bytes, got %d", ErrInvalidMessage, dictName, NodeIDLength, len(idsv.S))
	}
	return nil
}

// --- minimal bencode decoder ---

type beScanner struct {
	data []byte
	pos  int
}

func (s *beScanner) peek() byte {
	if s.pos >= len(s.data) {
		return 0
	}
	return s.data[s.pos]
}

func (s *beScanner) advance() byte {
	b := s.peek()
	if s.pos < len(s.data) {
		s.pos++
	}
	return b
}

func beDecode(data []byte) (bencode.Value, error) {
	s := &beScanner{data: data}
	v, err := s.parseValue()
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *beScanner) parseValue() (bencode.Value, error) {
	b := s.peek()
	switch {
	case b >= '0' && b <= '9':
		return s.parseString()
	case b == 'i':
		return s.parseInt()
	case b == 'l':
		return s.parseList()
	case b == 'd':
		return s.parseDict()
	case b == 0:
		return nil, fmt.Errorf("unexpected end of data at %d", s.pos)
	default:
		return nil, fmt.Errorf("unexpected byte %q at %d", b, s.pos)
	}
}

func (s *beScanner) parseString() (bencode.StringVal, error) {
	// Parse length prefix
	start := s.pos
	for {
		b := s.peek()
		if b == 0 {
			return bencode.StringVal{}, fmt.Errorf("unexpected end of string length at %d", s.pos)
		}
		if b == ':' {
			break
		}
		if b < '0' || b > '9' {
			return bencode.StringVal{}, fmt.Errorf("invalid char %q in string length at %d", b, s.pos)
		}
		s.advance()
	}
	lengthStr := string(s.data[start:s.pos])

	if s.pos > start+1 && s.data[start] == '0' {
		return bencode.StringVal{}, fmt.Errorf("leading zero in string length at %d", start)
	}

	var length int
	for _, c := range lengthStr {
		length = length*10 + int(c-'0')
	}

	s.advance() // skip ':'

	if s.pos+length > len(s.data) {
		return bencode.StringVal{}, fmt.Errorf("string length %d exceeds data at %d", length, s.pos)
	}
	str := string(s.data[s.pos : s.pos+length])
	s.pos += length
	return bencode.StringVal{S: str}, nil
}

func (s *beScanner) parseInt() (bencode.IntVal, error) {
	s.advance() // skip 'i'

	start := s.pos
	negative := false
	if s.peek() == '-' {
		negative = true
		s.advance()
	}

	if s.peek() == 0 {
		return bencode.IntVal{}, fmt.Errorf("unterminated integer at %d", s.pos)
	}

	// Must have at least one digit
	if s.peek() == 'e' && s.pos == start && !negative {
		return bencode.IntVal{}, fmt.Errorf("empty integer at %d", start)
	}
	if s.peek() == 'e' && negative {
		return bencode.IntVal{}, fmt.Errorf("i-e is invalid at %d", start)
	}

	zeroLeading := false
	if s.peek() == '0' {
		zeroLeading = true
		s.advance()
		if s.peek() != 'e' {
			return bencode.IntVal{}, fmt.Errorf("leading zero in integer at %d", start)
		}
	}

	var val int64
	if !zeroLeading {
		for {
			b := s.peek()
			if b == 'e' {
				break
			}
			if b == 0 {
				return bencode.IntVal{}, fmt.Errorf("unterminated integer at %d", s.pos)
			}
			if b < '0' || b > '9' {
				return bencode.IntVal{}, fmt.Errorf("invalid char in integer at %d", s.pos)
			}
			val = val*10 + int64(b-'0')
			s.advance()
		}
	}

	s.advance() // skip 'e'

	if negative {
		val = -val
	}
	return bencode.IntVal{I: val}, nil
}

func (s *beScanner) parseList() (bencode.ListVal, error) {
	s.advance() // skip 'l'
	items := make([]bencode.Value, 0)
	for {
		if s.peek() == 0 {
			return bencode.ListVal{}, fmt.Errorf("unterminated list at %d", s.pos)
		}
		if s.peek() == 'e' {
			s.advance()
			break
		}
		v, err := s.parseValue()
		if err != nil {
			return bencode.ListVal{}, err
		}
		items = append(items, v)
	}
	return bencode.ListVal{L: items}, nil
}

func (s *beScanner) parseDict() (*bencode.DictVal, error) {
	s.advance() // skip 'd'
	d := bencode.NewDict()
	for {
		if s.peek() == 0 {
			return nil, fmt.Errorf("unterminated dictionary at %d", s.pos)
		}
		if s.peek() == 'e' {
			s.advance()
			break
		}
		// Keys must be strings
		if s.peek() < '0' || s.peek() > '9' {
			return nil, fmt.Errorf("dictionary key must be string at %d", s.pos)
		}
		key, err := s.parseString()
		if err != nil {
			return nil, err
		}
		val, err := s.parseValue()
		if err != nil {
			return nil, err
		}
		d.Set(key.S, val)
	}
	return d, nil
}
