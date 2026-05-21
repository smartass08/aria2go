package progress

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

func makeInfoHash(i byte) []byte {
	h := make([]byte, infoHashLength)
	for j := range h {
		h[j] = i
	}
	return h
}

func makeBitfield(size int, v byte) []byte {
	b := make([]byte, size)
	for j := range b {
		b[j] = v
	}
	return b
}

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test")
}

// TestSaveBT saves a full BT progress file and loads it back, verifying all fields.
func TestSaveBT(t *testing.T) {
	info := &Info{
		InfoHash:     makeInfoHash(0xAB),
		PieceLength:  262144,
		TotalLength:  104857600,
		UploadLength: 524288,
		Bitfield:     makeBitfield(50, 0xFF),
		InFlight: []InFlightPiece{
			{Index: 10, Length: 65536, Bitfield: makeBitfield(8, 0xAA)},
			{Index: 20, Length: 32768, Bitfield: makeBitfield(4, 0xBB)},
		},
	}

	path := tempPath(t)
	if err := Save(path, info); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !bytes.Equal(loaded.InfoHash, info.InfoHash) {
		t.Errorf("InfoHash = %x, want %x", loaded.InfoHash, info.InfoHash)
	}
	if loaded.PieceLength != info.PieceLength {
		t.Errorf("PieceLength = %d, want %d", loaded.PieceLength, info.PieceLength)
	}
	if loaded.TotalLength != info.TotalLength {
		t.Errorf("TotalLength = %d, want %d", loaded.TotalLength, info.TotalLength)
	}
	if loaded.UploadLength != info.UploadLength {
		t.Errorf("UploadLength = %d, want %d", loaded.UploadLength, info.UploadLength)
	}
	if !bytes.Equal(loaded.Bitfield, info.Bitfield) {
		t.Errorf("Bitfield = %x, want %x", loaded.Bitfield, info.Bitfield)
	}
	if len(loaded.InFlight) != len(info.InFlight) {
		t.Fatalf("len(InFlight) = %d, want %d", len(loaded.InFlight), len(info.InFlight))
	}
	for i, p := range info.InFlight {
		lp := loaded.InFlight[i]
		if lp.Index != p.Index {
			t.Errorf("InFlight[%d].Index = %d, want %d", i, lp.Index, p.Index)
		}
		if lp.Length != p.Length {
			t.Errorf("InFlight[%d].Length = %d, want %d", i, lp.Length, p.Length)
		}
		if !bytes.Equal(lp.Bitfield, p.Bitfield) {
			t.Errorf("InFlight[%d].Bitfield = %x, want %x", i, lp.Bitfield, p.Bitfield)
		}
	}

	// Verify the file exists at the right path.
	fi, err := os.Stat(path + Suffix)
	if err != nil {
		t.Errorf("expected file at %s: %v", path+Suffix, err)
	}
	if fi != nil && fi.IsDir() {
		t.Error("expected regular file, got directory")
	}
}

