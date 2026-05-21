package engine

import (
	"context"
	"crypto/sha1"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/disk"
	"github.com/smartass08/aria2go/internal/mse"
	btpeer "github.com/smartass08/aria2go/internal/protocol/bittorrent/peer"
	"github.com/smartass08/aria2go/internal/torrent"
	"github.com/smartass08/aria2go/internal/tracker"
)

const (
	btBlockSize          = 16 * 1024
	btMaxPendingRequests = 4
	btChokeInterval      = 10 * time.Second
	btUnchokeSlots       = 3
	btEndgameThreshold   = 10
	btPeerDialTimeout    = 15 * time.Second
	btPeerConnectLimit   = 55
)

// btPieceSource adapts disk.Adaptor to btpeer.PieceSource.
type btPieceSource struct {
	adaptor   disk.Adaptor
	numPieces int
}

func (p *btPieceSource) NumPieces() int   { return p.numPieces }
func (p *btPieceSource) Have(i int) bool  { return p.adaptor.Have(i) }
func (p *btPieceSource) Bitfield() []byte { return p.adaptor.Bitfield() }

// peerState tracks a connected BT peer and its piece availability.
type peerState struct {
	conn     *btpeer.Conn
	addr     string
	bitfield []byte
	pieces   int

	dlBytes        int64
	lastDLSnapshot int64

	outstanding int
}

func (p *peerState) hasPiece(idx int) bool {
	if idx < 0 || idx >= p.pieces || p.bitfield == nil {
		return false
	}
	byteIdx := idx / 8
	bitIdx := 7 - (idx % 8)
	if byteIdx >= len(p.bitfield) {
		return false
	}
	return p.bitfield[byteIdx]&(1<<bitIdx) != 0
}

func (p *peerState) hasAllPieces() bool {
	if p.bitfield == nil || p.pieces == 0 {
		return false
	}
	nbytes := (p.pieces + 7) / 8
	for i := 0; i < nbytes-1; i++ {
		if p.bitfield[i] != 0xff {
			return false
		}
	}
	if nbytes > 0 {
		lastBits := p.pieces % 8
		mask := byte(0xff)
		if lastBits != 0 {
			mask = byte(0xff << (8 - lastBits))
		}
		if p.bitfield[nbytes-1] != mask {
			return false
		}
	}
	return true
}

func (p *peerState) setPiece(idx int) {
	if idx < 0 || idx >= p.pieces || p.bitfield == nil {
		return
	}
	byteIdx := idx / 8
	bitIdx := 7 - (idx % 8)
	p.bitfield[byteIdx] |= 1 << bitIdx
}

func (p *peerState) dlRate() int64 {
	snap := p.dlBytes
	delta := snap - p.lastDLSnapshot
	p.lastDLSnapshot = snap
	return delta
}

// peerMsg pairs a received peer message with its source peer.
type peerMsg struct {
	src *peerState
	msg btpeer.Message
}

// btSwarm manages peer connections and piece download state for a BT download.
type btSwarm struct {
	mu    sync.Mutex
	peers []*peerState

	adaptor     disk.Adaptor
	meta        *torrent.MetaInfo
	numPieces   int
	pieceLen    int64
	pieceHashes []byte
}

func (s *btSwarm) addPeer(p *peerState) {
	s.mu.Lock()
	s.peers = append(s.peers, p)
	s.mu.Unlock()
}

func (s *btSwarm) peerCount() int {
	s.mu.Lock()
	n := len(s.peers)
	s.mu.Unlock()
	return n
}

func (s *btSwarm) complete() bool {
	for i := 0; i < s.numPieces; i++ {
		if !s.adaptor.Have(i) {
			return false
		}
	}
	return true
}

func (s *btSwarm) missingCount() int {
	n := 0
	for i := 0; i < s.numPieces; i++ {
		if !s.adaptor.Have(i) {
			n++
		}
	}
	return n
}

func (s *btSwarm) endgameMode() bool {
	missing := s.missingCount()
	return missing > 0 && missing <= btEndgameThreshold
}

