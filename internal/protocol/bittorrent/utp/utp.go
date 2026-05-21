package utp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Packet types per BEP 29
const (
	stData  = 0
	stFin   = 1
	stState = 2
	stReset = 3
	stSyn   = 4
)

// Extension types
const (
	extNone         = 0
	extSelectiveAck = 1
)

const (
	headerSize    = 20
	minPacketSize = 150
	maxPacketSize = 1500 // Ethernet MTU - IP - UDP - uTP header

	defaultWindowSize = 2 * maxPacketSize

	targetDelay  = 100 * time.Millisecond
	baseDelayMax = 2 * time.Minute

	keepAliveInterval = 30 * time.Second
	connTimeout       = 120 * time.Second

	maxCwndIncreasePacketsPerRTT = 1.0
	gainFactor                   = 1.0
	lossWindowFactor             = 0.5

	recvBufferSize = 65536
)

var (
	errShortPacket  = errors.New("utp: packet too short")
	errBadVersion   = errors.New("utp: unsupported protocol version")
	errConnClosed   = errors.New("utp: connection closed")
	errConnTimeout  = errors.New("utp: connection timeout")
	errConnReset    = errors.New("utp: connection reset")
	errSocketClosed = errors.New("utp: socket closed")
	errReadTimeout  = &timeoutError{}

	sysClock = &systemClock{}
)

type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

type packet struct {
	typ          uint8
	ver          uint8
	ext          uint16
	connID       uint16
	timestamp    uint32
	tsDiff       uint32
	wndSize      uint32
	seqNr        uint16
	ackNr        uint16
	payload      []byte
	selectiveAck *selectiveAckMask
}

func (p *packet) hasPayload() bool {
	return p.typ == stData || p.typ == stSyn
}

func (p *packet) encode(buf []byte) int {
	buf[0] = (p.typ << 4) | (p.ver & 0x0F)
	if p.selectiveAck != nil && len(p.selectiveAck.bitmask) > 0 {
		buf[1] = extSelectiveAck
	} else {
		buf[1] = extNone
	}

	binary.BigEndian.PutUint16(buf[2:4], p.connID)
	binary.BigEndian.PutUint32(buf[4:8], p.timestamp)
	binary.BigEndian.PutUint32(buf[8:12], p.tsDiff)
	binary.BigEndian.PutUint32(buf[12:16], p.wndSize)
	binary.BigEndian.PutUint16(buf[16:18], p.seqNr)
	binary.BigEndian.PutUint16(buf[18:20], p.ackNr)

	total := headerSize

	if p.selectiveAck != nil && len(p.selectiveAck.bitmask) > 0 {
		extBuf := buf[total:]
		extBuf[0] = extNone
		extBuf[1] = byte(len(p.selectiveAck.bitmask))
		copy(extBuf[2:], p.selectiveAck.bitmask)
		total += 2 + len(p.selectiveAck.bitmask)
	}

	if len(p.payload) > 0 {
		copy(buf[total:], p.payload)
		total += len(p.payload)
	}

	return total
}

func decodePacket(buf []byte) (*packet, int, error) {
	if len(buf) < headerSize {
		return nil, 0, errShortPacket
	}

	p := &packet{}
	verTyp := buf[0]
	p.typ = verTyp >> 4
	p.ver = verTyp & 0x0F

	if p.ver != 1 {
		return nil, 0, errBadVersion
	}

	p.ext = uint16(buf[1])
	p.connID = binary.BigEndian.Uint16(buf[2:4])
	p.timestamp = binary.BigEndian.Uint32(buf[4:8])
	p.tsDiff = binary.BigEndian.Uint32(buf[8:12])
	p.wndSize = binary.BigEndian.Uint32(buf[12:16])
	p.seqNr = binary.BigEndian.Uint16(buf[16:18])
	p.ackNr = binary.BigEndian.Uint16(buf[18:20])

	rd := headerSize
	extBuf := buf[rd:]
	if p.ext != 0 {
		for readExt := uint8(p.ext); readExt != 0 && len(extBuf) >= 2; {
			extLen := int(extBuf[1])
			if len(extBuf) < 2+extLen {
				break
			}
			if readExt == extSelectiveAck {
				p.selectiveAck = &selectiveAckMask{
					bitmask: make([]byte, extLen),
				}
				copy(p.selectiveAck.bitmask, extBuf[2:2+extLen])
			}
			readExt = extBuf[0]
			extBuf = extBuf[2+extLen:]
		}
	}
	extTotal := len(buf) - len(extBuf) - headerSize
	if extTotal > 0 {
		rd += extTotal
	}

	payloadLen := len(buf) - rd
	if payloadLen > 0 {
		p.payload = make([]byte, payloadLen)
		copy(p.payload, buf[rd:])
	}

	return p, rd + payloadLen, nil
}

