package disk

import (
	"io"
	"testing"
)

type testWriterAt struct {
	data   []byte
	writes [][2]int64 // (offset, length) for each write
}

func newTestWriterAt(size int64) *testWriterAt {
	return &testWriterAt{data: make([]byte, size)}
}

func (w *testWriterAt) WriteAt(p []byte, off int64) (int, error) {
	w.writes = append(w.writes, [2]int64{off, int64(len(p))})
	copy(w.data[off:], p)
	return len(p), nil
}

func TestWriteBuffer_CoalesceContiguous(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 256)

	// Write contiguous blocks that fit in buffer
	wb.WriteAt([]byte("hello"), 0)
	wb.WriteAt([]byte("world"), 5)

	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}

	// Should be a single write
	if len(w.writes) != 1 {
		t.Fatalf("expected 1 coalesced write, got %d", len(w.writes))
	}
	if w.writes[0][0] != 0 || w.writes[0][1] != 10 {
		t.Errorf("expected write at offset 0 length 10, got offset=%d len=%d", w.writes[0][0], w.writes[0][1])
	}
	if string(w.data[:10]) != "helloworld" {
		t.Errorf("expected 'helloworld', got %q", string(w.data[:10]))
	}
}

func TestWriteBuffer_FlushNonContiguous(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 256)

	wb.WriteAt([]byte("hello"), 0)
	wb.WriteAt([]byte("world"), 10) // non-contiguous

	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}

	// Should be two writes — one for first buffer (flushed by non-contiguous write), one for second
	if len(w.writes) != 2 {
		t.Fatalf("expected 2 writes, got %d", len(w.writes))
	}
	if string(w.data[0:5]) != "hello" {
		t.Errorf("expected 'hello' at offset 0, got %q", string(w.data[0:5]))
	}
	if string(w.data[10:15]) != "world" {
		t.Errorf("expected 'world' at offset 10, got %q", string(w.data[10:15]))
	}
}

func TestWriteBuffer_FlushCorrectData(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 256)

	data := []byte("some data to write")
	n, err := wb.WriteAt(data, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(data) {
		t.Fatalf("WriteAt returned %d, want %d", n, len(data))
	}

	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}

	if string(w.data[100:100+len(data)]) != string(data) {
		t.Errorf("data mismatch at offset 100")
	}
}

func TestWriteBuffer_Discard(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 256)

	wb.WriteAt([]byte("discarded"), 0)
	wb.Discard()

	// Writer should have received nothing
	if len(w.writes) != 0 {
		t.Errorf("expected 0 writes after Discard, got %d", len(w.writes))
	}

	// Buffer should be reusable
	wb.WriteAt([]byte("newdata"), 50)
	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(w.writes) != 1 {
		t.Fatalf("expected 1 write after reuse, got %d", len(w.writes))
	}
	if string(w.data[50:57]) != "newdata" {
		t.Errorf("expected 'newdata' at offset 50, got %q", string(w.data[50:57]))
	}
}

func TestWriteBuffer_BufferFullFlushes(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 8)

	// Write that fits partially — fills buffer and triggers flush for first part
	wb.WriteAt([]byte("abcdefghij"), 0)

	// The buffer capacity is 8. abcdefgh fills it (flushes), then ij starts new buffer.
	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}

	if len(w.writes) != 2 {
		t.Fatalf("expected 2 writes (auto-flush + manual flush), got %d", len(w.writes))
	}
	if string(w.data[0:8]) != "abcdefgh" {
		t.Errorf("expected 'abcdefgh' at offset 0, got %q", string(w.data[0:8]))
	}
	if string(w.data[8:10]) != "ij" {
		t.Errorf("expected 'ij' at offset 8, got %q", string(w.data[8:10]))
	}
}

func TestWriteBuffer_ZeroLengthWrite(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 256)

	n, err := wb.WriteAt([]byte{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("WriteAt empty slice returned %d, want 0", n)
	}

	// Subsequent write should still work
	wb.WriteAt([]byte("data"), 0)
	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}
	if string(w.data[0:4]) != "data" {
		t.Errorf("expected 'data', got %q", string(w.data[0:4]))
	}
}

func TestWriteBuffer_FlushEmpty(t *testing.T) {
	w := newTestWriterAt(64)
	wb := NewWriteBuffer(w, 256)

	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(w.writes) != 0 {
		t.Errorf("expected 0 writes flushing empty buffer, got %d", len(w.writes))
	}
}

func TestWriteBuffer_NonContiguousAfterBufferReuse(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 256)

	wb.WriteAt([]byte("first"), 0)
	wb.Flush()
	wb.WriteAt([]byte("second"), 100)
	wb.Flush()

	if len(w.writes) != 2 {
		t.Fatalf("expected 2 writes, got %d", len(w.writes))
	}
	if string(w.data[0:5]) != "first" {
		t.Errorf("expected 'first' at offset 0, got %q", string(w.data[0:5]))
	}
	if string(w.data[100:106]) != "second" {
		t.Errorf("expected 'second' at offset 100, got %q", string(w.data[100:106]))
	}
}

func TestWriteBuffer_WriteAtReturnsCorrectN(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 64)

	p := make([]byte, 100)
	for i := range p {
		p[i] = byte(i % 256)
	}

	n, err := wb.WriteAt(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(p) {
		t.Errorf("WriteAt returned %d, want %d", n, len(p))
	}

	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}

	for i := range p {
		if w.data[i] != p[i] {
			t.Fatalf("data[%d] = %d, want %d", i, w.data[i], p[i])
		}
	}
}

func TestWriteBuffer_LargeWritePassthrough(t *testing.T) {
	w := newTestWriterAt(1024)
	wb := NewWriteBuffer(w, 8)

	// Write larger than buffer capacity — flushes any existing buffer,
	// then writes directly since it doesn't fit
	wb.WriteAt([]byte("x"), 0) // buffer at offset 0
	p := make([]byte, 20)
	for i := range p {
		p[i] = byte('A' + i%26)
	}

	n, err := wb.WriteAt(p, 50)
	if err != nil {
		t.Fatal(err)
	}
	if n != 20 {
		t.Errorf("WriteAt returned %d, want 20", n)
	}

	// Should have flushed the buffered "x" first
	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}

	if string(w.data[0:1]) != "x" {
		t.Errorf("expected 'x' at offset 0, got %q", w.data[0:1])
	}
	if string(w.data[50:70]) != string(p) {
		t.Errorf("large write data mismatch at offset 50")
	}
}

func TestWriteBuffer_DoubleFlush(t *testing.T) {
	w := newTestWriterAt(64)
	wb := NewWriteBuffer(w, 256)

	wb.WriteAt([]byte("test"), 0)
	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := wb.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(w.writes) != 1 {
		t.Errorf("expected 1 write after double flush, got %d", len(w.writes))
	}
}

// Ensure WriteBuffer implements io.WriterAt
var _ io.WriterAt = (*WriteBuffer)(nil)
