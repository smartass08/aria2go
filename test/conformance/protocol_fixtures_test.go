package conformance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/bencode"
)

type protocolFixtureStrategy struct {
	Name         string
	Offline      bool
	Differential bool
	Ready        bool
	Notes        string
}

func protocolFixtureStrategies() []protocolFixtureStrategy {
	return []protocolFixtureStrategy{
		{
			Name:         "FTP",
			Offline:      true,
			Differential: true,
			Ready:        true,
			Notes:        "local FTP server with active/passive data connections, SIZE, REST, MDTM, and RETR",
		},
		{
			Name:         "SFTP",
			Offline:      true,
			Differential: false,
			Ready:        false,
			Notes:        "needs a reusable SSH/SFTP fixture outside test/stack before dual-run coverage",
		},
		{
			Name:         "Metalink",
			Offline:      true,
			Differential: true,
			Ready:        true,
			Notes:        "programmatic Metalink v4 documents backed by local HTTP fixture URLs",
		},
		{
			Name:         "BitTorrent",
			Offline:      true,
			Differential: false,
			Ready:        true,
			Notes:        "programmatic .torrent, HTTP tracker, and single TCP peer; download parity is scaffolded",
		},
	}
}

func protocolPayload(label string, size int) []byte {
	seed := sha256.Sum256([]byte(label))
	out := make([]byte, size)
	for i := range out {
		out[i] = seed[i%len(seed)] ^ byte(i*31) ^ byte(i>>8)
	}
	return out
}

func protocolBaseArgs(dir string) []string {
	return []string{
		"--no-conf=true",
		"--dir=" + dir,
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--no-netrc=true",
		"--check-certificate=false",
		"--no-proxy=127.0.0.1,localhost",
		"--enable-dht=false",
		"--enable-dht6=false",
		"--bt-enable-lpd=false",
	}
}

func protocolRun(t *testing.T, ref bool, args []string) RunResult {
	t.Helper()

	opts := RunOptions{
		Timeout: 30 * time.Second,
		Env: []string{
			"http_proxy=",
			"HTTP_PROXY=",
			"https_proxy=",
			"HTTPS_PROXY=",
			"ftp_proxy=",
			"FTP_PROXY=",
			"all_proxy=",
			"ALL_PROXY=",
			"no_proxy=127.0.0.1,localhost",
			"NO_PROXY=127.0.0.1,localhost",
		},
	}

	var (
		result RunResult
		err    error
	)
	if ref {
		result, err = RunRefWithOptions(t, args, "", opts)
	} else {
		result, err = RunImplWithOptions(t, args, "", opts)
	}
	if err != nil {
		t.Fatalf("run ref=%v: %v\nstdout=%s\nstderr=%s", ref, err, result.Stdout, result.Stderr)
	}
	return result
}

func protocolRequireExitZero(t *testing.T, label string, result RunResult) {
	t.Helper()
	if result.ExitCode != 0 {
		t.Fatalf("%s exit=%d\nstdout=%s\nstderr=%s", label, result.ExitCode, result.Stdout, result.Stderr)
	}
}

func protocolRequireFile(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s bytes mismatch: got %d bytes want %d", path, len(got), len(want))
	}
}

type protocolHTTPFixture struct {
	*httptest.Server
	files map[string][]byte
}

func startProtocolHTTPFixture(t *testing.T, files map[string][]byte) *protocolHTTPFixture {
	t.Helper()

	copied := make(map[string][]byte, len(files))
	for name, data := range files {
		key := "/" + strings.TrimPrefix(path.Clean(name), "/")
		copied[key] = append([]byte(nil), data...)
	}

	f := &protocolHTTPFixture{files: copied}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := copied[path.Clean(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			return
		}
		if start, end, ok := protocolRangeBounds(r.Header.Get("Range"), len(data)); ok {
			w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[start : end+1])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}))
	return f
}

