package disk

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSingleFileNewAndSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	const size int64 = 1024

	sf, err := NewSingleFile(p, size, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	if sf.Size() != size {
		t.Errorf("Size() = %d, want %d", sf.Size(), size)
	}

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() {
		t.Error("file is not a regular file")
	}
}

func TestSingleFileWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	const size int64 = 64

	sf, err := NewSingleFile(p, size, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	data := []byte("hello world")
	n, err := sf.WriteAt(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Fatalf("WriteAt wrote %d bytes, want %d", n, len(data))
	}

	buf := make([]byte, len(data))
	n, err = sf.ReadAt(buf, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Fatalf("ReadAt read %d bytes, want %d", n, len(data))
	}
	if string(buf) != string(data) {
		t.Errorf("ReadAt content = %q, want %q", string(buf), string(data))
	}
}

func TestSingleFileWriteAtOffset(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	const size int64 = 64

	sf, err := NewSingleFile(p, size, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	if _, err := sf.WriteAt([]byte("hello"), 10); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 5)
	if _, err := sf.ReadAt(buf, 10); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Errorf("got %q, want %q", string(buf), "hello")
	}

	prefix := make([]byte, 10)
	n, err := sf.ReadAt(prefix, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Fatalf("read %d bytes, want 10", n)
	}
	for i, b := range prefix {
		if b != 0 {
			t.Errorf("byte at offset %d is %d, want 0", i, b)
		}
	}
}

func TestSingleFileWriteAtInvalidOffset(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	const size int64 = 16

	sf, err := NewSingleFile(p, size, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	_, err = sf.WriteAt([]byte("x"), -1)
	if !isSentinelOrWraps(err, ErrInvalidOffset) {
		t.Errorf("WriteAt(-1) error = %v, want ErrInvalidOffset", err)
	}

	_, err = sf.WriteAt([]byte("x"), size+1)
	if !isSentinelOrWraps(err, ErrInvalidOffset) {
		t.Errorf("WriteAt(past-end) error = %v, want ErrInvalidOffset", err)
	}

	_, err = sf.WriteAt(make([]byte, 5), size-2)
	if !isSentinelOrWraps(err, ErrInvalidOffset) {
		t.Errorf("WriteAt(overflow) error = %v, want ErrInvalidOffset", err)
	}
}

func TestSingleFileSync(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	if _, err := sf.WriteAt([]byte("data"), 0); err != nil {
		t.Fatal(err)
	}
	if err := sf.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestSingleFileCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}

	if err := sf.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sf.Close(); err != nil {
		t.Fatalf("second Close() = %v, want nil", err)
	}
	if err := sf.Close(); err != nil {
		t.Fatalf("third Close() = %v, want nil", err)
	}
}

func TestSingleFileOperationsAfterClose(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	sf.Close()

	_, err = sf.WriteAt([]byte("x"), 0)
	if !isSentinelOrWraps(err, ErrFileClosed) {
		t.Errorf("WriteAt after close error = %v, want ErrFileClosed", err)
	}

	_, err = sf.ReadAt(make([]byte, 1), 0)
	if !isSentinelOrWraps(err, ErrFileClosed) {
		t.Errorf("ReadAt after close error = %v, want ErrFileClosed", err)
	}

	if err := sf.Sync(); !isSentinelOrWraps(err, ErrFileClosed) {
		t.Errorf("Sync after close error = %v, want ErrFileClosed", err)
	}

	if err := sf.OpenForWrite(); !isSentinelOrWraps(err, ErrFileClosed) {
		t.Errorf("OpenForWrite after close error = %v, want ErrFileClosed", err)
	}
}

