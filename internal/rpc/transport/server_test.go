package transport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smartass08/aria2go/internal/rpc/jsonrpc"
	"github.com/smartass08/aria2go/internal/rpc/xmlrpc"
)

// ---- Mock Dispatcher ----

type mockDispatcher struct {
	call        func(method string, params []any) (any, error)
	subscribeFn func(ctx context.Context) (<-chan Notification, error)
}

func (m *mockDispatcher) Call(method string, params []any) (any, error) {
	if m.call != nil {
		return m.call(method, params)
	}
	return nil, errors.New("method not found")
}

func (m *mockDispatcher) SubscribeNotifications(ctx context.Context) (<-chan Notification, error) {
	if m.subscribeFn != nil {
		return m.subscribeFn(ctx)
	}
	ch := make(chan Notification)
	return ch, nil
}

// ---- Test Helpers ----

func newTestServer(dispatcher Dispatcher) *httptest.Server {
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     dispatcher,
	}
	srv, err := New(cfg)
	if err != nil {
		panic(err)
	}

	// We use httptest.Server for HTTP, and for WS we create a raw listener.
	// For now, use httptest.NewServer with the mux from srv.http.Handler.
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	return ts
}

func closeTestServer(ts *httptest.Server) {
	ts.Close()
}

// ---- JSON-RPC Tests ----

func TestJSONRPCSingleRequest(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method == "aria2.getVersion" {
				return "1.37.0", nil
			}
			return nil, errors.New("not found")
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	body := `{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]}`
	resp, err := http.Post(ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if r.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q", r.JSONRPC)
	}
	if r.Error != nil {
		t.Fatalf("unexpected error: %+v", r.Error)
	}
	if r.Result != "1.37.0" {
		t.Errorf("result = %v, want 1.37.0", r.Result)
	}
}

func TestJSONRPCNotification(t *testing.T) {
	disp := &mockDispatcher{}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	body := `{"jsonrpc":"2.0","method":"aria2.onDownloadStart","params":[{"gid":"abc123"}]}`
	resp, err := http.Post(ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	// aria2 rejects notifications with -32600 Invalid Request.
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestJSONRPCBatchRequest(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			switch method {
			case "aria2.getVersion":
				return "1.37.0", nil
			case "aria2.getGlobalStat":
				return map[string]any{"numActive": "0"}, nil
			default:
				return nil, errors.New("not found")
			}
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	body := `[{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]},{"jsonrpc":"2.0","id":"2","method":"aria2.getGlobalStat","params":[]}]`
	resp, err := http.Post(ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var batch []jsonrpc.Response
	if err := json.Unmarshal(b, &batch); err != nil {
		t.Fatalf("unmarshal batch response: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2", len(batch))
	}
	if batch[0].Result != "1.37.0" {
		t.Errorf("batch[0].result = %v", batch[0].Result)
	}
}

func TestJSONRPCBatchRequestMixedInvalidEntries(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method != "aria2.getVersion" {
				return nil, fmt.Errorf("unexpected method %q", method)
			}
			return "1.37.0", nil
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	body := `[
		{"jsonrpc":"2.0","id":"ok","method":"aria2.getVersion","params":[]},
		{"jsonrpc":"2.0","id":"bad","params":[]}
	]`
	resp, err := http.Post(ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var batch []jsonrpc.Response
	if err := json.Unmarshal(b, &batch); err != nil {
		t.Fatalf("unmarshal batch response: %v; body=%s", err, string(b))
	}
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2", len(batch))
	}
	if batch[0].Error != nil || batch[0].Result != "1.37.0" {
		t.Fatalf("batch[0] = %+v, want success result", batch[0])
	}
	if batch[1].Error == nil || batch[1].Error.Code != jsonrpc.ErrCodeInvalidRequest {
		t.Fatalf("batch[1] = %+v, want invalid request error", batch[1])
	}
	if batch[1].ID != "bad" {
		t.Fatalf("batch[1].id = %#v, want %q", batch[1].ID, "bad")
	}
}

func TestJSONRPCEmptyBatchReturnsEmptyArray(t *testing.T) {
	disp := &mockDispatcher{}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(`[]`))
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(b))
	}
	if string(b) != "[]" {
		t.Fatalf("body = %s, want []", string(b))
	}
}

func TestJSONRPCParseError(t *testing.T) {
	disp := &mockDispatcher{}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	body := `not json`
	resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if r.Error == nil || r.Error.Code != jsonrpc.ErrCodeParse {
		t.Errorf("error code = %d, want %d", getErrorCode(r.Error), jsonrpc.ErrCodeParse)
	}
}

