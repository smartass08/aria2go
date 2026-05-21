package metalink_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/hash"
	"github.com/smartass08/aria2go/internal/protocol/metalink"
)

func TestParseV4Basic(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <size>123456789</size>
    <url priority="1">http://mirror1.example.com/example.iso</url>
    <url priority="2" location="us">http://mirror2.example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
	f := doc.Files[0]
	if f.Name != "example.iso" {
		t.Errorf("Name = %q, want %q", f.Name, "example.iso")
	}
	if f.Size != 123456789 {
		t.Errorf("Size = %d, want %d", f.Size, 123456789)
	}
	if !f.SizeKnown {
		t.Error("SizeKnown = false, want true")
	}
	if len(f.URLs) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(f.URLs))
	}
	u0 := f.URLs[0]
	if u0.URL != "http://mirror1.example.com/example.iso" {
		t.Errorf("URLs[0].URL = %q", u0.URL)
	}
	if u0.Priority != 1 {
		t.Errorf("URLs[0].Priority = %d, want %d", u0.Priority, 1)
	}
	if u0.Location != "" {
		t.Errorf("URLs[0].Location = %q, want empty", u0.Location)
	}
	u1 := f.URLs[1]
	if u1.URL != "http://mirror2.example.com/example.iso" {
		t.Errorf("URLs[1].URL = %q", u1.URL)
	}
	if u1.Priority != 2 {
		t.Errorf("URLs[1].Priority = %d, want %d", u1.Priority, 2)
	}
	if u1.Location != "us" {
		t.Errorf("URLs[1].Location = %q, want %q", u1.Location, "us")
	}
}

func TestParseV4WithHash(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <size>123456789</size>
    <hash type="sha-256">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	f := doc.Files[0]
	if len(f.Hashes) != 1 {
		t.Fatalf("expected 1 hash, got %d", len(f.Hashes))
	}
	digest, ok := f.Hashes[hash.SHA256]
	if !ok {
		t.Fatal("expected sha-256 hash")
	}
	wantDigest := mustHexDecode(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	if len(digest) != len(wantDigest) {
		t.Fatalf("digest length = %d, want %d", len(digest), len(wantDigest))
	}
	for i := range digest {
		if digest[i] != wantDigest[i] {
			t.Errorf("digest byte %d = 0x%02x, want 0x%02x", i, digest[i], wantDigest[i])
		}
	}
}

func TestParseV4MultipleHashes(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <hash type="sha-1">a9993e364706816aba3e25717850c26c9cd0d89d</hash>
    <hash type="sha-256">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	f := doc.Files[0]
	if len(f.Hashes) != 2 {
		t.Fatalf("expected 2 hashes, got %d", len(f.Hashes))
	}
	if _, ok := f.Hashes[hash.SHA1]; !ok {
		t.Error("expected sha-1 hash")
	}
	if _, ok := f.Hashes[hash.SHA256]; !ok {
		t.Error("expected sha-256 hash")
	}
}

func TestParseV4WithPieces(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <pieces length="262144" type="sha-256">
      <hash>ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
      <hash>ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a</hash>
    </pieces>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	f := doc.Files[0]
	if f.PieceLength != 262144 {
		t.Errorf("PieceLength = %d, want %d", f.PieceLength, 262144)
	}
	if f.PieceHashKind != hash.SHA256 {
		t.Errorf("PieceHashKind = %q, want %q", f.PieceHashKind, hash.SHA256)
	}
	if len(f.Pieces) != 2 {
		t.Fatalf("expected 2 pieces, got %d", len(f.Pieces))
	}
	wantPiece0 := mustHexDecode(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	wantPiece1 := mustHexDecode(t, "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a")
	compareBytes(t, f.Pieces[0], wantPiece0, "Pieces[0]")
	compareBytes(t, f.Pieces[1], wantPiece1, "Pieces[1]")
}

func TestParseV4PiecesKeepStrongestHashAlgorithm(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <pieces length="1024" type="sha-1">
      <hash>a9993e364706816aba3e25717850c26c9cd0d89d</hash>
    </pieces>
    <pieces length="2048" type="sha-256">
      <hash>ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
    </pieces>
    <pieces length="4096" type="md5">
      <hash>900150983cd24fb0d6963f7d28e17f72</hash>
    </pieces>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	f := doc.Files[0]
	if f.PieceHashKind != hash.SHA256 {
		t.Fatalf("PieceHashKind = %q, want %q", f.PieceHashKind, hash.SHA256)
	}
	if f.PieceLength != 2048 {
		t.Errorf("PieceLength = %d, want 2048", f.PieceLength)
	}
	if len(f.Pieces) != 1 {
		t.Fatalf("expected 1 sha-256 piece, got %d", len(f.Pieces))
	}
	want := mustHexDecode(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	compareBytes(t, f.Pieces[0], want, "Pieces[0]")
}

func TestParseV4MultipleFiles(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="file1.bin">
    <url>http://a.example.com/file1.bin</url>
  </file>
  <file name="file2.bin">
    <url>http://a.example.com/file2.bin</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	if len(doc.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(doc.Files))
	}
	if doc.Files[0].Name != "file1.bin" {
		t.Errorf("Files[0].Name = %q", doc.Files[0].Name)
	}
	if doc.Files[1].Name != "file2.bin" {
		t.Errorf("Files[1].Name = %q", doc.Files[1].Name)
	}
}

func TestParseV4WithMetaFields(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <version>1.2.3</version>
    <language>en</language>
    <os>linux-x86_64</os>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	f := doc.Files[0]
	if f.Version != "1.2.3" {
		t.Errorf("Version = %q", f.Version)
	}
	if len(f.Languages) != 1 || f.Languages[0] != "en" {
		t.Errorf("Languages = %v, want [en]", f.Languages)
	}
	if len(f.OSes) != 1 || f.OSes[0] != "linux-x86_64" {
		t.Errorf("OSes = %v, want [linux-x86_64]", f.OSes)
	}
}

func TestParseV3Basic(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <size>123456789</size>
      <resources>
        <url type="http" location="us" preference="100">http://mirror1.example.com/example.iso</url>
        <url type="ftp" location="eu" preference="50">ftp://mirror2.example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
	f := doc.Files[0]
	if f.Name != "example.iso" {
		t.Errorf("Name = %q, want %q", f.Name, "example.iso")
	}
	if f.Size != 123456789 {
		t.Errorf("Size = %d, want %d", f.Size, 123456789)
	}
	if !f.SizeKnown {
		t.Error("SizeKnown = false, want true")
	}
	if len(f.URLs) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(f.URLs))
	}

	// V3 preference 100 → priority 1 (101-100)
	u0 := f.URLs[0]
	if u0.URL != "http://mirror1.example.com/example.iso" {
		t.Errorf("URLs[0].URL = %q", u0.URL)
	}
	if u0.Priority != 1 {
		t.Errorf("URLs[0].Priority = %d, want %d", u0.Priority, 1)
	}
	if u0.Location != "us" {
		t.Errorf("URLs[0].Location = %q", u0.Location)
	}
	if u0.Type != "http" {
		t.Errorf("URLs[0].Type = %q", u0.Type)
	}

	// V3 preference 50 → priority 51 (101-50)
	u1 := f.URLs[1]
	if u1.URL != "ftp://mirror2.example.com/example.iso" {
		t.Errorf("URLs[1].URL = %q", u1.URL)
	}
	if u1.Priority != 51 {
		t.Errorf("URLs[1].Priority = %d, want %d", u1.Priority, 51)
	}
	if u1.Location != "eu" {
		t.Errorf("URLs[1].Location = %q", u1.Location)
	}
	if u1.Type != "ftp" {
		t.Errorf("URLs[1].Type = %q", u1.Type)
	}
}

