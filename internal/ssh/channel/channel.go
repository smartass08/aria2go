// Package channel implements the SSH Connection Protocol (RFC 4254).
//
// It provides multiplexed channel management over an authenticated SSH
// transport connection. The primary use-case is opening "session" channels
// for remote command execution ("exec") and subsystem access ("subsystem",
// e.g. "sftp").
//
// Current limitation: localID is always 0, meaning only a single channel
// can be open at a time. Future multi-channel support requires a per-session
// ID allocator and a demultiplexing dispatch loop on the transport receive
// path to route incoming packets to the correct Channel by remote channel ID.
package channel

import (
	"fmt"
	"io"
	"sync"

	"github.com/smartass08/aria2go/internal/core"
	"github.com/smartass08/aria2go/internal/ssh/wire"
)

// TransportConn is the interface the SSH transport layer must satisfy
// for channel multiplexing.
type TransportConn interface {
	Send(payload []byte) error
	Receive() ([]byte, error)
}

// Connection Protocol message codes (RFC 4254 §9).
const (
	SSH_MSG_GLOBAL_REQUEST            = 80
	SSH_MSG_REQUEST_SUCCESS           = 81
	SSH_MSG_REQUEST_FAILURE           = 82
	SSH_MSG_CHANNEL_OPEN              = 90
	SSH_MSG_CHANNEL_OPEN_CONFIRMATION = 91
	SSH_MSG_CHANNEL_OPEN_FAILURE      = 92
	SSH_MSG_CHANNEL_WINDOW_ADJUST     = 93
	SSH_MSG_CHANNEL_DATA              = 94
	SSH_MSG_CHANNEL_EOF               = 96
	SSH_MSG_CHANNEL_CLOSE             = 97
	SSH_MSG_CHANNEL_REQUEST           = 98
	SSH_MSG_CHANNEL_SUCCESS           = 99
	SSH_MSG_CHANNEL_FAILURE           = 100
)

const (
	defaultWindowSize = 2 * 1024 * 1024 // 2 MB (RFC 4254 §5.1)
	defaultMaxPacket  = 32768           // 32 KB (RFC 4253 §6.1)
)

var (
	ErrChannelClosed   = core.NewError(core.ExitNetworkProblem, "SSH channel closed")
	ErrChannelOpenFail = core.NewError(core.ExitNetworkProblem, "SSH channel open failed")
	ErrRequestDenied   = core.NewError(core.ExitNetworkProblem, "SSH channel request denied")
	ErrBadPacket       = core.NewError(core.ExitUnknownError, "bad SSH channel packet")
)

// Channel represents an SSH channel multiplexed over a transport
// connection.
type Channel struct {
	conn TransportConn

	peersID     uint32
	sentEOF     bool
	receivedEOF bool
	sentClose   bool
	rcvdClose   bool

	localWindow     uint32
	remoteWindow    uint32
	maxPacket       uint32
	pendingConsumed uint32
	windowMu        sync.Mutex

	readBuf []byte
	readMu  sync.Mutex

	exitStatus *uint32

	closeOnce sync.Once
	closeErr  error
}

// OpenSession opens a new "session" channel.
func OpenSession(conn TransportConn) (*Channel, error) {
	return Open(conn, "session", nil)
}

// Open opens a new channel of the given type.
//
// Single-channel mode: localID is always 0, so only one channel per
// transport connection is supported currently.
func Open(conn TransportConn, channelType string, extra []byte) (*Channel, error) {
	localID := uint32(0) // single-channel limitation; see package doc

	b := wire.GetBuilder()
	b.PutByte(SSH_MSG_CHANNEL_OPEN)
	b.WriteString(channelType)
	b.WriteUint32(localID)
	b.WriteUint32(defaultWindowSize)
	b.WriteUint32(defaultMaxPacket)
	if len(extra) > 0 {
		b.Buf = append(b.Buf, extra...)
	}

	if err := conn.Send(b.Payload()); err != nil {
		wire.PutBuilder(b)
		return nil, fmt.Errorf("channel: open: %w", err)
	}
	wire.PutBuilder(b)

	resp, err := conn.Receive()
	if err != nil {
		return nil, fmt.Errorf("channel: open response: %w", err)
	}

	switch resp[0] {
	case SSH_MSG_CHANNEL_OPEN_CONFIRMATION:
		return parseChannelOpenConfirmation(resp, localID, conn)
	case SSH_MSG_CHANNEL_OPEN_FAILURE:
		return nil, ErrChannelOpenFail
	default:
		return nil, ErrBadPacket
	}
}