func TestOversizedRPCPOSTClosesWithoutResponse(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return nil, fmt.Errorf("dispatcher should not be called for oversized %s", method)
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
		MaxRequestSize: 16,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	cases := []struct {
		name        string
		path        string
		contentType string
	}{
		{name: "jsonrpc", path: "/jsonrpc", contentType: "application/json-rpc"},
		{name: "xmlrpc", path: "/rpc", contentType: "text/xml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatalf("dial raw HTTP: %v", err)
			}
			defer conn.Close()

			req := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: %s\r\nContent-Length: 32\r\n\r\n", tc.path, addr, tc.contentType)
			if _, err := io.WriteString(conn, req); err != nil {
				t.Fatalf("write oversized request header: %v", err)
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.CloseWrite()
			}

			conn.SetReadDeadline(time.Now().Add(time.Second))
			raw, err := io.ReadAll(conn)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					t.Fatalf("read timed out waiting for connection close")
				}
				t.Fatalf("read oversized request response: %v", err)
			}
			if len(raw) != 0 {
				t.Fatalf("expected no HTTP response, got %q", string(raw))
			}
		})
	}
}

func TestJSONRPCSingleInvalidRequestPreservesID(t *testing.T) {
	disp := &mockDispatcher{}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(`{"jsonrpc":"2.0","id":"bad","params":[]}`))
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(b))
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, string(b))
	}
	if r.Error == nil || r.Error.Code != jsonrpc.ErrCodeInvalidRequest {
		t.Fatalf("error = %+v, want invalid request", r.Error)
	}
	if r.ID != "bad" {
		t.Fatalf("id = %#v, want %q", r.ID, "bad")
	}
}

func TestJSONRPCSingleInvalidParamsPreservesID(t *testing.T) {
	disp := &mockDispatcher{}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(`{"jsonrpc":"2.0","id":"badp","method":"aria2.getVersion","params":{"k":"v"}}`))
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(b))
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", resp.StatusCode, string(b))
	}
	if r.Error == nil || r.Error.Code != jsonrpc.ErrCodeInvalidParams {
		t.Fatalf("error = %+v, want invalid params", r.Error)
	}
	if r.ID != "badp" {
		t.Fatalf("id = %#v, want %q", r.ID, "badp")
	}
}

func TestJSONRPCScalarTopLevelReturnsInvalidRequest(t *testing.T) {
	disp := &mockDispatcher{}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(`"str"`))
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(b))
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, string(b))
	}
	if r.Error == nil || r.Error.Code != jsonrpc.ErrCodeInvalidRequest {
		t.Fatalf("error = %+v, want invalid request", r.Error)
	}
	if r.ID != nil {
		t.Fatalf("id = %#v, want nil", r.ID)
	}
}

func TestJSONRPCErrorResponse(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return nil, &testRPCError{code: 1, msg: "something went wrong"}
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	body := `{"jsonrpc":"2.0","id":"1","method":"aria2.addUri","params":[["http://example.com"]]}`
	resp, err := http.Post(ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if r.Error == nil {
		t.Fatal("expected error response")
	}
	if r.Error.Code != 1 {
		t.Errorf("error code = %d, want 1", r.Error.Code)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("http status = %d, want 400", resp.StatusCode)
	}
}

func TestJSONRPCTokenAuth(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return "ok", nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
		Secret:         "mysecret",
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	// Without token — should fail.
	body := `{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]}`
	resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing-token status = %d, want 400; body=%s", resp.StatusCode, string(b))
	}
	if r.Error == nil {
		t.Fatal("expected error for missing token")
	}

	// Wrong token should also use the normal JSON-RPC method-error status.
	body = `{"jsonrpc":"2.0","id":"3","method":"aria2.getVersion","params":["token:wrong"]}`
	resp = doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var r3 jsonrpc.Response
	if err := json.Unmarshal(b, &r3); err != nil {
		t.Fatalf("unmarshal wrong-token response: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong-token status = %d, want 400; body=%s", resp.StatusCode, string(b))
	}
	if r3.Error == nil {
		t.Fatal("expected error for wrong token")
	}

	// With token — should succeed.
	body = `{"jsonrpc":"2.0","id":"2","method":"aria2.getVersion","params":["token:mysecret"]}`
	resp = doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var r2 jsonrpc.Response
	if err := json.Unmarshal(b, &r2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r2.Error != nil {
		t.Fatalf("unexpected error: %+v", r2.Error)
	}
	if r2.Result != "ok" {
		t.Errorf("result = %v, want ok", r2.Result)
	}
}

func TestJSONRPCSystemMulticallNestedAuthErrorKeepsHTTP200(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method != "system.multicall" {
				return nil, fmt.Errorf("method = %q, want system.multicall", method)
			}
			return []any{
				map[string]any{"code": int64(1), "message": "Unauthorized"},
				[]any{map[string]any{"numActive": "0"}},
			}, nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
		Secret:         "mysecret",
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":"1","method":"system.multicall","params":[[{"methodName":"aria2.getGlobalStat","params":[]}]]}`
	resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(b))
	}
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(b))
	}
	if r.Error != nil {
		t.Fatalf("unexpected top-level error: %+v", r.Error)
	}
}

