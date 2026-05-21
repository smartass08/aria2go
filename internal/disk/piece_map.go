package disk

import (
	"math/bits"
	"sync"
)

// popcnt is a 256-entry table for fast per-byte popcount.
var popcnt [256]int

func init() {
	for i := 0; i < 256; i++ {
		popcnt[i] = bits.OnesCount8(uint8(i))
	}
}

// bitfieldBufPool reuses temporary bitfield-sized []byte buffers across
// operations to reduce GC pressure in hot paths (piece selection, missing index
// queries). The pool stores *[]byte to avoid the slice-header copy that
// staticcheck flags.
var bitfieldBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 256)
		return &b
	},
}

func getBitfieldBuf(n int) []byte {
	bp := bitfieldBufPool.Get().(*[]byte)
	b := *bp
	if cap(b) >= n {
		return b[:n]
	}
	return make([]byte, n)
}

func putBitfieldBuf(b []byte) {
	bp := &b
	bitfieldBufPool.Put(bp)
}

// lastByteMask returns the bit mask for the last byte of a bitfield of nbits
// bits. Piece 0 is the MSB of byte 0 (network bit order).
//
//	nbits % 8 == 0 → 0xff
//	nbits % 8 != 0 → high (nbits % 8) bits set, e.g. 9 bits → 0x80, 12 bits → 0xf0
func lastByteMask(nbits int) byte {
	if nbits == 0 {
		return 0
	}
	s := nbits % 8
	if s == 0 {
		return 0xff
	}
	return byte(int(-256) >> s)
}

// setBitVal sets or clears the index-th bit in the byte slice b.
// index must be non-negative.
func setBitVal(b []byte, index int, on bool) {
	mask := byte(128 >> (index % 8))
	if on {
		b[index/8] |= mask
	} else {
		b[index/8] &^= mask
	}
}

// testBit reports whether the index-th bit is set in the byte slice b.
// index must be non-negative.
func testBit(b []byte, index int) bool {
	mask := byte(128 >> (index % 8))
	return (b[index/8] & mask) != 0
}

// countSetBits counts the number of set bits in the bitfield (nbits total bits).
func countSetBits(b []byte, nbits int) int {
	if nbits == 0 {
		return 0
	}
	nbytes := (nbits + 7) / 8
	count := 0
	for i := 0; i < nbytes-1; i++ {
		count += popcnt[b[i]]
	}
	count += popcnt[b[nbytes-1]&lastByteMask(nbits)]
	return count
}

// getFirstSetBitIndex finds the first set bit in the bitfield. Returns (index,
// true) if found, or (0, false) if none.
func getFirstSetBitIndex(b []byte, nbits int) (int, bool) {
	for i := 0; i < nbits; i++ {
		if testBit(b, i) {
			return i, true
		}
	}
	return 0, false
}

// getFirstNSetBitIndex collects up to n set bit indices from the bitfield.
// Returns the collected indices.
func getFirstNSetBitIndex(b []byte, nbits, n int) []int {
	if n == 0 {
		return nil
	}
	var out []int
	for i := 0; i < nbits && len(out) < n; i++ {
		if testBit(b, i) {
			out = append(out, i)
		}
	}
	return out
}

// pieceMap tracks which pieces have been verified (written + hash-checked).
// It implements the aria2 BitfieldMan: piece-level tracking with filters,
// use bits, cached lengths, and piece selection strategies.
type pieceMap struct {
	mu            sync.RWMutex
	blockLength   int32
	totalLength   int64
	blocks        int
	bitfieldLen   int
	haveBits      []byte
	useBits       []byte
	filterBits    []byte
	filterEnabled bool

	cachedCompletedLen         int64
	cachedFilteredCompletedLen int64
	cachedFilteredTotalLen     int64
	cachedNumMissingBlock      int
	cachedNumFilteredBlock     int
}