type selectiveAckMask struct {
	bitmask []byte
}

func (s *selectiveAckMask) isAcked(seq uint16) bool {
	if s == nil {
		return false
	}
	return s.checkBit(int(seq) - 2)
}

func (s *selectiveAckMask) checkBit(offset int) bool {
	if offset < 0 {
		return false
	}
	idx := offset >> 3
	bit := offset & 7
	if idx >= len(s.bitmask) {
		return false
	}
	return s.bitmask[idx]&(1<<bit) != 0
}

func newSelectiveAckMask(ackedPackets []uint16, baseSeq uint16) *selectiveAckMask {
	if len(ackedPackets) == 0 {
		return nil
	}
	maxOff := 0
	for _, pkt := range ackedPackets {
		off := seqDiff(pkt, baseSeq) - 2
		if off > maxOff {
			maxOff = off
		}
	}
	if maxOff < 0 {
		return nil
	}
	numBytes := maxOff>>3 + 1
	if numBytes < 4 {
		numBytes = 4
	}
	mask := &selectiveAckMask{
		bitmask: make([]byte, numBytes),
	}
	for _, pkt := range ackedPackets {
		off := seqDiff(pkt, baseSeq) - 2
		idx := off >> 3
		bit := off & 7
		mask.bitmask[idx] |= 1 << bit
	}
	return mask
}

func (s *selectiveAckMask) ackedPackets(baseSeq uint16) []uint16 {
	if s == nil {
		return nil
	}
	var result []uint16
	for byteIdx, b := range s.bitmask {
		for bit := 0; bit < 8; bit++ {
			if b&(1<<bit) != 0 {
				off := uint16(byteIdx*8 + bit)
				pktSeq := baseSeq + off + 2
				result = append(result, pktSeq)
			}
		}
	}
	return result
}

type clock interface {
	now() time.Time
	micros() uint32
}

type systemClock struct{}

func (c *systemClock) now() time.Time {
	return time.Now()
}

func (c *systemClock) micros() uint32 {
	return uint32(time.Now().UnixMicro() & 0xFFFFFFFF)
}

type connState int

const (
	csSynSent   connState = iota
	csSynRecv   connState = iota
	csConnected connState = iota
	csFinSent   connState = iota
	csFinRecv   connState = iota
	csClosed    connState = iota
)

type sentPacket struct {
	seqNr      uint16
	data       []byte
	timestamp  uint32
	sendTime   time.Time
	retransmit int
}

type ledbatCC struct {
	targetDelay   time.Duration
	baseDelay     time.Duration
	currentDelay  time.Duration
	ourDelay      time.Duration
	maxWindow     uint32
	curWindow     uint32
	rtt           time.Duration
	rttVar        time.Duration
	timeoutVal    time.Duration
	lastDelayTime time.Time
	lossCount     int
	mu            sync.Mutex
}

func newLEDBAT() *ledbatCC {
	return &ledbatCC{
		targetDelay: targetDelay,
		maxWindow:   defaultWindowSize,
		timeoutVal:  1000 * time.Millisecond,
	}
}

func (l *ledbatCC) updateDelay(delay time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.currentDelay = delay

	if l.baseDelay == 0 || delay < l.baseDelay {
		l.baseDelay = delay
		l.lastDelayTime = sysClock.now()
		return
	}

	if delay < l.baseDelay {
		l.baseDelay = delay
	}
}