func protocolRangeBounds(raw string, size int) (start int, end int, ok bool) {
	if !strings.HasPrefix(raw, "bytes=") || size <= 0 {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(raw, "bytes=")
	startText, endText, hasDash := strings.Cut(spec, "-")
	if !hasDash {
		return 0, 0, false
	}
	startText = strings.TrimSpace(startText)
	endText = strings.TrimSpace(endText)
	if startText == "" {
		return 0, 0, false
	}
	start, err := strconv.Atoi(startText)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	if endText == "" {
		return start, size - 1, true
	}
	end, err = strconv.Atoi(endText)
	if err != nil || end < start {
		return 0, 0, false
	}
	if end >= size {
		end = size - 1
	}
	return start, end, true
}

func (f *protocolHTTPFixture) URLPath(name string) string {
	return f.URL + "/" + strings.TrimPrefix(path.Clean(name), "/")
}

type protocolMetalinkFile struct {
	Name string
	URL  string
	Data []byte
}

func protocolMetalinkV4(t *testing.T, files []protocolMetalinkFile) []byte {
	t.Helper()

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<metalink xmlns="urn:ietf:params:xml:ns:metalink">` + "\n")
	for _, file := range files {
		sum := sha256.Sum256(file.Data)
		b.WriteString(`  <file name="`)
		xmlEscape(&b, file.Name)
		b.WriteString(`">` + "\n")
		b.WriteString("    <size>")
		b.WriteString(strconv.Itoa(len(file.Data)))
		b.WriteString("</size>\n")
		b.WriteString(`    <hash type="sha-256">`)
		b.WriteString(hex.EncodeToString(sum[:]))
		b.WriteString("</hash>\n")
		b.WriteString(`    <url priority="1" location="local">`)
		xmlEscape(&b, file.URL)
		b.WriteString("</url>\n")
		b.WriteString("  </file>\n")
	}
	b.WriteString("</metalink>\n")
	return []byte(b.String())
}

func xmlEscape(w io.Writer, s string) {
	_ = xml.EscapeText(w, []byte(s))
}

type protocolFTPFixture struct {
	ln     net.Listener
	cancel context.CancelFunc

	mu       sync.Mutex
	files    map[string][]byte
	commands []protocolFTPCommand

	supportSize bool
	modTime     time.Time
}

type protocolFTPFixtureOptions struct {
	SupportSize bool
	ModTime     time.Time
}

type protocolFTPCommand struct {
	Name string
	Arg  string
}

func startProtocolFTPFixture(t *testing.T, files map[string][]byte) *protocolFTPFixture {
	t.Helper()
	return startProtocolFTPFixtureWithOptions(t, files, protocolFTPFixtureOptions{
		SupportSize: true,
		ModTime:     protocolFTPMTime(),
	})
}

func startProtocolFTPFixtureWithOptions(t *testing.T, files map[string][]byte, opts protocolFTPFixtureOptions) *protocolFTPFixture {
	t.Helper()
	if opts.ModTime.IsZero() {
		opts.ModTime = protocolFTPMTime()
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ftp listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &protocolFTPFixture{
		ln:          ln,
		cancel:      cancel,
		files:       make(map[string][]byte, len(files)),
		supportSize: opts.SupportSize,
		modTime:     opts.ModTime,
	}
	for name, data := range files {
		key := "/" + strings.TrimPrefix(path.Clean(name), "/")
		f.files[key] = append([]byte(nil), data...)
	}

	go f.serve(ctx)
	t.Cleanup(f.Close)
	return f
}

func (f *protocolFTPFixture) URL(name string) string {
	return (&url.URL{
		Scheme: "ftp",
		Host:   f.ln.Addr().String(),
		Path:   "/" + strings.TrimPrefix(path.Clean(name), "/"),
	}).String()
}

func (f *protocolFTPFixture) Close() {
	f.cancel()
	_ = f.ln.Close()
}

func (f *protocolFTPFixture) snapshotCommands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.commands))
	for _, cmd := range f.commands {
		out = append(out, cmd.Name)
	}
	return out
}

func (f *protocolFTPFixture) snapshotCommandDetails() []protocolFTPCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocolFTPCommand(nil), f.commands...)
}

func (f *protocolFTPFixture) record(cmd, arg string) {
	f.mu.Lock()
	f.commands = append(f.commands, protocolFTPCommand{Name: cmd, Arg: arg})
	f.mu.Unlock()
}

func (f *protocolFTPFixture) serve(ctx context.Context) {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		go f.handleConn(ctx, conn)
	}
}

type protocolFTPSession struct {
	dataLn     net.Listener
	activeAddr string
	rest       int64
}