// newPieceMap creates a pieceMap with the given block (piece) length and total
// file length. If blockLength <= 0 or totalLength <= 0, the map is created with
// zero blocks (no-op state).
func newPieceMap(blockLength int32, totalLength int64) *pieceMap {
	pm := &pieceMap{
		blockLength: blockLength,
		totalLength: totalLength,
	}
	if blockLength > 0 && totalLength > 0 {
		pm.blocks = int((totalLength + int64(blockLength) - 1) / int64(blockLength))
		pm.bitfieldLen = pm.blocks/8 + b2i(pm.blocks%8 != 0)
		pm.haveBits = make([]byte, pm.bitfieldLen)
		pm.useBits = make([]byte, pm.bitfieldLen)
		pm.updateCache()
	}
	return pm
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Block / piece sizing
// ---------------------------------------------------------------------------

func (pm *pieceMap) getBlockLength() int32 {
	return pm.blockLength
}

func (pm *pieceMap) getLastBlockLength() int32 {
	return int32(pm.totalLength - int64(pm.blockLength)*int64(pm.blocks-1))
}

// getBlockLengthForIndex returns the byte length of piece i.
// Returns 0 for out-of-range indices.
func (pm *pieceMap) getBlockLengthForIndex(index int) int32 {
	if index == pm.blocks-1 {
		return pm.getLastBlockLength()
	}
	if index < pm.blocks-1 {
		return pm.blockLength
	}
	return 0
}

// ---------------------------------------------------------------------------
// Basic bit operations
// ---------------------------------------------------------------------------

func (pm *pieceMap) setBitInternal(b []byte, index int, on bool) bool {
	if index < 0 || pm.blocks <= index || pm.bitfieldLen == 0 {
		return false
	}
	setBitVal(b, index, on)
	return true
}

func (pm *pieceMap) setBit(index int) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	ok := pm.setBitInternal(pm.haveBits, index, true)
	pm.updateCache()
	return ok
}

func (pm *pieceMap) unsetBit(index int) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	ok := pm.setBitInternal(pm.haveBits, index, false)
	pm.updateCache()
	return ok
}

func (pm *pieceMap) isBitSet(index int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if index < 0 || index >= pm.blocks || pm.bitfieldLen == 0 {
		return false
	}
	return testBit(pm.haveBits, index)
}

func (pm *pieceMap) setUseBit(index int) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	ok := pm.setBitInternal(pm.useBits, index, true)
	pm.updateCache()
	return ok
}

func (pm *pieceMap) unsetUseBit(index int) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	ok := pm.setBitInternal(pm.useBits, index, false)
	pm.updateCache()
	return ok
}

func (pm *pieceMap) isUseBitSet(index int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if index < 0 || index >= pm.blocks || pm.bitfieldLen == 0 {
		return false
	}
	return testBit(pm.useBits, index)
}

func (pm *pieceMap) isAllBitSet() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return testAllBitSet(pm.haveBits, pm.bitfieldLen, pm.blocks)
}

func testAllBitSet(b []byte, length, nbits int) bool {
	if length == 0 {
		return true
	}
	for i := 0; i < length-1; i++ {
		if b[i] != 0xff {
			return false
		}
	}
	return b[length-1] == lastByteMask(nbits)
}

// ---------------------------------------------------------------------------
// Range operations
// ---------------------------------------------------------------------------

func (pm *pieceMap) setBitRange(startIndex, endIndex int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := startIndex; i <= endIndex && i < pm.blocks; i++ {
		pm.setBitInternal(pm.haveBits, i, true)
	}
	pm.updateCache()
}

func (pm *pieceMap) unsetBitRange(startIndex, endIndex int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := startIndex; i <= endIndex && i < pm.blocks; i++ {
		pm.setBitInternal(pm.haveBits, i, false)
	}
	pm.updateCache()
}

func (pm *pieceMap) isBitRangeSet(startIndex, endIndex int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for i := startIndex; i <= endIndex && i < pm.blocks; i++ {
		if !testBit(pm.haveBits, i) {
			return false
		}
	}
	return true
}

func (pm *pieceMap) setAllBit() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := 0; i < pm.blocks; i++ {
		pm.setBitInternal(pm.haveBits, i, true)
	}
	pm.updateCache()
}

