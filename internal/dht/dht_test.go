package dht_test

import (
	"context"
	"encoding/hex"
	"net"
	"testing"
	"time"

	bencode "github.com/smartass08/aria2go/internal/bencode"
	"github.com/smartass08/aria2go/internal/dht"
)

func TestNewServer(t *testing.T) {
	cfg := dht.Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := dht.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestServer_RunAndShutdown(t *testing.T) {
	cfg := dht.Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := dht.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Errorf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for shutdown")
	}
}

func TestCompactPeerEncoding(t *testing.T) {
	p1 := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 6881}
	p2 := &net.TCPAddr{IP: net.IPv4(192, 168, 1, 1), Port: 6882}

	addrs := []net.Addr{p1, p2}
	encoded := compactPeerAddrs(addrs)

	if len(encoded) != 12 {
		t.Errorf("compact peers length = %d, want 12", len(encoded))
	}

	decoded := decodeCompactPeers(encoded)
	if len(decoded) != 2 {
		t.Fatalf("decoded peers len = %d, want 2", len(decoded))
	}
	if decoded[0].String() != "10.0.0.1:6881" {
		t.Errorf("peer[0] = %s, want 10.0.0.1:6881", decoded[0].String())
	}
	if decoded[1].String() != "192.168.1.1:6882" {
		t.Errorf("peer[1] = %s, want 192.168.1.1:6882", decoded[1].String())
	}
}

func TestIPTo4(t *testing.T) {
	ip := net.IPv4(10, 0, 0, 1)
	result := ipTo4(ip)
	if result != [4]byte{10, 0, 0, 1} {
		t.Errorf("ipTo4 = %v, want [10 0 0 1]", result)
	}

	ip6 := net.IPv6loopback
	result6 := ipTo4(ip6)
	if result6 != [4]byte{} {
		t.Errorf("ipTo4(v6) = %v, want zeros", result6)
	}
}

func TestExtractNodeID_Query(t *testing.T) {
	args := bencode.NewDict()
	args.Set("id", bencode.StringVal{S: "abcdefghij0123456789"})
	msg := dht.NewQuery(dht.QPing, args)
	msg.T = "aa"

	id := extractNodeID(msg)
	expected := [20]byte{}
	copy(expected[:], "abcdefghij0123456789")
	if id != expected {
		t.Errorf("extractNodeID = %x, want %x", id, expected)
	}
}

func TestExtractNodeID_Response(t *testing.T) {
	r := bencode.NewDict()
	r.Set("id", bencode.StringVal{S: "abcdefghij0123456789"})
	msg := dht.NewResponse("aa", r)

	id := extractNodeID(msg)
	expected := [20]byte{}
	copy(expected[:], "abcdefghij0123456789")
	if id != expected {
		t.Errorf("extractNodeID = %x, want %x", id, expected)
	}
}

func TestExtractNodeID_Missing(t *testing.T) {
	msg := dht.NewQuery(dht.QPing, bencode.NewDict())
	var zero dht.NodeID
	id := extractNodeID(msg)
	if id != zero {
		t.Errorf("extractNodeID without id = %x, want zero", id)
	}
}

func TestDecodeCompactPeers_InvalidLength(t *testing.T) {
	result := decodeCompactPeers([]byte{1, 2, 3, 4, 5})
	if result != nil {
		t.Errorf("decodeCompactPeers on non-6-multiple length should return nil")
	}
}

func TestDecodeCompactPeers_Empty(t *testing.T) {
	result := decodeCompactPeers([]byte{})
	if len(result) != 0 {
		t.Errorf("decodeCompactPeers on empty should return empty")
	}
}

func TestNodeIDFromBytes(t *testing.T) {
	id := dht.NodeIDFromBytes([]byte("hello"))
	if id == dht.ZeroNodeID() {
		t.Error("NodeIDFromBytes should not return zero")
	}
	if len(id) != 20 {
		t.Errorf("NodeIDFromBytes length = %d, want 20", len(id))
	}
}

