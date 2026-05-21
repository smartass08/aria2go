package engine

import (
	"fmt"
	"sort"

	"github.com/smartass08/aria2go/internal/disk"
)

// DownloadPlan holds the file layout, piece metadata, and base path for a
// download.
type DownloadPlan struct {
	files      []disk.FileEntry
	pieceHash  [][]byte
	pieceLen   int64
	totalLen   int64
	basePath   string
	fileFilter []bool

	knowsTotalLength bool
	checksumVerified bool
}

// NewDownloadPlan creates a DownloadPlan for a single-file download.
// The path is used as the default base path and as the first file entry.
func NewDownloadPlan(pieceLen int64, totalLen int64, basePath string) *DownloadPlan {
	return &DownloadPlan{
		files: []disk.FileEntry{
			{Name: basePath, Length: totalLen, Offset: 0},
		},
		pieceLen:         pieceLen,
		totalLen:         totalLen,
		basePath:         basePath,
		fileFilter:       []bool{true},
		knowsTotalLength: true,
	}
}

// Files returns the file entries. Exported for tests.
func (dc *DownloadPlan) Files() []disk.FileEntry {
	return dc.files
}

// SetFiles sets the file entries from a slice.
func (dc *DownloadPlan) SetFiles(files []disk.FileEntry) {
	dc.files = make([]disk.FileEntry, len(files))
	copy(dc.files, files)
	dc.fileFilter = make([]bool, len(files))
	for i := range dc.fileFilter {
		dc.fileFilter[i] = true
	}
}

// PieceLength returns the piece length.
func (dc *DownloadPlan) PieceLength() int64 { return dc.pieceLen }

// SetPieceLength sets the piece length.
func (dc *DownloadPlan) SetPieceLength(l int64) { dc.pieceLen = l }

// TotalLength returns the total download length. If no files are present it
// returns 0; otherwise it returns the final file's end offset.
func (dc *DownloadPlan) TotalLength() int64 {
	if len(dc.files) == 0 {
		return 0
	}
	f := dc.files[len(dc.files)-1]
	return f.Offset + f.Length
}

// KnowsTotalLength returns whether the total length is known.
func (dc *DownloadPlan) KnowsTotalLength() bool { return dc.knowsTotalLength }

// SetKnowsTotalLength sets whether the total length is known.
func (dc *DownloadPlan) SetKnowsTotalLength(v bool) { dc.knowsTotalLength = v }

// SetPieceHashes sets the piece hash type and hashes.
func (dc *DownloadPlan) SetPieceHashes(hashes [][]byte) {
	dc.pieceHash = hashes
}

// GetNumPieces returns the number of pieces. Returns 0 if pieceLen is 0;
// otherwise it rounds the final file's end offset up to a full piece count.
func (dc *DownloadPlan) GetNumPieces() int {
	if dc.pieceLen == 0 {
		return 0
	}
	if len(dc.files) == 0 {
		return 0
	}
	lastOff := dc.files[len(dc.files)-1].Offset + dc.files[len(dc.files)-1].Length
	return int((lastOff + dc.pieceLen - 1) / dc.pieceLen)
}

// GetPieceHash returns the piece hash at the given index. Returns nil, false
// if the index is out of range.
func (dc *DownloadPlan) GetPieceHash(idx int) ([]byte, bool) {
	if idx < 0 || idx >= len(dc.pieceHash) {
		return nil, false
	}
	return dc.pieceHash[idx], true
}

// GetBasePath returns the base path. If basePath is empty, it returns the
// first file entry's name.
func (dc *DownloadPlan) GetBasePath() string {
	if dc.basePath != "" {
		return dc.basePath
	}
	if len(dc.files) == 0 {
		return ""
	}
	return dc.files[0].Name
}

// SetBasePath sets the base path.
func (dc *DownloadPlan) SetBasePath(path string) {
	dc.basePath = path
}

// FindFileEntryByOffset finds the file entry containing the given absolute
// byte offset. Returns the index and nil error on success. Returns -1 and an
// error if files are empty or offset is past the total length.
func (dc *DownloadPlan) FindFileEntryByOffset(offset int64) (int, error) {
	if len(dc.files) == 0 {
		return -1, fmt.Errorf("engine: no file entries")
	}

	last := dc.files[len(dc.files)-1]
	if offset > 0 && last.Offset+last.Length <= offset {
		return -1, fmt.Errorf("engine: offset %d out of range (total=%d)", offset, last.Offset+last.Length)
	}

	// Find the first file whose offset is greater than the requested offset.
	idx := sort.Search(len(dc.files), func(i int) bool {
		return dc.files[i].Offset > offset
	})

	if idx < len(dc.files) && dc.files[idx].Offset == offset {
		return idx, nil
	}
	return idx - 1, nil
}

// SetFileFilter marks which files are requested based on the filter slice.
// If filter is empty or only one file, all files are requested.
// Otherwise, filter entries mark which files are requested.
func (dc *DownloadPlan) SetFileFilter(filter []bool) {
	if filter == nil {
		dc.fileFilter = make([]bool, len(dc.files))
		for i := range dc.fileFilter {
			dc.fileFilter[i] = true
		}
		return
	}
	dc.fileFilter = make([]bool, len(dc.files))
	for i := range dc.fileFilter {
		if i < len(filter) {
			dc.fileFilter[i] = filter[i]
		} else {
			dc.fileFilter[i] = false
		}
	}
}

// FileFilter returns the file filter.
func (dc *DownloadPlan) FileFilter() []bool {
	return dc.fileFilter
}