func (pm *pieceMap) clearAllBit() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := range pm.haveBits {
		pm.haveBits[i] = 0
	}
	pm.updateCache()
}

func (pm *pieceMap) clearAllUseBit() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := range pm.useBits {
		pm.useBits[i] = 0
	}
	pm.updateCache()
}

func (pm *pieceMap) setAllUseBit() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i := 0; i < pm.blocks; i++ {
		pm.setBitInternal(pm.useBits, i, true)
	}
}

func (pm *pieceMap) getBitfield() []byte {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.bitfieldLen == 0 {
		return nil
	}
	bf := make([]byte, pm.bitfieldLen)
	copy(bf, pm.haveBits)
	return bf
}

func (pm *pieceMap) setBitfield(bitfield []byte) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.bitfieldLen == 0 || len(bitfield) != pm.bitfieldLen {
		return
	}
	copy(pm.haveBits, bitfield)
	for i := range pm.useBits {
		pm.useBits[i] = 0
	}
	pm.updateCache()
}

// ---------------------------------------------------------------------------
// Filter system
// ---------------------------------------------------------------------------

func (pm *pieceMap) ensureFilter() {
	if pm.filterBits == nil {
		pm.filterBits = make([]byte, pm.bitfieldLen)
	}
}

func (pm *pieceMap) addFilter(offset, length int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ensureFilter()
	if length > 0 && pm.blocks > 0 {
		startBlock := offset / int64(pm.blockLength)
		endBlock := (offset + length - 1) / int64(pm.blockLength)
		for i := startBlock; i <= endBlock && i < int64(pm.blocks); i++ {
			setBitVal(pm.filterBits, int(i), true)
		}
	}
	pm.updateCache()
}

func (pm *pieceMap) removeFilter(offset, length int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ensureFilter()
	if length > 0 && pm.blocks > 0 {
		startBlock := offset / int64(pm.blockLength)
		endBlock := (offset + length - 1) / int64(pm.blockLength)
		for i := startBlock; i <= endBlock && i < int64(pm.blocks); i++ {
			setBitVal(pm.filterBits, int(i), false)
		}
	}
	pm.updateCache()
}

func (pm *pieceMap) addNotFilter(offset, length int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ensureFilter()
	if length > 0 && pm.blocks > 0 {
		startBlock := offset / int64(pm.blockLength)
		if int64(pm.blocks) <= startBlock {
			startBlock = int64(pm.blocks)
		}
		endBlock := (offset + length - 1) / int64(pm.blockLength)
		for i := int64(0); i < startBlock; i++ {
			setBitVal(pm.filterBits, int(i), true)
		}
		for i := endBlock + 1; i < int64(pm.blocks); i++ {
			setBitVal(pm.filterBits, int(i), true)
		}
	}
	pm.updateCache()
}

func (pm *pieceMap) clearFilter() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.filterBits = nil
	pm.filterEnabled = false
	pm.updateCache()
}

func (pm *pieceMap) enableFilter() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ensureFilter()
	pm.filterEnabled = true
	pm.updateCache()
}

func (pm *pieceMap) disableFilter() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.filterEnabled = false
	pm.updateCache()
}

func (pm *pieceMap) isFilterEnabled() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.filterEnabled
}

func (pm *pieceMap) isFilterBitSet(index int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.filterBits == nil {
		return false
	}
	if index < 0 || index >= pm.blocks {
		return false
	}
	return testBit(pm.filterBits, index)
}

func (pm *pieceMap) isFilteredAllBitSet() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.filterEnabled && pm.filterBits != nil {
		for i := 0; i < pm.bitfieldLen; i++ {
			if (pm.haveBits[i] & pm.filterBits[i]) != pm.filterBits[i] {
				return false
			}
		}
		return true
	}
	return pm.testAllBitSetLocked()
}

func (pm *pieceMap) isAllFilterBitSet() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.filterBits == nil {
		return false
	}
	return testAllBitSet(pm.filterBits, pm.bitfieldLen, pm.blocks)
}

