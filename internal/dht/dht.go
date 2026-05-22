package dht

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/smartass08/aria2go/internal/bencode"
)

const (
	messageTimeout     = 10 * time.Second
	bucketRefreshInt   = 15 * time.Minute
	bucketRefreshCheck = 5 * time.Minute
	tokenUpdateInt     = 10 * time.Minute
	tokenSecretSize    = 4
	peerAnnounceCheck  = 5 * time.Minute
	transactionIDLen   = 8
	maxUDPMsgSize      = 65507
)

type Config struct {
	NodeID    NodeID
	Addr      string
	Bootstrap []string
	PersistTo string
}

type pendingTx struct {
	target    *routingNode
	txID      string
	msgType   string
	callback  func(msg *Message, err error)
	deadline  time.Time
	createdAt time.Time
	timeout   *timeoutEntry
}

var pendingTxPool = sync.Pool{
	New: func() any { return &pendingTx{} },
}

type Server struct {
	cfg  Config
	rt   *RoutingTable
	conn *net.UDPConn
	log  *slog.Logger

	mu  sync.Mutex
	txs map[string]*pendingTx

	peers   map[[20]byte][]net.Addr
	peersMu sync.RWMutex

	peerTokens   map[string]string
	peerTokensMu sync.RWMutex

	tokenSecrets   [2][tokenSecretSize]byte
	tokenSecretsMu sync.RWMutex

	sendRB sendRingBuffer

	th timeoutHeapType

	pingReply *bencode.DictVal
}

// --- sendRingBuffer: lock-based ring buffer replacing chan sendReq ---

const sendRingSize = 256

type sendRingBuffer struct {
	buf  [sendRingSize]sendReq
	head int
	tail int
	size int
	mu   sync.Mutex
	cond sync.Cond
}

func (rb *sendRingBuffer) init() {
	rb.cond.L = &rb.mu
}

func (rb *sendRingBuffer) enqueue(req sendReq) bool {
	rb.mu.Lock()
	if rb.size >= sendRingSize {
		rb.mu.Unlock()
		return false
	}
	rb.buf[rb.tail] = req
	rb.tail = (rb.tail + 1) % sendRingSize
	wasEmpty := rb.size == 0
	rb.size++
	if wasEmpty {
		rb.cond.Signal()
	}
	rb.mu.Unlock()
	return true
}

func (rb *sendRingBuffer) dequeue(ctx context.Context) (sendReq, bool) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	for rb.size == 0 && ctx.Err() == nil {
		rb.cond.Wait()
	}
	if ctx.Err() != nil {
		return sendReq{}, false
	}
	req := rb.buf[rb.head]
	rb.head = (rb.head + 1) % sendRingSize
	rb.size--
	return req, true
}

// --- timeoutHeapType: single goroutine with min-heap of deadlines ---

type timeoutEntry struct {
	txKey     string
	deadline  time.Time
	cancelled atomic.Bool
	index     int
}

type timeoutHeapInner []*timeoutEntry

func (h timeoutHeapInner) Len() int           { return len(h) }
func (h timeoutHeapInner) Less(i, j int) bool { return h[i].deadline.Before(h[j].deadline) }
func (h timeoutHeapInner) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *timeoutHeapInner) Push(x any)        { e := x.(*timeoutEntry); e.index = len(*h); *h = append(*h, e) }
func (h *timeoutHeapInner) Pop() any {
	n := len(*h)
	e := (*h)[n-1]
	(*h)[n-1] = nil
	*h = (*h)[:n-1]
	e.index = -1
	return e
}

type timeoutHeapType struct {
	entries []*timeoutEntry
	mu      sync.Mutex
	cond    sync.Cond
	signal  chan struct{}
}

func (h *timeoutHeapType) push(entry *timeoutEntry) {
	h.mu.Lock()
	wasEmpty := len(h.entries) == 0
	heap.Push((*timeoutHeapInner)(&h.entries), entry)
	shouldSignal := wasEmpty
	if !shouldSignal && len(h.entries) > 0 {
		shouldSignal = entry.deadline.Before(h.entries[0].deadline)
	}
	h.mu.Unlock()
	if shouldSignal {
		select {
		case h.signal <- struct{}{}:
		default:
		}
	}
}

// --- UDP read buffer pool (#1, #8) ---

var udpBufPool = sync.Pool{
	New: func() any { return make([]byte, maxUDPMsgSize) },
}

// --- sendReq type ---