func (l *ledbatCC) updateWindow(outstandingPackets, wndSize uint32) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.curWindow = outstandingPackets

	if l.baseDelay == 0 {
		return
	}

	ourDelay := l.currentDelay - l.baseDelay
	l.ourDelay = ourDelay

	offTarget := int64(l.targetDelay) - int64(ourDelay)
	delayFactor := float64(offTarget) / float64(l.targetDelay)

	var windowFactor float64
	if l.maxWindow > 0 {
		windowFactor = float64(outstandingPackets) / float64(l.maxWindow)
	} else {
		windowFactor = 1.0
	}

	scaledGain := maxCwndIncreasePacketsPerRTT * delayFactor * windowFactor * gainFactor

	windowChange := int64(scaledGain * float64(maxPacketSize))
	newWindow := int64(l.maxWindow) + windowChange

	minWin := int64(minPacketSize * 2)
	if newWindow < minWin {
		newWindow = minWin
	}
	if newWindow < 0 {
		newWindow = minWin
	}

	l.maxWindow = uint32(newWindow)
}

func (l *ledbatCC) applyCC(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.lastDelayTime = now
}

func (l *ledbatCC) updateRTT(rtt time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rtt == 0 {
		l.rtt = rtt
		l.rttVar = rtt / 2
	} else {
		delta := l.rtt - rtt
		if delta < 0 {
			delta = -delta
		}
		l.rttVar += (delta - l.rttVar) / 4
		l.rtt += (rtt - l.rtt) / 8
	}

	l.timeoutVal = l.rtt + l.rttVar*4
	if l.timeoutVal < 500*time.Millisecond {
		l.timeoutVal = 500 * time.Millisecond
	}
}

func (l *ledbatCC) onLoss() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.maxWindow = uint32(float64(l.maxWindow) * lossWindowFactor)
	minWin := uint32(minPacketSize * 2)
	if l.maxWindow < minWin {
		l.maxWindow = minWin
	}

	l.timeoutVal *= 2
}

func (l *ledbatCC) timeout() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rtt == 0 {
		return l.timeoutVal
	}

	to := l.rtt + l.rttVar*4
	if to < 500*time.Millisecond {
		to = 500 * time.Millisecond
	}
	return to
}

func (l *ledbatCC) cwnd() uint32 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.maxWindow
}

func (l *ledbatCC) resetTimeout() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.timeoutVal = 1000 * time.Millisecond
}

type Socket struct {
	conn      *net.UDPConn
	acceptCh  chan *Conn
	closeOnce sync.Once
	closed    atomic.Bool
	ctx       context.Context
	cancel    context.CancelFunc
	pendings  map[string]*Conn
	dialings  map[string]*Conn
	pendingMu sync.Mutex
}

func NewSocket(addr string) (*Socket, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("utp: resolve %s: %w", addr, err)
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, fmt.Errorf("utp: listen %s: %w", addr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Socket{
		conn:     conn,
		acceptCh: make(chan *Conn, 32),
		ctx:      ctx,
		cancel:   cancel,
		pendings: make(map[string]*Conn),
		dialings: make(map[string]*Conn),
	}

	go s.readLoop()

	return s, nil
}

func (s *Socket) pendingKey(connID uint16, addr string) string {
	return fmt.Sprintf("%x-%s", connID, addr)
}

func (s *Socket) readLoop() {
	buf := make([]byte, 65536)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}

		pkt, _, err := decodePacket(buf[:n])
		if err != nil {
			continue
		}

		addrStr := remoteAddr.String()

		if pkt.typ == stSyn {
			connIDLocal := pkt.connID + 1
			conn := newConn(s, connIDLocal, pkt.connID, remoteAddr, false)
			s.pendingMu.Lock()
			s.pendings[s.pendingKey(connIDLocal, addrStr)] = conn
			s.pendingMu.Unlock()

			go func() {
				if err := conn.acceptHandshake(pkt); err != nil {
					conn.Close()
					return
				}
				select {
				case s.acceptCh <- conn:
				case <-s.ctx.Done():
					conn.Close()
				}
			}()
		} else {
			s.pendingMu.Lock()
			conn := s.dialings[s.pendingKey(pkt.connID, addrStr)]
			if conn == nil {
				conn = s.pendings[s.pendingKey(pkt.connID, addrStr)]
			}
			s.pendingMu.Unlock()

			if conn != nil {
				conn.deliverPacket(pkt, remoteAddr)
			}
		}
	}
}

