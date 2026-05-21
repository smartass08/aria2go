//go:build !linux && !darwin

package disk

import (
	"os"

	"github.com/smartass08/aria2go/internal/platform"
)

// AllocatorFalloc uses filesystem-level preallocation where available.
// On non-Linux, non-Darwin platforms, allocation falls back to
// f.Truncate(size) after checking platform.Caps().Fallocate.
type AllocatorFalloc struct{}

func (AllocatorFalloc) Name() string { return "falloc" }

func (a AllocatorFalloc) Allocate(f *os.File, size int64) error {
	if !platform.Caps().Fallocate {
		return f.Truncate(size)
	}
	return f.Truncate(size)
}
