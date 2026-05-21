package disk

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeMultiFile(files []FileEntry) (*MultiFile, string) {
	dir, err := os.MkdirTemp("", "aria2go-multifile-*")
	if err != nil {
		panic(err)
	}
	mf, err := NewMultiFile(dir, files, 1024, AllocatorNone{})
	if err != nil {
		os.RemoveAll(dir)
		panic(err)
	}
	return mf, dir
}

func TestNewMultiFileRejectsEmptyDir(t *testing.T) {
	_, err := NewMultiFile("", []FileEntry{{Name: "a", Length: 100, Offset: 0}}, 1024, AllocatorNone{})
	if err == nil {
		t.Error("expected error for empty dir")
	}
}

func TestNewMultiFileRejectsNoFiles(t *testing.T) {
	_, err := NewMultiFile("/tmp", nil, 1024, AllocatorNone{})
	if err == nil {
		t.Error("expected error for no files")
	}
}

func TestNewMultiFileRejectsUnsortedFiles(t *testing.T) {
	files := []FileEntry{
		{Name: "b", Length: 100, Offset: 100},
		{Name: "a", Length: 100, Offset: 0},
	}
	_, err := NewMultiFile("/tmp", files, 1024, AllocatorNone{})
	if err == nil {
		t.Error("expected error for unsorted files")
	}
}

func TestNewMultiFileRejectsOverlappingFiles(t *testing.T) {
	files := []FileEntry{
		{Name: "a", Length: 100, Offset: 0},
		{Name: "b", Length: 100, Offset: 50},
	}
	_, err := NewMultiFile("/tmp", files, 1024, AllocatorNone{})
	if err == nil {
		t.Error("expected error for overlapping files")
	}
}

func TestNewMultiFileTotalSize(t *testing.T) {
	files := []FileEntry{
		{Name: "a", Length: 100, Offset: 0},
		{Name: "b", Length: 200, Offset: 100},
		{Name: "c", Length: 300, Offset: 300},
	}
	mf, err := NewMultiFile("/tmp", files, 1024, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	if mf.Size() != 600 {
		t.Errorf("Size() = %d, want 600", mf.Size())
	}
}

func TestMultiFileOpenForWriteCreatesFiles(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
		{Name: "b.txt", Length: 200, Offset: 100},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatalf("OpenForWrite failed: %v", err)
	}

	for _, fe := range files {
		path := filepath.Join(dir, fe.Name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("file %q does not exist: %v", path, err)
		}
	}
}

func TestMultiFileOpenForWriteIdempotent(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatalf("first OpenForWrite: %v", err)
	}
	if err := mf.OpenForWrite(); err != nil {
		t.Fatalf("second OpenForWrite: %v", err)
	}
}

func TestMultiFileOpenForWriteCreatesSubdirs(t *testing.T) {
	files := []FileEntry{
		{Name: filepath.Join("sub", "deep", "f.txt"), Length: 50, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatalf("OpenForWrite failed: %v", err)
	}

	path := filepath.Join(dir, files[0].Name)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("nested file does not exist: %v", err)
	}
}

func TestMultiFileWriteAtSingleFile(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 1024, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	data := []byte("hello world")
	n, err := mf.WriteAt(data, 0)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("wrote %d bytes, want %d", n, len(data))
	}

	buf := make([]byte, len(data))
	n, err = mf.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if string(buf[:n]) != "hello world" {
		t.Errorf("read %q, want %q", string(buf[:n]), "hello world")
	}
}

func TestMultiFileWriteAtCrossBoundary(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
		{Name: "b.txt", Length: 20, Offset: 10},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	// Write 20 bytes starting at offset 5 (5 bytes to a.txt, 15 to b.txt)
	data := make([]byte, 20)
	for i := range data {
		data[i] = byte(i)
	}
	n, err := mf.WriteAt(data, 5)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}
	if n != 20 {
		t.Errorf("wrote %d bytes, want 20", n)
	}

	// Read back from a.txt
	bufA := make([]byte, 10)
	n, err = mf.ReadAt(bufA, 0)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	// Bytes [5..10) should be data[0:5]
	for i := 5; i < 10; i++ {
		if bufA[i] != byte(i-5) {
			t.Errorf("a.txt[%d] = %d, want %d", i, bufA[i], i-5)
		}
	}

	// Read back from b.txt
	bufB := make([]byte, 20)
	n, err = mf.ReadAt(bufB, 5)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if bufB[i] != byte(i) {
			t.Errorf("b.txt[%d] = %d, want %d", i, bufB[i], i)
		}
	}
}