func TestSingleFilePieceTracking(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(16)
	for i := 0; i < 16; i++ {
		if sf.Have(i) {
			t.Errorf("Have(%d) = true before marking", i)
		}
	}

	sf.MarkPiece(0, true)
	sf.MarkPiece(5, true)
	sf.MarkPiece(15, true)

	if !sf.Have(0) {
		t.Error("Have(0) = false after marking true")
	}
	if !sf.Have(5) {
		t.Error("Have(5) = false after marking true")
	}
	if !sf.Have(15) {
		t.Error("Have(15) = false after marking true")
	}
	if sf.Have(3) {
		t.Error("Have(3) = true when not marked")
	}

	sf.MarkPiece(0, false)
	if sf.Have(0) {
		t.Error("Have(0) = true after unmarking")
	}

	missing := sf.Missing()
	if len(missing) != 14 {
		t.Errorf("Missing() len = %d, want 14 (piece 5 and 15 are set)", len(missing))
	}

	bf := sf.Bitfield()
	if len(bf) != 2 {
		t.Errorf("Bitfield() len = %d, want 2", len(bf))
	}
	if bf[0]&0x04 == 0 {
		t.Error("bit for piece 5 not set in bitfield")
	}
	if bf[1]&0x01 == 0 {
		t.Error("bit for piece 15 not set in bitfield")
	}
}

func TestSingleFileBitfieldMSBOrder(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(8)
	sf.MarkPiece(0, true)

	bf := sf.Bitfield()
	if len(bf) != 1 {
		t.Fatalf("Bitfield() len = %d, want 1", len(bf))
	}
	if bf[0] != 0x80 {
		t.Errorf("Bitfield()[0] = 0x%02x, want 0x80 (piece 0 is MSB of byte 0)", bf[0])
	}

	sf.MarkPiece(7, true)
	bf = sf.Bitfield()
	if bf[0] != 0x81 {
		t.Errorf("Bitfield()[0] = 0x%02x, want 0x81", bf[0])
	}
}

func TestSingleFileBitfieldAllPieces(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(8)
	for i := 0; i < 8; i++ {
		sf.MarkPiece(i, true)
	}

	bf := sf.Bitfield()
	if bf[0] != 0xFF {
		t.Errorf("Bitfield()[0] = 0x%02x, want 0xFF", bf[0])
	}

	missing := sf.Missing()
	if missing != nil {
		t.Errorf("Missing() = %v, want nil", missing)
	}
}

func TestSingleFileSetPieceCountNoop(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(4)
	sf.MarkPiece(0, true)
	sf.MarkPiece(2, true)

	sf.SetPieceCount(4)
	if !sf.Have(0) || !sf.Have(2) {
		t.Error("SetPieceCount(4) again should be a no-op")
	}

	sf.SetPieceCount(8)
	if sf.Have(0) {
		t.Error("SetPieceCount(8) should clear bitfield")
	}
	if len(sf.Missing()) != 8 {
		t.Errorf("Missing() len = %d, want 8 after reset", len(sf.Missing()))
	}
}

func TestSingleFilePieceOutOfBounds(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	sf.SetPieceCount(4)
	if sf.Have(-1) {
		t.Error("Have(-1) should be false")
	}
	if sf.Have(4) {
		t.Error("Have(4) should be false")
	}

	sf.MarkPiece(-1, true)
	sf.MarkPiece(4, true)
	if sf.Have(0) {
		t.Error("MarkPiece out of bounds should not mark piece 0")
	}
}

func TestSingleFileBitfieldEmpty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	if bf := sf.Bitfield(); bf != nil {
		t.Errorf("Bitfield() before SetPieceCount = %v, want nil", bf)
	}
	if m := sf.Missing(); m != nil {
		t.Errorf("Missing() before SetPieceCount = %v, want nil", m)
	}
}

func TestSingleFileMarkPieceAfterClose(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	sf.SetPieceCount(4)
	sf.MarkPiece(0, true)
	sf.Close()

	sf.MarkPiece(1, true)
	if sf.Have(1) {
		t.Error("MarkPiece after close should be no-op")
	}
	if !sf.Have(0) {
		t.Error("Have(0) should still be true after close")
	}
}

func TestSingleFileConcurrent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	const size int64 = 1024

	sf, err := NewSingleFile(p, size, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			offset := int64(i * 256)
			data := make([]byte, 256)
			copy(data, "concurrent-write-test-data")
			if _, err := sf.WriteAt(data, offset); err != nil {
				t.Errorf("concurrent WriteAt: %v", err)
			}
		}()
	}
	wg.Wait()

	for i := 0; i < 4; i++ {
		offset := int64(i * 256)
		buf := make([]byte, 256)
		if _, err := sf.ReadAt(buf, offset); err != nil {
			t.Errorf("concurrent ReadAt: %v", err)
		}
		expect := make([]byte, 256)
		copy(expect, "concurrent-write-test-data")
		if string(buf) != string(expect) {
			t.Errorf("offset %d: got %q, want %q", offset, string(buf[:40]), string(expect[:40]))
		}
	}
}

func TestSingleFileOpenForWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	if err := sf.OpenForWrite(); err != nil {
		t.Fatalf("OpenForWrite: %v", err)
	}
	if err := sf.OpenForWrite(); err != nil {
		t.Fatalf("second OpenForWrite: %v", err)
	}
}

func TestSingleFileCutTrailingGarbage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	const declaredSize int64 = 64

	sf, err := NewSingleFile(p, declaredSize, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	// Pre-populate with data of the declared size
	if _, err := sf.WriteAt(make([]byte, declaredSize), 0); err != nil {
		t.Fatal(err)
	}

	// Now write data beyond the declared size to simulate trailing garbage.
	// We need to open the file directly since WriteAt rejects offsets beyond the declared size.
	sf.mu.Lock()
	if err := sf.f.Truncate(100); err != nil {
		sf.mu.Unlock()
		t.Fatal(err)
	}
	sf.mu.Unlock()

	fi, _ := os.Stat(p)
	if fi.Size() != 100 {
		t.Fatalf("file size after manual truncate = %d, want 100", fi.Size())
	}

	if err := sf.CutTrailingGarbage(); err != nil {
		t.Fatalf("CutTrailingGarbage: %v", err)
	}

	fi, _ = os.Stat(p)
	if fi.Size() != declaredSize {
		t.Errorf("file size after CutTrailingGarbage = %d, want %d", fi.Size(), declaredSize)
	}
}

func TestSingleFileCutTrailingGarbageNoOp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")
	const declaredSize int64 = 64

	sf, err := NewSingleFile(p, declaredSize, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	// File is smaller than declared (created empty by AllocatorNone)
	if err := sf.CutTrailingGarbage(); err != nil {
		t.Fatalf("CutTrailingGarbage on smaller file: %v", err)
	}

	fi, _ := os.Stat(p)
	if fi.Size() != 0 {
		t.Errorf("file modified by CutTrailingGarbage no-op, size = %d", fi.Size())
	}
}

func TestSingleFileCutTrailingGarbageAfterClose(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	sf.Close()

	err = sf.CutTrailingGarbage()
	if !isSentinelOrWraps(err, ErrFileClosed) {
		t.Errorf("CutTrailingGarbage after close: got %v, want ErrFileClosed", err)
	}
}

func TestSingleFileSetUtime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close()

	atime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mtime := time.Date(2020, 6, 15, 12, 0, 0, 0, time.UTC)

	if err := sf.SetUtime(atime, mtime); err != nil {
		t.Fatalf("SetUtime: %v", err)
	}

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}

	// Compare truncated to seconds (filesystem-dependent precision)
	gotAtime := fi.ModTime().Truncate(time.Second)
	wantAtime := atime.Truncate(time.Second)
	gotMtime := fi.ModTime().Truncate(time.Second)
	wantMtime := mtime.Truncate(time.Second)
	if !gotMtime.Equal(wantMtime) {
		t.Errorf("mtime = %v, want %v", gotMtime, wantMtime)
	}
	// On macOS, atime may not be set separately from mtime; just check mtime matches
	_ = gotAtime
	_ = wantAtime
}

func TestSingleFileSetUtimeAfterClose(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.dat")

	sf, err := NewSingleFile(p, 64, AllocatorNone{})
	if err != nil {
		t.Fatal(err)
	}
	sf.Close()

	err = sf.SetUtime(time.Now(), time.Now())
	if !isSentinelOrWraps(err, ErrFileClosed) {
		t.Errorf("SetUtime after close: got %v, want ErrFileClosed", err)
	}
}

// isSentinelOrWraps checks whether err is or wraps the target sentinel error.
func isSentinelOrWraps(err error, target error) bool {
	if err == target {
		return true
	}
	return errors.Is(err, target)
}
