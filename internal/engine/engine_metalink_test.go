package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/config"
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
