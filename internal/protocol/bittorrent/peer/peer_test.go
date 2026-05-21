package peer

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/mse"
	"github.com/smartass08/aria2go/internal/netx"
)

func testConfig() Config {
	return Config{
		InfoHash:    [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14},
		LocalPeerID: [20]byte{'A', 'G', 'E', 'N', 'T', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		Reserved:    MakeReserved(true, true, false),
		Pieces:      &mockPieceSource{numPieces: 10},
		Encrypt:     mse.Off,
		Timeout:     5 * time.Second,
	}
}

type mockPieceSource struct {
	numPieces int
	have      []bool
}

func (m *mockPieceSource) NumPieces() int   { return m.numPieces }
func (m *mockPieceSource) Have(i int) bool  { return false }
func (m *mockPieceSource) Bitfield() []byte { return nil }

func TestHandshakeMarshalParse(t *testing.T) {
	cfg := testConfig()
	hs := marshalHandshake(cfg.InfoHash, cfg.LocalPeerID, cfg.Reserved)

	if len(hs) != handshakeLen {
		t.Fatalf("handshake length: got %d, want %d", len(hs), handshakeLen)
	}
	if hs[0] != pstrLen {
		t.Fatalf("pstrlen: got %d, want %d", hs[0], pstrLen)
	}
	if string(hs[1:20]) != pstr {
		t.Fatalf("protocol string: got %q, want %q", string(hs[1:20]), pstr)
	}

	parsed, err := parseHandshake(hs[:])
	if err != nil {
		t.Fatalf("parseHandshake: %v", err)
	}
	if parsed.InfoHash != cfg.InfoHash {
		t.Fatalf("info_hash mismatch")
	}
	if parsed.PeerID != cfg.LocalPeerID {
		t.Fatalf("peer_id mismatch")
	}
	if parsed.Reserved != cfg.Reserved {
		t.Fatalf("reserved mismatch")
	}
}

func TestHandshakeReservedFlags(t *testing.T) {
	r := MakeReserved(true, true, true)
	if !hasFastExtension(r) {
		t.Error("expected fast extension flag")
	}
	if !hasExtensionMessaging(r) {
		t.Error("expected extension messaging flag")
	}
	if !hasDHT(r) {
		t.Error("expected DHT flag")
	}

	r = MakeReserved(false, false, false)
	if hasFastExtension(r) {
		t.Error("unexpected fast extension flag")
	}
	if hasExtensionMessaging(r) {
		t.Error("unexpected extension messaging flag")
	}
	if hasDHT(r) {
		t.Error("unexpected DHT flag")
	}
}

func TestMessageEncodeDecode(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{"choke", NewMessage(MsgChoke, nil)},
		{"unchoke", NewMessage(MsgUnchoke, nil)},
		{"interested", NewMessage(MsgInterested, nil)},
		{"not_interested", NewMessage(MsgNotInterested, nil)},
		{"have_all", NewMessage(MsgHaveAll, nil)},
		{"have_none", NewMessage(MsgHaveNone, nil)},
		{"have", NewMessage(MsgHave, []byte{0, 0, 0, 5})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.msg.Encode()
			msg, err := DecodeMessage(data)
			if err != nil {
				t.Fatalf("DecodeMessage: %v", err)
			}
			if msg.ID != tt.msg.ID {
				t.Fatalf("id mismatch: got %d, want %d", msg.ID, tt.msg.ID)
			}
			if len(msg.Payload) != len(tt.msg.Payload) {
				t.Fatalf("payload length mismatch")
			}
		})
	}
}

func TestKeepAliveDecode(t *testing.T) {
	data := KeepAlive()
	if len(data) != 4 {
		t.Fatalf("keepalive length: got %d, want 4", len(data))
	}
	if binary.BigEndian.Uint32(data) != 0 {
		t.Fatalf("keepalive should be zero length")
	}
}

