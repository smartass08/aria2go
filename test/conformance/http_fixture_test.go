package conformance

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestHTTPFixtureEndpoints(t *testing.T) {
	payload := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	lastModified := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	fixture := NewHTTPFixture(t, HTTPFixtureOptions{
		Payload:             payload,
		Username:            "user",
		Password:            "secret",
		RequiredHeaders:     map[string]string{"X-Conformance": "yes"},
		Redirects:           2,
		DispositionFilename: "server-name.bin",
		LastModified:        lastModified,
		ETag:                `"fixture-etag"`,
		SlowChunkSize:       5,
		SlowDelay:           time.Millisecond,
	})
	client := &http.Client{Timeout: 5 * time.Second}

	status, header, body := fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteRange), map[string]string{
		"Range": "bytes=2-5",
	}, "", "")
	if status != http.StatusPartialContent {
		t.Fatalf("range status got %d want %d", status, http.StatusPartialContent)
	}
	if got := header.Get("Content-Range"); got != "bytes 2-5/"+strconv.Itoa(len(payload)) {
		t.Fatalf("Content-Range got %q", got)
	}
	if string(body) != "2345" {
		t.Fatalf("range body got %q", string(body))
	}
	if !fixture.SawRange(HTTPRouteRange) {
		t.Fatal("range endpoint did not record Range header")
	}

	status, _, _ = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteAuth), nil, "", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("auth challenge status got %d want %d", status, http.StatusUnauthorized)
	}
	status, _, body = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteAuth), nil, "user", "secret")
	if status != http.StatusOK {
		t.Fatalf("auth success status got %d want %d", status, http.StatusOK)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("auth body mismatch")
	}
	if !fixture.SawBasicAuth(HTTPRouteAuth, "user") {
		t.Fatal("auth endpoint did not record Basic auth")
	}

	status, _, _ = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteHeader), nil, "", "")
	if status != http.StatusForbidden {
		t.Fatalf("required header status got %d want %d", status, http.StatusForbidden)
	}
	status, _, body = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteHeader), map[string]string{
		"X-Conformance": "yes",
	}, "", "")
	if status != http.StatusOK {
		t.Fatalf("required header success status got %d want %d", status, http.StatusOK)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("header endpoint body mismatch")
	}
	if !fixture.SawHeader(HTTPRouteHeader, "X-Conformance", "yes") {
		t.Fatal("header endpoint did not record required header")
	}

	status, _, body = fixtureRequest(t, client, http.MethodGet, fixture.RedirectURL(), nil, "", "")
	if status != http.StatusOK {
		t.Fatalf("redirect final status got %d want %d", status, http.StatusOK)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("redirect body mismatch")
	}
	if got := fixture.Count(HTTPRouteRedirect); got != 3 {
		t.Fatalf("redirect count got %d want 3", got)
	}

	status, header, body = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteContentDisposition), nil, "", "")
	if status != http.StatusOK {
		t.Fatalf("content-disposition status got %d want %d", status, http.StatusOK)
	}
	if got := header.Get("Content-Disposition"); got != `attachment; filename="server-name.bin"` {
		t.Fatalf("Content-Disposition got %q", got)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("content-disposition body mismatch")
	}

	gzipTransport := &http.Transport{DisableCompression: true}
	t.Cleanup(gzipTransport.CloseIdleConnections)
	gzipClient := &http.Client{Timeout: 5 * time.Second, Transport: gzipTransport}
	status, header, body = fixtureRequest(t, gzipClient, http.MethodGet, fixture.URL(HTTPRouteGzip), nil, "", "")
	if status != http.StatusOK {
		t.Fatalf("gzip status got %d want %d", status, http.StatusOK)
	}
	if got := header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding got %q want gzip", got)
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	if err := zr.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("gzip decoded payload mismatch")
	}

	status, _, body = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteConditional), nil, "", "")
	if status != http.StatusOK {
		t.Fatalf("conditional initial status got %d want %d", status, http.StatusOK)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("conditional initial body mismatch")
	}
	status, _, body = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteConditional), map[string]string{
		"If-None-Match": `"fixture-etag"`,
	}, "", "")
	if status != http.StatusNotModified {
		t.Fatalf("conditional etag status got %d want %d", status, http.StatusNotModified)
	}
	if len(body) != 0 {
		t.Fatalf("conditional etag body got %d bytes want 0", len(body))
	}
	status, _, _ = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteConditional), map[string]string{
		"If-Modified-Since": lastModified.Format(http.TimeFormat),
	}, "", "")
	if status != http.StatusNotModified {
		t.Fatalf("conditional time status got %d want %d", status, http.StatusNotModified)
	}
	if !fixture.SawConditionalRequest(HTTPRouteConditional) {
		t.Fatal("conditional endpoint did not record validators")
	}

	status, _, body = fixtureRequest(t, client, http.MethodGet, fixture.URL(HTTPRouteSlow), nil, "", "")
	if status != http.StatusOK {
		t.Fatalf("slow status got %d want %d", status, http.StatusOK)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("slow body mismatch")
	}
}

func TestParseHTTPFixtureRange(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		size        int64
		wantStart   int64
		wantEnd     int64
		wantPartial bool
		wantOK      bool
	}{
		{name: "empty", size: 10, wantStart: 0, wantEnd: 9, wantOK: true},
		{name: "bounded", header: "bytes=2-5", size: 10, wantStart: 2, wantEnd: 5, wantPartial: true, wantOK: true},
		{name: "open ended", header: "bytes=7-", size: 10, wantStart: 7, wantEnd: 9, wantPartial: true, wantOK: true},
		{name: "suffix", header: "bytes=-3", size: 10, wantStart: 7, wantEnd: 9, wantPartial: true, wantOK: true},
		{name: "suffix longer than size", header: "bytes=-30", size: 10, wantStart: 0, wantEnd: 9, wantPartial: true, wantOK: true},
		{name: "multi range rejected", header: "bytes=0-1,3-4", size: 10, wantOK: false},
		{name: "past end rejected", header: "bytes=10-12", size: 10, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd, gotPartial, gotOK := parseHTTPFixtureRange(tt.header, tt.size)
			if gotOK != tt.wantOK || gotPartial != tt.wantPartial || gotStart != tt.wantStart || gotEnd != tt.wantEnd {
				t.Fatalf("parseHTTPFixtureRange() = (%d,%d,%v,%v), want (%d,%d,%v,%v)",
					gotStart, gotEnd, gotPartial, gotOK,
					tt.wantStart, tt.wantEnd, tt.wantPartial, tt.wantOK)
			}
		})
	}
}

func fixtureRequest(t *testing.T, client *http.Client, method string, url string, headers map[string]string, user string, pass string) (int, http.Header, []byte) {
	t.Helper()

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp.StatusCode, resp.Header.Clone(), body
}
