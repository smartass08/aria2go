package disk

import (
	"reflect"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Existing backward-compat tests (adapted to new constructor)
// ---------------------------------------------------------------------------

func TestPieceMapNew(t *testing.T) {
	pm := newPieceMap(1, 10)
	if pm.count() != 10 {
		t.Fatalf("expected count 10, got %d", pm.count())
	}
	for i := 0; i < 10; i++ {
		if pm.have(i) {
			t.Errorf("piece %d should not be marked", i)
		}
	}
}

func TestPieceMapMarkAndHave(t *testing.T) {
	pm := newPieceMap(1, 8)

	pm.mark(3, true)
	if !pm.have(3) {
		t.Error("piece 3 should be marked")
	}
	for i := 0; i < 8; i++ {
		if i != 3 && pm.have(i) {
			t.Errorf("piece %d should not be marked", i)
		}
	}

	pm.mark(3, false)
	if pm.have(3) {
		t.Error("piece 3 should be unmarked")
	}

	pm.mark(3, false)
	if pm.have(3) {
		t.Error("mark(false) should be idempotent")
	}
}

func TestPieceMapBitfieldEncoding(t *testing.T) {
	pm := newPieceMap(1, 16)

	pm.mark(0, true)
	bf := pm.bitfield()
	if len(bf) != 2 {
		t.Fatalf("expected bitfield len 2, got %d", len(bf))
	}
	if bf[0] != 0x80 {
		t.Errorf("expected bf[0]=0x80, got 0x%02x", bf[0])
	}
	if bf[1] != 0x00 {
		t.Errorf("expected bf[1]=0x00, got 0x%02x", bf[1])
	}

	pm.mark(7, true)
	bf = pm.bitfield()
	if bf[0] != 0x81 {
		t.Errorf("expected bf[0]=0x81 (pieces 0,7), got 0x%02x", bf[0])
	}

	pm.mark(8, true)
	bf = pm.bitfield()
	if bf[0] != 0x81 {
		t.Errorf("expected bf[0]=0x81, got 0x%02x", bf[0])
	}
	if bf[1] != 0x80 {
		t.Errorf("expected bf[1]=0x80, got 0x%02x", bf[1])
	}

	pm.mark(15, true)
	bf = pm.bitfield()
	if bf[1] != 0x81 {
		t.Errorf("expected bf[1]=0x81 (pieces 8,15), got 0x%02x", bf[1])
	}

	for i := 0; i < 16; i++ {
		pm.mark(i, true)
	}
	bf = pm.bitfield()
	if bf[0] != 0xff || bf[1] != 0xff {
		t.Errorf("expected 0xffff, got 0x%02x%02x", bf[0], bf[1])
	}
}

func TestPieceMapBitfieldOddCount(t *testing.T) {
	pm := newPieceMap(1, 10)
	for i := 0; i < 10; i++ {
		pm.mark(i, true)
	}
	bf := pm.bitfield()
	if len(bf) != 2 {
		t.Fatalf("expected 2 bytes, got %d", len(bf))
	}
	if bf[0] != 0xff {
		t.Errorf("expected bf[0]=0xff, got 0x%02x", bf[0])
	}
	if bf[1] != 0xc0 {
		t.Errorf("expected bf[1]=0xc0, got 0x%02x", bf[1])
	}
}

func TestPieceMapMissing(t *testing.T) {
	pm := newPieceMap(1, 5)
	missing := pm.missing()
	expected := []int{0, 1, 2, 3, 4}
	if !reflect.DeepEqual(missing, expected) {
		t.Errorf("expected %v, got %v", expected, missing)
	}

	pm.mark(0, true)
	pm.mark(2, true)
	pm.mark(4, true)
	missing = pm.missing()
	expected = []int{1, 3}
	if !reflect.DeepEqual(missing, expected) {
		t.Errorf("expected %v, got %v", expected, missing)
	}

	for i := 0; i < 5; i++ {
		pm.mark(i, true)
	}
	missing = pm.missing()
	if missing != nil {
		t.Errorf("expected nil from missing when all pieces are complete, got %v", missing)
	}
}

func TestPieceMapSetCount(t *testing.T) {
	pm := newPieceMap(1, 4)
	pm.mark(0, true)
	pm.mark(2, true)

	pm.setCount(4)
	if !pm.have(0) || !pm.have(2) || pm.have(1) || pm.have(3) {
		t.Error("same count should preserve marks")
	}

	pm.setCount(8)
	if pm.count() != 8 {
		t.Fatalf("expected count 8, got %d", pm.count())
	}
	for i := 0; i < 8; i++ {
		if pm.have(i) {
			t.Errorf("piece %d should be unmarked after count change", i)
		}
	}

	pm.setCount(0)
	if pm.count() != 0 {
		t.Fatalf("expected count 0, got %d", pm.count())
	}
}

func TestPieceMapMarkIdempotentFalse(t *testing.T) {
	pm := newPieceMap(1, 3)
	pm.mark(1, false)
	pm.mark(1, false)
	pm.mark(1, false)
	if pm.have(1) {
		t.Error("piece 1 should not be marked")
	}
}

func TestPieceMapZeroCount(t *testing.T) {
	pm := newPieceMap(0, 0)
	if pm.count() != 0 {
		t.Fatalf("expected count 0, got %d", pm.count())
	}
	if pm.have(0) {
		t.Error("have(0) should return false when count is 0")
	}
	if pm.mark(0, true); pm.have(0) {
		t.Error("mark on zero-count map should be no-op")
	}
	if pm.bitfield() != nil {
		t.Error("bitfield should return nil for zero count")
	}
	if pm.missing() != nil {
		t.Error("missing should return nil for zero count")
	}
}

func TestPieceMapOutOfBounds(t *testing.T) {
	pm := newPieceMap(1, 5)

	if pm.have(-1) {
		t.Error("have(-1) should return false")
	}
	if pm.have(5) {
		t.Error("have(5) should return false")
	}
	if pm.have(100) {
		t.Error("have(100) should return false")
	}

	pm.mark(-1, true)
	if pm.have(-1) {
		t.Error("mark(-1, true) should be no-op")
	}
	pm.mark(5, true)
	if pm.have(5) {
		t.Error("mark(5, true) should be no-op")
	}
	pm.mark(100, false)
	if pm.have(100) {
		t.Error("mark(100) should be no-op")
	}

	if pm.count() != 5 {
		t.Error("out-of-bounds calls should not change count")
	}
}

func TestPieceMapConcurrentAccess(t *testing.T) {
	pm := newPieceMap(1, 100)

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				pm.mark(i, true)
			}
		}()
	}
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = pm.have(i)
			}
		}()
	}
	wg.Wait()

	for i := 0; i < 100; i++ {
		if !pm.have(i) {
			t.Errorf("piece %d should be marked after concurrent writes", i)
		}
	}

	bf := pm.bitfield()
	nbytes := (100 + 7) / 8
	if len(bf) != nbytes {
		t.Errorf("expected bitfield len %d, got %d", nbytes, len(bf))
	}
	for i := 0; i < nbytes-1; i++ {
		if bf[i] != 0xff {
			t.Errorf("expected bf[%d]=0xff, got 0x%02x", i, bf[i])
		}
	}
	lastByte := bf[nbytes-1]
	rem := 100 % 8
	expectedLast := byte(((1 << rem) - 1) << (8 - rem))
	if lastByte != expectedLast {
		t.Errorf("expected bf[%d]=0x%02x, got 0x%02x", nbytes-1, expectedLast, lastByte)
	}

	missing := pm.missing()
	if len(missing) != 0 {
		t.Errorf("expected 0 missing pieces, got %d", len(missing))
	}
}

