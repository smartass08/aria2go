package conformance

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	httpEdgePathPayload  = "/payload.bin"
	httpEdgePathRange    = "/range.bin"
	httpEdgePathRedirect = "/redirect"
	httpEdgePathStatus   = "/status.bin"
	httpEdgePathGzip     = "/gzip.bin"
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
		f.handleGzip(w, r)
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

func (f *httpEdgeFixture) handleGzip(w http.ResponseWriter, r *http.Request) {
	if !headerContainsToken(r.Header.Get("Accept-Encoding"), "gzip") {
		f.servePayload(w, r, f.opts.payload)
		return
	}

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, _ = zw.Write(f.opts.payload)
	_ = zw.Close()

	w.Header().Set("Content-Encoding", "gzip")
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