func (s *Socket) Dial(ctx context.Context, addr *net.UDPAddr) (*Conn, error) {
	if s.closed.Load() {
		return nil, errSocketClosed
	}

	connID := uint16(rand.Intn(65536))
	connIDLocal := connID + 1
	conn := newConn(s, connIDLocal, connIDLocal, addr, true)

	addrStr := addr.String()

	s.pendingMu.Lock()
	s.dialings[s.pendingKey(connIDLocal, addrStr)] = conn
	s.dialings[s.pendingKey(connIDLocal+1, addrStr)] = conn
	s.pendingMu.Unlock()

	if err := conn.dialHandshake(ctx); err != nil {
		s.pendingMu.Lock()
		delete(s.dialings, s.pendingKey(connIDLocal, addrStr))
		delete(s.dialings, s.pendingKey(connIDLocal+1, addrStr))
		s.pendingMu.Unlock()
		return nil, err
	}

	// handshake succeeded, move to pendings
	s.pendingMu.Lock()
	delete(s.dialings, s.pendingKey(connIDLocal, addrStr))
	delete(s.dialings, s.pendingKey(connIDLocal+1, addrStr))
	s.pendings[s.pendingKey(connIDLocal, addrStr)] = conn
	s.pendingMu.Unlock()

	return conn, nil
}

func (s *Socket) Accept(ctx context.Context) (*Conn, error) {
	if s.closed.Load() {
		return nil, errSocketClosed
	}

	select {
	case conn := <-s.acceptCh:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, errSocketClosed
	}
}

func (s *Socket) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.cancel()
		err = s.conn.Close()
	})
	return err
}

func (s *Socket) LocalAddr() net.Addr {
	return s.conn.LocalAddr()
}

type Conn struct {
	socket      *Socket
	connIDSend  uint16
	connIDLocal uint16
	connIDRecv  uint16
	remote      *net.UDPAddr
	state       connState
	isInitiator bool

	seqNr    uint16
	ackNr    uint16
	eofSeqNr uint16
	hasEOF   bool

	sendBuf   []sentPacket
	sendBufMu sync.Mutex
	recvBuf   map[uint16][]byte
	recvBufMu sync.Mutex

	nextExpected  uint16
	recvAssembled chan []byte

	cc        *ledbatCC
	ccLastSeq uint16

	incoming chan packetEvent
	outgoing chan *packet

	readDeadline  time.Time
	writeDeadline time.Time
	mu            sync.Mutex

	closed    atomic.Bool
	closeOnce sync.Once
	closeCh   chan struct{}
	closeErr  error
	closeMu   sync.Mutex

	lastActivity time.Time
	wndSize      uint32

	ackedSeqs map[uint16]bool
	sentTimes map[uint16]time.Time

	replyMicro uint32

	writeBufCond *sync.Cond
	writeBuf     []byte
	writeBufOff  int
	writeBufDone bool

	readBuf    []byte
	readBufOff int
	readBufEnd int
}

type packetEvent struct {
	pkt  *packet
	addr *net.UDPAddr
}

func newConn(socket *Socket, connIDLocal, connIDRecv uint16, remote *net.UDPAddr, isInitiator bool) *Conn {
	c := &Conn{
		socket:        socket,
		connIDSend:    connIDRecv,
		connIDLocal:   connIDLocal,
		connIDRecv:    connIDRecv,
		remote:        remote,
		isInitiator:   isInitiator,
		recvBuf:       make(map[uint16][]byte),
		recvAssembled: make(chan []byte, 256),
		cc:            newLEDBAT(),
		incoming:      make(chan packetEvent, 256),
		outgoing:      make(chan *packet, 256),
		closeCh:       make(chan struct{}),
		lastActivity:  sysClock.now(),
		wndSize:       recvBufferSize,
		ackedSeqs:     make(map[uint16]bool),
		sentTimes:     make(map[uint16]time.Time),
	}
	c.writeBufCond = sync.NewCond(&c.mu)
	if isInitiator {
		c.seqNr = 1
		c.state = csSynSent
	} else {
		c.seqNr = uint16(rand.Intn(65536))
		c.ackNr = 0
		c.state = csClosed
	}
	c.ackNr = 0

	go c.writeLoop()
	go c.tickLoop()

	return c
}

