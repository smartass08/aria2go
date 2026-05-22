package peer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/ioutilx"
	"github.com/smartass08/aria2go/internal/mse"
	"github.com/smartass08/aria2go/internal/netx"
)

var (
	ErrPeerClosed           = errors.New("peer: connection closed")
	ErrProtocolViolation    = core.NewError(core.ExitNetworkProblem, "peer: protocol violation")
	ErrHandshakeTimeout     = core.WrapError(core.ExitTimeout, "peer: handshake timeout", nil)
	ErrConnectionTimeout    = core.WrapError(core.ExitTimeout, "peer: connection timeout", nil)
	ErrEOF                  = core.NewError(core.ExitNetworkProblem, "peer: connection closed by peer")
	ErrMaxPayloadExceeded   = core.NewError(core.ExitNetworkProblem, "peer: max payload exceeded")
	ErrSendQueueFull        = core.NewError(core.ExitNetworkProblem, "peer: send queue full")
	ErrFloodingDetected     = core.NewError(core.ExitNetworkProblem, "peer: flooding detected")
	ErrMutuallyUninterested = core.NewError(core.ExitNetworkProblem, "peer: mutually uninterested")
	ErrInactivityTimeout    = core.NewError(core.ExitNetworkProblem, "peer: inactivity timeout")
	ErrSeederToSeeder       = core.NewError(core.ExitNetworkProblem, "peer: both are seeders")
)

type floodingStat struct {
	chokeUnchokeCount atomic.Int64
	keepAliveCount    atomic.Int64
}

func (f *floodingStat) incChokeUnchoke() {
	f.chokeUnchokeCount.Add(1)
}

func (f *floodingStat) incKeepAlive() {
	f.keepAliveCount.Add(1)
}

func (f *floodingStat) reset() {
	f.chokeUnchokeCount.Store(0)
	f.keepAliveCount.Store(0)
}

func (f *floodingStat) chokeUnchoke() int {
	return int(f.chokeUnchokeCount.Load())
}

func (f *floodingStat) keepAlive() int {
	return int(f.keepAliveCount.Load())
}

type requestSlot struct {
	piece  int
	offset int
	length int
}

const floodingCheckInterval = 5 * time.Second

const (
	// aria2 PREF_BT_TIMEOUT default is 180 seconds (range 1-600)
	defaultTimeout       = 180 * time.Second
	defaultSendQueueSize = 32
)

var (
	// aria2 PREF_BT_KEEP_ALIVE_INTERVAL default is 120 seconds (range 1-120)
	keepAliveInterval = 120 * time.Second
)

var messagePool = sync.Pool{
	New: func() any {
		return &Message{}
	},
}

var bitfieldPayloadPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 4)
		return &b
	},
}

// PieceSource provides piece availability for a torrent.
type PieceSource interface {
	NumPieces() int
	Have(i int) bool
	Bitfield() []byte
}

type Config struct {
	InfoHash          [20]byte
	LocalPeerID       [20]byte
	Reserved          [8]byte
	Pieces            PieceSource
	PieceLength       int64
	Encrypt           mse.Mode
	Timeout           time.Duration
	KeepAliveInterval time.Duration // 0 means default (120s)
}

func (c Config) timeout() time.Duration {
	if c.Timeout <= 0 {
		return defaultTimeout
	}
	return c.Timeout
}

func (c Config) keepAlive() time.Duration {
	if c.KeepAliveInterval <= 0 {
		return keepAliveInterval
	}
	return c.KeepAliveInterval
}

// Conn is a BitTorrent peer wire connection.
type Conn struct {
	cfg Config

	conn      net.Conn
	sendCh    chan []byte
	recvCh    chan Message
	done      chan struct{}
	closeOnce sync.Once

	stats  statStore
	mu     sync.Mutex
	closed bool
	err    error

	peerHandshake  Handshake
	peerHasFastExt bool

	floodingStat   floodingStat
	floodingTimer  time.Time
	inactiveTimer  atomic.Int64 // unix nano
	allowedFastSet map[int]bool
	amAllowedFast  map[int]bool
	requestSlots   []requestSlot
	uploadSpeedFn  func() bool

	fastExtEnabled    bool
	extMsgEnabled     bool
	extensionIDs      map[int]uint8
	snubbing_         bool
	chokingReq_       bool
	optUnchoke_       bool
	bitfield          []byte
	numPieces         int
	hasAllPiecesCache atomic.Bool

	localHandshake [handshakeLen]byte
}

