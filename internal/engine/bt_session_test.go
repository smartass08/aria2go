package engine

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/mse"
	"github.com/smartass08/aria2go/internal/netx"
	btpeer "github.com/smartass08/aria2go/internal/protocol/bittorrent/peer"
	"github.com/smartass08/aria2go/internal/protocol/bittorrent/utp"
)

func TestNewBtSession(t *testing.T) {
	cfg := &config.Options{
		ListenPort: "6881-6999",
	}
	s := NewBtSession(cfg)
	if s == nil {
		t.Fatal("NewBtSession() returned nil")
	}
	if s.Port() == 0 {
		t.Error("BtSession.Port() returned 0, want non-zero")
	}
	pid := s.PeerID()
	if pid == [20]byte{} {
		t.Error("BtSession.PeerID() returned zero peer ID")
	}
}

func TestBtSession_DefaultPort(t *testing.T) {
	cfg := &config.Options{}
	s := NewBtSession(cfg)
	if s == nil {
		t.Fatal("NewBtSession() returned nil")
	}
	if s.Port() <= 0 || s.Port() > 65535 {
		t.Errorf("BtSession.Port() = %d, want between 1 and 65535", s.Port())
	}
}

func TestBtSession_DHTPort(t *testing.T) {
	cfg := &config.Options{
		ListenPort:    "6881",
		DHTListenPort: "6882",
	}
	s := NewBtSession(cfg)
	if s == nil {
		t.Fatal("NewBtSession() returned nil")
	}
	if s.Port() != 6881 {
		t.Errorf("BtSession.Port() = %d, want 6881", s.Port())
	}
}

func TestBtSession_PeerIDConsistency(t *testing.T) {
	cfg := &config.Options{}
	s := NewBtSession(cfg)
	pid1 := s.PeerID()
	pid2 := s.PeerID()
	if pid1 != pid2 {
		t.Error("PeerID() returned different values on successive calls")
	}
}

func TestBtSessionDialUsesUTP(t *testing.T) {
	listenPort := testFreePort(t)
	cfg := testOpts()
	cfg.ListenPort = fmt.Sprintf("%d", listenPort)

	session := NewBtSession(cfg)
	defer session.Close()
	if err := session.EnsureListening(testLogger(t)); err != nil {
		t.Fatalf("EnsureListening: %v", err)
	}

	dialer, err := netx.NewDialer(engineDialerConfig(cfg))
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}

	serverSock, err := utp.NewSocket("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewSocket: %v", err)
	}
	defer serverSock.Close()

	clientCfg := btpeer.Config{
		InfoHash:    [20]byte{1, 2, 3, 4},
		LocalPeerID: session.PeerID(),
		Reserved:    btpeer.MakeReserved(false, true, false),
		Pieces:      &btPieceSource{adaptor: nil, numPieces: 1},
		PieceLength: 16 * 1024,
		Encrypt:     mse.Off,
		Timeout:     5 * time.Second,
	}
	serverCfg := clientCfg
	serverCfg.LocalPeerID = [20]byte{'S', 'E', 'R', 'V'}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		rawConn, err := serverSock.Accept(ctx)
		if err != nil {
			serverErr <- err
			return
		}
		conn, err := btpeer.Accept(ctx, rawConn, serverCfg)
		if err == nil {
			_ = conn.Close()
		}
		serverErr <- err
	}()

	conn, err := session.Dial(ctx, dialer, serverSock.LocalAddr().String(), clientCfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if err := <-serverErr; err != nil {
		t.Fatalf("server Accept: %v", err)
	}
}

func testFreePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
