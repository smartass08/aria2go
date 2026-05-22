package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/hash"
)

func TestMetalinkDownloadEntriesUseParsedURLsByPriority(t *testing.T) {
	data := []byte(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="pkg.tar">
    <size>123</size>
    <url priority="20">http://slow.example/pkg.tar</url>
    <url priority="5">http://fast.example/pkg.tar</url>
  </file>
</metalink>`)

	got, err := metalinkDownloadEntries(data, &config.Options{})
	if err != nil {
		t.Fatalf("metalinkDownloadEntries() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if got[0].URI != "http://fast.example/pkg.tar" {
		t.Fatalf("first URI = %q, want fast mirror", got[0].URI)
	}
	if got[0].Name != "pkg.tar" || got[0].Size != 123 {
		t.Fatalf("metadata = (%q,%d), want (pkg.tar,123)", got[0].Name, got[0].Size)
	}
	if !got[0].SizeKnown {
		t.Fatal("SizeKnown = false, want true")
	}
}

func TestMetalinkDownloadEntriesPreferConfiguredProtocol(t *testing.T) {
	data := []byte(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="pkg.tar">
    <url priority="1">ftp://mirror.example/pkg.tar</url>
    <url priority="10">https://mirror.example/pkg.tar</url>
  </file>
</metalink>`)

	got, err := metalinkDownloadEntries(data, &config.Options{MetalinkPreferredProtocol: "https"})
	if err != nil {
		t.Fatalf("metalinkDownloadEntries() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if got[0].URI != "https://mirror.example/pkg.tar" {
		t.Fatalf("first URI = %q, want preferred https mirror", got[0].URI)
	}
}

func TestMetalinkDownloadEntriesPreferConfiguredLocation(t *testing.T) {
	data := []byte(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="pkg.tar">
    <url priority="1" location="de">http://de.example/pkg.tar</url>
    <url priority="20" location="us">http://us.example/pkg.tar</url>
  </file>
</metalink>`)

	got, err := metalinkDownloadEntries(data, &config.Options{MetalinkLocation: "us"})
	if err != nil {
		t.Fatalf("metalinkDownloadEntries() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if got[0].URI != "http://us.example/pkg.tar" || got[0].Name != "pkg.tar" {
		t.Fatalf("first entry = %#v", got[0])
	}
	if got[1].URI != "http://de.example/pkg.tar" {
		t.Fatalf("second URI = %q, want de mirror", got[1].URI)
	}
}

func TestMetalinkDownloadEntriesFilterByVersionLanguageOS(t *testing.T) {
	data := []byte(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="drop.bin">
    <version>2.0</version>
    <language>fr</language>
    <os>windows</os>
    <url>http://mirror.example/drop.bin</url>
  </file>
  <file name="keep.bin">
    <version>1.0</version>
    <language>en</language>
    <os>linux</os>
    <url>http://mirror.example/keep.bin</url>
  </file>
</metalink>`)

	got, err := metalinkDownloadEntries(data, &config.Options{
		MetalinkVersion:  "1.0",
		MetalinkLanguage: "en",
		MetalinkOS:       "linux",
	})
	if err != nil {
		t.Fatalf("metalinkDownloadEntries() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].Name != "keep.bin" || got[0].URI != "http://mirror.example/keep.bin" {
		t.Fatalf("entry = %#v, want keep.bin", got[0])
	}
}

func TestMetalinkDownloadEntriesResolveRelativeURLWithBaseURI(t *testing.T) {
	data := []byte(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="pkg.tar">
    <url>payload/pkg.tar</url>
  </file>
</metalink>`)

	got, err := metalinkDownloadEntries(data, &config.Options{
		MetalinkBaseURI: "http://mirror.example/downloads/",
	})
	if err != nil {
		t.Fatalf("metalinkDownloadEntries() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].URI != "http://mirror.example/downloads/payload/pkg.tar" {
		t.Fatalf("resolved URI = %q", got[0].URI)
	}
}

func TestMetalinkDownloadEntriesCarryParsedMetadata(t *testing.T) {
	data := []byte(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="pkg.tar">
    <size>3</size>
    <version>1.2.3</version>
    <language>en</language>
    <os>linux</os>
    <hash type="sha-256">039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81</hash>
    <pieces type="sha-256" length="2">
      <hash>96a296d224f285c67bee93c30f8a309157f0daa35dc5b87e410b78630a09cfc7</hash>
      <hash>2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881</hash>
    </pieces>
    <url>http://mirror.example/pkg.tar</url>
  </file>
</metalink>`)

	got, err := metalinkDownloadEntries(data, &config.Options{})
	if err != nil {
		t.Fatalf("metalinkDownloadEntries() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	entry := got[0]
	if entry.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", entry.Version)
	}
	if len(entry.Languages) != 1 || entry.Languages[0] != "en" {
		t.Fatalf("Languages = %#v, want [en]", entry.Languages)
	}
	if len(entry.OSes) != 1 || entry.OSes[0] != "linux" {
		t.Fatalf("OSes = %#v, want [linux]", entry.OSes)
	}
	if len(entry.Hashes[hash.SHA256]) != hash.SHA256.Size() {
		t.Fatalf("sha-256 hash length = %d, want %d", len(entry.Hashes[hash.SHA256]), hash.SHA256.Size())
	}
	if entry.PieceLength != 2 || len(entry.Pieces) != 2 {
		t.Fatalf("piece metadata = (%d,%d), want (2,2)", entry.PieceLength, len(entry.Pieces))
	}
	if entry.PieceHashKind != hash.SHA256 {
		t.Fatalf("PieceHashKind = %q, want %q", entry.PieceHashKind, hash.SHA256)
	}
}

func TestMetalinkDownloadEntriesCarryKnownZeroSize(t *testing.T) {
	data := []byte(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="empty">
    <size>0</size>
    <url>http://mirror.example/empty</url>
  </file>
</metalink>`)

	got, err := metalinkDownloadEntries(data, &config.Options{})
	if err != nil {
		t.Fatalf("metalinkDownloadEntries() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1", len(got))
	}
	if got[0].Size != 0 || !got[0].SizeKnown {
		t.Fatalf("size metadata = (%d,%v), want known zero size", got[0].Size, got[0].SizeKnown)
	}
}

func TestMetalinkDownloadEntriesIncludeAllFiles(t *testing.T) {
	data := []byte(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="first">
    <url priority="10">http://mirror.example/first</url>
  </file>
  <file name="second">
    <url priority="1">http://mirror.example/second</url>
  </file>
</metalink>`)

	got, err := metalinkDownloadEntries(data, &config.Options{})
	if err != nil {
		t.Fatalf("metalinkDownloadEntries() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if got[0].Name != "first" || got[1].Name != "second" {
		t.Fatalf("entry names = %q/%q, want first/second", got[0].Name, got[1].Name)
	}
}

func TestVerifyMetalinkDownloadRejectsWholeFileHashMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg.tar")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	code, msg := verifyMetalinkDownload(context.Background(), path, metalinkDownloadEntry{
		Hashes: map[hash.Kind][]byte{
			hash.SHA256: bytesForHash(t, hash.SHA256, "different"),
		},
	})
	if code != core.ExitChecksumError {
		t.Fatalf("verification code = %d, want %d", code, core.ExitChecksumError)
	}
	if msg == "" {
		t.Fatal("verification message is empty")
	}
}

func TestVerifyMetalinkDownloadRejectsPieceHashMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pkg.tar")
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	code, msg := verifyMetalinkDownload(context.Background(), path, metalinkDownloadEntry{
		PieceHashKind: hash.SHA1,
		PieceLength:   3,
		Pieces: [][]byte{
			bytesForHash(t, hash.SHA1, "abc"),
			bytesForHash(t, hash.SHA1, "zzz"),
		},
	})
	if code != core.ExitChecksumError {
		t.Fatalf("verification code = %d, want %d", code, core.ExitChecksumError)
	}
	if msg == "" {
		t.Fatal("verification message is empty")
	}
}

func bytesForHash(t *testing.T, kind hash.Kind, data string) []byte {
	t.Helper()
	h, err := hash.New(kind)
	if err != nil {
		t.Fatalf("hash.New(%q): %v", kind, err)
	}
	defer hash.PoolPut(kind, h)
	if _, err := h.Write([]byte(data)); err != nil {
		t.Fatalf("hash write: %v", err)
	}
	return h.Sum(nil)
}
