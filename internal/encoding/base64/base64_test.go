package base64

import (
	"errors"
	"testing"
)

func TestEncode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello World!", "SGVsbG8gV29ybGQh"},
		{"Hello World", "SGVsbG8gV29ybGQ="},
		{"Hello Worl", "SGVsbG8gV29ybA=="},
		{"Man", "TWFu"},
		{"M", "TQ=="},
		{"", ""},
	}
	for _, tc := range tests {
		got := Encode([]byte(tc.input))
		if got != tc.expected {
			t.Errorf("Encode(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestEncodeFFBytes(t *testing.T) {
	got := Encode([]byte{0xff})
	if got != "/w==" {
		t.Errorf("Encode({0xff}) = %q, want %q", got, "/w==")
	}
	got = Encode([]byte{0xff, 0xff})
	if got != "//8=" {
		t.Errorf("Encode({0xff,0xff}) = %q, want %q", got, "//8=")
	}
	got = Encode([]byte{0xff, 0xff, 0xff})
	if got != "////" {
		t.Errorf("Encode({0xff,0xff,0xff}) = %q, want %q", got, "////")
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SGVsbG8gV29ybGQh", "Hello World!"},
		{"SGVsbG8gV29ybGQ=", "Hello World"},
		{"SGVsbG8gV29ybA==", "Hello Worl"},
		{"TWFu", "Man"},
		{"TWFu\n", "Man"},
		{"TQ==", "M"},
		{"", ""},
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

func TestDecodeWithGarbage(t *testing.T) {
	got, err := Decode("SGVsbG8\ngV2*9ybGQ=")
	if err != nil {
		t.Fatalf("Decode with garbage: %v", err)
	}
	if string(got) != "Hello World" {
		t.Errorf("Decode(garbage) = %q, want %q", string(got), "Hello World")
	}
}

func TestDecodeInvalidGarbage(t *testing.T) {
	_, err := Decode("SGVsbG8\ngV2*9ybGQ")
	if err == nil {
		t.Fatal("expected error for garbage without trailing =")
	}
	if !errors.Is(err, ErrInvalidChar) {
		t.Errorf("error = %v, want ErrInvalidChar", err)
	}
}

func TestDecodeFFBytes(t *testing.T) {
	got, err := Decode("/w==")
	if err != nil {
		t.Fatalf("Decode(/w==): %v", err)
	}
	if len(got) != 1 || got[0] != 0xff {
		t.Errorf("Decode(/w==) = %x, want [ff]", got)
	}
}

func TestRoundTrip2KB(t *testing.T) {
	const size = 2048
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}
	encoded := Encode(data)
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode 2KB: %v", err)
	}
	if string(decoded) != string(data) {
		t.Fatal("2KB round-trip mismatch")
	}
}

func TestDecodeInvalidChar(t *testing.T) {
	_, err := Decode("!!!!")
	if err == nil {
		t.Fatal("expected error for all-invalid input")
	}
	if !errors.Is(err, ErrInvalidChar) {
		t.Errorf("error = %v, want ErrInvalidChar", err)
	}
}
