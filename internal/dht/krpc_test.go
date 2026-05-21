package dht_test

import (
	"errors"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
	"github.com/smartass08/aria2go/internal/dht"
)

func TestNewQuery(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	args.Set("target", bencode.NewString("mnopqrstuvwxyz123456"))

	m := dht.NewQuery(dht.QPing, args)

	if m.Y != "q" {
		t.Errorf("Y = %q, want q", m.Y)
	}
	if m.Q != dht.QPing {
		t.Errorf("Q = %q, want ping", m.Q)
	}
	if m.T != "" {
		t.Errorf("T = %q, want empty (caller sets T)", m.T)
	}
	if m.A != args {
		t.Errorf("A = %v, want %v", m.A, args)
	}

	// nil args should be initialised to empty dict
	m2 := dht.NewQuery(dht.QFindNode, nil)
	if m2.A == nil || len(m2.A.Keys) != 0 {
		t.Errorf("NewQuery with nil args should produce empty dict")
	}
}

func TestNewResponse(t *testing.T) {
	r := bencode.NewDict()
	r.Set("id", bencode.NewString("abcdefghij0123456789"))

	m := dht.NewResponse("aa", r)

	if m.Y != "r" {
		t.Errorf("Y = %q, want r", m.Y)
	}
	if m.T != "aa" {
		t.Errorf("T = %q, want aa", m.T)
	}
	if m.R != r {
		t.Errorf("R = %v, want %v", m.R, r)
	}

	m2 := dht.NewResponse("bb", nil)
	if m2.R == nil || len(m2.R.Keys) != 0 {
		t.Errorf("NewResponse with nil r should produce empty dict")
	}
}

func TestNewError(t *testing.T) {
	m := dht.NewError("tx1", 201, "Generic Error")

	if m.Y != "e" {
		t.Errorf("Y = %q, want e", m.Y)
	}
	if m.T != "tx1" {
		t.Errorf("T = %q, want tx1", m.T)
	}
	if m.E[0] != int64(201) {
		t.Errorf("E[0] = %v, want 201", m.E[0])
	}
	if m.E[1] != "Generic Error" {
		t.Errorf("E[1] = %q, want Generic Error", m.E[1])
	}
}

func TestMarshal_Query(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))

	m := &dht.Message{
		T: "aa",
		Y: "q",
		Q: dht.QPing,
		A: args,
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	// Expected: d1:ad2:id20:abcdefghij0123456789e1:q4:ping1:t2:aa1:v0:1:y1:qe
	// Keys in alphabetical order: a, q, t, v, y
	want := "d1:ad2:id20:abcdefghij0123456789e1:q4:ping1:t2:aa1:v0:1:y1:qe"
	if string(data) != want {
		t.Errorf("Marshal() = %q, want %q", string(data), want)
	}
}

