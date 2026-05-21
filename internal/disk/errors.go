package disk

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/smartass08/aria2go/internal/core"
)

var (
	ErrDiskFull      = core.NewError(core.ExitNotEnoughDiskSpace, "not enough disk space")
	ErrWriteError    = core.NewError(core.ExitFileIOError, "file write error")
	ErrReadError     = core.NewError(core.ExitFileIOError, "file read error")
	ErrInvalidOffset = core.NewError(core.ExitFileIOError, "invalid file offset")
	ErrFileClosed    = core.NewError(core.ExitFileIOError, "file already closed")
	ErrAllocFailed   = core.NewError(core.ExitFileCreateError, "file allocation failed")
	ErrVerifyFailed  = core.NewError(core.ExitChecksumError, "piece verification failed")
)

type wrappedDiskError struct {
	sentinel *core.Error
	cause    error
	msg      string
}

func (e *wrappedDiskError) Error() string { return e.msg }

func (e *wrappedDiskError) Unwrap() error { return e.cause }

func (e *wrappedDiskError) Is(target error) bool {
	if target == e.sentinel {
		return true
	}
	return errors.Is(e.cause, target)
}

func (e *wrappedDiskError) As(target interface{}) bool {
	if ce, ok := target.(**core.Error); ok {
		*ce = e.sentinel
		return true
	}
	return errors.As(e.cause, target)
}

// Wrap creates a disk error that wraps an OS I/O error with the appropriate
// disk error code. It examines the underlying error to map common syscall
// errors (ENOSPC, EIO, etc.) to disk-specific sentinel errors.
func Wrap(op string, path string, err error) error {
	sentinel := mapErrno(op, err)
	return &wrappedDiskError{
		sentinel: sentinel,
		cause:    err,
		msg:      fmt.Sprintf("%d: %s: %v", sentinel.Code, sentinel.Message, err),
	}
}

func mapErrno(op string, err error) *core.Error {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return sentinelForOp(op)
	}

	switch {
	case errno == syscall.ENOSPC:
		return ErrDiskFull
	case errno == syscall.EIO:
		if op == "write" {
			return ErrWriteError
		}
		return ErrReadError
	case errno == syscall.EINVAL:
		return ErrInvalidOffset
	case errno == syscall.EBADF:
		return ErrFileClosed
	default:
		return sentinelForOp(op)
	}
}

func sentinelForOp(op string) *core.Error {
	switch op {
	case "write":
		return ErrWriteError
	case "read":
		return ErrReadError
	default:
		return ErrWriteError
	}
}

// IsDiskFull reports whether err is or wraps a disk-full condition (ENOSPC).
func IsDiskFull(err error) bool {
	return errors.Is(err, syscall.ENOSPC)
}