func (pm *pieceMap) getFilterBitfield() []byte {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.filterBits == nil {
		return nil
	}
	bf := make([]byte, pm.bitfieldLen)
	copy(bf, pm.filterBits)
	return bf
}

func (pm *pieceMap) testAllBitSetLocked() bool {
	return testAllBitSet(pm.haveBits, pm.bitfieldLen, pm.blocks)
}

// ---------------------------------------------------------------------------
// Missing / unused index queries
// ---------------------------------------------------------------------------

func (pm *pieceMap) getFirstMissingIndex() (int, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	buf := getBitfieldBuf(pm.bitfieldLen)
	defer putBitfieldBuf(buf)
	if pm.filterEnabled {
		andNotInto(buf, pm.filterBits, pm.haveBits)
		return getFirstSetBitIndex(buf[:pm.bitfieldLen], pm.blocks)
	}
	notInto(buf, pm.haveBits)
	return getFirstSetBitIndex(buf[:pm.bitfieldLen], pm.blocks)
}

func (pm *pieceMap) getFirstMissingUnusedIndex() (int, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	buf := getBitfieldBuf(pm.bitfieldLen)
	defer putBitfieldBuf(buf)
	if pm.filterEnabled {
		for i := 0; i < pm.bitfieldLen; i++ {
			buf[i] = ^(pm.haveBits[i] | pm.useBits[i]) & pm.filterBits[i]
		}
	} else {
		for i := 0; i < pm.bitfieldLen; i++ {
			buf[i] = ^(pm.haveBits[i] | pm.useBits[i])
		}
	}
	return getFirstSetBitIndex(buf[:pm.bitfieldLen], pm.blocks)
}

func (pm *pieceMap) getFirstNMissingUnusedIndex(n int) []int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	buf := getBitfieldBuf(pm.bitfieldLen)
	defer putBitfieldBuf(buf)
	if pm.filterEnabled {
		for i := 0; i < pm.bitfieldLen; i++ {
			buf[i] = ^(pm.haveBits[i] | pm.useBits[i]) & pm.filterBits[i]
		}
	} else {
		for i := 0; i < pm.bitfieldLen; i++ {
			buf[i] = ^(pm.haveBits[i] | pm.useBits[i])
		}
	}
	return getFirstNSetBitIndex(buf[:pm.bitfieldLen], pm.blocks, n)
}

// combinedMissing constructs a composite bitfield where set bits represent
// "unavailable" pieces: either we have them, they're in use, they're in
// ignoreBitfield, or they're outside the filter. Pieces that are ZERO in the
// result are candidate missing-unused-in-filter pieces.
// The returned buffer is from the bitfield pool and must be freed by the caller.
func (pm *pieceMap) combinedMissing(ignoreBitfield []byte) []byte {
	c := getBitfieldBuf(pm.bitfieldLen)
	for i := 0; i < pm.bitfieldLen; i++ {
		v := pm.haveBits[i] | pm.useBits[i]
		if i < len(ignoreBitfield) {
			v |= ignoreBitfield[i]
		}
		if pm.filterEnabled && pm.filterBits != nil {
			v |= ^pm.filterBits[i]
		}
		c[i] = v
	}
	return c[:pm.bitfieldLen]
}

// getSparseMissingUnusedIndex finds the longest contiguous sequence of
// missing+unused pieces and returns the index at the start of the sequence,
// or the midpoint if startIndex-1 is in use. Respects minSplitSize.
func (pm *pieceMap) getSparseMissingUnusedIndex(minSplitSize int32, ignoreBitfield []byte) (int, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	c := pm.combinedMissing(ignoreBitfield)
	defer putBitfieldBuf(c)
	return sparseSelect(c, pm.useBits, pm.blocks, pm.blockLength, minSplitSize)
}