func TestMarshal_Response(t *testing.T) {
	r := bencode.NewDict()
	r.Set("id", bencode.NewString("abcdefghij0123456789"))
	r.Set("token", bencode.NewString("aoeusnth"))

	m := &dht.Message{
		T: "aa",
		Y: "r",
		R: r,
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	// Keys: r, t, v, y (alphabetical)
	want := "d1:rd2:id20:abcdefghij01234567895:token8:aoeusnthe1:t2:aa1:v0:1:y1:re"
	if string(data) != want {
		t.Errorf("Marshal() = %q, want %q", string(data), want)
	}
}

func TestMarshal_Error(t *testing.T) {
	m := &dht.Message{
		T: "aa",
		Y: "e",
		E: [2]interface{}{int64(201), "A Generic Error"},
	}

	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	want := "d1:eli201e15:A Generic Errore1:t2:aa1:v0:1:y1:ee"
	if string(data) != want {
		t.Errorf("Marshal() = %q, want %q", string(data), want)
	}
}

func TestMarshal_InvalidType(t *testing.T) {
	m := &dht.Message{
		T: "aa",
		Y: "z",
	}
	_, err := m.Marshal()
	if err == nil {
		t.Error("Marshal() with Y=z should error")
	}
}

func TestUnmarshal_Query(t *testing.T) {
	data := []byte("d1:ad2:id20:abcdefghij0123456789e1:q4:ping1:t2:aa1:y1:qe")

	m, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m.Y != "q" {
		t.Errorf("Y = %q, want q", m.Y)
	}
	if m.T != "aa" {
		t.Errorf("T = %q, want aa", m.T)
	}
	if m.Q != dht.QPing {
		t.Errorf("Q = %q, want ping", m.Q)
	}
	if m.A == nil {
		t.Fatal("A is nil")
	}
	idV, ok := m.A.Get("id")
	if !ok {
		t.Fatal("missing id in A")
	}
	if idV.(bencode.StringVal).S != "abcdefghij0123456789" {
		t.Errorf("id = %q", idV.(bencode.StringVal).S)
	}
}

func TestUnmarshal_Response(t *testing.T) {
	data := []byte("d1:rd2:id20:abcdefghij01234567895:token8:aoeusnthe1:t2:aa1:y1:re")

	m, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m.Y != "r" {
		t.Errorf("Y = %q, want r", m.Y)
	}
	if m.T != "aa" {
		t.Errorf("T = %q, want aa", m.T)
	}
	if m.R == nil {
		t.Fatal("R is nil")
	}
	tokenV, ok := m.R.Get("token")
	if !ok {
		t.Fatal("missing token in R")
	}
	if tokenV.(bencode.StringVal).S != "aoeusnth" {
		t.Errorf("token = %q", tokenV.(bencode.StringVal).S)
	}
}

func TestUnmarshal_Error(t *testing.T) {
	data := []byte("d1:eli201e15:A Generic Errore1:t2:aa1:y1:ee")

	m, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m.Y != "e" {
		t.Errorf("Y = %q, want e", m.Y)
	}
	if m.T != "aa" {
		t.Errorf("T = %q, want aa", m.T)
	}
	if m.E[0] != int64(201) {
		t.Errorf("E[0] = %v, want 201", m.E[0])
	}
	if m.E[1] != "A Generic Error" {
		t.Errorf("E[1] = %q, want A Generic Error", m.E[1])
	}
}

func TestRoundTrip_Query(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	args.Set("target", bencode.NewString("mnopqrstuvwxyz123456"))

	m1 := dht.NewQuery(dht.QFindNode, args)
	m1.T = "ab"

	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m2, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m2.Y != m1.Y || m2.T != m1.T || m2.Q != m1.Q {
		t.Error("round-trip fields mismatch")
	}
	idV, ok := m2.A.Get("id")
	if !ok || idV.(bencode.StringVal).S != "abcdefghij0123456789" {
		t.Error("round-trip args mismatch")
	}
}

func TestRoundTrip_Response(t *testing.T) {
	r := bencode.NewDict()
	r.Set("id", bencode.NewString("abcdefghij0123456789"))
	r.Set("nodes", bencode.NewString("compactnodesdata26"))

	m1 := dht.NewResponse("ab", r)

	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m2, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m2.Y != m1.Y || m2.T != m1.T {
		t.Error("round-trip fields mismatch")
	}
	nv, ok := m2.R.Get("nodes")
	if !ok || nv.(bencode.StringVal).S != "compactnodesdata26" {
		t.Error("round-trip response values mismatch")
	}
}

func TestRoundTrip_Error(t *testing.T) {
	m1 := dht.NewError("ab", 202, "Server Error")

	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m2, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m2.Y != m1.Y || m2.T != m1.T {
		t.Error("round-trip fields mismatch")
	}
	if m2.E[0] != int64(202) || m2.E[1] != "Server Error" {
		t.Error("round-trip error fields mismatch")
	}
}

func TestCompactNodeInfo(t *testing.T) {
	var n dht.NodeInfo
	n.ID = [20]byte{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
		10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
	}
	n.IP = [4]byte{10, 0, 0, 1}
	n.Port = 6881

	compact := n.Compact()
	if len(compact) != 26 {
		t.Errorf("Compact() length = %d, want 26", len(compact))
	}

	if compact[0] != 0 || compact[19] != 19 {
		t.Error("ID bytes mismatch")
	}
	if compact[20] != 10 || compact[23] != 1 {
		t.Error("IP bytes mismatch")
	}
	// Port 6881 = 0x1AE1 in network byte order
	if compact[24] != 0x1A || compact[25] != 0xE1 {
		t.Errorf("port bytes = [%02x %02x], want [1a e1]", compact[24], compact[25])
	}

	decoded := dht.DecodeCompactNodeInfo(compact)
	if decoded.ID != n.ID || decoded.IP != n.IP || decoded.Port != n.Port {
		t.Error("DecodeCompactNodeInfo round-trip mismatch")
	}
}

func TestCompactNodes(t *testing.T) {
	n1 := dht.NodeInfo{
		ID:   [20]byte{1},
		IP:   [4]byte{127, 0, 0, 1},
		Port: 6881,
	}
	n2 := dht.NodeInfo{
		ID:   [20]byte{2},
		IP:   [4]byte{192, 168, 1, 1},
		Port: 6882,
	}

	data := dht.CompactNodes([]dht.NodeInfo{n1, n2})
	if len(data) != 52 {
		t.Errorf("CompactNodes length = %d, want 52", len(data))
	}

	decoded, err := dht.DecodeCompactNodes(data)
	if err != nil {
		t.Fatalf("DecodeCompactNodes() error: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded length = %d, want 2", len(decoded))
	}
	if decoded[0].Port != 6881 || decoded[1].Port != 6882 {
		t.Error("DecodeCompactNodes port mismatch")
	}
	if decoded[0].ID[0] != 1 || decoded[1].ID[0] != 2 {
		t.Error("DecodeCompactNodes ID mismatch")
	}
}

func TestDecodeCompactNodes_SkipZeroIP(t *testing.T) {
	// Create 52 bytes with 2 nodes: first has all-zero IP (should be skipped)
	n1 := dht.NodeInfo{
		IP:   [4]byte{0, 0, 0, 0},
		Port: 1234,
	}
	n1.ID[0] = 1
	n2 := dht.NodeInfo{
		IP:   [4]byte{1, 2, 3, 4},
		Port: 5678,
	}
	n2.ID[0] = 2

	data := dht.CompactNodes([]dht.NodeInfo{n1, n2})
	nodes, err := dht.DecodeCompactNodes(data)
	if err != nil {
		t.Fatalf("DecodeCompactNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node (zero IP skipped), got %d", len(nodes))
	}
	if nodes[0].Port != 5678 || nodes[0].ID[0] != 2 {
		t.Error("wrong node decoded after zero-IP skip")
	}
}

func TestDecodeCompactNodes_InvalidLength(t *testing.T) {
	data := make([]byte, 27) // not multiple of 26
	_, err := dht.DecodeCompactNodes(data)
	if err == nil {
		t.Error("DecodeCompactNodes with non-26-multiple length should error")
	}
}

func TestDecodeCompactNodes_Empty(t *testing.T) {
	nodes, err := dht.DecodeCompactNodes([]byte{})
	if err != nil {
		t.Fatalf("DecodeCompactNodes([]) error: %v", err)
	}
	if nodes != nil {
		t.Error("DecodeCompactNodes([]) should return nil")
	}
}

func TestUnmarshal_InvalidBencode(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"truncated dict", []byte("d1:t2:aa")},
		{"not a dict", []byte("l1:a1:be")},
		{"missing t", []byte("d1:y1:qe")},
		{"missing y", []byte("d1:t2:aae")},
		{"t not string", []byte("d1:ti1e1:y1:qe")},
		{"y not string", []byte("d1:t2:aai1y1:qe")},
		{"unknown type", []byte("d1:t2:aa1:y1:ze")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dht.Unmarshal(tt.data)
			if err == nil {
				t.Errorf("Unmarshal(%q) should error", tt.data)
			}
		})
	}
}