func TestMultiFileReadAtAcrossBoundary(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
		{Name: "b.txt", Length: 10, Offset: 10},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	// Write to both files
	mf.WriteAt([]byte("aaaaaaaaaa"), 0)
	mf.WriteAt([]byte("bbbbbbbbbb"), 10)

	// Read across boundary: offset 5, 10 bytes (5 from a, 5 from b)
	buf := make([]byte, 10)
	n, err := mf.ReadAt(buf, 5)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	want := "aaaaabbbbb"
	if string(buf[:n]) != want {
		t.Errorf("cross-boundary read = %q, want %q", string(buf[:n]), want)
	}
}

func TestMultiFileWriteAtInvalidOffset(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	_, err := mf.WriteAt([]byte("x"), 200)
	if err == nil {
		t.Error("expected error for write beyond total size")
	}
}

func TestMultiFileReadAtEOF(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 5, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	mf.WriteAt([]byte("hello"), 0)

	buf := make([]byte, 10)
	n, err := mf.ReadAt(buf, 0)
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("read %q, want %q", string(buf[:n]), "hello")
	}
}

func TestMultiFileReadAtBeyondSize(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 10)
	_, err := mf.ReadAt(buf, 20)
	if err != io.EOF {
		t.Errorf("expected io.EOF for read beyond total size, got %v", err)
	}
}

func TestMultiFileClose(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	if err := mf.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Operations after close must return ErrFileClosed
	_, err := mf.WriteAt([]byte("x"), 0)
	if err != ErrFileClosed {
		t.Errorf("WriteAt after Close: got %v, want ErrFileClosed", err)
	}

	_, err = mf.ReadAt([]byte{0}, 0)
	if err != ErrFileClosed {
		t.Errorf("ReadAt after Close: got %v, want ErrFileClosed", err)
	}
}

func TestMultiFileCloseIdempotent(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	if err := mf.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mf.Close(); err != nil {
		t.Errorf("second Close returned error: %v", err)
	}
}

func TestMultiFileSync(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
		{Name: "b.txt", Length: 200, Offset: 100},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	mf.WriteAt([]byte("data"), 0)
	if err := mf.Sync(); err != nil {
		t.Errorf("Sync failed: %v", err)
	}
}

func TestMultiFilePieceTracking(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	// Set 8 pieces
	mf.SetPieceCount(8)

	for i := 0; i < 8; i++ {
		if mf.Have(i) {
			t.Errorf("piece %d should not be marked complete yet", i)
		}
	}

	// Mark some pieces
	mf.MarkPiece(0, true)
	mf.MarkPiece(2, true)
	mf.MarkPiece(7, true)

	if !mf.Have(0) {
		t.Error("piece 0 should be marked complete")
	}
	if !mf.Have(2) {
		t.Error("piece 2 should be marked complete")
	}
	if !mf.Have(7) {
		t.Error("piece 7 should be marked complete")
	}
	if mf.Have(1) {
		t.Error("piece 1 should not be marked complete")
	}

	missing := mf.Missing()
	if len(missing) != 5 {
		t.Errorf("Missing() = %v, want 5 items", missing)
	}

	// Verify bitfield (MSB-first, 8 pieces = 1 byte)
	// Pieces 0, 2, 7 set: bits 7, 5, 0 -> 10100001 = 0xa1
	bf := mf.Bitfield()
	if len(bf) != 1 {
		t.Fatalf("Bitfield() length = %d, want 1", len(bf))
	}
	if bf[0] != 0xa1 {
		t.Errorf("Bitfield()[0] = 0x%02x, want 0xa1", bf[0])
	}

	// Unmark piece 0
	mf.MarkPiece(0, false)
	if mf.Have(0) {
		t.Error("piece 0 should be cleared")
	}
}

