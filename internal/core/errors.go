package core

import (
	"errors"
	"strconv"
	"sync"
)

// Error is the standard error type for all aria2go packages.
// It carries an ErrorCode for RPC exit-code mapping and CLI exit handling.
type Error struct {
	Code    ErrorCode
	Message string
	msgFmt  string // pre-computed "CODE: Message" prefix
	Cause   error
}

var errorPool = sync.Pool{
	New: func() any { return &Error{} },
}

// newError allocates or pools an Error with pre-computed format prefix.
func newError(code ErrorCode, message string) *Error {
	e := errorPool.Get().(*Error)
	e.Code = code
	e.Message = message
	e.msgFmt = strconv.Itoa(int(code)) + ": " + message
	e.Cause = nil
	return e
}

// ReleaseError returns e to the pool. Only call this when no references
// to e remain in any error chain.
func ReleaseError(e *Error) {
	*e = Error{}
	errorPool.Put(e)
}

// Error returns the full error string.
func (e *Error) Error() string {
	if e.Cause != nil {
		return e.msgFmt + ": " + e.Cause.Error()
	}
	return e.msgFmt
}

// Unwrap returns the wrapped cause so that errors.Is and errors.As
// can traverse the error chain.
func (e *Error) Unwrap() error {
	return e.Cause
}

// NewError creates a core.Error with no wrapped cause.
func NewError(code ErrorCode, message string) *Error {
	return newError(code, message)
}

// WrapError creates a core.Error that wraps an underlying cause.
func WrapError(code ErrorCode, message string, cause error) *Error {
	e := newError(code, message)
	e.Cause = cause
	return e
}

// CodeFrom walks the error chain via errors.As to find a *core.Error
// and returns its ErrorCode. Returns ExitUnknownError if no ErrorCode is found.
func CodeFrom(err error) ErrorCode {
	if err == nil {
		return ExitUnknownError
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ExitUnknownError
}

// IsSentinel is a convenience wrapper around errors.Is for checking
// whether err wraps one of the sentinel errors.
func IsSentinel(err error, target error) bool {
	return errors.Is(err, target)
}

// Sentinel errors map to specific aria2 exit codes.
// They carry no cause and serve as terminal errors.
var (
	ErrNotFound         = NewError(ExitResourceNotFound, "resource not found")
	ErrTimeout          = NewError(ExitTimeout, "operation timed out")
	ErrNetworkProblem   = NewError(ExitNetworkProblem, "network problem")
	ErrDiskFull         = NewError(ExitNotEnoughDiskSpace, "not enough disk space")
	ErrFileExists       = NewError(ExitFileAlreadyExists, "file already exists")
	ErrAuthFailed       = NewError(ExitHTTPAuthFailed, "authorization failed")
	ErrBadOption        = NewError(ExitBadOption, "bad option value")
	ErrChecksumMismatch = NewError(ExitChecksumError, "checksum validation failed")
	ErrInternal         = NewError(ExitUnknownError, "internal error")
	ErrCancelled        = NewError(ExitRemoved, "download removed by user")
)
