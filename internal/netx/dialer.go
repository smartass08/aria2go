// Package netx provides low-level network dialing with timeout,
// keep-alive, proxy (HTTP CONNECT), interface binding, and IPv4/IPv6
// preference controls.
package netx

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/smartass08/aria2go/internal/platform"
)

// DialerConfig describes how a Dialer establishes connections.
type DialerConfig struct {
	Timeout    time.Duration
	KeepAlive  time.Duration
	Interface  string
	LocalAddr  string
	PreferIPv4 bool
	PreferIPv6 bool
	// DisableIPv6 hard-disables IPv6, including explicit IPv6 literals and
	// async DNS AAAA lookups. Mirrors aria2's --disable-ipv6 option.
	DisableIPv6 bool
	AsyncDNS    bool
	// EnableAsyncDNS6 enables AAAA lookups in the async resolver. It is ignored
	// when AsyncDNS is false, mirroring aria2's runtime behaviour.
	EnableAsyncDNS6 bool
	// AsyncDNSServer configures the DNS server list for AsyncDNS in aria2's
	// comma-separated IP[:port] syntax.
	AsyncDNSServer string
	ProxyURL       string
	ProxyUser      string
	ProxyPass      string
	NoProxy        string
	// SocketRecvBufferSize is the SO_RCVBUF value in bytes. 0 means system default.
	// Mirrors aria2's --socket-recv-buffer-size option.
	SocketRecvBufferSize int
	// DSCP is the Differentiated Services Code Point (0-63). 0 means disabled.
	// Mirrors aria2's --dscp option.
	DSCP int
	// DisableNodelay disables the TCP_NODELAY (Nagle algorithm disabling) option.
	// By default TCP_NODELAY is enabled. Mirrors aria2's --disable-nodelay option.
	DisableNodelay bool
	// Interfaces is a comma-separated list of network interfaces for round-robin
	// binding. Mirrors aria2's --multiple-interface option.
	Interfaces string
}

// Dialer establishes TCP and UDP connections with caller-controlled
// timeouts, keep-alive, proxy tunnelling, and interface binding.
//
// Close does not affect connections already returned — those are owned
// by the caller.
type Dialer struct {
	cfg      DialerConfig
	proxyURL *url.URL
	proxyTLS bool
	resolver *Resolver

	closed bool
	mu     sync.Mutex

	ifaceIdx atomic.Uint32
	ifaces   []string

	// Pre-computed from cfg: DSCP value shifted to TOS byte.
	dscpTOS      int
	dscpSet      bool
	recvBufSize  int
	nodefaultSet bool

	// baseDialer is a template net.Dialer with ControlContext pre-bound.
	// Per-call copies inherit the ControlContext method value without
	// allocating a new closure.
	baseDialer net.Dialer
	reuseKey   string
}

// NewDialer creates a Dialer from cfg.  It validates the configuration
// and returns an error if any field is unsupported or contradictory.
func NewDialer(cfg DialerConfig) (*Dialer, error) {
	d := &Dialer{cfg: cfg}

	if cfg.ProxyURL != "" {
		u, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("netx: invalid proxy URL: %w", err)
		}
		if u.Host == "" {
			return nil, errors.New("netx: proxy URL must include host")
		}
		switch u.Scheme {
		case "http", "https":
		case "socks5", "socks5h":
			return nil, errors.New("netx: SOCKS5 proxy not implemented")
		default:
			return nil, fmt.Errorf("netx: unsupported proxy scheme: %s", u.Scheme)
		}
		if cfg.ProxyUser == "" && u.User != nil {
			cfg.ProxyUser = u.User.Username()
			if pass, ok := u.User.Password(); ok {
				cfg.ProxyPass = pass
			}
		}
		d.proxyURL = u
		d.proxyTLS = u.Scheme == "https"
		d.cfg = cfg
	}

	if cfg.PreferIPv4 && cfg.PreferIPv6 {
		return nil, errors.New("netx: PreferIPv4 and PreferIPv6 are mutually exclusive")
	}
	if cfg.DisableIPv6 && cfg.PreferIPv6 {
		return nil, errors.New("netx: DisableIPv6 and PreferIPv6 are mutually exclusive")
	}

	if cfg.Interface != "" && !platform.Caps().InterfaceBind {
		return nil, fmt.Errorf("netx: interface binding not available on this platform")
	}

	if cfg.DSCP < 0 || cfg.DSCP > 63 {
		return nil, fmt.Errorf("netx: DSCP value must be in range 0-63, got %d", cfg.DSCP)
	}

	if cfg.Interfaces != "" {
		if !platform.Caps().InterfaceBind {
			return nil, fmt.Errorf("netx: interface binding not available on this platform")
		}
		for _, iface := range strings.Split(cfg.Interfaces, ",") {
			iface = strings.TrimSpace(iface)
			if iface == "" {
				return nil, errors.New("netx: Interfaces list contains empty entry")
			}
		}
		d.ifaces = parseInterfaces(cfg.Interfaces)
	}

	// Pre-compute socket options to avoid repeated field reads in the
	// Control callback hot path.
	if cfg.DSCP != 0 {
		d.dscpTOS = cfg.DSCP << 2
		d.dscpSet = true
	}
	d.recvBufSize = cfg.SocketRecvBufferSize
	d.nodefaultSet = cfg.DisableNodelay
	if cfg.AsyncDNS {
		d.resolver = NewResolverWithConfig(ResolverConfig{
			Servers:    cfg.AsyncDNSServer,
			EnableIPv6: asyncResolverIPv6Enabled(cfg),
		})
	}
	d.reuseKey = buildDialerReuseKey(cfg)

	// Bind the control method value once on the template dialer.
	// Per-call copies share this method value — no closure allocation.
	d.baseDialer.ControlContext = d.controlContext

	return d, nil
}

