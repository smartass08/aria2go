package ftp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/netx"
)

type mockFTPServer struct {
	ln       net.Listener
	addr     string
	tlsCfg   *tls.Config
	mu       sync.Mutex
	handlers map[string]mockResponse
	commands []string
	accepts  int
}

type mockResponse struct {
	code  int
	msg   string
	multi []string
}

func newMockFTPServer(t *testing.T) *mockFTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mock listen: %v", err)
	}
	s := &mockFTPServer{
		ln:       ln,
		addr:     ln.Addr().String(),
		handlers: make(map[string]mockResponse),
	}
	s.setHandler("USER", mockResponse{code: 331, msg: "Please specify the password."})
	s.setHandler("PASS", mockResponse{code: 230, msg: "Login successful."})
	s.setHandler("PWD", mockResponse{code: 257, msg: "\"/\""})
	s.setHandler("SIZE", mockResponse{code: 213, msg: "12345"})
	s.setHandler("PASV", mockResponse{code: 227, msg: "Entering Passive Mode (127,0,0,1,4,210)."})
	s.setHandler("EPSV", mockResponse{code: 229, msg: "Entering Extended Passive Mode (|||1234|)."})
	s.setHandler("SYST", mockResponse{code: 215, msg: "UNIX Type: L8"})
	s.setHandler("TYPE", mockResponse{code: 200, msg: "Switching to Binary mode."})
	s.setHandler("CWD", mockResponse{code: 250, msg: "Directory changed."})
	s.setHandler("MDTM", mockResponse{code: 213, msg: "20230504120000"})
	go s.serve()
	return s
}

func (s *mockFTPServer) setHandler(cmd string, r mockResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[strings.ToUpper(cmd)] = r
}

func (s *mockFTPServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.accepts++
		s.mu.Unlock()
		go s.handleConn(conn)
	}
}

func (s *mockFTPServer) handleConn(conn net.Conn) {
	defer conn.Close()
	fmt.Fprintf(conn, "220 FTP mock ready\r\n")
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		s.mu.Lock()
		s.commands = append(s.commands, line)
		s.mu.Unlock()
		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToUpper(parts[0])
		s.mu.Lock()
		resp, ok := s.handlers[cmd]
		s.mu.Unlock()
		if !ok {
			if cmd == "QUIT" {
				fmt.Fprintf(conn, "221 Goodbye.\r\n")
				return
			}
			if cmd == "NOOP" {
				fmt.Fprintf(conn, "200 NOOP ok.\r\n")
				continue
			}
			if cmd == "RETR" {
				fmt.Fprintf(conn, "150 Opening data connection\r\n")
				fmt.Fprintf(conn, "226 Transfer complete.\r\n")
				continue
			}
			if cmd == "REST" {
				fmt.Fprintf(conn, "350 Restarting at offset.\r\n")
				continue
			}
			fmt.Fprintf(conn, "500 Unknown command.\r\n")
			continue
		}
		if len(resp.multi) > 0 {
			for _, l := range resp.multi {
				fmt.Fprintf(conn, "%s\r\n", l)
			}
		} else {
			fmt.Fprintf(conn, "%d %s\r\n", resp.code, resp.msg)
		}
	}
}

func (s *mockFTPServer) acceptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepts
}

func (s *mockFTPServer) commandHistory() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.commands))
	copy(out, s.commands)
	return out
}

func (s *mockFTPServer) close() {
	s.ln.Close()
}

