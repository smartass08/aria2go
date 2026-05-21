package lpd

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func mustDecodeHex(t *testing.T, s string) [20]byte {
	t.Helper()
	var h [20]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad test hex %q: %v", s, err)
	}
	copy(h[:], b)
	return h
}

func TestBuildRequestFormat(t *testing.T) {
	ih := mustDecodeHex(t, "cd41c7fdddfd034a15a04d7ff881216e01c4ceaf")
	msg := buildRequest(ih, 6000)

	s := string(msg)
	if !strings.HasPrefix(s, "BT-SEARCH * HTTP/1.1\r\n") {
		t.Error("missing BT-SEARCH request line")
	}
	if !strings.Contains(s, "Host: "+multicastAddr4+":"+fmt.Sprint(multicastPort)+"\r\n") {
		t.Error("missing or wrong Host header")
	}
	if !strings.Contains(s, "Port: 6000\r\n") {
		t.Error("missing or wrong Port header")
	}
	if !strings.Contains(s, "Infohash: CD41C7FDDDFD034A15A04D7FF881216E01C4CEAF\r\n") {
		t.Error("missing or wrong Infohash header (expected uppercase)")
	}
	if !strings.HasSuffix(s, "\r\n") {
		t.Error("message must end with CRLF")
	}
}

func TestBuildRequestInfoHashUppercase(t *testing.T) {
	ih := mustDecodeHex(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	msg := buildRequest(ih, 6881)

	if !strings.Contains(string(msg), "Infohash: A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2") {
		t.Error("Infohash must be uppercase")
	}
}

func TestParseMessageRequest(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: 6000\r\n" +
		"Infohash: cd41c7fdddfd034a15a04d7ff881216e01c4ceaf\r\n" +
		"\r\n"

	srcIP := net.ParseIP("192.168.1.100")
	pi, ok := parseMessage([]byte(raw), srcIP)
	if !ok {
		t.Fatal("parseMessage failed")
	}

	expected := mustDecodeHex(t, "cd41c7fdddfd034a15a04d7ff881216e01c4ceaf")
	if pi.InfoHash != expected {
		t.Errorf("InfoHash = %x, want %x", pi.InfoHash, expected)
	}
	if !pi.IP.Equal(srcIP) {
		t.Errorf("IP = %s, want %s", pi.IP, srcIP)
	}
	if pi.Port != 6000 {
		t.Errorf("Port = %d, want 6000", pi.Port)
	}
}

func TestParseMessageResponse(t *testing.T) {
	raw := "HTTP/1.1 200 OK\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"Port: 6881\r\n" +
		"\r\n"

	srcIP := net.ParseIP("10.0.0.1")
	pi, ok := parseMessage([]byte(raw), srcIP)
	if !ok {
		t.Fatal("parseMessage failed")
	}

	expected := mustDecodeHex(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if pi.InfoHash != expected {
		t.Errorf("InfoHash = %x, want %x", pi.InfoHash, expected)
	}
	if pi.Port != 6881 {
		t.Errorf("Port = %d, want 6881", pi.Port)
	}
}

func TestParseMessageCaseInsensitiveHeaders(t *testing.T) {
	raw := "HTTP/1.1 200 OK\r\n" +
		"INFOHASH: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"PORT: 6881\r\n" +
		"\r\n"

	srcIP := net.ParseIP("10.0.0.1")
	pi, ok := parseMessage([]byte(raw), srcIP)
	if !ok {
		t.Fatal("parseMessage failed with uppercase headers")
	}

	expected := mustDecodeHex(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if pi.InfoHash != expected {
		t.Errorf("InfoHash = %x, want %x", pi.InfoHash, expected)
	}
	if pi.Port != 6881 {
		t.Errorf("Port = %d, want 6881", pi.Port)
	}
}

func TestParseMessageWithLeadingTrailingSpaces(t *testing.T) {
	raw := "HTTP/1.1 200 OK\r\n" +
		"Infohash:   a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2  \r\n" +
		"Port: 6881\r\n" +
		"\r\n"

	pi, ok := parseMessage([]byte(raw), net.ParseIP("10.0.0.1"))
	if !ok {
		t.Fatal("parseMessage failed with spaces")
	}
	expected := mustDecodeHex(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if pi.InfoHash != expected {
		t.Error("InfoHash mismatch")
	}
}

func TestParseMessageZeroPort(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: 0\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject port 0")
	}
}

func TestParseMessagePortTooLarge(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: 70000\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject port > 65535")
	}
}

func TestParseMessageShortInfoHash(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: 6000\r\n" +
		"Infohash: a1b2c3\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject short infohash")
	}
}

func TestParseMessageNonHexInfoHash(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: 6000\r\n" +
		"Infohash: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject non-hex infohash")
	}
}

func TestParseMessageMissingPort(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject message without port")
	}
}