func parseChannelOpenConfirmation(payload []byte, localID uint32, conn TransportConn) (*Channel, error) {
	r := &wire.Reader{Buf: payload}
	_ = r.GetByte()
	recipient := r.ReadUint32()
	if recipient != localID {
		return nil, fmt.Errorf("channel: open confirmation for wrong local id: %d != %d", recipient, localID)
	}
	sender := r.ReadUint32()
	initWindow := r.ReadUint32()
	maxPacket := r.ReadUint32()
	if r.Err != nil {
		return nil, fmt.Errorf("channel: parse open confirmation: %w", r.Err)
	}

	if initWindow == 0 {
		initWindow = defaultWindowSize
	}
	if maxPacket == 0 || maxPacket > defaultMaxPacket {
		maxPacket = defaultMaxPacket
	}

	ch := &Channel{
		conn:         conn,
		peersID:      sender,
		remoteWindow: initWindow,
		localWindow:  defaultWindowSize,
		maxPacket:    maxPacket,
	}
	return ch, nil
}

// Exec runs a command on the remote host (RFC 4254 §6.5).
func (c *Channel) Exec(cmd string) error {
	return c.channelRequest("exec", true, func(b *wire.Builder) {
		b.WriteString(cmd)
	})
}

// Shell requests the user's default shell (RFC 4254 §6.5).
func (c *Channel) Shell() error {
	return c.channelRequest("shell", true, nil)
}

// Subsystem requests a predefined subsystem (e.g. "sftp") (RFC 4254 §6.5).
func (c *Channel) Subsystem(name string) error {
	return c.channelRequest("subsystem", true, func(b *wire.Builder) {
		b.WriteString(name)
	})
}

func (c *Channel) channelRequest(requestType string, wantReply bool, writePayload func(*wire.Builder)) error {
	b := wire.GetBuilder()
	b.PutByte(SSH_MSG_CHANNEL_REQUEST)
	b.WriteUint32(c.peersID)
	b.WriteString(requestType)
	b.WriteBool(wantReply)
	if writePayload != nil {
		writePayload(b)
	}

	if err := c.conn.Send(b.Payload()); err != nil {
		wire.PutBuilder(b)
		return fmt.Errorf("channel: request %q: %w", requestType, err)
	}
	wire.PutBuilder(b)

	if !wantReply {
		return nil
	}

	resp, err := c.conn.Receive()
	if err != nil {
		return fmt.Errorf("channel: request %q response: %w", requestType, err)
	}
	if len(resp) < 1 {
		return ErrBadPacket
	}
	switch resp[0] {
	case SSH_MSG_CHANNEL_SUCCESS:
		return nil
	case SSH_MSG_CHANNEL_FAILURE:
		return ErrRequestDenied
	default:
		return ErrBadPacket
	}
}

// Read reads data from the channel, blocking if necessary.
func (c *Channel) Read(b []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	for len(c.readBuf) == 0 && !c.receivedEOF {
		if c.rcvdClose {
			return 0, io.EOF
		}
		c.readMu.Unlock()
		resp, err := c.conn.Receive()
		c.readMu.Lock()
		if err != nil {
			return 0, fmt.Errorf("channel: read: %w", err)
		}
		if err := c.handleReadPacket(resp); err != nil {
			return 0, err
		}
	}

	if len(c.readBuf) == 0 {
		return 0, io.EOF
	}

	n := copy(b, c.readBuf)
	c.readBuf = c.readBuf[n:]

	c.adjustLocalWindow(uint32(n))

	return n, nil
}

