package tracker

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/smartass08/aria2go/internal/netx"
)

var (
	// udpRecvPool reuses 4KB receive buffers for UDP tracker responses.
	udpRecvPool = sync.Pool{
		New: func() any {
			buf := make([]byte, maxUDPPacket)
			return &buf
		},
	}
	// announceBufPool reuses 100-byte announce request buffers.
	announceBufPool = sync.Pool{
		New: func() any {
			buf := make([]byte, udpAnnounceReqLen)
			return &buf
		},
	}
	// announceTemplate is a pre-populated 100-byte UDP announce template
	// with the static fields filled. Bytes 8-12 = announceAction,
	// bytes 84-88 = 0 (IP default), bytes 98-100 = 0 (extensions).
	// Dynamic fields are overwritten by buildAnnounceRequest.
	announceTemplate = func() *[udpAnnounceReqLen]byte {
		var t [udpAnnounceReqLen]byte
		binary.BigEndian.PutUint32(t[8:12], uint32(udpAnnounceAction))
		return &t
	}()
)

const (
	udpConnectAction  int32 = 0
	udpAnnounceAction int32 = 1
	udpScrapeAction   int32 = 2
	udpErrorAction    int32 = 3

	udpConnectReqLen  = 16
	udpConnectRespLen = 16
	udpAnnounceReqLen = 100

	udpConnectFirstTimeout  = 5 * time.Second
	udpConnectRetryTimeout  = 10 * time.Second
	udpAnnounceFirstTimeout = 5 * time.Second
	udpAnnounceRetryTimeout = 10 * time.Second

	// Magic constant per BEP 15: initial connection ID.
	udpInitialConnectionID uint64 = 0x41727101980

	// Maximum UDP packet we read.
	maxUDPPacket = 4096
)

// AnnounceUDP sends an announce request to a UDP tracker and returns the
// parsed response. It handles the BEP 15 connection-id handshake
// internally and retries on timeout with increasing backoff (5 s then
// 10 s) matching aria2 behaviour.
func AnnounceUDP(ctx context.Context, urlStr string, req AnnounceRequest, dialer *netx.Dialer) (*AnnounceResponse, error) {
	if err := req.ValidateEvent(); err != nil {
		return nil, err
	}

	addr, err := parseUDPTrackerURL(urlStr)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w", err)
	}

	conn, err := dialer.DialUDP(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w: %w", ErrNetwork, err)
	}
	defer conn.Close()

	remoteAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("tracker: %w: %w", ErrNetwork, err)
	}

	connectionID, err := udpConnect(ctx, conn, remoteAddr)
	if err != nil {
		return nil, err
	}

	return udpAnnounce(ctx, conn, remoteAddr, connectionID, req)
}

func parseUDPTrackerURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid UDP tracker URL %q: %w", urlStr, err)
	}
	if u.Scheme != "udp" {
		return "", fmt.Errorf("expected udp scheme, got %q", u.Scheme)
	}
	if u.Port() == "" {
		u.Host += ":80"
	}
	return u.Host, nil
}

func udpConnect(ctx context.Context, conn *net.UDPConn, remote *net.UDPAddr) (uint64, error) {
	txnID := newTransactionID()

	var buf [udpConnectReqLen]byte
	binary.BigEndian.PutUint64(buf[0:8], udpInitialConnectionID)
	binary.BigEndian.PutUint32(buf[8:12], uint32(udpConnectAction))
	binary.BigEndian.PutUint32(buf[12:16], txnID)

	deadline := udpConnectFirstTimeout

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := conn.WriteTo(buf[:], remote); err != nil {
			return 0, fmt.Errorf("tracker: %w: connect send: %w", ErrNetwork, err)
		}
		slog.Debug("UDPT sent CONNECT",
			"remote", remote.String(),
			"transaction_id", fmt.Sprintf("%08x", txnID))

		if dl, ok := ctx.Deadline(); ok {
			if remaining := time.Until(dl); remaining < deadline {
				deadline = remaining
			}
		}
		if deadline <= 0 {
			return 0, ErrTimeout
		}
		conn.SetReadDeadline(time.Now().Add(deadline))

		resp := *udpRecvPool.Get().(*[]byte)
		n, _, err := conn.ReadFrom(resp)
		if err != nil {
			udpRecvPool.Put(&resp)
			if isTimeout(err) {
				deadline = udpConnectRetryTimeout
				continue
			}
			return 0, fmt.Errorf("tracker: %w: connect read: %w", ErrNetwork, err)
		}

		cid, err := parseConnectResponse(resp[:n], txnID, remote.String())
		udpRecvPool.Put(&resp)
		if err != nil {
			return 0, err
		}
		return cid, nil
	}

	return 0, fmt.Errorf("tracker: %w: connect", ErrTimeout)
}