type sendReq struct {
	data []byte
	addr *net.UDPAddr
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":6881"
	}

	var zero NodeID
	explicitNodeID := cfg.NodeID != zero
	var persisted persistedRoutingTable
	if cfg.PersistTo != "" {
		loaded, err := loadRoutingTableFile(cfg.PersistTo)
		if err != nil {
			slog.Default().With("component", "dht").Debug("DHT persistence load skipped", "path", cfg.PersistTo, "err", err)
		} else {
			persisted = loaded
			if !explicitNodeID && persisted.localID != zero {
				cfg.NodeID = persisted.localID
			}
		}
	}
	if cfg.NodeID == zero {
		cfg.NodeID = RandomNodeID()
	}

	secret := make([]byte, tokenSecretSize)
	_, _ = rand.Read(secret)

	var secrets [2][tokenSecretSize]byte
	copy(secrets[0][:], secret)
	copy(secrets[1][:], secret)

	srv := &Server{
		cfg:          cfg,
		rt:           NewRoutingTable(cfg.NodeID),
		txs:          make(map[string]*pendingTx),
		peers:        make(map[[20]byte][]net.Addr),
		peerTokens:   make(map[string]string),
		tokenSecrets: secrets,
		log:          slog.Default().With("component", "dht"),
		th: timeoutHeapType{
			signal: make(chan struct{}, 1),
		},
	}
	srv.sendRB.init()
	srv.th.cond.L = &srv.th.mu

	for _, node := range persisted.nodes {
		srv.rt.AddNode(node)
	}

	srv.pingReply = bencode.AcquireDict()
	srv.pingReply.Set("id", bencode.NewString(string(cfg.NodeID[:])))

	return srv, nil
}

func (s *Server) Run(ctx context.Context) error {
	conn, err := s.bindWithPortShuffle()
	if err != nil {
		return fmt.Errorf("dht: bind: %w", err)
	}
	s.conn = conn
	defer s.conn.Close()

	if s.log != nil {
		s.log.Info("DHT listening", "addr", s.conn.LocalAddr())
	}
	if s.cfg.PersistTo != "" {
		defer s.saveRoutingTable()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopSend := context.AfterFunc(ctx, func() {
		s.sendRB.mu.Lock()
		s.sendRB.cond.Broadcast()
		s.sendRB.mu.Unlock()
	})
	defer stopSend()

	stopTO := context.AfterFunc(ctx, func() {
		s.th.mu.Lock()
		s.th.cond.Broadcast()
		s.th.mu.Unlock()
	})
	defer stopTO()

	go s.sendLoop(ctx)
	go s.readLoop(ctx, cancel)
	go s.periodicRefresh(ctx)
	go s.periodicTokenUpdate(ctx)
	go s.timeoutLoop(ctx)

	if s.log != nil {
		s.log.Info("DHT starting bootstrap")
	}
	s.bootstrap(ctx)

	<-ctx.Done()
	return ctx.Err()
}

func (s *Server) saveRoutingTable() {
	nodes := s.rt.snapshotGoodNodes()
	if err := saveRoutingTableFile(s.cfg.PersistTo, s.cfg.NodeID, nodes); err != nil {
		if s.log != nil {
			s.log.Debug("DHT persistence save failed", "path", s.cfg.PersistTo, "err", err)
		}
		return
	}
	if s.log != nil {
		s.log.Debug("DHT persistence saved", "path", s.cfg.PersistTo, "nodes", len(nodes))
	}
}

func (s *Server) bindWithPortShuffle() (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", s.cfg.Addr)
	if err != nil {
		return nil, err
	}

	if addr.Port != 0 {
		return net.ListenUDP("udp", addr)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// sendLoop uses the ring buffer + sync.Cond instead of a channel (#3).
func (s *Server) sendLoop(ctx context.Context) {
	for {
		req, ok := s.sendRB.dequeue(ctx)
		if !ok {
			return
		}
		_, err := s.conn.WriteToUDP(req.data, req.addr)
		if err != nil && s.log != nil {
			s.log.Debug("DHT send error", "addr", req.addr, "err", err)
		}
	}
}

// timeoutLoop manages a single goroutine with a min-heap of message deadlines (#4).
func (s *Server) timeoutLoop(ctx context.Context) {
	for {
		s.th.mu.Lock()
		for len(s.th.entries) == 0 && ctx.Err() == nil {
			s.th.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-s.th.signal:
			}
			s.th.mu.Lock()
		}
		if ctx.Err() != nil {
			s.th.mu.Unlock()
			return
		}
		nearest := s.th.entries[0].deadline
		s.th.mu.Unlock()

		wait := time.Until(nearest)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-s.th.signal:
			timer.Stop()
			continue
		}

		now := time.Now()
		s.th.mu.Lock()
		for len(s.th.entries) > 0 && !s.th.entries[0].deadline.After(now) {
			ent := heap.Pop((*timeoutHeapInner)(&s.th.entries)).(*timeoutEntry)
			s.th.mu.Unlock()

			if ent.cancelled.Load() {
				s.th.mu.Lock()
				continue
			}

			s.mu.Lock()
			tx, ok := s.txs[ent.txKey]
			delete(s.txs, ent.txKey)
			s.mu.Unlock()
			if ok {
				if tx.target != nil {
					s.rt.onTimeout(tx.target.id, tx.target.ip, tx.target.port)
				}
				if tx.callback != nil {
					tx.callback(nil, fmt.Errorf("dht: query timeout"))
				}
				tx.callback = nil
				tx.target = nil
				tx.timeout = nil
				pendingTxPool.Put(tx)
			}

			s.th.mu.Lock()
		}
		s.th.mu.Unlock()
	}
}

// readLoop uses pooled UDP buffers (#1, #8) and zero-copy parsing.
func (s *Server) readLoop(ctx context.Context, cancel context.CancelFunc) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		buf := udpBufPool.Get().([]byte)
		n, remote, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			udpBufPool.Put(buf)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			if s.log != nil {
				s.log.Error("DHT read error", "err", err)
			}
			continue
		}

		if n > 0 && buf[0] == 'd' {
			s.handlePacket(buf[:n], remote)
		}
		udpBufPool.Put(buf)
	}
}