func TestJSONRPCMethodNotFoundReturnsHTTP404(t *testing.T) {
	for _, message := range []string{"method not found", "No such method: aria2.noSuchMethod"} {
		t.Run(message, func(t *testing.T) {
			disp := &mockDispatcher{
				call: func(method string, params []any) (any, error) {
					return nil, errors.New(message)
				},
			}
			ts := newTestServer(disp)
			defer closeTestServer(ts)

			body := `{"jsonrpc":"2.0","id":"1","method":"aria2.noSuchMethod","params":[]}`
			resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
			defer resp.Body.Close()

			b, _ := io.ReadAll(resp.Body)
			var r jsonrpc.Response
			if err := json.Unmarshal(b, &r); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body=%s", resp.StatusCode, string(b))
			}
			if r.Error == nil || r.Error.Code != jsonrpc.ErrCodeMethodNotFound {
				t.Fatalf("error = %+v, want method not found", r.Error)
			}
		})
	}
}

func TestHeaderHasToken(t *testing.T) {
	h := http.Header{}
	h.Add("Connection", "keep-alive, Upgrade")
	if !headerHasToken(h, "Connection", "upgrade") {
		t.Fatal("expected comma-separated Connection header to contain upgrade token")
	}
	if headerHasToken(h, "Connection", "close") {
		t.Fatal("did not expect close token")
	}
}

func TestJSONRPCSystemListMethodsAreAuthless(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method == "system.listMethods" || method == "system.listNotifications" {
				return []string{"system.listMethods", "system.listNotifications"}, nil
			}
			return nil, errors.New("not found")
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
		Secret:         "mysecret",
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, method := range []string{"system.listMethods", "system.listNotifications"} {
		t.Run(method, func(t *testing.T) {
			authless := srv.processSingleJSONRPC(&jsonrpc.Request{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`"1"`),
				Method:  method,
				Params:  json.RawMessage(`[]`),
			})
			if authless.Error != nil {
				t.Fatalf("authless response error: %+v", authless.Error)
			}
		})
	}
}

func TestJSONPGetUsesRawQueryWithoutQuestionMark(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method != "system.listMethods" {
				return nil, fmt.Errorf("method = %q, want system.listMethods", method)
			}
			return []string{"system.listMethods"}, nil
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	resp := doTestRequest(t, ts.Client(), http.MethodGet, ts.URL+"/jsonrpc?method=system.listMethods&id=1", "", nil)
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(b))
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(b))
	}
	if r.Error != nil {
		t.Fatalf("unexpected error: %+v", r.Error)
	}
}

func TestJSONPGetPreservesRawPercentEncodedID(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method != "system.listMethods" {
				return nil, fmt.Errorf("method = %q, want system.listMethods", method)
			}
			return []string{"system.listMethods"}, nil
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	resp := doTestRequest(t, ts.Client(), http.MethodGet, ts.URL+"/jsonrpc?method=system.listMethods&id=a%2Fb", "", nil)
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(b))
	}
	if r.ID != "a%2Fb" {
		t.Fatalf("id = %#v, want %q", r.ID, "a%2Fb")
	}
}

func TestJSONPInvalidBase64ParamsReturnInvalidRequest(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return nil, fmt.Errorf("dispatcher should not be called for invalid JSONP params")
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	resp := doTestRequest(t, ts.Client(), http.MethodGet, ts.URL+"/jsonrpc?method=system.listMethods&id=x&params=not_base64", "", nil)
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var r jsonrpc.Response
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(b))
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, string(b))
	}
	if r.Error == nil || r.Error.Code != jsonrpc.ErrCodeInvalidRequest {
		t.Fatalf("error = %+v, want invalid request", r.Error)
	}
	if r.ID != nil {
		t.Fatalf("id = %#v, want nil", r.ID)
	}
}

// ---- XML-RPC Tests ----

func TestXMLRPCCall(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method == "aria2.getVersion" {
				return "1.37.0", nil
			}
			return nil, errors.New("not found")
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	body := `<?xml version="1.0"?><methodCall><methodName>aria2.getVersion</methodName><params/></methodCall>`
	resp, err := http.Post(ts.URL+"/rpc", "text/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "1.37.0") {
		t.Errorf("response does not contain version: %s", string(b))
	}
}

func TestXMLRPCUploadPayloadIsNormalizedOnceForDispatcher(t *testing.T) {
	rawPayload := []byte("dummy-torrent")
	encodedPayload := base64.StdEncoding.EncodeToString(rawPayload)
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method != "aria2.addTorrent" {
				return nil, fmt.Errorf("method = %q, want aria2.addTorrent", method)
			}
			if len(params) != 2 {
				return nil, fmt.Errorf("params len = %d, want 2", len(params))
			}
			if got, want := params[0], "token:secret"; got != want {
				return nil, fmt.Errorf("token = %#v, want %q", got, want)
			}
			if got, want := params[1], encodedPayload; got != want {
				return nil, fmt.Errorf("payload = %#v, want %q", got, want)
			}
			return "ok", nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	body := `<?xml version="1.0"?><methodCall><methodName>aria2.addTorrent</methodName><params>` +
		`<param><value><string>token:secret</string></value></param>` +
		`<param><value><base64>` + encodedPayload + `</base64></value></param>` +
		`</params></methodCall>`
	resp, err := http.Post(ts.URL+"/rpc", "text/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(b))
	}
}