// choosePiece selects the rarest piece this peer has that we're missing.
// Matches aria2's rarest-first piece selection in DefaultPieceStorage.
func (s *btSwarm) choosePiece(p *peerState) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.choosePieceLocked(p)
}

func (s *btSwarm) choosePieceLocked(p *peerState) (int, bool) {
	avail := make(map[int]int, s.numPieces)
	for _, peer := range s.peers {
		if peer == p {
			continue
		}
		for i := 0; i < s.numPieces; i++ {
			if !s.adaptor.Have(i) && peer.hasPiece(i) {
				avail[i]++
			}
		}
	}

	best := -1
	bestAvail := -1
	for i := 0; i < s.numPieces; i++ {
		if s.adaptor.Have(i) || !p.hasPiece(i) {
			continue
		}
		a := avail[i]
		if best == -1 || a < bestAvail {
			best = i
			bestAvail = a
		}
	}
	return best, best >= 0
}

func (s *btSwarm) closeAll() {
	s.mu.Lock()
	for _, p := range s.peers {
		p.conn.Close()
	}
	s.mu.Unlock()
}

func (s *btSwarm) verifyPiece(idx int) error {
	blen := s.pieceLen
	if idx == s.numPieces-1 {
		rem := s.meta.TotalSize() % s.pieceLen
		if rem > 0 {
			blen = rem
		}
	}
	offset := int64(idx) * s.pieceLen
	buf := make([]byte, blen)
	n, err := s.adaptor.ReadAt(buf, offset)
	if err != nil && n == 0 {
		return fmt.Errorf("read piece %d for verify: %w", idx, err)
	}
	expected := s.pieceHashes[idx*20 : (idx+1)*20]
	actual := sha1.Sum(buf[:n])
	if actual != *(*[20]byte)(expected) {
		return fmt.Errorf("piece %d hash mismatch", idx)
	}
	s.adaptor.MarkPiece(idx, true)
	return nil
}

// doChoke implements aria2's choking algorithm (PeerChokeCommand.cc).
// Unchokes the top N peers by download rate, plus one optimistic unchoke.
// Chokes all others.
func (s *btSwarm) doChoke() {
	s.mu.Lock()
	defer s.mu.Unlock()

	type rated struct {
		p    *peerState
		rate int64
	}
	rates := make([]rated, 0, len(s.peers))
	for _, p := range s.peers {
		rates = append(rates, rated{p, p.dlRate()})
	}
	sort.Slice(rates, func(i, j int) bool {
		return rates[i].rate > rates[j].rate
	})

	unchoked := 0
	for i := range rates {
		if unchoked < btUnchokeSlots && !rates[i].p.hasAllPieces() {
			rates[i].p.conn.Unchoke()
			unchoked++
		} else {
			rates[i].p.conn.Choke()
		}
	}

	if len(rates) > unchoked {
		pool := rates[unchoked:]
		if len(pool) > 0 {
			opt := pool[rand.IntN(len(pool))]
			opt.p.conn.Unchoke()
		}
	}
}

func (s *btSwarm) handleMsg(msg peerMsg) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch msg.msg.ID {
	case btpeer.MsgBitfield:
		bf := msg.msg.Payload
		nbytes := (s.numPieces + 7) / 8
		msg.src.bitfield = make([]byte, nbytes)
		copy(msg.src.bitfield, bf)
		msg.src.pieces = s.numPieces
	case btpeer.MsgHave:
		idx, err := btpeer.UnmarshalHave(msg.msg)
		if err == nil {
			msg.src.setPiece(idx)
		}
	case btpeer.MsgHaveAll:
		if msg.src.bitfield == nil {
			nbytes := (s.numPieces + 7) / 8
			msg.src.bitfield = make([]byte, nbytes)
		}
		for i := range msg.src.bitfield {
			msg.src.bitfield[i] = 0xff
		}
		if s.numPieces > 0 && len(msg.src.bitfield) > 0 {
			lastBits := s.numPieces % 8
			if lastBits != 0 {
				msg.src.bitfield[len(msg.src.bitfield)-1] = byte(0xff << (8 - lastBits))
			}
		}
		msg.src.pieces = s.numPieces
	case btpeer.MsgHaveNone:
		if msg.src.bitfield != nil {
			for i := range msg.src.bitfield {
				msg.src.bitfield[i] = 0
			}
		}
	}
}