func parseInterfaces(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// DialContext dials a TCP connection.  network must be "tcp", "tcp4", or
// "tcp6".  When ctx has a deadline, it overrides cfg.Timeout if the
// context deadline is sooner.
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, errors.New("netx: dialer is closed")
	}
	d.mu.Unlock()

	if err := d.rejectDisabledIPv6Address(addr); err != nil {
		return nil, err
	}

	if d.proxyURL != nil && !d.proxyBypassed(addr) {
		return d.dialProxy(ctx, network, addr)
	}
	return d.dialDirect(ctx, network, addr)
}

func (d *Dialer) proxyBypassed(addr string) bool {
	if d.cfg.NoProxy == "" {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	for _, raw := range strings.Split(d.cfg.NoProxy, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if noProxyMatch(host, entry) {
			return true
		}
	}
	return false
}

func noProxyMatch(host, entry string) bool {
	if entry == "*" {
		return true
	}
	if strings.Contains(entry, "/") {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return false
		}
		addr, err := netip.ParseAddr(host)
		return err == nil && prefix.Contains(addr)
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	entry = strings.TrimSuffix(strings.ToLower(entry), ".")
	if strings.HasPrefix(entry, ".") {
		return strings.HasSuffix(host, entry)
	}
	return host == entry || strings.HasSuffix(host, "."+entry)
}

// DialUDP dials a UDP connection.  When ctx has a deadline, it caps the
// dial timeout to the remaining duration.
func (d *Dialer) DialUDP(ctx context.Context, addr string) (*net.UDPConn, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, errors.New("netx: dialer is closed")
	}
	d.mu.Unlock()

	if err := d.rejectDisabledIPv6Address(addr); err != nil {
		return nil, err
	}

	network := d.preferredNetwork("udp")

	dl := d.baseDialer // shallow copy
	dl.Timeout = d.dialTimeout(ctx)
	dl.DualStack = d.dualStack()
	if d.cfg.LocalAddr != "" {
		localAddr, err := net.ResolveUDPAddr(network, d.cfg.LocalAddr)
		if err != nil {
			return nil, fmt.Errorf("netx: resolve local UDP addr: %w", err)
		}
		dl.LocalAddr = localAddr
	}
	if !d.cfg.PreferIPv4 && !d.cfg.PreferIPv6 {
		dl.FallbackDelay = 300 * time.Millisecond
	}

	conn, err := d.dialResolved(ctx, &dl, network, addr)
	if err != nil {
		return nil, fmt.Errorf("netx: dial UDP: %w", err)
	}
	uconn, ok := conn.(*net.UDPConn)
	if !ok {
		conn.Close()
		return nil, errors.New("netx: internal error: dial did not return *net.UDPConn")
	}
	return uconn, nil
}

// Close marks the Dialer as closed.  Subsequent calls to DialContext or
// DialUDP return an error.  Already-returned connections are unaffected.
func (d *Dialer) Close() error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func (d *Dialer) dialTimeout(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			if d.cfg.Timeout == 0 || remaining < d.cfg.Timeout {
				return remaining
			}
		}
	}
	return d.cfg.Timeout
}

