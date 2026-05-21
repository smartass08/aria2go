package disk

import (
	"errors"
	"syscall"
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

func TestWrap_ENOSPC(t *testing.T) {
	err := Wrap("write", "/tmp/foo", syscall.ENOSPC)
	if !errors.Is(err, ErrDiskFull) {
		t.Errorf("errors.Is(err, ErrDiskFull) = false, want true")
	}
	if !IsDiskFull(err) {
		t.Errorf("IsDiskFull(err) = false, want true")
	}
}

func TestWrap_EIO_write(t *testing.T) {
	err := Wrap("write", "/tmp/foo", syscall.EIO)
	if !errors.Is(err, ErrWriteError) {
		t.Errorf("errors.Is(err, ErrWriteError) = false, want true")
	}
}

func TestWrap_EIO_read(t *testing.T) {
	err := Wrap("read", "/tmp/foo", syscall.EIO)
	if !errors.Is(err, ErrReadError) {
		t.Errorf("errors.Is(err, ErrReadError) = false, want true")
	}
}

func TestWrap_EINVAL(t *testing.T) {
	err := Wrap("write", "/tmp/foo", syscall.EINVAL)
	if !errors.Is(err, ErrInvalidOffset) {
		t.Errorf("errors.Is(err, ErrInvalidOffset) = false, want true")
	}
}

func TestWrap_EBADF(t *testing.T) {
	err := Wrap("read", "/tmp/foo", syscall.EBADF)
	if !errors.Is(err, ErrFileClosed) {
		t.Errorf("errors.Is(err, ErrFileClosed) = false, want true")
	}
}

func TestIsDiskFull_false(t *testing.T) {
	if IsDiskFull(nil) {
		t.Errorf("IsDiskFull(nil) = true, want false")
	}
	if IsDiskFull(syscall.EIO) {
		t.Errorf("IsDiskFull(EIO) = true, want false")
	}
}

func TestIsDiskFull_nested(t *testing.T) {
	err := Wrap("write", "/tmp/foo", syscall.ENOSPC)
	if !IsDiskFull(err) {
		t.Errorf("IsDiskFull(wrapped ENOSPC) = false, want true")
	}
}

func TestWrap_nonErrno_write(t *testing.T) {
	base := errors.New("some write error")
	err := Wrap("write", "/tmp/foo", base)
	if !errors.Is(err, ErrWriteError) {
		t.Errorf("errors.Is(err, ErrWriteError) = false, want true")
	}
	var ce *core.Error
	if !errors.As(err, &ce) {
		t.Fatal("errors.As failed for core.Error")
	}
	if !errors.Is(err, base) {
		t.Errorf("errors.Is(err, base) = false, want true")
	}
}

func TestWrap_nonErrno_read(t *testing.T) {
	base := errors.New("some read error")
	err := Wrap("read", "/tmp/foo", base)
	if !errors.Is(err, ErrReadError) {
		t.Errorf("errors.Is(err, ErrReadError) = false, want true")
	}
}

func TestWrap_nonErrno_default(t *testing.T) {
	base := errors.New("unknown op error")
	err := Wrap("unknown", "/tmp/foo", base)
	if !errors.Is(err, ErrWriteError) {
		t.Errorf("errors.Is(err, ErrWriteError) = false, want true")
	}
}

func TestSentinel_errors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code core.ErrorCode
	}{
		{"ErrDiskFull", ErrDiskFull, core.ExitNotEnoughDiskSpace},
		{"ErrWriteError", ErrWriteError, core.ExitFileIOError},
		{"ErrReadError", ErrReadError, core.ExitFileIOError},
		{"ErrInvalidOffset", ErrInvalidOffset, core.ExitFileIOError},
		{"ErrFileClosed", ErrFileClosed, core.ExitFileIOError},
		{"ErrAllocFailed", ErrAllocFailed, core.ExitFileCreateError},
		{"ErrVerifyFailed", ErrVerifyFailed, core.ExitChecksumError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ce *core.Error
			if !errors.As(tt.err, &ce) {
				t.Fatal("sentinel is not *core.Error")
			}
			if ce.Code != tt.code {
				t.Errorf("Code = %d, want %d", ce.Code, tt.code)
			}
		})
	}
}

func TestWrap_preserves_wrapped_error(t *testing.T) {
	err := Wrap("write", "/tmp/foo", syscall.ENOSPC)
	if !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("errors.Is(err, syscall.ENOSPC) = false, want true")
	}
}
