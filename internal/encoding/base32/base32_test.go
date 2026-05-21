package base32

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestEncode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"1", "GE======"},
		{"12", "GEZA===="},
		{"123", "GEZDG==="},
		{"1234", "GEZDGNA="},
		{"12345", "GEZDGNBV"},
	}
	for _, tc := range tests {
		got := Encode([]byte(tc.input))
		if got != tc.expected {
			t.Errorf("Encode(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestEncodeHexToBase32(t *testing.T) {
	hexStr := "248d0a1cd08284"
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	got := Encode(raw)
	if want := "ESGQUHGQQKCA===="; got != want {
		t.Errorf("Encode(hex(%q)) = %q, want %q", hexStr, got, want)
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"GE======", "1"},
		{"GEZA====", "12"},
		{"GEZDG===", "123"},
		{"GEZDGNA=", "1234"},
		{"GEZDGNBV", "12345"},
	}
	for _, tc := range tests {
		got, err := Decode(tc.input)
		if err != nil {
			t.Errorf("Decode(%q) error: %v", tc.input, err)
			continue
		}
		if string(got) != tc.expected {
			t.Errorf("Decode(%q) = %q, want %q", tc.input, string(got), tc.expected)
		}
	}
}

func TestDecodeCaseInsensitive(t *testing.T) {
	got, err := Decode("gezdgnbv")
	if err != nil {
		t.Fatalf("Decode(lowercase): %v", err)
	}
	if string(got) != "12345" {
		t.Errorf("Decode(lowercase) = %q, want %q", string(got), "12345")
	}
}

func TestDecodeInvalidChar(t *testing.T) {
	tests := []string{
		"GEZDGNB0",
		"GEZDGNB!",
		"GEZDG!BV",
	}
	for _, input := range tests {
		_, err := Decode(input)
		if err == nil {
			t.Errorf("Decode(%q) expected error", input)
			continue
		}
		if !errors.Is(err, ErrInvalidChar) {
			t.Errorf("Decode(%q) error = %v, want ErrInvalidChar", input, err)
		}
	}
}

func TestDecodeLengthNotMultipleOf8(t *testing.T) {
	_, err := Decode("ABC")
	if err == nil {
		t.Fatal("expected error for length not multiple of 8")
	}
	if !errors.Is(err, ErrInvalidChar) {
		t.Errorf("error = %v, want ErrInvalidChar", err)
	}
}

func TestEncodeToString(t *testing.T) {
	got := EncodeToString([]byte("12345"))
	if want := "GEZDGNBV"; got != want {
		t.Errorf("EncodeToString = %q, want %q", got, want)
	}
}

func TestDecodeString(t *testing.T) {
	got, err := DecodeString("GEZDGNBV")
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if string(got) != "12345" {
		t.Errorf("DecodeString = %q, want %q", string(got), "12345")
	}
}

func TestRoundTripAllByteValues(t *testing.T) {
	for n := 1; n <= 20; n++ {
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i)
		}
		encoded := Encode(data)
		decoded, err := Decode(encoded)
		if err != nil {
			t.Errorf("Decode(Encode(%d bytes)) error: %v", n, err)
			continue
		}
		if string(decoded) != string(data) {
			t.Errorf("round-trip mismatch for %d bytes: got %x, want %x", n, decoded, data)
		}
	}
}
