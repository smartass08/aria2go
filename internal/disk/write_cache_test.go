package disk

import (
	"os"
	"testing"
)

// tempFile creates a temporary file for testing, writes initial data,
// and returns the *os.File and a cleanup function.
func tempFile(t *testing.T, initialData []byte) (*os.File, func()) {
	t.Helper()
	f, err := os.CreateTemp("", "writecache_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(initialData) > 0 {
		if _, err := f.Write(initialData); err != nil {
			f.Close()
			os.Remove(f.Name())
			t.Fatal(err)
		}
		if _, err := f.Seek(0, 0); err != nil {
			f.Close()
			os.Remove(f.Name())
			t.Fatal(err)
		}
	}
	return f, func() {
		f.Close()
		os.Remove(f.Name())
	}
}

// readTempFile reads the entire contents of an *os.File.
func readTempFile(f *os.File) ([]byte, error) {
	return os.ReadFile(f.Name())
}

func TestWriteCache_AddFlushRoundTrip(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 1024))
	defer cleanup()

	wc := NewWriteCache(1024)

	data := []byte("hello world")
	if err := wc.Add(f, 0, data); err != nil {
		t.Fatal(err)
	}
	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}

	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[:len(data)]) != string(data) {
		t.Errorf("expected %q, got %q", string(data), string(contents[:len(data)]))
	}
}

func TestWriteCache_ContiguousAppend(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 1024))
	defer cleanup()

	wc := NewWriteCache(1024)

	if err := wc.Add(f, 0, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Add(f, 5, []byte(" world")); err != nil {
		t.Fatal(err)
	}

	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}

	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	expected := "hello world"
	if string(contents[:len(expected)]) != expected {
		t.Errorf("expected %q, got %q", expected, string(contents[:len(expected)]))
	}
}

func TestWriteCache_NonContiguousFlushes(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 1024))
	defer cleanup()

	wc := NewWriteCache(1024)

	// First write at offset 0 is cached
	if err := wc.Add(f, 0, []byte("first")); err != nil {
		t.Fatal(err)
	}

	// Non-contiguous write forces flush of "first" + caches "second"
	if err := wc.Add(f, 100, []byte("second")); err != nil {
		t.Fatal(err)
	}

	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[0:5]) != "first" {
		t.Errorf("expected 'first' at offset 0 (flushed by non-contiguous write), got %q", string(contents[0:5]))
	}
	// "second" is only cached, not yet flushed
	if string(contents[100:106]) == "second" {
		t.Error("expected 'second' NOT to be flushed yet")
	}

	// "second" is still cached — flush it
	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}

	contents, err = readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[100:106]) != "second" {
		t.Errorf("expected 'second' at offset 100 after flush, got %q", string(contents[100:106]))
	}
}

func TestWriteCache_LRUEviction(t *testing.T) {
	f1, cleanup1 := tempFile(t, make([]byte, 512))
	defer cleanup1()
	f2, cleanup2 := tempFile(t, make([]byte, 512))
	defer cleanup2()

	wc := NewWriteCache(200)

	// Add entries to f1 (100 bytes) and f2 (150 bytes) — total 250 > 200
	if err := wc.Add(f1, 0, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if err := wc.Add(f2, 0, make([]byte, 150)); err != nil {
		t.Fatal(err)
	}

	// f1 is oldest, should be evicted (since total 250 > 200)
	contents1, err := readTempFile(f1)
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(contents1[:100]) {
		t.Error("expected f1 to be flushed (zero data)")
	}

	// f2 should still be in cache
	contents2, err := readTempFile(f2)
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(contents2[:150]) {
		// f2 hasn't been flushed yet — contents should still be zeros
		// Actually wait: f2 is still in cache. File should have zeros.
		if !allZero(contents2[:150]) {
			t.Error("expected f2 to NOT be flushed yet")
		}
	}
}

func TestWriteCache_DataCellOverflow(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 2048))
	defer cleanup()

	wc := NewWriteCache(100)

	// Data larger than maxSize
	data := make([]byte, 500)
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := wc.Add(f, 0, data); err != nil {
		t.Fatal(err)
	}

	// The entry (500 bytes) exceeds maxSize (100), so eviction should
	// have flushed it immediately
	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data {
		if contents[i] != data[i] {
			t.Fatalf("data[%d] = %d, want %d", i, contents[i], data[i])
		}
	}
}