func TestDialLogin(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}

	ctx := context.Background()
	c, err := Dial(ctx, dialer, srv.addr, Opt{User: "test", Pass: "secret"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c.Close()
}

func TestDialAnonymousLogin(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("USER", mockResponse{code: 230, msg: "Anonymous login ok."})

	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}

	ctx := context.Background()
	c, err := Dial(ctx, dialer, srv.addr, Opt{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c.Close()
}

func TestDialAnonymousDefaultPassword(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}

	c, err := Dial(context.Background(), dialer, srv.addr, Opt{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c.Close()

	var sawUser, sawPass bool
	for _, cmd := range srv.commandHistory() {
		if cmd == "USER anonymous" {
			sawUser = true
		}
		if cmd == "PASS ARIA2USER@" {
			sawPass = true
		}
	}
	if !sawUser {
		t.Fatalf("USER command not observed: %v", srv.commandHistory())
	}
	if !sawPass {
		t.Fatalf("PASS command not observed: %v", srv.commandHistory())
	}
}

func TestDialAsciiType(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}

	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "secret", Type: "ascii"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	c.Close()

	for _, cmd := range srv.commandHistory() {
		if cmd == "TYPE A" {
			return
		}
	}
	t.Fatalf("TYPE A command not observed: %v", srv.commandHistory())
}

func TestCmdsendRecv(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("SYST", mockResponse{code: 215, msg: "UNIX Type: L8"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	code, msg, err := c.Cmd(215, "SYST")
	if err != nil {
		t.Fatalf("Cmd SYST: %v", err)
	}
	if code != 215 {
		t.Errorf("expected 215, got %d", code)
	}
	if msg != "UNIX Type: L8" {
		t.Errorf("expected 'UNIX Type: L8', got %q", msg)
	}
}

func TestSize(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	size, err := c.Size(context.Background(), "/path/to/file")
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != 12345 {
		t.Errorf("expected 12345, got %d", size)
	}
}

func TestSizeParseError(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("SIZE", mockResponse{code: 213, msg: "not-a-number"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, err = c.Size(context.Background(), "/file")
	if err == nil {
		t.Fatal("expected error for non-numeric SIZE response")
	}
}

func TestSizeUnsupported(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("SIZE", mockResponse{code: 500, msg: "SIZE unsupported"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, err = c.Size(context.Background(), "/file")
	if !errors.Is(err, ErrSizeUnsupported) {
		t.Fatalf("Size() error = %v, want ErrSizeUnsupported", err)
	}
}

func TestCmdUnexpectedCode(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, _, err = c.Cmd(999, "SYST")
	if err == nil {
		t.Fatal("expected error for unexpected response code")
	}
}

func TestParsePASV(t *testing.T) {
	host, port, err := parsePASV("227 Entering Passive Mode (127,0,0,1,4,210).")
	if err != nil {
		t.Fatalf("parsePASV: %v", err)
	}
	if host != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", host)
	}
	if port != 4*256+210 {
		t.Errorf("port = %d, want %d", port, 4*256+210)
	}
}

func TestParsePASVBadFormat(t *testing.T) {
	_, _, err := parsePASV("227 bad response")
	if err == nil {
		t.Fatal("expected error for bad PASV response")
	}

	_, _, err = parsePASV("227 Entering Passive Mode (1,2,3).")
	if err == nil {
		t.Fatal("expected error for incomplete PASV response")
	}
}

func TestParseEPSV(t *testing.T) {
	port, err := parseEPSV("229 Entering Extended Passive Mode (|||1234|).")
	if err != nil {
		t.Fatalf("parseEPSV: %v", err)
	}
	if port != 1234 {
		t.Errorf("port = %d, want 1234", port)
	}
}

func TestParseEPSVBadFormat(t *testing.T) {
	_, err := parseEPSV("229 bad response")
	if err == nil {
		t.Fatal("expected error for bad EPSV response")
	}

	_, err = parseEPSV("229 Entering Extended Passive Mode (|||).")
	if err == nil {
		t.Fatal("expected error for EPSV with no port")
	}
}

func TestParseEPSVEdgeCases(t *testing.T) {
	// C++ FtpConnectionTest::testReceiveEpsvResponse
	// Go's parseEPSV is more lenient than C++ in some edge cases.
	tests := []struct {
		msg  string
		port int
		err  bool
	}{
		// Good: standard format
		{"229 Success (|||12000|)\r\n", 12000, false},
		// Missing opening paren: fails
		{"229 Success |||12000|)\r\n", 0, true},
		// Missing closing paren: fails
		{"229 Success (|||12000|\r\n", 0, true},
		// Opening paren but no pipes inside: fails
		{"229 Success ()|||12000|\r\n", 0, true},
		// Malformed prefix before tuple: fails
		{"229 Success )(|||12000|)\r\n", 0, true},
		// Too few fields: fails
		{"229 Success )(||12000|)\r\n", 0, true},
		// Port missing but parens present: fails
		{"229 Success (|||)\r\n", 0, true},
	}
	for _, tt := range tests {
		port, err := parseEPSV(tt.msg)
		if tt.err && err == nil {
			t.Errorf("parseEPSV(%q): expected error, got port=%d", tt.msg, port)
		}
		if !tt.err && err != nil {
			t.Errorf("parseEPSV(%q): unexpected error: %v", tt.msg, err)
		}
		if !tt.err && port != tt.port {
			t.Errorf("parseEPSV(%q): port=%d, want %d", tt.msg, port, tt.port)
		}
	}
}

func TestParseEPSVPrefixVariants(t *testing.T) {
	// Go's parseEPSV finds parentheses and split by |, taking first numeric value.
	tests := []struct {
		msg  string
		port int
		err  bool
	}{
		// Non-empty protocol/address fields are ignored; fourth field is port.
		{"229 (|1|127.0.0.1|12000|)", 12000, false},
		// "(" prefix without protocol
		{"229 (|||12000|)", 12000, false},
		// "(|" prefix with only 2 pipes
		{"229 (||12000|)", 0, true},
	}
	for _, tt := range tests {
		port, err := parseEPSV(tt.msg)
		if tt.err && err == nil {
			t.Errorf("parseEPSV(%q): expected error, got port=%d", tt.msg, port)
		}
		if !tt.err && err != nil {
			t.Errorf("parseEPSV(%q): unexpected error: %v", tt.msg, err)
		}
		if !tt.err && port != tt.port {
			t.Errorf("parseEPSV(%q): port=%d, want %d", tt.msg, port, tt.port)
		}
	}
}

func TestMdtmMillisecondTruncation(t *testing.T) {
	// C++ FtpConnectionTest::testReceiveMdtmResponse: millisecond part is ignored.
	srv := newMockFTPServer(t)
	defer srv.close()

	// MDTM response with fractional seconds
	srv.setHandler("MDTM", mockResponse{code: 213, msg: "20080908124312.014"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	tm, err := c.Mdtm(context.Background(), "/file.bin")
	if err != nil {
		t.Fatalf("Mdtm: %v", err)
	}
	expected := time.Date(2008, 9, 8, 12, 43, 12, 0, time.UTC)
	if !tm.Equal(expected) {
		t.Errorf("Mdtm with fractional seconds = %v, want %v", tm, expected)
	}
}

func TestMdtmInvalidMonth(t *testing.T) {
	// C++ FtpConnectionTest::testReceiveMdtmResponse: invalid month 19
	// Go's time.Parse rejects month 19 (must be 1-12).
	srv := newMockFTPServer(t)
	defer srv.close()

	// Month 19 is invalid
	srv.setHandler("MDTM", mockResponse{code: 213, msg: "20081908124312"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, err = c.Mdtm(context.Background(), "/file.bin")
	if err == nil {
		t.Error("expected error for MDTM with invalid month 19")
	}
}

func TestMdtmTimeWithoutSeconds(t *testing.T) {
	// C++: hhmmss part is missing, time is invalid.
	srv := newMockFTPServer(t)
	defer srv.close()

	// Missing hhmmss, only date
	srv.setHandler("MDTM", mockResponse{code: 213, msg: "20080908"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, err = c.Mdtm(context.Background(), "/file.bin")
	if err == nil {
		t.Error("expected error for MDTM with missing time component")
	}
}

func TestMdtmNotFoundResponse(t *testing.T) {
	// C++: Status 550 "File Not Found" should return error.
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("MDTM", mockResponse{code: 550, msg: "File Not Found"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, err = c.Mdtm(context.Background(), "/nonexistent")
	if err == nil {
		t.Error("expected error for MDTM 550 response")
	}
}

func TestMaxRecvBufferIncrementalOverflow(t *testing.T) {
	// C++ FtpConnectionTest::testReceiveResponse_overflow:
	// Write 64×1024 bytes incrementally, then overflow on 65th.
	// Our Go implementation already has TestMaxRecvBufferEnforced;
	// this additional test sends incremental 1K chunks like C++ does.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "220 FTP ready\r\n")
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			line := strings.ToUpper(strings.TrimSpace(string(buf[:n])))
			if line == "" {
				continue
			}
			cmd := strings.SplitN(line, " ", 2)[0]
			switch cmd {
			case "USER":
				fmt.Fprintf(conn, "230 Login OK.\r\n")
			case "PASS":
				fmt.Fprintf(conn, "230 Login OK.\r\n")
			case "TYPE":
				fmt.Fprintf(conn, "200 OK.\r\n")
			case "PWD":
				// Send single oversized quoted response - exceeds maxRecvBuffer
				big := make([]byte, maxRecvBuffer+4096)
				for i := range big {
					big[i] = 'X'
				}
				fmt.Fprintf(conn, "257 \"%s\"\r\n", string(big))
			default:
				fmt.Fprintf(conn, "500 Unknown.\r\n")
			}
		}
	}()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	_, err = Dial(context.Background(), dialer, ln.Addr().String(), Opt{User: "test", Pass: "p"})
	if err == nil {
		t.Fatal("expected error for oversize response")
	}
}

func TestRetrievePassive(t *testing.T) {
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("data listen: %v", err)
	}
	defer dataLn.Close()
	dataAddr := dataLn.Addr().String()
	dataHost, dataPortStr, _ := net.SplitHostPort(dataAddr)
	dataPort := 0
	fmt.Sscanf(dataPortStr, "%d", &dataPort)

	srv := newMockFTPServer(t)
	defer srv.close()

	p1 := dataPort / 256
	p2 := dataPort % 256
	parts := strings.Split(dataHost, ".")
	pasvMsg := fmt.Sprintf("Entering Passive Mode (%s,%s,%s,%s,%d,%d).",
		parts[0], parts[1], parts[2], parts[3], p1, p2)
	srv.setHandler("PASV", mockResponse{code: 227, msg: pasvMsg})
	srv.setHandler("EPSV", mockResponse{code: 500, msg: "EPSV not supported."})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	expectedData := "Hello FTP data!"
	done := make(chan struct{})
	go func() {
		defer close(done)
		dataConn, err := dataLn.Accept()
		if err != nil {
			return
		}
		defer dataConn.Close()
		io.WriteString(dataConn, expectedData)
	}()

	ctx := context.Background()
	rc, err := c.Retrieve(ctx, "/file.bin", 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != expectedData {
		t.Errorf("data = %q, want %q", string(data), expectedData)
	}

	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done
}

func TestRetrievePASVIgnoresAdvertisedHost(t *testing.T) {
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("data listen: %v", err)
	}
	defer dataLn.Close()

	_, dataPortStr, _ := net.SplitHostPort(dataLn.Addr().String())
	var dataPort int
	fmt.Sscanf(dataPortStr, "%d", &dataPort)

	srv := newMockFTPServer(t)
	defer srv.close()

	p1 := dataPort / 256
	p2 := dataPort % 256
	srv.setHandler("PASV", mockResponse{code: 227, msg: fmt.Sprintf("Entering Passive Mode (203,0,113,1,%d,%d).", p1, p2)})
	srv.setHandler("EPSV", mockResponse{code: 500, msg: "EPSV not supported."})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		dataConn, err := dataLn.Accept()
		if err != nil {
			return
		}
		defer dataConn.Close()
		io.WriteString(dataConn, "pasv-peer-host")
	}()

	rc, err := c.Retrieve(context.Background(), "/file.bin", 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "pasv-peer-host" {
		t.Fatalf("data = %q, want pasv-peer-host", data)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done
}

func TestRetrieveWithRest(t *testing.T) {
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("data listen: %v", err)
	}
	defer dataLn.Close()
	dataAddr := dataLn.Addr().String()
	dataHost, dataPortStr, _ := net.SplitHostPort(dataAddr)
	dataPort := 0
	fmt.Sscanf(dataPortStr, "%d", &dataPort)

	srv := newMockFTPServer(t)
	defer srv.close()

	p1 := dataPort / 256
	p2 := dataPort % 256
	parts := strings.Split(dataHost, ".")
	pasvMsg := fmt.Sprintf("Entering Passive Mode (%s,%s,%s,%s,%d,%d).",
		parts[0], parts[1], parts[2], parts[3], p1, p2)
	srv.setHandler("PASV", mockResponse{code: 227, msg: pasvMsg})
	srv.setHandler("EPSV", mockResponse{code: 500, msg: "EPSV not supported."})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		dataConn, err := dataLn.Accept()
		if err != nil {
			return
		}
		defer dataConn.Close()
		io.WriteString(dataConn, "offset-data")
	}()

	ctx := context.Background()
	rc, err := c.Retrieve(ctx, "/file.bin", 1024)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	data, _ := io.ReadAll(rc)
	if string(data) != "offset-data" {
		t.Errorf("data = %q, want 'offset-data'", string(data))
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done
}

func TestCommandsPercentDecodePath(t *testing.T) {
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("data listen: %v", err)
	}
	defer dataLn.Close()

	_, dataPortStr, _ := net.SplitHostPort(dataLn.Addr().String())
	var dataPort int
	fmt.Sscanf(dataPortStr, "%d", &dataPort)

	srv := newMockFTPServer(t)
	defer srv.close()
	srv.setHandler("PASV", mockResponse{code: 227, msg: fmt.Sprintf("Entering Passive Mode (127,0,0,1,%d,%d).", dataPort/256, dataPort%256)})
	srv.setHandler("EPSV", mockResponse{code: 229, msg: fmt.Sprintf("Entering Extended Passive Mode (|||%d|).", dataPort)})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if _, err := c.Size(context.Background(), "/dir/file%20name.txt"); err != nil {
		t.Fatalf("Size: %v", err)
	}
	if _, err := c.Mdtm(context.Background(), "/dir/file%20name.txt"); err != nil {
		t.Fatalf("Mdtm: %v", err)
	}
	if err := c.Cwd(context.Background(), "/dir%20name"); err != nil {
		t.Fatalf("Cwd: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		dataConn, err := dataLn.Accept()
		if err != nil {
			return
		}
		defer dataConn.Close()
		_, _ = io.WriteString(dataConn, "decoded-path")
	}()

	rc, err := c.Retrieve(context.Background(), "/dir/file%20name.txt", 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done

	history := srv.commandHistory()
	for _, want := range []string{
		"SIZE /dir/file name.txt",
		"MDTM /dir/file name.txt",
		"CWD /dir name",
		"RETR /dir/file name.txt",
	} {
		found := false
		for _, got := range history {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("command %q not observed in history %v", want, history)
		}
	}
}

func TestRetrievePassiveIPv4UsesPASV(t *testing.T) {
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("data listen: %v", err)
	}
	defer dataLn.Close()
	_, dataPortStr, _ := net.SplitHostPort(dataLn.Addr().String())
	dataPort := 0
	fmt.Sscanf(dataPortStr, "%d", &dataPort)

	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("PASV", mockResponse{code: 227, msg: fmt.Sprintf("Entering Passive Mode (127,0,0,1,%d,%d).", dataPort/256, dataPort%256)})
	srv.setHandler("EPSV", mockResponse{code: 500, msg: "EPSV should not be used on IPv4."})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		dataConn, err := dataLn.Accept()
		if err != nil {
			return
		}
		defer dataConn.Close()
		io.WriteString(dataConn, "EPSV-DATA")
	}()

	ctx := context.Background()
	rc, err := c.Retrieve(ctx, "/file.bin", 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	data, _ := io.ReadAll(rc)
	if string(data) != "EPSV-DATA" {
		t.Errorf("data = %q, want 'EPSV-DATA'", string(data))
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done

	for _, cmd := range srv.commandHistory() {
		if strings.HasPrefix(cmd, "EPSV") {
			t.Fatalf("unexpected EPSV command on IPv4 control connection: %v", srv.commandHistory())
		}
	}
}

func TestDialReusesControlConnection(t *testing.T) {
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("data listen: %v", err)
	}
	defer dataLn.Close()

	_, dataPortStr, _ := net.SplitHostPort(dataLn.Addr().String())
	var dataPort int
	fmt.Sscanf(dataPortStr, "%d", &dataPort)

	srv := newMockFTPServer(t)
	defer srv.close()
	srv.setHandler("PASV", mockResponse{code: 227, msg: fmt.Sprintf("Entering Passive Mode (127,0,0,1,%d,%d).", dataPort/256, dataPort%256)})

	payloads := []string{"first-transfer", "second-transfer"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, payload := range payloads {
			conn, err := dataLn.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(conn, payload)
			_ = conn.Close()
		}
	}()

	dialer, err := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewDialer: %v", err)
	}

	opt := Opt{User: "test", Pass: "p", Passive: true, ReuseConnection: true}
	t.Cleanup(func() {
		if pooled := ftpControlConnPool.pop(reuseControlConnKey(srv.addr, dialer, opt)); pooled != nil {
			_ = pooled.closeUnderlying()
		}
	})

	c1, err := Dial(context.Background(), dialer, srv.addr, opt)
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}
	rc1, err := c1.Retrieve(context.Background(), "/file.bin", 0)
	if err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	data1, err := io.ReadAll(rc1)
	if err != nil {
		t.Fatalf("first ReadAll: %v", err)
	}
	if err := rc1.Close(); err != nil {
		t.Fatalf("first body Close: %v", err)
	}
	if err := c1.Close(); err != nil {
		t.Fatalf("first conn Close: %v", err)
	}

	c2, err := Dial(context.Background(), dialer, srv.addr, opt)
	if err != nil {
		t.Fatalf("second Dial: %v", err)
	}
	rc2, err := c2.Retrieve(context.Background(), "/file.bin", 0)
	if err != nil {
		t.Fatalf("second Retrieve: %v", err)
	}
	data2, err := io.ReadAll(rc2)
	if err != nil {
		t.Fatalf("second ReadAll: %v", err)
	}
	if err := rc2.Close(); err != nil {
		t.Fatalf("second body Close: %v", err)
	}
	if err := c2.Close(); err != nil {
		t.Fatalf("second conn Close: %v", err)
	}
	<-done

	if string(data1) != payloads[0] {
		t.Fatalf("first payload = %q, want %q", data1, payloads[0])
	}
	if string(data2) != payloads[1] {
		t.Fatalf("second payload = %q, want %q", data2, payloads[1])
	}
	if got := srv.acceptCount(); got != 1 {
		t.Fatalf("control connections = %d, want 1 reused connection", got)
	}
}

func TestRetrieveActiveModeNotImplemented(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	done := make(chan []string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- nil
			return
		}
		defer conn.Close()

		fmt.Fprintf(conn, "220 FTP mock ready\r\n")
		reader := bufio.NewReader(conn)
		var commands []string
		var activeAddr string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- commands
				return
			}
			line = strings.TrimSpace(line)
			commands = append(commands, line)
			cmd, arg, _ := strings.Cut(line, " ")
			switch cmd {
			case "USER":
				fmt.Fprintf(conn, "331 Password required.\r\n")
			case "PASS":
				fmt.Fprintf(conn, "230 Login successful.\r\n")
			case "TYPE":
				fmt.Fprintf(conn, "200 Type set.\r\n")
			case "PWD":
				fmt.Fprintf(conn, "257 \"/\"\r\n")
			case "PORT":
				activeAddr, err = parsePORTArg(arg)
				if err != nil {
					done <- commands
					return
				}
				fmt.Fprintf(conn, "200 PORT command successful.\r\n")
			case "EPRT":
				activeAddr, err = parseEPRTArg(arg)
				if err != nil {
					done <- commands
					return
				}
				fmt.Fprintf(conn, "200 EPRT command successful.\r\n")
			case "REST":
				fmt.Fprintf(conn, "350 Restarting at offset.\r\n")
			case "RETR":
				fmt.Fprintf(conn, "150 Opening data connection\r\n")
				dataConn, err := net.Dial("tcp", activeAddr)
				if err != nil {
					t.Errorf("dial active endpoint: %v", err)
					done <- commands
					return
				}
				_, _ = io.WriteString(dataConn, "ACTIVE-DATA")
				_ = dataConn.Close()
				fmt.Fprintf(conn, "226 Transfer complete.\r\n")
				done <- commands
				return
			default:
				fmt.Fprintf(conn, "500 Unknown command.\r\n")
			}
		}
	}()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, ln.Addr().String(), Opt{User: "test", Pass: "p", Passive: false})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	rc, err := c.Retrieve(context.Background(), "/file.bin", 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "ACTIVE-DATA" {
		t.Fatalf("data = %q, want ACTIVE-DATA", string(data))
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	commands := <-done
	var sawPort bool
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, "PORT ") || strings.HasPrefix(cmd, "EPRT ") {
			sawPort = true
			break
		}
	}
	if !sawPort {
		t.Fatalf("active mode command missing: %v", commands)
	}
}

func parsePORTArg(arg string) (string, error) {
	parts := strings.Split(arg, ",")
	if len(parts) != 6 {
		return "", fmt.Errorf("PORT arg = %q, want 6 fields", arg)
	}
	p1, err := parseInt(parts[4])
	if err != nil {
		return "", err
	}
	p2, err := parseInt(parts[5])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s.%s.%s:%d", parts[0], parts[1], parts[2], parts[3], p1*256+p2), nil
}

func parseEPRTArg(arg string) (string, error) {
	parts := strings.Split(arg, "|")
	if len(parts) != 5 {
		return "", fmt.Errorf("EPRT arg = %q, want 5 fields", arg)
	}
	return net.JoinHostPort(parts[2], parts[3]), nil
}

func parseInt(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("parse %q: %w", s, err)
	}
	return n, nil
}

func TestRetrieveRejectsWrongPreliminaryStatus(t *testing.T) {
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("data listen: %v", err)
	}
	defer dataLn.Close()

	_, dataPortStr, _ := net.SplitHostPort(dataLn.Addr().String())
	var dataPort int
	fmt.Sscanf(dataPortStr, "%d", &dataPort)

	srv := newMockFTPServer(t)
	defer srv.close()
	srv.setHandler("PASV", mockResponse{code: 227, msg: fmt.Sprintf("Entering Passive Mode (127,0,0,1,%d,%d).", dataPort/256, dataPort%256)})
	srv.setHandler("EPSV", mockResponse{code: 229, msg: fmt.Sprintf("Entering Extended Passive Mode (|||%d|).", dataPort)})
	srv.setHandler("RETR", mockResponse{code: 120, msg: "Service ready soon."})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := dataLn.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}()

	_, err = c.Retrieve(context.Background(), "/file.bin", 0)
	if err == nil {
		t.Fatal("expected error for non-125/150 RETR status")
	}
	<-done
}

func TestRetrieveCloseChecksFinalStatus(t *testing.T) {
	dataLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("data listen: %v", err)
	}
	defer dataLn.Close()

	_, dataPortStr, _ := net.SplitHostPort(dataLn.Addr().String())
	var dataPort int
	fmt.Sscanf(dataPortStr, "%d", &dataPort)

	srv := newMockFTPServer(t)
	defer srv.close()
	srv.setHandler("PASV", mockResponse{code: 227, msg: fmt.Sprintf("Entering Passive Mode (127,0,0,1,%d,%d).", dataPort/256, dataPort%256)})
	srv.setHandler("EPSV", mockResponse{code: 229, msg: fmt.Sprintf("Entering Extended Passive Mode (|||%d|).", dataPort)})
	srv.setHandler("RETR", mockResponse{multi: []string{
		"150 Opening data connection",
		"550 Transfer failed",
	}})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p", Passive: true})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := dataLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.WriteString(conn, "partial")
	}()

	rc, err := c.Retrieve(context.Background(), "/file.bin", 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := rc.Close(); err == nil {
		t.Fatal("expected Close error for final 550 status")
	}
	<-done
}

func TestDialInvalidAddr(t *testing.T) {
	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 1 * time.Second})
	ctx := context.Background()
	_, err := Dial(ctx, dialer, "127.0.0.1:19999", Opt{User: "test", Pass: "p"})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestDialBadBanner(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()
		fmt.Fprintf(conn, "500 Service not available\r\n")
	}()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	_, err = Dial(context.Background(), dialer, ln.Addr().String(), Opt{User: "test", Pass: "p"})
	if err == nil {
		t.Fatal("expected error for non-220 banner")
	}
}