func (d *Dialer) dialDirect(ctx context.Context, network, addr string) (net.Conn, error) {
	network = d.preferredNetwork(network)
	dl := d.baseDialer // shallow copy — ControlContext method value is shared
	dl.Timeout = d.dialTimeout(ctx)
	dl.KeepAlive = d.cfg.KeepAlive
	dl.DualStack = d.dualStack()
	if d.cfg.LocalAddr != "" {
		localAddr, err := net.ResolveTCPAddr(network, d.cfg.LocalAddr)
		if err != nil {
			return nil, fmt.Errorf("netx: resolve local addr: %w", err)
		}
		dl.LocalAddr = localAddr
	}
	if !d.cfg.PreferIPv4 && !d.cfg.PreferIPv6 {
		dl.FallbackDelay = 300 * time.Millisecond
	}
	conn, err := d.dialResolved(ctx, &dl, network, addr)
	if err != nil {
		return nil, err
	}
	if d.nodefaultSet {
		d.applyPostConnect(conn)
	}
	return conn, nil
}

func (d *Dialer) dualStack() bool {
	return !d.cfg.DisableIPv6 && !d.cfg.PreferIPv4 && !d.cfg.PreferIPv6
}

func (d *Dialer) preferredNetwork(network string) string {
	switch network {
	case "tcp", "udp":
		if d.cfg.DisableIPv6 || d.cfg.PreferIPv4 {
			return network + "4"
		}
		if d.cfg.PreferIPv6 {
			return network + "6"
		}
	}
	return network
}

// controlContext is the method value bound to baseDialer.ControlContext.
// It applies SO_REUSEADDR, SO_RCVBUF, TCP_NODELAY, and DSCP to every
// socket, and optional interface binding with round-robin rotation
// across multiple interfaces.
//
// Non-blocking mode and cloexec are handled internally by Go's net.Dialer.
func (d *Dialer) controlContext(_ context.Context, network string, _ string, c syscall.RawConn) error {
	if err := d.setSocketDefaults(c, network); err != nil {
		return err
	}
	if platform.Caps().InterfaceBind {
		iface := d.nextInterface()
		if iface != "" {
			return bindToDevice(c, iface)
		}
	}
	return nil
}

// nextInterface returns the next interface in the round-robin rotation.
func (d *Dialer) nextInterface() string {
	cfgIface := d.cfg.Interface
	if cfgIface != "" {
		return cfgIface
	}
	if len(d.ifaces) == 0 {
		return ""
	}
	idx := d.ifaceIdx.Add(1) - 1
	return d.ifaces[idx%uint32(len(d.ifaces))]
}

// setSocketDefaults applies SO_REUSEADDR, SO_RCVBUF, TCP_NODELAY, and
// DSCP on every socket. These mirror aria2's SocketCore::create /
// establishConnection behavior.
//
// SO_REUSEADDR and SO_RCVBUF failures are non-fatal: they log a warning
// and continue. This matches aria2 where SO_REUSEADDR failure skips the
// address (not the entire dial), and SO_RCVBUF failure logs a warning.
func (d *Dialer) setSocketDefaults(c syscall.RawConn, network string) error {
	var lastErr error
	ctrlErr := c.Control(func(fd uintptr) {
		if err := setSockOptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
			slog.Debug("netx: SO_REUSEADDR failed", "error", err)
		}

		if d.recvBufSize > 0 {
			if err := setSockOptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, d.recvBufSize); err != nil {
				slog.Warn("netx: failed to set socket buffer size", "size", d.recvBufSize, "error", err)
			}
		}

		if isTCP(network) && !d.nodefaultSet {
			if err := setSockOptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1); err != nil {
				lastErr = err
				return
			}
		}

		if d.dscpSet {
			if err := applyDSCP(int(fd), d.dscpTOS); err != nil {
				slog.Debug("netx: DSCP application failed", "dscp", d.cfg.DSCP, "error", err)
			}
		}
	})
	if ctrlErr != nil {
		return ctrlErr
	}
	return lastErr
}

func (d *Dialer) dialResolved(ctx context.Context, dl *net.Dialer, network, addr string) (net.Conn, error) {
	if d.resolver == nil {
		return dl.DialContext(ctx, network, addr)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return dl.DialContext(ctx, network, addr)
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		ipNetwork, err := d.addressNetworkForIP(network, ip)
		if err != nil {
			return nil, err
		}
		return dl.DialContext(ctx, ipNetwork, net.JoinHostPort(host, port))
	}

	addrs, err := d.resolver.LookupHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("netx: resolve %s: %w", host, err)
	}
	var dialErrs []error
	var blockedErr error
	for _, resolved := range addrs {
		ip := net.ParseIP(resolved)
		if ip == nil {
			continue
		}
		ipNetwork, err := d.addressNetworkForIP(network, ip)
		if err != nil {
			blockedErr = err
			continue
		}
		conn, err := dl.DialContext(ctx, ipNetwork, net.JoinHostPort(resolved, port))
		if err == nil {
			return conn, nil
		}
		dialErrs = append(dialErrs, err)
	}
	if len(dialErrs) == 0 {
		if blockedErr != nil {
			return nil, blockedErr
		}
		return nil, fmt.Errorf("netx: resolve %s returned no dialable addresses", host)
	}
	if len(dialErrs) == 1 {
		return nil, dialErrs[0]
	}
	return nil, errors.Join(dialErrs...)
}