func parseConnectResponse(data []byte, expectedTxn uint32, remote string) (uint64, error) {
	if len(data) < 8 {
		slog.Info("UDPT received CONNECT reply invalid length",
			"remote", remote,
			"expected", 8, "actual", len(data))
		return 0, fmt.Errorf("%w: connect response too short (%d bytes)", ErrInvalidResp, len(data))
	}

	action := int32(binary.BigEndian.Uint32(data[0:4]))
	txnID := binary.BigEndian.Uint32(data[4:8])

	if txnID != expectedTxn {
		slog.Info("UDPT received CONNECT reply invalid transaction_id",
			"remote", remote,
			"transaction_id", fmt.Sprintf("%08x", txnID))
		return 0, fmt.Errorf("%w: transaction ID mismatch in connect response", ErrInvalidResp)
	}

	if action == udpErrorAction {
		errStr := string(data[8:])
		slog.Info("UDPT received ERROR reply",
			"remote", remote,
			"transaction_id", fmt.Sprintf("%08x", txnID),
			"error", errStr)
		return 0, fmt.Errorf("%w: tracker error: %s", ErrInvalidResp, errStr)
	}

	if len(data) < udpConnectRespLen {
		slog.Info("UDPT received CONNECT reply invalid length",
			"remote", remote,
			"expected", udpConnectRespLen, "actual", len(data))
		return 0, fmt.Errorf("%w: connect response too short (%d bytes)", ErrInvalidResp, len(data))
	}

	if action != udpConnectAction {
		slog.Info("UDPT received unexpected action in CONNECT reply",
			"remote", remote, "action", action)
		return 0, fmt.Errorf("%w: unexpected action %d in connect response", ErrInvalidResp, action)
	}

	connID := binary.BigEndian.Uint64(data[8:16])

	slog.Debug("UDPT received CONNECT reply",
		"remote", remote,
		"transaction_id", fmt.Sprintf("%08x", txnID),
		"connection_id", fmt.Sprintf("%016x", connID))
	return connID, nil
}

func udpAnnounce(ctx context.Context, conn *net.UDPConn, remote *net.UDPAddr, connID uint64, req AnnounceRequest) (*AnnounceResponse, error) {
	txnID := newTransactionID()

	bufPtr := announceBufPool.Get().(*[]byte)
	buf := *bufPtr
	buildAnnounceRequest(buf, connID, txnID, req)

	deadline := udpAnnounceFirstTimeout

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := conn.WriteTo(buf, remote); err != nil {
			announceBufPool.Put(bufPtr)
			return nil, fmt.Errorf("tracker: %w: announce send: %w", ErrNetwork, err)
		}
		slog.Debug("UDPT sent ANNOUNCE",
			"remote", remote.String(),
			"transaction_id", fmt.Sprintf("%08x", txnID),
			"connection_id", fmt.Sprintf("%016x", connID))

		if dl, ok := ctx.Deadline(); ok {
			if remaining := time.Until(dl); remaining < deadline {
				deadline = remaining
			}
		}
		if deadline <= 0 {
			announceBufPool.Put(bufPtr)
			return nil, ErrTimeout
		}
		conn.SetReadDeadline(time.Now().Add(deadline))

		resp := *udpRecvPool.Get().(*[]byte)
		n, _, err := conn.ReadFrom(resp)
		if err != nil {
			udpRecvPool.Put(&resp)
			announceBufPool.Put(bufPtr)
			if isTimeout(err) {
				deadline = udpAnnounceRetryTimeout
				continue
			}
			return nil, fmt.Errorf("tracker: %w: announce read: %w", ErrNetwork, err)
		}

		result, err := parseAnnounceResponse(resp[:n], txnID, remote.String())
		udpRecvPool.Put(&resp)
		announceBufPool.Put(bufPtr)
		return result, err
	}

	announceBufPool.Put(bufPtr)
	return nil, fmt.Errorf("tracker: %w: announce", ErrTimeout)
}