// Run starts the peer protocol main loop. It blocks until the context is
// cancelled, the connection is closed, or an error occurs.
// Received messages are sent to the channel returned by Messages().
func (c *Conn) Run(ctx context.Context) error {
	defer c.conn.Close()
	defer close(c.recvCh)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rerrCh := make(chan error, 1)
	go func() {
		rerrCh <- c.reader(ctx)
	}()
	go func() {
		c.writer(ctx)
	}()

	select {
	case err := <-rerrCh:
		c.setError(err)
		c.Close()
		return err
	case <-c.done:
		return ErrPeerClosed
	case <-ctx.Done():
		c.setError(ctx.Err())
		c.Close()
		return ctx.Err()
	}
}

func Dial(ctx context.Context, dialer *netx.Dialer, addr string, cfg Config) (*Conn, error) {
	return DialTransport(ctx, func(ctx context.Context) (net.Conn, error) {
		c, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("peer: dial %s: %w", addr, err)
		}
		return c, nil
	}, cfg)
}

func plainFallbackAllowed(mode mse.Mode) bool {
	return mode == mse.Allow || mode == mse.Prefer
}

// DialTransport establishes a peer connection over the provided transport
// dialer. When encryption is optional, it retries a fresh plain handshake
// after an MSE failure, matching aria2's legacy fallback.
func DialTransport(ctx context.Context, dial func(context.Context) (net.Conn, error), cfg Config) (*Conn, error) {
	c, err := dial(ctx)
	if err != nil {
		return nil, err
	}

	var transport net.Conn = c
	if cfg.Encrypt != mse.Off {
		encConn, _, _, err := mse.Initiate(c, cfg.InfoHash, cfg.Encrypt)
		if err != nil {
			c.Close()
			if !plainFallbackAllowed(cfg.Encrypt) {
				return nil, err
			}

			c, err = dial(ctx)
			if err != nil {
				return nil, err
			}
			transport = c
		} else {
			transport = encConn
		}
	}

	return dialHandshake(ctx, transport, cfg)
}

func dialHandshake(ctx context.Context, transport net.Conn, cfg Config) (*Conn, error) {
	conn, err := newConn(transport, cfg)
	if err != nil {
		transport.Close()
		return nil, err
	}

	if err := conn.setDeadline(ctx, cfg.timeout()); err != nil {
		conn.conn.Close()
		return nil, err
	}
	if _, err := conn.conn.Write(conn.localHandshake[:]); err != nil {
		conn.conn.Close()
		return nil, fmt.Errorf("peer: write handshake: %w", err)
	}

	peerHS, err := conn.readHandshake()
	if err != nil {
		conn.conn.Close()
		return nil, fmt.Errorf("peer: read handshake: %w", err)
	}
	conn.clearDeadline()
	conn.peerHandshake = peerHS
	if peerHS.InfoHash != cfg.InfoHash {
		conn.conn.Close()
		return nil, fmt.Errorf("%w: handshake info hash mismatch", ErrProtocolViolation)
	}
	conn.peerHasFastExt = hasFastExtension(peerHS.Reserved)

	return conn, nil
}

func Accept(ctx context.Context, c net.Conn, cfg Config) (*Conn, error) {
	stopClose := closeConnOnContextDone(ctx, c)
	defer stopClose()

	var transport net.Conn = c
	if cfg.Encrypt != mse.Off {
		if err := setConnDeadline(ctx, c, cfg.timeout()); err != nil {
			c.Close()
			return nil, err
		}
		defer c.SetDeadline(time.Time{})

		if plainFallbackAllowed(cfg.Encrypt) {
			var prefix [1 + pstrLen]byte
			if _, err := io.ReadFull(c, prefix[:]); err != nil {
				c.Close()
				return nil, fmt.Errorf("peer: read protocol prefix: %w", err)
			}
			transport = &prefixConn{Conn: c, pending: prefix[:]}
			if prefix[0] == pstrLen && string(prefix[1:]) == pstr {
				goto handshake
			}
		}

		infoHashes := [][20]byte{cfg.InfoHash}
		encConn, _, _, err := mse.Receive(transport, infoHashes, cfg.Encrypt)
		if err != nil {
			c.Close()
			return nil, err
		}
		transport = encConn
	}

handshake:
	return acceptHandshake(ctx, transport, cfg)
}