func TestPieceMapConcurrentMarkUnmark(t *testing.T) {
	pm := newPieceMap(1, 50)
	for i := 0; i < 50; i++ {
		pm.mark(i, true)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			pm.mark(i, false)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			_ = pm.have(i)
		}
	}()
	wg.Wait()

	for i := 0; i < 25; i++ {
		if pm.have(i) {
			t.Errorf("piece %d should be unmarked", i)
		}
	}
	for i := 25; i < 50; i++ {
		if !pm.have(i) {
			t.Errorf("piece %d should still be marked", i)
		}
	}
}

func TestPieceMapCountTracking(t *testing.T) {
	pm := newPieceMap(1, 7)
	if pm.count() != 7 {
		t.Fatalf("expected count 7, got %d", pm.count())
	}

	pm.setCount(3)
	if pm.count() != 3 {
		t.Fatalf("expected count 3 after setCount, got %d", pm.count())
	}
	for i := 0; i < 3; i++ {
		if pm.have(i) {
			t.Errorf("piece %d should be unmarked after setCount", i)
		}
	}

	pm.setCount(7)
	if pm.count() != 7 {
		t.Fatalf("expected count 7 after setCount back, got %d", pm.count())
	}

	pm.setCount(7)
	if pm.count() != 7 {
		t.Fatalf("same count should preserve marks")
	}
}

func TestPieceMapBitfieldRWMutex(t *testing.T) {
	pm := newPieceMap(1, 64)

	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pm.bitfield()
			_ = pm.missing()
			_ = pm.count()
			for i := 0; i < 64; i++ {
				_ = pm.have(i)
			}
		}()
	}
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for i := start; i < start+16; i++ {
				pm.mark(i, true)
			}
		}(g * 3 % 48)
	}
	wg.Wait()
}

func TestPieceMapSingleByteBitfield(t *testing.T) {
	pm := newPieceMap(1, 3)
	pm.mark(0, true)
	bf := pm.bitfield()
	if len(bf) != 1 {
		t.Fatalf("expected 1 byte, got %d", len(bf))
	}
	if bf[0] != 0x80 {
		t.Errorf("expected 0x80, got 0x%02x", bf[0])
	}

	pm.mark(1, true)
	bf = pm.bitfield()
	if bf[0] != 0xc0 {
		t.Errorf("expected 0xc0, got 0x%02x", bf[0])
	}

	pm.mark(2, true)
	bf = pm.bitfield()
	if bf[0] != 0xe0 {
		t.Errorf("expected 0xe0, got 0x%02x", bf[0])
	}
}

func TestPieceMapSetCountZero(t *testing.T) {
	pm := newPieceMap(1, 5)
	pm.mark(0, true)
	pm.mark(3, true)
	pm.setCount(0)

	if pm.count() != 0 {
		t.Fatalf("expected count 0, got %d", pm.count())
	}
	if pm.have(0) {
		t.Error("have should return false after setCount(0)")
	}
	if pm.bitfield() != nil {
		t.Error("bitfield should return nil after setCount(0)")
	}
}

// ---------------------------------------------------------------------------
// BitfieldMan tests (ported from C++ BitfieldManTest)
// ---------------------------------------------------------------------------

const K = 1024
const M = 1024 * 1024
const G = 1024 * 1024 * 1024

func TestPieceMapGetBlockSize(t *testing.T) {
	// BitfieldMan bt1(1_k, 10_k) — 10 pieces of 1K each
	pm := newPieceMap(1*K, 10*K)
	if bl := pm.getBlockLengthForIndex(9); bl != 1*K {
		t.Errorf("getBlockLengthForIndex(9) = %d, want %d", bl, 1*K)
	}

	// 10K + 1 total, 1K block: last piece has length 1
	pm2 := newPieceMap(1*K, 10*K+1)
	if bl := pm2.getBlockLengthForIndex(9); bl != 1*K {
		t.Errorf("getBlockLengthForIndex(9) = %d, want %d", bl, 1*K)
	}
	if bl := pm2.getBlockLengthForIndex(10); bl != 1 {
		t.Errorf("getBlockLengthForIndex(10) = %d, want 1", bl)
	}
	if bl := pm2.getBlockLengthForIndex(11); bl != 0 {
		t.Errorf("getBlockLengthForIndex(11) = %d, want 0", bl)
	}
}