// handlePacket processes a DHT message with zero-copy parsing (#1).
// The data slice must remain valid for the duration of the call but
// not beyond — Unmarshal copies string data into StringVal.S.
func (s *Server) handlePacket(data []byte, remote *net.UDPAddr) {
	msg, err := Unmarshal(data)
	if err != nil {
		return
	}

	switch msg.Y {
	case "r", "e":
		s.handleResponse(msg, remote)

	case "q":
		if isLocalNodeID(msg, s.cfg.NodeID) {
			return
		}
		s.handleQuery(msg, remote)
	}
}

func isLocalNodeID(msg *Message, localID NodeID) bool {
	if msg.A == nil {
		return false
	}
	idV, ok := msg.A.Get("id")
	if !ok {
		return false
	}
	idStr, ok := idV.(bencode.StringVal)
	if !ok {
		return false
	}
	if len(idStr.S) != NodeIDLength {
		return false
	}
	var id NodeID
	copy(id[:], idStr.S)
	return id == localID
}

func (s *Server) handleResponse(msg *Message, remote *net.UDPAddr) {
	txKey := s.makeTrackingKey(msg.T, remote)
	s.mu.Lock()
	tx, ok := s.txs[txKey]
	delete(s.txs, txKey)
	s.mu.Unlock()

	if !ok {
		return
	}

	if tx.timeout != nil {
		tx.timeout.cancelled.Store(true)
	}

	remoteID := extractNodeID(msg)
	remoteInfo := NodeInfo{ID: remoteID, IP: ipTo4(remote.IP), Port: uint16(remote.Port)}

	if msg.Y == "r" && tx.callback != nil {
		if tx.target != nil {
			var removeID NodeID
			if tx.target.id != remoteInfo.ID && remoteInfo.ID != (NodeID{}) {
				if s.log != nil {
					s.log.Debug("DHT node ID changed in response",
						"old", hex.EncodeToString(tx.target.id[:8]),
						"new", hex.EncodeToString(remoteInfo.ID[:8]))
				}
				removeID = tx.target.id
			}
			s.rt.BatchUpdate(remoteInfo, removeID, tx.target.id, tx.target.ip, tx.target.port)
		} else {
			s.rt.AddGoodNode(remoteInfo)
		}
		tx.callback(msg, nil)
	} else if msg.Y == "e" && tx.callback != nil {
		errMsg := "unknown error"
		if ev, ok := msg.E[1].(string); ok {
			errMsg = ev
		}
		tx.callback(nil, fmt.Errorf("dht: remote error [%v]: %s", msg.E[0], errMsg))
	}

	tx.callback = nil
	tx.target = nil
	tx.timeout = nil
	pendingTxPool.Put(tx)
}

func (s *Server) handleQuery(msg *Message, remote *net.UDPAddr) {
	remoteID := extractNodeID(msg)
	remoteInfo := NodeInfo{ID: remoteID, IP: ipTo4(remote.IP), Port: uint16(remote.Port)}

	if s.log != nil {
		s.log.Debug("DHT query", "method", msg.Q, "remote", remote)
	}

	switch msg.Q {
	case QPing:
		s.handlePing(msg, remoteInfo)
	case QFindNode:
		s.handleFindNode(msg, remoteInfo)
	case QGetPeers:
		s.handleGetPeers(msg, remoteInfo)
	case QAnnouncePeer:
		s.handleAnnouncePeer(msg, remoteInfo)
	default:
		s.sendError(msg.T, 204, "Method Unknown", remote)
	}
}

func (s *Server) handlePing(msg *Message, remote NodeInfo) {
	s.rt.AddGoodNode(remote)
	addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%d.%d.%d.%d:%d",
		remote.IP[0], remote.IP[1], remote.IP[2], remote.IP[3], remote.Port))

	// Use pre-built reply template (#5)
	s.sendResponse(msg.T, s.pingReply, addr)
}