func sparseSelect(combined, useBits []byte, blocks int, blockLength, minSplitSize int32) (int, bool) {
	type span struct{ start, end int }
	var best span
	nextIdx := 0
	for nextIdx < blocks {
		// find start of gap (where combined has 0)
		start := nextIdx
		for start < blocks && testBit(combined, start) {
			start++
		}
		if start == blocks {
			break
		}
		// find end of gap (where combined has 1 again)
		end := start
		for end < blocks && !testBit(combined, end) {
			end++
		}
		cur := span{start, end}

		// Adjust start: if the piece before the gap is usable (have but not in use),
		// start from the middle of the gap.
		if cur.start > 0 && testBit(useBits, cur.start-1) {
			cur.start = (cur.end-cur.start)/2 + cur.start
		}

		if best.end-best.start < cur.end-cur.start {
			best = cur
		} else if best.end-best.start == cur.end-cur.start &&
			best.start > 0 && cur.start > 0 &&
			(!testBit(combined, best.start-1) || testBit(useBits, best.start-1)) &&
			testBit(combined, cur.start-1) && !testBit(useBits, cur.start-1) {
			best = cur
		}
		nextIdx = end
	}

	if best.end-best.start > 0 {
		if best.start == 0 {
			return 0, true
		}
		if (!testBit(useBits, best.start-1) && testBit(combined, best.start-1)) ||
			int64(best.end-best.start)*int64(blockLength) >= int64(minSplitSize) {
			return best.start, true
		}
		return 0, false
	}
	return 0, false
}

// getGeomMissingUnusedIndex uses a geometric progression to search for
// missing+unused pieces. It starts at offsetIndex and examines windows of
// exponentially growing size (base^i). Within each window, it returns the
// first missing+unused piece. Falls back to sparse selection if no piece found.
func (pm *pieceMap) getGeomMissingUnusedIndex(minSplitSize int32, ignoreBitfield []byte, base float64, offsetIndex int) (int, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	c := pm.combinedMissing(ignoreBitfield)
	defer putBitfieldBuf(c)
	return geomSelect(c, pm.useBits, pm.blocks, pm.blockLength, minSplitSize, base, offsetIndex)
}

func geomSelect(combined, useBits []byte, blocks int, blockLength, minSplitSize int32, base float64, offsetIndex int) (int, bool) {
	start := 0.0
	end := 1.0
	for int(start)+offsetIndex < blocks {
		idx := blocks
		windowEnd := int(end + float64(offsetIndex))
		if windowEnd > blocks {
			windowEnd = blocks
		}
		for i := int(start) + offsetIndex; i < windowEnd; i++ {
			if testBit(useBits, i) {
				break
			}
			if !testBit(combined, i) {
				idx = i
				break
			}
		}
		if idx < blocks {
			return idx, true
		}
		start = end
		end *= base
	}
	return sparseSelect(combined, useBits, blocks, blockLength, minSplitSize)
}

// getInorderMissingUnusedIndex selects the first missing+unused piece in
// sequential order, respecting minSplitSize and ignoreBitfield.
func (pm *pieceMap) getInorderMissingUnusedIndex(minSplitSize int32, ignoreBitfield []byte) (int, bool) {
	return pm.getInorderMissingUnusedIndexRange(0, pm.blocks, minSplitSize, ignoreBitfield)
}

// getInorderMissingUnusedIndexRange selects the first missing+unused piece
// in the range [startIndex, endIndex).
func (pm *pieceMap) getInorderMissingUnusedIndexRange(startIndex, endIndex int, minSplitSize int32, ignoreBitfield []byte) (int, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if endIndex > pm.blocks {
		endIndex = pm.blocks
	}
	c := pm.combinedMissing(ignoreBitfield)
	defer putBitfieldBuf(c)
	return inorderSelect(c, pm.useBits, pm.blocks, pm.blockLength, minSplitSize, startIndex, endIndex)
}

func inorderSelect(combined, useBits []byte, blocks int, blockLength, minSplitSize int32, startIndex, endIndex int) (int, bool) {
	if startIndex >= endIndex || startIndex >= blocks {
		return 0, false
	}
	// Always return first piece if available.
	if !testBit(combined, startIndex) && !testBit(useBits, startIndex) {
		return startIndex, true
	}
	for i := startIndex + 1; i < endIndex; {
		if testBit(combined, i) || testBit(useBits, i) {
			i++
			continue
		}
		// If previous piece has been retrieved (not in use, we have it),
		// we can download from i.
		if !testBit(useBits, i-1) && testBit(combined, i-1) {
			return i, true
		}
		// Check if there's enough free space for minSplitSize.
		j := i
		for ; j < blocks; j++ {
			if testBit(combined, j) || testBit(useBits, j) {
				break
			}
			if int64(j-i+1)*int64(blockLength) >= int64(minSplitSize) {
				return j, true
			}
		}
		i = j + 1
	}
	return 0, false
}