func TestUnmarshal_QueryMissingFields(t *testing.T) {
	// missing q
	_, err := dht.Unmarshal([]byte("d1:ad2:id20:abcdefghij0123456789e1:t2:aa1:y1:qe"))
	if err == nil {
		t.Error("Unmarshal without q should error")
	}

	// missing a
	_, err = dht.Unmarshal([]byte("d1:q4:ping1:t2:aa1:y1:qe"))
	if err == nil {
		t.Error("Unmarshal without a should error")
	}
}

func TestUnmarshal_ResponseMissingFields(t *testing.T) {
	_, err := dht.Unmarshal([]byte("d1:t2:aa1:y1:re"))
	if err == nil {
		t.Error("Unmarshal response without r should error")
	}
}

func TestUnmarshal_ErrorBadFormat(t *testing.T) {
	// error list with 1 element
	_, err := dht.Unmarshal([]byte("d1:eli201ee1:t2:aa1:y1:ee"))
	if err == nil {
		t.Error("Unmarshal error with 1-element list should error")
	}

	// error list with 3 elements
	_, err = dht.Unmarshal([]byte("d1:eli201e1:xi3ee1:t2:aa1:y1:ee"))
	if err == nil {
		t.Error("Unmarshal error with 3-element list should error")
	}

	// error with non-int code
	_, err = dht.Unmarshal([]byte("d1:el10:not-an-int5:error1:t2:aa1:y1:ee"))
	if err == nil {
		t.Error("Unmarshal error with string code should error")
	}
}

