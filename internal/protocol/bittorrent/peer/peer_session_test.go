package peer

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

func newTestConn() *Conn {
	cfg := testConfig()
	c := &Conn{
		cfg:         cfg,
		sendCh:      make(chan []byte, defaultSendQueueSize),
		recvCh:      make(chan Message, defaultSendQueueSize),
		done:        make(chan struct{}),
		chokingReq_: true,
	}
	c.stats.choked.Store(true)
	c.stats.peerChoking.Store(true)
	return c
}

func TestPeerAllowedIndexSetContains(t *testing.T) {
	c := newTestConn()

	if c.peerAllowedIndexSetContains(567) {
		t.Error("expected false for index not in set")
	}
	c.addPeerAllowedIndex(567)
	c.addPeerAllowedIndex(789)

	if !c.peerAllowedIndexSetContains(567) {
		t.Error("expected true for added index 567")
	}
	if !c.peerAllowedIndexSetContains(789) {
		t.Error("expected true for added index 789")
	}
	if c.peerAllowedIndexSetContains(123) {
		t.Error("expected false for index not in set")
	}
}

func TestAmAllowedIndexSetContains(t *testing.T) {
	c := newTestConn()

	if c.amAllowedIndexSetContains(567) {
		t.Error("expected false for index not in set")
	}
	c.addAmAllowedIndex(567)
	c.addAmAllowedIndex(789)

	if !c.amAllowedIndexSetContains(567) {
		t.Error("expected true for added index 567")
	}
	if !c.amAllowedIndexSetContains(789) {
		t.Error("expected true for added index 789")
	}
	if c.amAllowedIndexSetContains(123) {
		t.Error("expected false for index not in set")
	}
}

func TestHasAllPieces(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(8)

	if c.hasAllPieces() {
		t.Error("expected false before bitfield init")
	}

	c.initBitfield()
	if c.hasAllPieces() {
		t.Error("expected false with empty bitfield")
	}

	c.markSeeder()
	if !c.hasAllPieces() {
		t.Error("expected true after markSeeder")
	}
}

func TestHasAllPiecesPartialLastByte(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(7)
	c.initBitfield()

	c.markSeeder()
	if !c.hasAllPieces() {
		t.Error("expected true for 7 pieces seeder with partial last byte")
	}

	if c.hasPiece(7) {
		t.Error("piece 7 should not exist for 7-piece torrent")
	}
}

func TestHasPiece(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(1024)
	c.initBitfield()

	if c.hasPiece(300) {
		t.Error("expected false before setting bit")
	}
	c.updateBitfield(300, 1)
	if !c.hasPiece(300) {
		t.Error("expected true after setting bit")
	}
	c.updateBitfield(300, 0)
	if c.hasPiece(300) {
		t.Error("expected false after clearing bit")
	}
}

func TestUpdateBitfield(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(1024)
	c.initBitfield()

	c.updateBitfield(0, 1)
	c.updateBitfield(1, 1)
	c.updateBitfield(7, 1)

	if !c.hasPiece(0) {
		t.Error("expected piece 0")
	}
	if !c.hasPiece(1) {
		t.Error("expected piece 1")
	}
	if !c.hasPiece(7) {
		t.Error("expected piece 7")
	}
	if c.hasPiece(2) {
		t.Error("unexpected piece 2")
	}

	c.updateBitfield(1, 0)
	if c.hasPiece(1) {
		t.Error("expected piece 1 to be cleared")
	}
}

func TestUpdateBitfieldOutOfBounds(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(8)
	c.initBitfield()

	c.updateBitfield(-1, 1)
	c.updateBitfield(8, 1)
	if c.hasAllPieces() {
		t.Error("out-of-bounds updates should not set bits")
	}
}

func TestUpdateUploadLength(t *testing.T) {
	c := newTestConn()

	if c.uploadLength() != 0 {
		t.Error("expected 0 upload length")
	}
	c.updateUploadLength(100)
	c.updateUploadLength(200)
	if c.uploadLength() != 300 {
		t.Errorf("expected 300, got %d", c.uploadLength())
	}
}

func TestUpdateDownloadLength(t *testing.T) {
	c := newTestConn()

	if c.downloadLength() != 0 {
		t.Error("expected 0 download length")
	}
	c.updateDownloadLength(100)
	c.updateDownloadLength(200)
	if c.downloadLength() != 300 {
		t.Errorf("expected 300, got %d", c.downloadLength())
	}
}