func (pm *pieceMap) countMissingBlock() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.cachedNumMissingBlock
}

func (pm *pieceMap) countBlock() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.blocks
}

func (pm *pieceMap) countFilteredBlock() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.cachedNumFilteredBlock
}

// ---------------------------------------------------------------------------
// Bulk missing-index operations
// ---------------------------------------------------------------------------

// getAllMissingIndexesNoArg returns a bitfield where set bits indicate missing
// pieces (affected by filter). Last byte garbage bits are masked to 0.
func (pm *pieceMap) getAllMissingIndexesNoArg() []byte {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.filterEnabled {
		return copyWithLastByteMask(andNot(pm.filterBits, pm.haveBits), pm.blocks)
	}
	return copyWithLastByteMask(not(pm.haveBits), pm.blocks)
}

// getAllMissingIndexes returns a bitfield where set bits indicate pieces that
// we're missing AND the peer has.
func (pm *pieceMap) getAllMissingIndexes(peerBitfield []byte) []byte {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.bitfieldLen != len(peerBitfield) {
		return nil
	}
	// C++: ~bitfield_ & peerBitfield [& filterBitfield_]
	d := and(not(pm.haveBits), peerBitfield)
	if pm.filterEnabled && pm.filterBits != nil {
		d = and(d, pm.filterBits)
	}
	return copyWithLastByteMask(d, pm.blocks)
}

// getAllMissingUnusedIndexes returns a bitfield where set bits indicate pieces
// the peer has, we're missing, and neither we nor the peer have in use.
func (pm *pieceMap) getAllMissingUnusedIndexes(peerBitfield []byte) []byte {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.bitfieldLen != len(peerBitfield) {
		return nil
	}
	// C++: ~bitfield_ & ~useBitfield_ & peerBitfield [& filterBitfield_]
	d := and(not(pm.haveBits), not(pm.useBits), peerBitfield)
	if pm.filterEnabled && pm.filterBits != nil {
		d = and(d, pm.filterBits)
	}
	return copyWithLastByteMask(d, pm.blocks)
}

func (pm *pieceMap) hasMissingPiece(peerBitfield []byte) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.bitfieldLen != len(peerBitfield) {
		return false
	}
	for i := 0; i < pm.bitfieldLen; i++ {
		temp := peerBitfield[i] & ^pm.haveBits[i]
		if pm.filterEnabled && pm.filterBits != nil {
			temp &= pm.filterBits[i]
		}
		if temp != 0 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Offset / byte-length operations
// ---------------------------------------------------------------------------

func (pm *pieceMap) isBitSetOffsetRange(offset, length int64) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if length <= 0 || pm.totalLength <= offset {
		return false
	}
	if pm.totalLength < offset+length {
		length = pm.totalLength - offset
	}
	startBlock := offset / int64(pm.blockLength)
	endBlock := (offset + length - 1) / int64(pm.blockLength)
	for i := startBlock; i <= endBlock && i < int64(pm.blocks); i++ {
		if !testBit(pm.haveBits, int(i)) {
			return false
		}
	}
	return true
}

func (pm *pieceMap) getOffsetCompletedLength(offset, length int64) int64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if length == 0 || pm.totalLength <= offset || pm.blockLength == 0 {
		return 0
	}
	if pm.totalLength < offset+length {
		length = pm.totalLength - offset
	}
	start := int(offset / int64(pm.blockLength))
	end := int((offset + length - 1) / int64(pm.blockLength))
	var res int64
	if start == end {
		if testBit(pm.haveBits, start) {
			res = length
		}
	} else {
		if testBit(pm.haveBits, start) {
			res += int64(start+1)*int64(pm.blockLength) - offset
		}
		for i := start + 1; i <= end-1; i++ {
			if testBit(pm.haveBits, i) {
				res += int64(pm.blockLength)
			}
		}
		if testBit(pm.haveBits, end) {
			res += offset + length - int64(end)*int64(pm.blockLength)
		}
	}
	return res
}