func (f *protocolFTPFixture) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	sess := &protocolFTPSession{}
	defer func() {
		if sess.dataLn != nil {
			_ = sess.dataLn.Close()
		}
	}()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	writeFTPLine(writer, "220 aria2go conformance FTP fixture")

	for {
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		} else {
			_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		cmd, arg := splitFTPCommand(line)
		f.record(cmd, arg)

		switch cmd {
		case "USER":
			writeFTPLine(writer, "331 password required")
		case "PASS":
			writeFTPLine(writer, "230 login ok")
		case "SYST":
			writeFTPLine(writer, "215 UNIX Type: L8")
		case "PWD":
			writeFTPLine(writer, `257 "/"`)
		case "TYPE":
			writeFTPLine(writer, "200 type ok")
		case "NOOP":
			writeFTPLine(writer, "200 noop ok")
		case "FEAT":
			writeFTPLine(writer, "211-Features")
			writeFTPLine(writer, " EPSV")
			if f.supportSize {
				writeFTPLine(writer, " SIZE")
			}
			writeFTPLine(writer, " MDTM")
			writeFTPLine(writer, " REST STREAM")
			writeFTPLine(writer, "211 End")
		case "OPTS":
			writeFTPLine(writer, "200 opts ok")
		case "CWD":
			writeFTPLine(writer, "250 directory changed")
		case "EPSV":
			port, ok := f.startFTPDataListener(writer, sess)
			if ok {
				writeFTPLine(writer, fmt.Sprintf("229 Entering Extended Passive Mode (|||%d|)", port))
			}
		case "PASV":
			port, ok := f.startFTPDataListener(writer, sess)
			if ok {
				writeFTPLine(writer, fmt.Sprintf("227 Entering Passive Mode (127,0,0,1,%d,%d)", port/256, port%256))
			}
		case "PORT":
			addr, err := protocolParseFTPPort(arg)
			if err != nil {
				writeFTPLine(writer, "501 bad PORT argument")
				continue
			}
			sess.activeAddr = addr
			writeFTPLine(writer, "200 PORT command ok")
		case "EPRT":
			addr, err := protocolParseFTPEprt(arg)
			if err != nil {
				writeFTPLine(writer, "501 bad EPRT argument")
				continue
			}
			sess.activeAddr = addr
			writeFTPLine(writer, "200 EPRT command ok")
		case "SIZE":
			if !f.supportSize {
				writeFTPLine(writer, "500 SIZE not understood")
				continue
			}
			data, ok := f.lookup(arg)
			if !ok {
				writeFTPLine(writer, "550 not found")
				continue
			}
			writeFTPLine(writer, fmt.Sprintf("213 %d", len(data)))
		case "MDTM":
			if _, ok := f.lookup(arg); !ok {
				writeFTPLine(writer, "550 not found")
				continue
			}
			writeFTPLine(writer, "213 "+f.modTime.UTC().Format("20060102150405"))
		case "REST":
			offset, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
			if err != nil || offset < 0 {
				writeFTPLine(writer, "501 bad restart offset")
				continue
			}
			sess.rest = offset
			writeFTPLine(writer, fmt.Sprintf("350 restarting at %d", offset))
		case "RETR":
			f.handleRETR(writer, sess, arg)
		case "QUIT":
			writeFTPLine(writer, "221 goodbye")
			return
		default:
			writeFTPLine(writer, "502 command not implemented")
		}
	}
}

func splitFTPCommand(line string) (string, string) {
	cmd, arg, ok := strings.Cut(line, " ")
	if !ok {
		return strings.ToUpper(cmd), ""
	}
	return strings.ToUpper(cmd), strings.TrimSpace(arg)
}

func writeFTPLine(w *bufio.Writer, line string) {
	_, _ = w.WriteString(line + "\r\n")
	_ = w.Flush()
}

func (f *protocolFTPFixture) startFTPDataListener(w *bufio.Writer, sess *protocolFTPSession) (int, bool) {
	if sess.dataLn != nil {
		_ = sess.dataLn.Close()
		sess.dataLn = nil
	}
	sess.activeAddr = ""
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		writeFTPLine(w, "425 cannot open data connection")
		return 0, false
	}
	sess.dataLn = ln
	return ln.Addr().(*net.TCPAddr).Port, true
}

func (f *protocolFTPFixture) handleRETR(w *bufio.Writer, sess *protocolFTPSession, name string) {
	if sess.dataLn == nil && sess.activeAddr == "" {
		writeFTPLine(w, "425 use active or passive mode first")
		return
	}
	data, ok := f.lookup(name)
	if !ok {
		writeFTPLine(w, "550 not found")
		return
	}
	offset := sess.rest
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	sess.rest = 0

	writeFTPLine(w, "150 opening data connection")
	var (
		dataConn net.Conn
		err      error
	)
	if sess.dataLn != nil {
		_ = sess.dataLn.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second))
		dataConn, err = sess.dataLn.Accept()
		if err != nil {
			writeFTPLine(w, "425 data connection failed")
			return
		}
	} else {
		dataConn, err = net.DialTimeout("tcp", sess.activeAddr, 10*time.Second)
		if err != nil {
			writeFTPLine(w, "425 data connection failed")
			return
		}
	}
	_, _ = dataConn.Write(data[offset:])
	_ = dataConn.Close()
	if sess.dataLn != nil {
		_ = sess.dataLn.Close()
		sess.dataLn = nil
	}
	sess.activeAddr = ""
	writeFTPLine(w, "226 transfer complete")
}

