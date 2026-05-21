package disk

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MultiFile implements Adaptor for multiple output files (e.g., multi-file
// torrents). It maps absolute byte offsets to the correct underlying file
// using the FileEntry offset/length metadata.
type MultiFile struct {
	dir         string
	files       []FileEntry
	pieceLen    int64
	totalSize   int64
	alloc       Allocator
	mu          sync.RWMutex
	closed      bool
	opened      bool
	pieces      *pieceMap
	fileHandles []*os.File
}

// NewMultiFile creates a MultiFile adaptor rooted at dir with the given file
// entries. The files slice must be sorted by Offset ascending and
// non-overlapping. No filesystem operations occur during construction; files
// are created during OpenForWrite.
func NewMultiFile(dir string, files []FileEntry, pieceLen int64, alloc Allocator) (*MultiFile, error) {
	if dir == "" {
		return nil, fmt.Errorf("disk: empty directory")
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("disk: no files provided")
	}
	for i := 1; i < len(files); i++ {
		prev := files[i-1]
		curr := files[i]
		if curr.Offset < prev.Offset {
			return nil, fmt.Errorf("disk: files not sorted by offset: %q offset %d before %q offset %d",
				curr.Name, curr.Offset, prev.Name, prev.Offset)
		}
		if curr.Offset < prev.Offset+prev.Length {
			return nil, fmt.Errorf("disk: overlapping file entries: %q [%d,%d) overlaps %q [%d,%d)",
				curr.Name, curr.Offset, curr.Offset+curr.Length,
				prev.Name, prev.Offset, prev.Offset+prev.Length)
		}
	}
	last := files[len(files)-1]
	totalSize := last.Offset + last.Length
	return &MultiFile{
		dir:         dir,
		files:       append([]FileEntry(nil), files...),
		pieceLen:    pieceLen,
		totalSize:   totalSize,
		alloc:       alloc,
		fileHandles: make([]*os.File, len(files)),
	}, nil
}

// OpenForWrite creates the output directory and all files. Idempotent after
// the first call.
func (mf *MultiFile) OpenForWrite() error {
	mf.mu.Lock()
	defer mf.mu.Unlock()

	if mf.closed {
		return ErrFileClosed
	}
	if mf.opened {
		return nil
	}

	if err := os.MkdirAll(mf.dir, 0755); err != nil {
		return Wrap("mkdir", mf.dir, err)
	}

	cleanup := func(upTo int) {
		for j := 0; j <= upTo; j++ {
			if mf.fileHandles[j] != nil {
				mf.fileHandles[j].Close()
				mf.fileHandles[j] = nil
			}
		}
	}

	for i, fe := range mf.files {
		if fe.Length == 0 {
			continue
		}

		filePath := filepath.Join(mf.dir, filepath.FromSlash(fe.Name))

		if subDir := filepath.Dir(filePath); subDir != mf.dir {
			if err := os.MkdirAll(subDir, 0755); err != nil {
				cleanup(i - 1)
				return Wrap("mkdir", subDir, err)
			}
		}

		f, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			cleanup(i - 1)
			return Wrap("open", filePath, err)
		}
		mf.fileHandles[i] = f

		fi, statErr := f.Stat()
		if statErr != nil {
			cleanup(i)
			return Wrap("stat", filePath, statErr)
		}

		if fi.Size() > fe.Length {
			if err := f.Truncate(fe.Length); err != nil {
				cleanup(i)
				return Wrap("truncate", filePath, err)
			}
		}

		if err := mf.alloc.Allocate(f, fe.Length); err != nil {
			cleanup(i)
			return Wrap("allocate", filePath, err)
		}
	}

	mf.opened = true
	return nil
}

// WriteAt writes len(p) bytes at absolute byte offset. A single call may span
// multiple files if the range crosses a FileEntry boundary.
func (mf *MultiFile) WriteAt(p []byte, offset int64) (int, error) {
	mf.mu.RLock()
	if mf.closed {
		mf.mu.RUnlock()
		return 0, ErrFileClosed
	}
	if !mf.opened {
		mf.mu.RUnlock()
		return 0, fmt.Errorf("disk: OpenForWrite not called")
	}
	handles := mf.fileHandles
	total := mf.totalSize
	mf.mu.RUnlock()

	if offset < 0 || offset > total {
		return 0, ErrInvalidOffset
	}
	if len(p) == 0 {
		return 0, nil
	}

	rem := p
	off := offset
	written := 0

	for len(rem) > 0 {
		fidx, fileOff := fileAtOffset(mf.files, off)
		if fidx < 0 {
			return written, Wrap("write", "", fmt.Errorf("gap at offset %d", off))
		}

		fe := mf.files[fidx]
		fh := handles[fidx]
		if fh == nil {
			return written, Wrap("write", fe.Name, fmt.Errorf("zero-length file"))
		}

		toWrite := min(int64(len(rem)), fe.Length-fileOff)
		n, err := fh.WriteAt(rem[:toWrite], fileOff)
		written += n
		if err != nil {
			return written, Wrap("write", fe.Name, err)
		}
		if n < int(toWrite) {
			return written, fmt.Errorf("disk: short write to %q: wrote %d of %d", fe.Name, n, toWrite)
		}

		rem = rem[n:]
		off += int64(n)
	}

	return written, nil
}

