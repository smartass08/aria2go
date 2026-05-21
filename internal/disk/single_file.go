package disk

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// SingleFile implements Adaptor for a single output file.
// It wraps one *os.File and provides concurrent-safe I/O
// with optional BitTorrent piece tracking.
type SingleFile struct {
	path   string
	size   int64
	alloc  Allocator
	f      *os.File
	mu     sync.RWMutex
	closed atomic.Bool
	pieces []bool
}

// NewSingleFile creates or opens the output file at path, pre-allocates
// disk space using alloc, and returns a ready-to-use SingleFile.
//
// It first tries to open the file for read/write (O_RDWR) without
// truncation to preserve previously downloaded data (resume support)
// — matching aria2's openFile → openExistingFile flow.  If the file
// does not exist it creates parent directories and opens with
// O_CREAT|O_RDWR|O_TRUNC, matching aria2's createFile.
func NewSingleFile(path string, size int64, alloc Allocator) (*SingleFile, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0666)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, Wrap("open", path, err)
		}
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, Wrap("mkdir", path, err)
			}
		}
		f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
		if err != nil {
			return nil, Wrap("create", path, err)
		}
	}
	if err := alloc.Allocate(f, size); err != nil {
		f.Close()
		return nil, Wrap("alloc", path, err)
	}
	return &SingleFile{
		path:  path,
		size:  size,
		alloc: alloc,
		f:     f,
	}, nil
}

// OpenForWrite ensures the underlying file is open for read/write.
// It is idempotent: after the first call, subsequent calls return nil
// immediately unless the file has been closed.
func (sf *SingleFile) OpenForWrite() error {
	if sf.closed.Load() {
		return ErrFileClosed
	}
	sf.mu.Lock()
	defer sf.mu.Unlock()
	if sf.closed.Load() {
		return ErrFileClosed
	}
	if sf.f != nil {
		return nil
	}
	f, err := os.OpenFile(sf.path, os.O_RDWR, 0)
	if err != nil {
		return Wrap("open", sf.path, err)
	}
	sf.f = f
	return nil
}

// WriteAt writes p at absolute byte offset within the file.
// It returns ErrInvalidOffset if the write would exceed the file size.
// Safe for concurrent use; overlapping ranges are serialized.
func (sf *SingleFile) WriteAt(p []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, ErrInvalidOffset
	}
	if offset > sf.size {
		return 0, ErrInvalidOffset
	}
	if int64(len(p)) > sf.size-offset {
		return 0, ErrInvalidOffset
	}
	if sf.closed.Load() {
		return 0, ErrFileClosed
	}
	sf.mu.RLock()
	f := sf.f
	sf.mu.RUnlock()
	if f == nil {
		return 0, ErrFileClosed
	}
	n, err := f.WriteAt(p, offset)
	if err != nil {
		return n, Wrap("write", sf.path, err)
	}
	return n, nil
}

// ReadAt reads len(p) bytes at absolute byte offset within the file.
// Returns io.ReaderAt semantics: n < len(p) with a non-nil error on EOF.
// Safe for concurrent use.
func (sf *SingleFile) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, ErrInvalidOffset
	}
	if sf.closed.Load() {
		return 0, ErrFileClosed
	}
	sf.mu.RLock()
	defer sf.mu.RUnlock()
	if sf.closed.Load() {
		return 0, ErrFileClosed
	}
	n, err := sf.f.ReadAt(p, offset)
	if err != nil {
		return n, Wrap("read", sf.path, err)
	}
	return n, nil
}

// Size returns the total logical file size in bytes.
func (sf *SingleFile) Size() int64 {
	return sf.size
}

// Sync flushes all buffered writes to disk.
func (sf *SingleFile) Sync() error {
	if sf.closed.Load() {
		return ErrFileClosed
	}
	sf.mu.Lock()
	defer sf.mu.Unlock()
	if sf.closed.Load() {
		return ErrFileClosed
	}
	if sf.f == nil {
		return nil
	}
	if err := sf.f.Sync(); err != nil {
		return Wrap("sync", sf.path, err)
	}
	return nil
}

// Close closes the underlying file handle. Idempotent; after the first
// call returns, subsequent calls return nil and all other methods return
// ErrFileClosed.
func (sf *SingleFile) Close() error {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	if sf.closed.Load() {
		return nil
	}
	sf.closed.Store(true)
	if sf.f != nil {
		err := sf.f.Close()
		sf.f = nil
		if err != nil {
			return Wrap("close", sf.path, err)
		}
	}
	return nil
}

// SetPieceCount configures the number of pieces. Must be called once
// before any piece operations. Calling again with the same n is a
// no-op; calling with a different n clears the bitfield.
func (sf *SingleFile) SetPieceCount(n int) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	if len(sf.pieces) == n {
		return
	}
	sf.pieces = make([]bool, n)
}

func (sf *SingleFile) MarkPiece(i int, ok bool) {
	if sf.closed.Load() {
		return
	}
	sf.mu.Lock()
	defer sf.mu.Unlock()
	if i < 0 || i >= len(sf.pieces) {
		return
	}
	sf.pieces[i] = ok
}

func (sf *SingleFile) Have(i int) bool {
	sf.mu.RLock()
	defer sf.mu.RUnlock()
	if i < 0 || i >= len(sf.pieces) {
		return false
	}
	return sf.pieces[i]
}

// Bitfield returns a compact bitfield as []byte where each byte holds
// 8 pieces, MSB-first within each byte. Piece 0 is the MSB of byte 0.
// Length is (pieceCount + 7) / 8.
func (sf *SingleFile) Bitfield() []byte {
	sf.mu.RLock()
	defer sf.mu.RUnlock()
	n := len(sf.pieces)
	if n == 0 {
		return nil
	}
	numBytes := (n + 7) / 8
	bf := make([]byte, numBytes)
	for i := 0; i < n; i++ {
		if sf.pieces[i] {
			bf[i/8] |= 1 << (7 - uint(i%8))
		}
	}
	return bf
}

// CutTrailingGarbage truncates the file to its declared size if the actual
// file on disk is larger. This handles the case where a previous download
// was larger than the current one, leaving trailing garbage bytes.
func (sf *SingleFile) CutTrailingGarbage() error {
	if sf.closed.Load() {
		return ErrFileClosed
	}
	sf.mu.Lock()
	defer sf.mu.Unlock()

	if sf.closed.Load() {
		return ErrFileClosed
	}
	if sf.f == nil {
		return nil
	}

	fi, err := sf.f.Stat()
	if err != nil {
		return Wrap("stat", sf.path, err)
	}

	if fi.Size() > sf.size {
		if err := sf.f.Truncate(sf.size); err != nil {
			return Wrap("truncate", sf.path, err)
		}
	}
	return nil
}

// SetUtime sets the access and modification times on the file using
// os.Chtimes (equivalent to the utimes syscall used by aria2).
// For SingleFile, the file is always considered downloaded.
func (sf *SingleFile) SetUtime(atime, mtime time.Time) error {
	if sf.closed.Load() {
		return ErrFileClosed
	}
	sf.mu.RLock()
	path := sf.path
	sf.mu.RUnlock()

	if err := os.Chtimes(path, atime, mtime); err != nil {
		return Wrap("utime", path, err)
	}
	return nil
}

// Missing returns a sorted slice of indices for all pieces not yet
// marked complete. Returns nil if all pieces are complete.
func (sf *SingleFile) Missing() []int {
	sf.mu.RLock()
	defer sf.mu.RUnlock()
	if len(sf.pieces) == 0 {
		return nil
	}
	var missing []int
	for i, ok := range sf.pieces {
		if !ok {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}