func protocolParseFTPPort(arg string) (string, error) {
	parts := strings.Split(arg, ",")
	if len(parts) != 6 {
		return "", fmt.Errorf("bad PORT argument %q", arg)
	}
	p1, err := strconv.Atoi(parts[4])
	if err != nil {
		return "", err
	}
	p2, err := strconv.Atoi(parts[5])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s.%s.%s:%d", parts[0], parts[1], parts[2], parts[3], p1*256+p2), nil
}

func protocolParseFTPEprt(arg string) (string, error) {
	parts := strings.Split(arg, "|")
	if len(parts) != 5 {
		return "", fmt.Errorf("bad EPRT argument %q", arg)
	}
	return net.JoinHostPort(parts[2], parts[3]), nil
}

func (f *protocolFTPFixture) lookup(name string) ([]byte, bool) {
	key := "/" + strings.TrimPrefix(path.Clean(name), "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[key]
	return data, ok
}

func protocolFTPMTime() time.Time {
	return time.Date(2026, time.May, 21, 0, 0, 0, 0, time.UTC)
}

type protocolFTPProxyRequest struct {
	Method string
	Target string
	Host   string
	Range  string
}

type protocolFTPProxyFixture struct {
	ln     net.Listener
	cancel context.CancelFunc

	mu      sync.Mutex
	files   map[string][]byte
	modTime time.Time
	records []protocolFTPProxyRequest
}

func startProtocolFTPProxyFixture(t *testing.T, files map[string][]byte) *protocolFTPProxyFixture {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ftp proxy listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &protocolFTPProxyFixture{
		ln:      ln,
		cancel:  cancel,
		files:   make(map[string][]byte, len(files)),
		modTime: protocolFTPMTime(),
	}
	for name, data := range files {
		key := "/" + strings.TrimPrefix(path.Clean(name), "/")
		f.files[key] = append([]byte(nil), data...)
	}
	go f.serve(ctx)
	t.Cleanup(f.Close)
	return f
}

func (f *protocolFTPProxyFixture) URL() string {
	return "http://" + f.ln.Addr().String()
}

func (f *protocolFTPProxyFixture) Close() {
	f.cancel()
	_ = f.ln.Close()
}

func (f *protocolFTPProxyFixture) snapshotRequests() []protocolFTPProxyRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocolFTPProxyRequest(nil), f.records...)
}

func (f *protocolFTPProxyFixture) serve(ctx context.Context) {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		go f.handleConn(ctx, conn)
	}
}

func (f *protocolFTPProxyFixture) handleConn(parent context.Context, client net.Conn) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer client.Close()

	_ = client.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(client)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return
	}
	method, target, proto := fields[0], fields[1], fields[2]

	host := ""
	rangeHeader := ""
	for {
		h, err := br.ReadString('\n')
		if err != nil {
			return
		}
		if h == "\r\n" || h == "\n" {
			break
		}
		if name, value, ok := strings.Cut(h, ":"); ok {
			switch {
			case strings.EqualFold(name, "Host"):
				host = strings.TrimSpace(value)
			case strings.EqualFold(name, "Range"):
				rangeHeader = strings.TrimSpace(value)
			}
		}
	}

	f.mu.Lock()
	f.records = append(f.records, protocolFTPProxyRequest{
		Method: method,
		Target: target,
		Host:   host,
		Range:  rangeHeader,
	})
	f.mu.Unlock()

	if method == http.MethodConnect {
		f.handleConnect(ctx, client, br, target)
		return
	}
	f.handleFTPProxyRequest(client, method, target, proto, rangeHeader)
}