// ReadAt reads len(p) bytes at absolute byte offset. Returns io.ReaderAt
// semantics: n < len(p) with a non-nil error on EOF or boundary errors.
// Reads that fall in gaps between files produce zero-filled bytes.
func (mf *MultiFile) ReadAt(p []byte, offset int64) (int, error) {
	mf.mu.RLock()
	if mf.closed {
		mf.mu.RUnlock()
		return 0, ErrFileClosed
	}
	if !mf.opened {
		mf.mu.RUnlock()
		return 0, fmt.Errorf("disk: OpenForWrite not called")
	}
	handles := mf.fileHandles
	total := mf.totalSize
	mf.mu.RUnlock()

	if len(p) == 0 {
		return 0, nil
	}
	if offset >= total {
		return 0, io.EOF
	}

	rem := p
	off := offset
	read := 0

	for len(rem) > 0 {
		if off >= total {
			return read, io.EOF
		}

		fidx, fileOff := fileAtOffset(mf.files, off)
		if fidx < 0 {
			nextOff := nextFileOffset(mf.files, off)
			if nextOff < 0 {
				nextOff = total
			}
			gapLen := min(int64(len(rem)), nextOff-off)
			clear(rem[:gapLen])
			read += int(gapLen)
			rem = rem[gapLen:]
			off += gapLen
			continue
		}

		fe := mf.files[fidx]
		fh := handles[fidx]
		if fh == nil || fe.Length == 0 {
			clear(rem[:1])
			read++
			rem = rem[1:]
			off++
			continue
		}

		toRead := min(int64(len(rem)), fe.Length-fileOff)
		n, err := fh.ReadAt(rem[:toRead], fileOff)
		read += n
		if err != nil && err != io.EOF {
			return read, Wrap("read", fe.Name, err)
		}
		if shortfall := int(toRead) - n; shortfall > 0 {
			clear(rem[n : n+shortfall])
			read += shortfall
		}
		rem = rem[toRead:]
		off += toRead
		if err == io.EOF && off >= total {
			return read, io.EOF
		}
	}

	if off >= total {
		return read, io.EOF
	}
	return read, nil
}

// Size returns the total logical size in bytes.
func (mf *MultiFile) Size() int64 {
	return mf.totalSize
}