func TestPieceMapGetFirstMissingUnusedIndex(t *testing.T) {
	// Without filter
	pm := newPieceMap(1*K, 10*K)
	idx, ok := pm.getFirstMissingUnusedIndex()
	if !ok || idx != 0 {
		t.Errorf("first missing/unused: want (0,true), got (%d,%v)", idx, ok)
	}
	pm.setUseBit(0)
	idx, ok = pm.getFirstMissingUnusedIndex()
	if !ok || idx != 1 {
		t.Errorf("after useBit(0): want (1,true), got (%d,%v)", idx, ok)
	}
	pm.unsetUseBit(0)
	pm.setBit(0)
	idx, ok = pm.getFirstMissingUnusedIndex()
	if !ok || idx != 1 {
		t.Errorf("after setBit(0): want (1,true), got (%d,%v)", idx, ok)
	}
	pm.setAllBit()
	_, ok = pm.getFirstMissingUnusedIndex()
	if ok {
		t.Error("all bits set: should return false")
	}

	// With filter
	pm2 := newPieceMap(1*K, 10*K)
	pm2.addFilter(1*K, 10*K)
	pm2.enableFilter()
	idx, ok = pm2.getFirstMissingUnusedIndex()
	if !ok || idx != 1 {
		t.Errorf("filter [1K,10K): want (1,true), got (%d,%v)", idx, ok)
	}
	pm2.setUseBit(1)
	idx, ok = pm2.getFirstMissingUnusedIndex()
	if !ok || idx != 2 {
		t.Errorf("filter + useBit(1): want (2,true), got (%d,%v)", idx, ok)
	}
	pm2.setBit(2)
	idx, ok = pm2.getFirstMissingUnusedIndex()
	if !ok || idx != 3 {
		t.Errorf("filter + setBit(2): want (3,true), got (%d,%v)", idx, ok)
	}
}

func TestPieceMapGetFirstMissingIndex(t *testing.T) {
	// Without filter
	pm := newPieceMap(1*K, 10*K)
	idx, ok := pm.getFirstMissingIndex()
	if !ok || idx != 0 {
		t.Errorf("first missing: want (0,true), got (%d,%v)", idx, ok)
	}
	pm.setUseBit(0)
	idx, ok = pm.getFirstMissingIndex()
	if !ok || idx != 0 {
		t.Errorf("useBit(0) doesn't affect missing: want (0,true), got (%d,%v)", idx, ok)
	}
	pm.unsetUseBit(0)
	pm.setBit(0)
	idx, ok = pm.getFirstMissingIndex()
	if !ok || idx != 1 {
		t.Errorf("after setBit(0): want (1,true), got (%d,%v)", idx, ok)
	}
	pm.setAllBit()
	_, ok = pm.getFirstMissingIndex()
	if ok {
		t.Error("all bits set: should return false")
	}

	// With filter
	pm2 := newPieceMap(1*K, 10*K)
	pm2.addFilter(1*K, 10*K)
	pm2.enableFilter()
	idx, ok = pm2.getFirstMissingIndex()
	if !ok || idx != 1 {
		t.Errorf("filter [1K,10K): want (1,true), got (%d,%v)", idx, ok)
	}
	pm2.setUseBit(1)
	idx, ok = pm2.getFirstMissingIndex()
	if !ok || idx != 1 {
		t.Errorf("filter + useBit(1): want (1,true), got (%d,%v)", idx, ok)
	}
	pm2.setBit(1)
	idx, ok = pm2.getFirstMissingIndex()
	if !ok || idx != 2 {
		t.Errorf("filter + setBit(1): want (2,true), got (%d,%v)", idx, ok)
	}
}

func TestPieceMapIsAllBitSet(t *testing.T) {
	pm := newPieceMap(1*K, 10*K)
	if pm.isAllBitSet() {
		t.Error("empty bitfield: isAllBitSet should be false")
	}
	pm.setBit(1)
	if pm.isAllBitSet() {
		t.Error("only bit 1 set: should be false")
	}

	for i := 0; i < 8; i++ {
		pm.setBit(i)
	}
	if pm.isAllBitSet() {
		t.Error("only 8 bits set: should be false")
	}

	pm.setAllBit()
	if !pm.isAllBitSet() {
		t.Error("all bits set: should be true")
	}

	// Zero-length torrent: isAllBitSet returns true
	pmZero := newPieceMap(1*K, 0)
	if !pmZero.isAllBitSet() {
		t.Error("zero-length: isAllBitSet should be true")
	}
}

func TestPieceMapFilter(t *testing.T) {
	pm := newPieceMap(2, 32)
	pm.addFilter(4, 12)
	pm.enableFilter()
	out := pm.getFirstNMissingUnusedIndex(32)
	expected := []int{2, 3, 4, 5, 6, 7}
	if len(out) != 6 {
		t.Errorf("filter [4,12) in 32: want %d pieces, got %d", 6, len(out))
	}
	for i, v := range expected {
		if i < len(out) && out[i] != v {
			t.Errorf("out[%d] = %d, want %d", i, out[i], v)
		}
	}
	if tl := pm.getFilteredTotalLength(); tl != 12 {
		t.Errorf("filtered total length = %d, want 12", tl)
	}

	// Second filter test
	pm2 := newPieceMap(2, 32)
	pm2.addFilter(5, 2)
	pm2.enableFilter()
	out = pm2.getFirstNMissingUnusedIndex(32)
	if len(out) != 2 || out[0] != 2 || out[1] != 3 {
		t.Errorf("filter [5,2): want [2,3], got %v", out)
	}
	pm2.setBit(2)
	pm2.setBit(3)
	if tl := pm2.getFilteredTotalLength(); tl != 4 {
		t.Errorf("filtered total length = %d, want 4", tl)
	}
	if !pm2.isFilteredAllBitSet() {
		t.Error("all filtered bits set: should be true")
	}

	// Non-multiple-of-blockLength total length
	pm3 := newPieceMap(2, 31)
	pm3.addFilter(0, 31)
	pm3.enableFilter()
	if tl := pm3.getFilteredTotalLength(); tl != 31 {
		t.Errorf("filtered total length 31 bytes: got %d, want 31", tl)
	}
}