func (s *Server) handleFindNode(msg *Message, remote NodeInfo) {
	s.rt.AddGoodNode(remote)
	addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%d.%d.%d.%d:%d",
		remote.IP[0], remote.IP[1], remote.IP[2], remote.IP[3], remote.Port))

	targetV, ok := msg.A.Get("target")
	if !ok {
		s.sendError(msg.T, 203, "Protocol Error, target missing", addr)
		return
	}
	targetStr, ok := targetV.(bencode.StringVal)
	if !ok || len(targetStr.S) != NodeIDLength {
		s.sendError(msg.T, 203, "Protocol Error, bad target", addr)
		return
	}
	var target NodeID
	copy(target[:], targetStr.S)

	nodes := s.rt.GetClosestNodes(target, bucketK)
	compactdata := CompactNodes(nodes)

	r := bencode.AcquireDict()
	r.Set("id", bencode.NewString(string(s.cfg.NodeID[:])))
	r.Set("nodes", bencode.NewString(string(compactdata)))
	s.sendResponse(msg.T, r, addr)
	bencode.ReleaseDict(r)
}

func (s *Server) handleGetPeers(msg *Message, remote NodeInfo) {
	s.rt.AddGoodNode(remote)
	addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%d.%d.%d.%d:%d",
		remote.IP[0], remote.IP[1], remote.IP[2], remote.IP[3], remote.Port))

	infoHashV, ok := msg.A.Get("info_hash")
	if !ok {
		s.sendError(msg.T, 203, "Protocol Error, info_hash missing", addr)
		return
	}
	infoHashStr, ok := infoHashV.(bencode.StringVal)
	if !ok || len(infoHashStr.S) != 20 {
		s.sendError(msg.T, 203, "Protocol Error, bad info_hash", addr)
		return
	}
	var infoHash [20]byte
	copy(infoHash[:], infoHashStr.S)

	s.peersMu.RLock()
	peerList := s.peers[infoHash]
	s.peersMu.RUnlock()

	r := bencode.AcquireDict()
	r.Set("id", bencode.NewString(string(s.cfg.NodeID[:])))
	r.Set("token", bencode.NewString(s.generateToken(infoHash, addr.IP.String(), uint16(addr.Port))))

	if len(peerList) > 0 {
		compactPeers := compactPeerAddrs(peerList)
		r.Set("values", bencode.NewString(string(compactPeers)))
	} else {
		nodes := s.rt.GetClosestNodes(infoHash, bucketK)
		compactdata := CompactNodes(nodes)
		r.Set("nodes", bencode.NewString(string(compactdata)))
	}

	s.sendResponse(msg.T, r, addr)
	bencode.ReleaseDict(r)
}

func (s *Server) handleAnnouncePeer(msg *Message, remote NodeInfo) {
	s.rt.AddGoodNode(remote)
	addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%d.%d.%d.%d:%d",
		remote.IP[0], remote.IP[1], remote.IP[2], remote.IP[3], remote.Port))

	infoHashV, ok := msg.A.Get("info_hash")
	if !ok {
		s.sendError(msg.T, 203, "Protocol Error, info_hash missing", addr)
		return
	}
	infoHashStr, ok := infoHashV.(bencode.StringVal)
	if !ok || len(infoHashStr.S) != 20 {
		s.sendError(msg.T, 203, "Protocol Error, bad info_hash", addr)
		return
	}

	tokenV, ok := msg.A.Get("token")
	if !ok {
		s.sendError(msg.T, 203, "Protocol Error, token missing", addr)
		return
	}
	tokenStr, ok := tokenV.(bencode.StringVal)
	if !ok {
		s.sendError(msg.T, 203, "Protocol Error, bad token", addr)
		return
	}

	var infoHash [20]byte
	copy(infoHash[:], infoHashStr.S)

	remoteIP := addr.IP.String()
	if !s.validateToken(tokenStr.S, infoHash, remoteIP, uint16(addr.Port)) {
		s.sendError(msg.T, 203, "Protocol Error, bad token", addr)
		return
	}

	var port int
	if portV, portOk := msg.A.Get("port"); portOk {
		if iv, ivOk := portV.(bencode.IntVal); ivOk {
			port = int(iv.I)
		}
	}
	if port == 0 {
		impliedPort := int64(0)
		if pv, pvOk := msg.A.Get("implied_port"); pvOk {
			if iv, ivOk := pv.(bencode.IntVal); ivOk {
				impliedPort = iv.I
			}
		}
		if impliedPort != 0 {
			port = addr.Port
		}
	}
	if port == 0 {
		s.sendError(msg.T, 204, "Protocol Error, port missing", addr)
		return
	}

	peerAddr := &net.TCPAddr{IP: addr.IP, Port: port}

	s.peersMu.Lock()
	existing := s.peers[infoHash]
	found := false
	for _, p := range existing {
		if p.String() == peerAddr.String() {
			found = true
			break
		}
	}
	if !found {
		s.peers[infoHash] = append(existing, peerAddr)
	}
	s.peersMu.Unlock()

	// Use pre-built pingReply template (same content: just id field) (#5)
	s.sendResponse(msg.T, s.pingReply, addr)
}

