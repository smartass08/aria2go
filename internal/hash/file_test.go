package hash_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/smartass08/aria2go/internal/hash"
)

func TestParseChecksumSpec(t *testing.T) {
	kind, digest, err := hash.ParseChecksumSpec("SHA-1 = a9993e364706816aba3e25717850c26c9cd0d89d")
	if err != nil {
		t.Fatalf("ParseChecksumSpec() error = %v", err)
	}
	if kind != hash.SHA1 {
		t.Fatalf("kind = %q, want %q", kind, hash.SHA1)
	}
	want := mustHexDecode(t, "a9993e364706816aba3e25717850c26c9cd0d89d")
	if !bytes.Equal(digest, want) {
		t.Fatalf("digest = %x, want %x", digest, want)
	}
}

func TestParseChecksumSpecRejectsInvalidDigest(t *testing.T) {
	if _, _, err := hash.ParseChecksumSpec("md5=xyz"); err == nil {
		t.Fatal("ParseChecksumSpec() error = nil, want invalid digest error")
	}
	if _, _, err := hash.ParseChecksumSpec("sha-1=abcd"); err == nil {
		t.Fatal("ParseChecksumSpec() error = nil, want invalid digest length error")
	}
}

func TestSumReaderAndFile(t *testing.T) {
	data := []byte("aria2go hash helpers\n")
	want := mustHexDecode(t, "0985122923940e7a774328a677fd5622804d8e352485d4dae39905844c603ce9")

	sum, err := hash.SumReader(bytes.NewReader(data), hash.SHA256)
	if err != nil {
		t.Fatalf("SumReader() error = %v", err)
	}
	if !bytes.Equal(sum, want) {
		t.Fatalf("SumReader() = %x, want %x", sum, want)
	}

	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fileSum, err := hash.SumFile(path, hash.SHA256)
	if err != nil {
		t.Fatalf("SumFile() error = %v", err)
	}
	if !bytes.Equal(fileSum, want) {
		t.Fatalf("SumFile() = %x, want %x", fileSum, want)
	}
}