func TestMarshalHelpers(t *testing.T) {
	t.Run("have", func(t *testing.T) {
		data := MarshalHave(42)
		if len(data) != 4+5 { // 4 len prefix + 1 id + 4 payload
			t.Fatalf("Have length: got %d", len(data))
		}
		msg, err := DecodeMessage(data)
		if err != nil {
			t.Fatal(err)
		}
		if msg.ID != MsgHave {
			t.Fatalf("expected MsgHave, got %d", msg.ID)
		}
		piece, err := UnmarshalHave(msg)
		if err != nil {
			t.Fatal(err)
		}
		if piece != 42 {
			t.Fatalf("piece: got %d, want 42", piece)
		}
	})

	t.Run("request", func(t *testing.T) {
		data := MarshalRequest(7, 16384, 16384)
		if len(data) != 4+13 {
			t.Fatalf("Request length: got %d", len(data))
		}
	})

	t.Run("piece", func(t *testing.T) {
		block := []byte("hello world piece data")
		data := MarshalPiece(3, 0, block)
		msg, err := DecodeMessage(data)
		if err != nil {
			t.Fatal(err)
		}
		piece, offset, gotData, err := UnmarshalPiece(msg)
		if err != nil {
			t.Fatal(err)
		}
		if piece != 3 {
			t.Fatalf("piece index: %d", piece)
		}
		if offset != 0 {
			t.Fatalf("offset: %d", offset)
		}
		if string(gotData) != string(block) {
			t.Fatalf("data mismatch")
		}
	})

	t.Run("cancel", func(t *testing.T) {
		data := MarshalCancel(5, 0, 4096)
		msg, err := DecodeMessage(data)
		if err != nil {
			t.Fatal(err)
		}
		piece, offset, length, err := UnmarshalCancel(msg)
		if err != nil {
			t.Fatal(err)
		}
		if piece != 5 || offset != 0 || length != 4096 {
			t.Fatalf("mismatch: piece=%d offset=%d length=%d", piece, offset, length)
		}
	})
}

func TestUnmarshalErrors(t *testing.T) {
	_, _, _, err := UnmarshalPiece(NewMessage(MsgHave, []byte{0, 0, 0, 1}))
	if err == nil {
		t.Error("expected error unmarshaling piece from have message")
	}

	_, err = UnmarshalHave(NewMessage(MsgPiece, nil))
	if err == nil {
		t.Error("expected error unmarshaling have from piece message")
	}

	_, _, _, err = UnmarshalPiece(NewMessage(MsgPiece, []byte{0}))
	if err == nil {
		t.Error("expected error with short payload")
	}
}