func (pm *pieceMap) getMissingUnusedLength(startingIndex int) int64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.blocks <= startingIndex {
		return 0
	}
	var length int64
	for i := startingIndex; i < pm.blocks; i++ {
		if testBit(pm.haveBits, i) || testBit(pm.useBits, i) {
			break
		}
		length += int64(pm.getBlockLengthForIndex(i))
	}
	return length
}

func (pm *pieceMap) getFilteredTotalLength() int64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.cachedFilteredTotalLen
}

func (pm *pieceMap) getCompletedLength() int64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.cachedCompletedLen
}

func (pm *pieceMap) getFilteredCompletedLength() int64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.cachedFilteredCompletedLen
}

// ---------------------------------------------------------------------------
// Cache update
// ---------------------------------------------------------------------------

func (pm *pieceMap) updateCache() {
	if pm.blocks == 0 {
		pm.cachedNumMissingBlock = 0
		pm.cachedNumFilteredBlock = 0
		pm.cachedFilteredTotalLen = 0
		pm.cachedCompletedLen = 0
		pm.cachedFilteredCompletedLen = 0
		return
	}
	pm.cachedNumMissingBlock = pm.countMissingBlockNow()
	pm.cachedNumFilteredBlock = pm.countFilteredBlockNow()
	pm.cachedFilteredTotalLen = pm.computeFilteredTotalLen()
	pm.cachedCompletedLen = pm.computeCompletedLen(false)
	pm.cachedFilteredCompletedLen = pm.computeCompletedLen(true)
}

func (pm *pieceMap) countMissingBlockNow() int {
	if pm.filterEnabled && pm.filterBits != nil {
		total := countSetBits(pm.filterBits, pm.blocks)
		buf := getBitfieldBuf(pm.bitfieldLen)
		andInto(buf, pm.haveBits, pm.filterBits)
		have := countSetBits(buf[:pm.bitfieldLen], pm.blocks)
		putBitfieldBuf(buf)
		return total - have
	}
	return pm.blocks - countSetBits(pm.haveBits, pm.blocks)
}

// countFilteredBlockNow returns the number of blocks in the filter (valid only
// when filter is enabled).
func (pm *pieceMap) countFilteredBlockNow() int {
	if pm.filterEnabled && pm.filterBits != nil {
		return countSetBits(pm.filterBits, pm.blocks)
	}
	return 0
}

func (pm *pieceMap) computeFilteredTotalLen() int64 {
	if pm.filterBits == nil {
		return 0
	}
	filteredBlocks := countSetBits(pm.filterBits, pm.blocks)
	if filteredBlocks == 0 {
		return 0
	}
	if testBit(pm.filterBits, pm.blocks-1) {
		return int64(filteredBlocks-1)*int64(pm.blockLength) + int64(pm.getLastBlockLength())
	}
	return int64(filteredBlocks) * int64(pm.blockLength)
}

func (pm *pieceMap) computeCompletedLen(useFilter bool) int64 {
	b := pm.haveBits
	var poolBuf []byte
	if useFilter && pm.filterEnabled && pm.filterBits != nil {
		poolBuf = getBitfieldBuf(pm.bitfieldLen)
		andInto(poolBuf, pm.haveBits, pm.filterBits)
		b = poolBuf[:pm.bitfieldLen]
		defer putBitfieldBuf(poolBuf)
	}
	completed := countSetBits(b, pm.blocks)
	if completed == 0 {
		return 0
	}
	if testBit(b, pm.blocks-1) {
		return int64(completed-1)*int64(pm.blockLength) + int64(pm.getLastBlockLength())
	}
	return int64(completed) * int64(pm.blockLength)
}