func TestRoundTrip_EmptyDictArgs(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	m1 := dht.NewQuery(dht.QPing, args)
	m1.T = "aa"

	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m2, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m2.Q != dht.QPing || m2.Y != "q" || m2.T != "aa" {
		t.Error("round-trip empty-args mismatch")
	}
}

func TestRoundTrip_NestedDict(t *testing.T) {
	inner := bencode.NewDict()
	inner.Set("x", bencode.NewInt(1))
	inner.Set("y", bencode.NewInt(2))
	inner.Set("z", bencode.NewString("test"))

	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	args.Set("nested", inner)

	m1 := dht.NewQuery(dht.QGetPeers, args)
	m1.T = "cc"

	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m2, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	nestedV, ok := m2.A.Get("nested")
	if !ok {
		t.Fatal("nested key not found")
	}
	nestedDict, ok := nestedV.(*bencode.DictVal)
	if !ok {
		t.Fatalf("nested value is %T, want *DictVal", nestedV)
	}
	xV, ok := nestedDict.Get("x")
	if !ok || xV.(bencode.IntVal).I != 1 {
		t.Error("nested.x mismatch")
	}
}

func TestNodeID(t *testing.T) {
	var id dht.NodeID
	if len(id) != 20 {
		t.Errorf("NodeID length = %d, want 20", len(id))
	}
	// test equality
	id1 := dht.NodeID{}
	id1[0] = 1
	id2 := dht.NodeID{}
	id2[0] = 1
	if id1 != id2 {
		t.Error("NodeID equality failed")
	}
}

func TestQueryNames(t *testing.T) {
	if dht.QPing != "ping" {
		t.Errorf("QPing = %q", dht.QPing)
	}
	if dht.QFindNode != "find_node" {
		t.Errorf("QFindNode = %q", dht.QFindNode)
	}
	if dht.QGetPeers != "get_peers" {
		t.Errorf("QGetPeers = %q", dht.QGetPeers)
	}
	if dht.QAnnouncePeer != "announce_peer" {
		t.Errorf("QAnnouncePeer = %q", dht.QAnnouncePeer)
	}
}

func TestMarshal_NilADict(t *testing.T) {
	// setting A=nil should marshal as empty dict
	m := &dht.Message{
		T: "aa",
		Y: "q",
		Q: dht.QPing,
		A: nil,
	}
	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	want := "d1:ade1:q4:ping1:t2:aa1:v0:1:y1:qe"
	if string(data) != want {
		t.Errorf("Marshal() = %q, want %q", string(data), want)
	}
}

func TestMarshal_NilRDict(t *testing.T) {
	m := &dht.Message{
		T: "aa",
		Y: "r",
		R: nil,
	}
	data, err := m.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	want := "d1:rde1:t2:aa1:v0:1:y1:re"
	if string(data) != want {
		t.Errorf("Marshal() = %q, want %q", string(data), want)
	}
}