func (c *Conn) dialHandshake(ctx context.Context) error {
	syn := &packet{
		typ:       stSyn,
		ver:       1,
		connID:    c.connIDRecv,
		timestamp: sysClock.micros(),
		seqNr:     c.seqNr,
	}
	c.seqNr++

	data := make([]byte, headerSize)
	n := syn.encode(data)
	if err := c.sendRaw(data[:n]); err != nil {
		return err
	}
	c.sentTimes[syn.seqNr] = sysClock.now()

	select {
	case ev := <-c.incoming:
		pkt := ev.pkt
		if pkt.typ != stState {
			return fmt.Errorf("utp: expected ST_STATE, got %d", pkt.typ)
		}
		c.connIDRecv = pkt.connID
		c.ackNr = pkt.seqNr
		c.nextExpected = pkt.seqNr + 1
		c.state = csConnected
		c.recvAssembled = make(chan []byte, 256)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return errConnTimeout
	}
}

func (c *Conn) acceptHandshake(syn *packet) error {
	c.ackNr = syn.seqNr
	c.state = csSynRecv

	statePkt := &packet{
		typ:       stState,
		ver:       1,
		connID:    c.connIDLocal,
		timestamp: sysClock.micros(),
		seqNr:     c.seqNr,
		ackNr:     c.ackNr,
		wndSize:   c.wndSize,
	}
	c.seqNr++

	data := make([]byte, headerSize)
	n := statePkt.encode(data)
	if err := c.sendRaw(data[:n]); err != nil {
		return err
	}

	c.state = csConnected
	c.nextExpected = syn.seqNr + 1
	return nil
}

func (c *Conn) deliverPacket(pkt *packet, addr *net.UDPAddr) {
	c.mu.Lock()
	c.lastActivity = sysClock.now()
	c.mu.Unlock()

	if pkt.tsDiff != 0 {
		c.cc.updateDelay(time.Duration(pkt.tsDiff) * time.Microsecond)
	}

	select {
	case c.incoming <- packetEvent{pkt: pkt, addr: addr}:
	default:
	}
}

func (c *Conn) writeLoop() {
	for {
		select {
		case pkt := <-c.outgoing:
			if pkt == nil {
				return
			}
			data := make([]byte, headerSize+maxPacketSize+32)
			n := pkt.encode(data)
			c.sendRaw(data[:n])
		case <-c.closeCh:
			return
		}
	}
}

func (c *Conn) tickLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.checkTimeouts()
			c.processIncoming()
			c.flushWriteBuf()
		case <-c.closeCh:
			return
		}
	}
}

func (c *Conn) processIncoming() {
	for {
		select {
		case ev := <-c.incoming:
			pkt := ev.pkt
			c.handlePacket(pkt)
		default:
			return
		}
	}
}

func (c *Conn) handlePacket(pkt *packet) {
	c.updateRTT(pkt)

	switch pkt.typ {
	case stData:
		c.handleData(pkt)
	case stState:
		c.handleState(pkt)
	case stFin:
		c.handleFin(pkt)
	case stReset:
		c.handleReset()
	}

	c.processAcks(pkt)

	if pkt.seqNr != 0 || pkt.typ == stSyn {
		c.sendAck()
	}
}

func (c *Conn) updateRTT(pkt *packet) {
	if pkt.tsDiff != 0 {
		c.replyMicro = sysClock.micros() - pkt.timestamp
	}

	c.sendBufMu.Lock()
	if sendTime, ok := c.sentTimes[pkt.ackNr]; ok {
		rtt := sysClock.now().Sub(sendTime)
		c.cc.updateRTT(rtt)
		delete(c.sentTimes, pkt.ackNr)
	}
	c.sendBufMu.Unlock()
}

func (c *Conn) handleData(pkt *packet) {
	c.ackNr = pkt.seqNr
	c.deliverData(pkt.seqNr, pkt.payload)
	c.cc.updateWindow(uint32(len(c.sendBuf)), c.wndSize)
	c.cc.applyCC(sysClock.now())
}