// ExitStatus returns the exit status of the remote command, or nil if
// the server has not yet sent an exit-status request (RFC 4254 §6.10).
func (c *Channel) ExitStatus() *uint32 {
	return c.exitStatus
}

func (c *Channel) adjustLocalWindow(consumed uint32) {
	c.windowMu.Lock()
	c.pendingConsumed += consumed
	if c.pendingConsumed >= defaultWindowSize/2 {
		adjust := c.pendingConsumed
		c.pendingConsumed = 0
		c.localWindow += adjust
		c.windowMu.Unlock()
		_ = c.sendWindowAdjust(adjust)
		return
	}
	c.windowMu.Unlock()
}

func (c *Channel) handleReadPacket(payload []byte) error {
	if len(payload) < 1 {
		return ErrBadPacket
	}
	switch payload[0] {
	case SSH_MSG_CHANNEL_DATA:
		return c.handleData(payload)
	case SSH_MSG_CHANNEL_EOF:
		c.receivedEOF = true
		return nil
	case SSH_MSG_CHANNEL_CLOSE:
		c.rcvdClose = true
		_ = c.sendClose()
		return nil
	case SSH_MSG_CHANNEL_WINDOW_ADJUST:
		return c.handleWindowAdjust(payload)
	case SSH_MSG_CHANNEL_REQUEST:
		return c.handlePeerRequest(payload)
	case SSH_MSG_CHANNEL_SUCCESS, SSH_MSG_CHANNEL_FAILURE:
		return nil
	default:
		return ErrBadPacket
	}
}

func (c *Channel) handleData(payload []byte) error {
	r := &wire.Reader{Buf: payload}
	_ = r.GetByte()
	_ = r.ReadUint32()
	data := r.ReadBytes()
	if r.Err != nil {
		return fmt.Errorf("channel: parse data: %w", r.Err)
	}

	c.windowMu.Lock()
	if uint32(len(data)) > c.localWindow {
		c.windowMu.Unlock()
		return fmt.Errorf("channel: data exceeds local window: %d > %d", len(data), c.localWindow)
	}
	c.localWindow -= uint32(len(data))
	c.windowMu.Unlock()

	c.readBuf = append(c.readBuf, data...)
	return nil
}

func (c *Channel) handleWindowAdjust(payload []byte) error {
	r := &wire.Reader{Buf: payload}
	_ = r.GetByte()
	_ = r.ReadUint32()
	bytesToAdd := r.ReadUint32()
	if r.Err != nil {
		return fmt.Errorf("channel: parse window adjust: %w", r.Err)
	}

	c.windowMu.Lock()
	c.remoteWindow += bytesToAdd
	c.windowMu.Unlock()
	return nil
}

func (c *Channel) handlePeerRequest(payload []byte) error {
	r := &wire.Reader{Buf: payload}
	_ = r.GetByte()
	_ = r.ReadUint32()
	reqType := r.ReadString()
	wantReply := r.ReadBool()
	if r.Err != nil {
		return fmt.Errorf("channel: parse peer request: %w", r.Err)
	}

	switch reqType {
	case "exit-status":
		status := r.ReadUint32()
		if r.Err != nil {
			return fmt.Errorf("channel: parse exit-status: %w", r.Err)
		}
		c.exitStatus = &status
		if wantReply {
			b := wire.GetBuilder()
			b.PutByte(SSH_MSG_CHANNEL_SUCCESS)
			b.WriteUint32(c.peersID)
			err := c.conn.Send(b.Payload())
			wire.PutBuilder(b)
			return err
		}
		return nil
	case "exit-signal":
		_ = r.ReadString() // signal name
		_ = r.ReadBool()   // core dumped
		_ = r.ReadString() // error message
		_ = r.ReadString() // language tag
		if wantReply {
			b := wire.GetBuilder()
			b.PutByte(SSH_MSG_CHANNEL_SUCCESS)
			b.WriteUint32(c.peersID)
			err := c.conn.Send(b.Payload())
			wire.PutBuilder(b)
			return err
		}
		return nil
	default:
		if wantReply {
			b := wire.GetBuilder()
			b.PutByte(SSH_MSG_CHANNEL_FAILURE)
			b.WriteUint32(c.peersID)
			err := c.conn.Send(b.Payload())
			wire.PutBuilder(b)
			return err
		}
		return nil
	}
}