func TestMultiFileSetPieceCountReinit(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	mf.SetPieceCount(4)
	mf.MarkPiece(0, true)
	mf.MarkPiece(2, true)

	// Same count — no-op, pieces preserved
	mf.SetPieceCount(4)
	if !mf.Have(0) || !mf.Have(2) {
		t.Error("pieces should be preserved with same count")
	}

	// Different count — clears
	mf.SetPieceCount(8)
	for i := 0; i < 8; i++ {
		if mf.Have(i) {
			t.Errorf("piece %d should be cleared after reinit", i)
		}
	}
}

func TestMultiFileBitfieldMultipleBytes(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	// 16 pieces = 2 bytes
	mf.SetPieceCount(16)

	// Mark piece 0 (MSB of byte 0) and piece 8 (MSB of byte 1)
	mf.MarkPiece(0, true)
	mf.MarkPiece(8, true)

	bf := mf.Bitfield()
	if len(bf) != 2 {
		t.Fatalf("Bitfield() length = %d, want 2", len(bf))
	}
	// Byte 0: piece 0 set = MSB = 0x80
	if bf[0] != 0x80 {
		t.Errorf("Bitfield()[0] = 0x%02x, want 0x80", bf[0])
	}
	// Byte 1: piece 8 set = MSB = 0x80
	if bf[1] != 0x80 {
		t.Errorf("Bitfield()[1] = 0x%02x, want 0x80", bf[1])
	}

	// All missing except 0 and 8
	missing := mf.Missing()
	if len(missing) != 14 {
		t.Errorf("Missing() len = %d, want 14: %v", len(missing), missing)
	}
}

func TestMultiFileZeroLengthFile(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
		{Name: "zero.txt", Length: 0, Offset: 10},
		{Name: "b.txt", Length: 10, Offset: 10},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	// Write/read across where zero-length file sits
	mf.WriteAt([]byte("aaaaaaaaaa"), 0)
	mf.WriteAt([]byte("bbbbbbbbbb"), 10)

	buf := make([]byte, 20)
	n, _ := mf.ReadAt(buf, 0)
	if string(buf[:n]) != "aaaaaaaaaabbbbbbbbbb" {
		t.Errorf("data across zero-length file = %q", string(buf[:n]))
	}

	// zero.txt should not be created as a file
	zeroPath := filepath.Join(dir, "zero.txt")
	if _, err := os.Stat(zeroPath); !os.IsNotExist(err) {
		t.Errorf("zero-length file %q should not exist", zeroPath)
	}
}

func TestMultiFileSize(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
		{Name: "b.txt", Length: 200, Offset: 150},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	wantSize := int64(150 + 200)
	if mf.Size() != wantSize {
		t.Errorf("Size() = %d, want %d", mf.Size(), wantSize)
	}
}

func TestMultiFileOpenForWriteAfterClose(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	if err := mf.Close(); err != nil {
		t.Fatal(err)
	}

	err := mf.OpenForWrite()
	if err != ErrFileClosed {
		t.Errorf("OpenForWrite after Close: got %v, want ErrFileClosed", err)
	}
}

func TestMultiFileDoubleWriteThenRead(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
		{Name: "b.txt", Length: 10, Offset: 10},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	// Write partial
	mf.WriteAt([]byte("HELLO"), 0)

	// Overwrite part
	mf.WriteAt([]byte("hello"), 0)

	// Verify: file is 10 bytes but only 5 written; holes read as zero
	buf := make([]byte, 10)
	_, _ = mf.ReadAt(buf, 0)
	if string(buf[:5]) != "hello" {
		t.Errorf("read %q after overwrite, want prefix %q", string(buf[:5]), "hello")
	}
	// Bytes 5-9 are zero (hole)
	for i := 5; i < 10; i++ {
		if buf[i] != 0 {
			t.Errorf("byte %d = %d, want 0 (hole)", i, buf[i])
		}
	}

	// Now write to second file
	mf.WriteAt([]byte("world"), 10)

	buf2 := make([]byte, 20)
	_, _ = mf.ReadAt(buf2, 0)
	want := "hello\x00\x00\x00\x00\x00world\x00\x00\x00\x00\x00"
	if string(buf2[:20]) != want {
		t.Errorf("full read = %q, want %q", string(buf2[:20]), want)
	}
}

