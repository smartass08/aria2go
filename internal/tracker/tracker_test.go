package tracker

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/bencode"
)

func TestValidateEvent(t *testing.T) {
	tests := []struct {
		event string
		ok    bool
	}{
		{"", true},
		{"started", true},
		{"stopped", true},
		{"completed", true},
		{"paused", false},
		{"STARTED", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		req := AnnounceRequest{Event: tt.event}
		err := req.ValidateEvent()
		if tt.ok && err != nil {
			t.Errorf("ValidateEvent() for %q: unexpected error %v", tt.event, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("ValidateEvent() for %q: expected error", tt.event)
		}
	}
}

func TestEventToInt(t *testing.T) {
	tests := []struct {
		event string
		want  int32
	}{
		{"", 0},
		{"completed", 1},
		{"started", 2},
		{"stopped", 3},
	}
	for _, tt := range tests {
		got := eventToInt(tt.event)
		if got != tt.want {
			t.Errorf("eventToInt(%q) = %d, want %d", tt.event, got, tt.want)
		}
	}
}

func TestUDPKey(t *testing.T) {
	if udpKey("") != 0 {
		t.Errorf("udpKey(\"\") = %d, want 0", udpKey(""))
	}
	h1 := udpKey("hello")
	if h1 == 0 {
		t.Error("udpKey(\"hello\") should not be 0")
	}
	h2 := udpKey("hello")
	if h1 != h2 {
		t.Errorf("udpKey is not deterministic: %d vs %d", h1, h2)
	}
}

func TestParseUDPTrackerURL(t *testing.T) {
	tests := []struct {
		url     string
		want    string
		wantErr bool
	}{
		{"udp://tracker.example.com:6969/announce", "tracker.example.com:6969", false},
		{"udp://10.0.0.1:1234", "10.0.0.1:1234", false},
		{"udp://tracker.example.com", "tracker.example.com:80", false},
		{"http://tracker.example.com:6969", "", true},
		{"not a url", "", true},
	}
	for _, tt := range tests {
		got, err := parseUDPTrackerURL(tt.url)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseUDPTrackerURL(%q): expected error", tt.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseUDPTrackerURL(%q): unexpected error: %v", tt.url, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseUDPTrackerURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestBuildAnnounceRequest(t *testing.T) {
	connID := uint64(0x1234567890ABCDEF)
	txnID := uint32(0xDEADBEEF)
	req := AnnounceRequest{
		Port:       6881,
		Uploaded:   100,
		Downloaded: 200,
		Left:       300,
		Event:      "started",
		NumWant:    50,
		Key:        "mykey",
	}
	req.InfoHash = [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	req.PeerID = [20]byte{0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe, 0xff, 0x00, 0x01, 0x02, 0x03, 0x04}

	buf := make([]byte, udpAnnounceReqLen)
	buildAnnounceRequest(buf, connID, txnID, req)

	if len(buf) != 100 {
		t.Fatalf("buildAnnounceRequest length = %d, want 100", len(buf))
	}

	if got := binary.BigEndian.Uint64(buf[0:8]); got != connID {
		t.Errorf("connection_id = %016x, want %016x", got, connID)
	}
	if got := binary.BigEndian.Uint32(buf[8:12]); got != 1 {
		t.Errorf("action = %d, want 1", got)
	}
	if got := binary.BigEndian.Uint32(buf[12:16]); got != txnID {
		t.Errorf("transaction_id = %08x, want %08x", got, txnID)
	}
	if got := [20]byte(buf[16:36]); got != req.InfoHash {
		t.Errorf("info_hash = %x, want %x", got, req.InfoHash)
	}
	if got := [20]byte(buf[36:56]); got != req.PeerID {
		t.Errorf("peer_id = %x, want %x", got, req.PeerID)
	}
	if got := binary.BigEndian.Uint64(buf[56:64]); got != 200 {
		t.Errorf("downloaded = %d, want 200", got)
	}
	if got := binary.BigEndian.Uint64(buf[64:72]); got != 300 {
		t.Errorf("left = %d, want 300", got)
	}
	if got := binary.BigEndian.Uint64(buf[72:80]); got != 100 {
		t.Errorf("uploaded = %d, want 100", got)
	}
	if got := binary.BigEndian.Uint32(buf[80:84]); got != 2 {
		t.Errorf("event = %d, want 2 (started)", got)
	}
	if got := binary.BigEndian.Uint32(buf[84:88]); got != 0 {
		t.Errorf("ip = %d, want 0", got)
	}
	if got := binary.BigEndian.Uint32(buf[92:96]); got != 50 {
		t.Errorf("num_want = %d, want 50", got)
	}
	if got := binary.BigEndian.Uint16(buf[96:98]); got != 6881 {
		t.Errorf("port = %d, want 6881", got)
	}
	if got := binary.BigEndian.Uint16(buf[98:100]); got != 0 {
		t.Errorf("extensions = %d, want 0", got)
	}

	req.NumWant = 0
	req.ExternalIP = "203.0.113.7"
	buildAnnounceRequest(buf, connID, txnID, req)
	if got := int32(binary.BigEndian.Uint32(buf[92:96])); got != -1 {
		t.Errorf("default num_want = %d, want -1", got)
	}
	if got := net.IP(buf[84:88]).String(); got != "203.0.113.7" {
		t.Errorf("external ip = %s, want 203.0.113.7", got)
	}
}

func TestParseConnectResponse(t *testing.T) {
	expectedConn := uint64(0xFEDCBA0987654321)
	txn := uint32(0xABCD1234)

	buf := make([]byte, 16)
	binary.BigEndian.PutUint32(buf[0:4], 0) // connect action
	binary.BigEndian.PutUint32(buf[4:8], txn)
	binary.BigEndian.PutUint64(buf[8:16], expectedConn)

	cid, err := parseConnectResponse(buf, txn, "1.2.3.4:6969")
	if err != nil {
		t.Fatalf("parseConnectResponse error: %v", err)
	}
	if cid != expectedConn {
		t.Errorf("connection_id = %016x, want %016x", cid, expectedConn)
	}

	// Wrong action
	buf[0] = 1
	_, err = parseConnectResponse(buf, txn, "1.2.3.4:6969")
	if err == nil {
		t.Error("expected error for wrong action")
	}

	// Reset and test wrong txn
	buf[0] = 0
	buf[1] = 0
	buf[2] = 0
	buf[3] = 0
	_, err = parseConnectResponse(buf, txn+1, "1.2.3.4:6969")
	if err == nil {
		t.Error("expected error for mismatched transaction ID")
	}

	// Too short
	_, err = parseConnectResponse(buf[:8], txn, "1.2.3.4:6969")
	if err == nil {
		t.Error("expected error for short response")
	}

	// Error response
	errBuf := make([]byte, 13)
	binary.BigEndian.PutUint32(errBuf[0:4], 3)
	binary.BigEndian.PutUint32(errBuf[4:8], txn)
	copy(errBuf[8:], "error")
	_, err = parseConnectResponse(errBuf, txn, "1.2.3.4:6969")
	if err == nil {
		t.Error("expected error for tracker error response")
	}
	_, err = parseConnectResponse(errBuf, txn+1, "1.2.3.4:6969")
	if !errors.Is(err, ErrInvalidResp) || !strings.Contains(err.Error(), "transaction ID mismatch") {
		t.Fatalf("expected transaction mismatch for wrong error txn, got %v", err)
	}
}

func TestParseAnnounceResponse(t *testing.T) {
	txn := uint32(0xDEADBEEF)

	buf := make([]byte, 20)
	binary.BigEndian.PutUint32(buf[0:4], 1)
	binary.BigEndian.PutUint32(buf[4:8], txn)
	binary.BigEndian.PutUint32(buf[8:12], 1800)
	binary.BigEndian.PutUint32(buf[12:16], 50)
	binary.BigEndian.PutUint32(buf[16:20], 100)

	resp, err := parseAnnounceResponse(buf, txn, "1.2.3.4:6969")
	if err != nil {
		t.Fatalf("parseAnnounceResponse error: %v", err)
	}
	if resp.Interval != 1800 {
		t.Errorf("interval = %d, want 1800", resp.Interval)
	}
	if resp.Incomplete != 50 {
		t.Errorf("incomplete = %d, want 50", resp.Incomplete)
	}
	if resp.Complete != 100 {
		t.Errorf("complete = %d, want 100", resp.Complete)
	}
	if len(resp.Peers) != 0 {
		t.Errorf("peers = %d, want 0", len(resp.Peers))
	}

	// With compact peers
	peerBuf := make([]byte, 6)
	peerBuf[0] = 192
	peerBuf[1] = 168
	peerBuf[2] = 1
	peerBuf[3] = 100
	peerBuf[4] = 0x1A
	peerBuf[5] = 0xE1
	buf = append(buf, peerBuf...)

	resp2, err := parseAnnounceResponse(buf, txn, "1.2.3.4:6969")
	if err != nil {
		t.Fatalf("parseAnnounceResponse with peers error: %v", err)
	}
	if len(resp2.Peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(resp2.Peers))
	}
	if !resp2.Peers[0].IP.Equal(net.IP{192, 168, 1, 100}) {
		t.Errorf("peer IP = %v, want 192.168.1.100", resp2.Peers[0].IP)
	}
	if resp2.Peers[0].Port != 6881 {
		t.Errorf("peer port = %d, want 6881", resp2.Peers[0].Port)
	}

	// Error response
	errBuf := make([]byte, 13)
	binary.BigEndian.PutUint32(errBuf[0:4], 3)
	binary.BigEndian.PutUint32(errBuf[4:8], txn)
	copy(errBuf[8:], "error")
	_, err = parseAnnounceResponse(errBuf, txn, "1.2.3.4:6969")
	if err == nil {
		t.Error("expected error for tracker error")
	}
	_, err = parseAnnounceResponse(errBuf, txn+1, "1.2.3.4:6969")
	if !errors.Is(err, ErrInvalidResp) || !strings.Contains(err.Error(), "transaction ID mismatch") {
		t.Fatalf("expected transaction mismatch for wrong error txn, got %v", err)
	}

	// Wrong action - build fresh buffer
	wrongBuf := make([]byte, 20)
	binary.BigEndian.PutUint32(wrongBuf[0:4], 0) // connect action
	binary.BigEndian.PutUint32(wrongBuf[4:8], txn)
	_, err = parseAnnounceResponse(wrongBuf, txn, "1.2.3.4:6969")
	if err == nil {
		t.Error("expected error for wrong action")
	}

	// Too short
	_, err = parseAnnounceResponse(buf[:10], txn, "1.2.3.4:6969")
	if err == nil {
		t.Error("expected error for short response")
	}

	// Mismatched transaction
	binary.BigEndian.PutUint32(buf[0:4], 1)
	_, err = parseAnnounceResponse(buf, txn+1, "1.2.3.4:6969")
	if err == nil {
		t.Error("expected error for mismatched transaction")
	}
}

func TestUnpackCompactPeers(t *testing.T) {
	// IPv4: 192.168.1.100:6881
	data := []byte{192, 168, 1, 100, 0x1A, 0xE1}
	peers, err := unpackCompactPeers(data, false)
	if err != nil {
		t.Fatalf("unpackCompactPeers error: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("len = %d, want 1", len(peers))
	}
	if !peers[0].IP.Equal(net.IP{192, 168, 1, 100}) {
		t.Errorf("IP = %v, want 192.168.1.100", peers[0].IP)
	}
	if peers[0].Port != 6881 {
		t.Errorf("port = %d, want 6881", peers[0].Port)
	}

	// Multiple peers
	data2 := []byte{
		10, 0, 0, 1, 0x0B, 0xB8,
		172, 16, 0, 2, 0x1F, 0x90,
	}
	peers2, err := unpackCompactPeers(data2, false)
	if err != nil {
		t.Fatalf("unpackCompactPeers error: %v", err)
	}
	if len(peers2) != 2 {
		t.Fatalf("len = %d, want 2", len(peers2))
	}
	if !peers2[0].IP.Equal(net.IP{10, 0, 0, 1}) {
		t.Errorf("peer0 IP = %v", peers2[0].IP)
	}
	if peers2[0].Port != 3000 {
		t.Errorf("peer0 port = %d", peers2[0].Port)
	}

	// Empty
	peers3, err := unpackCompactPeers(nil, false)
	if err != nil {
		t.Errorf("unpackCompactPeers(nil) error: %v", err)
	}
	if len(peers3) != 0 {
		t.Errorf("len(nil) = %d, want 0", len(peers3))
	}

	// Invalid length
	peers5, err := unpackCompactPeers([]byte{1, 2, 3, 4, 5}, false)
	if !errors.Is(err, ErrInvalidResp) {
		t.Fatalf("expected invalid response for trailing bytes, got %v", err)
	}
	if len(peers5) != 0 {
		t.Errorf("expected 0 peers for truncated data, got %d", len(peers5))
	}

	// Zero IP is included (matches C++ behaviour)
	zeroData := []byte{0, 0, 0, 0, 0, 0}
	peers4, err := unpackCompactPeers(zeroData, false)
	if err != nil {
		t.Errorf("unpackCompactPeers with zero IP error: %v", err)
	}
	if len(peers4) != 1 {
		t.Errorf("expected 1 peer for zero IP, got %d", len(peers4))
	}

	// IPv6
	ip6Data := []byte{
		0x20, 0x01, 0x0d, 0xb8, 0x85, 0xa3, 0x00, 0x00,
		0x00, 0x00, 0x8a, 0x2e, 0x03, 0x70, 0x73, 0x34,
		0x1A, 0xE1,
	}
	peers6, err := unpackCompactPeers(ip6Data, true)
	if err != nil {
		t.Fatalf("unpackCompactPeers IPv6 error: %v", err)
	}
	if len(peers6) != 1 {
		t.Fatalf("IPv6 len = %d, want 1", len(peers6))
	}
	if !peers6[0].IP.Equal(net.ParseIP("2001:db8:85a3::8a2e:370:7334")) {
		t.Errorf("IPv6 IP = %v", peers6[0].IP)
	}
	if peers6[0].Port != 6881 {
		t.Errorf("IPv6 port = %d, want 6881", peers6[0].Port)
	}
}

func TestBuildHTTPAnnounceURL(t *testing.T) {
	req := AnnounceRequest{
		Port:       6881,
		Uploaded:   100,
		Downloaded: 200,
		Left:       300,
		Event:      "started",
		NumWant:    50,
		Key:        "abc123",
	}
	req.InfoHash = [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	req.PeerID = [20]byte{0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe, 0xff, 0x00, 0x01, 0x02, 0x03, 0x04}

	urlStr, err := buildHTTPAnnounceURL("http://tracker.example.com:6969/announce", req)
	if err != nil {
		t.Fatalf("buildHTTPAnnounceURL error: %v", err)
	}

	if !strings.Contains(urlStr, "info_hash=") {
		t.Error("missing info_hash param")
	}
	if !strings.Contains(urlStr, "peer_id=") {
		t.Error("missing peer_id param")
	}
	if !strings.Contains(urlStr, "port=6881") {
		t.Error("missing port param")
	}
	if !strings.Contains(urlStr, "event=started") {
		t.Error("missing event param")
	}
	if !strings.Contains(urlStr, "compact=1") {
		t.Error("missing compact=1 param")
	}
	if !strings.Contains(urlStr, "numwant=50") {
		t.Error("missing numwant param")
	}
	if !strings.Contains(urlStr, "key=") {
		t.Error("missing key param")
	}
	if strings.Contains(urlStr, "supportcrypto=1") {
		t.Error("supportcrypto=1 param present for default CryptoSupport")
	}

	reqSupport := req
	reqSupport.CryptoSupport = "supportcrypto"
	urlSupport, _ := buildHTTPAnnounceURL("http://tracker.example.com:6969/announce", reqSupport)
	if !strings.Contains(urlSupport, "supportcrypto=1") {
		t.Error("missing supportcrypto=1 param")
	}

	// Port 0 should be omitted
	req2 := req
	req2.Port = 0
	urlStr2, err := buildHTTPAnnounceURL("http://tracker.example.com:6969/announce", req2)
	if err != nil {
		t.Fatalf("buildHTTPAnnounceURL error: %v", err)
	}
	if strings.Contains(urlStr2, "port=") {
		t.Error("port param present when port is 0")
	}

	// requirecrypto
	req3 := req
	req3.CryptoSupport = "requirecrypto"
	urlStr3, _ := buildHTTPAnnounceURL("http://tracker.example.com:6969/announce", req3)
	if !strings.Contains(urlStr3, "requirecrypto=1") {
		t.Error("missing requirecrypto=1 param")
	}

	// external IP
	req4 := req
	req4.ExternalIP = "1.2.3.4"
	urlStr4, _ := buildHTTPAnnounceURL("http://tracker.example.com:6969/announce", req4)
	if !strings.Contains(urlStr4, "ip=1.2.3.4") {
		t.Error("missing ip param")
	}

	// No ipv6 param
	if strings.Contains(urlStr, "ipv6") {
		t.Error("ipv6 param present when it should not be")
	}

	// Regular announce (no event)
	req5 := req
	req5.Event = ""
	urlStr5, err := buildHTTPAnnounceURL("http://tracker.example.com:6969/announce", req5)
	if err != nil {
		t.Fatalf("buildHTTPAnnounceURL error: %v", err)
	}
	if strings.Contains(urlStr5, "event=") {
		t.Error("event param present when it should not be")
	}

	// Key is derived from last 8 bytes of peer_id
	expectedKey := string(req.PeerID[12:])
	if !strings.Contains(urlStr, "key="+trackerQueryEscape([]byte(expectedKey))) {
		t.Errorf("key param should be peer_id bytes 12-20, got %q in %q", trackerQueryEscape([]byte(expectedKey)), urlStr)
	}

	reqSpace := req
	reqSpace.InfoHash = [20]byte{' ', 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18}
	urlSpace, err := buildHTTPAnnounceURL("http://tracker.example.com:6969/announce?existing=1", reqSpace)
	if err != nil {
		t.Fatalf("buildHTTPAnnounceURL binary escaping error: %v", err)
	}
	if !strings.Contains(urlSpace, "existing=1&info_hash=%20%00%01") {
		t.Errorf("info_hash should use percent escaping and preserve existing query, got %q", urlSpace)
	}

	// Invalid URL
	_, err = buildHTTPAnnounceURL("://bad", req)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestBuildScrapeURL(t *testing.T) {
	hashes := [][20]byte{
		{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14},
	}

	u, err := buildScrapeURL("http://tracker.example.com:6969/announce", hashes)
	if err != nil {
		t.Fatalf("buildScrapeURL error: %v", err)
	}
	if !strings.Contains(u, "/scrape") {
		t.Errorf("expected /scrape in URL, got %q", u)
	}
	if !strings.Contains(u, "info_hash=") {
		t.Error("missing info_hash param")
	}

	// No /announce in path
	u2, err := buildScrapeURL("http://tracker.example.com:6969/", hashes)
	if err != nil {
		t.Fatalf("buildScrapeURL error: %v", err)
	}
	if strings.Contains(u2, "/scrape") {
		t.Errorf("URL should not contain /scrape when announce path missing: %q", u2)
	}

	// Invalid URL
	_, err = buildScrapeURL("://bad", hashes)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestParseHTTPAnnounceResponse(t *testing.T) {
	peerData := []byte{192, 168, 1, 100, 0x1A, 0xE1}

	d := bencode.NewDict()
	d.Set("interval", bencode.IntVal{I: 1800})
	d.Set("complete", bencode.IntVal{I: 100})
	d.Set("incomplete", bencode.IntVal{I: 50})
	d.Set("peers", bencode.StringVal{S: string(peerData)})
	respBytes, err := bencode.Marshal(d)
	if err != nil {
		t.Fatalf("bencode.Marshal error: %v", err)
	}

	resp, err := parseHTTPAnnounceResponse(respBytes)
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse error: %v", err)
	}
	if resp.Interval != 1800 {
		t.Errorf("interval = %d, want 1800", resp.Interval)
	}
	if resp.Complete != 100 {
		t.Errorf("complete = %d, want 100", resp.Complete)
	}
	if resp.Incomplete != 50 {
		t.Errorf("incomplete = %d, want 50", resp.Incomplete)
	}
	if len(resp.Peers) != 1 {
		t.Fatalf("peers = %d, want 1", len(resp.Peers))
	}
	if !resp.Peers[0].IP.Equal(net.IP{192, 168, 1, 100}) {
		t.Errorf("peer IP = %v", resp.Peers[0].IP)
	}

	// Failure reason
	failResp := "d14:failure reason16:something broke"
	_, err = parseHTTPAnnounceResponse([]byte(failResp))
	if err == nil {
		t.Error("expected error for failure reason")
	}

	// Invalid bencode
	_, err = parseHTTPAnnounceResponse([]byte("not bencode"))
	if err == nil {
		t.Error("expected error for invalid bencode")
	}

	// Peers as list of dicts (built programmatically for correct bencode)
	peerList := bencode.NewList(
		buildPeerDict("10.0.0.1", 3000),
		buildPeerDict("172.16.0.2", 8080),
	)
	respDict := bencode.NewDict()
	respDict.Set("interval", bencode.IntVal{I: 1800})
	respDict.Set("peers", peerList)
	respBytes, err = bencode.Marshal(respDict)
	if err != nil {
		t.Fatalf("bencode.Marshal error: %v", err)
	}
	resp2, err := parseHTTPAnnounceResponse(respBytes)
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse list peers error: %v", err)
	}
	if len(resp2.Peers) != 2 {
		t.Fatalf("list peers count = %d, want 2", len(resp2.Peers))
	}
	if !resp2.Peers[0].IP.Equal(net.ParseIP("10.0.0.1")) {
		t.Errorf("list peer 0 IP = %v", resp2.Peers[0].IP)
	}
	if resp2.Peers[0].Port != 3000 {
		t.Errorf("list peer 0 port = %d", resp2.Peers[0].Port)
	}
	if !resp2.Peers[1].IP.Equal(net.ParseIP("172.16.0.2")) {
		t.Errorf("list peer 1 IP = %v", resp2.Peers[1].IP)
	}
	if resp2.Peers[1].Port != 8080 {
		t.Errorf("list peer 1 port = %d", resp2.Peers[1].Port)
	}

	badCompact := bencode.NewDict()
	badCompact.Set("interval", bencode.IntVal{I: 1800})
	badCompact.Set("peers", bencode.StringVal{S: string([]byte{1, 2, 3, 4, 5})})
	respBytes, err = bencode.Marshal(badCompact)
	if err != nil {
		t.Fatalf("bencode.Marshal error: %v", err)
	}
	_, err = parseHTTPAnnounceResponse(respBytes)
	if !errors.Is(err, ErrInvalidResp) {
		t.Fatalf("expected invalid response for bad compact peers, got %v", err)
	}

	badPeerList := bencode.NewList(buildPeerDict("10.0.0.1", 70000))
	respDict = bencode.NewDict()
	respDict.Set("interval", bencode.IntVal{I: 1800})
	respDict.Set("peers", badPeerList)
	respBytes, err = bencode.Marshal(respDict)
	if err != nil {
		t.Fatalf("bencode.Marshal error: %v", err)
	}
	resp3, err := parseHTTPAnnounceResponse(respBytes)
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse bad list peer error: %v", err)
	}
	if len(resp3.Peers) != 0 {
		t.Fatalf("invalid port peer count = %d, want 0", len(resp3.Peers))
	}
}

func TestParseScrapeResponse(t *testing.T) {
	ih := [20]byte{0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x30, 0x31, 0x32, 0x33}
	ihStr := string(ih[:])

	files := bencode.NewDict()
	entry := bencode.NewDict()
	entry.Set("complete", bencode.IntVal{I: 100})
	entry.Set("downloaded", bencode.IntVal{I: 500})
	entry.Set("incomplete", bencode.IntVal{I: 50})
	files.Set(ihStr, entry)

	outer := bencode.NewDict()
	outer.Set("files", files)
	respBytes, err := bencode.Marshal(outer)
	if err != nil {
		t.Fatalf("bencode.Marshal error: %v", err)
	}

	resp, err := parseScrapeResponse(respBytes)
	if err != nil {
		t.Fatalf("parseScrapeResponse error: %v", err)
	}
	sd, ok := resp[ih]
	if !ok {
		t.Fatal("info hash not found in scrape response")
	}
	if sd.Complete != 100 {
		t.Errorf("complete = %d, want 100", sd.Complete)
	}
	if sd.Downloaded != 500 {
		t.Errorf("downloaded = %d, want 500", sd.Downloaded)
	}
	if sd.Incomplete != 50 {
		t.Errorf("incomplete = %d, want 50", sd.Incomplete)
	}

	badEntry := bencode.NewDict()
	badEntry.Set("complete", bencode.IntVal{I: 1})
	badEntry.Set("incomplete", bencode.IntVal{I: 2})
	badFiles := bencode.NewDict()
	badFiles.Set(ihStr, badEntry)
	badOuter := bencode.NewDict()
	badOuter.Set("files", badFiles)
	respBytes, err = bencode.Marshal(badOuter)
	if err != nil {
		t.Fatalf("bencode.Marshal error: %v", err)
	}
	_, err = parseScrapeResponse(respBytes)
	if !errors.Is(err, ErrInvalidResp) {
		t.Fatalf("expected invalid response for missing downloaded, got %v", err)
	}

	badFiles = bencode.NewDict()
	badFiles.Set("short", entry)
	badOuter = bencode.NewDict()
	badOuter.Set("files", badFiles)
	respBytes, err = bencode.Marshal(badOuter)
	if err != nil {
		t.Fatalf("bencode.Marshal error: %v", err)
	}
	_, err = parseScrapeResponse(respBytes)
	if !errors.Is(err, ErrInvalidResp) {
		t.Fatalf("expected invalid response for short scrape key, got %v", err)
	}

	// Failure reason
	failResp := "d14:failure reason16:something broke"
	_, err = parseScrapeResponse([]byte(failResp))
	if err == nil {
		t.Error("expected error for scrape failure reason")
	}

	// Missing files key
	_, err = parseScrapeResponse([]byte("d8:intervali1800ee"))
	if err == nil {
		t.Error("expected error for missing files key")
	}

	// Invalid bencode
	_, err = parseScrapeResponse([]byte("not bencode"))
	if err == nil {
		t.Error("expected error for invalid bencode")
	}
}

func TestParseHTTPAnnounceResponse_TrackerID(t *testing.T) {
	respData := "d8:intervali1800e5:peers0:10:tracker id5:my_ide"
	resp, err := parseHTTPAnnounceResponse([]byte(respData))
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse error: %v", err)
	}
	if resp.TrackerID != "my_id" {
		t.Errorf("tracker_id = %q, want %q", resp.TrackerID, "my_id")
	}
}

func TestParseHTTPAnnounceResponse_WarningMessage(t *testing.T) {
	respData := "d8:intervali1800e5:peers0:15:warning message12:slow server!e"
	resp, err := parseHTTPAnnounceResponse([]byte(respData))
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse error: %v", err)
	}
	if resp.WarningMessage != "slow server!" {
		t.Errorf("warning_message = %q, want %q", resp.WarningMessage, "slow server!")
	}
}

func TestHTTPKey(t *testing.T) {
	peerID := [20]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	key := httpKey(peerID)
	if key != string(peerID[12:]) {
		t.Errorf("httpKey = %x, want %x", []byte(key), peerID[12:])
	}
	if len(key) != 8 {
		t.Errorf("httpKey length = %d, want 8", len(key))
	}
}

func TestParseHTTPAnnounceResponse_MinInterval(t *testing.T) {
	respData := "d8:intervali1800e12:min intervali600e5:peers0:e"
	resp, err := parseHTTPAnnounceResponse([]byte(respData))
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse error: %v", err)
	}
	if resp.MinInterval != 600 {
		t.Errorf("min_interval = %d, want 600", resp.MinInterval)
	}

	// min_interval > interval should be clamped
	respData2 := "d8:intervali1800e12:min intervali3600e5:peers0:e"
	resp2, err := parseHTTPAnnounceResponse([]byte(respData2))
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse error: %v", err)
	}
	if resp2.MinInterval != 1800 {
		t.Errorf("min_interval = %d, want 1800 (clamped)", resp2.MinInterval)
	}

	// No min_interval defaults to interval
	respData3 := "d8:intervali1800e5:peers0:e"
	resp3, err := parseHTTPAnnounceResponse([]byte(respData3))
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse error: %v", err)
	}
	if resp3.MinInterval != 1800 {
		t.Errorf("min_interval = %d, want 1800 (defaulted)", resp3.MinInterval)
	}
}

