package core

import (
	"testing"
	"time"
)

func TestParseGID_Decimal(t *testing.T) {
	tests := []struct {
		input string
		want  GID
	}{
		{"0", 0},
		{"1", 1},
		{"42", 42},
		{"18446744073709551615", 18446744073709551615},
	}
	for _, tc := range tests {
		g, err := ParseGID(tc.input)
		if err != nil {
			t.Errorf("ParseGID(%q) error: %v", tc.input, err)
			continue
		}
		if g != tc.want {
			t.Errorf("ParseGID(%q) = %d, want %d", tc.input, uint64(g), uint64(tc.want))
		}
	}
}

func TestParseGID_Hex(t *testing.T) {
	tests := []struct {
		input string
		want  GID
	}{
		{"0000000000000000", 0},
		{"0000000000000001", 1},
		{"000000000000002a", 42},
		{"ffffffffffffffff", 0xFFFFFFFFFFFFFFFF},
		{"deadbeefcafebabe", 0xDEADBEEFCAFEBABE},
	}
	for _, tc := range tests {
		g, err := ParseGID(tc.input)
		if err != nil {
			t.Errorf("ParseGID(%q) error: %v", tc.input, err)
			continue
		}
		if g != tc.want {
			t.Errorf("ParseGID(%q) = %d (0x%x), want %d (0x%x)", tc.input, uint64(g), uint64(g), uint64(tc.want), uint64(tc.want))
		}
	}
}

func TestParseGID_Invalid(t *testing.T) {
	inputs := []string{"", "notanumber", "1.5", "gggggggggggggggg", "-1"}
	for _, s := range inputs {
		_, err := ParseGID(s)
		if err == nil {
			t.Errorf("ParseGID(%q) expected error, got nil", s)
		}
	}
}

func TestParseGID_Ambiguous(t *testing.T) {
	// "0000000000000064" is 16 chars of valid hex → parse as hex = 100
	// "100" is decimal → parse as decimal = 100
	g1, err := ParseGID("0000000000000064")
	if err != nil || g1 != 100 {
		t.Errorf("ParseGID(hex) = %d, err=%v; want 100", uint64(g1), err)
	}
	g2, err := ParseGID("100")
	if err != nil || g2 != 100 {
		t.Errorf("ParseGID(decimal) = %d, err=%v; want 100", uint64(g2), err)
	}
}

func TestGID_String(t *testing.T) {
	tests := []struct {
		g    GID
		want string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{0xDEADBEEF, "3735928559"},
	}
	for _, tc := range tests {
		if got := tc.g.String(); got != tc.want {
			t.Errorf("GID(%d).String() = %q, want %q", uint64(tc.g), got, tc.want)
		}
	}
}

func TestGID_Hex(t *testing.T) {
	tests := []struct {
		g    GID
		want string
	}{
		{0, "0000000000000000"},
		{1, "0000000000000001"},
		{42, "000000000000002a"},
		{0xDEADBEEFCAFEBABE, "deadbeefcafebabe"},
		{0xFFFFFFFFFFFFFFFF, "ffffffffffffffff"},
	}
	for _, tc := range tests {
		if got := tc.g.Hex(); got != tc.want {
			t.Errorf("GID(%d).Hex() = %q, want %q", uint64(tc.g), got, tc.want)
		}
	}
}

func TestStatus_Values(t *testing.T) {
	// Verify exact iota values
	if int(StatusWaiting) != 0 {
		t.Errorf("StatusWaiting = %d, want 0", StatusWaiting)
	}
	if int(StatusActive) != 1 {
		t.Errorf("StatusActive = %d, want 1", StatusActive)
	}
	if int(StatusPaused) != 2 {
		t.Errorf("StatusPaused = %d, want 2", StatusPaused)
	}
	if int(StatusComplete) != 3 {
		t.Errorf("StatusComplete = %d, want 3", StatusComplete)
	}
	if int(StatusError) != 4 {
		t.Errorf("StatusError = %d, want 4", StatusError)
	}
	if int(StatusRemoved) != 5 {
		t.Errorf("StatusRemoved = %d, want 5", StatusRemoved)
	}
}

func TestStatus_String(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{StatusWaiting, "waiting"},
		{StatusActive, "active"},
		{StatusPaused, "paused"},
		{StatusComplete, "complete"},
		{StatusError, "error"},
		{StatusRemoved, "removed"},
		{Status(99), "unknown(99)"},
	}
	for _, tc := range tests {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("Status(%d).String() = %q, want %q", int(tc.s), got, tc.want)
		}
	}
}