// TestLoadCompatV0001 manually creates a v0001-format binary file and loads it.
func TestLoadCompatV0001(t *testing.T) {
	infoHash := makeInfoHash(0xCC)
	bitfield := makeBitfield(16, 0x0F)

	var buf bytes.Buffer

	// Version (2 bytes) — v0001.
	buf.Write([]byte{0x00, 0x01})

	// Extension (4 bytes) — BT flag.
	buf.Write([]byte{0x00, 0x00, 0x00, 0x01})

	// Info hash length (4 bytes) = 20.
	writeBE32(&buf, infoHashLength)

	// Info hash (20 bytes).
	buf.Write(infoHash)

	// Piece length (4 bytes) = 1048576.
	writeBE32(&buf, 1048576)

	// Total length (8 bytes) = 52428800.
	writeBE64(&buf, 52428800)

	// Upload length (8 bytes) = 1024.
	writeBE64(&buf, 1024)

	// Bitfield length (4 bytes) = 16.
	writeBE32(&buf, 16)

	// Bitfield (16 bytes).
	buf.Write(bitfield)

	// Num in-flight (4 bytes) = 1.
	writeBE32(&buf, 1)

	// In-flight piece 0.
	writeBE32(&buf, 5)     // index = 5
	writeBE32(&buf, 65536) // length = 65536
	writeBE32(&buf, 2)     // bitfield length = 2
	buf.Write([]byte{0xDE, 0xAD})

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !bytes.Equal(loaded.InfoHash, infoHash) {
		t.Errorf("InfoHash = %x, want %x", loaded.InfoHash, infoHash)
	}
	if loaded.PieceLength != 1048576 {
		t.Errorf("PieceLength = %d, want 1048576", loaded.PieceLength)
	}
	if loaded.TotalLength != 52428800 {
		t.Errorf("TotalLength = %d, want 52428800", loaded.TotalLength)
	}
	if loaded.UploadLength != 1024 {
		t.Errorf("UploadLength = %d, want 1024", loaded.UploadLength)
	}
	if !bytes.Equal(loaded.Bitfield, bitfield) {
		t.Errorf("Bitfield = %x, want %x", loaded.Bitfield, bitfield)
	}
	if len(loaded.InFlight) != 1 {
		t.Fatalf("len(InFlight) = %d, want 1", len(loaded.InFlight))
	}
	if loaded.InFlight[0].Index != 5 {
		t.Errorf("InFlight[0].Index = %d, want 5", loaded.InFlight[0].Index)
	}
	if loaded.InFlight[0].Length != 65536 {
		t.Errorf("InFlight[0].Length = %d, want 65536", loaded.InFlight[0].Length)
	}
	if !bytes.Equal(loaded.InFlight[0].Bitfield, []byte{0xDE, 0xAD}) {
		t.Errorf("InFlight[0].Bitfield = %x, want {DE, AD}", loaded.InFlight[0].Bitfield)
	}
}

// TestSaveNonBT saves a non-BT progress file and verifies the extension flag is 0.
func TestSaveNonBT(t *testing.T) {
	info := &Info{
		InfoHash:     nil,
		PieceLength:  512 * 1024,
		TotalLength:  1024 * 1024,
		UploadLength: 0,
		Bitfield:     makeBitfield(1, 0x01),
		InFlight:     []InFlightPiece{},
	}

	path := tempPath(t)
	if err := Save(path, info); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.InfoHash != nil {
		t.Errorf("InfoHash = %x, want nil", loaded.InfoHash)
	}
	if loaded.PieceLength != 512*1024 {
		t.Errorf("PieceLength = %d, want %d", loaded.PieceLength, 512*1024)
	}
	if loaded.TotalLength != 1024*1024 {
		t.Errorf("TotalLength = %d, want %d", loaded.TotalLength, 1024*1024)
	}
	if loaded.UploadLength != 0 {
		t.Errorf("UploadLength = %d, want 0", loaded.UploadLength)
	}
	if !bytes.Equal(loaded.Bitfield, []byte{0x01}) {
		t.Errorf("Bitfield = %x, want [01]", loaded.Bitfield)
	}
	if len(loaded.InFlight) != 0 {
		t.Errorf("len(InFlight) = %d, want 0", len(loaded.InFlight))
	}
}

// TestLoadNonBTCompat manually creates a v0001 non-BT file and loads it.
func TestLoadNonBTCompat(t *testing.T) {
	var buf bytes.Buffer

	// Version (2 bytes) — v0001.
	buf.Write([]byte{0x00, 0x01})

	// Extension (4 bytes) — non-BT (all zeros).
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00})

	// Info hash length (4 bytes) = 0.
	writeBE32(&buf, 0)

	// Piece length (4 bytes) = 4194304 (4 MiB, typical FTP/HTTP piece).
	writeBE32(&buf, 4194304)

	// Total length (8 bytes) = 1048576.
	writeBE64(&buf, 1048576)

	// Upload length (8 bytes) = 0.
	writeBE64(&buf, 0)

	// Bitfield length (4 bytes) = 2.
	writeBE32(&buf, 2)

	// Bitfield (2 bytes).
	buf.Write([]byte{0x03, 0x00})

	// Num in-flight (4 bytes) = 0.
	writeBE32(&buf, 0)

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.InfoHash != nil {
		t.Errorf("InfoHash = %x, want nil", loaded.InfoHash)
	}
	if loaded.PieceLength != 4194304 {
		t.Errorf("PieceLength = %d, want 4194304", loaded.PieceLength)
	}
	if loaded.TotalLength != 1048576 {
		t.Errorf("TotalLength = %d, want 1048576", loaded.TotalLength)
	}
	if loaded.UploadLength != 0 {
		t.Errorf("UploadLength = %d, want 0", loaded.UploadLength)
	}
	if !bytes.Equal(loaded.Bitfield, []byte{0x03, 0x00}) {
		t.Errorf("Bitfield mismatch")
	}
	if len(loaded.InFlight) != 0 {
		t.Errorf("len(InFlight) = %d, want 0", len(loaded.InFlight))
	}
}