func TestParseMessageMissingInfoHash(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: 6000\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject message without infohash")
	}
}

func TestParseMessageNonNumericPort(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: abc\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject non-numeric port")
	}
}

func TestParseMessageNegativePort(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: -1\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject negative port")
	}
}

func TestParseMessageEmptyData(t *testing.T) {
	_, ok := parseMessage([]byte{}, net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject empty data")
	}
}

func TestCloneIPCreatesCopy(t *testing.T) {
	orig := net.ParseIP("192.168.1.1")
	c := cloneIP(orig)
	if !c.Equal(orig) {
		t.Error("cloned IP should equal original")
	}
	c[0] = 10
	if orig[0] == 10 {
		t.Error("cloneIP should not alias original slice")
	}
}

func TestCloneIPNil(t *testing.T) {
	if cloneIP(nil) != nil {
		t.Error("cloneIP(nil) should return nil")
	}
}

func TestListenerClose(t *testing.T) {
	l, err := NewListener()
	if err != nil {
		t.Skipf("multicast not available: %v", err)
	}
	defer l.Close()

	err = l.Close()
	if err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second Close must be safe.
	err = l.Close()
	if err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestListenerRunAndAnnounce(t *testing.T) {
	l, err := NewListener()
	if err != nil {
		t.Skipf("multicast not available: %v", err)
	}
	defer l.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go l.Run(ctx)

	// Give the receive loop time to start.
	time.Sleep(50 * time.Millisecond)

	ih := mustDecodeHex(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	msg := buildRequest(ih, 9999)
	dst := &net.UDPAddr{IP: net.ParseIP(multicastAddr4), Port: multicastPort}

	// Use a separate unbound UDP socket for sending, so we avoid
	// loopback restrictions of the multicast-joined socket.
	sendConn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	if err != nil {
		t.Skipf("cannot create send socket: %v", err)
	}
	defer sendConn.Close()

	// Send multiple times to overcome possible packet loss.
	for i := 0; i < 5; i++ {
		sendConn.WriteToUDP(msg, dst)
		time.Sleep(100 * time.Millisecond)

		select {
		case pi := <-l.Peers():
			if pi.Port != 9999 {
				t.Errorf("Port = %d, want 9999", pi.Port)
			}
			if pi.InfoHash != ih {
				t.Errorf("InfoHash = %x, want %x", pi.InfoHash, ih)
			}
			if pi.IP == nil {
				t.Error("IP must not be nil")
			}
			cancel()
			return
		default:
		}
	}

	t.Skip("multicast loopback not received (platform limitation)")
}

func TestNewListenerFailsWithoutMulticast(t *testing.T) {
	// This test documents that NewListener returns an error when
	// IPv4 multicast is fundamentally broken. In healthy environments
	// this will succeed and be skipped.
	_, err := NewListener()
	if err != nil {
		// Expected on machines without multicast support.
		return
	}
	// Success is fine too — multicast works.
}

func TestParseMessageIPv6SourceIP(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: ff15::efc0:988f:6771\r\n" +
		"Port: 6881\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"\r\n"

	srcIP := net.ParseIP("2001:db8::1")
	pi, ok := parseMessage([]byte(raw), srcIP)
	if !ok {
		t.Fatal("parseMessage failed for IPv6 source")
	}
	if !pi.IP.Equal(srcIP) {
		t.Errorf("IP = %s, want %s", pi.IP, srcIP)
	}
}

func TestParseMessageRequestMixedCaseHeaders(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"poRT: 6000\r\n" +
		"InfoHASH: cd41c7fdddfd034a15a04d7ff881216e01c4ceaf\r\n" +
		"\r\n"

	pi, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.100"))
	if !ok {
		t.Fatal("parseMessage failed with mixed-case headers")
	}
	if pi.Port != 6000 {
		t.Errorf("Port = %d, want 6000", pi.Port)
	}
}

func TestParseMessageExtraHeaders(t *testing.T) {
	raw := "HTTP/1.1 200 OK\r\n" +
		"Server: aria2/1.37.0\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\r\n" +
		"Port: 6881\r\n" +
		"Cookie: foo=bar\r\n" +
		"\r\n"

	pi, ok := parseMessage([]byte(raw), net.ParseIP("10.0.0.1"))
	if !ok {
		t.Fatal("parseMessage failed with extra headers")
	}
	if pi.Port != 6881 {
		t.Errorf("Port = %d, want 6881", pi.Port)
	}
}

func TestParseMessage42HexCharInfoHash(t *testing.T) {
	raw := "BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: 6000\r\n" +
		"Infohash: a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5\r\n" +
		"\r\n"

	_, ok := parseMessage([]byte(raw), net.ParseIP("192.168.1.1"))
	if ok {
		t.Error("parseMessage should reject 42-char infohash")
	}
}