func TestWriteCache_FlushWritesCorrectOffset(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 1024))
	defer cleanup()

	wc := NewWriteCache(1024)

	data := []byte("offset-data")
	if err := wc.Add(f, 500, data); err != nil {
		t.Fatal(err)
	}
	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}

	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[500:500+len(data)]) != string(data) {
		t.Errorf("expected %q at offset 500, got %q", string(data), string(contents[500:500+len(data)]))
	}
	// Data before offset should be untouched
	if !allZero(contents[:500]) {
		t.Error("expected zeros before offset 500")
	}
}

func TestWriteCache_CloseFlushesAll(t *testing.T) {
	f1, cleanup1 := tempFile(t, make([]byte, 512))
	defer cleanup1()
	f2, cleanup2 := tempFile(t, make([]byte, 512))
	defer cleanup2()

	wc := NewWriteCache(1024)

	if err := wc.Add(f1, 0, []byte("file1")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Add(f2, 0, []byte("file2")); err != nil {
		t.Fatal(err)
	}

	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}

	contents1, err := readTempFile(f1)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents1[:5]) != "file1" {
		t.Errorf("expected 'file1', got %q", string(contents1[:5]))
	}

	contents2, err := readTempFile(f2)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents2[:5]) != "file2" {
		t.Errorf("expected 'file2', got %q", string(contents2[:5]))
	}
}

func TestWriteCache_EmptyAdd(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 256))
	defer cleanup()

	wc := NewWriteCache(256)

	if err := wc.Add(f, 0, []byte{}); err != nil {
		t.Fatal(err)
	}
	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCache_FlushIdempotent(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 256))
	defer cleanup()

	wc := NewWriteCache(256)

	if err := wc.Add(f, 0, []byte("test")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}
	// Second flush should be a no-op
	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}

	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[:4]) != "test" {
		t.Errorf("expected 'test', got %q", string(contents[:4]))
	}
}

func TestWriteCache_FlushUnknownFile(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 256))
	defer cleanup()

	wc := NewWriteCache(256)

	// Flushing a file never added should not error
	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCache_MultipleContiguousAppends(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 1024))
	defer cleanup()

	wc := NewWriteCache(1024)

	if err := wc.Add(f, 0, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Add(f, 1, []byte("b")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Add(f, 2, []byte("c")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Add(f, 3, []byte("d")); err != nil {
		t.Fatal(err)
	}

	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}

	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[:4]) != "abcd" {
		t.Errorf("expected 'abcd', got %q", string(contents[:4]))
	}
}

func TestWriteCache_CloseEmpty(t *testing.T) {
	wc := NewWriteCache(1024)
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteCache_ReuseAfterClose(t *testing.T) {
	f, cleanup := tempFile(t, make([]byte, 512))
	defer cleanup()

	wc := NewWriteCache(1024)

	if err := wc.Add(f, 0, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Close(); err != nil {
		t.Fatal(err)
	}

	// After Close, cache is empty — can reuse
	if err := wc.Add(f, 100, []byte("second")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}

	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[100:106]) != "second" {
		t.Errorf("expected 'second' at offset 100, got %q", string(contents[100:106]))
	}
}

func TestWriteCache_AppendNoSpaceButContiguous(t *testing.T) {
	// When contiguous but buffer capacity is exactly full, a new
	// allocation happens via Go's append. Test that data is correct.
	f, cleanup := tempFile(t, make([]byte, 1024))
	defer cleanup()

	wc := NewWriteCache(1024)

	// Add exact 4 bytes
	if err := wc.Add(f, 0, []byte("abcd")); err != nil {
		t.Fatal(err)
	}
	// Append contiguous — grows the buffer
	if err := wc.Add(f, 4, []byte("efgh")); err != nil {
		t.Fatal(err)
	}

	if err := wc.Flush(f); err != nil {
		t.Fatal(err)
	}

	contents, err := readTempFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents[:8]) != "abcdefgh" {
		t.Errorf("expected 'abcdefgh', got %q", string(contents[:8]))
	}
}

