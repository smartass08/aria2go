package engine

import (
	"sync"
	"testing"
)

func TestNewSegmentManCreatesSegments(t *testing.T) {
	sm := NewSegmentMan(1000, 4)

	if len(sm.segments) != 4 {
		t.Fatalf("NewSegmentMan(1000, 4) len=%d, want 4", len(sm.segments))
	}

	if sm.segments[0].Start != 0 {
		t.Errorf("segment[0].Start = %d, want 0", sm.segments[0].Start)
	}
	if sm.segments[0].End != 250 {
		t.Errorf("segment[0].End = %d, want 250", sm.segments[0].End)
	}
	if sm.segments[1].Start != 250 {
		t.Errorf("segment[1].Start = %d, want 250", sm.segments[1].Start)
	}
	if sm.segments[1].End != 500 {
		t.Errorf("segment[1].End = %d, want 500", sm.segments[1].End)
	}
	if sm.segments[3].Start != 750 {
		t.Errorf("segment[3].Start = %d, want 750", sm.segments[3].Start)
	}
	if sm.segments[3].End != 1000 {
		t.Errorf("segment[3].End = %d, want 1000", sm.segments[3].End)
	}
}

func TestNewSegmentManSingleSegment(t *testing.T) {
	sm := NewSegmentMan(100, 1)
	if len(sm.segments) != 1 {
		t.Fatalf("len=%d, want 1", len(sm.segments))
	}
	if sm.segments[0].Start != 0 || sm.segments[0].End != 100 {
		t.Errorf("segment = [%d,%d), want [0,100)", sm.segments[0].Start, sm.segments[0].End)
	}
}

func TestNewSegmentManMoreSegmentsThanSize(t *testing.T) {
	sm := NewSegmentMan(3, 10)
	if len(sm.segments) != 3 {
		t.Fatalf("len=%d, want 3", len(sm.segments))
	}
	for i, s := range sm.segments {
		if s.End != int64(i+1) {
			t.Errorf("segment[%d].End = %d, want %d", i, s.End, i+1)
		}
	}
}

func TestNewSegmentManNegativeSize(t *testing.T) {
	sm := NewSegmentMan(-1, 4)
	if len(sm.segments) != 4 {
		t.Fatalf("len=%d, want 4", len(sm.segments))
	}
	for _, s := range sm.segments {
		if s.End != -1 {
			t.Errorf("segment End = %d, want -1 (unknown)", s.End)
		}
	}
}

func TestNewSegmentManZeroSize(t *testing.T) {
	sm := NewSegmentMan(0, 5)
	if len(sm.segments) != 5 {
		t.Fatalf("len=%d, want 5", len(sm.segments))
	}
}

func TestSegmentManNextReturnsAndClaimsFirstUndone(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	seg := sm.Next()
	if seg == nil {
		t.Fatal("Next() returned nil")
	}
	if seg.Index != 0 {
		t.Errorf("Index = %d, want 0", seg.Index)
	}
	if !seg.Claimed {
		t.Error("segment not claimed after Next()")
	}
}

func TestSegmentManNextDoesNotReturnClaimedSegments(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	sm.Next() // claims 0
	sm.Next() // claims 1
	sm.Next() // claims 2
	// Only segment 3 is unclaimed/undone.
	seg := sm.Next()
	if seg == nil {
		t.Fatal("Next() returned nil")
	}
	if seg.Index != 3 {
		t.Errorf("Index = %d, want 3", seg.Index)
	}
	// All claimed or done; no more available.
	if s := sm.Next(); s != nil {
		t.Errorf("Next() = %v, want nil", s)
	}
}

func TestSegmentManNextReturnsNilWhenAllDone(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	for i := 0; i < 4; i++ {
		seg := sm.Next()
		if seg == nil {
			t.Fatalf("Next() returned nil at iteration %d", i)
		}
		sm.MarkDone(seg.Index, seg.End-seg.Start)
	}
	if seg := sm.Next(); seg != nil {
		t.Errorf("Next() after all done = %v, want nil", seg)
	}
}

func TestSegmentManMarkDoneSetsDone(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	seg := sm.Next()
	sm.MarkDone(seg.Index, 25)
	if !sm.segments[seg.Index].Done {
		t.Error("segment not marked as done")
	}
	if sm.segments[seg.Index].Written != 25 {
		t.Errorf("Written = %d, want 25", sm.segments[seg.Index].Written)
	}
	if sm.segments[seg.Index].Claimed {
		t.Error("Claimed should be false after MarkDone")
	}
}

