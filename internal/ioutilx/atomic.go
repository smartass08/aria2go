package ioutilx

import (
	"errors"
	"fmt"
	"os"
)

// AtomicWrite writes data atomically to path using a temp-file-and-rename
// strategy. It writes to <path>.tmp in the same directory, syncs, then
// atomically renames over the destination. If path already exists, its file
// mode is preserved; otherwise the provided perm is used. On any error the
// temp file is removed.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"

	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("atomic write: create temp: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write: write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write: sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write: close temp: %w", err)
	}

	mode := perm
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode()
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write: chmod temp: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic write: rename: %w", err)
	}
	return nil
}

// ErrAtomicWriterClosed is returned when writing to or closing an
// AtomicWriter that has already been committed or cancelled.
var ErrAtomicWriterClosed = errors.New("atomic writer: already committed or cancelled")

// AtomicWriter is an io.WriteCloser that buffers writes to a temp file and
// atomically commits on Close. It is not safe for concurrent use.
type AtomicWriter struct {
	tmp       *os.File
	path      string
	perm      os.FileMode
	committed bool
}

// NewAtomicWriter creates an AtomicWriter that writes to a temp file in the
// same directory as path. On Close, the temp file is atomically renamed over
// path. If path already exists, its permissions are preserved; otherwise the
// provided perm is used.
func NewAtomicWriter(path string, perm os.FileMode) (*AtomicWriter, error) {
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("atomic writer: create temp: %w", err)
	}
	return &AtomicWriter{tmp: f, path: path, perm: perm}, nil
}

// Write writes p to the underlying temp file. It returns an error if the
// writer has already been committed or cancelled.
func (w *AtomicWriter) Write(p []byte) (n int, err error) {
	if w.committed {
		return 0, ErrAtomicWriterClosed
	}
	return w.tmp.Write(p)
}

// Close syncs and closes the temp file, determines the correct permissions
// (preserving the original file's mode if it already exists), chmods the
// temp file, and atomically renames it over the destination. Returns an
// error if the writer has already been committed or cancelled. On error the
// temp file is removed.
func (w *AtomicWriter) Close() error {
	if w.committed {
		return ErrAtomicWriterClosed
	}
	w.committed = true

	tmpPath := w.tmp.Name()

	if err := w.tmp.Sync(); err != nil {
		w.tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("atomic writer: sync: %w", err)
	}
	if err := w.tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic writer: close temp: %w", err)
	}

	mode := w.perm
	if info, err := os.Stat(w.path); err == nil {
		mode = info.Mode()
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic writer: chmod: %w", err)
	}

	if err := os.Rename(tmpPath, w.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomic writer: rename: %w", err)
	}
	return nil
}

// Cancel aborts the write by closing and removing the temp file without
// touching the destination. It is safe to call multiple times.
func (w *AtomicWriter) Cancel() error {
	if w.committed {
		return nil
	}
	w.committed = true

	tmpPath := w.tmp.Name()
	w.tmp.Close()
	if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("atomic writer: cancel: %w", err)
	}
	return nil
}
