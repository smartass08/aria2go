package disk

// FileEntry describes one file in a multi-file download.
// It is a value type, safe to copy.
type FileEntry struct {
	Name      string // relative path within the download directory
	Length    int64  // file size in bytes
	Offset    int64  // cumulative byte offset from start of the multi-file aggregate
	Requested bool   // whether this file is selected for download
}

// Adaptor is the disk I/O abstraction used by the engine.
// It supports both single-file and multi-file downloads,
// with optional BitTorrent piece tracking.
//
// WriteAt and ReadAt are safe for concurrent use by multiple goroutines.
// Operations on disjoint byte ranges proceed without blocking each other;
// operations on overlapping ranges are serialized.
//
// Close is idempotent: after the first call completes, subsequent calls
// return nil immediately and all other methods return ErrFileClosed.
type Adaptor interface {
	// OpenForWrite opens or prepares the underlying files for writing.
	// For SingleFile, opens the target path. For MultiFile, creates the
	// directory and all files. Idempotent after first call.
	OpenForWrite() error

	// WriteAt writes len(p) bytes at absolute byte offset within the
	// logical file region. Safe for concurrent use.
	WriteAt(p []byte, offset int64) (int, error)

	// ReadAt reads len(p) bytes at absolute byte offset within the
	// logical file region. Safe for concurrent use. Returns io.ReaderAt
	// semantics: n < len(p) with a non-nil error on EOF.
	ReadAt(p []byte, offset int64) (int, error)

	// Size returns the total logical size in bytes.
	Size() int64

	// Sync flushes all buffered writes to disk. Returns the first error
	// encountered across all underlying files.
	Sync() error

	// Close closes all underlying file handles. Idempotent; after the
	// first call returns, subsequent calls return nil and all other
	// methods return ErrFileClosed.
	Close() error

	// SetPieceCount configures the number of pieces. Must be called once
	// before any piece operations. Calling again with the same n is a
	// no-op; calling with a different n clears the bitfield.
	SetPieceCount(n int)

	// MarkPiece marks piece i as complete (ok=true) or incomplete
	// (ok=false).
	MarkPiece(i int, ok bool)

	// Have reports whether piece i is marked complete. A piece is marked
	// Have only after its data has been fully written to disk AND
	// hash-verified against the expected value.
	Have(i int) bool

	// Bitfield returns a compact bitfield as []byte where each byte
	// holds 8 pieces, MSB-first within each byte. Piece 0 is the MSB of
	// byte 0. Bit i is set if piece i is verified. Length is
	// (pieceCount + 7) / 8.
	Bitfield() []byte

	// Missing returns a sorted slice of indices for all pieces not yet
	// marked complete. Returns nil if all pieces are complete.
	Missing() []int
}

// TotalSize computes the sum of all FileEntry.Length values.
func TotalSize(files []FileEntry) int64 {
	var total int64
	for _, f := range files {
		total += f.Length
	}
	return total
}