func TestDecodePeerDict(t *testing.T) {
	// nil for non-bencode value
	if pi := decodePeerDict(nil); pi != nil {
		t.Error("decodePeerDict(nil) should return nil")
	}

	// non-dict value
	if pi := decodePeerDict(bencode.IntVal{I: 42}); pi != nil {
		t.Error("decodePeerDict(non-dict) should return nil")
	}

	// missing ip
	d := bencode.NewDict()
	d.Set("port", bencode.IntVal{I: 6881})
	if pi := decodePeerDict(d); pi != nil {
		t.Error("decodePeerDict without ip should return nil")
	}

	// missing port
	d2 := bencode.NewDict()
	d2.Set("ip", bencode.StringVal{S: "1.2.3.4"})
	if pi := decodePeerDict(d2); pi != nil {
		t.Error("decodePeerDict without port should return nil")
	}

	// invalid IP
	d3 := bencode.NewDict()
	d3.Set("ip", bencode.StringVal{S: "not_an_ip"})
	d3.Set("port", bencode.IntVal{I: 6881})
	if pi := decodePeerDict(d3); pi != nil {
		t.Error("decodePeerDict with invalid IP should return nil")
	}

	// Valid
	d4 := bencode.NewDict()
	d4.Set("ip", bencode.StringVal{S: "10.0.0.1"})
	d4.Set("port", bencode.IntVal{I: 6881})
	pi := decodePeerDict(d4)
	if pi == nil {
		t.Fatal("decodePeerDict for valid dict returned nil")
	}
	if !pi.IP.Equal(net.IP{10, 0, 0, 1}) {
		t.Errorf("IP = %v", pi.IP)
	}
	if pi.Port != 6881 {
		t.Errorf("port = %d", pi.Port)
	}
}