type prefixConn struct {
	net.Conn
	pending []byte
}

func (c *prefixConn) Read(b []byte) (int, error) {
	if len(c.pending) == 0 {
		return c.Conn.Read(b)
	}
	n := copy(b, c.pending)
	c.pending = c.pending[n:]
	if n == len(b) {
		return n, nil
	}
	m, err := c.Conn.Read(b[n:])
	return n + m, err
}

func newConn(conn net.Conn, cfg Config) (*Conn, error) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}
	c := &Conn{
		cfg:            cfg,
		conn:           conn,
		sendCh:         make(chan []byte, defaultSendQueueSize),
		recvCh:         make(chan Message, defaultSendQueueSize),
		done:           make(chan struct{}),
		chokingReq_:    true,
		localHandshake: marshalHandshake(cfg.InfoHash, cfg.LocalPeerID, cfg.Reserved),
	}
	if cfg.Pieces != nil {
		c.numPieces = cfg.Pieces.NumPieces()
	}
	c.stats.peerChoking.Store(true)
	c.stats.choked.Store(true)
	return c, nil
}

func (c *Conn) send(data []byte) error {
	// non-blocking send; if queue is full return error
	select {
	case c.sendCh <- data:
		return nil
	default:
		return ErrSendQueueFull
	}
}

func (c *Conn) Choke() error {
	return c.send(NewMessage(MsgChoke, nil).Encode())
}

func (c *Conn) Unchoke() error {
	return c.send(NewMessage(MsgUnchoke, nil).Encode())
}

func (c *Conn) Interested() error {
	return c.send(NewMessage(MsgInterested, nil).Encode())
}

func (c *Conn) NotInterested() error {
	return c.send(NewMessage(MsgNotInterested, nil).Encode())
}

func (c *Conn) Have(piece int) error {
	if err := validatePieceIndex(piece, c.numPieces); err != nil {
		return err
	}
	return c.send(MarshalHave(piece))
}

func (c *Conn) Bitfield(bf []byte) error {
	if c.numPieces > 0 {
		if err := validateBitfieldPayload(bf, c.numPieces); err != nil {
			return err
		}
	}
	return c.send(MarshalBitfield(bf))
}

func (c *Conn) Request(piece, offset, length int) error {
	if err := validateBlockRange(piece, offset, length, c.numPieces, c.cfg.PieceLength); err != nil {
		return err
	}
	c.mu.Lock()
	c.requestSlots = append(c.requestSlots, requestSlot{piece: piece, offset: offset, length: length})
	c.mu.Unlock()
	return c.send(MarshalRequest(piece, offset, length))
}

func (c *Conn) Piece(piece, offset int, data []byte) error {
	if err := validateBlockRange(piece, offset, len(data), c.numPieces, c.cfg.PieceLength); err != nil {
		return err
	}
	return c.send(MarshalPiece(piece, offset, data))
}

func (c *Conn) Cancel(piece, offset, length int) error {
	if err := validateBlockRange(piece, offset, length, c.numPieces, c.cfg.PieceLength); err != nil {
		return err
	}
	c.mu.Lock()
	c.removeRequestSlot(piece, offset, length)
	c.mu.Unlock()
	return c.send(MarshalCancel(piece, offset, length))
}

func (c *Conn) Port(port uint16) error {
	return c.send(MarshalPort(port))
}

func (c *Conn) HaveAll() error {
	return c.send(NewMessage(MsgHaveAll, nil).Encode())
}

func (c *Conn) HaveNone() error {
	return c.send(NewMessage(MsgHaveNone, nil).Encode())
}

func (c *Conn) Suggest(piece int) error {
	if err := validatePieceIndex(piece, c.numPieces); err != nil {
		return err
	}
	buf := make([]byte, 4)
	putUint32(buf, uint32(piece))
	return c.send(NewMessage(MsgSuggest, buf).Encode())
}

func (c *Conn) AllowedFast(piece int) error {
	if err := validatePieceIndex(piece, c.numPieces); err != nil {
		return err
	}
	buf := make([]byte, 4)
	putUint32(buf, uint32(piece))
	return c.send(NewMessage(MsgAllowedFast, buf).Encode())
}