func (s *Server) generateToken(infoHash [20]byte, ip string, port uint16) string {
	s.tokenSecretsMu.RLock()
	secret := s.tokenSecrets[0]
	s.tokenSecretsMu.RUnlock()
	return s.generateTokenWithSecret(infoHash, ip, port, secret)
}

func (s *Server) generateTokenWithSecret(infoHash [20]byte, ip string, port uint16, secret [tokenSecretSize]byte) string {
	var src [42]byte
	copy(src[:20], infoHash[:])

	parsedIP := net.ParseIP(ip)
	if v4 := parsedIP.To4(); v4 != nil {
		copy(src[20:24], v4)
	} else {
		copy(src[20:36], parsedIP.To16())
	}
	binary.BigEndian.PutUint16(src[36:38], port)
	copy(src[38:42], secret[:])

	h := sha1.Sum(src[:])
	return string(h[:])
}

func (s *Server) validateToken(token string, infoHash [20]byte, ip string, port uint16) bool {
	s.tokenSecretsMu.RLock()
	defer s.tokenSecretsMu.RUnlock()
	for _, secret := range s.tokenSecrets {
		if s.generateTokenWithSecret(infoHash, ip, port, secret) == token {
			return true
		}
	}
	return false
}

func (s *Server) peerTokenKey(infoHash [20]byte, ip string, port uint16) string {
	return fmt.Sprintf("%x:%s:%d", infoHash[:], ip, port)
}

func (s *Server) storePeerToken(infoHash [20]byte, ip string, port uint16, token string) {
	key := s.peerTokenKey(infoHash, ip, port)
	s.peerTokensMu.Lock()
	s.peerTokens[key] = token
	s.peerTokensMu.Unlock()
}

func (s *Server) getPeerToken(infoHash [20]byte, ip string, port uint16) string {
	key := s.peerTokenKey(infoHash, ip, port)
	s.peerTokensMu.RLock()
	tok := s.peerTokens[key]
	s.peerTokensMu.RUnlock()
	return tok
}

func (s *Server) sendResponse(t string, r *bencode.DictVal, addr *net.UDPAddr) {
	msg := NewResponse(t, r)
	msg.V = clientVersion
	data, err := msg.Marshal()
	if err != nil {
		return
	}
	s.sendRB.enqueue(sendReq{data: data, addr: addr})
}

func (s *Server) sendError(t string, code int64, errMsg string, addr *net.UDPAddr) {
	msg := NewError(t, code, errMsg)
	msg.V = clientVersion
	data, err := msg.Marshal()
	if err != nil {
		return
	}
	s.sendRB.enqueue(sendReq{data: data, addr: addr})
}

func (s *Server) sendQuery(ctx context.Context, method string, addr *net.UDPAddr,
	targetID NodeID, extra *bencode.DictVal, callback func(*Message, error)) error {

	txID := s.newTxID()
	args := bencode.NewDict()
	args.Set("id", bencode.NewString(string(s.cfg.NodeID[:])))
	if extra != nil {
		for _, k := range extra.Keys {
			args.Set(k, extra.Values[k])
		}
	}

	msg := NewQuery(method, args)
	msg.T = txID
	msg.V = clientVersion

	data, err := msg.Marshal()
	if err != nil {
		return err
	}

	txKey := s.makeTrackingKey(txID, addr)

	var ip [4]byte
	copy(ip[:], addr.IP.To4())
	rn := &routingNode{id: targetID, ip: ip, port: uint16(addr.Port)}

	tx := pendingTxPool.Get().(*pendingTx)
	tx.target = rn
	tx.txID = txID
	tx.msgType = method
	tx.callback = callback
	tx.deadline = time.Now().Add(messageTimeout)
	tx.createdAt = time.Now()
	tx.timeout = nil

	s.mu.Lock()
	s.txs[txKey] = tx
	s.mu.Unlock()

	if !s.sendRB.enqueue(sendReq{data: data, addr: addr}) {
		s.mu.Lock()
		delete(s.txs, txKey)
		s.mu.Unlock()
		tx.callback = nil
		tx.target = nil
		pendingTxPool.Put(tx)
		return fmt.Errorf("dht: send queue full")
	}

	entry := &timeoutEntry{
		txKey:    txKey,
		deadline: tx.deadline,
	}
	tx.timeout = entry
	s.th.push(entry)

	return nil
}

