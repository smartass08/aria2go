package disk

import (
	"os"
	"runtime"
)

// SparseAllocator allocates sparse regions for files.
// It wraps another Allocator and is used for BitTorrent where pieces
// may arrive out of order and zero-filled gaps should not consume
// actual disk space.
type SparseAllocator struct {
	inner Allocator
}

// NewSparseAllocator returns a SparseAllocator that delegates
// actual file creation to inner.
func NewSparseAllocator(inner Allocator) *SparseAllocator {
	return &SparseAllocator{inner: inner}
}

// Allocate delegates to the inner Allocator. On Unix systems
// (Linux, macOS, FreeBSD, OpenBSD) the newly extended regions
// are already sparse — ftruncate creates holes that read as
// zeros without consuming disk blocks.
func (s *SparseAllocator) Allocate(f *os.File, size int64) error {
	return s.inner.Allocate(f, size)
}

// Name returns "sparse".
func (s *SparseAllocator) Name() string { return "sparse" }

// IsSparseSupported returns true on Unix systems where ftruncate
// creates sparse files by default. Returns false on Windows where
// explicit FSCTL_SET_SPARSE is required (not yet implemented).
func IsSparseSupported() bool {
	return runtime.GOOS != "windows"
}