func TestExtendedMessageEnabled(t *testing.T) {
	c := newTestConn()

	if c.extendedMessageEnabled() {
		t.Error("expected false initially")
	}
	c.setExtendedMessagingEnabled(true)
	if !c.extendedMessageEnabled() {
		t.Error("expected true after setting")
	}
	c.setExtendedMessagingEnabled(false)
	if c.extendedMessageEnabled() {
		t.Error("expected false after unsetting")
	}
}

func TestGetExtensionMessageID(t *testing.T) {
	c := newTestConn()

	const UT_PEX = 1
	const UT_METADATA = 2

	if c.getExtensionMessageID(UT_PEX) != 0 {
		t.Error("expected 0 for unset extension")
	}
	c.addExtension(UT_PEX, 9)
	if c.getExtensionMessageID(UT_PEX) != 9 {
		t.Error("expected 9 for UT_PEX")
	}
	if c.getExtensionMessageID(UT_METADATA) != 0 {
		t.Error("expected 0 for unset UT_METADATA")
	}
}

func TestFastExtensionEnabled(t *testing.T) {
	c := newTestConn()

	if c.fastExtensionEnabled() {
		t.Error("expected false initially")
	}
	c.setFastExtensionEnabled(true)
	if !c.fastExtensionEnabled() {
		t.Error("expected true after setting")
	}
	c.setFastExtensionEnabled(false)
	if c.fastExtensionEnabled() {
		t.Error("expected false after unsetting")
	}
}

func TestSnubbing(t *testing.T) {
	c := newTestConn()

	if c.snubbing() {
		t.Error("expected false initially")
	}
	c.setSnubbing(true)
	if !c.snubbing() {
		t.Error("expected true after setting")
	}
	if !c.chokingRequired() {
		t.Error("snubbing should set chokingRequired to true")
	}
	if c.optUnchoking() {
		t.Error("snubbing should set optUnchoking to false")
	}

	c.setSnubbing(false)
	if c.snubbing() {
		t.Error("expected false after unsetting")
	}
}

func TestChokeStateMachine(t *testing.T) {
	c := newTestConn()

	if !c.amChoking() {
		t.Error("amChoking should be true initially")
	}
	if c.amInterested() {
		t.Error("amInterested should be false initially")
	}
	if !c.peerChoking() {
		t.Error("peerChoking should be true initially")
	}
	if c.peerInterested() {
		t.Error("peerInterested should be false initially")
	}
}

func TestAmChoking(t *testing.T) {
	c := newTestConn()

	c.setAmChoking(false)
	if c.amChoking() {
		t.Error("expected false after setAmChoking(false)")
	}
	c.setAmChoking(true)
	if !c.amChoking() {
		t.Error("expected true after setAmChoking(true)")
	}
}

func TestAmInterested(t *testing.T) {
	c := newTestConn()

	c.setAmInterested(true)
	if !c.amInterested() {
		t.Error("expected true after setAmInterested(true)")
	}
	c.setAmInterested(false)
	if c.amInterested() {
		t.Error("expected false after setAmInterested(false)")
	}
}

func TestPeerChoking(t *testing.T) {
	c := newTestConn()

	c.setPeerChoking(false)
	if c.peerChoking() {
		t.Error("expected false after setPeerChoking(false)")
	}
	c.setPeerChoking(true)
	if !c.peerChoking() {
		t.Error("expected true after setPeerChoking(true)")
	}
}

func TestPeerInterested(t *testing.T) {
	c := newTestConn()

	c.setPeerInterested(true)
	if !c.peerInterested() {
		t.Error("expected true after setPeerInterested(true)")
	}
	c.setPeerInterested(false)
	if c.peerInterested() {
		t.Error("expected false after setPeerInterested(false)")
	}
}

func TestChokingRequired(t *testing.T) {
	c := newTestConn()

	if !c.chokingRequired() {
		t.Error("expected true initially")
	}
	c.setChokingRequired(false)
	if c.chokingRequired() {
		t.Error("expected false after unsetting")
	}
	c.setChokingRequired(true)
	if !c.chokingRequired() {
		t.Error("expected true after setting")
	}
}

func TestOptUnchoking(t *testing.T) {
	c := newTestConn()

	if c.optUnchoking() {
		t.Error("expected false initially")
	}
	c.setOptUnchoking(true)
	if !c.optUnchoking() {
		t.Error("expected true after setting")
	}
	c.setOptUnchoking(false)
	if c.optUnchoking() {
		t.Error("expected false after unsetting")
	}
}

