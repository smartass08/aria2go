package lpd

import (
	"encoding/hex"
	"net"
	"testing"
)

func mustDecodeHexBench(b *testing.B, s string) [20]byte {
	b.Helper()
	var h [20]byte
	data, err := hex.DecodeString(s)
	if err != nil {
		b.Fatalf("bad test hex %q: %v", s, err)
	}
	copy(h[:], data)
	return h
}

func BenchmarkParseMessage(b *testing.B) {
	raw := []byte("BT-SEARCH * HTTP/1.1\r\n" +
		"Host: 239.192.152.143:6771\r\n" +
		"Port: 6000\r\n" +
		"Infohash: cd41c7fdddfd034a15a04d7ff881216e01c4ceaf\r\n" +
		"\r\n")
	srcIP := net.ParseIP("192.168.1.100")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = parseMessage(raw, srcIP)
	}
}

func BenchmarkBuildRequest(b *testing.B) {
	ih := mustDecodeHexBench(b, "cd41c7fdddfd034a15a04d7ff881216e01c4ceaf")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildRequest(ih, 6000)
	}
}

func BenchmarkBuildRequestBuf(b *testing.B) {
	ih := mustDecodeHexBench(b, "cd41c7fdddfd034a15a04d7ff881216e01c4ceaf")
	portStr := "6000"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf [256]byte
		_ = buildRequestBuf(ih, portStr, buf[:0])
	}
}
