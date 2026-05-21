//go:build linux

package disk

import (
	"io"
	"os"
	"syscall"

	"github.com/smartass08/aria2go/internal/platform"
)

// AllocatorFalloc uses filesystem-level preallocation via the fallocate(2)
// syscall on Linux for efficient block allocation on ext4, xfs, and btrfs.
//
// It implements aria2's adaptive fallback strategy: first tries
// fallocate on the remaining range; if fallocate is not supported by
// the filesystem, falls back to zero-fill preallocation (matching
// aria2's AdaptiveFileAllocationIterator).
type AllocatorFalloc struct{}

func (AllocatorFalloc) Name() string { return "falloc" }

func (a AllocatorFalloc) Allocate(f *os.File, size int64) error {
	if !platform.Caps().Fallocate {
		return f.Truncate(size)
	}
	// Determine current file size so we only allocate the remaining
	// range — matching aria2's FallocFileAllocationIterator which
	// passes offset_ (the current size) and totalLength - offset_.
	cur, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return f.Truncate(size)
	}
	if cur >= size {
		return f.Truncate(size)
	}
	// Try fallocate(2) on the remaining range.
	err = syscall.Fallocate(int(f.Fd()), 0, cur, size-cur)
	if err == nil {
		return nil
	}
	// If the filesystem does not support fallocate (EOPNOTSUPP,
	// ENOSYS, EINVAL), fall back to zero-fill preallocation,
	// matching aria2's AdaptiveFileAllocationIterator fallback.
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.EOPNOTSUPP, syscall.ENOSYS, syscall.EINVAL:
			pa := &AllocatorPrealloc{}
			return pa.Allocate(f, size)
		}
	}
	return err
}
