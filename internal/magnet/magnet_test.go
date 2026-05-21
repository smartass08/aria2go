package magnet

import (
	"errors"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

func v1Ptr(h core.InfoHashV1) *core.InfoHashV1 { return &h }
func v2Ptr(h core.InfoHashV2) *core.InfoHashV2 { return &h }

func mustV1(t *testing.T, s string) core.InfoHashV1 {
	t.Helper()
	h, err := core.ParseInfoHashV1(s)
	if err != nil {
		t.Fatalf("bad test hash: %v", err)
	}
	return h
}

func mustV2(t *testing.T, s string) core.InfoHashV2 {
	t.Helper()
	h, err := core.ParseInfoHashV2(s)
	if err != nil {
		t.Fatalf("bad test hash: %v", err)
	}
	return h
}

func TestParseV1Hex(t *testing.T) {
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
	// The hash is case-insensitive on input; core.ParseInfoHashV1 handles lowercase
}

func TestParseV1HexUppercase(t *testing.T) {
	raw := "magnet:?xt=urn:btih:A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
}

func TestParseV1HexMixedCase(t *testing.T) {
	raw := "magnet:?xt=urn:btih:A1b2C3d4E5f6A1b2C3d4E5f6A1b2C3d4E5f6A1b2"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
}

func TestParseV1Base32(t *testing.T) {
	// "A" * 32 in base32 decodes to 20 zero bytes
	raw := "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
}

func TestParseV2(t *testing.T) {
	testHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	raw := "magnet:?xt=urn:btmh:" + strings.ToLower(testHash)
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV2 == nil {
		t.Fatal("expected v2 infohash")
	}
}

func TestParseV2WithMultihashPrefix(t *testing.T) {
	// 1220 is the multihash prefix for SHA-256
	raw := "magnet:?xt=urn:btmh:1220a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV2 == nil {
		t.Fatal("expected v2 infohash")
	}
}

func TestParseFullURI(t *testing.T) {
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"&dn=My%20File" +
		"&xl=1048576" +
		"&tr=http%3A%2F%2Ftracker.example.com%3A6969%2Fannounce" +
		"&tr=udp%3A%2F%2Ftracker2.example.com%3A6881%2Fannounce" +
		"&xs=http%3A%2F%2Fexample.com%2Ffile.torrent" +
		"&as=http%3A%2F%2Fexample.com%2Fsource" +
		"&x.pe=192.168.1.1%3A6881" +
		"&x.pe=%5B2001%3Adb8%3A%3A1%5D%3A6881" +
		"&kt=keyword1" +
		"&kt=keyword2" +
		"&mt=topic1"

	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
	if m.DisplayName != "My File" {
		t.Errorf("expected DisplayName 'My File', got %q", m.DisplayName)
	}
	if m.Length != 1048576 {
		t.Errorf("expected Length 1048576, got %d", m.Length)
	}
	if len(m.Trackers) != 2 {
		t.Errorf("expected 2 trackers, got %d", len(m.Trackers))
	}
	if m.Trackers[0] != "http://tracker.example.com:6969/announce" {
		t.Errorf("unexpected tracker[0]: %s", m.Trackers[0])
	}
	if len(m.ExactSources) != 1 {
		t.Errorf("expected 1 xs, got %d", len(m.ExactSources))
	}
	if len(m.AcceptableSources) != 1 {
		t.Errorf("expected 1 as, got %d", len(m.AcceptableSources))
	}
	if len(m.Peers) != 2 {
		t.Errorf("expected 2 peers, got %d", len(m.Peers))
	}
	if len(m.KeywordTopics) != 2 {
		t.Errorf("expected 2 kt, got %d", len(m.KeywordTopics))
	}
	if len(m.ManifestTopics) != 1 {
		t.Errorf("expected 1 mt, got %d", len(m.ManifestTopics))
	}
}

func TestParseCaseInsensitiveKeys(t *testing.T) {
	raw := "magnet:?XT=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"&DN=hello" +
		"&XL=500" +
		"&TR=http://t.example.com/announce"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
	if m.DisplayName != "hello" {
		t.Errorf("expected DisplayName 'hello', got %q", m.DisplayName)
	}
	if m.Length != 500 {
		t.Errorf("expected Length 500, got %d", m.Length)
	}
	if len(m.Trackers) != 1 {
		t.Errorf("expected 1 tracker, got %d", len(m.Trackers))
	}
}