func (c *Conn) deliverData(seq uint16, data []byte) {
	c.recvBufMu.Lock()
	defer c.recvBufMu.Unlock()

	if seqDiff(seq, c.nextExpected) > 32767 {
		return
	}

	c.recvBuf[seq] = data

	for {
		payload, ok := c.recvBuf[c.nextExpected]
		if !ok {
			break
		}
		delete(c.recvBuf, c.nextExpected)
		c.nextExpected++

		if len(payload) > 0 {
			c.mu.Lock()
			if c.readBuf == nil || c.readBufOff >= len(c.readBuf) {
				c.readBuf = make([]byte, 0, recvBufferSize)
				c.readBufOff = 0
			}
			c.readBuf = append(c.readBuf, payload...)
			c.readBufEnd = len(c.readBuf)
			c.mu.Unlock()
		}
	}
}

func (c *Conn) handleState(pkt *packet) {
	if pkt.ackNr != 0 {
		c.processAcks(pkt)
	}
}

func (c *Conn) handleFin(pkt *packet) {
	c.ackNr = pkt.seqNr
	c.hasEOF = true
	c.eofSeqNr = pkt.seqNr

	if c.state == csFinSent {
		c.state = csClosed
	}
}

func (c *Conn) handleReset() {
	c.closeWithError(errConnReset)
}

func (c *Conn) processAcks(pkt *packet) {
	c.sendBufMu.Lock()
	defer c.sendBufMu.Unlock()

	newSendBuf := c.sendBuf[:0]
	for _, sp := range c.sendBuf {
		if seqDiff(sp.seqNr, pkt.ackNr) > 32767 {
			continue
		}
		newSendBuf = append(newSendBuf, sp)
	}
	c.sendBuf = newSendBuf

	if pkt.selectiveAck != nil {
		acked := pkt.selectiveAck.ackedPackets(pkt.ackNr)
		for _, ackSeq := range acked {
			newSendBuf = newSendBuf[:0]
			for _, sp := range c.sendBuf {
				if sp.seqNr == ackSeq {
					continue
				}
				newSendBuf = append(newSendBuf, sp)
			}
			c.sendBuf = newSendBuf
		}
	}
}

func (c *Conn) sendAck() {
	if c.closed.Load() || c.state == csClosed {
		return
	}

	ackPkt := &packet{
		typ:       stState,
		ver:       1,
		connID:    c.connIDRecv,
		timestamp: sysClock.micros(),
		tsDiff:    c.replyMicro,
		wndSize:   c.wndSize,
		ackNr:     c.ackNr,
	}

	data := make([]byte, headerSize)
	ackPkt.encode(data)
	c.sendRaw(data)
}

func (c *Conn) checkTimeouts() {
	c.sendBufMu.Lock()
	defer c.sendBufMu.Unlock()

	now := sysClock.now()
	for i, sp := range c.sendBuf {
		if sp.retransmit > 0 || now.Sub(sp.sendTime) < c.cc.timeout() {
			continue
		}

		c.cc.onLoss()

		c.sendBuf[i].retransmit++
		data := make([]byte, headerSize+maxPacketSize)
		pkt := &packet{
			typ:       stData,
			ver:       1,
			connID:    c.connIDRecv,
			timestamp: sysClock.micros(),
			tsDiff:    c.replyMicro,
			wndSize:   c.wndSize,
			seqNr:     sp.seqNr,
			ackNr:     c.ackNr,
			payload:   sp.data,
		}
		n := pkt.encode(data)
		c.sendRaw(data[:n])
		c.sendBuf[i].sendTime = now
	}
}

func (c *Conn) sendRaw(data []byte) error {
	if c.closed.Load() {
		return errConnClosed
	}
	_, err := c.socket.conn.WriteToUDP(data, c.remote)
	return err
}

func (c *Conn) Read(b []byte) (int, error) {
	if c.closed.Load() {
		return 0, errConnClosed
	}

	for {
		c.mu.Lock()
		if c.readBufOff < c.readBufEnd {
			n := copy(b, c.readBuf[c.readBufOff:c.readBufEnd])
			c.readBufOff += n
			if c.readBufOff >= c.readBufEnd {
				c.readBuf = nil
				c.readBufOff = 0
				c.readBufEnd = 0
			}
			c.mu.Unlock()
			return n, nil
		}
		deadline := c.readDeadline
		c.mu.Unlock()

		if c.hasEOF && c.nextExpected > c.eofSeqNr {
			return 0, io.EOF
		}

		if !deadline.IsZero() && sysClock.now().After(deadline) {
			return 0, &net.OpError{Op: "read", Net: "utp", Source: c.LocalAddr(), Addr: c.RemoteAddr(), Err: errReadTimeout}
		}

		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-timer.C:
			c.processIncoming()
		case <-c.closeCh:
			timer.Stop()
			return 0, errConnClosed
		}
		timer.Stop()
	}
}