func TestDecodeCompactPeers(t *testing.T) {
	peerData := []byte{10, 0, 0, 1, 0x0B, 0xB8}
	peers, err := decodeCompactPeers(peerData, false)
	if err != nil {
		t.Fatalf("decodeCompactPeers error: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("len = %d, want 1", len(peers))
	}
	if !peers[0].IP.Equal(net.IP{10, 0, 0, 1}) {
		t.Errorf("IP = %v", peers[0].IP)
	}
	if peers[0].Port != 3000 {
		t.Errorf("port = %d", peers[0].Port)
	}

	// Invalid length
	if peers, err := decodeCompactPeers([]byte{1, 2, 3}, false); !errors.Is(err, ErrInvalidResp) || peers != nil {
		t.Fatalf("decodeCompactPeers with bad length = peers %v, err %v; want invalid response", peers, err)
	}

	// IPv6
	ip6Data := []byte{
		0xfe, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x0B, 0xB8,
	}
	peers6, err := decodeCompactPeers(ip6Data, true)
	if err != nil {
		t.Fatalf("decodeCompactPeers IPv6 error: %v", err)
	}
	if len(peers6) != 1 {
		t.Fatalf("IPv6 len = %d, want 1", len(peers6))
	}
	if !peers6[0].IP.Equal(net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}) {
		t.Errorf("IPv6 IP = %v", peers6[0].IP)
	}
	if peers6[0].Port != 3000 {
		t.Errorf("IPv6 port = %d", peers6[0].Port)
	}
}

