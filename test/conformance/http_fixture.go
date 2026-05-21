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
	"time"
)

// HTTPRoute names a built-in endpoint exposed by HTTPFixture.
type HTTPRoute string

const (
	// HTTPRouteFile serves the fixture payload with normal HTTP range support.
	HTTPRouteFile HTTPRoute = "/file.bin"
	// HTTPRouteRange serves the fixture payload and is intended for split/range probes.
	HTTPRouteRange HTTPRoute = "/range.bin"
	// HTTPRouteAuth requires HTTP Basic authentication before serving the payload.
	HTTPRouteAuth HTTPRoute = "/auth.bin"
	// HTTPRouteHeader requires configured request headers before serving the payload.
	HTTPRouteHeader HTTPRoute = "/header.bin"
	// HTTPRouteRedirect redirects through a deterministic chain before serving the payload.
	HTTPRouteRedirect HTTPRoute = "/redirect"
	// HTTPRouteContentDisposition serves the payload with Content-Disposition.
	HTTPRouteContentDisposition HTTPRoute = "/content-disposition"
	// HTTPRouteGzip serves a gzip-encoded response body.
	HTTPRouteGzip HTTPRoute = "/gzip"
	// HTTPRouteConditional serves validators and honors conditional request headers.
	HTTPRouteConditional HTTPRoute = "/conditional"
	// HTTPRouteSlow streams the payload in chunks with a delay between chunks.
	HTTPRouteSlow HTTPRoute = "/slow"
)

// HTTPFixtureOptions configures the offline HTTP fixture server.
type HTTPFixtureOptions struct {
	Payload             []byte
	Username            string
	Password            string
	RequiredHeaders     map[string]string
	Redirects           int
	DispositionFilename string
	DispositionValue    string
	LastModified        time.Time
	ETag                string
	SlowChunkSize       int
	SlowDelay           time.Duration
}

// HTTPRequestRecord is a stable snapshot of one request received by HTTPFixture.
type HTTPRequestRecord struct {
	Method          string
	Path            string
	RawQuery        string
	Header          http.Header
	Range           string
	IfModifiedSince string
	IfNoneMatch     string
	BasicAuthUser   string
	HasBasicAuth    bool
	At              time.Time
}

// HTTPFixture is an offline HTTP oracle fixture for conformance tests.
type HTTPFixture struct {
	server *httptest.Server
	opts   HTTPFixtureOptions

	mu      sync.Mutex
	records []HTTPRequestRecord
}

// NewHTTPFixture starts an offline HTTP fixture server and registers cleanup on t.
func NewHTTPFixture(t *testing.T, opts HTTPFixtureOptions) *HTTPFixture {
	t.Helper()

	if len(opts.Payload) == 0 {
		opts.Payload = []byte("aria2go conformance payload\n")
	}
	if opts.Username == "" {
		opts.Username = "user"
	}
	if opts.Password == "" {
		opts.Password = "password"
	}
	if opts.Redirects < 0 {
		opts.Redirects = 0
	}
	if opts.DispositionFilename == "" {
		opts.DispositionFilename = "fixture-download.bin"
	}
	if opts.LastModified.IsZero() {
		opts.LastModified = time.Unix(1_700_000_000, 0).UTC()
	}
	opts.LastModified = opts.LastModified.UTC().Truncate(time.Second)
	if opts.ETag == "" {
		opts.ETag = fmt.Sprintf(`"conformance-%x"`, len(opts.Payload))
	}
	if opts.SlowChunkSize <= 0 {
		opts.SlowChunkSize = 1024
	}
	if opts.SlowDelay <= 0 {
		opts.SlowDelay = 5 * time.Millisecond
	}
	if opts.RequiredHeaders == nil {
		opts.RequiredHeaders = map[string]string{"X-Conformance": "yes"}
	}

	fixture := &HTTPFixture{opts: opts}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.handle))
	t.Cleanup(fixture.Close)
	return fixture
}

// Close stops the fixture server.
func (f *HTTPFixture) Close() {
	if f.server != nil {
		f.server.Close()
		f.server = nil
	}
}