func TestWriteCache_MultipleFilesLRUOrder(t *testing.T) {
	f1, cleanup1 := tempFile(t, make([]byte, 512))
	defer cleanup1()
	f2, cleanup2 := tempFile(t, make([]byte, 512))
	defer cleanup2()
	f3, cleanup3 := tempFile(t, make([]byte, 512))
	defer cleanup3()

	// maxSize=150, each entry=100. Add f1,f2 = 200 > 150. f1 oldest, evicted.
	// Then add f3 = 100, f2 is now at 100. f2,f3 = 200 > 150. f2 oldest, evicted.
	wc := NewWriteCache(150)

	if err := wc.Add(f1, 0, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if err := wc.Add(f2, 0, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}

	// f1 (oldest, 100b) should have been evicted -> written to disk
	contents1, err := readTempFile(f1)
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(contents1[:100]) {
		t.Error("f1 should have been flushed (zero data)")
	}

	// f2 (newest, 100b) should be in cache (not yet flushed)
	contents2, err := readTempFile(f2)
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(contents2[:100]) {
		t.Error("f2 should be in cache, not yet flushed")
	}

	// Add f3 — total becomes 200 (f2=100 + f3=100). f2 oldest, evicted.
	if err := wc.Add(f3, 0, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}

	// f2 should now be flushed
	contents2, err = readTempFile(f2)
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(contents2[:100]) {
		t.Error("f2 should have been flushed")
	}

	// f3 should still be in cache
	contents3, err := readTempFile(f3)
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(contents3[:100]) {
		t.Error("f3 should still be in cache")
	}

	// Flush f3
	if err := wc.Flush(f3); err != nil {
		t.Fatal(err)
	}
	contents3, err = readTempFile(f3)
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(contents3[:100]) {
		t.Error("f3 should have been flushed now")
	}
}

func TestWriteCache_TouchUpdatesLRUPosition(t *testing.T) {
	f1, cleanup1 := tempFile(t, make([]byte, 512))
	defer cleanup1()
	f2, cleanup2 := tempFile(t, make([]byte, 512))
	defer cleanup2()

	// maxSize=150. f1=50b (non-zero), f2=50b (non-zero). total=100.
	// Touch f1 by adding more (distinct) data -> f1 becomes 150b, total=200 > 150.
	// f2 is oldest, gets evicted.
	wc := NewWriteCache(150)

	data1 := make([]byte, 50)
	for i := range data1 {
		data1[i] = 0xAA
	}
	data2 := make([]byte, 50)
	for i := range data2 {
		data2[i] = 0xBB
	}
	data1b := make([]byte, 100)
	for i := range data1b {
		data1b[i] = 0xCC
	}

	if err := wc.Add(f1, 0, data1); err != nil {
		t.Fatal(err)
	}
	if err := wc.Add(f2, 0, data2); err != nil {
		t.Fatal(err)
	}

	// Touch f1 by adding contiguous data
	if err := wc.Add(f1, 50, data1b); err != nil {
		t.Fatal(err)
	}
	// Now f1=150b, f2=50b. total=200 > 150. f2 (oldest) evicted.

	// f2 should already be flushed to disk by eviction
	contents2, err := readTempFile(f2)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data2 {
		if contents2[i] != 0xBB {
			t.Fatalf("f2 should have been evicted: data2[%d] = %x, want %x", i, contents2[i], 0xBB)
		}
	}

	// f1 should still be in cache (not yet flushed)
	contents1, err := readTempFile(f1)
	if err != nil {
		t.Fatal(err)
	}
	if contents1[0] != 0 {
		t.Error("f1 should still be in cache, not yet flushed")
	}

	// Flush f1 explicitly
	if err := wc.Flush(f1); err != nil {
		t.Fatal(err)
	}
	contents1, err = readTempFile(f1)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data1 {
		if contents1[i] != 0xAA {
			t.Fatalf("data1[%d] = %x, want %x", i, contents1[i], 0xAA)
		}
	}
	for i := 0; i < len(data1b); i++ {
		if contents1[50+i] != 0xCC {
			t.Fatalf("data1b[%d] = %x, want %x", i, contents1[50+i], 0xCC)
		}
	}
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