func (c *Conn) Write(b []byte) (int, error) {
	if c.closed.Load() || c.state == csClosed {
		return 0, errConnClosed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	totalWritten := 0
	remaining := b

	for len(remaining) > 0 {
		for c.sendBufLen() >= int(c.cc.cwnd()) && !c.closed.Load() {
			if !c.writeDeadline.IsZero() && sysClock.now().After(c.writeDeadline) {
				return totalWritten, &net.OpError{Op: "write", Net: "utp", Source: c.LocalAddr(), Addr: c.RemoteAddr(), Err: errors.New("i/o timeout")}
			}
			c.writeBufCond.Wait()
		}

		if c.closed.Load() {
			return totalWritten, errConnClosed
		}

		chunkSize := maxPacketSize - headerSize
		if len(remaining) < chunkSize {
			chunkSize = len(remaining)
		}

		chunk := remaining[:chunkSize]
		remaining = remaining[chunkSize:]

		c.seqNr++
		pkt := &packet{
			typ:       stData,
			ver:       1,
			connID:    c.connIDRecv,
			timestamp: sysClock.micros(),
			tsDiff:    c.replyMicro,
			wndSize:   c.wndSize,
			seqNr:     c.seqNr - 1,
			ackNr:     c.ackNr,
			payload:   chunk,
		}

		sp := sentPacket{
			seqNr:     c.seqNr - 1,
			data:      make([]byte, len(chunk)),
			timestamp: pkt.timestamp,
			sendTime:  sysClock.now(),
		}
		copy(sp.data, chunk)

		c.sendBufMu.Lock()
		c.sendBuf = append(c.sendBuf, sp)
		c.sendBufMu.Unlock()

		c.sentTimes[c.seqNr-1] = sysClock.now()

		data := make([]byte, headerSize+len(chunk))
		n := pkt.encode(data)
		if err := c.sendRaw(data[:n]); err != nil {
			return totalWritten, err
		}

		totalWritten += len(chunk)
	}

	return totalWritten, nil
}

func (c *Conn) sendBufLen() int {
	c.sendBufMu.Lock()
	defer c.sendBufMu.Unlock()
	return len(c.sendBuf)
}

func (c *Conn) flushWriteBuf() {
	c.mu.Lock()
	c.writeBufCond.Broadcast()
	c.mu.Unlock()
}

func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		defer c.closeMu.Unlock()

		c.closed.Store(true)

		if c.state == csConnected || c.state == csSynRecv {
			fin := &packet{
				typ:       stFin,
				ver:       1,
				connID:    c.connIDRecv,
				timestamp: sysClock.micros(),
				tsDiff:    c.replyMicro,
				seqNr:     c.seqNr,
				ackNr:     c.ackNr,
				wndSize:   c.wndSize,
			}
			data := make([]byte, headerSize)
			fin.encode(data)
			c.sendRaw(data)
			c.state = csFinSent
		}

		c.closeErr = errConnClosed
		close(c.closeCh)

		c.socket.pendingMu.Lock()
		addrStr := c.remote.String()
		delete(c.socket.dialings, c.socket.pendingKey(c.connIDLocal, addrStr))
		delete(c.socket.dialings, c.socket.pendingKey(c.connIDLocal+1, addrStr))
		delete(c.socket.pendings, c.socket.pendingKey(c.connIDLocal, addrStr))
		c.socket.pendingMu.Unlock()
	})

	return nil
}

func (c *Conn) closeWithError(err error) {
	c.closeMu.Lock()
	c.closeErr = err
	c.closeMu.Unlock()
	c.closed.Store(true)
	close(c.closeCh)
}

func (c *Conn) LocalAddr() net.Addr {
	return c.socket.conn.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *Conn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	c.writeDeadline = t
	return nil
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = t
	return nil
}

func seqDiff(a, b uint16) int {
	return (int(a) - int(b) + 65536) % 65536
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