func (s *Server) makeTrackingKey(txID string, addr *net.UDPAddr) string {
	return fmt.Sprintf("%s:%s:%d", txID, addr.IP.String(), addr.Port)
}

func (s *Server) newTxID() string {
	b := make([]byte, transactionIDLen/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) bootstrap(ctx context.Context) {
	if len(s.cfg.Bootstrap) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, addrStr := range s.cfg.Bootstrap {
		addr, err := net.ResolveUDPAddr("udp", addrStr)
		if err != nil {
			if s.log != nil {
				s.log.Debug("bootstrap: resolve failed", "addr", addrStr, "err", err)
			}
			continue
		}

		wg.Add(1)
		go func(resolved *net.UDPAddr, addrStr string) {
			defer wg.Done()

			sendCtx, cancel := context.WithTimeout(ctx, messageTimeout)
			defer cancel()

			done := make(chan struct{}, 1)
			_ = s.sendQuery(sendCtx, QPing, resolved, RandomNodeID(), nil, func(msg *Message, err error) {
				if err != nil && s.log != nil {
					s.log.Debug("bootstrap ping failed", "addr", addrStr, "err", err)
				}
				if msg != nil && s.log != nil {
					s.log.Debug("bootstrap ping ok", "addr", addrStr)
				}
				done <- struct{}{}
			})

			select {
			case <-done:
			case <-sendCtx.Done():
			case <-ctx.Done():
			}
		}(addr, addrStr)
	}

	wg.Wait()

	if s.log != nil {
		s.log.Info("DHT bootstrap complete")
	}
}

func (s *Server) periodicRefresh(ctx context.Context) {
	ticker := time.NewTicker(bucketRefreshCheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshBuckets(ctx)
		}
	}
}

func (s *Server) refreshBuckets(ctx context.Context) {
	s.rt.mu.RLock()
	buckets := s.rt.allBuckets()
	s.rt.mu.RUnlock()

	for _, b := range buckets {
		if !b.needsRefresh() {
			continue
		}
		b.lastUpdate = time.Now()
		targetID := b.randomNodeID()
		if s.log != nil {
			s.log.Debug("refreshing bucket", "target", hex.EncodeToString(targetID[:]))
		}
		go s.iterativeFindNode(ctx, targetID, func(msg *Message, err error) {})
	}
}

func (s *Server) iterativeFindNode(ctx context.Context, target NodeID, callback func(*Message, error)) {
	const alpha = 3

	seen := make(map[NodeID]bool)
	var mu sync.Mutex
	var pending int
	var wg sync.WaitGroup
	var once sync.Once
	var finalCallback func(*Message, error)

	once.Do(func() {})
	finalCallback = func(msg *Message, err error) {
		once.Do(func() {
			callback(msg, err)
		})
	}

	nodes := s.rt.GetClosestNodes(target, bucketK)
	if len(nodes) == 0 {
		finalCallback(nil, fmt.Errorf("no nodes"))
		return
	}

	for _, n := range nodes {
		seen[n.ID] = true
	}

	for i := 0; i < len(nodes) && i < alpha; i++ {
		node := nodes[i]
		wg.Add(1)
		pending++
		go func(n NodeInfo) {
			defer wg.Done()
			sendCtx, cancel := context.WithTimeout(ctx, messageTimeout)
			defer cancel()

			done := make(chan struct{}, 1)
			s.findNode(sendCtx, target, n, func(msg *Message, err error) {
				if err != nil || msg == nil || msg.R == nil {
					select {
					case done <- struct{}{}:
					default:
					}
					return
				}

				if nodesV, ok := msg.R.Get("nodes"); ok {
					if ns, ok := nodesV.(bencode.StringVal); ok {
						newNodes, decErr := DecodeCompactNodes([]byte(ns.S))
						if decErr == nil {
							mu.Lock()
							for _, nn := range newNodes {
								s.rt.AddNode(nn)
								if !seen[nn.ID] {
									seen[nn.ID] = true
								}
							}
							mu.Unlock()
						}
					}
				}
				select {
				case done <- struct{}{}:
				default:
				}
			})

			select {
			case <-done:
			case <-sendCtx.Done():
			case <-ctx.Done():
			}
		}(node)
	}
	wg.Wait()

	finalCallback(nil, nil)
}

func (s *Server) findNode(ctx context.Context, target NodeID, node NodeInfo, callback func(*Message, error)) {
	addr := &net.UDPAddr{IP: net.IPv4(node.IP[0], node.IP[1], node.IP[2], node.IP[3]), Port: int(node.Port)}
	extra := bencode.NewDict()
	extra.Set("target", bencode.NewString(string(target[:])))
	_ = s.sendQuery(ctx, QFindNode, addr, node.ID, extra, callback)
}

