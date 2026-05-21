package contracts

import (
	"context"
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

// TestInterfaceSatisfaction verifies that mock implementations satisfy each
// interface and that the Verify constants have correct values.
func TestInterfaceSatisfaction(t *testing.T) {
	var _ TorrentStatusProjector = (*mockStatusProjector)(nil)
	var _ FilePieceMap = (*mockFilePieceMap)(nil)
	var _ TorrentLifecycleControl = (*mockLifecycleControl)(nil)
	var _ TorrentRPCProjection = (*mockRPCProjection)(nil)

	if VerifyOK != 0 {
		t.Errorf("VerifyOK = %d, want 0", VerifyOK)
	}
	if VerifyMissing != -1 {
		t.Errorf("VerifyMissing = %d, want -1", VerifyMissing)
	}
	if VerifyBad != -2 {
		t.Errorf("VerifyBad = %d, want -2", VerifyBad)
	}
}

// TestFileSliceFields verifies FileSlice can be constructed and accessed.
func TestFileSliceFields(t *testing.T) {
	fs := FileSlice{
		Index:      3,
		Path:       "path/to/file.dat",
		FirstPiece: 10,
		LastPiece:  15,
		Length:     1 << 20,
		Selected:   true,
	}
	if fs.Index != 3 {
		t.Errorf("Index = %d, want 3", fs.Index)
	}
	if fs.Path != "path/to/file.dat" {
		t.Errorf("Path = %s, want path/to/file.dat", fs.Path)
	}
	if fs.FirstPiece != 10 {
		t.Errorf("FirstPiece = %d, want 10", fs.FirstPiece)
	}
	if fs.LastPiece != 15 {
		t.Errorf("LastPiece = %d, want 15", fs.LastPiece)
	}
	if fs.Length != 1<<20 {
		t.Errorf("Length = %d, want %d", fs.Length, 1<<20)
	}
	if !fs.Selected {
		t.Errorf("Selected = false, want true")
	}
}

type mockStatusProjector struct {
	project func(gid core.GID, keys []string) map[string]any
}

func (m *mockStatusProjector) Project(gid core.GID, keys []string) map[string]any {
	if m.project != nil {
		return m.project(gid, keys)
	}
	return nil
}

type mockFilePieceMap struct {
	files         func(gid core.GID) []FileSlice
	piecesForFile func(gid core.GID, idx int) (int, int)
}

func (m *mockFilePieceMap) Files(gid core.GID) []FileSlice {
	if m.files != nil {
		return m.files(gid)
	}
	return nil
}

func (m *mockFilePieceMap) PiecesForFile(gid core.GID, idx int) (int, int) {
	if m.piecesForFile != nil {
		return m.piecesForFile(gid, idx)
	}
	return 0, 0
}

type mockLifecycleControl struct {
	pause     func() error
	stop      func(force bool) error
	rehashAll func(ctx context.Context) error
	verify    func(ctx context.Context) ([]int, error)
}

func (m *mockLifecycleControl) Pause() error {
	if m.pause != nil {
		return m.pause()
	}
	return nil
}

func (m *mockLifecycleControl) Stop(force bool) error {
	if m.stop != nil {
		return m.stop(force)
	}
	return nil
}

func (m *mockLifecycleControl) RehashAll(ctx context.Context) error {
	if m.rehashAll != nil {
		return m.rehashAll(ctx)
	}
	return nil
}

func (m *mockLifecycleControl) Verify(ctx context.Context) ([]int, error) {
	if m.verify != nil {
		return m.verify(ctx)
	}
	return nil, nil
}

type mockRPCProjection struct {
	peers   func(gid core.GID) []map[string]any
	servers func(gid core.GID) []map[string]any
}

func (m *mockRPCProjection) Peers(gid core.GID) []map[string]any {
	if m.peers != nil {
		return m.peers(gid)
	}
	return nil
}

func (m *mockRPCProjection) Servers(gid core.GID) []map[string]any {
	if m.servers != nil {
		return m.servers(gid)
	}
	return nil
}
