package engine

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/netx"
	btpeer "github.com/smartass08/aria2go/internal/protocol/bittorrent/peer"
	"github.com/smartass08/aria2go/internal/protocol/bittorrent/utp"
)

// BtSession holds BitTorrent session-level state shared across downloads.
type BtSession struct {
	port    int
	peerID  [20]byte
	dhtPort int

	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	tcpLn    net.Listener
	utpSock  *utp.Socket
	registry map[[20]byte]*btInboundRegistration
	closed   bool
	wg       sync.WaitGroup
}

type btInboundRegistration struct {
	cfg       btpeer.Config
	C         chan *btpeer.Conn
	mu        sync.Mutex
	closed    bool
	closeFn   func()
	closeOnce sync.Once
}

func (r *btInboundRegistration) deliver(conn *btpeer.Conn) bool {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return false
	}
	ch := r.C
	r.mu.Unlock()

	select {
	case ch <- conn:
		return true
	default:
		return false
	}
}

func (r *btInboundRegistration) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		if !r.closed {
			r.closed = true
			if r.closeFn != nil {
				r.closeFn()
			}
			close(r.C)
		}
		r.mu.Unlock()
	})
	return nil
}

func NewBtSession(cfg *config.Options) *BtSession {
	port := parseListenPort(cfg.ListenPort)
	dhtPort := port
	if cfg.DHTListenPort != "" {
		if p := parseListenPort(cfg.DHTListenPort); p != 0 {
			dhtPort = p
		}
	}

	var peerID [20]byte
	if _, err := rand.Read(peerID[:]); err != nil {
		panic("engine: failed to generate BT peer ID: " + err.Error())
	}

	return &BtSession{
		port:     port,
		peerID:   peerID,
		dhtPort:  dhtPort,
		registry: make(map[[20]byte]*btInboundRegistration),
	}
}

func (s *BtSession) PeerID() [20]byte {
	return s.peerID
}

func (s *BtSession) Port() int {
	return s.port
}

func (s *BtSession) DHTPort() int {
	return s.dhtPort
}

func (s *BtSession) Dial(ctx context.Context, dialer *netx.Dialer, addr string, cfg btpeer.Config) (*btpeer.Conn, error) {
	utpSock := s.currentUTPSocket()
	if utpSock == nil {
		return btpeer.Dial(ctx, dialer, addr, cfg)
	}

	type result struct {
		conn *btpeer.Conn
		err  error
	}

	results := make(chan result, 2)
	done := make(chan struct{})
	defer close(done)

	startDial := func(fn func(context.Context) (*btpeer.Conn, error)) {
		go func() {
			conn, err := fn(ctx)
			select {
			case results <- result{conn: conn, err: err}:
			case <-done:
				if conn != nil {
					_ = conn.Close()
				}
			}
		}()
	}

	started := 1
	startDial(func(ctx context.Context) (*btpeer.Conn, error) {
		return btpeer.Dial(ctx, dialer, addr, cfg)
	})

	if udpAddr, err := net.ResolveUDPAddr("udp", addr); err == nil {
		started++
		startDial(func(ctx context.Context) (*btpeer.Conn, error) {
			return btpeer.DialTransport(ctx, func(ctx context.Context) (net.Conn, error) {
				c, err := utpSock.Dial(ctx, udpAddr)
				if err != nil {
					return nil, fmt.Errorf("peer: dial utp %s: %w", addr, err)
				}
				return c, nil
			}, cfg)
		})
	}

	var errs []error
	for i := 0; i < started; i++ {
		res := <-results
		if res.err == nil {
			return res.conn, nil
		}
		errs = append(errs, res.err)
	}
	if len(errs) == 2 {
		return nil, fmt.Errorf("%v; %w", errs[0], errs[1])
	}
	return nil, errs[0]
}

func (s *BtSession) EnsureListening(log *slog.Logger) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("bt: session closed")
	}
	if s.tcpLn != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	tcpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		cancel()
		return fmt.Errorf("bt: listen tcp %d: %w", s.port, err)
	}

	s.ctx = ctx
	s.cancel = cancel
	s.tcpLn = tcpLn

	utpSock, err := utp.NewSocket(fmt.Sprintf(":%d", s.port))
	if err != nil {
		log.Warn("BT uTP listener unavailable", "port", s.port, "error", err)
	} else {
		s.utpSock = utpSock
	}

	s.wg.Add(1)
	go s.acceptTCP(log, ctx, tcpLn)
	if s.utpSock != nil {
		s.wg.Add(1)
		go s.acceptUTP(log, ctx, s.utpSock)
	}

	return nil
}

func (s *BtSession) Register(cfg btpeer.Config) (*btInboundRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, fmt.Errorf("bt: session closed")
	}
	if _, exists := s.registry[cfg.InfoHash]; exists {
		return nil, fmt.Errorf("bt: inbound registration already exists for infohash")
	}

	reg := &btInboundRegistration{
		cfg: cfg,
		C:   make(chan *btpeer.Conn, 16),
	}
	reg.closeFn = func() {
		s.mu.Lock()
		if s.registry[cfg.InfoHash] == reg {
			delete(s.registry, cfg.InfoHash)
		}
		s.mu.Unlock()
	}
	s.registry[cfg.InfoHash] = reg
	return reg, nil
}

func (s *BtSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cancel := s.cancel
	tcpLn := s.tcpLn
	utpSock := s.utpSock
	registry := s.registry
	s.cancel = nil
	s.ctx = nil
	s.tcpLn = nil
	s.utpSock = nil
	s.registry = make(map[[20]byte]*btInboundRegistration)
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	var firstErr error
	if tcpLn != nil {
		if err := tcpLn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if utpSock != nil {
		if err := utpSock.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, reg := range registry {
		_ = reg.Close()
	}
	s.wg.Wait()
	return firstErr
}

func (s *BtSession) currentUTPSocket() *utp.Socket {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.utpSock
}

func (s *BtSession) snapshotConfigs() map[[20]byte]btpeer.Config {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.registry) == 0 {
		return nil
	}
	snapshot := make(map[[20]byte]btpeer.Config, len(s.registry))
	for infoHash, reg := range s.registry {
		snapshot[infoHash] = reg.cfg
	}
	return snapshot
}

func (s *BtSession) registration(infoHash [20]byte) *btInboundRegistration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registry[infoHash]
}

func (s *BtSession) acceptTCP(log *slog.Logger, ctx context.Context, ln net.Listener) {
	defer s.wg.Done()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Debug("BT inbound TCP accept failed", "error", err)
				continue
			}
		}
		go s.handleIncoming(log, ctx, conn)
	}
}

func (s *BtSession) acceptUTP(log *slog.Logger, ctx context.Context, sock *utp.Socket) {
	defer s.wg.Done()

	for {
		conn, err := sock.Accept(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Debug("BT inbound uTP accept failed", "error", err)
				continue
			}
		}
		go s.handleIncoming(log, ctx, conn)
	}
}

func (s *BtSession) handleIncoming(log *slog.Logger, ctx context.Context, conn net.Conn) {
	configs := s.snapshotConfigs()
	if len(configs) == 0 {
		_ = conn.Close()
		return
	}

	peerConn, infoHash, err := btpeer.AcceptAny(ctx, conn, configs)
	if err != nil {
		log.Debug("BT inbound handshake failed", "remote", conn.RemoteAddr(), "error", err)
		return
	}

	reg := s.registration(infoHash)
	if reg == nil || !reg.deliver(peerConn) {
		_ = peerConn.Close()
	}
}