func TestShouldBeChoking(t *testing.T) {
	c := newTestConn()

	if !c.shouldBeChoking() {
		t.Error("expected true when chokingRequired=true, optUnchoking=false")
	}

	c.setChokingRequired(false)
	if c.shouldBeChoking() {
		t.Error("expected false when chokingRequired=false, optUnchoking=false")
	}

	c.setChokingRequired(true)
	c.setOptUnchoking(true)
	if c.shouldBeChoking() {
		t.Error("expected false when chokingRequired=true, optUnchoking=true")
	}
}

func TestCountOutstandingRequest(t *testing.T) {
	c := newTestConn()

	if c.countOutstandingRequest() != 0 {
		t.Error("expected 0 outstanding requests initially")
	}
	c.requestSlots = append(c.requestSlots, requestSlot{piece: 0, offset: 0, length: 16384})
	c.requestSlots = append(c.requestSlots, requestSlot{piece: 1, offset: 0, length: 16384})
	if c.countOutstandingRequest() != 2 {
		t.Errorf("expected 2 outstanding requests, got %d", c.countOutstandingRequest())
	}
}

func TestAllowedFastSetComputation(t *testing.T) {
	infoHash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}

	ip := net.ParseIP("192.168.1.1")
	if ip == nil {
		t.Fatal("invalid ip")
	}

	result := ComputeAllowedFast(ip, 1000, infoHash, 10)

	if len(result) != 10 {
		t.Fatalf("expected 10 allowed fast pieces, got %d", len(result))
	}
	seen := make(map[int]bool)
	for _, idx := range result {
		if idx < 0 || idx >= 1000 {
			t.Errorf("piece index %d out of range [0, 1000)", idx)
		}
		if seen[idx] {
			t.Errorf("duplicate piece index %d", idx)
		}
		seen[idx] = true
	}
}

func TestAllowedFastSmallTorrent(t *testing.T) {
	infoHash := [20]byte{0x01}
	ip := net.ParseIP("10.0.0.1")

	result := ComputeAllowedFast(ip, 5, infoHash, 10)
	if len(result) != 5 {
		t.Fatalf("expected 5 pieces for 5-piece torrent with fastSetSize=10, got %d", len(result))
	}
}

func TestAllowedFastNilIPv4(t *testing.T) {
	ip := net.ParseIP("::1")
	infoHash := [20]byte{0x01}
	result := ComputeAllowedFast(ip, 100, infoHash, 10)
	if result != nil {
		t.Error("expected nil for non-IPv4 address")
	}
}

func TestAllowedFastZeroPieces(t *testing.T) {
	ip := net.ParseIP("1.2.3.4")
	infoHash := [20]byte{0x01}
	result := ComputeAllowedFast(ip, 0, infoHash, 10)
	if result != nil {
		t.Error("expected nil for zero pieces")
	}
}

func TestCountSeeder(t *testing.T) {
	peers := make([]*Conn, 5)
	for i := range peers {
		peers[i] = newTestConn()
		peers[i].setNumPieces(8)
		peers[i].initBitfield()
	}

	peers[1].markSeeder()
	peers[3].markSeeder()
	peers[4].markSeeder()

	if countSeeder(peers) != 3 {
		t.Errorf("expected 3 seeders, got %d", countSeeder(peers))
	}
}

func TestCountSeederEmpty(t *testing.T) {
	if countSeeder(nil) != 0 {
		t.Error("expected 0 seeders for nil list")
	}
}

func TestReserveBufferDataPreservation(t *testing.T) {
	buf := make([]byte, maxBufferCapacity)
	var readBuf []byte

	preset := []byte("foo")
	copy(buf, preset)
	readBuf = make([]byte, len(preset), maxBufferCapacity)
	copy(readBuf, preset)

	if len(readBuf) != len(preset) {
		t.Fatalf("expected %d bytes, got %d", len(preset), len(readBuf))
	}

	readBuf = make([]byte, len(readBuf), maxBufferCapacity*2)
	copy(readBuf[:len(preset)], preset)

	if string(readBuf[:len(preset)]) != "foo" {
		t.Fatal("data not preserved during buffer resize")
	}
	if len(readBuf) != len(preset) {
		t.Fatalf("expected length %d after resize, got %d", len(preset), len(readBuf))
	}
}

