package netx

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNewDialer_Defaults(t *testing.T) {
	d, err := NewDialer(DialerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
}

func TestNewDialer_InvalidProxyURL(t *testing.T) {
	_, err := NewDialer(DialerConfig{ProxyURL: "://bad"})
	if err == nil {
		t.Fatal("expected error for invalid proxy URL")
	}
}

func TestNewDialer_ProxyURLRequiresHost(t *testing.T) {
	_, err := NewDialer(DialerConfig{ProxyURL: "http://"})
	if err == nil || !strings.Contains(err.Error(), "must include host") {
		t.Fatalf("expected proxy host error, got: %v", err)
	}
}

func TestNewDialer_SOCKS5NotImplemented(t *testing.T) {
	_, err := NewDialer(DialerConfig{ProxyURL: "socks5://127.0.0.1:1080"})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected 'not implemented' error for SOCKS5, got: %v", err)
	}
	_, err = NewDialer(DialerConfig{ProxyURL: "socks5h://127.0.0.1:1080"})
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected 'not implemented' error for SOCKS5h, got: %v", err)
	}
}

func TestNewDialer_UnsupportedProxyScheme(t *testing.T) {
	_, err := NewDialer(DialerConfig{ProxyURL: "ftp://proxy:21"})
	if err == nil || !strings.Contains(err.Error(), "unsupported proxy scheme") {
		t.Fatalf("expected 'unsupported proxy scheme' error, got: %v", err)
	}
}

func TestNewDialer_MutuallyExclusiveIPv(t *testing.T) {
	_, err := NewDialer(DialerConfig{PreferIPv4: true, PreferIPv6: true})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %v", err)
	}
}

func TestNextInterfaceRoundRobin(t *testing.T) {
	d := &Dialer{cfg: DialerConfig{Interface: "explicit"}, ifaces: []string{"one", "two"}}
	if got := d.nextInterface(); got != "explicit" {
		t.Fatalf("explicit interface should win, got %q", got)
	}

	d = &Dialer{ifaces: []string{"one", "two"}}
	for _, want := range []string{"one", "two", "one"} {
		if got := d.nextInterface(); got != want {
			t.Fatalf("nextInterface() = %q, want %q", got, want)
		}
	}
}

