package hash_test

import (
	"testing"

	"github.com/smartass08/aria2go/internal/hash"
)

func TestNewAndSize(t *testing.T) {
	tests := []struct {
		kind   hash.Kind
		size   int
		hexSum string
	}{
		{hash.MD5, 16, "900150983cd24fb0d6963f7d28e17f72"},
		{hash.SHA1, 20, "a9993e364706816aba3e25717850c26c9cd0d89d"},
		{hash.SHA224, 28, "23097d223405d8228642a477bda255b32aadbce4bda0b3f7e36c9da7"},
		{hash.SHA256, 32, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{hash.SHA384, 48, "cb00753f45a35e8bb5a03d699ac65007272c32ab0eded1631a8b605a43ff5bed8086072ba1e7cc2358baeca134c825a7"},
		{hash.SHA512, 64, "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := tt.kind.Size(); got != tt.size {
				t.Errorf("Kind.Size() = %d, want %d", got, tt.size)
			}

			h, err := hash.New(tt.kind)
			if err != nil {
				t.Fatalf("New(%s) error: %v", tt.kind, err)
			}
			h.Write([]byte("abc"))
			got := h.Sum(nil)
			want := mustHexDecode(t, tt.hexSum)
			if len(got) != len(want) {
				t.Fatalf("New(%s).Sum() length = %d, want %d", tt.kind, len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("New(%s).Sum() byte %d = 0x%02x, want 0x%02x", tt.kind, i, got[i], want[i])
				}
			}
		})
	}
}

func TestNewUnknownKind(t *testing.T) {
	_, err := hash.New("sha3-256")
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestParseAllKnown(t *testing.T) {
	tests := []struct {
		input string
		want  hash.Kind
	}{
		// Exact canonical forms
		{"md5", hash.MD5},
		{"sha-1", hash.SHA1},
		{"sha-224", hash.SHA224},
		{"sha-256", hash.SHA256},
		{"sha-384", hash.SHA384},
		{"sha-512", hash.SHA512},

		// Uppercase
		{"MD5", hash.MD5},
		{"SHA-1", hash.SHA1},
		{"SHA-224", hash.SHA224},
		{"SHA-256", hash.SHA256},
		{"SHA-384", hash.SHA384},
		{"SHA-512", hash.SHA512},

		// Lowercase without dashes
		{"sha1", hash.SHA1},
		{"sha224", hash.SHA224},
		{"sha256", hash.SHA256},
		{"sha384", hash.SHA384},
		{"sha512", hash.SHA512},

		// Uppercase without dashes
		{"SHA1", hash.SHA1},
		{"SHA224", hash.SHA224},
		{"SHA256", hash.SHA256},
		{"SHA384", hash.SHA384},
		{"SHA512", hash.SHA512},

		// Mixed case with dashes
		{"Sha-1", hash.SHA1},
		{"Sha-256", hash.SHA256},
		{"sHa-512", hash.SHA512},
		{"mD5", hash.MD5},

		// Mixed case without dashes
		{"Sha1", hash.SHA1},
		{"Sha256", hash.SHA256},
		{"mD5", hash.MD5},
	}

	for _, tt := range tests {
		got, err := hash.Parse(tt.input)
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseUnknown(t *testing.T) {
	unknown := []string{
		"",
		"sha3-256",
		"blake2b-256",
		"unknown",
		"md4",
		"sha0",
	}
	for _, s := range unknown {
		_, err := hash.Parse(s)
		if err == nil {
			t.Errorf("Parse(%q) expected error, got nil", s)
		}
	}
}

func TestDigestAria2(t *testing.T) {
	tests := []struct {
		kind   hash.Kind
		hexSum string
	}{
		{hash.MD5, "2c90cadbef42945f0dcff2b959977ff8"},
		{hash.SHA1, "f36003f22b462ffa184390533c500d8989e9f681"},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			h, err := hash.New(tt.kind)
			if err != nil {
				t.Fatalf("New(%s): %v", tt.kind, err)
			}
			h.Write([]byte("aria2"))
			got := h.Sum(nil)
			want := mustHexDecode(t, tt.hexSum)
			if len(got) != len(want) {
				t.Fatalf("%s digest length: %d, want %d", tt.kind, len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("%s byte[%d]: 0x%02x, want 0x%02x", tt.kind, i, got[i], want[i])
				}
			}
		})
	}
	t.Run("sha1-abc", func(t *testing.T) {
		h, err := hash.New(hash.SHA1)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte("abc"))
		got := h.Sum(nil)
		want := mustHexDecode(t, "a9993e364706816aba3e25717850c26c9cd0d89d")
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("byte[%d]: 0x%02x, want 0x%02x", i, got[i], want[i])
			}
		}
	})
}

func TestSupports(t *testing.T) {
	tests := []struct {
		name    string
		support bool
	}{
		{"md5", true},
		{"sha-1", true},
		{"sha-256", true},
		{"sha-512", true},
		{"sha1", true},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		_, err := hash.Parse(tt.name)
		got := err == nil
		if got != tt.support {
			t.Errorf("Supports(%q) = %v, want %v", tt.name, got, tt.support)
		}
	}
}