func TestUnmarshal_ResponseWithValueList(t *testing.T) {
	r := bencode.NewDict()
	r.Set("id", bencode.NewString("abcdefghij0123456789"))
	r.Set("values", bencode.NewList(
		bencode.NewString("peer1"),
		bencode.NewString("peer2"),
	))

	m1 := dht.NewResponse("aa", r)
	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m.Y != "r" || m.T != "aa" {
		t.Error("response fields mismatch")
	}
	valsV, ok := m.R.Get("values")
	if !ok {
		t.Fatal("missing values in R")
	}
	valsList, ok := valsV.(bencode.ListVal)
	if !ok {
		t.Fatalf("values is %T, want ListVal", valsV)
	}
	if len(valsList.L) != 2 {
		t.Fatalf("values list len = %d, want 2", len(valsList.L))
	}
	if valsList.L[0].(bencode.StringVal).S != "peer1" {
		t.Errorf("values[0] = %q", valsList.L[0].(bencode.StringVal).S)
	}
	if valsList.L[1].(bencode.StringVal).S != "peer2" {
		t.Errorf("values[1] = %q", valsList.L[1].(bencode.StringVal).S)
	}
}

func TestBencodeDecode_ZeroLengthString(t *testing.T) {
	data := []byte("d1:ad2:id20:abcdefghij0123456789e1:q4:ping1:t0:1:y1:qe")
	m, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() with zero-length t error: %v", err)
	}
	if m.T != "" {
		t.Errorf("T = %q, want empty", m.T)
	}
}

func TestBencodeDecode_EmptyInt(t *testing.T) {
	_, err := dht.Unmarshal([]byte("d1:q4:ping1:ti-e1:y1:qe"))
	if err == nil {
		t.Error("Unmarshal with i-e should error")
	}
}

func TestBencodeDecode_LeadingZeroInt(t *testing.T) {
	// "i03e" is invalid
	_, err := dht.Unmarshal([]byte("d1:q4:ping1:ti03e1:y1:qe"))
	if err == nil {
		t.Error("Unmarshal with i03e should error")
	}
}

func TestRoundTrip_ZeroLengthTransaction(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.NewString("abcdefghij0123456789"))
	m1 := dht.NewQuery(dht.QPing, args)
	m1.T = "" // zero-length transaction ID is valid

	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m2, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m2.T != "" || m2.Q != dht.QPing {
		t.Error("round-trip zero-length transaction mismatch")
	}
}

func TestRoundTrip_ErrorCodeZero(t *testing.T) {
	m1 := dht.NewError("ab", 0, "")

	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m2, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m2.E[0] != int64(0) || m2.E[1] != "" {
		t.Error("round-trip zero-error-code mismatch")
	}
}

func TestVersionField(t *testing.T) {
	// Marshal with version
	r := bencode.NewDict()
	r.Set("id", bencode.NewString("abcdefghij0123456789"))
	m1 := dht.NewResponse("aa", r)
	m1.V = "A2\x00\x03"

	data, err := m1.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	m2, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if m2.V != m1.V {
		t.Errorf("V = %q, want %q", m2.V, m1.V)
	}

	// Marshal without version should include v0: per DHTAbstractMessage.cc
	m3 := dht.NewResponse("bb", nil)
	data2, err := m3.Marshal()
	if err != nil {
		t.Fatalf("Marshal() without V error: %v", err)
	}
	if string(data2) != "d1:rde1:t2:bb1:v0:1:y1:re" {
		t.Errorf("Marshal() without V = %q, want %q", string(data2), "d1:rde1:t2:bb1:v0:1:y1:re")
	}
}

