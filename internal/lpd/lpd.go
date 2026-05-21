package lpd

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	multicastAddr4  = "239.192.152.143"
	multicastAddr6  = "ff15::efc0:988f"
	multicastPort   = 6771
	maxUDPMsgSize   = 2048
	readDeadlineGap = 500 * time.Millisecond
	peerChannelCap  = 32
)

var (
	multicastUDPAddr4 = &net.UDPAddr{
		IP:   net.ParseIP(multicastAddr4),
		Port: multicastPort,
	}
	multicastUDPAddr6 = &net.UDPAddr{
		IP:   net.ParseIP(multicastAddr6),
		Port: multicastPort,
	}
	multicastHostV4 = net.JoinHostPort(multicastAddr4, strconv.Itoa(multicastPort))
)

type PeerInfo struct {
	InfoHash [20]byte
	IP       net.IP
	Port     uint16
}

type Listener struct {
	conn   *net.UDPConn
	conn6  *net.UDPConn
	peerCh chan PeerInfo

	closeOnce sync.Once
	closed    chan struct{}
}

func NewListener() (*Listener, error) {
	l := &Listener{
		peerCh: make(chan PeerInfo, peerChannelCap),
		closed: make(chan struct{}),
	}

	conn4, err := listenMulticastUDP("udp4", multicastAddr4)
	if err != nil {
		return nil, fmt.Errorf("lpd: IPv4 multicast listen: %w", err)
	}
	l.conn = conn4

	conn6, _ := listenMulticastUDP("udp6", multicastAddr6)
	l.conn6 = conn6

	return l, nil
}

func listenMulticastUDP(network, addr string) (*net.UDPConn, error) {
	maddr := &net.UDPAddr{
		IP:   net.ParseIP(addr),
		Port: multicastPort,
	}
	conn, err := net.ListenMulticastUDP(network, nil, maddr)
	if err != nil {
		return nil, err
	}
	_ = enableMulticastLoopback(conn)
	return conn, nil
}

func (l *Listener) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	if l.conn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.receiveLoop(ctx, l.conn)
		}()
	}
	if l.conn6 != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.receiveLoop(ctx, l.conn6)
		}()
	}

	wg.Wait()
	return nil
}

func (l *Listener) receiveLoop(ctx context.Context, conn *net.UDPConn) {
	buf := make([]byte, maxUDPMsgSize)
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.closed:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(readDeadlineGap))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			select {
			case <-l.closed:
				return
			default:
			}
			continue
		}

		pi, ok := parseMessage(buf[:n], remote.IP)
		if !ok {
			continue
		}

		select {
		case l.peerCh <- pi:
		case <-ctx.Done():
			return
		case <-l.closed:
			return
		}
	}
}

func (l *Listener) Announce(infoHashes [][20]byte, port uint16) error {
	portStr := strconv.FormatUint(uint64(port), 10)
	var msgBuf [256]byte // large enough for single BT-SEARCH message

	for _, ih := range infoHashes {
		msg := buildRequestBuf(ih, portStr, msgBuf[:0])

		if l.conn != nil {
			l.conn.WriteToUDP(msg, multicastUDPAddr4)
		}
		if l.conn6 != nil {
			l.conn6.WriteToUDP(msg, multicastUDPAddr6)
		}
	}
	return nil
}

func (l *Listener) Peers() <-chan PeerInfo {
	return l.peerCh
}

func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
		if l.conn != nil {
			l.conn.Close()
		}
		if l.conn6 != nil {
			l.conn6.Close()
		}
	})
	return nil
}

const btSearchFrameStart = "BT-SEARCH * HTTP/1.1\r\nHost: "
const btSearchFrameAfterHost = "\r\nPort: "

func buildRequestBuf(infoHash [20]byte, portStr string, dst []byte) []byte {
	dst = append(dst, btSearchFrameStart...)
	dst = append(dst, multicastHostV4...)
	dst = append(dst, btSearchFrameAfterHost...)
	dst = append(dst, portStr...)
	dst = append(dst, "\r\nInfohash: "...)
	dst = appendHexUpper(dst, infoHash[:])
	dst = append(dst, "\r\n\r\n"...)
	return dst
}

func buildRequest(infoHash [20]byte, port uint16) []byte {
	portStr := strconv.FormatUint(uint64(port), 10)
	return buildRequestBuf(infoHash, portStr, make([]byte, 0, 256))
}

func appendHexUpper(dst []byte, src []byte) []byte {
	const hexTable = "0123456789ABCDEF"
	for _, b := range src {
		dst = append(dst, hexTable[b>>4], hexTable[b&0x0F])
	}
	return dst
}

func parseMessage(data []byte, srcIP net.IP) (PeerInfo, bool) {
	var (
		port  uint16
		ihHex []byte
	)

	rest := data
	for len(rest) > 0 {
		// Find \r\n
		crlf := -1
		for i := 0; i < len(rest)-1; i++ {
			if rest[i] == '\r' && rest[i+1] == '\n' {
				crlf = i
				break
			}
		}
		var line []byte
		if crlf < 0 {
			line = rest
			rest = nil
		} else {
			line = rest[:crlf]
			rest = rest[crlf+2:]
			if len(line) == 0 {
				break
			}
		}

		if len(line) == 0 {
			break
		}

		colon := -1
		for i := 0; i < len(line); i++ {
			if line[i] == ':' {
				colon = i
				break
			}
		}
		if colon < 0 {
			continue
		}

		key := line[:colon]
		val := line[colon+1:]

		// Trim leading/trailing whitespace from val.
		for len(val) > 0 && (val[0] == ' ' || val[0] == '\t') {
			val = val[1:]
		}
		for len(val) > 0 && (val[len(val)-1] == ' ' || val[len(val)-1] == '\t') {
			val = val[:len(val)-1]
		}

		switch {
		case bytesEqFold(key, []byte("infohash")):
			ihHex = val
		case bytesEqFold(key, []byte("port")):
			if len(val) == 0 {
				return PeerInfo{}, false
			}
			p := parseUint16Bytes(val)
			if p == 0 || p > math.MaxUint16 {
				return PeerInfo{}, false
			}
			port = uint16(p)
		}
	}

	if port == 0 || len(ihHex) != 40 {
		return PeerInfo{}, false
	}

	var ih [20]byte
	if _, err := hex.Decode(ih[:], ihHex); err != nil {
		return PeerInfo{}, false
	}

	return PeerInfo{
		InfoHash: ih,
		IP:       cloneIP(srcIP),
		Port:     port,
	}, true
}

func bytesEqFold(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func parseUint16Bytes(b []byte) uint64 {
	var n uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}

func cloneIP(ip net.IP) net.IP {
	if len(ip) == 0 {
		return nil
	}
	c := make(net.IP, len(ip))
	copy(c, ip)
	return c
}