func TestParseV3WithHash(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <verification>
        <hash type="sha-256">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
      </verification>
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	f := doc.Files[0]
	if len(f.Hashes) != 1 {
		t.Fatalf("expected 1 hash, got %d", len(f.Hashes))
	}
	digest, ok := f.Hashes[hash.SHA256]
	if !ok {
		t.Fatal("expected sha-256 hash")
	}
	wantDigest := mustHexDecode(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	compareBytes(t, digest, wantDigest, "digest")
}

func TestParseV3WithPieces(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <verification>
        <pieces length="262144" type="sha-256">
          <hash piece="0">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
          <hash piece="1">ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a</hash>
        </pieces>
      </verification>
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	f := doc.Files[0]
	if f.PieceLength != 262144 {
		t.Errorf("PieceLength = %d, want %d", f.PieceLength, 262144)
	}
	if f.PieceHashKind != hash.SHA256 {
		t.Errorf("PieceHashKind = %q, want %q", f.PieceHashKind, hash.SHA256)
	}
	if len(f.Pieces) != 2 {
		t.Fatalf("expected 2 pieces, got %d", len(f.Pieces))
	}
	wantPiece0 := mustHexDecode(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	wantPiece1 := mustHexDecode(t, "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a")
	compareBytes(t, f.Pieces[0], wantPiece0, "Pieces[0]")
	compareBytes(t, f.Pieces[1], wantPiece1, "Pieces[1]")
}

func TestParseV3PiecesKeepStrongestHashAlgorithm(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <verification>
        <pieces length="1024" type="sha-256">
          <hash piece="0">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
        </pieces>
        <pieces length="4096" type="md5">
          <hash piece="0">900150983cd24fb0d6963f7d28e17f72</hash>
        </pieces>
      </verification>
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	f := doc.Files[0]
	if f.PieceHashKind != hash.SHA256 {
		t.Fatalf("PieceHashKind = %q, want %q", f.PieceHashKind, hash.SHA256)
	}
	if f.PieceLength != 1024 {
		t.Errorf("PieceLength = %d, want 1024", f.PieceLength)
	}
	if len(f.Pieces) != 1 {
		t.Fatalf("expected 1 sha-256 piece, got %d", len(f.Pieces))
	}
	want := mustHexDecode(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	compareBytes(t, f.Pieces[0], want, "Pieces[0]")
}

func TestParseV3VersionLanguageOS(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <version>2.0</version>
      <language>en</language>
      <os>linux</os>
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	f := doc.Files[0]
	if f.Version != "2.0" {
		t.Errorf("Version = %q", f.Version)
	}
	if len(f.Languages) != 1 || f.Languages[0] != "en" {
		t.Errorf("Languages = %v, want [en]", f.Languages)
	}
	if len(f.OSes) != 1 || f.OSes[0] != "linux" {
		t.Errorf("OSes = %v, want [linux]", f.OSes)
	}
}

func TestParseAutoDetectV4(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
}

func TestParseAutoDetectV3(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
}

func TestParseV4InvalidHash(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <hash type="sha-256">zzz_invalid_hex_zzz</hash>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
	if len(doc.Files[0].Hashes) != 0 {
		t.Errorf("expected 0 hashes for invalid hex, got %d", len(doc.Files[0].Hashes))
	}
}

func TestParseV4UnsupportedHashType(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <hash type="sha3-256">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
	if len(doc.Files[0].Hashes) != 0 {
		t.Errorf("expected 0 hashes for unsupported type, got %d", len(doc.Files[0].Hashes))
	}
}

func TestParseV4DirTraversal(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="../etc/passwd">
    <url>http://example.com/file.bin</url>
  </file>
  <file name="safe.bin">
    <url>http://example.com/safe.bin</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file after dir traversal filtered, got %d", len(doc.Files))
	}
	if doc.Files[0].Name != "safe.bin" {
		t.Errorf("Name = %q", doc.Files[0].Name)
	}
}

func TestParseEmptyInput(t *testing.T) {
	_, err := metalink.Parse(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestParseMalformedXML(t *testing.T) {
	_, err := metalink.Parse(strings.NewReader("<metalink><unclosed>"))
	if err == nil {
		t.Fatal("expected error for malformed XML")
	}
}

func TestParseV4WithV3Input(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	_, err := metalink.ParseV4(strings.NewReader(xml))
	if err == nil {
		t.Fatal("expected error for V3 input to ParseV4")
	}
}

func TestParseV4V3WithoutName(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file>
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(doc.Files) != 0 {
		t.Fatalf("expected 0 files without name attr, got %d", len(doc.Files))
	}
}

func TestParseV3URLWithoutType(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <resources>
        <url preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
	if len(doc.Files[0].URLs) != 0 {
		t.Errorf("expected 0 URLs without type attr, got %d", len(doc.Files[0].URLs))
	}
}

func TestParseV4HashCaseInsensitiveType(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <hash type="SHA-256">ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad</hash>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	if len(doc.Files[0].Hashes) != 1 {
		t.Fatal("expected sha-256 hash with case-insensitive type")
	}
	if _, ok := doc.Files[0].Hashes[hash.SHA256]; !ok {
		t.Error("expected sha-256 hash")
	}
}

func TestParseV4BadPriorityDiscardsURL(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <url priority="0">http://bad.example.com/file.iso</url>
    <url priority="1000000">http://bad2.example.com/file.iso</url>
    <url priority="invalid">http://bad3.example.com/file.iso</url>
    <url priority="5">http://good.example.com/file.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
	if len(doc.Files[0].URLs) != 1 {
		t.Fatalf("expected 1 valid URL, got %d", len(doc.Files[0].URLs))
	}
	if doc.Files[0].URLs[0].URL != "http://good.example.com/file.iso" {
		t.Errorf("URL = %q", doc.Files[0].URLs[0].URL)
	}
	if doc.Files[0].URLs[0].Priority != 5 {
		t.Errorf("Priority = %d, want 5", doc.Files[0].URLs[0].Priority)
	}
}

func TestParseV4HashLengthValidation(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <hash type="sha-256">ba7816bf8f01cfea414140de5dae2223</hash>
    <hash type="sha-1">a9993e364706816aba3e25717850c26c9cd0d89d</hash>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	f := doc.Files[0]
	if len(f.Hashes) != 1 {
		t.Fatalf("expected 1 valid hash, got %d", len(f.Hashes))
	}
	if _, ok := f.Hashes[hash.SHA1]; !ok {
		t.Error("expected sha-1 hash (sha-256 was wrong length)")
	}
}

func TestParseV4MultipleLanguages(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="example.iso">
    <language>en</language>
    <language>ja</language>
    <os>linux</os>
    <os>windows</os>
    <url>http://example.com/example.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	f := doc.Files[0]
	if len(f.Languages) != 2 || f.Languages[0] != "en" || f.Languages[1] != "ja" {
		t.Errorf("Languages = %v, want [en ja]", f.Languages)
	}
	if len(f.OSes) != 2 || f.OSes[0] != "linux" || f.OSes[1] != "windows" {
		t.Errorf("OSes = %v, want [linux windows]", f.OSes)
	}
}

func TestParseV3MultipleLanguages(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <language>en</language>
      <language>ja</language>
      <language>de</language>
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	f := doc.Files[0]
	if len(f.Languages) != 3 {
		t.Fatalf("expected 3 languages, got %d", len(f.Languages))
	}
	if f.Languages[0] != "en" || f.Languages[1] != "ja" || f.Languages[2] != "de" {
		t.Errorf("Languages = %v, want [en ja de]", f.Languages)
	}
}

func TestParseBasicMeta4Testdata(t *testing.T) {
	data, err := os.ReadFile("testdata/basic.meta4")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	doc, err := metalink.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(doc.Files))
	}
	f := doc.Files[0]
	if f.Name != "example.iso" {
		t.Errorf("Name = %q", f.Name)
	}
	if f.Size != 1048576 {
		t.Errorf("Size = %d", f.Size)
	}
	if f.Version != "1.0" {
		t.Errorf("Version = %q", f.Version)
	}
	if len(f.Languages) != 1 || f.Languages[0] != "en" {
		t.Errorf("Languages = %v", f.Languages)
	}
	if len(f.OSes) != 1 || f.OSes[0] != "linux" {
		t.Errorf("OSes = %v", f.OSes)
	}
	if len(f.URLs) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(f.URLs))
	}
	if f.URLs[0].URL != "https://mirror1.example.com/example.iso" {
		t.Errorf("URLs[0].URL = %q", f.URLs[0].URL)
	}
	if f.URLs[0].Priority != 1 {
		t.Errorf("URLs[0].Priority = %d", f.URLs[0].Priority)
	}
	if f.URLs[1].URL != "https://mirror2.example.com/example.iso" {
		t.Errorf("URLs[1].URL = %q", f.URLs[1].URL)
	}
	if f.URLs[1].Priority != 2 {
		t.Errorf("URLs[1].Priority = %d", f.URLs[1].Priority)
	}
	if len(f.Hashes) != 1 {
		t.Fatalf("expected 1 hash, got %d", len(f.Hashes))
	}
	digest, ok := f.Hashes[hash.SHA256]
	if !ok {
		t.Fatal("expected sha-256 hash")
	}
	wantDigest := mustHexDecode(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824")
	compareBytes(t, digest, wantDigest, "digest")
}

func TestParseV4InvalidSizeCancelsEntry(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="urn:ietf:params:xml:ns:metalink">
  <file name="bad.iso">
    <size>not_a_number</size>
    <url>http://example.com/bad.iso</url>
  </file>
  <file name="good.iso">
    <size>100</size>
    <url>http://example.com/good.iso</url>
  </file>
</metalink>`

	doc, err := metalink.ParseV4(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("ParseV4 error: %v", err)
	}
	if len(doc.Files) != 1 {
		t.Fatalf("expected 1 file (invalid size cancels entry), got %d", len(doc.Files))
	}
	if doc.Files[0].Name != "good.iso" {
		t.Errorf("Name = %q, want good.iso", doc.Files[0].Name)
	}
	if doc.Files[0].Size != 100 {
		t.Errorf("Size = %d, want 100", doc.Files[0].Size)
	}
}

func TestParseV3MsgCheckLengthValidation(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<metalink xmlns="http://www.metalinker.org/">
  <files>
    <file name="example.iso">
      <verification>
        <hash type="sha-256">too_short</hash>
        <hash type="sha-1">a9993e364706816aba3e25717850c26c9cd0d89d</hash>
      </verification>
      <resources>
        <url type="http" preference="100">http://example.com/example.iso</url>
      </resources>
    </file>
  </files>
</metalink>`

	doc, err := metalink.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	f := doc.Files[0]
	if len(f.Hashes) != 1 {
		t.Fatalf("expected 1 valid hash, got %d", len(f.Hashes))
	}
	if _, ok := f.Hashes[hash.SHA1]; !ok {
		t.Error("expected sha-1 hash (sha-256 was wrong length)")
	}
}

func compareBytes(t *testing.T, got, want []byte, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d", label, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s byte %d = 0x%02x, want 0x%02x", label, i, got[i], want[i])
		}
	}
}

func mustHexDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("invalid hex string %q: %v", s, err)
	}
	return b
}