func TestMessageEncodeDecodeAllowedFast(t *testing.T) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(42))
	msg := NewMessage(MsgAllowedFast, buf)
	data := msg.Encode()

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != MsgAllowedFast {
		t.Fatalf("expected MsgAllowedFast, got %d", decoded.ID)
	}
	piece := int(binary.BigEndian.Uint32(decoded.Payload))
	if piece != 42 {
		t.Fatalf("expected piece 42, got %d", piece)
	}
}

func TestMessageEncodeDecodeSuggest(t *testing.T) {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(99))
	msg := NewMessage(MsgSuggest, buf)
	data := msg.Encode()

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != MsgSuggest {
		t.Fatalf("expected MsgSuggest, got %d", decoded.ID)
	}
	piece := int(binary.BigEndian.Uint32(decoded.Payload))
	if piece != 99 {
		t.Fatalf("expected piece 99, got %d", piece)
	}
}

func TestMessageEncodeDecodeReject(t *testing.T) {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint32(buf[0:4], uint32(5))
	binary.BigEndian.PutUint32(buf[4:8], uint32(16384))
	binary.BigEndian.PutUint32(buf[8:12], uint32(16384))
	msg := NewMessage(MsgReject, buf)
	data := msg.Encode()

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != MsgReject {
		t.Fatalf("expected MsgReject, got %d", decoded.ID)
	}
	piece := int(binary.BigEndian.Uint32(decoded.Payload[0:4]))
	offset := int(binary.BigEndian.Uint32(decoded.Payload[4:8]))
	length := int(binary.BigEndian.Uint32(decoded.Payload[8:12]))
	if piece != 5 || offset != 16384 || length != 16384 {
		t.Fatalf("mismatch: piece=%d offset=%d length=%d", piece, offset, length)
	}
}

func TestMessageEncodeDecodeHaveAll(t *testing.T) {
	msg := NewMessage(MsgHaveAll, nil)
	data := msg.Encode()

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != MsgHaveAll {
		t.Fatalf("expected MsgHaveAll, got %d", decoded.ID)
	}
	if len(decoded.Payload) != 0 {
		t.Fatalf("expected empty payload, got %d bytes", len(decoded.Payload))
	}
}

func TestMessageEncodeDecodeHaveNone(t *testing.T) {
	msg := NewMessage(MsgHaveNone, nil)
	data := msg.Encode()

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != MsgHaveNone {
		t.Fatalf("expected MsgHaveNone, got %d", decoded.ID)
	}
	if len(decoded.Payload) != 0 {
		t.Fatalf("expected empty payload, got %d bytes", len(decoded.Payload))
	}
}

func TestDecodeMessageRejectsInvalidFixedPayloadLength(t *testing.T) {
	_, err := DecodeMessage(NewMessage(MsgHave, []byte{0, 0, 0, 1, 2}).Encode())
	if err == nil {
		t.Fatal("expected invalid have payload length")
	}

	_, err = DecodeMessage(NewMessage(MsgChoke, []byte{1}).Encode())
	if err == nil {
		t.Fatal("expected invalid choke payload length")
	}
}

func TestRequestRejectsInvalidBlockBounds(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(2)
	c.cfg.PieceLength = 16 * 1024

	if err := c.Request(2, 0, 1); err == nil {
		t.Fatal("expected out-of-range piece error")
	}
	if err := c.Request(0, 0, 0); err == nil {
		t.Fatal("expected zero-length request error")
	}
	if err := c.Request(0, 16*1024, 1); err == nil {
		t.Fatal("expected offset out-of-range error")
	}
	if err := c.Request(0, 0, maxBlockLength+1); err == nil {
		t.Fatal("expected oversized block error")
	}
}

func TestHandleBitfield(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(16)

	bf := []byte{0x80, 0x00}
	if err := c.handleBitfield(bf); err != nil {
		t.Fatal(err)
	}

	if len(c.bitfield) != 2 {
		t.Fatalf("expected bitfield length 2, got %d", len(c.bitfield))
	}
	if !c.hasPiece(0) {
		t.Error("expected piece 0 from bitfield")
	}
	if c.hasPiece(1) {
		t.Error("unexpected piece 1 from bitfield")
	}
}

func TestHandleBitfieldTruncated(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(16)

	bf := []byte{0x80}
	if err := c.handleBitfield(bf); err == nil {
		t.Fatal("expected error for truncated bitfield")
	}

	if c.bitfield != nil {
		t.Fatal("invalid bitfield must not be installed")
	}
}