func (f *protocolFTPProxyFixture) handleFTPProxyRequest(client net.Conn, method, target, proto, rangeHeader string) {
	targetURL, err := url.Parse(target)
	if err != nil || !strings.EqualFold(targetURL.Scheme, "ftp") {
		_, _ = io.WriteString(client, proto+" 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	data, ok := f.lookupProxyFile(targetURL.Path)
	if !ok {
		_, _ = io.WriteString(client, proto+" 404 Not Found\r\nContent-Length: 0\r\n\r\n")
		return
	}

	status := http.StatusOK
	body := data
	contentRange := ""

	// Range handling is intentionally simple: enough for resume and segment probes.
	if start, ok := parseProtocolHTTPRange(rangeHeader); ok {
		if start > int64(len(data)) {
			_, _ = io.WriteString(client, proto+" 416 Requested Range Not Satisfiable\r\nContent-Length: 0\r\n\r\n")
			return
		}
		status = http.StatusPartialContent
		body = data[start:]
		contentRange = fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %d %s\r\n", proto, status, http.StatusText(status))
	fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	b.WriteString("Content-Type: application/octet-stream\r\n")
	b.WriteString("Accept-Ranges: bytes\r\n")
	b.WriteString("Last-Modified: " + f.modTime.Format(http.TimeFormat) + "\r\n")
	if contentRange != "" {
		b.WriteString("Content-Range: " + contentRange + "\r\n")
	}
	b.WriteString("\r\n")
	_, _ = io.WriteString(client, b.String())
	if method != http.MethodHead {
		_, _ = client.Write(body)
	}
}

func (f *protocolFTPProxyFixture) handleConnect(parent context.Context, client net.Conn, br *bufio.Reader, target string) {
	upstream, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer upstream.Close()

	_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	done := make(chan struct{}, 2)

	go func() {
		<-ctx.Done()
		_ = client.Close()
		_ = upstream.Close()
	}()
	go func() {
		_, _ = io.Copy(upstream, br)
		_ = upstream.Close()
		cancel()
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		cancel()
		done <- struct{}{}
	}()
	<-done
}

func (f *protocolFTPProxyFixture) lookupProxyFile(name string) ([]byte, bool) {
	key := "/" + strings.TrimPrefix(path.Clean(name), "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[key]
	return data, ok
}

func parseProtocolHTTPRange(value string) (int64, bool) {
	if !strings.HasPrefix(value, "bytes=") || !strings.HasSuffix(value, "-") {
		return 0, false
	}
	start, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(value, "bytes="), "-"), 10, 64)
	if err != nil || start < 0 {
		return 0, false
	}
	return start, true
}

type protocolBTFixture struct {
	TorrentData []byte
	InfoHash    [20]byte
	Name        string

	payload []byte
	piece   int
	peerLn  net.Listener
	tracker *httptest.Server
	cancel  context.CancelFunc
}

func startProtocolBTFixture(t *testing.T, name string, payload []byte, pieceLength int) *protocolBTFixture {
	t.Helper()

	peerLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("bt peer listen: %v", err)
	}
	peerPort := peerLn.Addr().(*net.TCPAddr).Port

	var torrentData []byte
	var infoHash [20]byte
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/announce" {
			http.NotFound(w, r)
			return
		}
		resp, err := protocolTrackerResponse(peerPort)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(resp)
	}))

	torrentData, infoHash, err = protocolTorrentSingleFile(tracker.URL+"/announce", name, payload, pieceLength)
	if err != nil {
		tracker.Close()
		_ = peerLn.Close()
		t.Fatalf("build torrent fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	f := &protocolBTFixture{
		TorrentData: append([]byte(nil), torrentData...),
		InfoHash:    infoHash,
		Name:        name,
		payload:     append([]byte(nil), payload...),
		piece:       pieceLength,
		peerLn:      peerLn,
		tracker:     tracker,
		cancel:      cancel,
	}
	go f.servePeer(ctx)
	t.Cleanup(f.Close)
	return f
}

func (f *protocolBTFixture) Close() {
	f.cancel()
	_ = f.peerLn.Close()
	f.tracker.Close()
}

func (f *protocolBTFixture) writeTorrentFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, f.Name+".torrent")
	if err := os.WriteFile(p, f.TorrentData, 0o644); err != nil {
		t.Fatalf("write torrent fixture: %v", err)
	}
	return p
}