func TestXMLRPCPathViaRoot(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return "1.37.0", nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	body := `<?xml version="1.0"?><methodCall><methodName>aria2.getVersion</methodName><params/></methodCall>`
	resp, err := http.Post(ts.URL+"/", "text/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (aria2 does not support XML-RPC at root path)", resp.StatusCode)
	}
}

func TestXMLRPCFault(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return nil, &testRPCError{code: 1, msg: "method error"}
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	body := `<?xml version="1.0"?><methodCall><methodName>aria2.addUri</methodName><params/></methodCall>`
	resp, err := http.Post(ts.URL+"/rpc", "text/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "<fault>") {
		t.Errorf("expected fault in response: %s", string(b))
	}
}

func TestXMLRPCMalformedXMLReturnsHTTP400(t *testing.T) {
	disp := &mockDispatcher{}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	resp := doTestRequest(t, ts.Client(), http.MethodPost, ts.URL+"/rpc", "text/xml", strings.NewReader(`<methodCall><methodName>`))
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.StatusCode, string(b))
	}
	if strings.Contains(string(b), "<fault>") {
		t.Fatalf("malformed XML returned XML-RPC fault body: %s", string(b))
	}
}

func TestXMLRPCTokenAuth(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return "ok", nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
		Secret:         "mysecret",
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	// Without token — should get fault.
	body := `<?xml version="1.0"?><methodCall><methodName>aria2.getVersion</methodName><params/></methodCall>`
	resp, _ := http.Post(ts.URL+"/rpc", "text/xml", strings.NewReader(body))
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "<fault>") {
		t.Error("expected fault for missing token")
	}

	// With token — should succeed.
	body = `<?xml version="1.0"?><methodCall><methodName>aria2.getVersion</methodName><params><param><value><string>token:mysecret</string></value></param></params></methodCall>`
	resp, _ = http.Post(ts.URL+"/rpc", "text/xml", strings.NewReader(body))
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(b), "<fault>") {
		t.Errorf("unexpected fault: %s", string(b))
	}
}

// ---- CORS Tests ----

func TestCORSPreflight(t *testing.T) {
	disp := &mockDispatcher{}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/jsonrpc", nil)
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("allow-origin = %q, want *", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if resp.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Error("missing Access-Control-Allow-Methods")
	}
}