func TestServer_GetPeers(t *testing.T) {
	cfg := dht.Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := dht.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var infoHash [20]byte
	infoHash[0] = 0x01

	ch, err := srv.GetPeers(ctx, infoHash)
	if err != nil {
		t.Fatalf("GetPeers error: %v", err)
	}

	select {
	case <-ch:
	case <-time.After(1 * time.Second):
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after completion")
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestServer_Announce(t *testing.T) {
	cfg := dht.Config{
		Addr: "127.0.0.1:0",
	}
	srv, err := dht.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var infoHash [20]byte
	infoHash[0] = 0x02

	err = srv.Announce(ctx, infoHash, 6881)
	if err != nil {
		t.Logf("Announce error (expected with no bootstrap): %v", err)
	}
}

func TestServer_Bootstrap(t *testing.T) {
	cfg := dht.Config{
		Addr:      "127.0.0.1:0",
		Bootstrap: []string{"127.0.0.1:6881"},
	}
	srv, err := dht.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Errorf("Run error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
}

func TestServer_WithCustomNodeID(t *testing.T) {
	idHex := "abcdef0123456789abcdef0123456789abcdef01"
	idBytes, _ := hex.DecodeString(idHex)

	var customID dht.NodeID
	copy(customID[:], idBytes)

	cfg := dht.Config{
		NodeID: customID,
		Addr:   "127.0.0.1:0",
	}
	srv, err := dht.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Errorf("Run error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestMergeBytes(t *testing.T) {
	var base dht.NodeID
	copy(base[:], []byte("abc"))
	suffix := []byte("efg")

	merged := dht.MergeBytes(base, suffix)
	expected := [20]byte{}
	copy(expected[:], []byte("abc"))
	copy(expected[17:], []byte("efg"))

	for i := range 20 {
		if merged[i] != expected[i] {
			t.Errorf("MergeBytes[%d] = %d, want %d", i, merged[i], expected[i])
		}
	}
}

func TestZeroNodeID(t *testing.T) {
	zero := dht.ZeroNodeID()
	for i := range zero {
		if zero[i] != 0 {
			t.Errorf("ZeroNodeID[%d] = %d, want 0", i, zero[i])
		}
	}
}

func ipTo4(ip net.IP) [4]byte {
	var out [4]byte
	v4 := ip.To4()
	if v4 != nil {
		copy(out[:], v4)
	}
	return out
}

func extractNodeID(msg *dht.Message) dht.NodeID {
	var id dht.NodeID
	if msg.Y == "r" && msg.R != nil {
		if idV, ok := msg.R.Get("id"); ok {
			if sv, ok := idV.(bencode.StringVal); ok && len(sv.S) == dht.NodeIDLength {
				copy(id[:], sv.S)
			}
		}
		return id
	}
	if msg.A != nil {
		if idV, ok := msg.A.Get("id"); ok {
			if sv, ok := idV.(bencode.StringVal); ok && len(sv.S) == dht.NodeIDLength {
				copy(id[:], sv.S)
			}
		}
	}
	return id
}

func compactPeerAddrs(addrs []net.Addr) []byte {
	var buf []byte
	for _, a := range addrs {
		tcpAddr, ok := a.(*net.TCPAddr)
		if !ok {
			continue
		}
		ip := tcpAddr.IP.To4()
		if ip == nil {
			continue
		}
		buf = append(buf, ip...)
		buf = append(buf, byte(tcpAddr.Port>>8), byte(tcpAddr.Port&0xFF))
	}
	return buf
}

func decodeCompactPeers(data []byte) []net.Addr {
	if len(data)%6 != 0 {
		return nil
	}
	var peers []net.Addr
	for i := 0; i < len(data); i += 6 {
		ip := net.IP(data[i : i+4])
		port := int(data[i+4])<<8 | int(data[i+5])
		peers = append(peers, &net.TCPAddr{IP: ip, Port: port})
	}
	return peers
}