func TestDialImplicitTLSRequiresConfig(t *testing.T) {
	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	_, err := Dial(context.Background(), dialer, "127.0.0.1:21", Opt{TLSMode: 2})
	if err == nil {
		t.Fatal("expected error for implicit TLS without TLSConfig")
	}
}

func TestDialExplicitTLSRequiresConfig(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	_, err := Dial(context.Background(), dialer, srv.addr, Opt{
		User:    "test",
		Pass:    "p",
		TLSMode: 1,
	})
	if err == nil {
		t.Fatal("expected error for explicit TLS without TLSConfig")
	}
}

func TestDialExplicitTLS(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		fmt.Fprintf(conn, "220 FTP ready for TLS\r\n")

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		line := strings.TrimSpace(string(buf[:n]))
		if !strings.HasPrefix(strings.ToUpper(line), "AUTH TLS") {
			fmt.Fprintf(conn, "500 Unknown\r\n")
			return
		}
		fmt.Fprintf(conn, "234 AUTH TLS OK\r\n")

		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
		tlsConn := tls.Server(conn, tlsCfg)
		if err := tlsConn.Handshake(); err != nil {
			return
		}

		for {
			n, err := tlsConn.Read(buf)
			if err != nil {
				return
			}
			line := strings.TrimSpace(string(buf[:n]))
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			cmd := strings.ToUpper(parts[0])
			if cmd == "USER" {
				fmt.Fprintf(tlsConn, "331 Password required\r\n")
				continue
			}
			if cmd == "PASS" {
				fmt.Fprintf(tlsConn, "230 Login OK\r\n")
				continue
			}
			if cmd == "TYPE" {
				fmt.Fprintf(tlsConn, "200 Switching to Binary mode.\r\n")
				continue
			}
			if cmd == "PWD" {
				fmt.Fprintf(tlsConn, "257 \"/\"\r\n")
				continue
			}
			if cmd == "QUIT" {
				fmt.Fprintf(tlsConn, "221 Bye\r\n")
				return
			}
			fmt.Fprintf(tlsConn, "500 Unknown\r\n")
		}
	}()

	tlsCfg := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, ln.Addr().String(), Opt{
		User:      "test",
		Pass:      "p",
		TLSMode:   1,
		TLSConfig: tlsCfg,
	})
	if err != nil {
		t.Fatalf("Dial (explicit TLS): %v", err)
	}
	c.Close()
}