// ---------------------------------------------------------------------------
// Bitfield algebra helpers (allocate new slice, caller must hold lock)
// ---------------------------------------------------------------------------

func not(a []byte) []byte {
	r := make([]byte, len(a))
	for i := range a {
		r[i] = ^a[i]
	}
	return r
}

func or(a, b []byte) []byte {
	r := make([]byte, len(a))
	for i := range a {
		r[i] = a[i] | b[i]
	}
	return r
}

func and(a, b []byte, extra ...[]byte) []byte {
	r := make([]byte, len(a))
	for i := range a {
		v := a[i] & b[i]
		for _, e := range extra {
			if i < len(e) {
				v &= e[i]
			}
		}
		r[i] = v
	}
	return r
}

func andNot(a []byte, bs ...[]byte) []byte {
	r := make([]byte, len(a))
	for i := range a {
		v := a[i]
		for _, b := range bs {
			if i < len(b) {
				v &^= b[i]
			}
		}
		r[i] = v
	}
	return r
}

func copyWithLastByteMask(b []byte, nbits int) []byte {
	r := make([]byte, len(b))
	copy(r, b)
	if len(r) > 0 {
		r[len(r)-1] &= lastByteMask(nbits)
	}
	return r
}

// notInto writes ^a into dst in-place. dst must be at least len(a).
func notInto(dst, a []byte) {
	for i := range a {
		dst[i] = ^a[i]
	}
}

// andInto writes a&b into dst in-place. dst must be at least len(a).
func andInto(dst, a, b []byte) {
	for i := range a {
		dst[i] = a[i] & b[i]
	}
}

// orInto writes a|b into dst in-place. dst must be at least len(a).
func orInto(dst, a, b []byte) {
	for i := range a {
		dst[i] = a[i] | b[i]
	}
}

// andNotInto writes a &^ b1 &^ b2... into dst in-place.
func andNotInto(dst, a []byte, bs ...[]byte) {
	for i := range a {
		v := a[i]
		for _, b := range bs {
			if i < len(b) {
				v &^= b[i]
			}
		}
		dst[i] = v
	}
}

// ---------------------------------------------------------------------------
// Backward-compatible API (used by MultiFile)
// ---------------------------------------------------------------------------

// setCount changes the number of pieces. If n differs from the current count,
// bitfields are cleared and reallocated. Same n is a no-op.
func (pm *pieceMap) setCount(n int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.blocks == n {
		return
	}
	pm.blocks = n
	pm.bitfieldLen = n/8 + b2i(n%8 != 0)
	pm.haveBits = make([]byte, pm.bitfieldLen)
	pm.useBits = make([]byte, pm.bitfieldLen)
	pm.filterBits = nil
	pm.filterEnabled = false
	pm.totalLength = int64(pm.blockLength) * int64(n)
	pm.updateCache()
}

// mark sets piece i as verified (ok=true) or unverified (ok=false).
func (pm *pieceMap) mark(i int, ok bool) {
	if ok {
		pm.setBit(i)
	} else {
		pm.unsetBit(i)
	}
}

// have reports whether piece i is marked as verified.
func (pm *pieceMap) have(i int) bool {
	return pm.isBitSet(i)
}

// count returns the total number of pieces.
func (pm *pieceMap) count() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.blocks
}

// bitfield returns a byte slice representing the bitfield.
// Piece 0 is the MSB of byte 0 (network bit order).
func (pm *pieceMap) bitfield() []byte {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.blocks == 0 {
		return nil
	}
	nbytes := (pm.blocks + 7) / 8
	bf := make([]byte, nbytes)
	for i := 0; i < pm.blocks; i++ {
		if testBit(pm.haveBits, i) {
			byteIdx := i / 8
			bitIdx := 7 - (i % 8)
			bf[byteIdx] |= 1 << bitIdx
		}
	}
	return bf
}

// missing returns indices of all unverified pieces in sorted order.
func (pm *pieceMap) missing() []int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pm.blocks == 0 {
		return nil
	}
	var out []int
	for i := 0; i < pm.blocks; i++ {
		if !testBit(pm.haveBits, i) {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
