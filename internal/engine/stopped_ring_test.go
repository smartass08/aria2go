package engine

import (
	"testing"

	"github.com/smartass08/aria2go/internal/core"
)

func TestStoppedRing_Push(t *testing.T) {
	ring := newStoppedRing(5)
	if ring.len() != 0 {
		t.Fatalf("expected 0, got %d", ring.len())
	}

	dr := &downloadResult{gid: 1, state: core.StatusComplete}
	ring.push(dr)

	if ring.len() != 1 {
		t.Fatalf("expected 1, got %d", ring.len())
	}

	got, ok := ring.getByGID(1)
	if !ok {
		t.Fatal("expected to find GID 1")
	}
	if got.gid != 1 {
		t.Errorf("expected gid 1, got %d", got.gid)
	}
}

func TestStoppedRing_Eviction(t *testing.T) {
	ring := newStoppedRing(3)

	for i := 0; i < 5; i++ {
		ring.push(&downloadResult{
			gid:     core.GID(i + 1),
			state:   core.StatusComplete,
			errCode: core.ExitSuccess,
		})
	}

	// Only last 3 should be kept (capacity=3).
	if ring.len() != 3 {
		t.Fatalf("expected len 3, got %d", ring.len())
	}

	// GID 1 and 2 should be evicted.
	if _, ok := ring.getByGID(1); ok {
		t.Error("GID 1 should have been evicted")
	}
	if _, ok := ring.getByGID(2); ok {
		t.Error("GID 2 should have been evicted")
	}

	// GID 3, 4, 5 should be present.
	for i := 3; i <= 5; i++ {
		if _, ok := ring.getByGID(core.GID(i)); !ok {
			t.Errorf("GID %d should be present", i)
		}
	}
}

func TestStoppedRing_GetOffset(t *testing.T) {
	ring := newStoppedRing(5)

	for i := 0; i < 5; i++ {
		ring.push(&downloadResult{
			gid:   core.GID(i + 1),
			state: core.StatusComplete,
		})
	}

	// get from offset 0, num 2
	results := ring.get(0, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].gid != 1 || results[1].gid != 2 {
		t.Errorf("expected GIDs 1,2 got %d,%d", results[0].gid, results[1].gid)
	}

	// get from offset 2, num 2
	results = ring.get(2, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].gid != 3 || results[1].gid != 4 {
		t.Errorf("expected GIDs 3,4 got %d,%d", results[0].gid, results[1].gid)
	}
}

func TestStoppedRing_GetOffsetWrapsAround(t *testing.T) {
	ring := newStoppedRing(3)

	// fill 3 items
	for i := 0; i < 3; i++ {
		ring.push(&downloadResult{gid: core.GID(i + 1)})
	}
	// push one more to cause wrap
	ring.push(&downloadResult{gid: 4})

	// Should have GIDs 2, 3, 4 in order
	results := ring.get(0, 3)
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
	for i, expected := range []core.GID{2, 3, 4} {
		if results[i].gid != expected {
			t.Errorf("offset %d: expected GID %d, got %d", i, expected, results[i].gid)
		}
	}
}

func TestStoppedRing_GetOffsetOutOfRange(t *testing.T) {
	ring := newStoppedRing(3)
	ring.push(&downloadResult{gid: 1})
	ring.push(&downloadResult{gid: 2})

	// offset >= len should return nil
	results := ring.get(2, 1)
	if results != nil {
		t.Errorf("expected nil for out-of-range offset, got %v", results)
	}

	// offset in range but num large
	results = ring.get(0, 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results (capped to len), got %d", len(results))
	}
}

func TestStoppedRing_Purge(t *testing.T) {
	ring := newStoppedRing(10)

	for i := 0; i < 5; i++ {
		ring.push(&downloadResult{gid: core.GID(i + 1)})
	}

	ring.purge()

	if ring.len() != 0 {
		t.Errorf("expected 0 after purge, got %d", ring.len())
	}
	results := ring.get(0, 10)
	if results != nil {
		t.Errorf("expected nil after purge, got %v", results)
	}
	if _, ok := ring.getByGID(1); ok {
		t.Error("GID 1 should not exist after purge")
	}
}

func TestStoppedRing_EvictionTracking(t *testing.T) {
	ring := newStoppedRing(2)

	// Push first with error
	ring.push(&downloadResult{
		gid:       1,
		errCode:   core.ExitUnknownError,
		belongsTo: 0,
	})

	// Push second
	ring.push(&downloadResult{
		gid:       2,
		errCode:   core.ExitSuccess,
		belongsTo: 0,
	})

	// Push third to evict first
	ring.push(&downloadResult{
		gid:       3,
		errCode:   core.ExitSuccess,
		belongsTo: 0,
	})

	ring.mu.Lock()
	total, removedErrs, _ := ring.evictionInfo()
	ring.mu.Unlock()

	// First entry was error, so it should count
	if total < 1 {
		t.Errorf("expected at least 1 eviction, got %d", total)
	}
	if removedErrs < 1 {
		t.Errorf("expected at least 1 removed error, got %d", removedErrs)
	}
}

func TestStoppedRing_Empty(t *testing.T) {
	ring := newStoppedRing(5)

	if ring.len() != 0 {
		t.Errorf("expected 0, got %d", ring.len())
	}

	results := ring.get(0, 10)
	if results != nil {
		t.Errorf("expected nil, got %v", results)
	}

	if _, ok := ring.getByGID(1); ok {
		t.Error("expected not found")
	}

	ring.purge() // should not panic
}
