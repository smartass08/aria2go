package channel

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/smartass08/aria2go/internal/ssh/wire"
)

type mockTransport struct {
	mu       sync.Mutex
	incoming [][]byte
	outgoing [][]byte
	outIdx   int
}

func newMockTransport() *mockTransport {
	return &mockTransport{}
}

func (m *mockTransport) Send(payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.incoming = append(m.incoming, cp)
	return nil
}

func (m *mockTransport) Receive() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.outIdx >= len(m.outgoing) {
		return nil, io.EOF
	}
	p := m.outgoing[m.outIdx]
	m.outIdx++
	return p, nil
}

func (m *mockTransport) queueResponse(payload []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.outgoing = append(m.outgoing, cp)
}

func TestOpenSession_Success(t *testing.T) {
	conn := newMockTransport()

	// Queue SSH_MSG_CHANNEL_OPEN_CONFIRMATION
	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)       // recipient channel (our localID)
	confirm.WriteUint32(42)      // sender channel (peer's ID)
	confirm.WriteUint32(1 << 20) // initial window
	confirm.WriteUint32(32768)   // max packet
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.peersID != 42 {
		t.Errorf("peersID = %d, want 42", ch.peersID)
	}

	// Verify SSH_MSG_CHANNEL_OPEN was sent.
	if len(conn.incoming) != 1 {
		t.Fatalf("expected 1 open packet, got %d", len(conn.incoming))
	}
	p := &wire.Reader{Buf: conn.incoming[0]}
	if p.GetByte() != SSH_MSG_CHANNEL_OPEN {
		t.Fatal("not a channel open packet")
	}
	if ct := p.ReadString(); ct != "session" {
		t.Errorf("channel type = %q, want session", ct)
	}
}