// TestV0000Compat creates a v0000-format file (host byte order) and loads it.
func TestV0000Compat(t *testing.T) {
	infoHash := makeInfoHash(0x99)
	bitfield := makeBitfield(4, 0xAA)

	var buf bytes.Buffer

	// Version (2 bytes) — v0000 {0x00, 0x00}.
	buf.Write([]byte{0x00, 0x00})

	// Extension (4 bytes) — BT.
	writeNative32(&buf, 1)

	// Info hash length (4 bytes) = 20.
	writeNative32(&buf, infoHashLength)

	// Info hash (20 bytes).
	buf.Write(infoHash)

	// Piece length (4 bytes) = 262144.
	writeNative32(&buf, 262144)

	// Total length (8 bytes) = 2097152.
	writeNative64(&buf, 2097152)

	// Upload length (8 bytes) = 0.
	writeNative64(&buf, 0)

	// Bitfield length (4 bytes) = 4.
	writeNative32(&buf, 4)

	// Bitfield (4 bytes).
	buf.Write(bitfield)

	// Num in-flight (4 bytes).
	writeNative32(&buf, 0)

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !bytes.Equal(loaded.InfoHash, infoHash) {
		t.Errorf("InfoHash = %x, want %x", loaded.InfoHash, infoHash)
	}
	if loaded.PieceLength != 262144 {
		t.Errorf("PieceLength = %d, want 262144", loaded.PieceLength)
	}
	if loaded.TotalLength != 2097152 {
		t.Errorf("TotalLength = %d, want 2097152", loaded.TotalLength)
	}
	if !bytes.Equal(loaded.Bitfield, bitfield) {
		t.Errorf("Bitfield mismatch")
	}
	if len(loaded.InFlight) != 0 {
		t.Errorf("len(InFlight) = %d, want 0", len(loaded.InFlight))
	}
}

// TestV0000CompatWithInFlight creates a v0000-format file with in-flight pieces,
// then verifies Load correctly parses host byte order data.
func TestV0000CompatInFlight(t *testing.T) {
	var buf bytes.Buffer

	// Version = v0000.
	buf.Write([]byte{0x00, 0x00})

	// Extension — non-BT (all zeros).
	writeNative32(&buf, 0)

	// Info hash length = 0.
	writeNative32(&buf, 0)

	// Piece length.
	writeNative32(&buf, 512*1024)

	// Total length.
	writeNative64(&buf, 10*1024*1024)

	// Upload length.
	writeNative64(&buf, 0)

	// Bitfield length.
	writeNative32(&buf, 3)
	buf.Write([]byte{0x80, 0x00, 0x00})

	// Num in-flight = 2.
	writeNative32(&buf, 2)

	// Piece 0.
	writeNative32(&buf, 0)     // index
	writeNative32(&buf, 65536) // length
	writeNative32(&buf, 1)     // bitfield length
	buf.Write([]byte{0xFF})

	// Piece 1.
	writeNative32(&buf, 7)      // index
	writeNative32(&buf, 262144) // length
	writeNative32(&buf, 2)      // bitfield length
	buf.Write([]byte{0x55, 0xAA})

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.PieceLength != 512*1024 {
		t.Errorf("PieceLength = %d, want %d", loaded.PieceLength, 512*1024)
	}
	if loaded.TotalLength != 10*1024*1024 {
		t.Errorf("TotalLength = %d, want %d", loaded.TotalLength, 10*1024*1024)
	}
	if !bytes.Equal(loaded.Bitfield, []byte{0x80, 0x00, 0x00}) {
		t.Errorf("Bitfield mismatch")
	}
	if len(loaded.InFlight) != 2 {
		t.Fatalf("len(InFlight) = %d, want 2", len(loaded.InFlight))
	}
	if loaded.InFlight[0].Index != 0 || loaded.InFlight[0].Length != 65536 {
		t.Errorf("InFlight[0] mismatch: %+v", loaded.InFlight[0])
	}
	if !bytes.Equal(loaded.InFlight[0].Bitfield, []byte{0xFF}) {
		t.Errorf("InFlight[0] bitfield mismatch")
	}
	if loaded.InFlight[1].Index != 7 || loaded.InFlight[1].Length != 262144 {
		t.Errorf("InFlight[1] mismatch: %+v", loaded.InFlight[1])
	}
	if !bytes.Equal(loaded.InFlight[1].Bitfield, []byte{0x55, 0xAA}) {
		t.Errorf("InFlight[1] bitfield mismatch")
	}
}

