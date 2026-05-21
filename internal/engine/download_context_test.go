package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/disk"
)

func TestNewDownloadPlan(t *testing.T) {
	dc := NewDownloadPlan(1024, 5000, "/downloads/file.iso")

	if dc.PieceLength() != 1024 {
		t.Errorf("PieceLength = %d, want 1024", dc.PieceLength())
	}
	if dc.TotalLength() != 5000 {
		t.Errorf("TotalLength = %d, want 5000", dc.TotalLength())
	}
	if dc.GetBasePath() != "/downloads/file.iso" {
		t.Errorf("GetBasePath = %q, want /downloads/file.iso", dc.GetBasePath())
	}
	if len(dc.Files()) != 1 {
		t.Fatalf("Files len = %d, want 1", len(dc.Files()))
	}
	if dc.Files()[0].Name != "/downloads/file.iso" {
		t.Errorf("File[0].Name = %q, want /downloads/file.iso", dc.Files()[0].Name)
	}
	if dc.Files()[0].Length != 5000 {
		t.Errorf("File[0].Length = %d, want 5000", dc.Files()[0].Length)
	}
	if dc.Files()[0].Offset != 0 {
		t.Errorf("File[0].Offset = %d, want 0", dc.Files()[0].Offset)
	}
}

func TestGetNumPieces(t *testing.T) {
	tests := []struct {
		name     string
		files    []disk.FileEntry
		pieceLen int64
		want     int
	}{
		{
			name:     "single file aligned",
			files:    []disk.FileEntry{{Name: "a", Length: 4096, Offset: 0}},
			pieceLen: 1024,
			want:     4,
		},
		{
			name:     "single file unaligned",
			files:    []disk.FileEntry{{Name: "a", Length: 5000, Offset: 0}},
			pieceLen: 1024,
			want:     5, // ceil(5000/1024) = 5
		},
		{
			name: "multiple files",
			files: []disk.FileEntry{
				{Name: "a", Length: 3000, Offset: 0},
				{Name: "b", Length: 2000, Offset: 3000},
			},
			pieceLen: 1024,
			want:     5, // ceil(5000/1024) = 5
		},
		{
			name:     "zero piece length",
			files:    []disk.FileEntry{{Name: "a", Length: 5000, Offset: 0}},
			pieceLen: 0,
			want:     0,
		},
		{
			name:     "empty files",
			files:    []disk.FileEntry{},
			pieceLen: 1024,
			want:     0,
		},
		{
			name:     "exact boundary",
			files:    []disk.FileEntry{{Name: "a", Length: 1024, Offset: 0}},
			pieceLen: 1024,
			want:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dc := NewDownloadPlan(tt.pieceLen, 0, "")
			dc.SetFiles(tt.files)
			if got := dc.GetNumPieces(); got != tt.want {
				t.Errorf("GetNumPieces() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetPieceHash(t *testing.T) {
	dc := NewDownloadPlan(1024, 5000, "/test")
	hashes := [][]byte{
		{0x01, 0x02},
		{0x03, 0x04},
		{0x05, 0x06},
	}
	dc.SetPieceHashes(hashes)

	tests := []struct {
		idx      int
		wantOK   bool
		wantHash []byte
	}{
		{0, true, []byte{0x01, 0x02}},
		{1, true, []byte{0x03, 0x04}},
		{2, true, []byte{0x05, 0x06}},
		{3, false, nil},  // out of range
		{-1, false, nil}, // negative
	}

	for _, tt := range tests {
		got, ok := dc.GetPieceHash(tt.idx)
		if ok != tt.wantOK {
			t.Errorf("GetPieceHash(%d) ok = %v, want %v", tt.idx, ok, tt.wantOK)
		}
		if ok && string(got) != string(tt.wantHash) {
			t.Errorf("GetPieceHash(%d) = %v, want %v", tt.idx, got, tt.wantHash)
		}
	}
}

func TestGetPieceHash_Empty(t *testing.T) {
	dc := NewDownloadPlan(1024, 5000, "/test")
	got, ok := dc.GetPieceHash(0)
	if ok {
		t.Error("GetPieceHash(0) should return false for empty hashes")
	}
	if got != nil {
		t.Errorf("GetPieceHash(0) = %v, want nil", got)
	}
}

func TestGetBasePath(t *testing.T) {
	// With explicit base path.
	dc := NewDownloadPlan(1024, 5000, "/downloads/file.iso")
	dc.SetBasePath("/custom/path")
	if got := dc.GetBasePath(); got != "/custom/path" {
		t.Errorf("GetBasePath = %q, want /custom/path", got)
	}

	// Without explicit base path, should return first file's Name.
	dc2 := NewDownloadPlan(1024, 5000, "/downloads/file.iso")
	if got := dc2.GetBasePath(); got != "/downloads/file.iso" {
		t.Errorf("GetBasePath = %q, want /downloads/file.iso", got)
	}

	// Empty files.
	dc3 := &DownloadPlan{}
	if got := dc3.GetBasePath(); got != "" {
		t.Errorf("GetBasePath = %q, want empty", got)
	}
}

func TestFindFileEntryByOffset(t *testing.T) {
	dc := NewDownloadPlan(0, 0, "")
	dc.SetFiles([]disk.FileEntry{
		{Name: "a.txt", Length: 1000, Offset: 0},
		{Name: "b.txt", Length: 2000, Offset: 1000},
		{Name: "c.txt", Length: 3000, Offset: 3000},
	})

	tests := []struct {
		offset  int64
		wantIdx int
		wantErr bool
	}{
		{0, 0, false},
		{500, 0, false},
		{999, 0, false},
		{1000, 1, false},
		{2000, 1, false},
		{2999, 1, false},
		{3000, 2, false},
		{4000, 2, false},
		{5999, 2, false},
		{6000, -1, true}, // past end
		{7000, -1, true}, // past end
	}

	for _, tt := range tests {
		idx, err := dc.FindFileEntryByOffset(tt.offset)
		if tt.wantErr && err == nil {
			t.Errorf("FindFileEntryByOffset(%d) expected error", tt.offset)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("FindFileEntryByOffset(%d) unexpected error: %v", tt.offset, err)
		}
		if idx != tt.wantIdx {
			t.Errorf("FindFileEntryByOffset(%d) = %d, want %d", tt.offset, idx, tt.wantIdx)
		}
	}
}

func TestFindFileEntryByOffset_ExactBoundaries(t *testing.T) {
	dc := NewDownloadPlan(0, 0, "")
	dc.SetFiles([]disk.FileEntry{
		{Name: "a", Length: 1024, Offset: 0},
		{Name: "b", Length: 2048, Offset: 1024},
	})

	idx, err := dc.FindFileEntryByOffset(1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 1 {
		t.Errorf("offset 1024 = idx %d, want 1", idx)
	}

	idx, err = dc.FindFileEntryByOffset(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != 0 {
		t.Errorf("offset 0 = idx %d, want 0", idx)
	}
}

func TestFindFileEntryByOffset_EmptyFiles(t *testing.T) {
	dc := &DownloadPlan{}
	_, err := dc.FindFileEntryByOffset(0)
	if err == nil {
		t.Error("expected error for empty files")
	}
}

func TestSetFileFilter(t *testing.T) {
	dc := NewDownloadPlan(0, 0, "")
	dc.SetFiles([]disk.FileEntry{
		{Name: "a"}, {Name: "b"}, {Name: "c"},
	})

	dc.SetFileFilter([]bool{true, false, true})
	filter := dc.FileFilter()
	if len(filter) != 3 {
		t.Fatalf("filter len = %d, want 3", len(filter))
	}
	if !filter[0] || filter[1] || !filter[2] {
		t.Errorf("filter = %v, want [true false true]", filter)
	}
}

func TestSetFileFilter_ShorterFilter(t *testing.T) {
	dc := NewDownloadPlan(0, 0, "")
	dc.SetFiles([]disk.FileEntry{
		{Name: "a"}, {Name: "b"}, {Name: "c"},
	})

	dc.SetFileFilter([]bool{true})
	filter := dc.FileFilter()
	if len(filter) != 3 {
		t.Fatalf("filter len = %d, want 3", len(filter))
	}
	if !filter[0] || filter[1] || filter[2] {
		t.Errorf("filter = %v, want [true false false]", filter)
	}
}

func TestSetFileFilter_Nil(t *testing.T) {
	dc := NewDownloadPlan(0, 0, "")
	dc.SetFiles([]disk.FileEntry{
		{Name: "a"}, {Name: "b"},
	})

	dc.SetFileFilter(nil)
	filter := dc.FileFilter()
	if len(filter) != 2 {
		t.Fatalf("filter len = %d, want 2", len(filter))
	}
	if !filter[0] || !filter[1] {
		t.Errorf("nil filter should default to all true, got %v", filter)
	}
}

func TestTotalLength(t *testing.T) {
	dc := NewDownloadPlan(0, 0, "")
	dc.SetFiles([]disk.FileEntry{
		{Name: "a", Length: 100, Offset: 0},
		{Name: "b", Length: 200, Offset: 100},
	})
	if got := dc.TotalLength(); got != 300 {
		t.Errorf("TotalLength = %d, want 300", got)
	}

	dc2 := &DownloadPlan{}
	if got := dc2.TotalLength(); got != 0 {
		t.Errorf("TotalLength empty = %d, want 0", got)
	}
}