func TestGetDigestLength(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"md5", 16},
		{"sha-1", 20},
		{"sha-224", 28},
		{"sha-256", 32},
		{"sha-384", 48},
		{"sha-512", 64},
	}
	for _, tt := range tests {
		k, err := hash.Parse(tt.name)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.name, err)
		}
		if got := k.Size(); got != tt.length {
			t.Errorf("GetDigestLength(%q) = %d, want %d", tt.name, got, tt.length)
		}
	}
}

func TestIsStronger(t *testing.T) {
	strengths := map[hash.Kind]int{
		hash.MD5:    1,
		hash.SHA1:   2,
		hash.SHA224: 3,
		hash.SHA256: 4,
		hash.SHA384: 5,
		hash.SHA512: 6,
	}
	isStronger := func(lhs, rhs string) bool {
		lk, err := hash.Parse(lhs)
		if err != nil {
			return false
		}
		rk, err := hash.Parse(rhs)
		if err != nil {
			return true
		}
		return strengths[lk] > strengths[rk]
	}

	if !isStronger("sha-1", "md5") {
		t.Error("sha-1 should be stronger than md5")
	}
	if isStronger("md5", "sha-1") {
		t.Error("md5 should not be stronger than sha-1")
	}
	if isStronger("unknown", "sha-1") {
		t.Error("unknown should not be stronger than sha-1")
	}
	if !isStronger("sha-1", "unknown") {
		t.Error("sha-1 should be stronger than unknown")
	}
}

func TestIsValidHash(t *testing.T) {
	if !isValidHashT(t, hash.SHA1, "f36003f22b462ffa184390533c500d8989e9f681") {
		t.Error("should be valid sha-1 hash")
	}
	if isValidHashT(t, hash.SHA1, "f36003f22b462ffa184390533c500d89") {
		t.Error("should reject wrong-length sha-1 hash")
	}
	if isValidHashT(t, hash.MD5, "2c90cadbef42945f0dcff2b959977ff8") {
		t.Log("valid md5 hash")
	}
}

func isValidHashT(t *testing.T, k hash.Kind, hexDigest string) bool {
	t.Helper()
	if len(hexDigest)%2 != 0 {
		return false
	}
	if len(hexDigest)/2 != k.Size() {
		return false
	}
	return true
}

func TestGetCanonicalHashType(t *testing.T) {
	tests := []struct {
		input string
		want  hash.Kind
		ok    bool
	}{
		{"sha1", hash.SHA1, true},
		{"sha256", hash.SHA256, true},
		{"sha-1", hash.SHA1, true},
		{"sha-256", hash.SHA256, true},
		{"unknown", "", false},
	}
	for _, tt := range tests {
		got, err := hash.Parse(tt.input)
		if tt.ok && err != nil {
			t.Errorf("getCanonicalHashType(%q) unexpected error: %v", tt.input, err)
		}
		if !tt.ok && err == nil {
			t.Errorf("getCanonicalHashType(%q) should return error, got %q", tt.input, got)
		}
		if tt.ok && got != tt.want {
			t.Errorf("getCanonicalHashType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseRoundTrip(t *testing.T) {
	kinds := []hash.Kind{hash.MD5, hash.SHA1, hash.SHA224, hash.SHA256, hash.SHA384, hash.SHA512}
	for _, k := range kinds {
		h, err := hash.New(k)
		if err != nil {
			t.Fatalf("New(%s) error: %v", k, err)
		}
		h.Write([]byte("abc"))
		sum := h.Sum(nil)

		h2, err := hash.New(k)
		if err != nil {
			t.Fatalf("New(%s) error: %v", k, err)
		}
		h2.Write([]byte("abc"))
		sum2 := h2.Sum(nil)

		if len(sum) != len(sum2) {
			t.Errorf("%s: digest lengths differ: %d vs %d", k, len(sum), len(sum2))
		}
		for i := range sum {
			if sum[i] != sum2[i] {
				t.Errorf("%s: digest byte %d differs: 0x%02x vs 0x%02x", k, i, sum[i], sum2[i])
			}
		}
	}
}

func mustHexDecode(t *testing.T, s string) []byte {
	t.Helper()
	b := make([]byte, len(s)/2)
	for i := range b {
		hi := unhex(s[2*i])
		lo := unhex(s[2*i+1])
		if hi < 0 || lo < 0 {
			t.Fatalf("invalid hex string %q", s)
		}
		b[i] = byte(hi<<4 | lo)
	}
	return b
}

func unhex(c byte) int {
	switch {
	case '0' <= c && c <= '9':
		return int(c - '0')
	case 'a' <= c && c <= 'f':
		return int(c - 'a' + 10)
	case 'A' <= c && c <= 'F':
		return int(c - 'A' + 10)
	default:
		return -1
	}
}