func TestOpenSession_Failure(t *testing.T) {
	conn := newMockTransport()

	fail := wire.NewBuilder()
	fail.PutByte(SSH_MSG_CHANNEL_OPEN_FAILURE)
	fail.WriteUint32(0)
	fail.WriteUint32(1) // SSH_OPEN_ADMINISTRATIVELY_PROHIBITED
	fail.WriteString("nope")
	fail.WriteString("en")
	conn.queueResponse(fail.Payload())

	_, err := OpenSession(conn)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExec(t *testing.T) {
	conn := newMockTransport()

	// Open confirmation
	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(7)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	// Channel request success
	reqSuccess := wire.NewBuilder()
	reqSuccess.PutByte(SSH_MSG_CHANNEL_SUCCESS)
	reqSuccess.WriteUint32(7)
	conn.queueResponse(reqSuccess.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	err = ch.Exec("ls -la")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify EXEC request was sent.
	if len(conn.incoming) < 2 {
		t.Fatalf("expected at least 2 packets, got %d", len(conn.incoming))
	}
	execPkt := conn.incoming[1]
	p := &wire.Reader{Buf: execPkt}
	if p.GetByte() != SSH_MSG_CHANNEL_REQUEST {
		t.Fatal("not a channel request")
	}
	if p.ReadUint32() != 7 {
		t.Error("wrong recipient channel")
	}
	if rt := p.ReadString(); rt != "exec" {
		t.Errorf("request type = %q, want exec", rt)
	}
	if !p.ReadBool() {
		t.Error("want_reply should be true")
	}
	if cmd := p.ReadString(); cmd != "ls -la" {
		t.Errorf("command = %q, want ls -la", cmd)
	}
}

func TestShell(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(1)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	reqSuccess := wire.NewBuilder()
	reqSuccess.PutByte(SSH_MSG_CHANNEL_SUCCESS)
	reqSuccess.WriteUint32(1)
	conn.queueResponse(reqSuccess.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	err = ch.Shell()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubsystem(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(7)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	reqSuccess := wire.NewBuilder()
	reqSuccess.PutByte(SSH_MSG_CHANNEL_SUCCESS)
	reqSuccess.WriteUint32(7)
	conn.queueResponse(reqSuccess.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	err = ch.Subsystem("sftp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.incoming) < 2 {
		t.Fatalf("expected at least 2 packets, got %d", len(conn.incoming))
	}
	subPkt := conn.incoming[1]
	p := &wire.Reader{Buf: subPkt}
	_ = p.GetByte()
	_ = p.ReadUint32()
	if rt := p.ReadString(); rt != "subsystem" {
		t.Errorf("request type = %q, want subsystem", rt)
	}
	_ = p.ReadBool()
	if name := p.ReadString(); name != "sftp" {
		t.Errorf("subsystem name = %q, want sftp", name)
	}
}

func TestReadWrite(t *testing.T) {
	conn := newMockTransport()

	// Open.
	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(3)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	// Queue some data from the server.
	dataPacket := wire.NewBuilder()
	dataPacket.PutByte(SSH_MSG_CHANNEL_DATA)
	dataPacket.WriteUint32(0) // our localID? No — recipient is the peer's channel number.
	// Wait, the data packet has recipient_channel = peer's ID for the channel.
	// In a real implementation, transport handles demultiplexing.
	// For our mock, we just queue a data packet for our channel.
	dataPacket.WriteString("hello")
	conn.queueResponse(dataPacket.Payload())
	conn.queueResponse([]byte{SSH_MSG_CHANNEL_EOF})

	buf := make([]byte, 128)
	n, err := ch.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 || string(buf[:n]) != "hello" {
		t.Errorf("read: got %q, want hello", buf[:n])
	}

	// Write data.
	windowAdjust := wire.NewBuilder()
	windowAdjust.PutByte(SSH_MSG_CHANNEL_WINDOW_ADJUST)
	windowAdjust.WriteUint32(3)
	windowAdjust.WriteUint32(32768)
	conn.queueResponse(windowAdjust.Payload())

	// Reset incoming so we can check what was sent.
	conn.incoming = conn.incoming[:0]
	ch.remoteWindow = 32768

	nw, err := ch.Write([]byte("world"))
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if nw != 5 {
		t.Errorf("wrote %d bytes, want 5", nw)
	}
	if len(conn.incoming) < 1 {
		t.Fatal("no write packet sent")
	}
	wp := &wire.Reader{Buf: conn.incoming[0]}
	if wp.GetByte() != SSH_MSG_CHANNEL_DATA {
		t.Fatal("not a data packet")
	}
	_ = wp.ReadUint32()
	data := wp.ReadString()
	if data != "world" {
		t.Errorf("data = %q, want world", data)
	}
}

func TestClose(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(1)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	conn.incoming = conn.incoming[:0]
	err = ch.Close()
	if err != nil {
		t.Fatalf("close error: %v", err)
	}

	// Should have sent EOF then CLOSE.
	if len(conn.incoming) < 2 {
		t.Fatalf("expected EOF + CLOSE, got %d packets", len(conn.incoming))
	}
	if conn.incoming[0][0] != SSH_MSG_CHANNEL_EOF {
		t.Fatal("first packet not EOF")
	}
	if conn.incoming[1][0] != SSH_MSG_CHANNEL_CLOSE {
		t.Fatal("second packet not CLOSE")
	}

	// Double close should be safe.
	err = ch.Close()
	if err != nil {
		t.Fatalf("double close error: %v", err)
	}
}

func TestWindowAdjust(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(7)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	// Queue a WINDOW_ADJUST from server.
	wa := wire.NewBuilder()
	wa.PutByte(SSH_MSG_CHANNEL_WINDOW_ADJUST)
	wa.WriteUint32(7)
	wa.WriteUint32(65536)
	conn.queueResponse(wa.Payload())

	// Write some data to trigger processing of the window adjust.
	conn.queueResponse([]byte{SSH_MSG_CHANNEL_EOF}) // to prevent blocking on next read

	ch.remoteWindow = 0
	_, _ = ch.Write([]byte("x")) // will try to read window adjust

	if ch.remoteWindow != 65535 {
		t.Errorf("remoteWindow = %d, want 65535 (65536 - 1 written)", ch.remoteWindow)
	}
}

func TestReadEOF(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(1)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	// Queue EOF from server.
	conn.queueResponse([]byte{SSH_MSG_CHANNEL_EOF})

	buf := make([]byte, 128)
	n, err := ch.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected EOF, got err=%v n=%d", err, n)
	}
}

func TestReadClose(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(2)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	// Queue CLOSE from server.
	conn.queueResponse([]byte{SSH_MSG_CHANNEL_CLOSE})

	buf := make([]byte, 128)
	n, err := ch.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected EOF on close, got err=%v n=%d", err, n)
	}

	// Our side should have sent CLOSE in response.
	conn.mu.Lock()
	foundClose := false
	for _, p := range conn.incoming {
		if len(p) > 0 && p[0] == SSH_MSG_CHANNEL_CLOSE {
			foundClose = true
			break
		}
	}
	conn.mu.Unlock()
	if !foundClose {
		t.Fatal("did not send CLOSE in response to peer CLOSE")
	}
}

func TestLargeWriteChunking(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(5)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(1024) // small max packet
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	ch.remoteWindow = 5000
	conn.incoming = conn.incoming[:0]

	data := bytes.Repeat([]byte("x"), 3000)
	n, err := ch.Write(data)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if n != 3000 {
		t.Errorf("wrote %d, want 3000", n)
	}

	// Should have sent at least 3 chunks (3000 / 1024).
	if len(conn.incoming) < 3 {
		t.Errorf("expected at least 3 data packets, got %d", len(conn.incoming))
	}

	for _, p := range conn.incoming {
		if len(p) < 1 || p[0] != SSH_MSG_CHANNEL_DATA {
			t.Fatal("non-data packet in write")
		}
	}
}

func TestExitStatus(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(1)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	if ch.ExitStatus() != nil {
		t.Fatal("exit status should be nil before server sends it")
	}

	// Queue exit-status request from server (RFC 4254 §6.10, want_reply=false).
	exitStatusReq := wire.NewBuilder()
	exitStatusReq.PutByte(SSH_MSG_CHANNEL_REQUEST)
	exitStatusReq.WriteUint32(1)             // recipient = our channel (peer ID)
	exitStatusReq.WriteString("exit-status") // request type
	exitStatusReq.WriteBool(false)           // want_reply
	exitStatusReq.WriteUint32(42)            // exit status
	conn.queueResponse(exitStatusReq.Payload())

	// Also queue EOF so Read doesn't block forever.
	conn.queueResponse([]byte{SSH_MSG_CHANNEL_EOF})

	buf := make([]byte, 128)
	_, err = ch.Read(buf)
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}

	status := ch.ExitStatus()
	if status == nil {
		t.Fatal("exit status should not be nil")
	}
	if *status != 42 {
		t.Errorf("exit status = %d, want 42", *status)
	}
}

func TestWriteOnReceivedClose(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(1)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	// Set rcvdClose directly to simulate peer closing.
	ch.rcvdClose = true

	_, err = ch.Write([]byte("should fail"))
	if err != io.ErrClosedPipe {
		t.Fatalf("expected ErrClosedPipe, got %v", err)
	}
}

func TestWriteStopsOnCloseWhileWaitingForWindow(t *testing.T) {
	conn := newMockTransport()

	confirm := wire.NewBuilder()
	confirm.PutByte(SSH_MSG_CHANNEL_OPEN_CONFIRMATION)
	confirm.WriteUint32(0)
	confirm.WriteUint32(7)
	confirm.WriteUint32(1 << 20)
	confirm.WriteUint32(32768)
	conn.queueResponse(confirm.Payload())

	ch, err := OpenSession(conn)
	if err != nil {
		t.Fatal(err)
	}

	// Peer sends CLOSE while we're waiting for window.
	closePkt := wire.NewBuilder()
	closePkt.PutByte(SSH_MSG_CHANNEL_CLOSE)
	closePkt.WriteUint32(7)
	conn.queueResponse(closePkt.Payload())

	ch.remoteWindow = 0
	_, err = ch.Write([]byte("should fail"))
	if err != io.ErrClosedPipe {
		t.Fatalf("expected ErrClosedPipe, got %v", err)
	}
}