func TestDialAcceptHandshake(t *testing.T) {
	cfg := testConfig()
	peerCfg := testConfig()
	peerCfg.LocalPeerID = [20]byte{'P', 'E', 'E', 'R', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	peerCfg.Reserved = MakeReserved(false, true, true)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	var accepted *Conn
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		accepted, aerr = Accept(ctx, c, cfg)
		errCh <- aerr
	}()

	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer dialer.Close()

	initiated, err := Dial(ctx, dialer, ln.Addr().String(), peerCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer initiated.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if accepted == nil {
		t.Fatal("accepted is nil")
	}
	defer accepted.Close()

	if initiated.peerHandshake.InfoHash != cfg.InfoHash {
		t.Error("initiated: peer info hash mismatch")
	}
	if accepted.peerHandshake.InfoHash != peerCfg.InfoHash {
		t.Error("accepted: peer info hash mismatch")
	}
}

func TestDialRejectsMismatchedInfoHash(t *testing.T) {
	cfg := testConfig()
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	serverErr := make(chan error, 1)
	go func() {
		var hs [handshakeLen]byte
		if _, err := io.ReadFull(server, hs[:]); err != nil {
			serverErr <- err
			return
		}
		wrongHash := cfg.InfoHash
		wrongHash[0] ^= 0xff
		resp := marshalHandshake(wrongHash, [20]byte{'P', 'E', 'E', 'R'}, cfg.Reserved)
		_, err := server.Write(resp[:])
		serverErr <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := dialHandshake(ctx, client, cfg)
	if err == nil {
		t.Fatal("expected info hash mismatch")
	}
	if !errors.Is(err, ErrProtocolViolation) {
		t.Fatalf("expected ErrProtocolViolation, got %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestAcceptAllowsPlaintextWhenMSEOptional(t *testing.T) {
	cfg := testConfig()
	cfg.Encrypt = mse.Allow
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()

	acceptErr := make(chan error, 1)
	var accepted *Conn
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		var err error
		accepted, err = Accept(ctx, local, cfg)
		acceptErr <- err
	}()

	remotePeerID := [20]byte{'P', 'L', 'A', 'I', 'N'}
	req := marshalHandshake(cfg.InfoHash, remotePeerID, cfg.Reserved)
	if _, err := remote.Write(req[:]); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	var resp [handshakeLen]byte
	if _, err := io.ReadFull(remote, resp[:]); err != nil {
		t.Fatalf("read response handshake: %v", err)
	}
	parsed, err := parseHandshake(resp[:])
	if err != nil {
		t.Fatalf("parse response handshake: %v", err)
	}
	if parsed.InfoHash != cfg.InfoHash {
		t.Fatal("response info hash mismatch")
	}
	if err := <-acceptErr; err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer accepted.Close()
	if accepted.peerHandshake.PeerID != remotePeerID {
		t.Fatalf("peer id = %q, want %q", accepted.peerHandshake.PeerID, remotePeerID)
	}
}

func TestDialAllowFallsBackToPlainWhenMSERejected(t *testing.T) {
	cfg := testConfig()
	cfg.Encrypt = mse.Allow
	remotePeerID := [20]byte{'P', 'L', 'A', 'I', 'N', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	go func() {
		first, aerr := ln.Accept()
		if aerr != nil {
			serverErr <- aerr
			return
		}
		first.Close()

		second, aerr := ln.Accept()
		if aerr != nil {
			serverErr <- aerr
			return
		}
		defer second.Close()

		var hs [handshakeLen]byte
		if _, aerr := io.ReadFull(second, hs[:]); aerr != nil {
			serverErr <- aerr
			return
		}
		parsed, aerr := parseHandshake(hs[:])
		if aerr != nil {
			serverErr <- aerr
			return
		}
		if parsed.InfoHash != cfg.InfoHash {
			serverErr <- ErrProtocolViolation
			return
		}

		resp := marshalHandshake(cfg.InfoHash, remotePeerID, cfg.Reserved)
		_, aerr = second.Write(resp[:])
		serverErr <- aerr
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer dialer.Close()

	conn, err := Dial(ctx, dialer, ln.Addr().String(), cfg)
	if err != nil {
		t.Fatalf("Dial should fall back to plain: %v", err)
	}
	defer conn.Close()

	if conn.peerHandshake.PeerID != remotePeerID {
		t.Fatalf("peer id = %q, want %q", conn.peerHandshake.PeerID, remotePeerID)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestSendReceiveMessages(t *testing.T) {
	cfg := testConfig()
	peerCfg := testConfig()
	peerCfg.LocalPeerID = [20]byte{'P', 'E', 'E', 'R', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	var accepted *Conn
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		accepted, aerr = Accept(ctx, c, cfg)
		errCh <- aerr
	}()

	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer dialer.Close()

	initiated, err := Dial(ctx, dialer, ln.Addr().String(), peerCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer initiated.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()

	msgCtx, msgCancel := context.WithCancel(ctx)
	defer msgCancel()

	go func() {
		_ = initiated.Run(msgCtx)
	}()
	go func() {
		_ = accepted.Run(msgCtx)
	}()

	if err := initiated.Choke(); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-accepted.Messages():
		if msg.ID != MsgChoke {
			t.Fatalf("expected choke, got id=%d", msg.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for choke message")
	}
}

func TestAllMessageTypes(t *testing.T) {
	cfg := testConfig()
	peerCfg := testConfig()
	peerCfg.LocalPeerID = [20]byte{'P', 'E', 'E', 'R', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	var accepted *Conn
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		accepted, aerr = Accept(ctx, c, cfg)
		errCh <- aerr
	}()

	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer dialer.Close()

	initiated, err := Dial(ctx, dialer, ln.Addr().String(), peerCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer initiated.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()

	msgCtx, msgCancel := context.WithCancel(ctx)
	defer msgCancel()
	go func() { _ = initiated.Run(msgCtx) }()
	go func() { _ = accepted.Run(msgCtx) }()

	messages := []struct {
		name string
		send func(*Conn) error
		id   MessageID
	}{
		{"unchoke", (*Conn).Unchoke, MsgUnchoke},
		{"interested", (*Conn).Interested, MsgInterested},
		{"not_interested", (*Conn).NotInterested, MsgNotInterested},
		{"have", func(c *Conn) error { return c.Have(1) }, MsgHave},
		{"have_all", func(c *Conn) error { return c.HaveAll() }, MsgHaveAll},
		{"have_none", func(c *Conn) error { return c.HaveNone() }, MsgHaveNone},
		{"suggest", func(c *Conn) error { return c.Suggest(5) }, MsgSuggest},
		{"allowed_fast", func(c *Conn) error { return c.AllowedFast(3) }, MsgAllowedFast},
		{"bitfield", func(c *Conn) error { return c.Bitfield([]byte{0x80, 0x00}) }, MsgBitfield},
		{"request", func(c *Conn) error { return c.Request(0, 0, 4096) }, MsgRequest},
		{"cancel", func(c *Conn) error { return c.Cancel(0, 0, 4096) }, MsgCancel},
		{"reject", func(c *Conn) error { return c.Reject(0, 0, 4096) }, MsgReject},
	}

	for _, tt := range messages {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.send(initiated); err != nil {
				t.Fatalf("send %s: %v", tt.name, err)
			}
			select {
			case msg := <-accepted.Messages():
				if msg.ID != tt.id {
					t.Fatalf("expected %s (id=%d), got id=%d", tt.name, tt.id, msg.ID)
				}
			case <-time.After(1 * time.Second):
				t.Fatalf("timeout waiting for %s", tt.name)
			}
		})
	}
}

func TestSnapshot(t *testing.T) {
	cfg := testConfig()
	stats := configToStats(cfg)
	snap := stats.snapshot()
	if snap.Downloaded != 0 {
		t.Error("expected 0 downloaded")
	}
	stats.addDownloaded(100)
	if stats.snapshot().Downloaded != 100 {
		t.Error("expected 100 downloaded")
	}
	stats.addUploaded(50)
	if stats.snapshot().Uploaded != 50 {
		t.Error("expected 50 uploaded")
	}
}

func configToStats(cfg Config) *statStore {
	return &statStore{}
}

func TestPeerChokeUnchokeState(t *testing.T) {
	cfg := testConfig()
	peerCfg := testConfig()
	peerCfg.LocalPeerID = [20]byte{'P', 'E', 'E', 'R', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	var accepted *Conn
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		accepted, aerr = Accept(ctx, c, cfg)
		errCh <- aerr
	}()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	defer dialer.Close()
	initiated, _ := Dial(ctx, dialer, ln.Addr().String(), peerCfg)
	defer initiated.Close()
	<-errCh
	defer accepted.Close()

	msgCtx, msgCancel := context.WithCancel(ctx)
	defer msgCancel()
	go func() { _ = initiated.Run(msgCtx) }()
	go func() { _ = accepted.Run(msgCtx) }()

	// initiated chokes accepted
	initiated.Choke()
	<-accepted.Messages() // consume choke

	snap := accepted.Snapshot()
	if !snap.PeerChoking {
		t.Error("expected peer choking to be true after receiving choke")
	}

	// initiated unchokes accepted
	initiated.Unchoke()
	<-accepted.Messages() // consume unchoke

	snap = accepted.Snapshot()
	if snap.PeerChoking {
		t.Error("expected peer choking to be false after receiving unchoke")
	}
}