func TestErrorCode_Values(t *testing.T) {
	// Verify the exact mapping per contracts/error-codes.md
	expected := map[ErrorCode]int{
		ExitSuccess:                 0,
		ExitUnknownError:            1,
		ExitTimeout:                 2,
		ExitResourceNotFound:        3,
		ExitMaxFileNotFound:         4,
		ExitTooSlow:                 5,
		ExitNetworkProblem:          6,
		ExitUnfinishedDownloads:     7,
		ExitRemoteFileError:         8,
		ExitNotEnoughDiskSpace:      9,
		ExitPieceLengthChanged:      10,
		ExitSameFileDownloading:     11,
		ExitSameInfoHashDownloading: 12,
		ExitFileAlreadyExists:       13,
		ExitRenameFailed:            14,
		ExitOpenFileError:           15,
		ExitFileCreateError:         16,
		ExitFileIOError:             17,
		ExitDirCreateError:          18,
		ExitNameResolveError:        19,
		ExitMetalinkParseError:      20,
		ExitFTPProtocolError:        21,
		ExitHTTPProtocolError:       22,
		ExitTooManyRedirects:        23,
		ExitHTTPAuthFailed:          24,
		ExitBencodeParseError:       25,
		ExitTorrentParseError:       26,
		ExitMagnetParseError:        27,
		ExitBadOption:               28,
		ExitHTTPServiceUnavailable:  29,
		ExitJSONParseError:          30,
		ExitRemoved:                 31,
		ExitChecksumError:           32,
	}
	for code, want := range expected {
		if int(code) != want {
			t.Errorf("ErrorCode %d has value %d, want %d", want, int(code), want)
		}
	}
}

func TestEventKind_Values(t *testing.T) {
	if int(EvStart) != 0 {
		t.Errorf("EvStart = %d, want 0", EvStart)
	}
	if int(EvPause) != 1 {
		t.Errorf("EvPause = %d, want 1", EvPause)
	}
	if int(EvStop) != 2 {
		t.Errorf("EvStop = %d, want 2", EvStop)
	}
	if int(EvComplete) != 3 {
		t.Errorf("EvComplete = %d, want 3", EvComplete)
	}
	if int(EvError) != 4 {
		t.Errorf("EvError = %d, want 4", EvError)
	}
	if int(EvBTComplete) != 5 {
		t.Errorf("EvBTComplete = %d, want 5", EvBTComplete)
	}
}

func TestEventKind_String(t *testing.T) {
	tests := []struct {
		k    EventKind
		want string
	}{
		{EvStart, "start"},
		{EvPause, "pause"},
		{EvStop, "stop"},
		{EvComplete, "complete"},
		{EvError, "error"},
		{EvBTComplete, "btcomplete"},
		{EventKind(99), "unknown(99)"},
	}
	for _, tc := range tests {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("EventKind(%d).String() = %q, want %q", int(tc.k), got, tc.want)
		}
	}
}

func TestEvent(t *testing.T) {
	now := time.Now()
	ev := Event{Kind: EvStart, GID: 42, Time: now}
	if ev.Kind != EvStart {
		t.Error("Event.Kind mismatch")
	}
	if ev.GID != 42 {
		t.Error("Event.GID mismatch")
	}
	if !ev.Time.Equal(now) {
		t.Error("Event.Time mismatch")
	}
}

func TestInfoHashV1(t *testing.T) {
	// round-trip
	hash := InfoHashV1{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	hex := hash.String()
	if hex != "000102030405060708090a0b0c0d0e0f10111213" {
		t.Errorf("InfoHashV1.String() = %q", hex)
	}

	parsed, err := ParseInfoHashV1(hex)
	if err != nil {
		t.Fatalf("ParseInfoHashV1(%q) error: %v", hex, err)
	}
	if parsed != hash {
		t.Errorf("ParseInfoHashV1 round-trip failed: %x != %x", parsed[:], hash[:])
	}
}

func TestParseInfoHashV1_Invalid(t *testing.T) {
	_, err := ParseInfoHashV1("short")
	if err == nil {
		t.Error("ParseInfoHashV1(short) expected error")
	}
	_, err = ParseInfoHashV1("000102030405060708090a0b0c0d0e0f1011121g")
	if err == nil {
		t.Error("ParseInfoHashV1(invalid hex) expected error")
	}
}

func TestInfoHashV2(t *testing.T) {
	hash := InfoHashV2{}
	for i := range hash {
		hash[i] = byte(i)
	}
	hex := hash.String()
	expectedHex := "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	if hex != expectedHex {
		t.Errorf("InfoHashV2.String() = %q, want %q", hex, expectedHex)
	}

	parsed, err := ParseInfoHashV2(hex)
	if err != nil {
		t.Fatalf("ParseInfoHashV2(%q) error: %v", hex, err)
	}
	if parsed != hash {
		t.Errorf("ParseInfoHashV2 round-trip failed: %x != %x", parsed[:], hash[:])
	}
}

func TestParseInfoHashV2_Invalid(t *testing.T) {
	_, err := ParseInfoHashV2("short")
	if err == nil {
		t.Error("ParseInfoHashV2(short) expected error")
	}
	_, err = ParseInfoHashV2("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1g")
	if err == nil {
		t.Error("ParseInfoHashV2(invalid hex) expected error")
	}
}
