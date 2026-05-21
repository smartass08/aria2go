package disk

import "io"

// WriteBuffer coalesces small writes into larger contiguous writes
// to reduce syscall overhead for small block writes (e.g., BT 16K blocks).
type WriteBuffer struct {
	w     io.WriterAt
	buf   []byte
	size  int
	start int64
	used  int
	dirty bool
}

// NewWriteBuffer creates a write buffer that will flush to w.
// size is the buffer capacity (typically 256KB).
func NewWriteBuffer(w io.WriterAt, size int) *WriteBuffer {
	return &WriteBuffer{
		w:    w,
		buf:  make([]byte, size),
		size: size,
	}
}

// WriteAt buffers the write. If the write is contiguous with the current
// buffer, it appends. Otherwise it flushes first, then buffers.
func (wb *WriteBuffer) WriteAt(p []byte, offset int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	wrote := len(p)

	// Coalesce: append if contiguous and space exists
	if offset == wb.start+int64(wb.used) && wb.used < wb.size {
		n := wb.size - wb.used
		if n > len(p) {
			n = len(p)
		}
		wb.dirty = true
		copy(wb.buf[wb.used:], p[:n])
		wb.used += n
		if wb.used == wb.size {
			if err := wb.flush(); err != nil {
				return 0, err
			}
		}
		if n == len(p) {
			return wrote, nil
		}
		p = p[n:]
		offset += int64(n)
	}

	// Flush current buffer if dirty
	if wb.dirty {
		if err := wb.flush(); err != nil {
			return 0, err
		}
	}

	// If remaining data is larger than buffer, write directly
	if len(p) >= wb.size {
		_, err := wb.w.WriteAt(p, offset)
		return wrote, err
	}

	// Start new buffer
	wb.start = offset
	copy(wb.buf, p)
	wb.used = len(p)
	wb.dirty = true
	return wrote, nil
}

// Flush writes any buffered data to the underlying writer.
func (wb *WriteBuffer) Flush() error {
	if !wb.dirty {
		return nil
	}
	return wb.flush()
}

func (wb *WriteBuffer) flush() error {
	_, err := wb.w.WriteAt(wb.buf[:wb.used], wb.start)
	wb.used = 0
	wb.dirty = false
	return err
}

// Discard drops buffered data without writing.
func (wb *WriteBuffer) Discard() {
	wb.used = 0
	wb.dirty = false
}