func TestParseMissingXT(t *testing.T) {
	_, err := Parse("magnet:?dn=hello")
	if err == nil {
		t.Fatal("expected error for missing xt")
	}
	var me *Error
	if !errors.As(err, &me) {
		t.Fatalf("expected *magnet.Error, got %T", err)
	}
	if me.Code != core.ExitMagnetParseError {
		t.Errorf("expected code %d, got %d", core.ExitMagnetParseError, me.Code)
	}
}

func TestParseNoPrefix(t *testing.T) {
	_, err := Parse("http://example.com")
	if err == nil {
		t.Fatal("expected error for missing magnet: prefix")
	}
	var me *Error
	if !errors.As(err, &me) {
		t.Fatalf("expected *magnet.Error, got %T", err)
	}
}

func TestParseEmptyQuery(t *testing.T) {
	_, err := Parse("magnet:?")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestParseInvalidXTHash(t *testing.T) {
	_, err := Parse("magnet:?xt=urn:btih:not40hex")
	if err == nil {
		t.Fatal("expected error for invalid hash")
	}
}

func TestParseV2HashTooShort(t *testing.T) {
	_, err := Parse("magnet:?xt=urn:btmh:tooshort")
	if err == nil {
		t.Fatal("expected error for short v2 hash")
	}
}

func TestParseInvalidXL(t *testing.T) {
	_, err := Parse("magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2&xl=abc")
	if err == nil {
		t.Fatal("expected error for invalid xl")
	}
}

func TestParseUnsupportedXTURN(t *testing.T) {
	_, err := Parse("magnet:?xt=urn:sha1:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if err == nil {
		t.Fatal("expected error for unsupported URN")
	}
}

func TestParseIgnoresUnsupportedXTWhenValidBTIHExists(t *testing.T) {
	raw := "magnet:?xt=urn:sha1:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"&xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
}

func TestParsePercentEncodedXT(t *testing.T) {
	raw := "magnet:?xt=urn%3Abtih%3Aa1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
}

func TestParseDuplicateV1XT(t *testing.T) {
	// C++ silently takes the first valid xt; Go matches this.
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"&xt=urn:btih:b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expected := mustV1(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if *m.InfoHashV1 != expected {
		t.Error("expected first v1 infohash")
	}
}

func TestParseBothV1AndV2(t *testing.T) {
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"&xt=urn:btmh:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
	if m.InfoHashV2 == nil {
		t.Fatal("expected v2 infohash")
	}
}

func TestRoundTripV1(t *testing.T) {
	raw := "magnet:?xt=urn:btih:A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2" +
		"&dn=My%20File" +
		"&xl=1048576" +
		"&tr=http%3A%2F%2Ftracker.example.com%3A6969%2Fannounce" +
		"&x.pe=192.168.1.1%3A6881"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := m.String()
	m2, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse round-trip: %v\noutput: %s", err, out)
	}
	if m2.InfoHashV1 == nil || *m.InfoHashV1 != *m2.InfoHashV1 {
		t.Error("v1 hash mismatch after round-trip")
	}
	if m2.DisplayName != m.DisplayName {
		t.Errorf("dn mismatch: %q vs %q", m.DisplayName, m2.DisplayName)
	}
	if m2.Length != m.Length {
		t.Errorf("xl mismatch: %d vs %d", m.Length, m2.Length)
	}
	if len(m2.Trackers) != len(m.Trackers) {
		t.Errorf("tr count mismatch: %d vs %d", len(m.Trackers), len(m2.Trackers))
	}
	if len(m2.Peers) != len(m.Peers) {
		t.Errorf("peers count mismatch: %d vs %d", len(m.Peers), len(m2.Peers))
	}
}

func TestRoundTripV2(t *testing.T) {
	raw := "magnet:?xt=urn:btmh:A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2" +
		"&dn=Test"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	out := m.String()
	m2, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse round-trip: %v", err)
	}
	if m2.InfoHashV2 == nil || *m.InfoHashV2 != *m2.InfoHashV2 {
		t.Error("v2 hash mismatch after round-trip")
	}
}