func buildAnnounceRequest(buf []byte, connID uint64, txnID uint32, req AnnounceRequest) {
	// Start with template: action at [8:12], zero IP at [84:88], zero extensions at [98:100].
	copy(buf, announceTemplate[:])
	binary.BigEndian.PutUint64(buf[0:8], connID)
	binary.BigEndian.PutUint32(buf[12:16], txnID)
	copy(buf[16:36], req.InfoHash[:])
	copy(buf[36:56], req.PeerID[:])
	binary.BigEndian.PutUint64(buf[56:64], uint64(req.Downloaded))
	binary.BigEndian.PutUint64(buf[64:72], uint64(req.Left))
	binary.BigEndian.PutUint64(buf[72:80], uint64(req.Uploaded))
	binary.BigEndian.PutUint32(buf[80:84], uint32(eventToInt(req.Event)))
	if ip := net.ParseIP(req.ExternalIP).To4(); ip != nil {
		copy(buf[84:88], ip)
	}
	binary.BigEndian.PutUint32(buf[88:92], udpKey(req.Key))
	numWant := int32(req.NumWant)
	if req.NumWant < 0 {
		numWant = -1
	}
	binary.BigEndian.PutUint32(buf[92:96], uint32(numWant))
	binary.BigEndian.PutUint16(buf[96:98], req.Port)
}

func eventToInt(event string) int32 {
	switch event {
	case "completed":
		return 1
	case "started":
		return 2
	case "stopped":
		return 3
	default:
		return 0 // NONE / regular
	}
}

func udpKey(key string) uint32 {
	if key == "" {
		return 0
	}
	h := uint32(0)
	for _, b := range []byte(key) {
		h = h*31 + uint32(b)
	}
	return h
}

func parseAnnounceResponse(data []byte, expectedTxn uint32, remote string) (*AnnounceResponse, error) {
	if len(data) < 8 {
		slog.Info("UDPT received ANNOUNCE reply length too short",
			"remote", remote, "min", 8, "actual", len(data))
		return nil, fmt.Errorf("%w: announce response too short (%d bytes)", ErrInvalidResp, len(data))
	}

	action := int32(binary.BigEndian.Uint32(data[0:4]))
	txnID := binary.BigEndian.Uint32(data[4:8])

	if txnID != expectedTxn {
		slog.Info("UDPT received ANNOUNCE reply invalid transaction_id",
			"remote", remote,
			"transaction_id", fmt.Sprintf("%08x", txnID))
		return nil, fmt.Errorf("%w: transaction ID mismatch in announce response (action=%d)", ErrInvalidResp, action)
	}

	if action == udpErrorAction {
		errStr := string(data[8:])
		slog.Info("UDPT received ERROR reply",
			"remote", remote,
			"transaction_id", fmt.Sprintf("%08x", txnID),
			"error", errStr)
		return nil, fmt.Errorf("%w: tracker error: %s", ErrInvalidResp, errStr)
	}

	if len(data) < 20 {
		slog.Info("UDPT received ANNOUNCE reply length too short",
			"remote", remote, "min", 20, "actual", len(data))
		return nil, fmt.Errorf("%w: announce response too short (%d bytes)", ErrInvalidResp, len(data))
	}

	if action != udpAnnounceAction {
		slog.Info("UDPT received unexpected action in ANNOUNCE reply",
			"remote", remote, "action", action)
		return nil, fmt.Errorf("%w: unexpected action %d in announce response", ErrInvalidResp, action)
	}

	interval := int32(binary.BigEndian.Uint32(data[8:12]))
	resp := &AnnounceResponse{
		Interval:    interval,
		MinInterval: interval, // UDP has no separate min_interval; C++ sets both from reply interval
		Incomplete:  int32(binary.BigEndian.Uint32(data[12:16])),
		Complete:    int32(binary.BigEndian.Uint32(data[16:20])),
	}

	peers, err := unpackCompactPeers(data[20:], false)
	if err != nil {
		return nil, err
	}
	resp.Peers = peers

	return resp, nil
}

func unpackCompactPeers(data []byte, ipv6 bool) ([]PeerInfo, error) {
	unit := 6
	if ipv6 {
		unit = 18
	}

	if len(data)%unit != 0 {
		return nil, fmt.Errorf("%w: compact peers length %d is not a multiple of %d", ErrInvalidResp, len(data), unit)
	}

	peers := make([]PeerInfo, 0, len(data)/unit)
	for i := 0; i < len(data); i += unit {
		if ipv6 {
			ip := make(net.IP, 16)
			copy(ip, data[i:i+16])
			port := binary.BigEndian.Uint16(data[i+16 : i+18])
			peers = append(peers, PeerInfo{IP: ip, Port: port})
		} else {
			ip := net.IPv4(data[i], data[i+1], data[i+2], data[i+3])
			port := binary.BigEndian.Uint16(data[i+4 : i+6])
			peers = append(peers, PeerInfo{IP: ip, Port: port})
		}
	}
	return peers, nil
}

func newTransactionID() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("tracker: crypto/rand failed: " + err.Error())
	}
	return binary.BigEndian.Uint32(b[:])
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}