// TestPieceLengthMismatch verifies that loading a file with a different piece
// length than expected does not corrupt the data — the loaded data reflects
// what was on disk. (Reshuffling is a higher-level concern.)
func TestPieceLengthMismatch(t *testing.T) {
	info := &Info{
		InfoHash:     makeInfoHash(0x11),
		PieceLength:  256 * 1024,
		TotalLength:  10 * 1024 * 1024,
		UploadLength: 0,
		Bitfield:     makeBitfield(5, 0xFF),
		InFlight: []InFlightPiece{
			{Index: 3, Length: 128 * 1024, Bitfield: makeBitfield(2, 0x01)},
		},
	}

	path := tempPath(t)
	if err := Save(path, info); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify load gives back the same piece length.
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.PieceLength != 256*1024 {
		t.Errorf("PieceLength = %d, want %d", loaded.PieceLength, 256*1024)
	}
	if len(loaded.InFlight) != 1 {
		t.Fatal("expected 1 in-flight piece")
	}
	if loaded.InFlight[0].Index != 3 {
		t.Errorf("InFlight[0].Index = %d, want 3", loaded.InFlight[0].Index)
	}

	// Now load a file with a very different piece length.
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x01})             // v0001
	buf.Write([]byte{0x00, 0x00, 0x00, 0x01}) // BT extension
	writeBE32(&buf, uint32(len(info.InfoHash)))
	buf.Write(info.InfoHash)
	writeBE32(&buf, 128*1024) // piece length = 128K (different from 256K)
	writeBE64(&buf, uint64(info.TotalLength))
	writeBE64(&buf, uint64(info.UploadLength))
	writeBE32(&buf, uint32(len(info.Bitfield)))
	buf.Write(info.Bitfield)
	writeBE32(&buf, 0) // no in-flight

	path2 := tempPath(t)
	if err := os.WriteFile(path2+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	loaded2, err := Load(path2)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded2.PieceLength != 128*1024 {
		t.Errorf("PieceLength = %d, want %d", loaded2.PieceLength, 128*1024)
	}
}

// TestFilenameUpdate verifies Save/Load work with different path values.
func TestFilenameUpdate(t *testing.T) {
	info := &Info{
		InfoHash:    makeInfoHash(0x22),
		PieceLength: 1024,
		TotalLength: 4096,
		Bitfield:    makeBitfield(1, 0x01),
	}

	paths := []string{
		filepath.Join(t.TempDir(), "dl1"),
		filepath.Join(t.TempDir(), "sub", "dl2"),
	}

	if err := os.MkdirAll(filepath.Dir(paths[1]), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, p := range paths {
		if err := Save(p, info); err != nil {
			t.Fatalf("Save(%q) error = %v", p, err)
		}
		if _, err := os.Stat(p + Suffix); err != nil {
			t.Errorf("expected file at %s: %v", p+Suffix, err)
		}

		loaded, err := Load(p)
		if err != nil {
			t.Fatalf("Load(%q) error = %v", p, err)
		}
		if loaded.PieceLength != 1024 {
			t.Errorf("PieceLength = %d, want 1024", loaded.PieceLength)
		}
	}
}

// TestEmptyBitfield verifies an empty bitfield round-trips correctly.
func TestEmptyBitfield(t *testing.T) {
	info := &Info{
		PieceLength: 1024,
		TotalLength: 1024,
		Bitfield:    []byte{},
		InFlight:    []InFlightPiece{},
	}

	path := tempPath(t)
	if err := Save(path, info); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded.Bitfield) != 0 {
		t.Errorf("Bitfield = %x, want empty", loaded.Bitfield)
	}
}