func (c *Channel) sendWindowAdjust(bytes uint32) error {
	b := wire.GetBuilder()
	b.PutByte(SSH_MSG_CHANNEL_WINDOW_ADJUST)
	b.WriteUint32(c.peersID)
	b.WriteUint32(bytes)
	err := c.conn.Send(b.Payload())
	wire.PutBuilder(b)
	return err
}

// Write sends data on the channel, respecting the remote window.
func (c *Channel) Write(data []byte) (int, error) {
	if c.sentEOF || c.sentClose || c.rcvdClose {
		return 0, io.ErrClosedPipe
	}

	total := 0
	for len(data) > 0 {
		c.windowMu.Lock()
		for c.remoteWindow == 0 && !c.sentClose && !c.rcvdClose {
			c.windowMu.Unlock()
			resp, err := c.conn.Receive()
			if err != nil {
				return total, fmt.Errorf("channel: write read: %w", err)
			}
			c.readMu.Lock()
			_ = c.handleReadPacket(resp)
			c.readMu.Unlock()
			c.windowMu.Lock()
		}
		if c.rcvdClose || c.sentClose {
			c.windowMu.Unlock()
			if total > 0 {
				return total, io.ErrClosedPipe
			}
			return 0, io.ErrClosedPipe
		}
		if c.remoteWindow == 0 {
			c.windowMu.Unlock()
			return total, io.ErrClosedPipe
		}

		toSend := uint32(len(data))
		if toSend > c.maxPacket {
			toSend = c.maxPacket
		}
		if toSend > c.remoteWindow {
			toSend = c.remoteWindow
		}

		chunk := data[:toSend]
		data = data[toSend:]
		c.remoteWindow -= toSend

		b := wire.GetBuilder()
		b.PutByte(SSH_MSG_CHANNEL_DATA)
		b.WriteUint32(c.peersID)
		b.WriteString(string(chunk))
		c.windowMu.Unlock()

		if err := c.conn.Send(b.Payload()); err != nil {
			wire.PutBuilder(b)
			return total, fmt.Errorf("channel: write: %w", err)
		}
		wire.PutBuilder(b)
		total += int(toSend)
	}
	return total, nil
}

// SendEOF sends an EOF indication on the channel (RFC 4254 §5.3).
func (c *Channel) SendEOF() error {
	if c.sentEOF {
		return nil
	}
	c.sentEOF = true
	b := wire.GetBuilder()
	b.PutByte(SSH_MSG_CHANNEL_EOF)
	b.WriteUint32(c.peersID)
	err := c.conn.Send(b.Payload())
	wire.PutBuilder(b)
	return err
}

func (c *Channel) sendClose() error {
	if c.sentClose {
		return nil
	}
	c.sentClose = true
	b := wire.GetBuilder()
	b.PutByte(SSH_MSG_CHANNEL_CLOSE)
	b.WriteUint32(c.peersID)
	err := c.conn.Send(b.Payload())
	wire.PutBuilder(b)
	return err
}

// Close gracefully closes the channel.
func (c *Channel) Close() error {
	c.closeOnce.Do(func() {
		_ = c.SendEOF()
		c.closeErr = c.sendClose()
	})
	return c.closeErr
}
