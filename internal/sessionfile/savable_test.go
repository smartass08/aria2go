package sessionfile

import (
	"bytes"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

func TestShouldSave_AllStatuses(t *testing.T) {
	tests := []struct {
		status core.Status
		want   bool
	}{
		{core.StatusWaiting, true},
		{core.StatusActive, true},
		{core.StatusPaused, true},
		{core.StatusComplete, false},
		{core.StatusError, false},
		{core.StatusRemoved, false},
	}
	for _, tt := range tests {
		got := ShouldSave(tt.status)
		if got != tt.want {
			t.Errorf("ShouldSave(%s) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestHasAnyURI(t *testing.T) {
	if HasAnyURI(Entry{URIs: nil}) {
		t.Error("HasAnyURI(nil URIs) = true, want false")
	}
	if HasAnyURI(Entry{URIs: []string{}}) {
		t.Error("HasAnyURI(empty URIs) = true, want false")
	}
	if !HasAnyURI(Entry{URIs: []string{"https://example.com/file.zip"}}) {
		t.Error("HasAnyURI(with URI) = false, want true")
	}
}

func TestDedupURIs_AllSpent(t *testing.T) {
	uris := []string{"https://a.example/file", "https://b.example/file"}
	spent := []string{"https://a.example/file", "https://b.example/file"}
	remaining, dedupedSpent := DedupURIs(uris, spent)
	if len(remaining) != 0 {
		t.Errorf("remaining = %v, want empty", remaining)
	}
	if len(dedupedSpent) != 2 {
		t.Errorf("dedupedSpent = %v, want 2 URIs", dedupedSpent)
	}
}

func TestDedupURIs_SomeSpent(t *testing.T) {
	uris := []string{"https://a.example/file", "https://b.example/file", "https://c.example/file"}
	spent := []string{"https://b.example/file"}
	remaining, dedupedSpent := DedupURIs(uris, spent)
	if len(remaining) != 2 {
		t.Errorf("remaining = %v, want 2 URIs", remaining)
	}
	if remaining[0] != "https://a.example/file" || remaining[1] != "https://c.example/file" {
		t.Errorf("remaining = %v, want [a, c]", remaining)
	}
	if len(dedupedSpent) != 1 || dedupedSpent[0] != "https://b.example/file" {
		t.Errorf("dedupedSpent = %v, want [b]", dedupedSpent)
	}
}

func TestDedupURIs_NoSpent(t *testing.T) {
	uris := []string{"https://a.example/file", "https://b.example/file"}
	spent := []string{}
	remaining, dedupedSpent := DedupURIs(uris, spent)
	if len(remaining) != 2 {
		t.Errorf("remaining = %v, want 2 URIs", remaining)
	}
	if len(dedupedSpent) != 0 {
		t.Errorf("dedupedSpent = %v, want empty", dedupedSpent)
	}
}

func TestDedupURIs_EmptyURIs(t *testing.T) {
	remaining, dedupedSpent := DedupURIs(nil, []string{"https://a.example/file"})
	if len(remaining) != 0 {
		t.Errorf("remaining = %v, want empty", remaining)
	}
	if len(dedupedSpent) != 1 {
		t.Errorf("dedupedSpent = %v, want 1 URI", dedupedSpent)
	}
}

func TestDedupURIs_PreservesOrder(t *testing.T) {
	uris := []string{"https://c.example/file", "https://a.example/file", "https://b.example/file"}
	spent := []string{"https://a.example/file"}
	remaining, dedupedSpent := DedupURIs(uris, spent)
	if len(remaining) != 2 {
		t.Fatalf("remaining = %v, want 2 URIs", remaining)
	}
	if remaining[0] != "https://c.example/file" || remaining[1] != "https://b.example/file" {
		t.Errorf("remaining = %v, want [c, b] order preserved", remaining)
	}
	if dedupedSpent[0] != "https://a.example/file" {
		t.Errorf("dedupedSpent = %v, want [a]", dedupedSpent)
	}
}

func TestSaveExcludesErrorEntry(t *testing.T) {
	entries := []Entry{
		{
			URIs:    []string{"https://example.com/error-file.zip"},
			GID:     core.GID(1),
			Status:  core.StatusError,
			Options: map[string]string{"dir": "/tmp"},
		},
		{
			URIs:    []string{"https://example.com/active-file.zip"},
			GID:     core.GID(2),
			Status:  core.StatusActive,
			Options: map[string]string{"dir": "/tmp"},
		},
	}

	var savable []Entry
	for _, e := range entries {
		if ShouldSave(e.Status) && HasAnyURI(e) {
			savable = append(savable, e)
		}
	}

	var buf bytes.Buffer
	if err := Write(&buf, savable); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	if strings.Count(out, " gid=") != 1 {
		t.Errorf("expected 1 entry in output (error excluded), got:\n%s", out)
	}
	if !strings.Contains(out, "gid=0000000000000002") {
		t.Error("expected active entry gid=0000000000000002 in output")
	}
}

func TestSaveExcludesCompletedEntry(t *testing.T) {
	entries := []Entry{
		{
			URIs:    []string{"https://example.com/done-file.zip"},
			GID:     core.GID(1),
			Status:  core.StatusComplete,
			Options: map[string]string{"dir": "/tmp"},
		},
		{
			URIs:    []string{"https://example.com/waiting-file.zip"},
			GID:     core.GID(2),
			Status:  core.StatusWaiting,
			Options: map[string]string{"dir": "/tmp"},
		},
	}

	var savable []Entry
	for _, e := range entries {
		if ShouldSave(e.Status) && HasAnyURI(e) {
			savable = append(savable, e)
		}
	}

	var buf bytes.Buffer
	if err := Write(&buf, savable); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	if strings.Count(out, " gid=") != 1 {
		t.Errorf("expected 1 entry in output (completed excluded), got:\n%s", out)
	}
	if !strings.Contains(out, "gid=0000000000000002") {
		t.Error("expected waiting entry gid=0000000000000002 in output")
	}
}

func TestSaveExcludesZeroURIs(t *testing.T) {
	entries := []Entry{
		{
			URIs:    nil,
			GID:     core.GID(1),
			Status:  core.StatusWaiting,
			Options: map[string]string{"dir": "/tmp"},
		},
		{
			URIs:    []string{},
			GID:     core.GID(2),
			Status:  core.StatusWaiting,
			Options: map[string]string{"dir": "/tmp"},
		},
		{
			URIs:    []string{"https://example.com/valid.zip"},
			GID:     core.GID(3),
			Status:  core.StatusWaiting,
			Options: map[string]string{"dir": "/tmp"},
		},
	}

	var savable []Entry
	for _, e := range entries {
		if ShouldSave(e.Status) && HasAnyURI(e) {
			savable = append(savable, e)
		}
	}

	var buf bytes.Buffer
	if err := Write(&buf, savable); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	if strings.Count(out, " gid=") != 1 {
		t.Errorf("expected 1 entry in output (zero-URI entries excluded), got:\n%s", out)
	}
	if !strings.Contains(out, "gid=0000000000000003") {
		t.Error("expected gid=0000000000000003 in output")
	}
}

func TestSaveExcludesRemovedEntry(t *testing.T) {
	entries := []Entry{
		{
			URIs:    []string{"https://example.com/removed-file.zip"},
			GID:     core.GID(1),
			Status:  core.StatusRemoved,
			Options: map[string]string{"dir": "/tmp"},
		},
		{
			URIs:    []string{"https://example.com/paused-file.zip"},
			GID:     core.GID(2),
			Status:  core.StatusPaused,
			Options: map[string]string{"dir": "/tmp"},
		},
	}

	var savable []Entry
	for _, e := range entries {
		if ShouldSave(e.Status) && HasAnyURI(e) {
			savable = append(savable, e)
		}
	}

	var buf bytes.Buffer
	if err := Write(&buf, savable); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	if strings.Count(out, " gid=") != 1 {
		t.Errorf("expected 1 entry in output (removed excluded), got:\n%s", out)
	}
	if !strings.Contains(out, "gid=0000000000000002") {
		t.Error("expected paused entry gid=0000000000000002 in output")
	}
}

func TestRoundTripWithMixedStatuses(t *testing.T) {
	original := []Entry{
		{
			URIs:    []string{"https://example.com/active.zip"},
			GID:     core.GID(0xaaaa),
			Status:  core.StatusActive,
			Options: map[string]string{"dir": "/downloads", "out": "active.zip"},
		},
		{
			URIs:    []string{"https://example.com/error.zip"},
			GID:     core.GID(0xbbbb),
			Status:  core.StatusError,
			Options: map[string]string{"dir": "/downloads"},
		},
		{
			URIs:    []string{"https://example.com/waiting.zip"},
			GID:     core.GID(0xcccc),
			Status:  core.StatusWaiting,
			Options: map[string]string{"dir": "/downloads", "out": "waiting.zip"},
		},
		{
			URIs:    []string{"https://example.com/complete.zip"},
			GID:     core.GID(0xdddd),
			Status:  core.StatusComplete,
			Options: map[string]string{"dir": "/downloads"},
		},
		{
			URIs:    []string{"https://example.com/paused.zip", "https://mirror.example.com/paused.zip"},
			GID:     core.GID(0xeeee),
			Status:  core.StatusPaused,
			Options: map[string]string{"dir": "/downloads", "out": "paused.zip"},
		},
		{
			URIs:    []string{"https://example.com/removed.zip"},
			GID:     core.GID(0xffff),
			Status:  core.StatusRemoved,
			Options: map[string]string{"dir": "/downloads"},
		},
	}

	var savable []Entry
	for _, e := range original {
		if ShouldSave(e.Status) && HasAnyURI(e) {
			savable = append(savable, e)
		}
	}

	if len(savable) != 3 {
		t.Fatalf("expected 3 savable entries, got %d", len(savable))
	}

	// Write
	var buf bytes.Buffer
	if err := Write(&buf, savable); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read back
	result, err := Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 entries after round-trip, got %d", len(result))
	}

	// Collect GIDs from result
	gids := make(map[string]bool)
	for _, e := range result {
		gids[e.GID.Hex()] = true
	}

	if !gids["000000000000aaaa"] {
		t.Error("missing active entry (gid=aaaa)")
	}
	if !gids["000000000000cccc"] {
		t.Error("missing waiting entry (gid=cccc)")
	}
	if !gids["000000000000eeee"] {
		t.Error("missing paused entry (gid=eeee)")
	}
	if gids["000000000000bbbb"] {
		t.Error("error entry (gid=bbbb) should have been excluded")
	}
	if gids["000000000000dddd"] {
		t.Error("complete entry (gid=dddd) should have been excluded")
	}
	if gids["000000000000ffff"] {
		t.Error("removed entry (gid=ffff) should have been excluded")
	}

	// Verify paused entry has correct status and URIs
	for _, e := range result {
		if e.GID == core.GID(0xeeee) {
			if e.Status != core.StatusPaused {
				t.Errorf("paused entry status = %v, want Paused", e.Status)
			}
			if len(e.URIs) != 2 {
				t.Errorf("paused entry URI count = %d, want 2", len(e.URIs))
			}
		}
	}
}