func TestThreePeersUnpack(t *testing.T) {
	peersData := []byte{
		192, 168, 1, 100, 0x1A, 0xE1,
		10, 0, 0, 1, 0x0B, 0xB8,
		172, 16, 0, 2, 0x1F, 0x90,
	}
	peers, err := unpackCompactPeers(peersData, false)
	if err != nil {
		t.Fatalf("unpackCompactPeers error: %v", err)
	}
	if len(peers) != 3 {
		t.Fatalf("len = %d, want 3", len(peers))
	}
	want := []struct {
		ip   string
		port uint16
	}{
		{"192.168.1.100", 6881},
		{"10.0.0.1", 3000},
		{"172.16.0.2", 8080},
	}
	for i, w := range want {
		if !peers[i].IP.Equal(net.ParseIP(w.ip)) {
			t.Errorf("peers[%d].IP = %v, want %s", i, peers[i].IP, w.ip)
		}
		if peers[i].Port != w.port {
			t.Errorf("peers[%d].Port = %d, want %d", i, peers[i].Port, w.port)
		}
	}
}

func TestScrapeData_Struct(t *testing.T) {
	sd := ScrapeData{Complete: 100, Incomplete: 50, Downloaded: 500}
	if sd.Complete != 100 {
		t.Errorf("Complete = %d", sd.Complete)
	}
	if sd.Incomplete != 50 {
		t.Errorf("Incomplete = %d", sd.Incomplete)
	}
	if sd.Downloaded != 500 {
		t.Errorf("Downloaded = %d", sd.Downloaded)
	}
}