func (d *Dialer) addressNetworkForIP(network string, ip net.IP) (string, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
		if ip.To4() != nil {
			return "tcp4", nil
		}
		if d.cfg.DisableIPv6 {
			return "", fmt.Errorf("netx: IPv6 disabled for address %s", ip.String())
		}
		return "tcp6", nil
	case "udp", "udp4", "udp6":
		if ip.To4() != nil {
			return "udp4", nil
		}
		if d.cfg.DisableIPv6 {
			return "", fmt.Errorf("netx: IPv6 disabled for address %s", ip.String())
		}
		return "udp6", nil
	default:
		return network, nil
	}
}

func isTCP(network string) bool {
	return network == "tcp" || network == "tcp4" || network == "tcp6"
}

// dialProxy establishes a TCP tunnel through an HTTP CONNECT proxy.
func (d *Dialer) dialProxy(ctx context.Context, network, addr string) (net.Conn, error) {
	network = d.preferredNetwork(network)
	host, port, err := net.SplitHostPort(d.proxyURL.Host)
	if err != nil {
		host = d.proxyURL.Host
		if d.proxyTLS {
			port = "443"
		} else {
			port = "80"
		}
	}
	proxyAddr := net.JoinHostPort(host, port)
	if err := d.rejectDisabledIPv6Address(proxyAddr); err != nil {
		return nil, err
	}

	dl := d.baseDialer // shallow copy
	dl.Timeout = d.dialTimeout(ctx)
	dl.KeepAlive = d.cfg.KeepAlive
	dl.DualStack = d.dualStack()
	if d.cfg.LocalAddr != "" {
		localAddr, err := net.ResolveTCPAddr(network, d.cfg.LocalAddr)
		if err != nil {
			return nil, fmt.Errorf("netx: resolve local proxy addr: %w", err)
		}
		dl.LocalAddr = localAddr
	}
	if !d.cfg.PreferIPv4 && !d.cfg.PreferIPv6 {
		dl.FallbackDelay = 300 * time.Millisecond
	}
	proxyNetwork := network
	if !isTCP(proxyNetwork) {
		proxyNetwork = d.preferredNetwork("tcp")
	}
	conn, err := d.dialResolved(ctx, &dl, proxyNetwork, proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("netx: dial proxy %s: %w", proxyAddr, err)
	}

	deadline := d.dialTimeout(ctx)
	if deadline > 0 {
		if err := conn.SetDeadline(time.Now().Add(deadline)); err != nil {
			conn.Close()
			return nil, fmt.Errorf("netx: set proxy deadline: %w", err)
		}
	}
	if d.proxyTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: proxyTLSServerName(host)})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("netx: TLS handshake with proxy %s: %w", proxyAddr, err)
		}
		conn = tlsConn
	}

	var req strings.Builder
	req.Grow(256)
	req.WriteString("CONNECT ")
	req.WriteString(addr)
	req.WriteString(" HTTP/1.1\r\nHost: ")
	req.WriteString(addr)
	req.WriteString("\r\n")
	if d.cfg.ProxyUser != "" {
		req.WriteString("Proxy-Authorization: Basic ")
		req.WriteString(encodeBasicAuth(d.cfg.ProxyUser, d.cfg.ProxyPass))
		req.WriteString("\r\n")
	}
	req.WriteString("\r\n")

	if _, err := conn.Write([]byte(req.String())); err != nil {
		conn.Close()
		return nil, fmt.Errorf("netx: CONNECT write: %w", err)
	}

	br := bufio.NewReader(conn)
	respLine, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("netx: CONNECT response: %w", err)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("netx: clear proxy deadline: %w", err)
	}

	if !strings.HasPrefix(respLine, "HTTP/") {
		conn.Close()
		return nil, fmt.Errorf("netx: invalid CONNECT response: %s", strings.TrimSpace(respLine))
	}

	parts := strings.SplitN(strings.TrimSpace(respLine), " ", 3)
	if len(parts) < 2 {
		conn.Close()
		return nil, fmt.Errorf("netx: malformed CONNECT response: %s", respLine)
	}
	statusCode, err := strconv.Atoi(parts[1])
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("netx: CONNECT response status: %w", err)
	}
	if statusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("netx: proxy CONNECT returned %d", statusCode)
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("netx: reading proxy headers: %w", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	if d.nodefaultSet {
		d.applyPostConnect(conn)
	}
	return proxyTunnelConn(conn, br), nil
}

