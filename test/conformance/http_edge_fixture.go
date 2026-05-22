package conformance

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	httpEdgePathPayload  = "/payload.bin"
	httpEdgePathRange    = "/range.bin"
	httpEdgePathRedirect = "/redirect"
	httpEdgePathStatus   = "/status.bin"
	httpEdgePathGzip     = "/gzip.bin"
	httpEdgePathDeflate  = "/deflate.bin"
	httpEdgePathCookie   = "/cookie.bin"
)

type httpEdgeFixtureOptions struct {
	payload        []byte
	redirects      int
	statusSequence []int
	requiredCookie string
	setCookie      string
}

type httpEdgeFixture struct {
	server *httptest.Server
	opts   httpEdgeFixtureOptions

	mu      sync.Mutex
	records []httpEdgeRequest
	status  map[string]int
}

type httpEdgeRequest struct {
	Method         string
	Path           string
	RawQuery       string
	Range          string
	AcceptEncoding string
	Cookie         string
}

func newHTTPEdgeFixture(t *testing.T, opts httpEdgeFixtureOptions) *httpEdgeFixture {
	t.Helper()

	if len(opts.payload) == 0 {
		opts.payload = []byte("aria2go HTTP edge payload\n")
	}
	if opts.redirects < 0 {
		opts.redirects = 0
	}

	f := &httpEdgeFixture{
		opts:   opts,
		status: make(map[string]int),
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.close)
	return f
}

func (f *httpEdgeFixture) close() {
	if f.server != nil {
		f.server.Close()
		f.server = nil
	}
}

func (f *httpEdgeFixture) url(path string) string {
	return f.server.URL + path
}

func (f *httpEdgeFixture) recordsFor(path string) []httpEdgeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]httpEdgeRequest, 0, len(f.records))
	for _, record := range f.records {
		if record.Path == path {
			out = append(out, record)
		}
	}
	return out
}

func (f *httpEdgeFixture) count(path string) int {
	return len(f.recordsFor(path))
}

func (f *httpEdgeFixture) sawAcceptEncoding(path, token string) bool {
	token = strings.ToLower(token)
	for _, record := range f.recordsFor(path) {
		for _, part := range strings.Split(record.AcceptEncoding, ",") {
			if strings.TrimSpace(strings.ToLower(part)) == token {
				return true
			}
		}
	}
	return false
}

func (f *httpEdgeFixture) sawCookie(path, cookie string) bool {
	for _, record := range f.recordsFor(path) {
		if record.Cookie == cookie || strings.Contains(record.Cookie, "; "+cookie) || strings.Contains(record.Cookie, cookie+";") {
			return true
		}
	}
	return false
}

func (f *httpEdgeFixture) rangeCount(path string) int {
	count := 0
	for _, record := range f.recordsFor(path) {
		if strings.HasPrefix(record.Range, "bytes=") {
			count++
		}
	}
	return count
}

func (f *httpEdgeFixture) methodsFor(path string) []string {
	records := f.recordsFor(path)
	methods := make([]string, 0, len(records))
	for _, record := range records {
		methods = append(methods, record.Method)
	}
	return methods
}

func (f *httpEdgeFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.record(r)

	switch r.URL.Path {
	case httpEdgePathPayload, httpEdgePathRange:
		f.servePayload(w, r, f.opts.payload)
	case httpEdgePathRedirect:
		f.handleRedirect(w, r)
	case httpEdgePathStatus:
		f.handleStatus(w, r)
	case httpEdgePathGzip:
		f.handleEncoded(w, r, "gzip")
	case httpEdgePathDeflate:
		f.handleEncoded(w, r, "deflate")
	case httpEdgePathCookie:
		f.handleCookie(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *httpEdgeFixture) record(r *http.Request) {
	record := httpEdgeRequest{
		Method:         r.Method,
		Path:           r.URL.Path,
		RawQuery:       r.URL.RawQuery,
		Range:          r.Header.Get("Range"),
		AcceptEncoding: r.Header.Get("Accept-Encoding"),
		Cookie:         r.Header.Get("Cookie"),
	}
	f.mu.Lock()
	f.records = append(f.records, record)
	f.mu.Unlock()
}

func (f *httpEdgeFixture) handleRedirect(w http.ResponseWriter, r *http.Request) {
	step := 0
	if raw := r.URL.Query().Get("step"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			step = parsed
		}
	}
	if step < f.opts.redirects {
		http.Redirect(w, r, fmt.Sprintf("%s?step=%d", httpEdgePathRedirect, step+1), http.StatusFound)
		return
	}
	f.servePayload(w, r, f.opts.payload)
}

func (f *httpEdgeFixture) handleStatus(w http.ResponseWriter, r *http.Request) {
	if status := f.nextStatus(httpEdgePathStatus); status != 0 {
		http.Error(w, http.StatusText(status), status)
		return
	}
	f.servePayload(w, r, f.opts.payload)
}

func (f *httpEdgeFixture) nextStatus(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	idx := f.status[path]
	if idx >= len(f.opts.statusSequence) {
		return 0
	}
	f.status[path] = idx + 1
	return f.opts.statusSequence[idx]
}

func (f *httpEdgeFixture) handleEncoded(w http.ResponseWriter, r *http.Request, encoding string) {
	if !headerContainsToken(r.Header.Get("Accept-Encoding"), encoding) {
		f.servePayload(w, r, f.opts.payload)
		return
	}

	var compressed bytes.Buffer
	switch encoding {
	case "gzip":
		zw := gzip.NewWriter(&compressed)
		_, _ = zw.Write(f.opts.payload)
		_ = zw.Close()
	case "deflate":
		zw, err := zlib.NewWriterLevel(&compressed, flate.DefaultCompression)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = zw.Write(f.opts.payload)
		_ = zw.Close()
	default:
		http.Error(w, "unsupported content encoding", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Encoding", encoding)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(compressed.Bytes())
	}
}

func (f *httpEdgeFixture) handleCookie(w http.ResponseWriter, r *http.Request) {
	if f.opts.requiredCookie != "" && !headerContainsCookie(r.Header.Get("Cookie"), f.opts.requiredCookie) {
		http.Error(w, "missing cookie", http.StatusForbidden)
		return
	}
	if f.opts.setCookie != "" {
		w.Header().Add("Set-Cookie", f.opts.setCookie)
	}
	f.servePayload(w, r, f.opts.payload)
}

func (f *httpEdgeFixture) servePayload(w http.ResponseWriter, r *http.Request, payload []byte) {
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/octet-stream")

	start, end, partial, ok := parseHTTPFixtureRange(r.Header.Get("Range"), int64(len(payload)))
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(payload)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	body := payload
	if partial {
		body = payload[start : end+1]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
	}
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, bytes.NewReader(body))
	}
}