// URL returns an absolute URL for a fixture route.
func (f *HTTPFixture) URL(route HTTPRoute) string {
	return f.server.URL + string(route)
}

// RedirectURL returns the first URL in the deterministic redirect chain.
func (f *HTTPFixture) RedirectURL() string {
	return f.URL(HTTPRouteRedirect)
}

// Records returns a copy of all request records observed by the fixture.
func (f *HTTPFixture) Records() []HTTPRequestRecord {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]HTTPRequestRecord, len(f.records))
	copy(out, f.records)
	return out
}

// RecordsFor returns request records matching route.
func (f *HTTPFixture) RecordsFor(route HTTPRoute) []HTTPRequestRecord {
	path := string(route)
	records := f.Records()
	out := make([]HTTPRequestRecord, 0, len(records))
	for _, record := range records {
		if record.Path == path {
			out = append(out, record)
		}
	}
	return out
}

// Count returns the number of requests received for route.
func (f *HTTPFixture) Count(route HTTPRoute) int {
	return len(f.RecordsFor(route))
}

// SawRange reports whether route received a byte Range request.
func (f *HTTPFixture) SawRange(route HTTPRoute) bool {
	for _, record := range f.RecordsFor(route) {
		if strings.HasPrefix(record.Range, "bytes=") {
			return true
		}
	}
	return false
}

// SawHeader reports whether route received a request header with value.
func (f *HTTPFixture) SawHeader(route HTTPRoute, name string, value string) bool {
	for _, record := range f.RecordsFor(route) {
		for _, got := range record.Header.Values(name) {
			if got == value {
				return true
			}
		}
	}
	return false
}

// SawBasicAuth reports whether route received Basic auth for username.
func (f *HTTPFixture) SawBasicAuth(route HTTPRoute, username string) bool {
	for _, record := range f.RecordsFor(route) {
		if record.HasBasicAuth && record.BasicAuthUser == username {
			return true
		}
	}
	return false
}

// SawConditionalRequest reports whether route received HTTP conditional headers.
func (f *HTTPFixture) SawConditionalRequest(route HTTPRoute) bool {
	for _, record := range f.RecordsFor(route) {
		if record.IfModifiedSince != "" || record.IfNoneMatch != "" {
			return true
		}
	}
	return false
}

