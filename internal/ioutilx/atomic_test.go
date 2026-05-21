package ioutilx

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func requireUnixPerm(t *testing.T, path string, want os.FileMode, msg string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("perm = %o, want %o%s", info.Mode().Perm(), want, msg)
	}
}

func TestAtomicWrite_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	data := []byte("hello world")

	if err := AtomicWrite(path, data, 0644); err != nil {
		t.Fatalf("AtomicWrite failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("content = %q, want %q", got, data)
	}

	requireUnixPerm(t, path, 0644, "")
}

func TestAtomicWrite_PreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	origData := []byte("original content")
	if err := os.WriteFile(path, origData, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	newData := []byte("replacement")
	if err := AtomicWrite(path, newData, 0644); err != nil {
		t.Fatalf("AtomicWrite failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(newData) {
		t.Fatalf("content = %q, want %q", got, newData)
	}

	requireUnixPerm(t, path, 0600, " (original preserved)")
}

func TestAtomicWrite_OverwriteMultiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	for i := 0; i < 5; i++ {
		data := []byte(string(rune('A' + i)))
		if err := AtomicWrite(path, data, 0644); err != nil {
			t.Fatalf("AtomicWrite #%d failed: %v", i, err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "E" {
		t.Fatalf("content = %q, want %q", got, "E")
	}
}

func TestAtomicWrite_EmptyData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	if err := AtomicWrite(path, []byte{}, 0644); err != nil {
		t.Fatalf("AtomicWrite failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("content length = %d, want 0", len(got))
	}
}

func TestAtomicWrite_NoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	tmpPath := path + ".tmp"

	if err := AtomicWrite(path, []byte("data"), 0644); err != nil {
		t.Fatalf("AtomicWrite failed: %v", err)
	}

	if _, err := os.Stat(tmpPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file %s still exists", tmpPath)
	}
}

func TestAtomicWriter_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}

	parts := []string{"hello", " ", "world"}
	for _, p := range parts {
		n, err := w.Write([]byte(p))
		if err != nil {
			t.Fatalf("Write(%q) failed: %v", p, err)
		}
		if n != len(p) {
			t.Fatalf("Write(%q) = %d, want %d", p, n, len(p))
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("content = %q, want %q", got, "hello world")
	}

	requireUnixPerm(t, path, 0644, "")
}

func TestAtomicWriter_PreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}
	if _, err := w.Write([]byte("replaced")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	requireUnixPerm(t, path, 0600, " (original preserved)")
}

func TestAtomicWriter_Cancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	tmpPath := path + ".tmp"

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := w.Cancel(); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	// Temp file should be gone.
	if _, err := os.Stat(tmpPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file %s still exists after cancel", tmpPath)
	}

	// Destination should be untouched.
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination %s exists after cancel (should not)", path)
	}
}

func TestAtomicWriter_CancelIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := w.Cancel(); err != nil {
		t.Fatalf("first Cancel failed: %v", err)
	}
	if err := w.Cancel(); err != nil {
		t.Fatalf("second Cancel failed: %v", err)
	}
}

func TestAtomicWriter_WriteAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	_, err = w.Write([]byte("more"))
	if !errors.Is(err, ErrAtomicWriterClosed) {
		t.Fatalf("Write after Close: err = %v, want %v", err, ErrAtomicWriterClosed)
	}
}

func TestAtomicWriter_WriteAfterCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Cancel(); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	_, err = w.Write([]byte("more"))
	if !errors.Is(err, ErrAtomicWriterClosed) {
		t.Fatalf("Write after Cancel: err = %v, want %v", err, ErrAtomicWriterClosed)
	}
}

func TestAtomicWriter_CloseAfterCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Cancel(); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	err = w.Close()
	if !errors.Is(err, ErrAtomicWriterClosed) {
		t.Fatalf("Close after Cancel: err = %v, want %v", err, ErrAtomicWriterClosed)
	}
}

func TestAtomicWriter_DoubleClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	err = w.Close()
	if !errors.Is(err, ErrAtomicWriterClosed) {
		t.Fatalf("second Close: err = %v, want %v", err, ErrAtomicWriterClosed)
	}
}

func TestAtomicWriter_NoTempFileAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	tmpPath := path + ".tmp"

	w, err := NewAtomicWriter(path, 0644)
	if err != nil {
		t.Fatalf("NewAtomicWriter failed: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if _, err := os.Stat(tmpPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file %s still exists after close", tmpPath)
	}
}