func TestHandleBitfieldZeroPieces(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(0)
	bf := []byte{0x80}
	if err := c.handleBitfield(bf); err == nil {
		t.Fatal("expected error for non-empty zero-piece bitfield")
	}

	if c.bitfield != nil {
		t.Error("expected nil bitfield for zero pieces")
	}
}

func TestHandleBitfieldEmpty(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(8)
	if err := c.handleBitfield(nil); err == nil {
		t.Fatal("expected error for empty bitfield when pieces are known")
	}

	if c.bitfield != nil {
		t.Error("expected nil bitfield for empty input")
	}
}

func TestMarkSeederClearsCorrectBits(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(10)
	c.initBitfield()

	c.markSeeder()
	for i := 0; i < 10; i++ {
		if !c.hasPiece(i) {
			t.Errorf("expected piece %d to be set after markSeeder", i)
		}
	}
	if c.hasPiece(10) {
		t.Error("piece 10 should not exist for 10-piece torrent")
	}

	lastByte := c.bitfield[1]
	if lastByte != 0xc0 {
		t.Errorf("expected last byte 0xc0 (only high 2 bits set for 10 pieces), got %08b", lastByte)
	}
}

func TestBitfieldCacheConsistency(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(8)
	c.initBitfield()

	c.markSeeder()
	if !c.hasAllPieces() {
		t.Error("expected hasAllPieces true")
	}

	c.updateBitfield(0, 0)
	if c.hasAllPieces() {
		t.Error("expected hasAllPieces false after clearing one bit")
	}
}

func TestChokedActionPreservesAllowedFast(t *testing.T) {
	c := newTestConn()

	c.SetAllowedFast(4)

	c.requestSlots = []requestSlot{
		{piece: 1, offset: 0, length: 16384},
		{piece: 4, offset: 0, length: 16384},
		{piece: 3, offset: 16384, length: 16384},
	}

	c.doChokedAction()

	if len(c.requestSlots) != 1 {
		t.Fatalf("expected 1 slot preserved (allowed fast), got %d", len(c.requestSlots))
	}
	if c.requestSlots[0].piece != 4 {
		t.Fatalf("expected piece 4 to be preserved, got %d", c.requestSlots[0].piece)
	}
}

func TestMarshalBitfield(t *testing.T) {
	bf := []byte{0x80, 0x00}
	data := MarshalBitfield(bf)

	msg, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != MsgBitfield {
		t.Fatalf("expected MsgBitfield, got %d", msg.ID)
	}
	if len(msg.Payload) != 2 {
		t.Fatalf("expected payload length 2, got %d", len(msg.Payload))
	}
	if msg.Payload[0] != 0x80 || msg.Payload[1] != 0x00 {
		t.Fatalf("payload mismatch")
	}
}

func TestMarshalPort(t *testing.T) {
	data := MarshalPort(6881)

	msg, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != MsgPort {
		t.Fatalf("expected MsgPort, got %d", msg.ID)
	}
	port := binary.BigEndian.Uint16(msg.Payload)
	if port != 6881 {
		t.Fatalf("expected port 6881, got %d", port)
	}
}

func TestSnapshotInitialState(t *testing.T) {
	c := newTestConn()
	snap := c.Snapshot()

	if !snap.Choked {
		t.Error("expected Choked=true initially")
	}
	if !snap.PeerChoking {
		t.Error("expected PeerChoking=true initially")
	}
	if snap.Interested {
		t.Error("expected Interested=false initially")
	}
	if snap.PeerInterest {
		t.Error("expected PeerInterest=false initially")
	}
	if snap.Downloaded != 0 {
		t.Error("expected Downloaded=0 initially")
	}
	if snap.Uploaded != 0 {
		t.Error("expected Uploaded=0 initially")
	}
}

func TestRequestAddsSlot(t *testing.T) {
	c := newTestConn()

	if c.countOutstandingRequest() != 0 {
		t.Error("expected 0 requests initially")
	}

	c.requestSlots = append(c.requestSlots, requestSlot{piece: 0, offset: 0, length: 16384})
	if c.countOutstandingRequest() != 1 {
		t.Errorf("expected 1 request, got %d", c.countOutstandingRequest())
	}
}

func TestCancelRemovesSlot(t *testing.T) {
	c := newTestConn()
	c.requestSlots = []requestSlot{
		{piece: 0, offset: 0, length: 16384},
		{piece: 1, offset: 0, length: 16384},
		{piece: 0, offset: 16384, length: 16384},
	}

	c.removeRequestSlot(0, 0, 16384)

	if c.countOutstandingRequest() != 2 {
		t.Errorf("expected 2 remaining requests, got %d", c.countOutstandingRequest())
	}
	if c.requestSlots[0].piece != 1 {
		t.Error("expected slot shift")
	}
}