// runBTDownload executes a BitTorrent download for the given requestGroup.
func (e *Engine) runBTDownload(ctx context.Context, rg *requestGroup, torrentData []byte) error {
	meta, err := torrent.Load(torrentData)
	if err != nil {
		e.log.Error("BT torrent parse failed", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitTorrentParseError
		rg.errMsg = err.Error()
		return err
	}

	infoHash, err := meta.InfoHash()
	if err != nil {
		e.log.Error("BT infohash compute failed", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitTorrentParseError
		rg.errMsg = err.Error()
		return err
	}

	dir := rg.opts.Dir
	if dir == "" {
		dir = "."
	}
	basePath := filepath.Join(dir, meta.Info.Name)

	outPath := rg.filePath
	if outPath == "" {
		if len(meta.Info.Files) > 0 {
			outPath = basePath
		} else {
			outPath = filepath.Join(dir, meta.Info.Name)
		}
	}

	// Resolve collision and auto-renaming for BT download under queuesMu lock to prevent race conditions
	e.queuesMu.Lock()
	targetPath := outPath
	if len(meta.Info.Files) > 0 {
		targetPath = basePath
	}

	originalPath := targetPath
	suffix := 1
	for {
		collision := false
		if e.isSameFileBeingDownloadedLocked(targetPath, rg.gid) {
			collision = true
		} else if e.controlFileAllowsResume(targetPath, rg.opts, meta.TotalSize(), meta.Info.PieceLength, infoHash[:]) {
			collision = false
		} else {
			if _, err := os.Stat(targetPath); err == nil && !rg.opts.Continue && !rg.opts.AllowOverwrite {
				collision = true
			}
		}

		if collision {
			if !rg.opts.AutoFileRenaming {
				e.log.Error("BT collision detected but auto-file-renaming is disabled", "gid", rg.gid, "path", targetPath)
				e.queuesMu.Unlock()
				if _, ok := e.groups.getLocked(rg.gid); ok {
					rg.errCode = core.ExitFileAlreadyExists
					rg.errMsg = fmt.Sprintf("file/directory already exists: %s", targetPath)
					e.groups.unlock(rg.gid)
				}
				return fmt.Errorf("file/directory already exists: %s", targetPath)
			}
			targetPath = tryAutoFileRenamingWithSuffix(originalPath, suffix)
			suffix++
		} else {
			break
		}
	}

	// Update the appropriate path
	if len(meta.Info.Files) > 0 {
		basePath = targetPath
		outPath = targetPath
	} else {
		outPath = targetPath
	}

	// Update the request group's filePath and fileName under lock
	if _, ok := e.groups.getLocked(rg.gid); ok {
		rg.filePath = outPath
		rg.fileName = filepath.Base(outPath)
		e.groups.unlock(rg.gid)
	}
	e.queuesMu.Unlock()

	e.initControlInfo(rg, outPath, meta.TotalSize(), meta.Info.PieceLength, infoHash[:])

	var alloc disk.Allocator = disk.AllocatorNone{}
	if rg.opts.FileAllocation == "trunc" {
		alloc = disk.AllocatorTrunc{}
	}

	var adaptor disk.Adaptor
	var wantedPieces []bool
	if len(meta.Info.Files) > 0 {
		files, pieceFilter, fileErr := torrentFilesToDiskEntriesWithOptions(filepath.Base(basePath), meta.Info.Files, rg.opts, meta.Info.PieceLength, meta.NumPieces())
		if fileErr != nil {
			e.log.Error("BT file option setup failed", "gid", rg.gid, "error", fileErr)
			rg.errCode = core.ExitBadOption
			rg.errMsg = fileErr.Error()
			return fileErr
		}
		wantedPieces = pieceFilter
		mf, aErr := disk.NewMultiFile(dir, files, meta.Info.PieceLength, alloc)
		if aErr != nil {
			e.log.Error("BT disk setup failed", "gid", rg.gid, "error", aErr)
			rg.errCode = core.ExitFileCreateError
			rg.errMsg = aErr.Error()
			return aErr
		}
		adaptor = mf
	} else {
		if rg.opts.AllowOverwrite && !rg.opts.Continue && !e.controlLoaded(rg) {
			if truncErr := os.Truncate(outPath, 0); truncErr != nil && !os.IsNotExist(truncErr) {
				e.log.Error("BT disk truncate failed", "gid", rg.gid, "error", truncErr)
				rg.errCode = core.ExitFileIOError
				rg.errMsg = truncErr.Error()
				return truncErr
			}
		}
		sf, aErr := disk.NewSingleFile(outPath, meta.Info.Length, alloc)
		if aErr != nil {
			e.log.Error("BT disk setup failed", "gid", rg.gid, "error", aErr)
			rg.errCode = core.ExitFileCreateError
			rg.errMsg = aErr.Error()
			return aErr
		}
		adaptor = sf
	}

	if err := adaptor.OpenForWrite(); err != nil {
		e.log.Error("BT disk open failed", "gid", rg.gid, "error", err)
		rg.errCode = core.ExitFileCreateError
		rg.errMsg = err.Error()
		adaptor.Close()
		return err
	}
	e.setControlAdaptor(rg, adaptor)
	defer adaptor.Close()
	defer e.syncControlAdaptor(rg)

	numPieces := meta.NumPieces()
	adaptor.SetPieceCount(numPieces)
	e.applyControlBitfield(rg, adaptor)
	for i, wanted := range wantedPieces {
		if !wanted {
			adaptor.MarkPiece(i, true)
		}
	}

	e.log.Info("BT download started",
		"gid", rg.gid,
		"name", meta.Info.Name,
		"size", meta.Info.Length,
		"pieces", numPieces,
		"infohash", fmt.Sprintf("%x", infoHash[:]),
	)

	if e.lpdListener != nil {
		if err := e.lpdListener.Announce([][20]byte{infoHash}, uint16(e.btSession.Port())); err != nil {
			e.log.Error("lpd announce failed", "gid", rg.gid, "error", err)
		}
	}

	peerAddrs := e.collectPeers(ctx, meta, rg.gid)

	peerCfg := e.btPeerConfig(meta, adaptor)

	swarm := &btSwarm{
		adaptor:     adaptor,
		meta:        meta,
		numPieces:   numPieces,
		pieceLen:    meta.Info.PieceLength,
		pieceHashes: meta.Info.Pieces,
	}

	e.connectPeers(ctx, swarm, peerAddrs, peerCfg)

	if swarm.peerCount() == 0 {
		e.log.Error("BT no peers available", "gid", rg.gid)
		rg.errCode = core.ExitResourceNotFound
		rg.errMsg = "no peers available"
		return fmt.Errorf("bt: no peers available")
	}

	swarmCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := swarm.downloadLoop(swarmCtx, rg, e); err != nil {
		return err
	}

	if !rg.opts.BTSeedUnverified {
		e.log.Info("BT verifying pieces", "gid", rg.gid)
		if err := verifySelectedPieces(ctx, adaptor, meta, wantedPieces); err != nil {
			e.log.Error("BT verification failed", "gid", rg.gid, "error", err)
			rg.errCode = core.ExitFileIOError
			rg.errMsg = fmt.Sprintf("piece verification failed: %v", err)
			return err
		}
	}

	rg.seeder = true
	rg.errCode = core.ExitSuccess
	e.log.Info("BT download complete, entering seeding", "gid", rg.gid)
	e.emit(core.EvBTComplete, rg.gid)

	numFiles := 1
	if len(meta.Info.Files) > 0 {
		numFiles = len(meta.Info.Files)
	}
	e.runHookByName(rg, numFiles, "on-bt-download-complete")

	return nil
}

func (s *btSwarm) downloadLoop(ctx context.Context, rg *requestGroup, e *Engine) error {
	msgCh := make(chan peerMsg, 64)
	var wg sync.WaitGroup

	s.mu.Lock()
	for _, p := range s.peers {
		wg.Add(1)
		go s.readPeer(ctx, &wg, p, msgCh)
	}
	s.mu.Unlock()

	chokeTicker := time.NewTicker(btChokeInterval)
	defer chokeTicker.Stop()

	reqTicker := time.NewTicker(100 * time.Millisecond)
	defer reqTicker.Stop()

	bf := s.adaptor.Bitfield()
	s.mu.Lock()
	for _, p := range s.peers {
		p.conn.Bitfield(bf)
	}
	s.mu.Unlock()

	for !s.complete() {
		select {
		case <-ctx.Done():
			wg.Wait()
			rg.errCode = core.ExitRemoved
			rg.errMsg = "download cancelled"
			return ctx.Err()

		case msg, ok := <-msgCh:
			if !ok {
				continue
			}
			s.handleMsg(msg)

			if msg.msg.ID == btpeer.MsgPiece {
				s.handlePiece(ctx, msg, rg, e)
			} else if msg.msg.ID == btpeer.MsgRequest {
				s.handleRequest(ctx, msg, rg, e)
			}

		case <-chokeTicker.C:
			s.doChoke()

		case <-reqTicker.C:
			s.requestPieces()
		}
	}

	s.closeAll()
	wg.Wait()
	return nil
}

func (s *btSwarm) handlePiece(ctx context.Context, msg peerMsg, rg *requestGroup, e *Engine) {
	pieceIdx, offset, data, err := btpeer.UnmarshalPiece(msg.msg)
	if err != nil {
		return
	}

	if len(data) > 0 {
		if err := e.rateGlobal.Wait(ctx, len(data)); err != nil {
			return
		}
		if rg.downloadLimit != nil {
			if err := rg.downloadLimit.Wait(ctx, len(data)); err != nil {
				return
			}
		}
	}

	absOffset := int64(pieceIdx)*s.pieceLen + int64(offset)
	n, werr := s.adaptor.WriteAt(data, absOffset)
	if werr != nil {
		return
	}

	s.mu.Lock()
	msg.src.dlBytes += int64(n)
	msg.src.outstanding--
	s.mu.Unlock()

	atomic.AddInt64(&rg.bytesDownloaded, int64(n))

	pieceBlkLen := int64(s.pieceLen)
	if pieceIdx == s.numPieces-1 {
		rem := s.meta.TotalSize() % s.pieceLen
		if rem > 0 {
			pieceBlkLen = rem
		}
	}

	if absOffset+int64(n) >= int64(pieceIdx)*s.pieceLen+pieceBlkLen {
		if verr := s.verifyPiece(pieceIdx); verr != nil {
			s.adaptor.MarkPiece(pieceIdx, false)
			e.markControlPiece(rg, pieceIdx, false)
			return
		}
		e.markControlPiece(rg, pieceIdx, true)

		s.mu.Lock()
		for _, p := range s.peers {
			p.conn.Have(pieceIdx)
		}
		s.mu.Unlock()
		return
	}

	// Request next block in this piece
	nextOffset := offset + len(data)
	nextLen := btBlockSize
	if nextOffset+nextLen > int(pieceBlkLen) {
		nextLen = int(pieceBlkLen) - nextOffset
	}
	if nextLen > 0 {
		msg.src.conn.Request(pieceIdx, nextOffset, nextLen)
		s.mu.Lock()
		msg.src.outstanding++
		s.mu.Unlock()
	}
}

func (s *btSwarm) handleRequest(ctx context.Context, msg peerMsg, rg *requestGroup, e *Engine) {
	pieceIdx, offset, length, err := btpeer.UnmarshalRequest(msg.msg)
	if err != nil {
		return
	}

	if !s.adaptor.Have(pieceIdx) {
		return
	}

	blen := s.pieceLen
	if pieceIdx == s.numPieces-1 {
		rem := s.meta.TotalSize() % s.pieceLen
		if rem > 0 {
			blen = rem
		}
	}

	if offset+length > int(blen) {
		length = int(blen) - offset
	}
	if length <= 0 {
		return
	}

	buf := make([]byte, length)
	absOffset := int64(pieceIdx)*s.pieceLen + int64(offset)
	n, rerr := s.adaptor.ReadAt(buf, absOffset)
	if rerr != nil || n < length {
		return
	}

	if err := e.rateGlobalUp.Wait(ctx, n); err != nil {
		return
	}

	msg.src.conn.Piece(pieceIdx, offset, buf[:n])
}

func (s *btSwarm) requestPieces() {
	s.mu.Lock()
	defer s.mu.Unlock()

	endgame := s.endgameMode()
	for _, p := range s.peers {
		if p.outstanding >= btMaxPendingRequests {
			continue
		}

		pieceIdx, ok := s.choosePieceLocked(p)
		if !ok {
			continue
		}

		blen := int(s.pieceLen)
		if pieceIdx == s.numPieces-1 {
			rem := s.meta.TotalSize() % s.pieceLen
			if rem > 0 {
				blen = int(rem)
			}
		}

		blockLen := btBlockSize
		if blockLen > blen {
			blockLen = blen
		}

		p.conn.Request(pieceIdx, 0, blockLen)
		p.outstanding++

		// Endgame: request same piece from all other peers that have it
		if endgame {
			for _, q := range s.peers {
				if q == p || q.outstanding >= btMaxPendingRequests*2 {
					continue
				}
				if q.hasPiece(pieceIdx) {
					q.conn.Request(pieceIdx, 0, blockLen)
					q.outstanding++
				}
			}
		}
	}
}

func (s *btSwarm) readPeer(ctx context.Context, wg *sync.WaitGroup, p *peerState, msgCh chan peerMsg) {
	defer wg.Done()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- p.conn.Run(ctx)
	}()
	ch := p.conn.Messages()
	for {
		select {
		case <-ctx.Done():
			p.conn.Close()
			return
		case <-runErrCh:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			select {
			case msgCh <- peerMsg{src: p, msg: msg}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (e *Engine) btPeerConfig(meta *torrent.MetaInfo, adaptor disk.Adaptor) btpeer.Config {
	infoHash, _ := meta.InfoHash()
	numPieces := meta.NumPieces()

	peerID := e.btSession.PeerID()

	reserved := btpeer.MakeReserved(false, false, true)

	src := &btPieceSource{
		adaptor:   adaptor,
		numPieces: numPieces,
	}

	return btpeer.Config{
		InfoHash:    infoHash,
		LocalPeerID: peerID,
		Reserved:    reserved,
		Pieces:      src,
		PieceLength: meta.Info.PieceLength,
		Encrypt:     btMSEEncryption(e.cfg),
		Timeout:     btTimeout(e.cfg),
	}
}

func btMSEEncryption(opts *config.Options) mse.Mode {
	if opts.BTRequireCrypto || opts.BTForceEncryption || strings.EqualFold(opts.BTMinCryptoLevel, "arc4") {
		return mse.Require
	}
	return mse.Allow
}

func btTimeout(opts *config.Options) time.Duration {
	if opts.BTTimeout != "" {
		if n, err := strconv.Atoi(opts.BTTimeout); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 180 * time.Second
}

func (e *Engine) collectPeers(ctx context.Context, meta *torrent.MetaInfo, gid core.GID) []string {
	infohash, err := meta.InfoHash()
	if err != nil {
		return nil
	}

	peerID := e.btSession.PeerID()

	req := tracker.AnnounceRequest{
		InfoHash: infohash,
		PeerID:   peerID,
		Port:     uint16(e.btSession.Port()),
		Event:    "started",
		NumWant:  50,
	}

	var addrs []string
	urls := announceURLs(meta)
	for _, u := range urls {
		resp, tErr := tracker.AnnounceHTTP(ctx, u, req, e.httpDriver)
		if tErr != nil {
			e.log.Warn("BT announce failed", "gid", gid, "url", u, "error", tErr)
			continue
		}
		for _, p := range resp.Peers {
			addr := net.JoinHostPort(p.IP.String(), fmt.Sprintf("%d", p.Port))
			addrs = append(addrs, addr)
		}
		for _, p := range resp.Peers6 {
			addr := net.JoinHostPort(p.IP.String(), fmt.Sprintf("%d", p.Port))
			addrs = append(addrs, addr)
		}
		e.log.Debug("BT announce ok", "gid", gid, "url", u, "peers", len(resp.Peers))
	}
	return addrs
}

func (e *Engine) connectPeers(ctx context.Context, swarm *btSwarm, addrs []string, cfg btpeer.Config) {
	maxPeers := e.cfg.BTMaxPeers
	if maxPeers <= 0 {
		maxPeers = btPeerConnectLimit
	}

	limit := make(chan struct{}, 8)
	var mu sync.Mutex
	connected := 0

	for _, addr := range addrs {
		mu.Lock()
		if connected >= maxPeers {
			mu.Unlock()
			break
		}
		connected++
		mu.Unlock()

		limit <- struct{}{}
		go func(addr string) {
			defer func() { <-limit }()

			dialCtx, cancel := context.WithTimeout(ctx, btPeerDialTimeout)
			defer cancel()

			conn, err := btpeer.Dial(dialCtx, e.netDialer, addr, cfg)
			if err != nil {
				e.log.Debug("BT peer dial failed", "addr", addr, "error", err)
				return
			}

			swarm.addPeer(&peerState{
				conn:   conn,
				addr:   addr,
				pieces: swarm.numPieces,
			})
		}(addr)
	}

	for i := 0; i < cap(limit); i++ {
		limit <- struct{}{}
	}
}

func verifyAllPieces(ctx context.Context, adaptor disk.Adaptor, meta *torrent.MetaInfo) error {
	return verifySelectedPieces(ctx, adaptor, meta, nil)
}

func verifySelectedPieces(ctx context.Context, adaptor disk.Adaptor, meta *torrent.MetaInfo, wanted []bool) error {
	pieceLen := meta.Info.PieceLength
	pieces := meta.Info.Pieces
	buf := make([]byte, pieceLen)
	for i := 0; i < meta.NumPieces(); i++ {
		if len(wanted) > 0 && !wanted[i] {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		offset := int64(i) * pieceLen
		n, rErr := adaptor.ReadAt(buf, offset)
		if rErr != nil && n == 0 {
			return fmt.Errorf("read piece %d: %w", i, rErr)
		}
		expected := pieces[i*20 : (i+1)*20]
		actual := sha1Sum(buf[:n])
		if actual != *(*[20]byte)(expected) {
			return fmt.Errorf("piece %d hash mismatch", i)
		}
	}
	return nil
}

func announceURLs(meta *torrent.MetaInfo) []string {
	if meta.AnnounceList != nil {
		var urls []string
		for _, tier := range meta.AnnounceList {
			urls = append(urls, tier...)
		}
		return urls
	}
	if meta.Announce != "" {
		return []string{meta.Announce}
	}
	return nil
}

func sha1Sum(data []byte) [20]byte {
	return sha1.Sum(data)
}

func torrentFilesToDiskEntries(files []torrent.FileInfo) []disk.FileEntry {
	entries := make([]disk.FileEntry, len(files))
	var offset int64
	for i, f := range files {
		path := path.Join(f.Path...)
		entries[i] = disk.FileEntry{
			Name:      path,
			Length:    f.Length,
			Offset:    offset,
			Requested: true,
		}
		offset += f.Length
	}
	return entries
}

func torrentFilesToDiskEntriesWithOptions(root string, files []torrent.FileInfo, opts *config.Options, pieceLen int64, numPieces int) ([]disk.FileEntry, []bool, error) {
	selected, err := parseBTSelectedFiles("", len(files))
	if err != nil {
		return nil, nil, err
	}
	if opts != nil {
		selected, err = parseBTSelectedFiles(opts.SelectFile, len(files))
		if err != nil {
			return nil, nil, err
		}
	}
	indexOut, err := parseBTIndexOut(nil, len(files))
	if err != nil {
		return nil, nil, err
	}
	if opts != nil {
		indexOut, err = parseBTIndexOut(opts.IndexOut, len(files))
		if err != nil {
			return nil, nil, err
		}
	}

	pieceFilter := selectedPiecesForFiles(files, selected, pieceLen, numPieces)
	entries := make([]disk.FileEntry, len(files))
	var offset int64
	for i, f := range files {
		name := path.Join(append([]string{root}, f.Path...)...)
		if mapped, ok := indexOut[i]; ok {
			name = filepath.Clean(mapped)
		}
		requested := len(selected) == 0 || selected[i]
		touchedBySelectedPiece := requested || fileOverlapsWantedPiece(offset, f.Length, pieceLen, pieceFilter)
		length := f.Length
		if !touchedBySelectedPiece {
			length = 0
		}
		entries[i] = disk.FileEntry{
			Name:      name,
			Length:    length,
			Offset:    offset,
			Requested: requested,
		}
		offset += f.Length
	}

	return entries, pieceFilter, nil
}

func parseBTSelectedFiles(expr string, n int) ([]bool, error) {
	if expr == "" || n <= 1 {
		return nil, nil
	}
	selected := make([]bool, n)
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("select-file: empty index")
		}
		startText, endText, hasRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(strings.TrimSpace(startText))
		if err != nil || start <= 0 {
			return nil, fmt.Errorf("select-file: invalid index %q", part)
		}
		end := start
		if hasRange {
			end, err = strconv.Atoi(strings.TrimSpace(endText))
			if err != nil || end <= 0 || end < start {
				return nil, fmt.Errorf("select-file: invalid range %q", part)
			}
		}
		if start > n || end > n {
			return nil, fmt.Errorf("select-file: index out of range %q", part)
		}
		for i := start; i <= end; i++ {
			selected[i-1] = true
		}
	}
	for _, ok := range selected {
		if ok {
			return selected, nil
		}
	}
	return nil, fmt.Errorf("select-file: no files selected")
}

func parseBTIndexOut(values []string, n int) (map[int]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	indexOut := make(map[int]string, len(values))
	for _, value := range values {
		indexText, path, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(indexText) == "" || path == "" {
			return nil, fmt.Errorf("index-out: invalid mapping %q", value)
		}
		index, err := strconv.Atoi(strings.TrimSpace(indexText))
		if err != nil || index <= 0 || index > n {
			return nil, fmt.Errorf("index-out: index out of range %q", value)
		}
		if filepath.IsAbs(path) {
			return nil, fmt.Errorf("index-out: path must be relative %q", value)
		}
		indexOut[index-1] = path
	}
	return indexOut, nil
}

func selectedPiecesForFiles(files []torrent.FileInfo, selected []bool, pieceLen int64, numPieces int) []bool {
	if len(selected) == 0 || pieceLen <= 0 || numPieces <= 0 {
		return nil
	}
	wanted := make([]bool, numPieces)
	var offset int64
	for i, file := range files {
		if i < len(selected) && selected[i] && file.Length > 0 {
			startPiece := offset / pieceLen
			endPiece := (offset + file.Length - 1) / pieceLen
			for piece := startPiece; piece <= endPiece && piece < int64(numPieces); piece++ {
				wanted[piece] = true
			}
		}
		offset += file.Length
	}
	return wanted
}

func fileOverlapsWantedPiece(offset, length, pieceLen int64, wanted []bool) bool {
	if len(wanted) == 0 || length <= 0 || pieceLen <= 0 {
		return false
	}
	startPiece := offset / pieceLen
	endPiece := (offset + length - 1) / pieceLen
	for piece := startPiece; piece <= endPiece && piece < int64(len(wanted)); piece++ {
		if wanted[piece] {
			return true
		}
	}
	return false
}
