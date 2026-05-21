//go:build darwin

package disk

import (
	"io"
	"os"
	"syscall"
	"unsafe"

	"github.com/smartass08/aria2go/internal/platform"
)

const (
	fAllocateContig = 0x00000002
	fAllocateAll    = 0x00000004
	fPeofPosMode    = 3
	fPreallocate    = 42
)

type fstore struct {
	Flags      uint32
	Posmode    int32
	Offset     int64
	Length     int64
	Bytesalloc int64
}

// AllocatorFalloc uses filesystem-level preallocation via fcntl(F_PREALLOCATE)
// on Darwin (macOS), matching aria2's allocate() with Apple-specific
// fcntl-based preallocation and retry logic.
type AllocatorFalloc struct{}

func (AllocatorFalloc) Name() string { return "falloc" }

func (a AllocatorFalloc) Allocate(f *os.File, size int64) error {
	if !platform.Caps().Fallocate {
		return f.Truncate(size)
	}
	// Determine current file size so we only allocate the remaining
	// range — matching aria2's allocation iterators that pass
	// offset_ as the current size.
	cur, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return f.Truncate(size)
	}
	if cur >= size {
		return f.Truncate(size)
	}
	toalloc := size - cur
	fd := int(f.Fd())
	// Try contiguous + all first.
	fs := fstore{
		Flags:   fAllocateContig | fAllocateAll,
		Posmode: fPeofPosMode,
		Length:  toalloc,
	}
	_, _, e1 := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), fPreallocate, uintptr(unsafe.Pointer(&fs)))
	if e1 == 0 {
		return f.Truncate(size)
	}
	// Retry non-contiguous, matching aria2's retry logic.
	fs.Flags = fAllocateAll
	_, _, e1 = syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), fPreallocate, uintptr(unsafe.Pointer(&fs)))
	if e1 == 0 {
		return f.Truncate(size)
	}
	// Both attempts failed — fall back to zero-fill preallocation
	// (matching aria2's AdaptiveFileAllocationIterator fallback).
	pa := &AllocatorPrealloc{}
	return pa.Allocate(f, size)
}
