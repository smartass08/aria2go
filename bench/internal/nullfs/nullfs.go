package nullfs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
)

var TotalBytesWritten atomic.Int64

type Mount struct {
	Path    string
	cleanup func()
}

func New(parent string, sizeMb int) (*Mount, error) {
	if parent == "" {
		parent = filepath.Join(os.TempDir(), "aria2-bench-void")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", parent, err)
	}
	switch runtime.GOOS {
	case "linux":
		return mountFUSE(parent)
	default:
		return mountTmpDir(parent)
	}
}

func (m *Mount) Unmount() error {
	if m == nil || m.cleanup == nil {
		return nil
	}
	m.cleanup()
	m.cleanup = nil
	return nil
}

func mountTmpDir(parent string) (*Mount, error) {
	return &Mount{Path: parent, cleanup: func() {}}, nil
}