func headerContainsToken(header, token string) bool {
	token = strings.ToLower(token)
	for _, part := range strings.Split(header, ",") {
		if strings.TrimSpace(strings.ToLower(part)) == token {
			return true
		}
	}
	return false
}

func headerContainsCookie(header, cookie string) bool {
	for _, part := range strings.Split(header, ";") {
		if strings.TrimSpace(part) == cookie {
			return true
		}
	}
	return false
}

type httpEdgeProxyRequest struct {
	Method string
	Target string
	Host   string
}

type httpEdgeProxyFixture struct {
	ln net.Listener

	mu      sync.Mutex
	records []httpEdgeProxyRequest
}

func newHTTPEdgeProxyFixture(t *testing.T) *httpEdgeProxyFixture {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen proxy fixture: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &httpEdgeProxyFixture{ln: ln}
	go p.serve(ctx)
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
	})
	return p
}

func (p *httpEdgeProxyFixture) url() string {
	return "http://" + p.ln.Addr().String()
}

func (p *httpEdgeProxyFixture) recordsSnapshot() []httpEdgeProxyRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]httpEdgeProxyRequest, len(p.records))
	copy(out, p.records)
	return out
}

func (p *httpEdgeProxyFixture) sawMethod(method string) bool {
	for _, record := range p.recordsSnapshot() {
		if record.Method == method {
			return true
		}
	}
	return false
}

func (p *httpEdgeProxyFixture) sawAbsoluteGET(rawURL string) bool {
	for _, record := range p.recordsSnapshot() {
		if record.Method == http.MethodGet && record.Target == rawURL {
			return true
		}
	}
	return false
}

func (p *httpEdgeProxyFixture) methodsAndTargets() []string {
	records := p.recordsSnapshot()
	out := make([]string, 0, len(records))
	for _, record := range records {
		out = append(out, record.Method+" "+record.Target)
	}
	return out
}

func (p *httpEdgeProxyFixture) serve(ctx context.Context) {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			return
		}
		go p.handleConn(ctx, conn)
	}
}

func (p *httpEdgeProxyFixture) handleConn(parent context.Context, client net.Conn) {
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

	headers := make([]string, 0, 16)
	host := ""
	for {
		h, err := br.ReadString('\n')
		if err != nil {
			return
		}
		headers = append(headers, h)
		if h == "\r\n" || h == "\n" {
			break
		}
		if name, value, ok := strings.Cut(h, ":"); ok && strings.EqualFold(name, "Host") {
			host = strings.TrimSpace(value)
		}
	}

	p.mu.Lock()
	p.records = append(p.records, httpEdgeProxyRequest{
		Method: method,
		Target: target,
		Host:   host,
	})
	p.mu.Unlock()

	if method == http.MethodConnect {
		p.handleConnect(ctx, client, br, target)
		return
	}

	p.handleForward(client, method, target, proto, headers)
}

func (p *httpEdgeProxyFixture) handleForward(client net.Conn, method, target, proto string, headers []string) {
	upstreamURL, err := url.Parse(target)
	if err != nil || upstreamURL.Host == "" {
		_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}

	addr := upstreamURL.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "80")
	}

	upstream, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer upstream.Close()
	_ = upstream.SetDeadline(time.Now().Add(10 * time.Second))

	requestTarget := upstreamURL.RequestURI()
	if requestTarget == "" {
		requestTarget = "/"
	}
	if _, err := fmt.Fprintf(upstream, "%s %s %s\r\n", method, requestTarget, proto); err != nil {
		return
	}
	for _, header := range headers {
		if _, err := io.WriteString(upstream, header); err != nil {
			return
		}
	}
	_, _ = io.Copy(client, upstream)
}

func (p *httpEdgeProxyFixture) handleConnect(parent context.Context, client net.Conn, br *bufio.Reader, target string) {
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