// TestNoInFlightPieces verifies zero in-flight pieces round-trips.
func TestNoInFlightPieces(t *testing.T) {
	info := &Info{
		InfoHash:    makeInfoHash(0xAA),
		PieceLength: 512 * 1024,
		TotalLength: 10 * 1024 * 1024,
		Bitfield:    makeBitfield(4, 0xFF),
		InFlight:    nil,
	}

	path := tempPath(t)
	if err := Save(path, info); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded.InFlight) != 0 {
		t.Errorf("len(InFlight) = %d, want 0", len(loaded.InFlight))
	}
}

// TestBadVersion tests loading a file with an unsupported version.
func TestBadVersion(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0xDE, 0xAD}) // invalid version

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for bad version")
	}
	if !errors.Is(err, ErrBadVersion) {
		t.Errorf("expected ErrBadVersion, got %v", err)
	}
}

// TestTruncatedFile tests loading a truncated file.
func TestTruncatedFile(t *testing.T) {
	// File with only version bytes and nothing else.
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x01})

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for truncated file")
	}
}

// TestZeroPieceLength tests loading a file with piece length = 0.
func TestZeroPieceLength(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x01})             // v0001
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00}) // non-BT
	writeBE32(&buf, 0)                        // info hash length = 0
	writeBE32(&buf, 0)                        // piece length = 0 (invalid)

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for zero piece length")
	}
	if !errors.Is(err, ErrZeroPieceLength) {
		t.Errorf("expected ErrZeroPieceLength, got %v", err)
	}
}

// TestBadInfoHashLength verifies info hash lengths > 20 are rejected.
func TestBadInfoHashLength(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x01})             // v0001
	buf.Write([]byte{0x00, 0x00, 0x00, 0x00}) // non-BT
	writeBE32(&buf, 30)                       // info hash length = 30 (invalid)
	writeBE32(&buf, 1024)
	writeBE64(&buf, 1024)
	writeBE64(&buf, 0)
	writeBE32(&buf, 0)
	writeBE32(&buf, 0)

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for bad info hash length")
	}
	if !errors.Is(err, ErrBadInfoHash) {
		t.Errorf("expected ErrBadInfoHash, got %v", err)
	}
}

// TestSaveOverwrite verifies that saving twice overwrites the file.
func TestSaveOverwrite(t *testing.T) {
	path := tempPath(t)

	info1 := &Info{
		InfoHash:    makeInfoHash(0x11),
		PieceLength: 1024,
		TotalLength: 2048,
	}
	if err := Save(path, info1); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info2 := &Info{
		InfoHash:    makeInfoHash(0x22),
		PieceLength: 2048,
		TotalLength: 4096,
	}
	if err := Save(path, info2); err != nil {
		t.Fatalf("Save() (overwrite) error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.PieceLength != 2048 {
		t.Errorf("PieceLength = %d, want 2048", loaded.PieceLength)
	}
	if loaded.TotalLength != 4096 {
		t.Errorf("TotalLength = %d, want 4096", loaded.TotalLength)
	}
}

// TestSaveWithBadPieceLength ensures Save rejects out-of-range piece lengths.
func TestSaveWithBadPieceLength(t *testing.T) {
	path := tempPath(t)

	// Zero piece length.
	info := &Info{PieceLength: 0, TotalLength: 1024}
	if err := Save(path, info); err == nil {
		t.Error("expected error for zero piece length")
	}

	// Negative piece length.
	info2 := &Info{PieceLength: -1, TotalLength: 1024}
	if err := Save(path, info2); err == nil {
		t.Error("expected error for negative piece length")
	}
}

// TestLoadFileNotFound tests loading a non-existent file.
func TestLoadFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/no/file/test")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	var ce *core.Error
	if !errors.As(err, &ce) || ce.Code != core.ExitFileIOError {
		t.Errorf("expected ExitFileIOError, got %v", err)
	}
}

