package core

import (
	"errors"
	"fmt"
	"testing"
)

func TestError_Error_NilCause(t *testing.T) {
	e := NewError(ExitTimeout, "operation timed out")
	want := "2: operation timed out"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestError_Error_WithCause(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	e := WrapError(ExitNetworkProblem, "network problem", cause)
	got := e.Error()
	want := "6: network problem: connection refused"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("disk full")
	e := WrapError(ExitNotEnoughDiskSpace, "not enough disk space", cause)
	unwrapped := e.Unwrap()
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}
}

func TestError_Unwrap_Nil(t *testing.T) {
	e := NewError(ExitBadOption, "bad option value")
	if e.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil", e.Unwrap())
	}
}

func TestCodeFrom_Direct(t *testing.T) {
	err := NewError(ExitTimeout, "timed out")
	if got := CodeFrom(err); got != ExitTimeout {
		t.Errorf("CodeFrom() = %d, want %d", got, ExitTimeout)
	}
}

func TestCodeFrom_Wrapped(t *testing.T) {
	cause := fmt.Errorf("read error")
	err := WrapError(ExitFileIOError, "file I/O error", cause)
	wrapped := fmt.Errorf("download failed: %w", err)
	if got := CodeFrom(wrapped); got != ExitFileIOError {
		t.Errorf("CodeFrom() = %d, want %d", got, ExitFileIOError)
	}
}

func TestCodeFrom_PlainError(t *testing.T) {
	err := fmt.Errorf("something went wrong")
	if got := CodeFrom(err); got != ExitUnknownError {
		t.Errorf("CodeFrom() = %d, want %d (ExitUnknownError)", got, ExitUnknownError)
	}
}

func TestCodeFrom_Nil(t *testing.T) {
	if got := CodeFrom(nil); got != ExitUnknownError {
		t.Errorf("CodeFrom(nil) = %d, want %d", got, ExitUnknownError)
	}
}

func TestCodeFrom_DeeplyWrapped(t *testing.T) {
	root := NewError(ExitChecksumError, "checksum mismatch")
	mid := fmt.Errorf("validation: %w", root)
	outer := fmt.Errorf("download error: %w", mid)
	if got := CodeFrom(outer); got != ExitChecksumError {
		t.Errorf("CodeFrom() = %d, want %d", got, ExitChecksumError)
	}
}

func TestIsSentinel(t *testing.T) {
	if !IsSentinel(ErrNotFound, ErrNotFound) {
		t.Error("IsSentinel(ErrNotFound, ErrNotFound) = false, want true")
	}
	if !IsSentinel(fmt.Errorf("wrapped: %w", ErrNotFound), ErrNotFound) {
		t.Error("IsSentinel(wrapped ErrNotFound, ErrNotFound) = false, want true")
	}
	if IsSentinel(ErrNotFound, ErrTimeout) {
		t.Error("IsSentinel(ErrNotFound, ErrTimeout) = true, want false")
	}
}

func TestSentinelErrorValues(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		code ErrorCode
		msg  string
	}{
		{"ErrNotFound", ErrNotFound, ExitResourceNotFound, "resource not found"},
		{"ErrTimeout", ErrTimeout, ExitTimeout, "operation timed out"},
		{"ErrNetworkProblem", ErrNetworkProblem, ExitNetworkProblem, "network problem"},
		{"ErrDiskFull", ErrDiskFull, ExitNotEnoughDiskSpace, "not enough disk space"},
		{"ErrFileExists", ErrFileExists, ExitFileAlreadyExists, "file already exists"},
		{"ErrAuthFailed", ErrAuthFailed, ExitHTTPAuthFailed, "authorization failed"},
		{"ErrBadOption", ErrBadOption, ExitBadOption, "bad option value"},
		{"ErrChecksumMismatch", ErrChecksumMismatch, ExitChecksumError, "checksum validation failed"},
		{"ErrInternal", ErrInternal, ExitUnknownError, "internal error"},
		{"ErrCancelled", ErrCancelled, ExitRemoved, "download removed by user"},
	}
	for _, tt := range tests {
		if tt.err.Code != tt.code {
			t.Errorf("%s.Code = %d, want %d", tt.name, tt.err.Code, tt.code)
		}
		if tt.err.Message != tt.msg {
			t.Errorf("%s.Message = %q, want %q", tt.name, tt.err.Message, tt.msg)
		}
		if tt.err.Cause != nil {
			t.Errorf("%s.Cause = %v, want nil (sentinel must have no cause)", tt.name, tt.err.Cause)
		}
	}
}

func TestWrapErrorInFmtErrorf(t *testing.T) {
	// Verify that %w wrapping works so errors.Is can traverse the chain.
	cause := &netOpError{s: "read tcp"}
	wrapped := WrapError(ExitNetworkProblem, "network problem", cause)
	e := fmt.Errorf("level1: %w", wrapped)
	e = fmt.Errorf("level2: %w", e)

	var coreErr *Error
	if !errors.As(e, &coreErr) {
		t.Fatal("expected core.Error in chain")
	}
	if coreErr.Code != ExitNetworkProblem {
		t.Errorf("Code = %d, want %d", coreErr.Code, ExitNetworkProblem)
	}

	var netErr *netOpError
	if !errors.As(e, &netErr) {
		t.Fatal("expected netOpError in chain")
	}
}

func TestError_IsSentinelWithWrapError(t *testing.T) {
	// Sentinel errors have nil cause; wrapping with WrapError adds a cause.
	// errors.Is on ErrNotFound should still match a WrapError that chains to it.
	e := WrapError(ExitResourceNotFound, "custom not found", ErrNotFound)
	if !errors.Is(e, ErrNotFound) {
		t.Error("WrapError(..., ErrNotFound) should match ErrNotFound via errors.Is")
	}
}

// netOpError is a minimal test-only error type used to verify errors.As chaining.
type netOpError struct {
	s string
}

func (e *netOpError) Error() string { return e.s }
