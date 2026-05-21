package engine

import (
	"sync"
)

// Segment represents a byte range of a download.  Start is inclusive,
// End is exclusive.  When End is -1 the total size is unknown.
type Segment struct {
	Index   int
	Start   int64
	End     int64
	Written int64
	Done    bool
	Claimed bool
}

// SegmentMan manages a collection of segments for parallel download.
// Workers call Next to claim an undone segment and MarkDone when
// finished.  Unclaim releases a claimed segment on transient error.
// Split subdivides the largest in-progress segment into
// two halves; the new half becomes available via Next.
type SegmentMan struct {
	mu           sync.Mutex
	segments     []*Segment
	nextIdx      int
	totalSize    int64
	minSplitSize int64
}

// NewSegmentMan divides totalSize into numSegments equal-sized segments.
// When totalSize <= 0 the End of every segment is -1 (unknown size).
// When numSegments > totalSize each byte gets its own segment.
func NewSegmentMan(totalSize int64, numSegments int) *SegmentMan {
	return NewSegmentManWithSplit(totalSize, numSegments, 0)
}

// NewSegmentManWithSplit is like NewSegmentMan but also sets the
// minimum split size used by Split.
func NewSegmentManWithSplit(totalSize int64, numSegments int, minSplitSize int64) *SegmentMan {
	if numSegments <= 0 {
		numSegments = 1
	}
	if totalSize > 0 && int64(numSegments) > totalSize {
		numSegments = int(totalSize)
	}
	segments := make([]*Segment, numSegments)
	chunkSize := int64(1)
	if totalSize > 0 {
		chunkSize = totalSize / int64(numSegments)
	}
	var start int64
	for i := 0; i < numSegments; i++ {
		end := int64(-1)
		if totalSize > 0 {
			end = start + chunkSize
			if i == numSegments-1 {
				end = totalSize
			}
		}
		segments[i] = &Segment{
			Index: i,
			Start: start,
			End:   end,
		}
		start = end
	}
	return &SegmentMan{
		segments:     segments,
		totalSize:    totalSize,
		minSplitSize: minSplitSize,
	}
}

// Next returns an unclaimed, undone segment.  It scans circularly from
// the last returned position.  Returns nil when every segment is either
// claimed or done.
func (sm *SegmentMan) Next() *Segment {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	start := sm.nextIdx
	for i := 0; i < len(sm.segments); i++ {
		idx := (start + i) % len(sm.segments)
		s := sm.segments[idx]
		if !s.Done && !s.Claimed {
			s.Claimed = true
			sm.nextIdx = idx + 1
			if sm.nextIdx >= len(sm.segments) {
				sm.nextIdx = 0
			}
			return s
		}
	}
	return nil
}

// Unclaim releases a claimed but not done segment back to the pool,
// so another worker can pick it up (e.g. after a transient error).
// Does nothing if idx is out of range or the segment is already done.
func (sm *SegmentMan) Unclaim(idx int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if idx < 0 || idx >= len(sm.segments) {
		return
	}
	s := sm.segments[idx]
	if s.Done {
		return
	}
	s.Claimed = false
}

// MarkDone marks a segment as complete and records the number of bytes
// written.  Multiple calls update Written but do not undo the Done flag.
func (sm *SegmentMan) MarkDone(idx int, written int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if idx < 0 || idx >= len(sm.segments) {
		return
	}
	s := sm.segments[idx]
	s.Written = written
	s.Done = true
	s.Claimed = false
}

// Split subdivides the in-progress (claimed but not done) segment
// with the largest remaining bytes, provided it is at least
// 2*minSplitSize bytes long.  The original segment is halved in
// place and a new segment covering the second half is appended.
// The original remains claimed; the new segment is unclaimed and
// available via Next.
//
// Returns the new segment or nil when no segment qualifies.
func (sm *SegmentMan) Split(minSplitSize int64) *Segment {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if minSplitSize <= 0 {
		return nil
	}

	var largest *Segment
	var largestRemaining int64
	for _, s := range sm.segments {
		if s.Done || !s.Claimed || s.End == -1 {
			continue
		}
		segSize := s.End - s.Start
		if segSize < 2*minSplitSize {
			continue
		}
		remaining := segSize - s.Written
		if remaining > largestRemaining {
			largestRemaining = remaining
			largest = s
		}
	}
	if largest == nil {
		return nil
	}

	mid := (largest.Start + largest.End) / 2
	oldEnd := largest.End
	largest.End = mid

	newSeg := &Segment{
		Index:   len(sm.segments),
		Start:   mid,
		End:     oldEnd,
		Written: 0,
		Done:    false,
		Claimed: false,
	}
	sm.segments = append(sm.segments, newSeg)
	sm.nextIdx = 0
	return newSeg
}

// Done reports whether every segment has been marked done.
func (sm *SegmentMan) Done() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, s := range sm.segments {
		if !s.Done {
			return false
		}
	}
	return true
}

// Written returns the total number of bytes recorded via MarkDone.
func (sm *SegmentMan) Written() int64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var total int64
	for _, s := range sm.segments {
		total += s.Written
	}
	return total
}

// SegmentCount returns the current number of segments (including split segments).
func (sm *SegmentMan) SegmentCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.segments)
}