func TestCompactV6NodeInfo(t *testing.T) {
	var n dht.V6NodeInfo
	n.ID = [20]byte{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
		10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
	}
	n.IP = [16]byte{
		0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 1,
	}
	n.Port = 6881

	compact := n.Compact()
	if len(compact) != 38 {
		t.Errorf("Compact() length = %d, want 38", len(compact))
	}

	// Check ID bytes in compact
	if compact[0] != 0 || compact[19] != 19 {
		t.Error("ID bytes mismatch")
	}
	// Check IPv6 bytes: bytes 20-35
	if compact[20] != 0x20 || compact[21] != 0x01 || compact[23] != 0xb8 {
		t.Error("IPv6 bytes mismatch")
	}
	if compact[35] != 0x01 {
		t.Error("IPv6 last byte mismatch")
	}
	// Port 6881 = 0x1AE1
	if compact[36] != 0x1A || compact[37] != 0xE1 {
		t.Errorf("port bytes = [%02x %02x], want [1a e1]", compact[36], compact[37])
	}

	decoded := dht.DecodeCompactV6NodeInfo(compact)
	if decoded.ID != n.ID || decoded.IP != n.IP || decoded.Port != n.Port {
		t.Error("DecodeCompactV6NodeInfo round-trip mismatch")
	}
}

func TestCompactV6Nodes(t *testing.T) {
	n1 := dht.V6NodeInfo{
		ID:   [20]byte{1},
		IP:   [16]byte{0: 0xfe, 1: 0x80, 15: 1},
		Port: 6881,
	}
	n2 := dht.V6NodeInfo{
		ID:   [20]byte{2},
		IP:   [16]byte{0: 0xfc, 1: 0x00, 15: 2},
		Port: 6882,
	}

	data := dht.CompactV6Nodes([]dht.V6NodeInfo{n1, n2})
	if len(data) != 76 {
		t.Errorf("CompactV6Nodes length = %d, want 76", len(data))
	}

	decoded, err := dht.DecodeCompactV6Nodes(data)
	if err != nil {
		t.Fatalf("DecodeCompactV6Nodes() error: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded length = %d, want 2", len(decoded))
	}
	if decoded[0].Port != 6881 || decoded[1].Port != 6882 {
		t.Error("DecodeCompactV6Nodes port mismatch")
	}
	if decoded[0].ID[0] != 1 || decoded[1].ID[0] != 2 {
		t.Error("DecodeCompactV6Nodes ID mismatch")
	}
}

func TestDecodeCompactV6Nodes_InvalidLength(t *testing.T) {
	data := make([]byte, 40) // not multiple of 38
	_, err := dht.DecodeCompactV6Nodes(data)
	if err == nil {
		t.Error("DecodeCompactV6Nodes with non-38-multiple length should error")
	}
}

func TestDecodeCompactV6Nodes_Empty(t *testing.T) {
	nodes, err := dht.DecodeCompactV6Nodes([]byte{})
	if err != nil {
		t.Fatalf("DecodeCompactV6Nodes([]) error: %v", err)
	}
	if nodes != nil {
		t.Error("DecodeCompactV6Nodes([]) should return nil")
	}
}

func TestDecodeCompactV6Nodes_SkipZeroIP(t *testing.T) {
	n1 := dht.V6NodeInfo{
		IP:   [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Port: 1234,
	}
	n1.ID[0] = 1
	n2 := dht.V6NodeInfo{
		IP:   [16]byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		Port: 5678,
	}
	n2.ID[0] = 2

	data := dht.CompactV6Nodes([]dht.V6NodeInfo{n1, n2})
	nodes, err := dht.DecodeCompactV6Nodes(data)
	if err != nil {
		t.Fatalf("DecodeCompactV6Nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node (zero IP skipped), got %d", len(nodes))
	}
	if nodes[0].Port != 5678 || nodes[0].ID[0] != 2 {
		t.Error("wrong node decoded after zero-IP skip")
	}
}

func TestValidateID(t *testing.T) {
	err := dht.ValidateID(nil)
	if err == nil {
		t.Error("ValidateID(nil) should error")
	}

	err = dht.ValidateID(make([]byte, 19))
	if err == nil {
		t.Error("ValidateID(19 bytes) should error")
	}

	err = dht.ValidateID(make([]byte, 20))
	if err != nil {
		t.Errorf("ValidateID(20 bytes) should succeed, got: %v", err)
	}

	err = dht.ValidateID(make([]byte, 21))
	if err == nil {
		t.Error("ValidateID(21 bytes) should error")
	}
}

func TestValidatePort(t *testing.T) {
	err := dht.ValidatePort(0)
	if err == nil {
		t.Error("ValidatePort(0) should error")
	}

	err = dht.ValidatePort(1)
	if err != nil {
		t.Errorf("ValidatePort(1) should succeed, got: %v", err)
	}

	err = dht.ValidatePort(6881)
	if err != nil {
		t.Errorf("ValidatePort(6881) should succeed, got: %v", err)
	}

	err = dht.ValidatePort(65535)
	if err == nil {
		t.Error("ValidatePort(65535) should error (port must be < UINT16_MAX)")
	}
}

func TestUnmarshal_VersionField(t *testing.T) {
	data := []byte("d1:rd2:id20:abcdefghij0123456789e1:t2:aa1:v4:A2\x00\x031:y1:re")
	m, err := dht.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() with v error: %v", err)
	}
	if m.V != "A2\x00\x03" {
		t.Errorf("V = %q, want %q", m.V, "A2\x00\x03")
	}
}