func protocolTorrentSingleFile(announce, name string, data []byte, pieceLength int) ([]byte, [20]byte, error) {
	if pieceLength <= 0 {
		return nil, [20]byte{}, fmt.Errorf("piece length must be positive")
	}
	var pieces []byte
	for off := 0; off < len(data); off += pieceLength {
		end := off + pieceLength
		if end > len(data) {
			end = len(data)
		}
		sum := sha1.Sum(data[off:end])
		pieces = append(pieces, sum[:]...)
	}

	info := bencode.NewDict()
	info.Set("length", bencode.NewInt(int64(len(data))))
	info.Set("name", bencode.NewString(name))
	info.Set("piece length", bencode.NewInt(int64(pieceLength)))
	info.Set("pieces", bencode.NewString(string(pieces)))

	top := bencode.NewDict()
	top.Set("announce", bencode.NewString(announce))
	top.Set("created by", bencode.NewString("aria2go conformance fixture"))
	top.Set("info", info)

	torrentData, err := bencode.Marshal(top)
	if err != nil {
		return nil, [20]byte{}, err
	}
	infoData, err := bencode.Marshal(info)
	if err != nil {
		return nil, [20]byte{}, err
	}
	return torrentData, sha1.Sum(infoData), nil
}

func protocolTrackerResponse(peerPort int) ([]byte, error) {
	ip := net.ParseIP("127.0.0.1").To4()
	if ip == nil {
		return nil, fmt.Errorf("cannot encode loopback peer")
	}
	var compact [6]byte
	copy(compact[:4], ip)
	binary.BigEndian.PutUint16(compact[4:], uint16(peerPort))

	resp := bencode.NewDict()
	resp.Set("interval", bencode.NewInt(1800))
	resp.Set("peers", bencode.NewString(string(compact[:])))
	return bencode.Marshal(resp)
}

func (f *protocolBTFixture) servePeer(ctx context.Context) {
	for {
		conn, err := f.peerLn.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		go f.handlePeer(ctx, conn)
	}
}

func (f *protocolBTFixture) handlePeer(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	var hs [68]byte
	if _, err := io.ReadFull(conn, hs[:]); err != nil {
		return
	}
	if hs[0] != 19 || string(hs[1:20]) != "BitTorrent protocol" {
		return
	}
	if !bytes.Equal(hs[28:48], f.InfoHash[:]) {
		return
	}

	var resp [68]byte
	resp[0] = 19
	copy(resp[1:20], "BitTorrent protocol")
	copy(resp[28:48], f.InfoHash[:])
	copy(resp[48:68], []byte("-AG0001-conformpeerX"))
	if _, err := conn.Write(resp[:]); err != nil {
		return
	}
	if err := f.writeBitfield(conn); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0, 0, 0, 1, 1}); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var lenBuf [4]byte
		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}
		msgLen := binary.BigEndian.Uint32(lenBuf[:])
		if msgLen == 0 {
			continue
		}
		payload := make([]byte, msgLen)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
		if len(payload) == 0 {
			continue
		}
		switch payload[0] {
		case 2:
			if _, err := conn.Write([]byte{0, 0, 0, 1, 1}); err != nil {
				return
			}
		case 6:
			if len(payload) < 13 {
				return
			}
			index := binary.BigEndian.Uint32(payload[1:5])
			begin := binary.BigEndian.Uint32(payload[5:9])
			length := binary.BigEndian.Uint32(payload[9:13])
			if err := f.writePiece(conn, index, begin, length); err != nil {
				return
			}
		}
	}
}

func (f *protocolBTFixture) writeBitfield(w io.Writer) error {
	numPieces := (len(f.payload) + f.piece - 1) / f.piece
	bitfield := make([]byte, (numPieces+7)/8)
	for i := 0; i < numPieces; i++ {
		bitfield[i/8] |= 1 << (7 - (i % 8))
	}
	msg := make([]byte, 4+1+len(bitfield))
	binary.BigEndian.PutUint32(msg[:4], uint32(1+len(bitfield)))
	msg[4] = 5
	copy(msg[5:], bitfield)
	_, err := w.Write(msg)
	return err
}

func (f *protocolBTFixture) writePiece(w io.Writer, index, begin, length uint32) error {
	offset := int64(index)*int64(f.piece) + int64(begin)
	if offset < 0 || offset >= int64(len(f.payload)) {
		return nil
	}
	end := offset + int64(length)
	if end > int64(len(f.payload)) {
		end = int64(len(f.payload))
	}
	block := f.payload[offset:end]
	msg := make([]byte, 4+1+8+len(block))
	binary.BigEndian.PutUint32(msg[:4], uint32(9+len(block)))
	msg[4] = 7
	binary.BigEndian.PutUint32(msg[5:9], index)
	binary.BigEndian.PutUint32(msg[9:13], begin)
	copy(msg[13:], block)
	_, err := w.Write(msg)
	return err
}