func TestCORSResponseHeaders(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return "ok", nil
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	body := `{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]}`
	resp, err := http.Post(ts.URL+"/jsonrpc", "application/json-rpc", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("allow-origin = %q", resp.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSSpecificOrigin(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return "ok", nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"http://localhost:3000"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	body := `{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]}`

	// Request from localhost should pass.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/jsonrpc", strings.NewReader(body))
	req.Header.Set("Origin", "http://localhost:3000")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Errorf("allow-origin = %q", resp.Header.Get("Access-Control-Allow-Origin"))
	}

	// Request from other origin should NOT get CORS header.
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/jsonrpc", strings.NewReader(body))
	req2.Header.Set("Origin", "http://evil.com")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("unexpected allow-origin header: %q", resp2.Header.Get("Access-Control-Allow-Origin"))
	}
}

// ---- WebSocket Tests ----

func TestWebSocketUpgradeHandshake(t *testing.T) {
	disp := &mockDispatcher{
		subscribeFn: func(ctx context.Context) (<-chan Notification, error) {
			ch := make(chan Notification)
			return ch, nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewUnstartedServer(srv.http.Handler)
	ts.EnableHTTP2 = false
	ts.Start()
	defer ts.Close()

	wsURL := "http" + strings.TrimPrefix(ts.URL, "http")
	clientKey := "dGhlIHNhbXBsZSBub25jZQ==" // "the sample nonce" base64

	req, _ := http.NewRequest(http.MethodGet, wsURL+"/jsonrpc", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", clientKey)
	req.Header.Set("Sec-WebSocket-Version", "13")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET upgrade failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}

	expectedKey := computeAcceptKey(clientKey)
	actualKey := resp.Header.Get("Sec-WebSocket-Accept")
	if actualKey != expectedKey {
		t.Errorf("accept key = %q, want %q", actualKey, expectedKey)
	}
}

func TestWebSocketUpgradeNonGet(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return "ok", nil
		},
	}
	ts := newTestServer(disp)
	defer closeTestServer(ts)

	body := `{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/jsonrpc", strings.NewReader(body))
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	// POST with upgrade headers should still be treated as POST, not upgrade.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestComputeAcceptKey(t *testing.T) {
	clientKey := "dGhlIHNhbXBsZSBub25jZQ=="
	expected := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	got := computeAcceptKey(clientKey)
	if got != expected {
		t.Errorf("accept key = %q, want %q", got, expected)
	}
}

// ---- WebSocket Frame Tests ----

func TestWSFrameReadWrite(t *testing.T) {
	// Create a pipe.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		frame := wsFrame{
			fin:     true,
			opcode:  opText,
			payload: []byte("hello"),
		}
		if err := writeFramex(server, frame); err != nil {
			t.Errorf("writeFramex: %v", err)
		}
	}()

	reader := bufio.NewReader(client)
	frame, err := readFramex(reader)
	if err != nil {
		t.Fatalf("readFramex: %v", err)
	}
	if !frame.fin {
		t.Error("expected fin = true")
	}
	if frame.opcode != opText {
		t.Errorf("opcode = 0x%x, want 0x%x", frame.opcode, opText)
	}
	if string(frame.payload) != "hello" {
		t.Errorf("payload = %q, want %q", string(frame.payload), "hello")
	}
}

func TestWSFrameMasking(t *testing.T) {
	// Write a masked frame, read back unmasked.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	payload := []byte("secret")
	maskKey := [4]byte{0x01, 0x02, 0x03, 0x04}
	masked := make([]byte, len(payload))
	copy(masked, payload)
	applyMask(masked, maskKey)

	go func() {
		writer := bufio.NewWriter(server)
		// Build masked frame header manually.
		header := []byte{0x81, 0x86} // FIN + text opcode, MASK bit + len=6
		writer.Write(header)
		writer.Write(maskKey[:])
		writer.Write(masked)
		writer.Flush()
	}()

	reader := bufio.NewReader(client)
	frame, err := readFramex(reader)
	if err != nil {
		t.Fatalf("readFramex: %v", err)
	}
	if string(frame.payload) != "secret" {
		t.Errorf("payload = %q, want %q", string(frame.payload), "secret")
	}
}

func TestWebSocketRejectsUnmaskedClientFrame(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return "ok", nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		_ = srv.http.Serve(ln)
	}()
	go srv.broadcastNotifications(ctx)

	conn, reader := openRawWebSocket(t, ln.Addr().String())
	defer conn.Close()

	frame := wsFrame{
		fin:     true,
		opcode:  opText,
		payload: []byte(`{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]}`),
	}
	if err := writeFramex(conn, frame); err != nil {
		t.Fatalf("write unmasked frame: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	got, err := readFramex(reader)
	if err != nil {
		t.Fatalf("read close frame: %v", err)
	}
	defer got.free()
	if got.opcode != opClose {
		t.Fatalf("opcode = 0x%x, want close", got.opcode)
	}
	if len(got.payload) < 2 {
		t.Fatalf("close payload too short: %d", len(got.payload))
	}
	code := int(got.payload[0])<<8 | int(got.payload[1])
	if code != closeProtocolErr {
		t.Fatalf("close code = %d, want %d", code, closeProtocolErr)
	}
}

func TestWebSocketFragmentedMessagesAreAccumulated(t *testing.T) {
	cases := []struct {
		name   string
		opcode byte
	}{
		{name: "text", opcode: opText},
		{name: "binary", opcode: opBinary},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			disp := &mockDispatcher{
				call: func(method string, params []any) (any, error) {
					if method != "aria2.getVersion" {
						return nil, fmt.Errorf("method = %q, want aria2.getVersion", method)
					}
					return "1.37.0", nil
				},
			}
			cfg := Config{
				Listen:         ":0",
				AllowedOrigins: []string{"*"},
				Dispatcher:     disp,
			}
			srv, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()

			go func() {
				_ = srv.http.Serve(ln)
			}()
			go srv.broadcastNotifications(ctx)

			conn, reader := openRawWebSocket(t, ln.Addr().String())
			defer conn.Close()

			payload := []byte(`{"jsonrpc":"2.0","id":"1","method":"aria2.getVersion","params":[]}`)
			split := len(payload) / 2
			if err := writeMaskedClientFrameWithFIN(conn, false, tc.opcode, payload[:split]); err != nil {
				t.Fatalf("write first fragment: %v", err)
			}
			if err := writeMaskedClientFrameWithFIN(conn, true, opContinuation, payload[split:]); err != nil {
				t.Fatalf("write final continuation: %v", err)
			}

			conn.SetReadDeadline(time.Now().Add(time.Second))
			got, err := readFramex(reader)
			if err != nil {
				t.Fatalf("read fragmented response frame: %v", err)
			}
			defer got.free()
			if got.opcode != opText {
				t.Fatalf("opcode = 0x%x, want text", got.opcode)
			}
			var r jsonrpc.Response
			if err := json.Unmarshal(got.payload, &r); err != nil {
				t.Fatalf("unmarshal fragmented ws response: %v; body=%s", err, string(got.payload))
			}
			if r.Error != nil {
				t.Fatalf("unexpected error: %+v", r.Error)
			}
			if r.Result != "1.37.0" {
				t.Fatalf("result = %#v, want %q", r.Result, "1.37.0")
			}
		})
	}
}

func TestWebSocketSystemListMethodsAreAuthless(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method != "system.listMethods" {
				return nil, fmt.Errorf("method = %q, want system.listMethods", method)
			}
			return []string{"system.listMethods"}, nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
		Secret:         "mysecret",
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		_ = srv.http.Serve(ln)
	}()
	go srv.broadcastNotifications(ctx)

	conn, reader := openRawWebSocket(t, ln.Addr().String())
	defer conn.Close()

	payload := []byte(`{"jsonrpc":"2.0","id":"1","method":"system.listMethods","params":[]}`)
	if err := writeMaskedClientFrame(conn, opText, payload); err != nil {
		t.Fatalf("write masked frame: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	got, err := readFramex(reader)
	if err != nil {
		t.Fatalf("read response frame: %v", err)
	}
	defer got.free()
	if got.opcode != opText {
		t.Fatalf("opcode = 0x%x, want text", got.opcode)
	}
	var r jsonrpc.Response
	if err := json.Unmarshal(got.payload, &r); err != nil {
		t.Fatalf("unmarshal ws response: %v; body=%s", err, string(got.payload))
	}
	if r.Error != nil {
		t.Fatalf("unexpected error: %+v", r.Error)
	}
}

func TestWebSocketBatchMixedInvalidEntries(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method != "aria2.getVersion" {
				return nil, fmt.Errorf("method = %q, want aria2.getVersion", method)
			}
			return "1.37.0", nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		_ = srv.http.Serve(ln)
	}()
	go srv.broadcastNotifications(ctx)

	conn, reader := openRawWebSocket(t, ln.Addr().String())
	defer conn.Close()

	payload := []byte(`[
		{"jsonrpc":"2.0","id":"ok","method":"aria2.getVersion","params":[]},
		{"jsonrpc":"2.0","id":"bad","params":[]}
	]`)
	if err := writeMaskedClientFrame(conn, opText, payload); err != nil {
		t.Fatalf("write masked frame: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	got, err := readFramex(reader)
	if err != nil {
		t.Fatalf("read response frame: %v", err)
	}
	defer got.free()
	if got.opcode != opText {
		t.Fatalf("opcode = 0x%x, want text", got.opcode)
	}
	var batch []jsonrpc.Response
	if err := json.Unmarshal(got.payload, &batch); err != nil {
		t.Fatalf("unmarshal ws batch: %v; body=%s", err, string(got.payload))
	}
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2", len(batch))
	}
	if batch[0].Error != nil || batch[0].Result != "1.37.0" {
		t.Fatalf("batch[0] = %+v, want success result", batch[0])
	}
	if batch[1].Error == nil || batch[1].Error.Code != jsonrpc.ErrCodeInvalidRequest {
		t.Fatalf("batch[1] = %+v, want invalid request error", batch[1])
	}
	if batch[1].ID != "bad" {
		t.Fatalf("batch[1].id = %#v, want %q", batch[1].ID, "bad")
	}
}

func TestWebSocketBatchMissingIDReturnsInvalidRequest(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			if method != "aria2.getVersion" {
				return nil, fmt.Errorf("method = %q, want aria2.getVersion", method)
			}
			return "1.37.0", nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		_ = srv.http.Serve(ln)
	}()
	go srv.broadcastNotifications(ctx)

	conn, reader := openRawWebSocket(t, ln.Addr().String())
	defer conn.Close()

	payload := []byte(`[
		{"jsonrpc":"2.0","id":"ok","method":"aria2.getVersion","params":[]},
		{"jsonrpc":"2.0","method":"aria2.getVersion","params":[]}
	]`)
	if err := writeMaskedClientFrame(conn, opText, payload); err != nil {
		t.Fatalf("write masked frame: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(time.Second))
	got, err := readFramex(reader)
	if err != nil {
		t.Fatalf("read response frame: %v", err)
	}
	defer got.free()
	if got.opcode != opText {
		t.Fatalf("opcode = 0x%x, want text", got.opcode)
	}
	var batch []jsonrpc.Response
	if err := json.Unmarshal(got.payload, &batch); err != nil {
		t.Fatalf("unmarshal ws batch: %v; body=%s", err, string(got.payload))
	}
	if len(batch) != 2 {
		t.Fatalf("len(batch) = %d, want 2", len(batch))
	}
	if batch[0].Error != nil || batch[0].Result != "1.37.0" {
		t.Fatalf("batch[0] = %+v, want success result", batch[0])
	}
	if batch[1].Error == nil || batch[1].Error.Code != jsonrpc.ErrCodeInvalidRequest {
		t.Fatalf("batch[1] = %+v, want invalid request error", batch[1])
	}
	if batch[1].ID != nil {
		t.Fatalf("batch[1].id = %#v, want nil", batch[1].ID)
	}
}

func TestWSFramePingPong(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	pingPayload := []byte("pingdata")

	go func() {
		// Send ping.
		frame := wsFrame{
			fin:     true,
			opcode:  opPing,
			payload: pingPayload,
		}
		if err := writeFramex(server, frame); err != nil {
			t.Errorf("writeFramex ping: %v", err)
		}

		// Read pong.
		reader := bufio.NewReader(server)
		resp, err := readFramex(reader)
		if err != nil {
			t.Errorf("readFramex pong: %v", err)
			return
		}
		if resp.opcode != opPong {
			t.Errorf("expected pong (0xA), got 0x%x", resp.opcode)
		}
		if string(resp.payload) != string(pingPayload) {
			t.Errorf("pong payload = %q, want %q", string(resp.payload), string(pingPayload))
		}
	}()

	// Read ping.
	reader := bufio.NewReader(client)
	frame, err := readFramex(reader)
	if err != nil {
		t.Fatalf("readFramex ping: %v", err)
	}
	if frame.opcode != opPing {
		t.Fatalf("expected ping (0x9), got 0x%x", frame.opcode)
	}

	// Send pong.
	pongFrame := wsFrame{
		fin:     true,
		opcode:  opPong,
		payload: frame.payload,
	}
	if err := writeFramex(client, pongFrame); err != nil {
		t.Fatalf("writeFramex pong: %v", err)
	}
}

func TestWSFrameCloseWithCode(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		payload := make([]byte, 4)
		// status code 1000 + reason "bye"
		payload[0] = 0x03
		payload[1] = 0xE8
		copy(payload[2:], "bye")
		frame := wsFrame{
			fin:     true,
			opcode:  opClose,
			payload: payload,
		}
		writeFramex(server, frame)
	}()

	reader := bufio.NewReader(client)
	frame, err := readFramex(reader)
	if err != nil {
		t.Fatalf("readFramex: %v", err)
	}
	if frame.opcode != opClose {
		t.Errorf("opcode = 0x%x, want 0x%x", frame.opcode, opClose)
	}
	if len(frame.payload) < 2 {
		t.Fatalf("payload too short: %d bytes", len(frame.payload))
	}
	code := int(frame.payload[0])<<8 | int(frame.payload[1])
	if code != closeNormal {
		t.Errorf("close code = %d, want %d", code, closeNormal)
	}
}

func TestWSFrameExtendedLength126(t *testing.T) {
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		frame := wsFrame{
			fin:     true,
			opcode:  opBinary,
			payload: payload,
		}
		writeFramex(server, frame)
	}()

	reader := bufio.NewReader(client)
	frame, err := readFramex(reader)
	if err != nil {
		t.Fatalf("readFramex: %v", err)
	}
	if len(frame.payload) != 256 {
		t.Errorf("payload len = %d, want 256", len(frame.payload))
	}
	for i := range frame.payload {
		if frame.payload[i] != payload[i] {
			t.Errorf("payload[%d] = %d, want %d", i, frame.payload[i], payload[i])
			break
		}
	}
}

// ---- End-to-End WebSocket Session Test ----

func TestWebSocketSessionEndToEnd(t *testing.T) {
	disp := &mockDispatcher{
		call: func(method string, params []any) (any, error) {
			return fmt.Sprintf("result-for-%s", method), nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		_ = srv.http.Serve(ln)
	}()
	go srv.broadcastNotifications(ctx)

	// Open a raw TCP connection and do the WebSocket handshake.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	clientKey := "dGhlIHNhbXBsZSBub25jZQ=="
	req := fmt.Sprintf("GET /jsonrpc HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Connection: Upgrade\r\n"+
		"Upgrade: websocket\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n"+
		"\r\n", ln.Addr().String(), clientKey)
	_, err = conn.Write([]byte(req))
	if err != nil {
		t.Fatal(err)
	}

	// Read the upgrade response.
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	resp := string(buf[:n])
	if !strings.Contains(resp, "101 Switching Protocols") {
		t.Fatalf("expected 101, got: %s", resp)
	}

	time.Sleep(100 * time.Millisecond)
	if srv.wsMan.count() == 0 {
		t.Fatal("expected at least 1 websocket session")
	}

	cancel()
}

func TestWebSocketBroadcast(t *testing.T) {
	notifCh := make(chan Notification, 10)
	disp := &mockDispatcher{
		subscribeFn: func(ctx context.Context) (<-chan Notification, error) {
			return notifCh, nil
		},
	}
	cfg := Config{
		Listen:         ":0",
		AllowedOrigins: []string{"*"},
		Dispatcher:     disp,
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		_ = srv.http.Serve(ln)
	}()
	go srv.broadcastNotifications(ctx)

	// Open a raw TCP connection for WebSocket.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	clientKey := "dGhlIHNhbXBsZSBub25jZQ=="
	req := fmt.Sprintf("GET /jsonrpc HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Connection: Upgrade\r\n"+
		"Upgrade: websocket\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n"+
		"\r\n", ln.Addr().String(), clientKey)
	_, err = conn.Write([]byte(req))
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), "101") {
		t.Fatal("upgrade failed")
	}

	time.Sleep(100 * time.Millisecond)

	if srv.wsMan.count() == 0 {
		t.Fatal("expected at least 1 session")
	}

	// Send a notification.
	notifCh <- Notification{
		Method: "aria2.onDownloadStart",
		Params: []any{map[string]any{"gid": "test-gid"}},
	}
	time.Sleep(50 * time.Millisecond)

	cancel()
}

// ---- Helper Types ----

func doTestRequest(t *testing.T, client *http.Client, method, target, contentType string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s failed: %v", method, err)
	}
	return resp
}

func openRawWebSocket(t *testing.T, addr string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	clientKey := "dGhlIHNhbXBsZSBub25jZQ=="
	req := fmt.Sprintf("GET /jsonrpc HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Connection: Upgrade\r\n"+
		"Upgrade: websocket\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n"+
		"\r\n", addr, clientKey)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		t.Fatalf("read upgrade response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Sec-WebSocket-Accept"), computeAcceptKey(clientKey); got != want {
		conn.Close()
		t.Fatalf("accept key = %q, want %q", got, want)
	}
	return conn, reader
}

func writeMaskedClientFrame(w io.Writer, opcode byte, payload []byte) error {
	return writeMaskedClientFrameWithFIN(w, true, opcode, payload)
}

func writeMaskedClientFrameWithFIN(w io.Writer, fin bool, opcode byte, payload []byte) error {
	header := make([]byte, 2, 10)
	header[0] = opcode & 0x0F
	if fin {
		header[0] |= 0x80
	}
	length := len(payload)
	switch {
	case length <= 125:
		header[1] = 0x80 | byte(length)
	case length <= 65535:
		header[1] = 0x80 | 126
		header = append(header, byte(length>>8), byte(length))
	default:
		header[1] = 0x80 | 127
		for shift := 56; shift >= 0; shift -= 8 {
			header = append(header, byte(uint64(length)>>shift))
		}
	}
	maskKey := [4]byte{0x12, 0x34, 0x56, 0x78}
	masked := make([]byte, len(payload))
	copy(masked, payload)
	applyMask(masked, maskKey)
	if _, err := w.Write(header); err != nil {
		return err
	}
	if _, err := w.Write(maskKey[:]); err != nil {
		return err
	}
	if len(masked) == 0 {
		return nil
	}
	_, err := w.Write(masked)
	return err
}

type testRPCError struct {
	code int
	msg  string
}

func (e *testRPCError) Error() string { return e.msg }
func (e *testRPCError) Code() int     { return e.code }

func getErrorCode(e *jsonrpc.Error) int {
	if e == nil {
		return 0
	}
	return e.Code
}

// ---- Integration: XML-RPC reply round-trip check ----

func TestXMLRPCReplyFormat(t *testing.T) {
	var buf bytes.Buffer
	err := xmlrpc.EncodeReply(&buf, xmlrpc.Reply{Result: "test-result"})
	if err != nil {
		t.Fatal(err)
	}
	// Verify it parses back as valid XML.
	var result struct {
		XMLName xml.Name `xml:"methodResponse"`
	}
	if err := xml.NewDecoder(&buf).Decode(&result); err != nil {
		t.Fatalf("invalid XML reply: %v", err)
	}
}

// ---- WebSocket computeAcceptKey correctness test (RFC 6455 example) ----

func TestComputeAcceptKeyRFCExample(t *testing.T) {
	clientKey := "dGhlIHNhbXBsZSBub25jZQ=="
	expected := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	got := computeAcceptKey(clientKey)
	if got != expected {
		t.Errorf("accept key = %q, want %q", got, expected)
	}
}

// ---- SHA1 + base64 verification ----

func TestSHA1WebSocketAccept(t *testing.T) {
	clientKey := "dGhlIHNhbXBsZSBub25jZQ=="
	h := sha1.New()
	h.Write([]byte(clientKey))
	h.Write([]byte(websocketGUID))
	acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if acceptKey != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("accept key doesn't match RFC 6455 example: %s", acceptKey)
	}
}

// ---- Dispatcher nil check in New ----

func TestNewNilDispatcher(t *testing.T) {
	_, err := New(Config{Listen: ":0"})
	if err == nil {
		t.Fatal("expected error for nil dispatcher")
	}
}
