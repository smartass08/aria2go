package peer

func (c *Conn) hasAllPieces() bool {
	if c.bitfield == nil || c.numPieces == 0 {
		return false
	}
	if c.hasAllPiecesCache.Load() {
		return true
	}
	for i := 0; i < c.numPieces; i++ {
		byteIdx := i / 8
		bitIdx := 7 - (i % 8)
		if byteIdx >= len(c.bitfield) || c.bitfield[byteIdx]&(1<<bitIdx) == 0 {
			return false
		}
	}
	c.hasAllPiecesCache.Store(true)
	return true
}

func (c *Conn) hasPiece(index int) bool {
	if c.bitfield == nil || index < 0 || index >= c.numPieces {
		return false
	}
	byteIdx := index / 8
	bitIdx := 7 - (index % 8)
	if byteIdx >= len(c.bitfield) {
		return false
	}
	return c.bitfield[byteIdx]&(1<<bitIdx) != 0
}

func (c *Conn) updateBitfield(index int, operation int) {
	if c.bitfield == nil || index < 0 || index >= c.numPieces {
		return
	}
	byteIdx := index / 8
	bitIdx := 7 - (index % 8)
	if byteIdx >= len(c.bitfield) {
		return
	}
	if operation == 1 {
		c.bitfield[byteIdx] |= 1 << bitIdx
	} else if operation == 0 {
		c.bitfield[byteIdx] &^= 1 << bitIdx
	}
	c.hasAllPiecesCache.Store(false)
}

func (c *Conn) markSeeder() {
	if c.bitfield == nil {
		return
	}
	for i := range c.bitfield {
		c.bitfield[i] = 0xff
	}
	if c.numPieces > 0 {
		lastBits := c.numPieces % 8
		if lastBits != 0 {
			c.bitfield[len(c.bitfield)-1] = byte(0xff << (8 - lastBits))
		}
	}
	c.hasAllPiecesCache.Store(true)
}

func (c *Conn) handleBitfield(bf []byte) error {
	if c.numPieces == 0 {
		if len(bf) != 0 {
			return validateBitfieldPayload(bf, c.numPieces)
		}
		return nil
	}
	if err := validateBitfieldPayload(bf, c.numPieces); err != nil {
		return err
	}
	needLen := (c.numPieces + 7) / 8
	c.bitfield = make([]byte, needLen)
	copy(c.bitfield, bf)
	c.hasAllPiecesCache.Store(false)
	return nil
}

func (c *Conn) peerAllowedIndexSetContains(index int) bool {
	return c.allowedFastSet != nil && c.allowedFastSet[index]
}

func (c *Conn) addPeerAllowedIndex(index int) {
	c.SetAllowedFast(index)
}

func (c *Conn) amAllowedIndexSetContains(index int) bool {
	return c.amAllowedFast != nil && c.amAllowedFast[index]
}

func (c *Conn) addAmAllowedIndex(index int) {
	if c.amAllowedFast == nil {
		c.amAllowedFast = make(map[int]bool)
	}
	c.amAllowedFast[index] = true
}

func (c *Conn) extendedMessageEnabled() bool {
	return c.extMsgEnabled
}

func (c *Conn) setExtendedMessagingEnabled(b bool) {
	c.extMsgEnabled = b
}

func (c *Conn) getExtensionMessageID(key int) uint8 {
	if c.extensionIDs == nil {
		return 0
	}
	return c.extensionIDs[key]
}

func (c *Conn) addExtension(key int, id uint8) {
	if c.extensionIDs == nil {
		c.extensionIDs = make(map[int]uint8)
	}
	c.extensionIDs[key] = id
}

func (c *Conn) fastExtensionEnabled() bool {
	return c.fastExtEnabled
}

func (c *Conn) setFastExtensionEnabled(b bool) {
	c.fastExtEnabled = b
}

func (c *Conn) snubbing() bool {
	return c.snubbing_
}

func (c *Conn) setSnubbing(b bool) {
	c.snubbing_ = b
	if b {
		c.chokingReq_ = true
		c.optUnchoke_ = false
	}
}

func (c *Conn) amChoking() bool {
	return c.stats.choked.Load()
}

func (c *Conn) setAmChoking(b bool) {
	c.stats.choked.Store(b)
}

func (c *Conn) amInterested() bool {
	return c.stats.interested.Load()
}

func (c *Conn) setAmInterested(b bool) {
	c.stats.interested.Store(b)
}

func (c *Conn) peerChoking() bool {
	return c.stats.peerChoking.Load()
}

func (c *Conn) setPeerChoking(b bool) {
	c.stats.peerChoking.Store(b)
}

func (c *Conn) peerInterested() bool {
	return c.stats.peerInterest.Load()
}

func (c *Conn) setPeerInterested(b bool) {
	c.stats.peerInterest.Store(b)
}

func (c *Conn) chokingRequired() bool {
	return c.chokingReq_
}

func (c *Conn) setChokingRequired(b bool) {
	c.chokingReq_ = b
}

func (c *Conn) optUnchoking() bool {
	return c.optUnchoke_
}

func (c *Conn) setOptUnchoking(b bool) {
	c.optUnchoke_ = b
}

func (c *Conn) shouldBeChoking() bool {
	if c.optUnchoke_ {
		return false
	}
	return c.chokingReq_
}

func (c *Conn) countOutstandingRequest() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requestSlots)
}

func (c *Conn) updateUploadLength(n int) {
	c.stats.addUploaded(n)
}

func (c *Conn) updateDownloadLength(n int) {
	c.stats.addDownloaded(n)
}

func (c *Conn) uploadLength() int64 {
	return c.stats.snapshot().Uploaded
}

func (c *Conn) downloadLength() int64 {
	return c.stats.snapshot().Downloaded
}

func countSeeder(peers []*Conn) int {
	n := 0
	for _, p := range peers {
		if p.hasAllPieces() {
			n++
		}
	}
	return n
}

func (c *Conn) setNumPieces(n int) {
	c.numPieces = n
}

func (c *Conn) initBitfield() {
	needLen := (c.numPieces + 7) / 8
	c.bitfield = make([]byte, needLen)
}