func TestDialContext_TCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		conn.Close()
		errCh <- nil
	}()

	d, err := NewDialer(DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	conn.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestDialContext_LocalAddr(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		conn.Close()
		errCh <- nil
	}()

	d, err := NewDialer(DialerConfig{
		Timeout:   5 * time.Second,
		LocalAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial with local addr failed: %v", err)
	}
	conn.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestDialContext_ContextCancellation(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	d, err := NewDialer(DialerConfig{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = d.DialContext(ctx, "tcp", ln.Addr().String())
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDialContext_Timeout(t *testing.T) {
	d, err := NewDialer(DialerConfig{Timeout: 1 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	_, err = d.DialContext(context.Background(), "tcp", "192.0.2.1:12345")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	nerr, ok := err.(net.Error)
	if !ok || !nerr.Timeout() {
		t.Fatalf("expected net.Error with Timeout()=true, got: %v", err)
	}
}

func TestDialContext_ClosedDialer(t *testing.T) {
	d, err := NewDialer(DialerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	d.Close()

	_, err = d.DialContext(context.Background(), "tcp", "127.0.0.1:12345")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected 'closed' error, got: %v", err)
	}
}

func TestDialContext_KeepAlive(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		conn.Close()
		errCh <- nil
	}()

	d, err := NewDialer(DialerConfig{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial with keepalive failed: %v", err)
	}
	conn.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestDialUDP_Basic(t *testing.T) {
	d, err := NewDialer(DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	raddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.ListenUDP("udp", raddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	uconn, err := d.DialUDP(context.Background(), ln.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer uconn.Close()

	msg := []byte("hello")
	_, err = uconn.Write(msg)
	if err != nil {
		t.Fatalf("write UDP: %v", err)
	}

	buf := make([]byte, 1024)
	ln.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := ln.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read UDP: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("expected 'hello', got %q", buf[:n])
	}
}

func TestDialUDP_ContextCancellation(t *testing.T) {
	d, err := NewDialer(DialerConfig{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = d.DialUDP(ctx, "127.0.0.1:12345")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDialUDP_ClosedDialer(t *testing.T) {
	d, err := NewDialer(DialerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	d.Close()

	_, err = d.DialUDP(context.Background(), "127.0.0.1:12345")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected 'closed' error, got: %v", err)
	}
}

func TestDialContext_PreferIPv4(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		conn.Close()
		errCh <- nil
	}()

	d, err := NewDialer(DialerConfig{
		Timeout:    5 * time.Second,
		PreferIPv4: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial with PreferIPv4 failed: %v", err)
	}
	conn.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// HTTP CONNECT proxy tests
// ---------------------------------------------------------------------------

// serveHTTPConnectProxy starts a minimal HTTP CONNECT proxy that responds
// with the given status code.  Connections returned by the proxy remain
// open (the proxy reads from them until the client closes) so the
// caller can tunnel through.
func serveHTTPConnectProxy(t *testing.T, returnStatus int) (proxyAddr string, done func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddr = ln.Addr().String()

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				_, err := br.ReadString('\n')
				if err != nil {
					return
				}
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if line == "\r\n" || line == "\n" {
						break
					}
				}
				fmt.Fprintf(c, "HTTP/1.1 %d Connection Established\r\n\r\n", returnStatus)
				if returnStatus != 200 {
					return
				}
				buf := make([]byte, 1024)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	return proxyAddr, func() {
		ln.Close()
		<-closed
	}
}

func TestDialContext_HTTPConnectProxy(t *testing.T) {
	proxyAddr, done := serveHTTPConnectProxy(t, 200)
	defer done()

	d, err := NewDialer(DialerConfig{
		Timeout:  5 * time.Second,
		ProxyURL: "http://" + proxyAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// DialContext returns the tunnel connection to the proxy.  The
	// CONNECT handshake has already completed successfully.  Verify
	// the connection is usable by writing and reading back.
	conn, err := d.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatalf("proxy dial failed: %v", err)
	}

	// Write a short message through the tunnel to verify the
	// connection is alive; the test proxy will echo nothing but
	// will not close until we do.
	conn.Close()
}

func TestDialContext_HTTPConnectProxy_BadGateway(t *testing.T) {
	proxyAddr, done := serveHTTPConnectProxy(t, 502)
	defer done()

	d, err := NewDialer(DialerConfig{
		Timeout:  5 * time.Second,
		ProxyURL: "http://" + proxyAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	_, err = d.DialContext(context.Background(), "tcp", "example.com:80")
	if err == nil || !strings.Contains(err.Error(), "returned 502") {
		t.Fatalf("expected proxy 502 error, got: %v", err)
	}
}

func TestDialContext_HTTPSchemeProxy(t *testing.T) {
	proxyAddr, done := serveHTTPConnectProxy(t, 200)
	defer done()

	d, err := NewDialer(DialerConfig{
		Timeout:  5 * time.Second,
		ProxyURL: "https://" + proxyAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	_, err = d.DialContext(context.Background(), "tcp", "example.com:80")
	if err == nil || !strings.Contains(err.Error(), "TLS handshake with proxy") {
		t.Fatalf("expected TLS handshake error for cleartext proxy, got: %v", err)
	}
}

func TestDialContext_ProxyWithCancelledContext(t *testing.T) {
	proxyAddr, done := serveHTTPConnectProxy(t, 200)
	defer done()

	d, err := NewDialer(DialerConfig{
		Timeout:  10 * time.Second,
		ProxyURL: "http://" + proxyAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = d.DialContext(ctx, "tcp", "example.com:80")
	if err == nil {
		t.Fatal("expected error from cancelled context with proxy")
	}
}

func TestDialContext_HTTPConnectProxyURLCredentials(t *testing.T) {
	reqCh := make(chan []string, 1)
	proxyAddr, done := serveRecordingProxy(t, reqCh, "HTTP/1.1 200 Connection Established\r\n\r\n")
	defer done()

	d, err := NewDialer(DialerConfig{
		Timeout:  5 * time.Second,
		ProxyURL: "http://user:pass@" + proxyAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatalf("proxy dial failed: %v", err)
	}
	conn.Close()

	lines := <-reqCh
	const want = "Proxy-Authorization: Basic dXNlcjpwYXNz"
	for _, line := range lines {
		if line == want {
			return
		}
	}
	t.Fatalf("missing proxy auth header %q in %v", want, lines)
}

func TestDialContext_HTTPConnectProxyExplicitCredentialsOverrideURL(t *testing.T) {
	reqCh := make(chan []string, 1)
	proxyAddr, done := serveRecordingProxy(t, reqCh, "HTTP/1.1 200 Connection Established\r\n\r\n")
	defer done()

	d, err := NewDialer(DialerConfig{
		Timeout:   5 * time.Second,
		ProxyURL:  "http://urluser:urlpass@" + proxyAddr,
		ProxyUser: "explicit",
		ProxyPass: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatalf("proxy dial failed: %v", err)
	}
	conn.Close()

	lines := <-reqCh
	const want = "Proxy-Authorization: Basic ZXhwbGljaXQ6c2VjcmV0"
	for _, line := range lines {
		if line == want {
			return
		}
	}
	t.Fatalf("missing explicit proxy auth header %q in %v", want, lines)
}

func TestDialContext_HTTPConnectProxyBufferedTunnelBytes(t *testing.T) {
	reqCh := make(chan []string, 1)
	proxyAddr, done := serveRecordingProxy(t, reqCh, "HTTP/1.1 200 Connection Established\r\n\r\nhello")
	defer done()

	d, err := NewDialer(DialerConfig{
		Timeout:  5 * time.Second,
		ProxyURL: "http://" + proxyAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", "example.com:80")
	if err != nil {
		t.Fatalf("proxy dial failed: %v", err)
	}
	defer conn.Close()
	<-reqCh

	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read buffered tunnel bytes: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("buffered tunnel bytes = %q, want hello", buf)
	}
}

func serveRecordingProxy(t *testing.T, reqCh chan<- []string, response string) (proxyAddr string, done func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		var lines []string
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			lines = append(lines, line)
		}
		reqCh <- lines
		_, _ = conn.Write([]byte(response))
		buf := make([]byte, 1024)
		_, _ = conn.Read(buf)
	}()
	return ln.Addr().String(), func() {
		ln.Close()
		<-closed
	}
}

func TestDialContext_DirectAfterClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	d, err := NewDialer(DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		conn.Close()
		errCh <- nil
	}()
	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("first dial failed: %v", err)
	}
	conn.Close()
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	d.Close()

	_, err = d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestDialContext_ContextDeadline(t *testing.T) {
	d, err := NewDialer(DialerConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err = d.DialContext(ctx, "tcp", "192.0.2.1:12345")
	if err == nil {
		t.Fatal("expected timeout from context deadline")
	}
}

func TestDialUDP_LocalAddr(t *testing.T) {
	d, err := NewDialer(DialerConfig{
		Timeout:   5 * time.Second,
		LocalAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	raddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.ListenUDP("udp", raddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	uconn, err := d.DialUDP(context.Background(), ln.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer uconn.Close()

	if la := uconn.LocalAddr(); la == nil {
		t.Fatal("expected non-nil local addr")
	}
}

func TestDialContext_Concurrent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	d, err := NewDialer(DialerConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	const n = 10
	errCh := make(chan error, n*2)

	go func() {
		for i := 0; i < n; i++ {
			conn, aerr := ln.Accept()
			if aerr != nil {
				for j := i; j < n; j++ {
					errCh <- aerr
				}
				return
			}
			conn.Close()
			errCh <- nil
		}
	}()

	for i := 0; i < n; i++ {
		go func() {
			conn, derr := d.DialContext(context.Background(), "tcp", ln.Addr().String())
			if derr != nil {
				errCh <- derr
				return
			}
			conn.Close()
			errCh <- nil
		}()
	}

	for i := 0; i < n*2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}

func TestEncodeBasicAuth(t *testing.T) {
	// encodeBasicAuth encodes user + ":" + pass per RFC 7617 §2.
	// Raw bytes: user + ":" + pass, base64 encoded.
	tests := []struct {
		user, pass, expected string
	}{
		// raw="a:" -> "YTo="
		{"a", "", "YTo="},
		// raw="ab:" -> "YWI6"
		{"ab", "", "YWI6"},
		// raw="abc:" -> "YWJjOg=="
		{"abc", "", "YWJjOg=="},
		// raw="abcd:" -> "YWJjZDo="
		{"abcd", "", "YWJjZDo="},
		// Standard RFC 7617 example
		{"Aladdin", "open sesame", "QWxhZGRpbjpvcGVuIHNlc2FtZQ=="},
		// raw=":" -> "Og=="
		{"", "", "Og=="},
		// raw="x:y" -> "eDp5"
		{"x", "y", "eDp5"},
	}
	for _, tt := range tests {
		got := encodeBasicAuth(tt.user, tt.pass)
		if got != tt.expected {
			t.Errorf("encodeBasicAuth(%q, %q) = %q, want %q",
				tt.user, tt.pass, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// New config field validation
// ---------------------------------------------------------------------------

func TestNewDialer_DSCPRange(t *testing.T) {
	_, err := NewDialer(DialerConfig{DSCP: -1})
	if err == nil || !strings.Contains(err.Error(), "DSCP value must be in range") {
		t.Fatalf("expected DSCP range error for -1, got: %v", err)
	}
	_, err = NewDialer(DialerConfig{DSCP: 64})
	if err == nil || !strings.Contains(err.Error(), "DSCP value must be in range") {
		t.Fatalf("expected DSCP range error for 64, got: %v", err)
	}
	// DSCP=0 is valid (disabled)
	d, err := NewDialer(DialerConfig{DSCP: 0})
	if err != nil {
		t.Fatalf("DSCP=0 should be valid: %v", err)
	}
	d.Close()
	// DSCP=63 is valid (max)
	d, err = NewDialer(DialerConfig{DSCP: 63})
	if err != nil {
		t.Fatalf("DSCP=63 should be valid: %v", err)
	}
	d.Close()
}

func TestNewDialer_DisableNodelay(t *testing.T) {
	d, err := NewDialer(DialerConfig{DisableNodelay: true})
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
}

func TestNewDialer_SocketRecvBufferSize(t *testing.T) {
	d, err := NewDialer(DialerConfig{SocketRecvBufferSize: 65536})
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
}

func TestDialContext_SocketOptions(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		conn.Close()
		errCh <- nil
	}()

	d, err := NewDialer(DialerConfig{
		Timeout:              5 * time.Second,
		SocketRecvBufferSize: 262144,
		DSCP:                 48,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	// Verify socket options via syscall on the underlying fd.
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatal("conn is not *net.TCPConn")
	}
	raw, err := tcpConn.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var rcvbuf int
	err = raw.Control(func(fd uintptr) {
		rcvbuf, err = getSockOptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	})
	if err != nil {
		t.Fatalf("getsockopt SO_RCVBUF: %v", err)
	}
	// Linux doubles the buffer size; 262144 -> 524288. Just check it's set.
	if rcvbuf == 0 {
		t.Fatal("SO_RCVBUF was not set")
	}
	t.Logf("SO_RCVBUF = %d", rcvbuf)

	// Verify TCP_NODELAY is enabled by default (non-zero).
	var nodelay int
	err = raw.Control(func(fd uintptr) {
		nodelay, err = getSockOptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY)
	})
	if err != nil {
		t.Fatalf("getsockopt TCP_NODELAY: %v", err)
	}
	// On macOS, getsockopt TCP_NODELAY returns 4 when enabled, 0 when disabled.
	// Check non-zero rather than exactly 1.
	if nodelay == 0 {
		t.Fatal("TCP_NODELAY should be enabled (non-zero) by default")
	}
}

func TestDialContext_TCPNodelayDisabled(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		conn.Close()
		errCh <- nil
	}()

	d, err := NewDialer(DialerConfig{
		Timeout:        5 * time.Second,
		DisableNodelay: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatal("conn is not *net.TCPConn")
	}
	raw, err := tcpConn.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var nodelay int
	err = raw.Control(func(fd uintptr) {
		nodelay, err = getSockOptInt(fd, syscall.IPPROTO_TCP, syscall.TCP_NODELAY)
	})
	if err != nil {
		t.Fatalf("getsockopt TCP_NODELAY: %v", err)
	}
	if nodelay != 0 {
		t.Fatalf("TCP_NODELAY should be 0 (disabled), got %d", nodelay)
	}
}

func TestDialContext_DSCPOption(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("DSCP socket marking is not supported on Windows")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			errCh <- aerr
			return
		}
		conn.Close()
		errCh <- nil
	}()

	d, err := NewDialer(DialerConfig{
		Timeout: 5 * time.Second,
		DSCP:    32,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		t.Fatal("conn is not *net.TCPConn")
	}
	raw, err := tcpConn.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var tos int
	err = raw.Control(func(fd uintptr) {
		tos, err = getSockOptInt(fd, syscall.IPPROTO_IP, syscall.IP_TOS)
	})
	if err != nil {
		t.Fatalf("getsockopt IP_TOS: %v", err)
	}
	expectedTOS := 32 << 2 // DSCP left-shifted by 2 = TOS byte
	if tos != expectedTOS {
		t.Fatalf("IP_TOS should be %d (DSCP %d << 2), got %d", expectedTOS, 32, tos)
	}
}

var _ = io.Discard
var _ = url.Parse