func TestUnmarshal_QueryRequiresNodeID(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "missing id",
			data: []byte("d1:ade1:q4:ping1:t2:aa1:y1:qe"),
		},
		{
			name: "short id",
			data: []byte("d1:ad2:id5:shorte1:q4:ping1:t2:aa1:y1:qe"),
		},
		{
			name: "non string id",
			data: []byte("d1:ad2:idi1ee1:q4:ping1:t2:aa1:y1:qe"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dht.Unmarshal(tt.data)
			if err == nil {
				t.Fatal("expected invalid message error")
			}
			if !errors.Is(err, dht.ErrInvalidMessage) {
				t.Fatalf("error %v should wrap ErrInvalidMessage", err)
			}
		})
	}
}

func TestUnmarshal_ResponseRequiresNodeID(t *testing.T) {
	_, err := dht.Unmarshal([]byte("d1:rde1:t2:aa1:y1:re"))
	if err == nil {
		t.Fatal("expected invalid message error")
	}
	if !errors.Is(err, dht.ErrInvalidMessage) {
		t.Fatalf("error %v should wrap ErrInvalidMessage", err)
	}
}

func TestErrInvalidMessage_Sentinel(t *testing.T) {
	// Verify ErrInvalidMessage wraps correctly
	_, err := dht.Unmarshal([]byte("d1:t2:aa1:y1:ze"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, dht.ErrInvalidMessage) {
		t.Errorf("error %v should wrap ErrInvalidMessage", err)
	}
}

func TestCompactNodes_DifferentSizes(t *testing.T) {
	// IPv4: 26 bytes is a valid multiple (use non-zero IP to pass zero-IP filter)
	n1 := dht.NodeInfo{IP: [4]byte{1, 1, 1, 1}}
	n2 := dht.NodeInfo{IP: [4]byte{2, 2, 2, 2}}
	data := dht.CompactNodes([]dht.NodeInfo{n1, n2})
	nodes, err := dht.DecodeCompactNodes(data)
	if err != nil {
		t.Fatalf("DecodeCompactNodes(52) error: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("len = %d, want 2", len(nodes))
	}

	// IPv4: 38 is not valid for IPv4 compact
	_, err = dht.DecodeCompactNodes(make([]byte, 38))
	if err == nil {
		t.Error("DecodeCompactNodes(38) should error (38 not multiple of 26)")
	}

	// IPv6: 38 is valid for IPv6 compact (use non-zero IP to pass zero-IP filter)
	v6n1 := dht.V6NodeInfo{IP: [16]byte{0: 1, 15: 1}}
	v6data := dht.CompactV6Nodes([]dht.V6NodeInfo{v6n1})
	v6nodes, err := dht.DecodeCompactV6Nodes(v6data)
	if err != nil {
		t.Fatalf("DecodeCompactV6Nodes(38) error: %v", err)
	}
	if len(v6nodes) != 1 {
		t.Errorf("len = %d, want 1", len(v6nodes))
	}

	// IPv6: 26 is not valid for IPv6 compact
	_, err = dht.DecodeCompactV6Nodes(make([]byte, 26))
	if err == nil {
		t.Error("DecodeCompactV6Nodes(26) should error (26 not multiple of 38)")
	}
}