func (c *Conn) Reject(piece, offset, length int) error {
	if err := validateBlockRange(piece, offset, length, c.numPieces, c.cfg.PieceLength); err != nil {
		return err
	}
	buf := make([]byte, 12)
	putUint32(buf[0:4], uint32(piece))
	putUint32(buf[4:8], uint32(offset))
	putUint32(buf[8:12], uint32(length))
	return c.send(NewMessage(MsgReject, buf).Encode())
}

func (c *Conn) Snapshot() Stat {
	return c.stats.snapshot()
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return c.err
	}
	c.closed = true
	c.err = ErrPeerClosed
	c.closeOnce.Do(func() {
		close(c.done)
	})
	return c.conn.Close()
}

func (c *Conn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Conn) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.err = err
	}
}

// Messages returns the receive channel. The channel is closed when the
// connection shuts down or Run returns.
func (c *Conn) Messages() <-chan Message {
	return c.recvCh
}

func (c *Conn) PeerHandshake() Handshake {
	return c.peerHandshake
}

func (c *Conn) PeerSupportsExtensionMessaging() bool {
	return hasExtensionMessaging(c.peerHandshake.Reserved)
}

func (c *Conn) PeerSupportsDHT() bool {
	return hasDHT(c.peerHandshake.Reserved)
}

func (c *Conn) ExtensionMessageID(key int) uint8 {
	return c.getExtensionMessageID(key)
}

func acceptHandshake(ctx context.Context, transport net.Conn, cfg Config) (*Conn, error) {
	conn, err := newConn(transport, cfg)
	if err != nil {
		transport.Close()
		return nil, err
	}

	if err := conn.setDeadline(ctx, cfg.timeout()); err != nil {
		conn.conn.Close()
		return nil, err
	}
	peerHS, err := conn.readHandshake()
	if err != nil {
		conn.conn.Close()
		return nil, fmt.Errorf("peer: read handshake: %w", err)
	}
	conn.peerHandshake = peerHS
	if peerHS.InfoHash != cfg.InfoHash {
		conn.conn.Close()
		return nil, fmt.Errorf("%w: handshake info hash mismatch", ErrProtocolViolation)
	}
	conn.peerHasFastExt = hasFastExtension(peerHS.Reserved)

	if _, err := conn.conn.Write(conn.localHandshake[:]); err != nil {
		conn.conn.Close()
		return nil, fmt.Errorf("peer: write handshake: %w", err)
	}
	conn.clearDeadline()

	return conn, nil
}

func (c *Conn) SetExtensionMessageID(key int, id uint8) {
	c.addExtension(key, id)
}

func (c *Conn) Extended(id uint8, payload []byte) error {
	data := make([]byte, 1+len(payload))
	data[0] = id
	copy(data[1:], payload)
	return c.send(NewMessage(MsgExtended, data).Encode())
}

func (c *Conn) SendEncoded(data []byte) error {
	return c.send(cloneBytes(data))
}

func (c *Conn) writer(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		if !c.closed {
			c.closeOnce.Do(func() {
				close(c.done)
			})
		}
		c.mu.Unlock()
		c.conn.Close()
	}()

	keepAliveCh := make(chan struct{}, 1)
	var keepAliveScheduled atomic.Bool

	scheduleKeepAlive := func() {
		if !keepAliveScheduled.CompareAndSwap(false, true) {
			return
		}
		time.AfterFunc(c.cfg.keepAlive(), func() {
			keepAliveScheduled.Store(false)
			select {
			case keepAliveCh <- struct{}{}:
			default:
			}
		})
	}

	scheduleKeepAlive()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case data, ok := <-c.sendCh:
			if !ok {
				return
			}
			n, err := c.conn.Write(data)
			if err != nil {
				c.setError(err)
				return
			}
			if n > 0 {
				c.stats.addUploaded(n)
			}
			scheduleKeepAlive()
		case <-keepAliveCh:
			ka := KeepAlive()
			n, err := c.conn.Write(ka)
			if err != nil {
				c.setError(err)
				return
			}
			if n > 0 {
				c.stats.addUploaded(n)
			}
			scheduleKeepAlive()
		}
	}
}