// Sync flushes all buffered writes to disk for every open file.
func (mf *MultiFile) Sync() error {
	mf.mu.RLock()
	if mf.closed {
		mf.mu.RUnlock()
		return ErrFileClosed
	}
	handles := mf.fileHandles
	mf.mu.RUnlock()

	var firstErr error
	for _, fh := range handles {
		if fh != nil {
			if err := fh.Sync(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Close closes all open file handles. Idempotent; after the first call
// returns, subsequent calls return nil and all other methods return
// ErrFileClosed.
func (mf *MultiFile) Close() error {
	mf.mu.Lock()
	defer mf.mu.Unlock()

	if mf.closed {
		return nil
	}
	mf.closed = true

	var firstErr error
	for i, fh := range mf.fileHandles {
		if fh != nil {
			if err := fh.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
			mf.fileHandles[i] = nil
		}
	}
	return firstErr
}

// SetPieceCount configures the number of pieces. Must be called once before
// any piece operations. Calling again with the same n is a no-op; calling
// with a different n clears the bitfield.
func (mf *MultiFile) SetPieceCount(n int) {
	if mf.pieces == nil {
		mf.pieces = newPieceMap(int32(mf.pieceLen), int64(mf.pieceLen)*int64(n))
		return
	}
	mf.pieces.setCount(n)
}

// MarkPiece marks piece i as complete (ok=true) or incomplete (ok=false).
func (mf *MultiFile) MarkPiece(i int, ok bool) {
	if mf.pieces != nil {
		mf.pieces.mark(i, ok)
	}
}

// Have reports whether piece i is marked complete.
func (mf *MultiFile) Have(i int) bool {
	if mf.pieces == nil {
		return false
	}
	return mf.pieces.have(i)
}

// Bitfield returns a compact bitfield as []byte where each byte holds 8
// pieces, MSB-first within each byte.
func (mf *MultiFile) Bitfield() []byte {
	if mf.pieces == nil {
		return nil
	}
	return mf.pieces.bitfield()
}

// Missing returns a sorted slice of indices for all pieces not yet marked
// complete.
func (mf *MultiFile) Missing() []int {
	if mf.pieces == nil {
		return nil
	}
	return mf.pieces.missing()
}

// CutTrailingGarbage iterates all files and truncates each one whose
// on-disk size exceeds the declared FileEntry.Length. This cleans up
// trailing garbage bytes from a previous larger download.
func (mf *MultiFile) CutTrailingGarbage() error {
	mf.mu.RLock()
	if mf.closed {
		mf.mu.RUnlock()
		return ErrFileClosed
	}
	files := mf.files
	handles := mf.fileHandles
	dir := mf.dir
	mf.mu.RUnlock()

	var firstErr error
	for i, fe := range files {
		if fe.Length == 0 {
			continue
		}

		filePath := filepath.Join(dir, filepath.FromSlash(fe.Name))

		var currentSize int64
		if handles != nil && i < len(handles) && handles[i] != nil {
			fi, err := handles[i].Stat()
			if err != nil {
				if firstErr == nil {
					firstErr = Wrap("stat", filePath, err)
				}
				continue
			}
			currentSize = fi.Size()
		} else {
			fi, err := os.Stat(filePath)
			if err != nil {
				if firstErr == nil {
					firstErr = Wrap("stat", filePath, err)
				}
				continue
			}
			currentSize = fi.Size()
		}

		if currentSize > fe.Length {
			if handles != nil && i < len(handles) && handles[i] != nil {
				if err := handles[i].Truncate(fe.Length); err != nil {
					if firstErr == nil {
						firstErr = Wrap("truncate", filePath, err)
					}
				}
			} else {
				if err := os.Truncate(filePath, fe.Length); err != nil {
					if firstErr == nil {
						firstErr = Wrap("truncate", filePath, err)
					}
				}
			}
		}
	}
	return firstErr
}

// SetUtime sets access and modification times on all requested files.
// Only regular files that have been selected for download (Requested)
// are affected. Returns the number of files successfully changed.
func (mf *MultiFile) SetUtime(atime, mtime time.Time) (int, error) {
	mf.mu.RLock()
	if mf.closed {
		mf.mu.RUnlock()
		return 0, ErrFileClosed
	}
	files := mf.files
	dir := mf.dir
	mf.mu.RUnlock()

	var firstErr error
	numOK := 0
	for _, fe := range files {
		if !fe.Requested {
			continue
		}
		filePath := filepath.Join(dir, filepath.FromSlash(fe.Name))
		fi, err := os.Stat(filePath)
		if err != nil {
			if firstErr == nil {
				firstErr = Wrap("stat", filePath, err)
			}
			continue
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		if err := os.Chtimes(filePath, atime, mtime); err != nil {
			if firstErr == nil {
				firstErr = Wrap("utime", filePath, err)
			}
			continue
		}
		numOK++
	}
	return numOK, firstErr
}

// SetRequested marks the given file indices as requested (selected for
// download). Only requested files are affected by SetUtime.
func (mf *MultiFile) SetRequested(indices []int) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	for _, i := range indices {
		if i >= 0 && i < len(mf.files) {
			mf.files[i].Requested = true
		}
	}
}

// SetAllRequested marks all files as requested.
func (mf *MultiFile) SetAllRequested() {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	for i := range mf.files {
		mf.files[i].Requested = true
	}
}

func fileAtOffset(files []FileEntry, offset int64) (int, int64) {
	lo, hi := 0, len(files)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if files[mid].Offset+files[mid].Length <= offset {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(files) {
		return -1, 0
	}
	fe := files[lo]
	if offset >= fe.Offset && offset < fe.Offset+fe.Length {
		return lo, offset - fe.Offset
	}
	return -1, 0
}

func nextFileOffset(files []FileEntry, offset int64) int64 {
	for _, fe := range files {
		if fe.Offset > offset {
			return fe.Offset
		}
	}
	return -1
}