// TestSaveMultipleInFlight verifies >1 in-flight piece serialization.
func TestSaveMultipleInFlight(t *testing.T) {
	info := &Info{
		PieceLength: 1024,
		TotalLength: 10240,
		Bitfield:    makeBitfield(2, 0xff),
		InFlight: []InFlightPiece{
			{Index: 0, Length: 512, Bitfield: []byte{0x01}},
			{Index: 1, Length: 512, Bitfield: []byte{0x02}},
			{Index: 2, Length: 256, Bitfield: []byte{0x03}},
		},
	}

	path := tempPath(t)
	if err := Save(path, info); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded.InFlight) != 3 {
		t.Fatalf("len(InFlight) = %d, want 3", len(loaded.InFlight))
	}
	for i := range info.InFlight {
		if loaded.InFlight[i].Index != info.InFlight[i].Index {
			t.Errorf("InFlight[%d].Index = %d, want %d", i, loaded.InFlight[i].Index, info.InFlight[i].Index)
		}
		if loaded.InFlight[i].Length != info.InFlight[i].Length {
			t.Errorf("InFlight[%d].Length = %d, want %d", i, loaded.InFlight[i].Length, info.InFlight[i].Length)
		}
		if !bytes.Equal(loaded.InFlight[i].Bitfield, info.InFlight[i].Bitfield) {
			t.Errorf("InFlight[%d].Bitfield mismatch", i)
		}
	}
}

// TestSaveLargeUploadLength verifies large positive upload length round-trips.
func TestSaveLargeUploadLength(t *testing.T) {
	const want int64 = 1 << 60
	info := &Info{
		PieceLength:  1024,
		TotalLength:  1024,
		UploadLength: want,
		Bitfield:     makeBitfield(1, 0x01),
	}

	path := tempPath(t)
	if err := Save(path, info); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.UploadLength != want {
		t.Errorf("UploadLength = %d, want %d", loaded.UploadLength, want)
	}
}

// TestV0000NonBTCompat tests loading a v0000 non-BT file.
func TestV0000NonBTCompat(t *testing.T) {
	bitfield := []byte{0x0F, 0xF0}

	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x00}) // v0000
	writeNative32(&buf, 0)        // extension = 0 (non-BT)
	writeNative32(&buf, 0)        // info hash length = 0
	writeNative32(&buf, 1048576)  // piece length
	writeNative64(&buf, 4194304)  // total length
	writeNative64(&buf, 1024)     // upload length
	writeNative32(&buf, uint32(len(bitfield)))
	buf.Write(bitfield)
	writeNative32(&buf, 0) // no in-flight

	path := tempPath(t)
	if err := os.WriteFile(path+Suffix, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.InfoHash != nil {
		t.Errorf("InfoHash = %x, want nil", loaded.InfoHash)
	}
	if loaded.PieceLength != 1048576 {
		t.Errorf("PieceLength = %d, want 1048576", loaded.PieceLength)
	}
	if loaded.TotalLength != 4194304 {
		t.Errorf("TotalLength = %d, want 4194304", loaded.TotalLength)
	}
	if loaded.UploadLength != 1024 {
		t.Errorf("UploadLength = %d, want 1024", loaded.UploadLength)
	}
	if !bytes.Equal(loaded.Bitfield, bitfield) {
		t.Errorf("Bitfield mismatch")
	}
}

// --- helpers ---

func writeBE32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

func writeBE64(buf *bytes.Buffer, v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	buf.Write(b[:])
}

func writeNative32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.NativeEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

func writeNative64(buf *bytes.Buffer, v uint64) {
	var b [8]byte
	binary.NativeEndian.PutUint64(b[:], v)
	buf.Write(b[:])
}