func TestMultiFileReadAtGap(t *testing.T) {
	// Files with a gap between them
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
		{Name: "b.txt", Length: 10, Offset: 20}, // gap from 10 to 20
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	mf.WriteAt([]byte("aaaaaaaaaa"), 0)
	mf.WriteAt([]byte("bbbbbbbbbb"), 20)

	// Read across the gap: offset 5, 20 bytes
	buf := make([]byte, 20)
	_, err := mf.ReadAt(buf, 5)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	// First 5 bytes: "aaaaa" (from a.txt offset 5-9)
	if string(buf[:5]) != "aaaaa" {
		t.Errorf("bytes 0-4 = %q, want %q", string(buf[:5]), "aaaaa")
	}
	// Next 10 bytes: gap -> zeros
	for i := 5; i < 15; i++ {
		if buf[i] != 0 {
			t.Errorf("gap byte %d = %d, want 0", i, buf[i])
		}
	}
	// Last 5 bytes: "bbbbb" (from b.txt offset 0-4)
	if string(buf[15:20]) != "bbbbb" {
		t.Errorf("bytes 15-19 = %q, want %q", string(buf[15:20]), "bbbbb")
	}
}

func TestMultiFileReadAtPartialFile(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 5, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	mf.WriteAt([]byte("hello"), 0)

	// Read more than file size
	buf := make([]byte, 10)
	n, err := mf.ReadAt(buf, 3)
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
	if string(buf[:n]) != "lo" {
		t.Errorf("partial read = %q, want %q", string(buf[:n]), "lo")
	}
}

func TestMultiFileWriteAtEmptyBuffer(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	n, err := mf.WriteAt([]byte{}, 0)
	if err != nil {
		t.Errorf("WriteAt empty: %v", err)
	}
	if n != 0 {
		t.Errorf("WriteAt empty wrote %d bytes", n)
	}
}

func TestMultiFileCutTrailingGarbage(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
		{Name: "b.txt", Length: 10, Offset: 10},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	// Write data then manually extend file b.txt to simulate trailing garbage
	mf.WriteAt([]byte("aaaaaaaaaa"), 0)
	mf.WriteAt([]byte("bbbbbbbbbb"), 10)

	bf, err := os.OpenFile(filepath.Join(dir, "b.txt"), os.O_RDWR, 0666)
	if err != nil {
		t.Fatal(err)
	}
	if err := bf.Truncate(20); err != nil {
		bf.Close()
		t.Fatal(err)
	}
	bf.Close()

	fi, _ := os.Stat(filepath.Join(dir, "b.txt"))
	if fi.Size() != 20 {
		t.Fatalf("b.txt pre-truncate size = %d, want 20", fi.Size())
	}

	if err := mf.CutTrailingGarbage(); err != nil {
		t.Fatalf("CutTrailingGarbage: %v", err)
	}

	fi, _ = os.Stat(filepath.Join(dir, "b.txt"))
	if fi.Size() != 10 {
		t.Errorf("b.txt after CutTrailingGarbage size = %d, want 10", fi.Size())
	}
}

func TestMultiFileCutTrailingGarbageNoOp(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	// File was created empty (size 0 < 100) — no truncation needed
	if err := mf.CutTrailingGarbage(); err != nil {
		t.Fatalf("CutTrailingGarbage on smaller file: %v", err)
	}
}

func TestMultiFileCutTrailingGarbageAfterClose(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 100, Offset: 0},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	mf.Close()

	err := mf.CutTrailingGarbage()
	if !errors.Is(err, ErrFileClosed) {
		t.Errorf("CutTrailingGarbage after close: got %v, want ErrFileClosed", err)
	}
}