func (s *Server) legacyFindNode(ctx context.Context, target NodeID, callback func(*Message, error)) {
	nodes := s.rt.GetClosestNodes(target, bucketK)
	if len(nodes) == 0 {
		callback(nil, fmt.Errorf("no nodes"))
		return
	}
	node := nodes[0]
	s.findNode(ctx, target, node, callback)
}

// ObservePeerPort injects a BitTorrent peer's advertised DHT port into the
// routing table by pinging the peer, matching aria2's BtPortMessage flow.
func (s *Server) ObservePeerPort(ctx context.Context, ip net.IP, port uint16) {
	ip4 := ip.To4()
	if ip4 == nil || port == 0 || s.conn == nil {
		return
	}

	addr := &net.UDPAddr{IP: ip4, Port: int(port)}
	nodeID := RandomNodeID()
	target := NodeInfo{
		ID:   nodeID,
		IP:   ipTo4(ip4),
		Port: port,
	}

	pingCtx, cancel := context.WithTimeout(ctx, messageTimeout)
	defer cancel()

	_ = s.sendQuery(pingCtx, QPing, addr, target.ID, nil, func(msg *Message, err error) {
		if err != nil || msg == nil {
			return
		}
		if s.rt.NumBuckets() == 1 {
			go s.iterativeFindNode(ctx, s.cfg.NodeID, func(*Message, error) {})
		}
	})
}

func (s *Server) periodicTokenUpdate(ctx context.Context) {
	ticker := time.NewTicker(tokenUpdateInt)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.updateTokenSecret()
		}
	}
}

func (s *Server) updateTokenSecret() {
	s.tokenSecretsMu.Lock()
	s.tokenSecrets[1] = s.tokenSecrets[0]
	_, _ = rand.Read(s.tokenSecrets[0][:])
	s.tokenSecretsMu.Unlock()

	s.peerTokensMu.Lock()
	s.peerTokens = make(map[string]string)
	s.peerTokensMu.Unlock()
}

func (s *Server) GetPeers(ctx context.Context, infoHash [20]byte) (<-chan net.Addr, error) {
	s.peersMu.RLock()
	existing := make([]net.Addr, len(s.peers[infoHash]))
	copy(existing, s.peers[infoHash])
	s.peersMu.RUnlock()

	ch := make(chan net.Addr, 64)

	go func() {
		defer close(ch)

		for _, p := range existing {
			select {
			case <-ctx.Done():
				return
			case ch <- p:
			}
		}

		nodes := s.rt.GetClosestNodes(infoHash, bucketK)
		if len(nodes) == 0 {
			return
		}

		var wg sync.WaitGroup
		for _, node := range nodes {
			wg.Add(1)
			go func(n NodeInfo) {
				defer wg.Done()
				addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%d.%d.%d.%d:%d",
					n.IP[0], n.IP[1], n.IP[2], n.IP[3], n.Port))
				extra := bencode.NewDict()
				extra.Set("info_hash", bencode.NewString(string(infoHash[:])))
				_ = s.sendQuery(ctx, QGetPeers, addr, n.ID, extra, func(msg *Message, err error) {
					if err != nil || msg == nil || msg.R == nil {
						return
					}

					if tv, tokOk := msg.R.Get("token"); tokOk {
						if tsv, tokOk := tv.(bencode.StringVal); tokOk {
							s.storePeerToken(infoHash, addr.IP.String(), uint16(addr.Port), tsv.S)
						}
					}

					if valuesV, ok := msg.R.Get("values"); ok {
						if vs, ok := valuesV.(bencode.StringVal); ok {
							peers := decodeCompactPeers([]byte(vs.S))
							for _, p := range peers {
								select {
								case <-ctx.Done():
									return
								case ch <- p:
								}
							}
						}
					}
					if nodesV, ok := msg.R.Get("nodes"); ok {
						if ns, ok := nodesV.(bencode.StringVal); ok {
							newNodes, err := DecodeCompactNodes([]byte(ns.S))
							if err != nil {
								return
							}
							for _, nn := range newNodes {
								s.rt.AddNode(nn)
							}
						}
					}
				})
			}(node)
		}
		wg.Wait()
	}()

	return ch, nil
}

