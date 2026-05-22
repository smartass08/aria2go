package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
)

func newRPCTestEngine(t *testing.T) *Engine {
	t.Helper()

	e, err := New(&config.Options{
		Dir:                    t.TempDir(),
		MaxConcurrentDownloads: 1,
		MaxDownloadResult:      10,
	}, testLogger(t))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return e
}

func TestTellStatusStoppedPreservesSnapshot(t *testing.T) {
	e := newRPCTestEngine(t)

	gid, err := e.Add(AddSpec{URIs: []string{"http://example.com/archive.iso"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	if err := e.Remove(gid, false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	status, err := e.TellStatus(gid)
	if err != nil {
		t.Fatalf("TellStatus() error = %v", err)
	}
	if status.Status != core.StatusRemoved {
		t.Fatalf("status = %s, want removed", status.Status)
	}
	if status.ErrorCode != core.ExitRemoved {
		t.Fatalf("errorCode = %v, want %v", status.ErrorCode, core.ExitRemoved)
	}
	if len(status.Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(status.Files))
	}
	if len(status.Files[0].URIs) != 2 {
		t.Fatalf("len(uris) = %d, want 2", len(status.Files[0].URIs))
	}
	if got := status.Files[0].URIs[0]; got.Status != "used" || got.URI != "http://example.com/archive.iso" {
		t.Fatalf("first stopped URI = %+v, want used archive URI", got)
	}
	if got := status.Files[0].URIs[1]; got.Status != "waiting" || got.URI != "http://example.com/archive.iso" {
		t.Fatalf("second stopped URI = %+v, want waiting archive URI", got)
	}
}

func TestTellStoppedUsesFullSnapshot(t *testing.T) {
	e := newRPCTestEngine(t)

	gid, err := e.Add(AddSpec{URIs: []string{"http://example.com/archive.iso"}})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	e.fillRequestGroupFromReserver()
	if err := e.Remove(gid, false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	stopped := e.TellStopped(0, 10)
	if len(stopped) != 1 {
		t.Fatalf("len(stopped) = %d, want 1", len(stopped))
	}
	if stopped[0].GID != gid {
		t.Fatalf("gid = %s, want %s", stopped[0].GID, gid)
	}
	if len(stopped[0].Files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(stopped[0].Files))
	}
	if stopped[0].Dir == "" {
		t.Fatal("dir is empty")
	}
}