func TestSegmentManMarkDoneOnAlreadyDoneIsNoop(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	sm.Next()
	sm.MarkDone(0, 30)
	sm.MarkDone(0, 40)
	if sm.segments[0].Written != 40 {
		t.Errorf("Written = %d, want 40", sm.segments[0].Written)
	}
}

func TestSegmentManConcurrentNext(t *testing.T) {
	sm := NewSegmentMan(1000, 20)
	var mu sync.Mutex
	claimed := make(map[int]bool)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				seg := sm.Next()
				if seg == nil {
					return
				}
				mu.Lock()
				if claimed[seg.Index] {
					mu.Unlock()
					t.Errorf("segment %d claimed twice", seg.Index)
					return
				}
				claimed[seg.Index] = true
				mu.Unlock()
				sm.MarkDone(seg.Index, 50)
			}
		}()
	}
	wg.Wait()
	sm.mu.Lock()
	total := 0
	for _, s := range sm.segments {
		if s.Done {
			total++
		}
	}
	sm.mu.Unlock()
	if total != 20 {
		t.Errorf("Done segments = %d, want 20", total)
	}
}

func TestSegmentManSplitInProgressSegment(t *testing.T) {
	sm := NewSegmentMan(1000, 10)
	sm.Next()
	sm.segments[0].Written = 10
	sm.Next()
	sm.segments[1].Written = 5

	if len(sm.segments) != 10 {
		t.Fatalf("initial len=%d, want 10", len(sm.segments))
	}

	newSeg := sm.Split(10)
	if newSeg == nil {
		t.Fatal("Split returned nil")
	}
	if len(sm.segments) != 11 {
		t.Fatalf("after split len=%d, want 11", len(sm.segments))
	}

	// Original segment 1 should still be claimed and not done.
	if sm.segments[1].Done {
		t.Error("original segment should not be done after split")
	}
	if !sm.segments[1].Claimed {
		t.Error("original segment should remain claimed after split")
	}

	if sm.segments[1].Start != 100 || sm.segments[1].End != 150 {
		t.Errorf("original segment = [%d, %d), want [100, 150)", sm.segments[1].Start, sm.segments[1].End)
	}
	if newSeg.Start != 150 {
		t.Errorf("new segment Start = %d, want 150", newSeg.Start)
	}
	if newSeg.Claimed {
		t.Error("new segment should not be claimed")
	}
	if newSeg.Done {
		t.Error("new segment should not be done")
	}
}

func TestSegmentManSplitBelowMinSize(t *testing.T) {
	sm := NewSegmentMan(100, 2)
	sm.Next()
	sm.segments[0].Written = 10
	sm.Next()
	sm.segments[1].Written = 5

	newSeg := sm.Split(100)
	if newSeg != nil {
		t.Error("Split with minSplitSize larger than segment should return nil")
	}
}

func TestSegmentManSplitWhenAllDone(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	for i := 0; i < 4; i++ {
		seg := sm.Next()
		sm.MarkDone(seg.Index, 25)
	}
	newSeg := sm.Split(10)
	if newSeg != nil {
		t.Error("Split when all done should return nil")
	}
}

func TestSegmentManSplitUnknownSize(t *testing.T) {
	sm := NewSegmentMan(-1, 4)
	sm.Next()
	sm.segments[0].Written = 100
	newSeg := sm.Split(10)
	if newSeg != nil {
		t.Error("Split with unknown End should return nil")
	}
}

func TestSegmentManDone(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	if sm.Done() {
		t.Error("Done=true before all segments done")
	}
	for i := 0; i < 4; i++ {
		seg := sm.Next()
		sm.MarkDone(seg.Index, 25)
	}
	if !sm.Done() {
		t.Error("Done=false after all segments done")
	}
}

func TestSegmentManWrittenBytes(t *testing.T) {
	sm := NewSegmentMan(1000, 5)
	sm.Next()
	sm.MarkDone(0, 100)
	sm.Next()
	sm.MarkDone(1, 200)
	if n := sm.Written(); n != 300 {
		t.Errorf("Written = %d, want 300", n)
	}
}