func TestMultiFileSetUtimeOnlyRequested(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0, Requested: true},
		{Name: "b.txt", Length: 10, Offset: 10, Requested: false},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	mf.WriteAt([]byte("aaaaaaaaaa"), 0)
	mf.WriteAt([]byte("bbbbbbbbbb"), 10)

	atime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mtime := time.Date(2020, 6, 15, 12, 0, 0, 0, time.UTC)
	oldMtime := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)

	// Set b.txt to an old mtime so we can verify it was NOT changed
	if err := os.Chtimes(filepath.Join(dir, "b.txt"), oldMtime, oldMtime); err != nil {
		t.Fatal(err)
	}

	numOK, err := mf.SetUtime(atime, mtime)
	if err != nil {
		t.Fatalf("SetUtime: %v", err)
	}
	if numOK != 1 {
		t.Errorf("SetUtime numOK = %d, want 1 (only a.txt is requested)", numOK)
	}

	af, _ := os.Stat(filepath.Join(dir, "a.txt"))
	gotMtimeA := af.ModTime().Truncate(time.Second)
	wantMtime := mtime.Truncate(time.Second)
	if !gotMtimeA.Equal(wantMtime) {
		t.Errorf("a.txt mtime = %v, want %v", gotMtimeA, wantMtime)
	}

	bf, _ := os.Stat(filepath.Join(dir, "b.txt"))
	gotMtimeB := bf.ModTime().Truncate(time.Second)
	wantOld := oldMtime.Truncate(time.Second)
	if !gotMtimeB.Equal(wantOld) {
		t.Errorf("b.txt mtime = %v, want %v (should not be changed)", gotMtimeB, wantOld)
	}
}

func TestMultiFileSetUtimeAllRequested(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
		{Name: "b.txt", Length: 10, Offset: 10},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	mf.WriteAt([]byte("aaaaaaaaaa"), 0)
	mf.WriteAt([]byte("bbbbbbbbbb"), 10)

	mf.SetAllRequested()

	atime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mtime := time.Date(2020, 6, 15, 12, 0, 0, 0, time.UTC)

	numOK, err := mf.SetUtime(atime, mtime)
	if err != nil {
		t.Fatalf("SetUtime: %v", err)
	}
	if numOK != 2 {
		t.Errorf("SetUtime numOK = %d, want 2", numOK)
	}

	for _, name := range []string{"a.txt", "b.txt"} {
		fi, _ := os.Stat(filepath.Join(dir, name))
		got := fi.ModTime().Truncate(time.Second)
		want := mtime.Truncate(time.Second)
		if !got.Equal(want) {
			t.Errorf("%s mtime = %v, want %v", name, got, want)
		}
	}
}

func TestMultiFileSetRequested(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0},
		{Name: "b.txt", Length: 10, Offset: 10},
		{Name: "c.txt", Length: 10, Offset: 20},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	mf.WriteAt([]byte("aaaaaaaaaa"), 0)
	mf.WriteAt([]byte("bbbbbbbbbb"), 10)
	mf.WriteAt([]byte("cccccccccc"), 20)

	mf.SetRequested([]int{0, 2})

	atime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mtime := time.Date(2020, 6, 15, 12, 0, 0, 0, time.UTC)
	oldMtime := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)

	os.Chtimes(filepath.Join(dir, "b.txt"), oldMtime, oldMtime)

	numOK, err := mf.SetUtime(atime, mtime)
	if err != nil {
		t.Fatalf("SetUtime: %v", err)
	}
	if numOK != 2 {
		t.Errorf("SetUtime numOK = %d, want 2", numOK)
	}

	bf, _ := os.Stat(filepath.Join(dir, "b.txt"))
	if !bf.ModTime().Truncate(time.Second).Equal(oldMtime.Truncate(time.Second)) {
		t.Error("b.txt mtime changed but it was not requested")
	}
}

func TestMultiFileSetUtimeAfterClose(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0, Requested: true},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}
	mf.Close()

	_, err := mf.SetUtime(time.Now(), time.Now())
	if !errors.Is(err, ErrFileClosed) {
		t.Errorf("SetUtime after close: got %v, want ErrFileClosed", err)
	}
}

func TestMultiFileSetUtimeZeroRequested(t *testing.T) {
	files := []FileEntry{
		{Name: "a.txt", Length: 10, Offset: 0, Requested: false},
	}
	mf, dir := makeMultiFile(files)
	defer os.RemoveAll(dir)

	if err := mf.OpenForWrite(); err != nil {
		t.Fatal(err)
	}

	mf.WriteAt([]byte("aaaaaaaaaa"), 0)

	numOK, err := mf.SetUtime(time.Now(), time.Now())
	if err != nil {
		t.Fatalf("SetUtime with no requested files: %v", err)
	}
	if numOK != 0 {
		t.Errorf("SetUtime numOK = %d, want 0", numOK)
	}
}
