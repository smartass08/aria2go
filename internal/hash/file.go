package hash

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseChecksumSpec parses a --checksum value of the form TYPE=HEX_DIGEST.
func ParseChecksumSpec(spec string) (Kind, []byte, error) {
	algo, digestHex, ok := strings.Cut(spec, "=")
	if !ok || strings.TrimSpace(algo) == "" || strings.TrimSpace(digestHex) == "" {
		return "", nil, fmt.Errorf("hash: invalid checksum spec %q", spec)
	}
	kind, err := Parse(strings.TrimSpace(algo))
	if err != nil {
		return "", nil, err
	}
	digest, err := hex.DecodeString(strings.TrimSpace(digestHex))
	if err != nil {
		return "", nil, fmt.Errorf("hash: invalid checksum digest %q: %w", spec, err)
	}
	if len(digest) != kind.Size() {
		return "", nil, fmt.Errorf("hash: checksum digest size %d does not match %s", len(digest), kind)
	}
	return kind, digest, nil
}

// SumFile computes the checksum of path using the given algorithm.
func SumFile(path string, kind Kind) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return SumReader(f, kind)
}

// SumReader computes the checksum of r using the given algorithm.
func SumReader(r io.Reader, kind Kind) ([]byte, error) {
	h, err := New(kind)
	if err != nil {
		return nil, err
	}
	defer PoolPut(kind, h)
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