func TestPieceMapIsFilterBitSet(t *testing.T) {
	pm := newPieceMap(2, 32)
	if pm.isFilterBitSet(0) {
		t.Error("no filter: isFilterBitSet should be false")
	}
	pm.addFilter(0, 2) // blocks 0 covered (offset 0..1)
	if !pm.isFilterBitSet(0) {
		t.Error("filter [0,2): block 0 should be filtered")
	}
	if pm.isFilterBitSet(1) {
		t.Error("filter [0,2): block 1 should NOT be filtered")
	}
	pm.addFilter(2, 4) // blocks 1 covered (offset 2..5)
	if !pm.isFilterBitSet(1) {
		t.Error("after addFilter [2,4): block 1 should be filtered")
	}
}

func TestPieceMapAddFilterZeroLength(t *testing.T) {
	pm := newPieceMap(1*K, 1*M)
	pm.addFilter(2*K, 0)
	pm.enableFilter()
	if nb := pm.countMissingBlock(); nb != 0 {
		t.Errorf("zero-length filter: countMissingBlock = %d, want 0", nb)
	}
	if !pm.isFilteredAllBitSet() {
		t.Error("zero-length filter: isFilteredAllBitSet should be true")
	}
}

func TestPieceMapAddNotFilter(t *testing.T) {
	pm := newPieceMap(2, 32)
	pm.addNotFilter(3, 6) // blocks 1 through 4 are excluded (offset 3..8)
	fb := pm.getFilterBitfield()
	// Block 0 (offset 0..1): should be filtered
	if !testBit(fb, 0) {
		t.Error("addNotFilter [3,6): block 0 should be filtered")
	}
	for i := 1; i < 5; i++ {
		if testBit(fb, i) {
			t.Errorf("addNotFilter [3,6): block %d should NOT be filtered", i)
		}
	}
	for i := 5; i < 16; i++ {
		if !testBit(fb, i) {
			t.Errorf("addNotFilter [3,6): block %d should be filtered", i)
		}
	}
}

func TestPieceMapAddNotFilterZeroLength(t *testing.T) {
	pm := newPieceMap(2, 6)
	pm.addNotFilter(2, 0)
	fb := pm.getFilterBitfield()
	if testBit(fb, 0) || testBit(fb, 1) || testBit(fb, 2) {
		t.Error("addNotFilter [2,0): no blocks should be filtered")
	}
}

func TestPieceMapAddNotFilterOverflow(t *testing.T) {
	pm := newPieceMap(2, 6)
	pm.addNotFilter(6, 100) // all 3 blocks should be filtered (excluded range is past end)
	fb := pm.getFilterBitfield()
	if !testBit(fb, 0) || !testBit(fb, 1) || !testBit(fb, 2) {
		t.Error("addNotFilter [6,100): all 3 blocks should be filtered")
	}
}