func TestNoRemoveNonExistentSlot(t *testing.T) {
	c := newTestConn()
	c.requestSlots = []requestSlot{
		{piece: 0, offset: 0, length: 16384},
	}

	c.removeRequestSlot(999, 0, 16384)

	if c.countOutstandingRequest() != 1 {
		t.Error("non-existent slot should not affect count")
	}
}

func TestMessageEncodeDecodeLength(t *testing.T) {
	for _, id := range []MessageID{MsgChoke, MsgUnchoke, MsgInterested, MsgNotInterested,
		MsgHaveAll, MsgHaveNone} {
		msg := NewMessage(id, nil)
		data := msg.Encode()
		if len(data) != 5 {
			t.Errorf("message id=%d: expected length 5, got %d", id, len(data))
		}
		length := binary.BigEndian.Uint32(data[:4])
		if length != 1 {
			t.Errorf("message id=%d: expected length prefix 1, got %d", id, length)
		}
	}
}

func TestSetAllowedFastNilMap(t *testing.T) {
	c := newTestConn()
	if c.peerAllowedIndexSetContains(0) {
		t.Error("nil map should return false")
	}
	c.SetAllowedFast(0)
	if !c.peerAllowedIndexSetContains(0) {
		t.Error("should return true after SetAllowedFast")
	}
}

func TestGetExtensionMessageIDNilMap(t *testing.T) {
	c := newTestConn()
	if c.getExtensionMessageID(1) != 0 {
		t.Error("nil extension map should return 0")
	}
}

func TestAmAllowedNilMap(t *testing.T) {
	c := newTestConn()
	if c.amAllowedIndexSetContains(0) {
		t.Error("nil map should return false")
	}
}

func TestDecodeMessageErrors(t *testing.T) {
	_, err := DecodeMessage(nil)
	if err == nil {
		t.Error("expected error for nil data")
	}

	_, err = DecodeMessage([]byte{0x00})
	if err == nil {
		t.Error("expected error for too-short data")
	}

	_, err = DecodeMessage([]byte{0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Error("expected error for keep-alive message (no ID)")
	}

	_, err = DecodeMessage([]byte{0x00, 0x00, 0x00, 0x05, 0x00})
	if err == nil {
		t.Error("expected error for incomplete message")
	}

	tooLarge := make([]byte, 4)
	binary.BigEndian.PutUint32(tooLarge, uint32(maxBufferCapacity+1))
	_, err = DecodeMessage(tooLarge)
	if err == nil {
		t.Error("expected error for too-large message")
	}
}

func TestRemoveRequestSlotPrecise(t *testing.T) {
	c := newTestConn()
	c.requestSlots = []requestSlot{
		{piece: 0, offset: 0, length: 16384},
		{piece: 0, offset: 0, length: 32768},
		{piece: 0, offset: 16384, length: 16384},
	}

	c.removeRequestSlot(0, 0, 32768)

	if c.countOutstandingRequest() != 2 {
		t.Errorf("expected 2 remaining, got %d", c.countOutstandingRequest())
	}
	if c.requestSlots[0].length != 16384 {
		t.Error("wrong slot removed")
	}
}

func TestBitfieldEdgePieces(t *testing.T) {
	c := newTestConn()
	c.setNumPieces(1024)
	c.initBitfield()

	c.updateBitfield(0, 1)
	c.updateBitfield(1023, 1)

	if !c.hasPiece(0) {
		t.Error("expected piece 0")
	}
	if !c.hasPiece(1023) {
		t.Error("expected piece 1023")
	}
}

func TestMessageEncodePreservePayload(t *testing.T) {
	payload := []byte("test payload data")
	msg := NewMessage(MsgExtended, payload)
	data := msg.Encode()

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.Payload, payload) {
		t.Fatal("payload not preserved")
	}
}

func TestRequestSlotDoesNotAffectSend(t *testing.T) {
	c := newTestConn()
	c.requestSlots = append(c.requestSlots, requestSlot{piece: 0, offset: 0, length: 16384})
	c.requestSlots = append(c.requestSlots, requestSlot{piece: 1, offset: 0, length: 16384})

	if c.countOutstandingRequest() != 2 {
		t.Errorf("expected 2, got %d", c.countOutstandingRequest())
	}
}