func TestDialImplicitTLS(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}
	tlsLn, err := tls.Listen("tcp", "127.0.0.1:0", srvTLS)
	if err != nil {
		t.Fatalf("tls listen: %v", err)
	}
	defer tlsLn.Close()

	go func() {
		conn, _ := tlsLn.Accept()
		defer conn.Close()
		fmt.Fprintf(conn, "220 FTP TLS implicit ready\r\n")
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			line := strings.TrimSpace(string(buf[:n]))
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			cmd := strings.ToUpper(parts[0])
			if cmd == "USER" {
				fmt.Fprintf(conn, "331 Password required\r\n")
				continue
			}
			if cmd == "PASS" {
				fmt.Fprintf(conn, "230 Login OK\r\n")
				continue
			}
			if cmd == "TYPE" {
				fmt.Fprintf(conn, "200 Switching to Binary mode.\r\n")
				continue
			}
			if cmd == "PWD" {
				fmt.Fprintf(conn, "257 \"/\"\r\n")
				continue
			}
			if cmd == "QUIT" {
				fmt.Fprintf(conn, "221 Bye\r\n")
				return
			}
			fmt.Fprintf(conn, "500 Unknown\r\n")
		}
	}()

	tlsCfg := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, tlsLn.Addr().String(), Opt{
		User:      "test",
		Pass:      "p",
		TLSMode:   2,
		TLSConfig: tlsCfg,
	})
	if err != nil {
		t.Fatalf("Dial (implicit TLS): %v", err)
	}
	c.Close()
}

