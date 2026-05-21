// Package disk provides the file I/O substrate for downloads and seeding,
// including Allocator strategies for pre-allocating disk space.
package disk

import (
	"io"
	"os"
	"sync"
)

var preallocPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 4096)
		return &buf
	},
}

// Allocator pre-allocates disk space for a file.  The concrete strategy
// is selected by the engine based on the --file-allocation option.
type Allocator interface {
	Allocate(f *os.File, size int64) error
	Name() string
}

// AllocatorNone does no pre-allocation.  The file grows only via writes.
type AllocatorNone struct{}

func (AllocatorNone) Allocate(f *os.File, size int64) error { return nil }
func (AllocatorNone) Name() string                          { return "none" }

// AllocatorTrunc uses f.Truncate(size) to pre-allocate.  Simple and
// portable, though it may create sparse files on some filesystems.
type AllocatorTrunc struct{}

func (AllocatorTrunc) Allocate(f *os.File, size int64) error { return f.Truncate(size) }
func (AllocatorTrunc) Name() string                          { return "trunc" }

// AllocatorPrealloc pre-allocates by sequentially writing zero-filled
// blocks.  This is the legacy slow path but guarantees real block
// allocation on all filesystems.
type AllocatorPrealloc struct {
	BufSize int64 // chunk size in bytes for zero writes; 4096 if zero.
}

func (a *AllocatorPrealloc) Allocate(f *os.File, size int64) error {
	bufSize := a.BufSize
	if bufSize <= 0 {
		bufSize = 4096
	}
	cur, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if cur >= size {
		return f.Truncate(size)
	}
	bufPtr := preallocPool.Get().(*[]byte)
	buf := *bufPtr
	if int64(cap(buf)) < bufSize {
		buf = make([]byte, bufSize)
	} else {
		buf = buf[:bufSize]
	}
	clear(buf)
	defer func() {
		*bufPtr = buf[:cap(buf)]
		preallocPool.Put(bufPtr)
	}()
	written := cur
	for written < size {
		chunk := buf
		if remaining := size - written; remaining < int64(len(chunk)) {
			chunk = chunk[:remaining]
		}
		n, err := f.Write(chunk)
		if n > 0 {
			written += int64(n)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *AllocatorPrealloc) Name() string { return "prealloc" }