func (s *Server) Announce(ctx context.Context, infoHash [20]byte, port int) error {
	nodes := s.rt.GetClosestNodes(infoHash, bucketK)
	if len(nodes) == 0 {
		if s.log != nil {
			s.log.Debug("DHT Announce: no nodes in routing table")
		}
		return nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for _, node := range nodes {
		wg.Add(1)
		go func(n NodeInfo) {
			defer wg.Done()

			addr := &net.UDPAddr{IP: net.IPv4(n.IP[0], n.IP[1], n.IP[2], n.IP[3]), Port: int(n.Port)}
			token := s.getPeerToken(infoHash, addr.IP.String(), uint16(addr.Port))

			if token == "" {
				token = s.getPeerTokenByAddr(infoHash, addr.IP.String(), uint16(addr.Port))
			}

			if token == "" {
				getCtx, cancel := context.WithTimeout(ctx, messageTimeout)
				defer cancel()

				getDone := make(chan string, 1)
				getExtra := bencode.NewDict()
				getExtra.Set("info_hash", bencode.NewString(string(infoHash[:])))
				_ = s.sendQuery(getCtx, QGetPeers, addr, n.ID, getExtra, func(msg *Message, err error) {
					if err != nil || msg == nil || msg.R == nil {
						select {
						case getDone <- "":
						default:
						}
						return
					}
					if tv, ok := msg.R.Get("token"); ok {
						if tsv, ok := tv.(bencode.StringVal); ok {
							select {
							case getDone <- tsv.S:
							default:
							}
							return
						}
					}
					select {
					case getDone <- "":
					default:
					}
				})

				select {
				case token = <-getDone:
				case <-getCtx.Done():
					token = ""
				case <-ctx.Done():
					return
				}

				if token == "" {
					mu.Lock()
					errs = append(errs, fmt.Errorf("dht: no token from %s", addr))
					mu.Unlock()
					return
				}
			}

			annDone := make(chan error, 1)
			annExtra := bencode.NewDict()
			annExtra.Set("info_hash", bencode.NewString(string(infoHash[:])))
			annExtra.Set("port", bencode.NewInt(int64(port)))
			annExtra.Set("token", bencode.NewString(token))

			sendCtx, cancel := context.WithTimeout(ctx, messageTimeout)
			defer cancel()

			err := s.sendQuery(sendCtx, QAnnouncePeer, addr, n.ID, annExtra, func(msg *Message, err error) {
				annDone <- err
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}

			select {
			case <-sendCtx.Done():
				mu.Lock()
				errs = append(errs, sendCtx.Err())
				mu.Unlock()
			case <-ctx.Done():
				return
			case e := <-annDone:
				if e != nil {
					mu.Lock()
					errs = append(errs, e)
					mu.Unlock()
				}
			}
		}(node)
	}

	wg.Wait()

	if len(errs) > 0 && len(errs) == len(nodes) {
		return fmt.Errorf("dht: all announces failed")
	}
	return nil
}

func (s *Server) getPeerTokenByAddr(infoHash [20]byte, ip string, port uint16) string {
	s.peerTokensMu.RLock()
	defer s.peerTokensMu.RUnlock()
	for k, v := range s.peerTokens {
		if len(k) > 40 {
			expectedPrefix := fmt.Sprintf("%x:", infoHash[:])
			if k[:len(expectedPrefix)] == expectedPrefix {
				if v != "" {
					return v
				}
			}
		}
	}
	return ""
}

func extractNodeID(msg *Message) NodeID {
	var id NodeID
	if msg.Y == "r" && msg.R != nil {
		if idV, ok := msg.R.Get("id"); ok {
			if sv, ok := idV.(bencode.StringVal); ok && len(sv.S) == NodeIDLength {
				copy(id[:], sv.S)
			}
		}
		return id
	}
	if msg.A != nil {
		if idV, ok := msg.A.Get("id"); ok {
			if sv, ok := idV.(bencode.StringVal); ok && len(sv.S) == NodeIDLength {
				copy(id[:], sv.S)
			}
		}
		return id
	}
	return id
}

func ipTo4(ip net.IP) [4]byte {
	var out [4]byte
	v4 := ip.To4()
	if v4 != nil {
		copy(out[:], v4)
	}
	return out
}

func compactPeerAddrs(addrs []net.Addr) []byte {
	var buf bytes.Buffer
	var portBuf [2]byte
	for _, a := range addrs {
		tcpAddr, ok := a.(*net.TCPAddr)
		if !ok {
			continue
		}
		ip := tcpAddr.IP.To4()
		if ip == nil {
			continue
		}
		buf.Write(ip)
		binary.BigEndian.PutUint16(portBuf[:], uint16(tcpAddr.Port))
		buf.Write(portBuf[:])
	}
	return buf.Bytes()
}

func decodeCompactPeers(data []byte) []net.Addr {
	if len(data)%6 != 0 {
		return nil
	}
	var peers []net.Addr
	for i := 0; i < len(data); i += 6 {
		ip := net.IP(data[i : i+4])
		port := binary.BigEndian.Uint16(data[i+4 : i+6])
		peers = append(peers, &net.TCPAddr{IP: ip, Port: int(port)})
	}
	return peers
}

func MergeBytes(base NodeID, suffix []byte) NodeID {
	var out NodeID
	copy(out[:], base[:])
	copy(out[len(base)-len(suffix):], suffix)
	return out
}

func XORDistance(a, b NodeID) *big.Int {
	return xorDistance(a, b)
}

func ZeroNodeID() NodeID {
	return NodeID{}
}