func TestSegmentManUnclaimReturnsSegmentToPool(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	seg := sm.Next()
	if seg == nil {
		t.Fatal("Next() returned nil")
	}
	if seg.Index != 0 {
		t.Fatalf("expected segment 0, got %d", seg.Index)
	}
	sm.Unclaim(seg.Index)
	if seg.Claimed {
		t.Error("segment still claimed after Unclaim")
	}
	// Next scans round-robin; after Unclaim seg 0 is available but
	// nextIdx moved past it. Call Next until it wraps around to 0.
	found := false
	for i := 0; i < 10; i++ {
		seg2 := sm.Next()
		if seg2 == nil {
			continue
		}
		if seg2.Index == 0 {
			found = true
			break
		}
		// Re-unclaim so we don't exhaust the pool.
		sm.Unclaim(seg2.Index)
	}
	if !found {
		t.Error("unclaimed segment 0 was not returned by Next within 10 iterations")
	}
}

func TestSegmentManUnclaimPreservesWrittenBytes(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	_ = sm.Next()
	sm.segments[0].Written = 30
	sm.Unclaim(0)
	if sm.segments[0].Written != 30 {
		t.Errorf("Written = %d, want 30", sm.segments[0].Written)
	}
	if sm.segments[0].Done {
		t.Error("segment should not be done after Unclaim")
	}
}

func TestSegmentManUnclaimDoesNotAffectDone(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	seg := sm.Next()
	sm.MarkDone(seg.Index, 25)
	sm.Unclaim(seg.Index)
	if !sm.segments[seg.Index].Done {
		t.Error("done segment should stay done after Unclaim")
	}
}

func TestSegmentManSplitPicksLargestRemainingNotFewestWritten(t *testing.T) {
	// Prove Split picks by largest remaining bytes, NOT fewest written.
	// seg A (index 0): total=500, written=80, remaining=420 (large written, LARGEST remaining)
	// seg B (index 1): total=100, written=10, remaining=90 (small written, SMALL remaining)
	// Old code picks B (written=10 < 80). New code picks A (remaining=420 > 90).
	sm := NewSegmentMan(1000, 10)
	sm.segments[0].Claimed = true
	sm.segments[0].Written = 80
	sm.segments[1].Claimed = true
	sm.segments[1].Written = 10

	sm.segments[0].End = 500
	sm.segments[1].End = 200

	newSeg := sm.Split(20)
	if newSeg == nil {
		t.Fatal("Split returned nil")
	}
	if newSeg.Start != 250 {
		t.Errorf("new seg Start = %d, want 250 (midpoint of [0,500))", newSeg.Start)
	}
	if newSeg.End != 500 {
		t.Errorf("new seg End = %d, want 500", newSeg.End)
	}
}

func TestSegmentManWorkAvailableAfterSplitByAnotherWorker(t *testing.T) {
	// Simulate: worker A finishes fast, calls Next+Split and both return nil.
	// Worker B is still slow. Later worker B finishes and MarkDone frees up
	// a segment that was part of a larger claimed segment.
	// The worker loop should retry and pick up new work.
	sm := NewSegmentMan(1000, 3)
	// seg 0: [0, 333), large, slow (claimed by worker A)
	sm.segments[0].Claimed = true

	// Worker B tries Next: seg 1 and seg 2 are unclaimed.
	_ = sm.Next()   // claims seg 1
	s3 := sm.Next() // claims seg 2
	if s3 == nil {
		t.Fatal("Next should return seg 2")
	}

	// No more segments. Next() returns nil, Split() checks seg 0.
	// seg 0: 333 bytes, minSplitSize=50, 333 >= 100 => qualifies.
	// Split should split seg 0.
	newSeg := sm.Split(50)
	if newSeg == nil {
		t.Fatal("Split should have split seg 0")
	}
	if newSeg.Claimed {
		t.Error("new split segment should not be claimed")
	}
	// Worker B can now call Next() and get the new segment.
	found := sm.Next()
	if found == nil {
		t.Fatal("Next should return the newly split segment")
	}
	if found.Index != newSeg.Index {
		t.Errorf("Next returned index %d, want %d", found.Index, newSeg.Index)
	}
}

func TestSegmentManUnclaimBogusIndex(t *testing.T) {
	sm := NewSegmentMan(100, 4)
	sm.Unclaim(-1)
	sm.Unclaim(99)
	if len(sm.segments) != 4 {
		t.Fatal("segment count changed unexpectedly")
	}
}