func (c *Conn) reader(ctx context.Context) error {
	sockBuf := ioutilx.Pool64K.Get()
	defer sockBuf.Free()
	buf := sockBuf.Bytes()
	if cap(buf) > maxBufferCapacity {
		buf = buf[:maxBufferCapacity]
	} else {
		buf = buf[:cap(buf)]
	}

	msgBuf := ioutilx.Pool64K.Get()
	defer msgBuf.Free()
	var readBuf []byte

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if c.isClosed() {
			return ErrPeerClosed
		}

		n, err := c.conn.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrEOF
			}
			return fmt.Errorf("peer: read: %w", err)
		}
		c.stats.addDownloaded(n)

		if readBuf == nil {
			readBuf = msgBuf.Bytes()[:n]
			copy(readBuf, buf[:n])
		} else {
			readBuf = append(readBuf, buf[:n]...)
		}

		for {
			msg, consumed, err := tryDecodeMessage(readBuf)
			if err != nil {
				return err
			}
			if consumed > 0 {
				if consumed == len(readBuf) {
					readBuf = readBuf[:0]
				} else {
					readBuf = readBuf[consumed:]
				}
			}
			if msg == nil {
				if consumed > 0 {
					c.floodingStat.incKeepAlive()
				}
				break
			}

			if err := c.handleIncoming(msg); err != nil {
				messagePool.Put(msg)
				return err
			}

			select {
			case c.recvCh <- *msg:
			case <-c.done:
				return ErrPeerClosed
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			messagePool.Put(msg)
		}
	}
}

func tryDecodeMessage(buf []byte) (*Message, int, error) {
	if len(buf) < 4 {
		return nil, 0, nil
	}
	length := getUint32(buf[:4])
	if length == 0 {
		// keep-alive
		return nil, 4, nil
	}
	if length > uint32(maxBufferCapacity) {
		return nil, 0, fmt.Errorf("peer: payload too large: %d", length)
	}
	totalLen := 4 + int(length)
	if len(buf) < totalLen {
		return nil, 0, nil
	}
	msg := messagePool.Get().(*Message)
	msg.ID = MessageID(buf[4])
	msg.Payload = cloneBytes(buf[5:totalLen])
	if err := validatePayloadShape(*msg); err != nil {
		messagePool.Put(msg)
		return nil, 0, err
	}
	return msg, totalLen, nil
}

func (c *Conn) handleIncoming(msg *Message) error {
	switch msg.ID {
	case MsgChoke:
		if !c.stats.peerChoking.Load() {
			c.floodingStat.incChokeUnchoke()
		}
		c.stats.peerChoking.Store(true)
		c.doChokedAction()
	case MsgUnchoke:
		if c.stats.peerChoking.Load() {
			c.floodingStat.incChokeUnchoke()
		}
		c.stats.peerChoking.Store(false)
	case MsgInterested:
		c.stats.peerInterest.Store(true)
	case MsgNotInterested:
		c.stats.peerInterest.Store(false)
	case MsgBitfield:
		if err := c.handleBitfield(msg.Payload); err != nil {
			return err
		}
	case MsgHaveAll:
		c.markSeeder()
	case MsgHaveNone:
		if c.bitfield != nil {
			for i := range c.bitfield {
				c.bitfield[i] = 0
			}
			c.hasAllPiecesCache.Store(false)
		}
	case MsgHave:
		piece, err := UnmarshalHave(*msg)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrProtocolViolation, err)
		}
		if err := validatePieceIndex(piece, c.numPieces); err != nil {
			return err
		}
		c.updateBitfield(piece, 1)
	case MsgRequest, MsgCancel, MsgReject:
		piece, offset, length, err := unmarshalRange(*msg)
		if err != nil {
			return err
		}
		if err := validateBlockRange(piece, offset, length, c.numPieces, c.cfg.PieceLength); err != nil {
			return err
		}
		if msg.ID == MsgRequest {
			c.inactiveTimer.Store(time.Now().UnixNano())
		}
	case MsgPiece:
		piece, offset, data, err := UnmarshalPiece(*msg)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrProtocolViolation, err)
		}
		if err := validateBlockRange(piece, offset, len(data), c.numPieces, c.cfg.PieceLength); err != nil {
			return err
		}
		c.inactiveTimer.Store(time.Now().UnixNano())
	case MsgPort:
		_ = len(msg.Payload) // port message received, upper layer handles
	}
	return nil
}