func TestRoundTripV1AndV2(t *testing.T) {
	v1 := mustV1(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	v2 := mustV2(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	m := &Magnet{
		InfoHashV1:  v1Ptr(v1),
		InfoHashV2:  v2Ptr(v2),
		DisplayName: "hybrid",
		Trackers:    []string{"http://tracker.example.com/announce"},
		Peers:       []string{"192.168.1.1:6881"},
	}
	out := m.String()
	m2, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse round-trip: %v\noutput: %s", err, out)
	}
	if *m2.InfoHashV1 != v1 {
		t.Error("v1 hash mismatch")
	}
	if *m2.InfoHashV2 != v2 {
		t.Error("v2 hash mismatch")
	}
	if m2.DisplayName != "hybrid" {
		t.Errorf("dn mismatch: %q", m2.DisplayName)
	}
	if len(m2.Trackers) != 1 {
		t.Errorf("tr count: %d", len(m2.Trackers))
	}
}

func TestStringSkipsEmptyFields(t *testing.T) {
	v1 := mustV1(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	m := &Magnet{InfoHashV1: v1Ptr(v1)}
	out := m.String()
	if !strings.Contains(out, "xt=urn:btih:") {
		t.Error("expected xt in output")
	}
	if strings.Contains(out, "dn=") {
		t.Error("unexpected dn= in output for empty DisplayName")
	}
	if strings.Contains(out, "xl=") {
		t.Error("unexpected xl= in output for zero Length")
	}
}

func TestErrorCodeMapping(t *testing.T) {
	_, err := Parse("magnet:?dn=hello")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, &Error{Code: core.ExitMagnetParseError}) {
		t.Error("expected ExitMagnetParseError sentinel")
	}
}

func TestParseV1Base32Uppercase(t *testing.T) {
	// Real base32 encoded hash — 32 uppercase chars
	raw := "magnet:?xt=urn:btih:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
}

func TestParseDuplicateV2XT(t *testing.T) {
	raw := "magnet:?xt=urn:btmh:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" +
		"&xt=urn:btmh:b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	expected := mustV2(t, "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if *m.InfoHashV2 != expected {
		t.Error("expected first v2 infohash")
	}
}

func TestParsePlusNotConvertedToSpace(t *testing.T) {
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2&dn=file+name"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.DisplayName != "file+name" {
		t.Errorf("expected DisplayName 'file+name', got %q", m.DisplayName)
	}
}

func TestPercentDecodeInvalidSequences(t *testing.T) {
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2&dn=hello%zzworld%2Etxt"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// %zz is kept literal, %2E is decoded to '.'
	if m.DisplayName != "hello%zzworld.txt" {
		t.Errorf("expected DisplayName 'hello%%zzworld.txt', got %q", m.DisplayName)
	}
}

func TestParseKeyOnlyParams(t *testing.T) {
	// Parameters without '=' should be treated as key="" (not silently skipped).
	// Matches aria2: magnet.cc:55-57 + util::divide(key_range, '', key_range, empty_range).
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2&dn"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
	if m.DisplayName != "" {
		t.Errorf("expected empty DisplayName for key-only dn, got %q", m.DisplayName)
	}

	raw2 := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2&kt&tr=http://t.example.com/announce"
	m2, err := Parse(raw2)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(m2.KeywordTopics) != 1 {
		t.Errorf("expected 1 kt for key-only param, got %d", len(m2.KeywordTopics))
	}
	if m2.KeywordTopics[0] != "" {
		t.Errorf("expected empty kt value, got %q", m2.KeywordTopics[0])
	}
	if len(m2.Trackers) != 1 {
		t.Errorf("expected 1 tr, got %d", len(m2.Trackers))
	}
}

func TestPercentDecodeTruncatedEscape(t *testing.T) {
	raw := "magnet:?xt=urn:btih:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2&dn=file%2"
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// truncated escape at end of string kept literal
	if m.DisplayName != "file%2" {
		t.Errorf("expected DisplayName 'file%%2', got %q", m.DisplayName)
	}
}

func TestParseRealWorldMagnet(t *testing.T) {
	// A typical real-world magnet link
	raw := "magnet:?xt=urn:btih:7C2C2DDF4F22F3A366A2A23ECB0B37B7793C9E21" +
		"&dn=ubuntu-22.04.3-desktop-amd64.iso" +
		"&tr=http://tracker.example.com:6969/announce" +
		"&tr=udp://tracker.opentrackr.org:1337/announce" +
		"&xl=5041676288"

	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.InfoHashV1 == nil {
		t.Fatal("expected v1 infohash")
	}
	if m.DisplayName != "ubuntu-22.04.3-desktop-amd64.iso" {
		t.Errorf("DisplayName: %q", m.DisplayName)
	}
	if len(m.Trackers) != 2 {
		t.Errorf("expected 2 trackers, got %d", len(m.Trackers))
	}
	if m.Length != 5041676288 {
		t.Errorf("Length: %d", m.Length)
	}
}