func TestAnnounceRequest_Defaults(t *testing.T) {
	req := AnnounceRequest{}
	if err := req.ValidateEvent(); err != nil {
		t.Errorf("empty event should be valid: %v", err)
	}
	if req.NumWant != 0 {
		t.Errorf("NumWant default should be 0")
	}
}

func TestParseHTTPAnnounceResponse_Peers6(t *testing.T) {
	peer6Data := []byte{
		0x20, 0x01, 0x0d, 0xb8, 0x85, 0xa3, 0x00, 0x00,
		0x00, 0x00, 0x8a, 0x2e, 0x03, 0x70, 0x73, 0x34,
		0x1A, 0xE1,
	}

	d := bencode.NewDict()
	d.Set("interval", bencode.IntVal{I: 1800})
	d.Set("peers", bencode.StringVal{S: ""})
	d.Set("peers6", bencode.StringVal{S: string(peer6Data)})
	respBytes, err := bencode.Marshal(d)
	if err != nil {
		t.Fatalf("bencode.Marshal error: %v", err)
	}

	resp, err := parseHTTPAnnounceResponse(respBytes)
	if err != nil {
		t.Fatalf("parseHTTPAnnounceResponse error: %v", err)
	}
	if len(resp.Peers6) != 1 {
		t.Fatalf("peers6 = %d, want 1", len(resp.Peers6))
	}
	if !resp.Peers6[0].IP.Equal(net.ParseIP("2001:db8:85a3::8a2e:370:7334")) {
		t.Errorf("IPv6 IP = %v", resp.Peers6[0].IP)
	}
	if resp.Peers6[0].Port != 6881 {
		t.Errorf("IPv6 port = %d", resp.Peers6[0].Port)
	}
}

func buildPeerDict(ip string, port int) bencode.Value {
	d := bencode.NewDict()
	d.Set("ip", bencode.StringVal{S: ip})
	d.Set("port", bencode.IntVal{I: int64(port)})
	return d
}