func (f *HTTPFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.record(r)

	switch r.URL.Path {
	case string(HTTPRouteFile), string(HTTPRouteRange):
		f.servePayload(w, r, f.opts.Payload, payloadHeaders{})
	case string(HTTPRouteAuth):
		f.handleAuth(w, r)
	case string(HTTPRouteHeader):
		f.handleHeader(w, r)
	case string(HTTPRouteRedirect):
		f.handleRedirect(w, r)
	case string(HTTPRouteContentDisposition):
		headers := payloadHeaders{ContentDisposition: f.contentDisposition()}
		f.servePayload(w, r, f.opts.Payload, headers)
	case string(HTTPRouteGzip):
		f.handleGzip(w, r)
	case string(HTTPRouteConditional):
		headers := payloadHeaders{LastModified: f.opts.LastModified, ETag: f.opts.ETag, Conditional: true}
		f.servePayload(w, r, f.opts.Payload, headers)
	case string(HTTPRouteSlow):
		f.handleSlow(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *HTTPFixture) record(r *http.Request) {
	user, _, hasAuth := r.BasicAuth()
	record := HTTPRequestRecord{
		Method:          r.Method,
		Path:            r.URL.Path,
		RawQuery:        r.URL.RawQuery,
		Header:          r.Header.Clone(),
		Range:           r.Header.Get("Range"),
		IfModifiedSince: r.Header.Get("If-Modified-Since"),
		IfNoneMatch:     r.Header.Get("If-None-Match"),
		BasicAuthUser:   user,
		HasBasicAuth:    hasAuth,
		At:              time.Now().UTC(),
	}

	f.mu.Lock()
	f.records = append(f.records, record)
	f.mu.Unlock()
}

func (f *HTTPFixture) handleAuth(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	if !ok || user != f.opts.Username || pass != f.opts.Password {
		w.Header().Set("WWW-Authenticate", `Basic realm="conformance"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	f.servePayload(w, r, f.opts.Payload, payloadHeaders{})
}

func (f *HTTPFixture) handleHeader(w http.ResponseWriter, r *http.Request) {
	for name, want := range f.opts.RequiredHeaders {
		if got := r.Header.Get(name); got != want {
			http.Error(w, "missing required header", http.StatusForbidden)
			return
		}
	}
	f.servePayload(w, r, f.opts.Payload, payloadHeaders{})
}

func (f *HTTPFixture) handleRedirect(w http.ResponseWriter, r *http.Request) {
	step := 0
	if raw := r.URL.Query().Get("step"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 {
			step = parsed
		}
	}
	if step < f.opts.Redirects {
		next := fmt.Sprintf("%s?step=%d", f.URL(HTTPRouteRedirect), step+1)
		http.Redirect(w, r, next, http.StatusFound)
		return
	}
	http.Redirect(w, r, f.URL(HTTPRouteFile), http.StatusFound)
}

func (f *HTTPFixture) contentDisposition() string {
	if f.opts.DispositionValue != "" {
		return f.opts.DispositionValue
	}
	quoted := strings.ReplaceAll(f.opts.DispositionFilename, `\`, `\\`)
	quoted = strings.ReplaceAll(quoted, `"`, `\"`)
	return `attachment; filename="` + quoted + `"`
}

func (f *HTTPFixture) handleGzip(w http.ResponseWriter, r *http.Request) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, _ = zw.Write(f.opts.Payload)
	_ = zw.Close()

	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(compressed.Bytes())
	}
}

func (f *HTTPFixture) handleSlow(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(f.opts.Payload)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}

	flusher, _ := w.(http.Flusher)
	for start := 0; start < len(f.opts.Payload); start += f.opts.SlowChunkSize {
		end := start + f.opts.SlowChunkSize
		if end > len(f.opts.Payload) {
			end = len(f.opts.Payload)
		}
		if _, err := w.Write(f.opts.Payload[start:end]); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
		if end == len(f.opts.Payload) {
			return
		}
		timer := time.NewTimer(f.opts.SlowDelay)
		select {
		case <-timer.C:
		case <-r.Context().Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

type payloadHeaders struct {
	ContentDisposition string
	LastModified       time.Time
	ETag               string
	Conditional        bool
}

func (f *HTTPFixture) servePayload(w http.ResponseWriter, r *http.Request, payload []byte, headers payloadHeaders) {
	if headers.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", headers.ContentDisposition)
	}
	if !headers.LastModified.IsZero() {
		w.Header().Set("Last-Modified", headers.LastModified.Format(http.TimeFormat))
	}
	if headers.ETag != "" {
		w.Header().Set("ETag", headers.ETag)
	}
	if headers.Conditional && requestNotModified(r, headers.LastModified, headers.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

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

func requestNotModified(r *http.Request, lastModified time.Time, etag string) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" && etag != "" {
		for _, candidate := range strings.Split(inm, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || candidate == etag {
				return true
			}
		}
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && !lastModified.IsZero() {
		t, err := http.ParseTime(ims)
		if err == nil && !lastModified.After(t) {
			return true
		}
	}
	return false
}

func parseHTTPFixtureRange(header string, size int64) (start, end int64, partial, ok bool) {
	if header == "" {
		if size == 0 {
			return 0, -1, false, true
		}
		return 0, size - 1, false, true
	}
	if size <= 0 || !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false, false
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, false
	}

	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false, false
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true, true
	}

	parsedStart, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || parsedStart < 0 || parsedStart >= size {
		return 0, 0, false, false
	}
	parsedEnd := size - 1
	if parts[1] != "" {
		parsedEnd, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, false, false
		}
	}
	if parsedEnd < parsedStart || parsedEnd >= size {
		return 0, 0, false, false
	}
	return parsedStart, parsedEnd, true, true
}