func unmarshalRange(msg Message) (piece, offset, length int, err error) {
	switch msg.ID {
	case MsgRequest:
		return UnmarshalRequest(msg)
	case MsgCancel:
		return UnmarshalCancel(msg)
	case MsgReject:
		if len(msg.Payload) != 12 {
			return 0, 0, 0, fmt.Errorf("%w: reject payload length = %d, want 12", ErrProtocolViolation, len(msg.Payload))
		}
		piece = int(getUint32(msg.Payload[0:4]))
		offset = int(getUint32(msg.Payload[4:8]))
		length = int(getUint32(msg.Payload[8:12]))
		return piece, offset, length, nil
	default:
		return 0, 0, 0, fmt.Errorf("%w: expected range message, got %d", ErrProtocolViolation, msg.ID)
	}
}

func (c *Conn) removeRequestSlot(piece, offset, length int) {
	for i, slot := range c.requestSlots {
		if slot.piece == piece && slot.offset == offset && slot.length == length {
			c.requestSlots = append(c.requestSlots[:i], c.requestSlots[i+1:]...)
			return
		}
	}
}

func (c *Conn) doChokedAction() {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.requestSlots[:0]
	for _, slot := range c.requestSlots {
		if c.allowedFastSet != nil && c.allowedFastSet[slot.piece] {
			remaining = append(remaining, slot)
		}
	}
	c.requestSlots = remaining
}

func (c *Conn) SetAllowedFast(piece int) {
	if c.allowedFastSet == nil {
		c.allowedFastSet = make(map[int]bool)
	}
	c.allowedFastSet[piece] = true
}

func (c *Conn) SetUploadSpeedCheck(fn func() bool) {
	c.uploadSpeedFn = fn
}

func (c *Conn) CheckFlooding() error {
	if c.floodingTimer.IsZero() {
		c.floodingTimer = time.Now()
		return nil
	}
	if time.Since(c.floodingTimer) >= floodingCheckInterval {
		if c.floodingStat.chokeUnchoke() >= 2 || c.floodingStat.keepAlive() >= 2 {
			c.floodingStat.reset()
			c.floodingTimer = time.Now()
			return ErrFloodingDetected
		}
		c.floodingStat.reset()
		c.floodingTimer = time.Now()
	}
	return nil
}

func (c *Conn) CheckInactivity(amInterested, peerInterested bool, isPeerSeeder, isLocalSeeder bool) error {
	lastActivity := c.inactiveTimer.Load()
	if lastActivity == 0 {
		c.inactiveTimer.Store(time.Now().UnixNano())
		return nil
	}
	inactiveTime := time.Since(time.Unix(0, lastActivity))

	if !amInterested && !peerInterested && inactiveTime >= 30*time.Second {
		return ErrMutuallyUninterested
	}
	if inactiveTime >= 60*time.Second {
		return ErrInactivityTimeout
	}
	if isPeerSeeder && isLocalSeeder {
		return ErrSeederToSeeder
	}
	return nil
}

func (c *Conn) CheckActiveInteraction(amInterested, peerInterested bool, isPeerSeeder, isLocalSeeder bool) error {
	if err := c.CheckInactivity(amInterested, peerInterested, isPeerSeeder, isLocalSeeder); err != nil {
		return err
	}
	if err := c.CheckFlooding(); err != nil {
		return err
	}
	return nil
}

func getUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func putUint32(b []byte, v uint32) {
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)
}

func (c *Conn) setDeadline(ctx context.Context, d time.Duration) error {
	return setConnDeadline(ctx, c.conn, d)
}

func setConnDeadline(ctx context.Context, conn net.Conn, d time.Duration) error {
	deadline, ok := ctx.Deadline()
	if ok {
		remaining := time.Until(deadline)
		if remaining < d {
			d = remaining
		}
	}
	if d <= 0 {
		return ErrHandshakeTimeout
	}
	return conn.SetDeadline(time.Now().Add(d))
}

func closeConnOnContextDone(ctx context.Context, conn net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func (c *Conn) clearDeadline() {
	c.conn.SetDeadline(time.Time{})
}

func (c *Conn) readHandshake() (Handshake, error) {
	var buf [handshakeLen]byte
	if _, err := io.ReadFull(c.conn, buf[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return Handshake{}, ErrEOF
		}
		return Handshake{}, fmt.Errorf("peer: read handshake: %w", err)
	}
	return parseHandshake(buf[:])
}