// ReuseKey returns a stable key for pooling higher-level protocol connections
// that depend on this dialer's network and proxy settings.
func (d *Dialer) ReuseKey() string {
	return d.reuseKey
}

func proxyTLSServerName(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		return ""
	}
	return host
}

func proxyTunnelConn(conn net.Conn, br *bufio.Reader) net.Conn {
	if br.Buffered() == 0 {
		return conn
	}
	return &bufferedConn{Conn: conn, r: br}
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if c.r.Buffered() > 0 {
		return c.r.Read(p)
	}
	return c.Conn.Read(p)
}

// applyPostConnect applies socket options that Go's net.Dialer resets
// after our Control function runs. Currently only TCP_NODELAY=0
// (Nagle enabled) needs this treatment; Go always enables TCP_NODELAY
// after a successful connect.
func (d *Dialer) applyPostConnect(conn net.Conn) {
	if wrapper, ok := conn.(interface{ NetConn() net.Conn }); ok {
		conn = wrapper.NetConn()
	}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return
	}
	rawConn.Control(func(fd uintptr) {
		_ = setSockOptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 0)
	})
}

var basicAuthPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 0, 80)
		return &buf
	},
}

func encodeBasicAuth(user, pass string) string {
	raw := user + ":" + pass
	bufp := basicAuthPool.Get().(*[]byte)
	buf := (*bufp)[:0]
	if cap(buf) < ((len(raw)+2)/3)*4 {
		buf = make([]byte, 0, ((len(raw)+2)/3)*4)
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for i := 0; i < len(raw); i += 3 {
		var b [3]byte
		copy(b[:], raw[i:])
		buf = append(buf,
			alphabet[b[0]>>2],
			alphabet[((b[0]&0x03)<<4)|(b[1]>>4)],
		)
		if i+1 < len(raw) {
			buf = append(buf, alphabet[((b[1]&0x0f)<<2)|(b[2]>>6)])
		} else {
			buf = append(buf, '=')
		}
		if i+2 < len(raw) {
			buf = append(buf, alphabet[b[2]&0x3f])
		} else {
			buf = append(buf, '=')
		}
	}
	result := strings.Clone(string(buf))
	*bufp = buf[:0]
	basicAuthPool.Put(bufp)
	return result
}

var probeIPv6Availability = func() bool {
	conn, err := net.ListenPacket("udp6", "[::1]:0")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func asyncResolverIPv6Enabled(cfg DialerConfig) bool {
	if !cfg.EnableAsyncDNS6 || cfg.PreferIPv4 || cfg.DisableIPv6 {
		return false
	}
	return probeIPv6Availability()
}

func buildDialerReuseKey(cfg DialerConfig) string {
	parts := []string{
		cfg.Interface,
		cfg.LocalAddr,
		strconv.FormatBool(cfg.PreferIPv4),
		strconv.FormatBool(cfg.PreferIPv6),
		strconv.FormatBool(cfg.DisableIPv6),
		strconv.FormatBool(cfg.AsyncDNS),
		strconv.FormatBool(cfg.EnableAsyncDNS6),
		cfg.AsyncDNSServer,
		cfg.ProxyURL,
		cfg.ProxyUser,
		cfg.ProxyPass,
		cfg.NoProxy,
		strconv.Itoa(cfg.SocketRecvBufferSize),
		strconv.Itoa(cfg.DSCP),
		strconv.FormatBool(cfg.DisableNodelay),
		cfg.Interfaces,
	}
	return strings.Join(parts, "\x00")
}

func (d *Dialer) rejectDisabledIPv6Address(addr string) error {
	if !d.cfg.DisableIPv6 {
		return nil
	}
	host := addrHost(addr)
	if host == "" {
		return nil
	}
	ipHost := host
	if i := strings.LastIndex(ipHost, "%"); i != -1 {
		ipHost = ipHost[:i]
	}
	ip := net.ParseIP(ipHost)
	if ip == nil || ip.To4() != nil {
		return nil
	}
	return fmt.Errorf("netx: IPv6 disabled for address %s", host)
}

func addrHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(addr, "[]")
}