func TestPieceMapGetSparseMissingUnusedIndex(t *testing.T) {
	pm := newPieceMap(1*M, 10*M)
	ignore := make([]byte, 2)
	minSplit := int32(1 * M)

	// All unused, all missing — longest gap is [0,10), start at 0
	idx, ok := pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 0 {
		t.Fatalf("initial sparse: want (0,true), got (%d,%v)", idx, ok)
	}

	// Set use bits one by one, verify midpoint selection
	// Use bit at 0 → longest gap is [1,10), midpoint is 5
	pm.setUseBit(0)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 5 {
		t.Errorf("after useBit(0): want (5,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(5)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 3 {
		t.Errorf("after useBit(5): want (3,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(3)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 8 {
		t.Errorf("after useBit(3): want (8,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(8)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 2 {
		t.Errorf("after useBit(8): want (2,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(2)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 1 {
		t.Errorf("after useBit(2): want (1,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(1)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 4 {
		t.Errorf("after useBit(1): want (4,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(4)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 7 {
		t.Errorf("after useBit(4): want (7,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(7)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 6 {
		t.Errorf("after useBit(7): want (6,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(6)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 9 {
		t.Errorf("after useBit(6): want (9,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(9)
	_, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if ok {
		t.Error("all use bits set: should return false")
	}
}

func TestPieceMapGetSparseMissingUnusedIndexSetBit(t *testing.T) {
	pm := newPieceMap(1*M, 10*M)
	ignore := make([]byte, 2)
	minSplit := int32(1 * M)

	// Set bits one by one (have the piece) → index advances sequentially
	idx, ok := pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 0 {
		t.Fatalf("initial sparse setBit: want (0,true), got (%d,%v)", idx, ok)
	}
	for i := 0; i < 9; i++ {
		pm.setBit(i)
		idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
		if !ok || idx != i+1 {
			t.Errorf("after setBit(%d): want (%d,true), got (%d,%v)", i, i+1, idx, ok)
		}
	}
	pm.setBit(9)
	_, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if ok {
		t.Error("all bits set: should return false")
	}
}

func TestPieceMapGetSparseMissingUnusedIndexWithMinSplitSize(t *testing.T) {
	pm := newPieceMap(1*M, 10*M)
	ignore := make([]byte, 2)
	minSplit := int32(2 * M) // need at least 2 consecutive blocks

	pm.setUseBit(1)
	idx, ok := pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 6 {
		t.Errorf("minSplit=2M, useBit(1): want (6,true), got (%d,%v)", idx, ok)
	}

	pm.setBit(6)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 7 {
		t.Errorf("+setBit(6): want (7,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(7)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 4 {
		t.Errorf("+useBit(7): want (4,true), got (%d,%v)", idx, ok)
	}

	pm.setBit(4)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 0 {
		t.Errorf("+setBit(4): want (0,true), got (%d,%v)", idx, ok)
	}

	pm.setBit(0)
	idx, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 5 {
		t.Errorf("+setBit(0): want (5,true), got (%d,%v)", idx, ok)
	}

	pm.setBit(5)
	_, ok = pm.getSparseMissingUnusedIndex(minSplit, ignore)
	if ok {
		t.Error("+setBit(5): should return false")
	}
}

func TestPieceMapIsBitSetOffsetRange(t *testing.T) {
	pm := newPieceMap(4*M, 4*G)
	pm.setAllBit()

	// Zero length
	if pm.isBitSetOffsetRange(0, 0) {
		t.Error("length 0: should return false")
	}
	// Offset at totalLength
	if pm.isBitSetOffsetRange(4*G, 100) {
		t.Error("offset at totalLength: should return false")
	}
	if pm.isBitSetOffsetRange(4*G+1, 100) {
		t.Error("offset past totalLength: should return false")
	}

	if !pm.isBitSetOffsetRange(0, 4*G) {
		t.Error("all bits set, 0..totalLength: should be true")
	}
	if !pm.isBitSetOffsetRange(0, 4*G+1) {
		t.Error("all bits set, 0..totalLength+1: should be true")
	}

	pm.clearAllBit()
	pm.setBit(100)
	pm.setBit(101)

	if !pm.isBitSetOffsetRange(4*M*100, 4*M*2) {
		t.Error("pieces 100-101: range covers both")
	}
	if pm.isBitSetOffsetRange(4*M*100-10, 4*M*2) {
		t.Error("start slightly before piece 100: missing byte should fail")
	}
	if pm.isBitSetOffsetRange(4*M*100, 4*M*2+1) {
		t.Error("end slightly past piece 101: missing byte should fail")
	}

	pm.clearAllBit()
	pm.setBit(100)
	pm.setBit(102)
	if pm.isBitSetOffsetRange(4*M*100, 4*M*3) {
		t.Error("pieces 100,102 (101 missing): range should fail")
	}
}

func TestPieceMapGetOffsetCompletedLength(t *testing.T) {
	pm := newPieceMap(1*K, 20*K)

	// All missing
	if l := pm.getOffsetCompletedLength(0, 1*K); l != 0 {
		t.Errorf("all missing: completed(0,1K) = %d, want 0", l)
	}
	if l := pm.getOffsetCompletedLength(0, 0); l != 0 {
		t.Errorf("length 0: completed = %d, want 0", l)
	}

	for i := 2; i <= 4; i++ {
		pm.setBit(i)
	}
	// Pieces 2,3,4 set = bytes 2048..5119 completed
	if l := pm.getOffsetCompletedLength(2048, 3072); l != 3072 {
		t.Errorf("offset 2048, len 3072: completed = %d, want 3072", l)
	}
	if l := pm.getOffsetCompletedLength(2047, 3072); l != 3071 {
		t.Errorf("offset 2047, len 3072: completed = %d, want 3071", l)
	}
	if l := pm.getOffsetCompletedLength(2049, 3072); l != 3071 {
		t.Errorf("offset 2049, len 3072: completed = %d, want 3071", l)
	}
	if l := pm.getOffsetCompletedLength(2048, 0); l != 0 {
		t.Errorf("offset 2048, len 0: completed = %d, want 0", l)
	}
	if l := pm.getOffsetCompletedLength(2048, 1); l != 1 {
		t.Errorf("offset 2048, len 1: completed = %d, want 1", l)
	}
	if l := pm.getOffsetCompletedLength(2047, 1); l != 0 {
		t.Errorf("offset 2047, len 1: completed = %d, want 0", l)
	}
	if l := pm.getOffsetCompletedLength(0, 20*K); l != 3072 {
		t.Errorf("offset 0, len 20K: completed = %d, want 3072", l)
	}
	if l := pm.getOffsetCompletedLength(0, 20*K+10); l != 3072 {
		t.Errorf("offset 0, len 20K+10: completed = %d, want 3072", l)
	}
	if l := pm.getOffsetCompletedLength(20*K, 1); l != 0 {
		t.Errorf("offset 20K, len 1: completed = %d, want 0", l)
	}
}

func TestPieceMapGetOffsetCompletedLengthLargeFile(t *testing.T) {
	blockLen := int32(4 * M)
	totalLen := int64(1 << 40) // 1 TiB
	pm := newPieceMap(blockLen, totalLen)

	base := 1 << 11 // 2048
	pm.setBit(base)
	pm.setBit(base + 1)
	pm.setBit(base + 2)

	cl := pm.getOffsetCompletedLength(1<<33, 1<<24)
	expected := int64(blockLen) * 3
	if cl != expected {
		t.Errorf("large file: completed = %d, want %d", cl, expected)
	}

	cl = pm.getOffsetCompletedLength(int64(1<<33)-int64(blockLen), 1<<24)
	if cl != expected {
		t.Errorf("large file (shifted): completed = %d, want %d", cl, expected)
	}
}

func TestPieceMapGetMissingUnusedLength(t *testing.T) {
	pm := newPieceMap(1*K, 10*K+10) // 11 pieces, last is 10 bytes

	// All unused, all missing from index 0 → totalLength
	if l := pm.getMissingUnusedLength(0); l != 10*K+10 {
		t.Errorf("from 0: missingUnused = %d, want %d", l, 10*K+10)
	}
	// From index 10 (last piece) → 10 bytes
	if l := pm.getMissingUnusedLength(10); l != 10 {
		t.Errorf("from 10: missingUnused = %d, want 10", l)
	}
	// Out of range
	if l := pm.getMissingUnusedLength(11); l != 0 {
		t.Errorf("from 11: missingUnused = %d, want 0", l)
	}
	if l := pm.getMissingUnusedLength(12); l != 0 {
		t.Errorf("from 12: missingUnused = %d, want 0", l)
	}

	// Use bit 5 → stops at 5 blocks
	pm.setUseBit(5)
	if l := pm.getMissingUnusedLength(0); l != 5*K {
		t.Errorf("useBit(5) from 0: missingUnused = %d, want %d", l, 5*K)
	}

	// Set bit 4 → stops at 4 blocks
	pm.setBit(4)
	if l := pm.getMissingUnusedLength(0); l != 4*K {
		t.Errorf("setBit(4) from 0: missingUnused = %d, want %d", l, 4*K)
	}

	// From index 1
	if l := pm.getMissingUnusedLength(1); l != 3*K {
		t.Errorf("from 1: missingUnused = %d, want %d", l, 3*K)
	}
}

func TestPieceMapSetBitRange(t *testing.T) {
	pm := newPieceMap(1*M, 10*M) // 10 pieces
	pm.setBitRange(0, 4)

	for i := 0; i < 5; i++ {
		if !pm.isBitSet(i) {
			t.Errorf("piece %d should be set", i)
		}
	}
	for i := 5; i < 10; i++ {
		if pm.isBitSet(i) {
			t.Errorf("piece %d should NOT be set", i)
		}
	}
	if cl := pm.getCompletedLength(); cl != 5*M {
		t.Errorf("completed length = %d, want %d", cl, 5*M)
	}
}

func TestPieceMapGetAllMissingIndexesNoArg(t *testing.T) {
	pm := newPieceMap(16*K, 1*M) // 64 pieces (1M / 16K = 64)
	nbits := 64
	bf := pm.getAllMissingIndexesNoArg()
	if cnt := countSetBits(bf, nbits); cnt != 64 {
		t.Errorf("all missing: count = %d, want 64", cnt)
	}

	for i := 0; i < 63; i++ {
		pm.setBit(i)
	}
	bf = pm.getAllMissingIndexesNoArg()
	if cnt := countSetBits(bf, nbits); cnt != 1 {
		t.Errorf("63 set, 1 missing: count = %d, want 1", cnt)
	}
	if !testBit(bf, 63) {
		t.Error("last bit (63) should be set")
	}
}

func TestPieceMapGetAllMissingIndexesCheckLastByte(t *testing.T) {
	pm := newPieceMap(16*K, 16*K*2) // 2 pieces
	bf := pm.getAllMissingIndexesNoArg()
	if cnt := countSetBits(bf, 2); cnt != 2 {
		t.Errorf("2 pieces all missing: count = %d, want 2", cnt)
	}
	if !testBit(bf, 0) || !testBit(bf, 1) {
		t.Error("both missing bits should be set")
	}
	// Last byte garbage bits should be 0
	if bf[0]&0x3f != 0 { // bottom 6 bits should be 0
		t.Errorf("last byte garbage bits: 0x%02x, want bottom 6 bits clear", bf[0])
	}
}

func TestPieceMapGetAllMissingIndexes(t *testing.T) {
	pm := newPieceMap(16*K, 1*M) // 64 pieces
	peer := newPieceMap(16*K, 1*M)
	peer.setAllBit()

	bf := pm.getAllMissingIndexes(peer.getBitfield())
	if cnt := countSetBits(bf, 64); cnt != 64 {
		t.Errorf("peer has all, we have none: count = %d, want 64", cnt)
	}

	for i := 0; i < 62; i++ {
		pm.setBit(i)
	}
	peer.unsetBit(62)

	bf = pm.getAllMissingIndexes(peer.getBitfield())
	if cnt := countSetBits(bf, 64); cnt != 1 {
		t.Errorf("we miss 63, peer misses 62: count = %d, want 1", cnt)
	}
	if !testBit(bf, 63) {
		t.Error("only missing bit peer has should be 63")
	}
}

func TestPieceMapGetAllMissingUnusedIndexes(t *testing.T) {
	pm := newPieceMap(16*K, 1*M) // 64 pieces
	peer := newPieceMap(16*K, 1*M)
	peer.setAllBit()

	bf := pm.getAllMissingUnusedIndexes(peer.getBitfield())
	if cnt := countSetBits(bf, 64); cnt != 64 {
		t.Errorf("all missing+unused peer has all: count = %d, want 64", cnt)
	}

	for i := 0; i < 61; i++ {
		pm.setBit(i)
	}
	pm.setUseBit(61)
	peer.unsetBit(62)

	bf = pm.getAllMissingUnusedIndexes(peer.getBitfield())
	if cnt := countSetBits(bf, 64); cnt != 1 {
		t.Errorf("only 63 missing+unused+peer: count = %d, want 1", cnt)
	}
	if !testBit(bf, 63) {
		t.Error("only candidate should be piece 63")
	}
}

func TestPieceMapCountFilteredBlock(t *testing.T) {
	pm := newPieceMap(1*K, 256*K) // 256 pieces
	if nb := pm.countBlock(); nb != 256 {
		t.Errorf("countBlock = %d, want 256", nb)
	}
	if nb := pm.countFilteredBlock(); nb != 0 {
		t.Errorf("filter disabled: countFilteredBlock = %d, want 0", nb)
	}
	pm.addFilter(1*K, 256*K)
	pm.enableFilter()
	if nb := pm.countBlock(); nb != 256 {
		t.Errorf("countBlock with filter = %d, want 256", nb)
	}
	if nb := pm.countFilteredBlock(); nb != 255 {
		t.Errorf("filter [1K,256K): countFilteredBlock = %d, want 255", nb)
	}
	pm.disableFilter()
	if nb := pm.countBlock(); nb != 256 {
		t.Errorf("countBlock after disable = %d, want 256", nb)
	}
	if nb := pm.countFilteredBlock(); nb != 0 {
		t.Errorf("countFilteredBlock after disable = %d, want 0", nb)
	}
}

func TestPieceMapCountMissingBlock(t *testing.T) {
	pm := newPieceMap(1*K, 10*K) // 10 pieces
	if nb := pm.countMissingBlock(); nb != 10 {
		t.Errorf("all missing: countMissingBlock = %d, want 10", nb)
	}
	pm.setBit(1)
	if nb := pm.countMissingBlock(); nb != 9 {
		t.Errorf("after setBit(1): countMissingBlock = %d, want 9", nb)
	}
	pm.setAllBit()
	if nb := pm.countMissingBlock(); nb != 0 {
		t.Errorf("all set: countMissingBlock = %d, want 0", nb)
	}
}

func TestPieceMapZeroLengthFilter(t *testing.T) {
	pm := newPieceMap(1*K, 10*K)
	pm.enableFilter()
	// Filter enabled but no filter bits set → all blocks are excluded from filter
	if nb := pm.countMissingBlock(); nb != 0 {
		t.Errorf("zero-length filter: countMissingBlock = %d, want 0", nb)
	}
}

func TestPieceMapGetFirstNMissingUnusedIndex(t *testing.T) {
	pm := newPieceMap(1*K, 10*K) // 10 pieces
	pm.setUseBit(1)
	pm.setBit(5)

	out := pm.getFirstNMissingUnusedIndex(256)
	expected := []int{0, 2, 3, 4, 6, 7, 8, 9}
	if len(out) != 8 {
		t.Fatalf("getFirstN(256): want %d, got %d", 8, len(out))
	}
	for i, v := range expected {
		if out[i] != v {
			t.Errorf("out[%d] = %d, want %d", i, out[i], v)
		}
	}

	out = pm.getFirstNMissingUnusedIndex(3)
	if len(out) != 3 {
		t.Fatalf("getFirstN(3): want 3, got %d", len(out))
	}
	for i := 0; i < 3; i++ {
		if out[i] != expected[i] {
			t.Errorf("out[%d] = %d, want %d", i, out[i], expected[i])
		}
	}

	out = pm.getFirstNMissingUnusedIndex(0)
	if out != nil {
		t.Error("getFirstN(0): want nil")
	}

	pm.setAllBit()
	out = pm.getFirstNMissingUnusedIndex(10)
	if len(out) != 0 {
		t.Errorf("all set: want empty, got %v", out)
	}

	// With filter
	pm.clearAllBit()
	pm.clearAllUseBit()
	pm.addFilter(9*K, 1*K)
	pm.enableFilter()
	out = pm.getFirstNMissingUnusedIndex(256)
	if len(out) != 1 || out[0] != 9 {
		t.Errorf("filter [9K,10K): want [9], got %v", out)
	}
}

func TestPieceMapGetInorderMissingUnusedIndex(t *testing.T) {
	pm := newPieceMap(1*K, 20*K) // 20 pieces
	ignore := make([]byte, 3)
	minSplit := int32(1 * K)

	// All missing, all unused → first piece
	idx, ok := pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 0 {
		t.Errorf("initial inorder: want (0,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(0)
	idx, ok = pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 1 {
		t.Errorf("useBit(0): want (1,true), got (%d,%v)", idx, ok)
	}

	// With minSplit=2K, piece 1 alone isn't enough (it's separated by useBit(0))
	// Actually, inorder with minSplit=2K from index 1: piece 1 is missing+unused,
	// but piece 0 is used so we can't use i-1 check...
	// Actually, inorder checks: piece 1 has piece 0 as useBit true, so we skip i.
	// Then we look for a run of 2+. Pieces 2..19 are all free, so j=2 gets index 2.
	minSplit = 2 * K
	idx, ok = pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 2 {
		t.Errorf("minSplit=2K after useBit(0): want (2,true), got (%d,%v)", idx, ok)
	}

	pm.unsetUseBit(0)
	pm.setBit(0)
	minSplit = 2 * K
	idx, ok = pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 1 {
		t.Errorf("setBit(0): want (1,true), got (%d,%v)", idx, ok)
	}

	// All set except piece 10
	pm.setAllBit()
	pm.unsetBit(10)
	idx, ok = pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 10 {
		t.Errorf("all but 10: want (10,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(10)
	_, ok = pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if ok {
		t.Error("useBit(10) after all set: should return false")
	}

	// All set → no missing pieces
	pm.unsetUseBit(10)
	pm.setAllBit()
	_, ok = pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if ok {
		t.Error("all bits set: should return false")
	}

	// Ignore bitfield test
	pm.clearAllBit()
	pm.clearAllUseBit()
	// Set ignore bits for pieces 0 and 1
	ignore[0] = 0xc0 // bits for pieces 0,1 set (MSB first 2 bits)
	minSplit = 2 * K
	idx, ok = pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 2 {
		t.Errorf("ignore pieces 0,1: want (2,true), got (%d,%v)", idx, ok)
	}

	// With filter
	pm.clearFilter()
	pm.addFilter(3*K, 3*K)
	pm.enableFilter()
	idx, ok = pm.getInorderMissingUnusedIndex(minSplit, ignore)
	if !ok || idx != 3 {
		t.Errorf("filter [3K,6K)+ignore 0,1: want (3,true), got (%d,%v)", idx, ok)
	}
}

func TestPieceMapGetGeomMissingUnusedIndex(t *testing.T) {
	pm := newPieceMap(1*K, 20*K) // 20 pieces
	ignore := make([]byte, 3)
	minSplit := int32(1 * K)

	// All missing/unused → first piece at offset 0
	idx, ok := pm.getGeomMissingUnusedIndex(minSplit, ignore, 2, 0)
	if !ok || idx != 0 {
		t.Errorf("initial geom: want (0,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(0)
	idx, ok = pm.getGeomMissingUnusedIndex(minSplit, ignore, 2, 0)
	if !ok || idx != 1 {
		t.Errorf("useBit(0): want (1,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(1)
	idx, ok = pm.getGeomMissingUnusedIndex(minSplit, ignore, 2, 0)
	if !ok || idx != 2 {
		t.Errorf("useBit(1): want (2,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(2)
	// Window [0,1): all used → skip. Window [1*2^0+0, 1*2^1+0) = [1,2)... but base^1*offset?
	// Actually geom: start=0, end=1 → window [0+0, 1+0)=[0,1), all bits used.
	// start=1, end=2 → [1,2): piece 1 is used, break. Window done.
	// start=2, end=4 → [2,4): piece 2 used, break. Window done.
	// start=4, end=8 → [4,8): piece 4 is free → idx=4
	idx, ok = pm.getGeomMissingUnusedIndex(minSplit, ignore, 2, 0)
	if !ok || idx != 4 {
		t.Errorf("useBits 0,1,2: want (4,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(4)
	// Window [0,1): all used. [1,2): piece 1 used. [2,4): piece 2 used, piece 3 free.
	// But wait: [2,4) -> piece 2 is used, break immediately (test useBits first).
	// Then [4,8): piece 4 used. Then [8,16): piece 8 free -> idx=8
	idx, ok = pm.getGeomMissingUnusedIndex(minSplit, ignore, 2, 0)
	if !ok || idx != 8 {
		t.Errorf("useBits 0,1,2,4: want (8,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(8)
	// [0,1): used. [1,2): used. [2,4): used(break). [4,8): used(break). [8,16): used(break).
	// [16,32): piece 16 free -> idx=16
	idx, ok = pm.getGeomMissingUnusedIndex(minSplit, ignore, 2, 0)
	if !ok || idx != 16 {
		t.Errorf("useBits 0,1,2,4,8: want (16,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(16)
	// Fall through all geometric windows, falls back to sparse:
	// Gaps: [3,3], [5,7], [9,15], [17,19]
	// Longest is [9,15] (7 blocks), all have prev not-in-use...
	// Actually sparse picks longest gap, start is 9 (prev piece 8 is in use, so start moves to midpoint? No, sparse logic: if start > 0 and useBit[start-1] is set, then start = midIndex.
	// For gap [9,15], start=9. useBit[8] is true (set above). So start = (15-9)/2 + 9 = 3+9=12.
	// So idx=12.
	idx, ok = pm.getGeomMissingUnusedIndex(minSplit, ignore, 2, 0)
	if !ok || idx != 12 {
		t.Errorf("useBits 0,1,2,4,8,16: want (12,true), got (%d,%v)", idx, ok)
	}

	pm.setUseBit(12)
	// fallback to sparse again: now gaps are [3,3], [5,7], [9,11], [13,15], [17,19]
	// Longest: [9,11] and [13,15] both 3 blocks. Let me trace:
	// [3,3]: size=1 -> not longest
	// [5,7]: size=3 -> best
	// [9,11]: size=3 -> tie. Tiebreaker: prev piece set? best has start=5, prev=4 (not in use, set in combined since piece 4 is useBit... wait, piece 4 IS in use. So useBit[4]=true. testBit(combined, 4) = true (useBit makes it true in combined). Wait, combined includes useBits. So combined[4] is set (use bit).
	// The tiebreaker checks: best.start>0 && cur.start>0 && (!testBit(combined,best.start-1) || testBit(useBits,best.start-1)) && testBit(combined,cur.start-1) && !testBit(useBits,cur.start-1)
	// For best [5,7]: best.start-1=4. testBit(combined,4) is true (useBit set). So !testBit(combined,4) = false. testBit(useBits,4) = true. So the first part is (false || true) = true.
	// For cur [9,11]: cur.start-1=8. testBit(combined,8) is true (useBit). !testBit(useBits,8) = false (useBit[8] is set). Second part fails. So best stays [5,7].
	// Actually let me just trust the C++ test which says idx=12 when useBit 12 is set.
	// Actually the test has ANOTHER iteration. Let me look at the original test again.
	//
	// After useBits 0,1,2,4,8,16 are set, it gets 12. Then it sets useBit(12). There's no further check in the test - it just ends.
	pm.setUseBit(12) // This is the end of the C++ test
}

func TestPieceMapLastByteMask(t *testing.T) {
	if m := lastByteMask(0); m != 0 {
		t.Errorf("lastByteMask(0) = 0x%02x, want 0x00", m)
	}
	if m := lastByteMask(8); m != 0xff {
		t.Errorf("lastByteMask(8) = 0x%02x, want 0xff", m)
	}
	if m := lastByteMask(9); m != 0x80 {
		t.Errorf("lastByteMask(9) = 0x%02x, want 0x80", m)
	}
	if m := lastByteMask(12); m != 0xf0 {
		t.Errorf("lastByteMask(12) = 0x%02x, want 0xf0", m)
	}
	if m := lastByteMask(1); m != 0x80 {
		t.Errorf("lastByteMask(1) = 0x%02x, want 0x80", m)
	}
	if m := lastByteMask(7); m != 0xfe {
		t.Errorf("lastByteMask(7) = 0x%02x, want 0xfe", m)
	}
}

func TestPieceMapPopcnt(t *testing.T) {
	if cnt := countSetBits([]byte{0x00}, 8); cnt != 0 {
		t.Errorf("countSetBits(0x00) = %d, want 0", cnt)
	}
	if cnt := countSetBits([]byte{0xff}, 8); cnt != 8 {
		t.Errorf("countSetBits(0xff) = %d, want 8", cnt)
	}
	if cnt := countSetBits([]byte{0x80, 0x01}, 16); cnt != 2 {
		t.Errorf("countSetBits([0x80,0x01]) = %d, want 2", cnt)
	}
	// Test last byte masking
	if cnt := countSetBits([]byte{0xff, 0xff}, 9); cnt != 9 {
		t.Errorf("countSetBits 9 bits all set = %d, want 9", cnt)
	}
	if cnt := countSetBits([]byte{0x00}, 0); cnt != 0 {
		t.Errorf("countSetBits 0 bits = %d, want 0", cnt)
	}
}

func TestPieceMapHasMissingPiece(t *testing.T) {
	pm := newPieceMap(1*K, 10*K) // 10 pieces
	peer := newPieceMap(1*K, 10*K)
	peer.setAllBit()

	if !pm.hasMissingPiece(peer.getBitfield()) {
		t.Error("peer has all, we have none: should have missing piece")
	}

	for i := 0; i < 10; i++ {
		pm.setBit(i)
	}
	if pm.hasMissingPiece(peer.getBitfield()) {
		t.Error("we have all too: should NOT have missing piece")
	}

	pm.clearAllBit()
	peer.clearAllBit()
	if pm.hasMissingPiece(peer.getBitfield()) {
		t.Error("neither has anything: should NOT have missing piece")
	}

	// Mismatched lengths
	shortBF := make([]byte, 1)
	if pm.hasMissingPiece(shortBF) {
		t.Error("mismatched bitfield lengths: should return false")
	}
}

func TestPieceMapGetFirstSetBitIndex(t *testing.T) {
	b := []byte{0x00, 0x0f} // bits: 0-11=0, 12-15=1
	idx, ok := getFirstSetBitIndex(b, 16)
	if !ok || idx != 12 {
		t.Errorf("getFirstSetBitIndex([0x00,0x0f]): want (12,true), got (%d,%v)", idx, ok)
	}

	idx, ok = getFirstSetBitIndex([]byte{0x00}, 8)
	if ok {
		t.Error("getFirstSetBitIndex(all zeros): should return false")
	}
	if idx != 0 {
		t.Errorf("false return should have idx=0, got %d", idx)
	}
}

func TestPieceMapGetBlockLength(t *testing.T) {
	pm := newPieceMap(1*K, 10*K+1) // 11 pieces, last is 1 byte
	if bl := pm.getBlockLength(); bl != 1*K {
		t.Errorf("getBlockLength = %d, want %d", bl, 1*K)
	}
	if bl := pm.getLastBlockLength(); bl != 1 {
		t.Errorf("getLastBlockLength = %d, want 1", bl)
	}
}