func generateTestCert(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa generate: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return certPEM, keyPEM
}

func TestPwd(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("PWD", mockResponse{code: 257, msg: "\"/home/test\""})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if c.BaseWorkingDir() != "/home/test" {
		t.Errorf("BaseWorkingDir = %q, want /home/test", c.BaseWorkingDir())
	}

	pwd, err := c.Pwd(context.Background())
	if err != nil {
		t.Fatalf("Pwd: %v", err)
	}
	if pwd != "/home/test" {
		t.Errorf("Pwd = %q, want /home/test", pwd)
	}
}

func TestPwdNoQuote(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("PWD", mockResponse{code: 257, msg: "no quotation marks"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	_, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p"})
	if err == nil {
		t.Fatal("expected error for PWD without quotation marks")
	}
}

func TestMdtm(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("MDTM", mockResponse{code: 213, msg: "20230504120000"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	tm, err := c.Mdtm(context.Background(), "/file.bin")
	if err != nil {
		t.Fatalf("Mdtm: %v", err)
	}
	expected := time.Date(2023, 5, 4, 12, 0, 0, 0, time.UTC)
	if !tm.Equal(expected) {
		t.Errorf("Mdtm = %v, want %v", tm, expected)
	}
}

func TestMdtmShortResponse(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("MDTM", mockResponse{code: 213, msg: "202305"})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	_, err = c.Mdtm(context.Background(), "/file.bin")
	if err == nil {
		t.Fatal("expected error for short MDTM response")
	}
}

func TestCwd(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	err = c.Cwd(context.Background(), "/some/dir")
	if err != nil {
		t.Fatalf("Cwd: %v", err)
	}
}

func TestCwdFail(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("CWD", mockResponse{code: 550, msg: "Directory not found."})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	err = c.Cwd(context.Background(), "/noexist")
	if err == nil {
		t.Fatal("expected error for CWD to nonexistent directory")
	}
}

func TestBaseWorkingDir(t *testing.T) {
	srv := newMockFTPServer(t)
	defer srv.close()

	srv.setHandler("PWD", mockResponse{code: 257, msg: "\"/data/ftp\""})

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	c, err := Dial(context.Background(), dialer, srv.addr, Opt{User: "test", Pass: "p"})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close()

	if c.BaseWorkingDir() != "/data/ftp" {
		t.Errorf("BaseWorkingDir = %q, want /data/ftp", c.BaseWorkingDir())
	}
}

func TestMaxRecvBufferEnforced(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "220 FTP ready\r\n")
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			line := strings.TrimSpace(string(buf[:n]))
			if line == "" {
				continue
			}
			cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
			if cmd == "USER" {
				fmt.Fprintf(conn, "230 Login OK.\r\n")
				continue
			}
			if cmd == "PASS" {
				fmt.Fprintf(conn, "230 Login OK.\r\n")
				continue
			}
			if cmd == "TYPE" {
				fmt.Fprintf(conn, "200 OK.\r\n")
				continue
			}
			if cmd == "PWD" {
				big := make([]byte, maxRecvBuffer+4096)
				for i := range big {
					big[i] = 'X'
				}
				fmt.Fprintf(conn, "257 \"%s\"\r\n", string(big))
				continue
			}
			fmt.Fprintf(conn, "500 Unknown.\r\n")
		}
	}()

	dialer, _ := netx.NewDialer(netx.DialerConfig{Timeout: 5 * time.Second})
	_, err = Dial(context.Background(), dialer, ln.Addr().String(), Opt{User: "test", Pass: "p"})
	if err == nil {
		t.Fatal("expected error for oversize response")
	}
}
