package conformance

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type rpcDeepWireResponse struct {
	ID      json.RawMessage `json:"id"`
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func TestRPCDeep_JSONRPCBatchMixedStatusMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})

	requests := []map[string]any{
		{"jsonrpc": "2.0", "id": "methods", "method": "system.listMethods", "params": []any{}},
		{"jsonrpc": "2.0", "id": "bad-position", "method": "aria2.addUri", "params": []any{[]string{"http://127.0.0.1:1/bad"}, map[string]string{}, float64(-1)}},
		{"jsonrpc": "2.0", "id": "unknown-gid", "method": "aria2.tellStatus", "params": []any{"00000000000000ff"}},
		{"jsonrpc": "2.0", "id": "stat", "method": "aria2.getGlobalStat", "params": []any{}},
	}

	refStatus, refBatch := rpcPostBatch(t, refPort, requests)
	implStatus, implBatch := rpcPostBatch(t, implPort, requests)
	if refStatus != http.StatusOK {
		t.Fatalf("ref batch HTTP status got %d want 200", refStatus)
	}
	if implStatus != refStatus {
		t.Fatalf("impl batch HTTP status got %d want ref status %d", implStatus, refStatus)
	}

	compareBatchEnvelope(t, refBatch, implBatch)
	requireBatchErrorCode(t, "ref bad position", refBatch, "bad-position", 1)
	requireBatchErrorCode(t, "impl bad position", implBatch, "bad-position", 1)
	requireBatchErrorCode(t, "ref unknown gid", refBatch, "unknown-gid", 1)
	requireBatchErrorCode(t, "impl unknown gid", implBatch, "unknown-gid", 1)

	refMethods := batchResultStrings(t, refBatch, "methods")
	implMethods := batchResultStrings(t, implBatch, "methods")
	compareStringSet(t, "batch listMethods", refMethods, implMethods)

	refStat := batchResultStringMap(t, refBatch, "stat")
	implStat := batchResultStringMap(t, implBatch, "stat")
	for _, key := range []string{"downloadSpeed", "uploadSpeed", "numActive", "numWaiting", "numStopped", "numStoppedTotal"} {
		if refStat[key] != implStat[key] {
			t.Errorf("batch getGlobalStat %s mismatch: ref=%q impl=%q", key, refStat[key], implStat[key])
		}
	}
}

func TestRPCDeep_JSONRPCBatchInvalidObjectMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})

	requests := []any{
		map[string]any{"jsonrpc": "2.0", "id": "methods", "method": "system.listMethods", "params": []any{}},
		map[string]any{"jsonrpc": "2.0", "id": "broken"},
		map[string]any{"jsonrpc": "2.0", "id": "stat", "method": "aria2.getGlobalStat", "params": []any{}},
	}

	refStatus, refBatch := rpcPostBatch(t, refPort, requests)
	implStatus, implBatch := rpcPostBatch(t, implPort, requests)
	if refStatus != http.StatusOK {
		t.Fatalf("ref invalid-object batch HTTP status got %d want 200", refStatus)
	}
	if implStatus != refStatus {
		t.Fatalf("impl invalid-object batch HTTP status got %d want ref status %d", implStatus, refStatus)
	}

	compareBatchEnvelope(t, refBatch, implBatch)
	requireBatchErrorCode(t, "ref invalid object", refBatch, "broken", -32600)
	requireBatchErrorCode(t, "impl invalid object", implBatch, "broken", -32600)
	compareStringSet(t, "invalid-object batch listMethods",
		batchResultStrings(t, refBatch, "methods"),
		batchResultStrings(t, implBatch, "methods"))

	refStat := batchResultStringMap(t, refBatch, "stat")
	implStat := batchResultStringMap(t, implBatch, "stat")
	for _, key := range []string{"downloadSpeed", "uploadSpeed", "numActive", "numWaiting", "numStopped", "numStoppedTotal"} {
		if refStat[key] != implStat[key] {
			t.Errorf("invalid-object batch getGlobalStat %s mismatch: ref=%q impl=%q", key, refStat[key], implStat[key])
		}
	}
}

func TestRPCDeep_JSONRPCSingleInvalidShapeMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})

	cases := []struct {
		name string
		body string
	}{
		{
			name: "missing method preserves id",
			body: `{"jsonrpc":"2.0","id":"bad","params":[]}`,
		},
		{
			name: "invalid params preserves id",
			body: `{"jsonrpc":"2.0","id":"badp","method":"aria2.getVersion","params":{}}`,
		},
		{
			name: "top level scalar is invalid request",
			body: `"str"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refStatus, refRaw := rpcPostRaw(t, httpClient, "http", refPort, []byte(tc.body))
			implStatus, implRaw := rpcPostRaw(t, httpClient, "http", implPort, []byte(tc.body))
			if implStatus != refStatus {
				t.Fatalf("HTTP status mismatch: ref=%d impl=%d", refStatus, implStatus)
			}

			ref := decodeDeepRPCResponse(t, "ref "+tc.name, refRaw)
			impl := decodeDeepRPCResponse(t, "impl "+tc.name, implRaw)
			if string(ref.ID) != string(impl.ID) {
				t.Fatalf("id mismatch: ref=%s impl=%s", ref.ID, impl.ID)
			}
			if ref.JSONRPC != "2.0" || impl.JSONRPC != "2.0" {
				t.Fatalf("jsonrpc mismatch: ref=%q impl=%q", ref.JSONRPC, impl.JSONRPC)
			}
			if ref.Error == nil || impl.Error == nil {
				t.Fatalf("expected errors: ref=%#v impl=%#v", ref, impl)
			}
			if ref.Error.Code != impl.Error.Code {
				t.Fatalf("error code mismatch: ref=%d impl=%d", ref.Error.Code, impl.Error.Code)
			}
			if ref.Error.Message != impl.Error.Message {
				t.Fatalf("error message mismatch: ref=%q impl=%q", ref.Error.Message, impl.Error.Message)
			}
		})
	}
}

func TestRPCDeep_XMLRPCTransportParityMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})
	if !rpcFeatureEnabled(t, refPort, "XML-RPC") {
		t.Skip("reference aria2c does not advertise XML-RPC support")
	}

	cases := []struct {
		name   string
		method string
		params []any
		assert func(*testing.T, xmlRPCReply, xmlRPCReply)
	}{
		{
			name:   "listNotifications",
			method: "system.listNotifications",
			assert: func(t *testing.T, ref, impl xmlRPCReply) {
				t.Helper()
				requireNoXMLFault(t, "ref listNotifications", ref)
				requireNoXMLFault(t, "impl listNotifications", impl)
				compareStringSet(t, "XML listNotifications", xmlStringSlice(t, ref.Value), xmlStringSlice(t, impl.Value))
			},
		},
		{
			name:   "getGlobalStat",
			method: "aria2.getGlobalStat",
			assert: func(t *testing.T, ref, impl xmlRPCReply) {
				t.Helper()
				requireNoXMLFault(t, "ref getGlobalStat", ref)
				requireNoXMLFault(t, "impl getGlobalStat", impl)
				refMap := xmlStringMap(t, ref.Value)
				implMap := xmlStringMap(t, impl.Value)
				for _, key := range []string{"downloadSpeed", "uploadSpeed", "numActive", "numWaiting", "numStopped", "numStoppedTotal"} {
					if refMap[key] != implMap[key] {
						t.Errorf("XML getGlobalStat %s mismatch: ref=%q impl=%q", key, refMap[key], implMap[key])
					}
				}
			},
		},
		{
			name:   "unknownGIDFault",
			method: "aria2.tellStatus",
			params: []any{"00000000000000ff"},
			assert: func(t *testing.T, ref, impl xmlRPCReply) {
				t.Helper()
				requireXMLFaultCode(t, "ref tellStatus unknown GID", ref, 1)
				requireXMLFaultCode(t, "impl tellStatus unknown GID", impl, 1)
				if ref.Fault.String == "" || impl.Fault.String == "" {
					t.Fatalf("XML fault string must be non-empty: ref=%q impl=%q", ref.Fault.String, impl.Fault.String)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := xmlRPCCall(t, refPort, tc.method, tc.params...)
			impl := xmlRPCCall(t, implPort, tc.method, tc.params...)
			tc.assert(t, ref, impl)
		})
	}
}

func TestRPCDeep_HTTPGETJSONPMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})

	cases := []struct {
		name     string
		callback string
		query    url.Values
		assert   func(*testing.T, json.RawMessage, json.RawMessage)
	}{
		{
			name:     "single getGlobalStat",
			callback: "cb_stat",
			query:    jsonRPCGETQuery("aria2.getGlobalStat", "get-stat", []any{}, "cb_stat"),
			assert: func(t *testing.T, refRaw, implRaw json.RawMessage) {
				t.Helper()
				ref := decodeDeepRPCResponse(t, "ref GET stat", refRaw)
				impl := decodeDeepRPCResponse(t, "impl GET stat", implRaw)
				if string(ref.ID) != `"get-stat"` || string(impl.ID) != `"get-stat"` {
					t.Fatalf("GET id mismatch: ref=%s impl=%s", ref.ID, impl.ID)
				}
				if ref.Error != nil || impl.Error != nil {
					t.Fatalf("GET getGlobalStat returned errors: ref=%v impl=%v", ref.Error, impl.Error)
				}
				refStat := mustStringMap(t, "ref GET stat", ref.Result)
				implStat := mustStringMap(t, "impl GET stat", impl.Result)
				if !reflect.DeepEqual(refStat, implStat) {
					t.Fatalf("GET getGlobalStat mismatch: ref=%#v impl=%#v", refStat, implStat)
				}
			},
		},
		{
			name:     "batch mixed",
			callback: "cb_batch",
			query: jsonRPCGETBatchQuery([]map[string]any{
				{"jsonrpc": "2.0", "id": "notifications", "method": "system.listNotifications", "params": []any{}},
				{"jsonrpc": "2.0", "id": "bad-position", "method": "aria2.addUri", "params": []any{[]string{"http://127.0.0.1:1/bad"}, map[string]string{}, float64(-1)}},
			}, "cb_batch"),
			assert: func(t *testing.T, refRaw, implRaw json.RawMessage) {
				t.Helper()
				refBatch := decodeDeepRPCBatch(t, "ref GET batch", refRaw)
				implBatch := decodeDeepRPCBatch(t, "impl GET batch", implRaw)
				compareBatchEnvelope(t, refBatch, implBatch)
				requireBatchErrorCode(t, "ref GET bad position", refBatch, "bad-position", 1)
				requireBatchErrorCode(t, "impl GET bad position", implBatch, "bad-position", 1)
				compareStringSet(t, "GET batch notifications",
					batchResultStrings(t, refBatch, "notifications"),
					batchResultStrings(t, implBatch, "notifications"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refStatus, refContentType, refRaw := rpcJSONPGET(t, refPort, tc.query, tc.callback)
			implStatus, implContentType, implRaw := rpcJSONPGET(t, implPort, tc.query, tc.callback)
			if refStatus != http.StatusOK {
				t.Fatalf("ref GET status got %d want 200", refStatus)
			}
			if implStatus != refStatus {
				t.Fatalf("impl GET status got %d want ref status %d", implStatus, refStatus)
			}
			if !strings.HasPrefix(refContentType, "text/javascript") || !strings.HasPrefix(implContentType, "text/javascript") {
				t.Fatalf("JSONP content type mismatch: ref=%q impl=%q", refContentType, implContentType)
			}
			tc.assert(t, refRaw, implRaw)
		})
	}
}

func TestRPCDeep_HTTPGETJSONPEdgeMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})

	cases := []struct {
		name     string
		callback string
		query    string
		assert   func(*testing.T, json.RawMessage, json.RawMessage)
	}{
		{
			name:     "percent encoded id is preserved raw",
			callback: "cb_raw",
			query:    "method=system.listMethods&id=a%2Fb&jsoncallback=cb_raw",
			assert: func(t *testing.T, refRaw, implRaw json.RawMessage) {
				t.Helper()
				ref := decodeDeepRPCResponse(t, "ref raw id", refRaw)
				impl := decodeDeepRPCResponse(t, "impl raw id", implRaw)
				if string(ref.ID) != string(impl.ID) {
					t.Fatalf("id mismatch: ref=%s impl=%s", ref.ID, impl.ID)
				}
				if string(ref.ID) != `"a%2Fb"` {
					t.Fatalf("ref id got %s want %q", ref.ID, `"a%2Fb"`)
				}
				if ref.Error != nil || impl.Error != nil {
					t.Fatalf("unexpected errors: ref=%#v impl=%#v", ref.Error, impl.Error)
				}
			},
		},
		{
			name:     "invalid base64 params become invalid request",
			callback: "cb_bad",
			query:    "method=system.listMethods&id=x&params=not_base64&jsoncallback=cb_bad",
			assert: func(t *testing.T, refRaw, implRaw json.RawMessage) {
				t.Helper()
				ref := decodeDeepRPCResponse(t, "ref bad base64", refRaw)
				impl := decodeDeepRPCResponse(t, "impl bad base64", implRaw)
				if string(ref.ID) != string(impl.ID) {
					t.Fatalf("id mismatch: ref=%s impl=%s", ref.ID, impl.ID)
				}
				if string(ref.ID) != "null" {
					t.Fatalf("ref id got %s want null", ref.ID)
				}
				if ref.Error == nil || impl.Error == nil {
					t.Fatalf("expected errors: ref=%#v impl=%#v", ref, impl)
				}
				if ref.Error.Code != impl.Error.Code {
					t.Fatalf("error code mismatch: ref=%d impl=%d", ref.Error.Code, impl.Error.Code)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refStatus, refContentType, refRaw := rpcJSONPGETRaw(t, refPort, tc.query, tc.callback)
			implStatus, implContentType, implRaw := rpcJSONPGETRaw(t, implPort, tc.query, tc.callback)
			if implStatus != refStatus {
				t.Fatalf("HTTP status mismatch: ref=%d impl=%d", refStatus, implStatus)
			}
			if refContentType != implContentType {
				t.Fatalf("content type mismatch: ref=%q impl=%q", refContentType, implContentType)
			}
			tc.assert(t, refRaw, implRaw)
		})
	}
}

func TestRPCDeep_WebSocketNotificationsMatrix(t *testing.T) {
	SkipIfNoRef(t)

	release := make(chan struct{})
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("w"), 1024))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer fileSrv.Close()
	defer close(release)

	dir := t.TempDir()
	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--dir=" + filepath.Join(dir, "ref")},
		[]string{"--no-conf", "--dir=" + filepath.Join(dir, "impl")},
	)
	if !rpcFeatureEnabled(t, refPort, "WebSocket") {
		t.Skip("reference aria2c does not advertise WebSocket support")
	}

	const gid = "0000000000000f01"
	refFlow := runWebSocketAddURIFlow(t, refPort, gid, fileSrv.URL+"/ws.bin")
	implFlow := runWebSocketAddURIFlow(t, implPort, gid, fileSrv.URL+"/ws.bin")

	if refFlow.ResultGID != gid || implFlow.ResultGID != gid {
		t.Fatalf("WebSocket addUri result mismatch: ref=%q impl=%q want %q", refFlow.ResultGID, implFlow.ResultGID, gid)
	}
	if !reflect.DeepEqual(refFlow.Notifications, implFlow.Notifications) {
		t.Fatalf("WebSocket notifications mismatch: ref=%#v impl=%#v", refFlow.Notifications, implFlow.Notifications)
	}
	if len(implFlow.Notifications) == 0 || implFlow.Notifications[0].Method != "aria2.onDownloadStart" {
		t.Fatalf("WebSocket missing onDownloadStart notification: %#v", implFlow.Notifications)
	}
}

func TestRPCDeep_WebSocketBatchMissingIDMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})
	if !rpcFeatureEnabled(t, refPort, "WebSocket") {
		t.Skip("reference aria2c does not advertise WebSocket support")
	}

	payload := []byte(`[
		{"jsonrpc":"2.0","id":"ok","method":"aria2.getVersion","params":[]},
		{"jsonrpc":"2.0","method":"aria2.getVersion","params":[]}
	]`)

	refBatch := wsBatchCall(t, openWebSocket(t, refPort), payload)
	implBatch := wsBatchCall(t, openWebSocket(t, implPort), payload)

	compareBatchEnvelope(t, refBatch, implBatch)
	if len(refBatch) != 2 || len(implBatch) != 2 {
		t.Fatalf("unexpected ws batch sizes: ref=%d impl=%d", len(refBatch), len(implBatch))
	}
	if refBatch[1].Error == nil || implBatch[1].Error == nil {
		t.Fatalf("expected ws batch missing-id errors: ref=%#v impl=%#v", refBatch[1], implBatch[1])
	}
	if refBatch[1].Error.Code != implBatch[1].Error.Code {
		t.Fatalf("ws batch missing-id error code mismatch: ref=%d impl=%d", refBatch[1].Error.Code, implBatch[1].Error.Code)
	}
	if string(refBatch[1].ID) != string(implBatch[1].ID) {
		t.Fatalf("ws batch missing-id id mismatch: ref=%s impl=%s", refBatch[1].ID, implBatch[1].ID)
	}
	if string(refBatch[1].ID) != "null" {
		t.Fatalf("ref ws batch missing-id id got %s want null", refBatch[1].ID)
	}
}

func TestRPCDeep_WebSocketFragmentedMessageMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t, []string{"--no-conf"}, []string{"--no-conf"})
	if !rpcFeatureEnabled(t, refPort, "WebSocket") {
		t.Skip("reference aria2c does not advertise WebSocket support")
	}

	payload := []byte(`{"jsonrpc":"2.0","id":"frag","method":"system.listMethods","params":[]}`)
	refResp := wsFragmentedCall(t, openWebSocket(t, refPort), 0x1, payload)
	implResp := wsFragmentedCall(t, openWebSocket(t, implPort), 0x1, payload)

	if string(refResp.ID) != string(implResp.ID) {
		t.Fatalf("fragmented ws id mismatch: ref=%s impl=%s", refResp.ID, implResp.ID)
	}
	if refResp.JSONRPC != "2.0" || implResp.JSONRPC != "2.0" {
		t.Fatalf("fragmented ws jsonrpc mismatch: ref=%q impl=%q", refResp.JSONRPC, implResp.JSONRPC)
	}
	if refResp.Error != nil || implResp.Error != nil {
		t.Fatalf("fragmented ws returned errors: ref=%#v impl=%#v", refResp.Error, implResp.Error)
	}
	compareStringSet(t, "fragmented ws listMethods",
		mustRPCStringSlice(t, "ref fragmented ws listMethods", refResp.Result),
		mustRPCStringSlice(t, "impl fragmented ws listMethods", implResp.Result))
}

func TestRPCDeep_JSONRPCOversizedPOSTMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--rpc-max-request-size=96"},
		[]string{"--no-conf", "--rpc-max-request-size=96"},
	)

	req := fmt.Sprintf("POST /jsonrpc HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nContent-Type: application/json\r\nContent-Length: 128\r\n\r\n", refPort)
	refRaw := rawHTTPExchange(t, refPort, req)
	req = fmt.Sprintf("POST /jsonrpc HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nContent-Type: application/json\r\nContent-Length: 128\r\n\r\n", implPort)
	implRaw := rawHTTPExchange(t, implPort, req)

	if len(refRaw) != 0 {
		t.Fatalf("reference oversized POST returned unexpected data: %q", string(refRaw))
	}
	if len(implRaw) != 0 {
		t.Fatalf("impl oversized POST returned unexpected data: %q", string(implRaw))
	}
}

func TestRPCDeep_SecureTransportMatrix(t *testing.T) {
	SkipIfNoRef(t)
	if refUsesAppleTLS(t) {
		t.Skip("reference aria2c uses AppleTLS; secure RPC needs KeyChain-backed certificate setup")
	}

	insecureRefPort, _ := startPairedRPCServers(t, "--no-conf")
	if !rpcFeatureEnabled(t, insecureRefPort, "HTTPS") {
		t.Skip("reference aria2c does not advertise HTTPS support")
	}

	_, _, pkcs12File := writeRPCSecureTestMaterial(t)
	refPort := findFreePort(t)
	implPort := findFreePort(t)

	refSrv := startRPCRef(t, refPort,
		"--no-conf",
		"--rpc-secure=true",
		"--rpc-certificate="+pkcs12File,
	)
	defer refSrv.Stop(t)
	waitReadyHTTPS(t, refPort)

	implArgs := []string{
		"--no-conf",
		"--rpc-secure=true",
		"--rpc-certificate=" + pkcs12File,
	}
	implSrv := startRPCImpl(t, implPort, implArgs...)
	defer implSrv.Stop(t)
	waitReadyHTTPS(t, implPort)

	client := insecureTLSHTTPClient()
	reqBody := []byte(`{"jsonrpc":"2.0","id":"methods","method":"system.listMethods","params":[]}`)
	refStatus, refRaw := rpcPostRaw(t, client, "https", refPort, reqBody)
	implStatus, implRaw := rpcPostRaw(t, client, "https", implPort, reqBody)
	if implStatus != refStatus {
		t.Fatalf("secure HTTP status mismatch: ref=%d impl=%d", refStatus, implStatus)
	}
	refResp := decodeDeepRPCResponse(t, "ref secure HTTP", refRaw)
	implResp := decodeDeepRPCResponse(t, "impl secure HTTP", implRaw)
	if refResp.Error != nil || implResp.Error != nil {
		t.Fatalf("secure HTTP returned errors: ref=%#v impl=%#v", refResp, implResp)
	}
	compareStringSet(t, "secure HTTP listMethods",
		mustRPCStringSlice(t, "ref secure HTTP listMethods", refResp.Result),
		mustRPCStringSlice(t, "impl secure HTTP listMethods", implResp.Result))

	refWS := wsCall(t, openSecureWebSocket(t, refPort), []byte(`{"jsonrpc":"2.0","id":"ws","method":"system.listMethods","params":[]}`))
	implWS := wsCall(t, openSecureWebSocket(t, implPort), []byte(`{"jsonrpc":"2.0","id":"ws","method":"system.listMethods","params":[]}`))
	if refWS.Error != nil || implWS.Error != nil {
		t.Fatalf("secure WS returned errors: ref=%#v impl=%#v", refWS, implWS)
	}
	compareStringSet(t, "secure WS listMethods",
		mustRPCStringSlice(t, "ref secure WS listMethods", refWS.Result),
		mustRPCStringSlice(t, "impl secure WS listMethods", implWS.Result))
}

func TestRPCDeep_SaveSessionSideEffectsMatrix(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	downloadDir := filepath.Join(dir, "downloads")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatalf("mkdir downloads: %v", err)
	}
	refSession := filepath.Join(dir, "ref.session")
	implSession := filepath.Join(dir, "impl.session")
	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--dir=" + downloadDir, "--save-session=" + refSession},
		[]string{"--no-conf", "--dir=" + downloadDir, "--save-session=" + implSession},
	)

	const gid = "0000000000000f02"
	const uri = "http://127.0.0.1:1/save-session.bin"
	addPausedURI(t, refPort, gid, uri)
	addPausedURI(t, implPort, gid, uri)

	requireRPCSuccess(t, "ref saveSession", rpcCall(t, refPort, "aria2.saveSession", []any{}))
	requireRPCSuccess(t, "impl saveSession", rpcCall(t, implPort, "aria2.saveSession", []any{}))

	refEntries := readSessionEntries(t, refSession)
	implEntries := readSessionEntries(t, implSession)
	refEntry := requireSessionEntry(t, "ref session", refEntries, uri)
	implEntry := requireSessionEntry(t, "impl session", implEntries, uri)
	for _, entry := range []struct {
		label string
		value sessionEntry
	}{
		{label: "ref", value: refEntry},
		{label: "impl", value: implEntry},
	} {
		if got := entry.value.Options["gid"]; got != gid {
			t.Fatalf("%s session gid got %q want %q", entry.label, got, gid)
		}
		if got := entry.value.Options["pause"]; got != "true" {
			t.Fatalf("%s session pause got %q want true", entry.label, got)
		}
	}
	if _, err := os.Stat(refSession + "__temp"); !os.IsNotExist(err) {
		t.Fatalf("ref temp session file state: %v", err)
	}
	if _, err := os.Stat(implSession + "__temp"); !os.IsNotExist(err) {
		t.Fatalf("impl temp session file state: %v", err)
	}
}

func TestRPCDeep_OptionMutationEffectsMatrix(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	initialDir := filepath.Join(dir, "initial")
	changedDir := filepath.Join(dir, "changed")
	if err := os.MkdirAll(initialDir, 0o755); err != nil {
		t.Fatalf("mkdir initial: %v", err)
	}
	if err := os.MkdirAll(changedDir, 0o755); err != nil {
		t.Fatalf("mkdir changed: %v", err)
	}

	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--pause=true", "--dir=" + initialDir},
		[]string{"--no-conf", "--pause=true", "--dir=" + initialDir},
	)

	changeGlobal := []any{map[string]string{
		"dir":                changedDir,
		"max-download-limit": "65536",
	}}
	requireRPCSuccess(t, "ref changeGlobalOption", rpcCall(t, refPort, "aria2.changeGlobalOption", changeGlobal))
	requireRPCSuccess(t, "impl changeGlobalOption", rpcCall(t, implPort, "aria2.changeGlobalOption", changeGlobal))

	const gid = "0000000000000f03"
	const uri = "http://127.0.0.1:1/global-option.bin"
	addPausedURI(t, refPort, gid, uri)
	addPausedURI(t, implPort, gid, uri)

	refOpt := mustStringMap(t, "ref getOption inherited", rpcCallOK(t, refPort, "aria2.getOption", []any{gid}).Result)
	implOpt := mustStringMap(t, "impl getOption inherited", rpcCallOK(t, implPort, "aria2.getOption", []any{gid}).Result)
	requireOptionValues(t, "ref inherited options", refOpt, map[string]string{"dir": changedDir, "max-download-limit": "65536"})
	requireOptionValues(t, "impl inherited options", implOpt, map[string]string{"dir": changedDir, "max-download-limit": "65536"})

	changeDownload := []any{gid, map[string]string{
		"max-download-limit": "32768",
		"out":                "renamed.bin",
	}}
	requireRPCSuccess(t, "ref changeOption", rpcCall(t, refPort, "aria2.changeOption", changeDownload))
	requireRPCSuccess(t, "impl changeOption", rpcCall(t, implPort, "aria2.changeOption", changeDownload))

	refOpt = mustStringMap(t, "ref getOption changed", rpcCallOK(t, refPort, "aria2.getOption", []any{gid}).Result)
	implOpt = mustStringMap(t, "impl getOption changed", rpcCallOK(t, implPort, "aria2.getOption", []any{gid}).Result)
	requireOptionValues(t, "ref changed options", refOpt, map[string]string{"max-download-limit": "32768", "out": "renamed.bin"})
	requireOptionValues(t, "impl changed options", implOpt, map[string]string{"max-download-limit": "32768", "out": "renamed.bin"})
}

func TestRPCDeep_ActiveDownloadFilesAndServersMatrix(t *testing.T) {
	SkipIfNoRef(t)

	const contentLength = 1048576
	release := make(chan struct{})
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(contentLength))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("a"), 1024))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer fileSrv.Close()
	defer close(release)

	dir := t.TempDir()
	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--dir=" + filepath.Join(dir, "ref")},
		[]string{"--no-conf", "--dir=" + filepath.Join(dir, "impl")},
	)

	const gid = "0000000000000f04"
	uri := fileSrv.URL + "/active-values.bin"
	addURIWithGID(t, refPort, gid, uri)
	addURIWithGID(t, implPort, gid, uri)
	waitForRPCStatus(t, refPort, gid, "active")
	waitForRPCStatus(t, implPort, gid, "active")

	refFiles := waitForActiveFiles(t, refPort, gid, uri, contentLength)
	implFiles := waitForActiveFiles(t, implPort, gid, uri, contentLength)
	if !reflect.DeepEqual(refFiles, implFiles) {
		t.Fatalf("active getFiles stable values mismatch: ref=%#v impl=%#v", refFiles, implFiles)
	}

	refServers := waitForActiveServers(t, refPort, gid, uri)
	implServers := waitForActiveServers(t, implPort, gid, uri)
	if !reflect.DeepEqual(refServers, implServers) {
		t.Fatalf("active getServers stable values mismatch: ref=%#v impl=%#v", refServers, implServers)
	}
}

func rpcPostBatch(t *testing.T, port int, requests any) (int, []rpcDeepWireResponse) {
	t.Helper()

	body, err := json.Marshal(requests)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	resp, err := httpClient.Post("http://127.0.0.1:"+strconv.Itoa(port)+"/jsonrpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post batch: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read batch response: %v", err)
	}
	return resp.StatusCode, decodeDeepRPCBatch(t, "batch response", raw)
}

func rpcPostRaw(t *testing.T, client *http.Client, scheme string, port int, body []byte) (int, json.RawMessage) {
	t.Helper()
	resp, err := client.Post(scheme+"://127.0.0.1:"+strconv.Itoa(port)+"/jsonrpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post raw RPC: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read raw RPC response: %v", err)
	}
	return resp.StatusCode, json.RawMessage(raw)
}

func rawHTTPExchange(t *testing.T, port int, request string) []byte {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		t.Fatalf("dial raw HTTP: %v", err)
	}
	defer conn.Close()

	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatalf("write raw HTTP request: %v", err)
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	raw, err := io.ReadAll(conn)
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatalf("timed out waiting for raw HTTP response on port %d", port)
		}
		t.Fatalf("read raw HTTP response: %v", err)
	}
	return raw
}

func decodeDeepRPCResponse(t *testing.T, label string, raw []byte) rpcDeepWireResponse {
	t.Helper()
	var resp rpcDeepWireResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("%s: unmarshal response: %v raw=%s", label, err, string(raw))
	}
	return resp
}

func decodeDeepRPCBatch(t *testing.T, label string, raw []byte) []rpcDeepWireResponse {
	t.Helper()
	var batch []rpcDeepWireResponse
	if err := json.Unmarshal(raw, &batch); err != nil {
		t.Fatalf("%s: unmarshal batch: %v raw=%s", label, err, string(raw))
	}
	return batch
}

func compareBatchEnvelope(t *testing.T, ref, impl []rpcDeepWireResponse) {
	t.Helper()
	if len(ref) != len(impl) {
		t.Fatalf("batch length mismatch: ref=%d impl=%d", len(ref), len(impl))
	}
	for i := range ref {
		if string(ref[i].ID) != string(impl[i].ID) {
			t.Errorf("batch[%d] id mismatch: ref=%s impl=%s", i, ref[i].ID, impl[i].ID)
		}
		if ref[i].JSONRPC != "2.0" || impl[i].JSONRPC != "2.0" {
			t.Errorf("batch[%d] jsonrpc mismatch: ref=%q impl=%q", i, ref[i].JSONRPC, impl[i].JSONRPC)
		}
		if (ref[i].Error == nil) != (impl[i].Error == nil) {
			t.Errorf("batch[%d] success/error mismatch: refErr=%v implErr=%v", i, ref[i].Error, impl[i].Error)
			continue
		}
		if ref[i].Error != nil && ref[i].Error.Code != impl[i].Error.Code {
			t.Errorf("batch[%d] error code mismatch: ref=%d impl=%d", i, ref[i].Error.Code, impl[i].Error.Code)
		}
	}
}

func requireBatchErrorCode(t *testing.T, label string, batch []rpcDeepWireResponse, id string, want int) {
	t.Helper()
	resp := requireBatchResponse(t, batch, id)
	if resp.Error == nil {
		t.Fatalf("%s: got success, want error code %d", label, want)
	}
	if resp.Error.Code != want {
		t.Fatalf("%s: error code got %d want %d", label, resp.Error.Code, want)
	}
}

func requireBatchResponse(t *testing.T, batch []rpcDeepWireResponse, id string) rpcDeepWireResponse {
	t.Helper()
	want := strconv.Quote(id)
	for _, resp := range batch {
		if string(resp.ID) == want {
			return resp
		}
	}
	t.Fatalf("batch response id %q not found in %#v", id, batch)
	return rpcDeepWireResponse{}
}

func batchResultStrings(t *testing.T, batch []rpcDeepWireResponse, id string) []string {
	t.Helper()
	resp := requireBatchResponse(t, batch, id)
	if resp.Error != nil {
		t.Fatalf("batch %s returned error: %#v", id, resp.Error)
	}
	var values []string
	if err := json.Unmarshal(resp.Result, &values); err != nil {
		t.Fatalf("batch %s result strings: %v raw=%s", id, err, string(resp.Result))
	}
	return values
}

func batchResultStringMap(t *testing.T, batch []rpcDeepWireResponse, id string) map[string]string {
	t.Helper()
	resp := requireBatchResponse(t, batch, id)
	if resp.Error != nil {
		t.Fatalf("batch %s returned error: %#v", id, resp.Error)
	}
	return mustStringMap(t, "batch "+id, resp.Result)
}

func rpcFeatureEnabled(t *testing.T, port int, feature string) bool {
	t.Helper()
	resp := rpcCallOK(t, port, "aria2.getVersion", []any{})
	var version struct {
		EnabledFeatures []string `json:"enabledFeatures"`
	}
	if err := json.Unmarshal(resp.Result, &version); err != nil {
		t.Fatalf("unmarshal getVersion: %v", err)
	}
	for _, got := range version.EnabledFeatures {
		if got == feature {
			return true
		}
	}
	return false
}

func jsonRPCGETQuery(method, id string, params []any, callback string) url.Values {
	values := url.Values{}
	values.Set("method", method)
	values.Set("id", id)
	paramsJSON, _ := json.Marshal(params)
	values.Set("params", base64.StdEncoding.EncodeToString(paramsJSON))
	values.Set("jsoncallback", callback)
	return values
}

func jsonRPCGETBatchQuery(batch []map[string]any, callback string) url.Values {
	values := url.Values{}
	paramsJSON, _ := json.Marshal(batch)
	values.Set("params", base64.StdEncoding.EncodeToString(paramsJSON))
	values.Set("jsoncallback", callback)
	return values
}

func rpcJSONPGET(t *testing.T, port int, values url.Values, callback string) (int, string, json.RawMessage) {
	t.Helper()
	u := "http://127.0.0.1:" + strconv.Itoa(port) + "/jsonrpc?" + values.Encode()
	return rpcJSONPGETURL(t, httpClient, u, callback)
}

func rpcJSONPGETRaw(t *testing.T, port int, rawQuery string, callback string) (int, string, json.RawMessage) {
	t.Helper()
	u := "http://127.0.0.1:" + strconv.Itoa(port) + "/jsonrpc?" + rawQuery
	return rpcJSONPGETURL(t, httpClient, u, callback)
}

func rpcJSONPGETURL(t *testing.T, client *http.Client, u string, callback string) (int, string, json.RawMessage) {
	t.Helper()
	resp, err := client.Get(u)
	if err != nil {
		t.Fatalf("GET JSONP: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read JSONP: %v", err)
	}
	prefix := callback + "("
	if !bytes.HasPrefix(raw, []byte(prefix)) || !bytes.HasSuffix(raw, []byte(")")) {
		t.Fatalf("JSONP wrapper mismatch: callback=%q raw=%s", callback, string(raw))
	}
	inner := raw[len(prefix) : len(raw)-1]
	return resp.StatusCode, resp.Header.Get("Content-Type"), json.RawMessage(inner)
}

func mustRPCStringSlice(t *testing.T, label string, raw json.RawMessage) []string {
	t.Helper()
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("%s: unmarshal []string: %v raw=%s", label, err, string(raw))
	}
	return values
}

type xmlRPCReply struct {
	Value any
	Fault *xmlrpcFault
}

type xmlrpcFault struct {
	Code   int64
	String string
}

func xmlRPCCall(t *testing.T, port int, method string, params ...any) xmlRPCReply {
	t.Helper()
	body := encodeXMLRPCCall(method, params...)
	resp, err := httpClient.Post("http://127.0.0.1:"+strconv.Itoa(port)+"/rpc", "text/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("XML-RPC post %s: %v", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("XML-RPC read %s: %v", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("XML-RPC %s HTTP status got %d body=%s", method, resp.StatusCode, string(raw))
	}
	return parseXMLRPCReply(t, method, raw)
}

func encodeXMLRPCCall(method string, params ...any) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	xml.EscapeText(&b, []byte(method))
	b.WriteString(`</methodName><params>`)
	for _, param := range params {
		b.WriteString(`<param>`)
		encodeXMLRPCValue(&b, param)
		b.WriteString(`</param>`)
	}
	b.WriteString(`</params></methodCall>`)
	return b.String()
}

func encodeXMLRPCValue(b *strings.Builder, v any) {
	b.WriteString(`<value>`)
	switch x := v.(type) {
	case string:
		b.WriteString(`<string>`)
		xml.EscapeText(b, []byte(x))
		b.WriteString(`</string>`)
	case int:
		fmt.Fprintf(b, `<int>%d</int>`, x)
	case int64:
		fmt.Fprintf(b, `<int>%d</int>`, x)
	case []any:
		b.WriteString(`<array><data>`)
		for _, elem := range x {
			encodeXMLRPCValue(b, elem)
		}
		b.WriteString(`</data></array>`)
	case []string:
		b.WriteString(`<array><data>`)
		for _, elem := range x {
			encodeXMLRPCValue(b, elem)
		}
		b.WriteString(`</data></array>`)
	case map[string]string:
		b.WriteString(`<struct>`)
		for key, val := range x {
			b.WriteString(`<member><name>`)
			xml.EscapeText(b, []byte(key))
			b.WriteString(`</name>`)
			encodeXMLRPCValue(b, val)
			b.WriteString(`</member>`)
		}
		b.WriteString(`</struct>`)
	default:
		b.WriteString(`<string>`)
		xml.EscapeText(b, []byte(fmt.Sprint(x)))
		b.WriteString(`</string>`)
	}
	b.WriteString(`</value>`)
}

type xmlNode struct {
	Name     string
	Text     string
	Children []*xmlNode
}

func parseXMLRPCReply(t *testing.T, label string, raw []byte) xmlRPCReply {
	t.Helper()
	root := parseXMLNode(t, raw)
	if root.Name != "methodResponse" {
		t.Fatalf("%s: XML root got %q raw=%s", label, root.Name, string(raw))
	}
	if fault := firstChild(root, "fault"); fault != nil {
		value := firstDescendantValue(fault)
		faultMap := xmlAnyMap(parseXMLRPCValue(value))
		return xmlRPCReply{Fault: &xmlrpcFault{
			Code:   xmlInt64(faultMap["faultCode"]),
			String: fmt.Sprint(faultMap["faultString"]),
		}}
	}
	params := firstChild(root, "params")
	if params == nil {
		t.Fatalf("%s: methodResponse missing params/fault raw=%s", label, string(raw))
	}
	value := firstDescendantValue(params)
	return xmlRPCReply{Value: parseXMLRPCValue(value)}
}

func parseXMLNode(t *testing.T, raw []byte) *xmlNode {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var stack []*xmlNode
	var root *xmlNode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("parse XML: %v raw=%s", err, string(raw))
		}
		switch x := tok.(type) {
		case xml.StartElement:
			n := &xmlNode{Name: x.Name.Local}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(x)
			}
		}
	}
	if root == nil {
		t.Fatalf("parse XML: missing root raw=%s", string(raw))
	}
	return root
}

func parseXMLRPCValue(n *xmlNode) any {
	if n == nil {
		return nil
	}
	if n.Name == "value" {
		for _, child := range n.Children {
			return parseXMLRPCValue(child)
		}
		return n.Text
	}
	switch n.Name {
	case "string", "double", "dateTime.iso8601", "base64":
		return n.Text
	case "int", "i4":
		v, _ := strconv.ParseInt(strings.TrimSpace(n.Text), 10, 64)
		return v
	case "boolean":
		return strings.TrimSpace(n.Text) == "1"
	case "array":
		var values []any
		if data := firstChild(n, "data"); data != nil {
			for _, child := range data.Children {
				if child.Name == "value" {
					values = append(values, parseXMLRPCValue(child))
				}
			}
		}
		return values
	case "struct":
		values := make(map[string]any)
		for _, member := range n.Children {
			if member.Name != "member" {
				continue
			}
			nameNode := firstChild(member, "name")
			valueNode := firstChild(member, "value")
			if nameNode != nil {
				values[nameNode.Text] = parseXMLRPCValue(valueNode)
			}
		}
		return values
	default:
		return n.Text
	}
}

func firstChild(n *xmlNode, name string) *xmlNode {
	if n == nil {
		return nil
	}
	for _, child := range n.Children {
		if child.Name == name {
			return child
		}
	}
	return nil
}

func firstDescendantValue(n *xmlNode) *xmlNode {
	if n == nil {
		return nil
	}
	if n.Name == "value" {
		return n
	}
	for _, child := range n.Children {
		if value := firstDescendantValue(child); value != nil {
			return value
		}
	}
	return nil
}

func requireNoXMLFault(t *testing.T, label string, reply xmlRPCReply) {
	t.Helper()
	if reply.Fault != nil {
		t.Fatalf("%s fault: code=%d string=%q", label, reply.Fault.Code, reply.Fault.String)
	}
}

func requireXMLFaultCode(t *testing.T, label string, reply xmlRPCReply, want int64) {
	t.Helper()
	if reply.Fault == nil {
		t.Fatalf("%s got success, want fault", label)
	}
	if reply.Fault.Code != want {
		t.Fatalf("%s fault code got %d want %d", label, reply.Fault.Code, want)
	}
}

func xmlStringSlice(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("XML value got %T, want []any", value)
	}
	out := make([]string, len(raw))
	for i, elem := range raw {
		out[i] = fmt.Sprint(elem)
	}
	return out
}

func xmlStringMap(t *testing.T, value any) map[string]string {
	t.Helper()
	raw := xmlAnyMap(value)
	out := make(map[string]string, len(raw))
	for key, val := range raw {
		out[key] = fmt.Sprint(val)
	}
	return out
}

func xmlAnyMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func xmlInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}

type wsFlowResult struct {
	ResultGID     string
	Notifications []wsNotification
}

type wsNotification struct {
	Method string
	GID    string
}

func runWebSocketAddURIFlow(t *testing.T, port int, gid string, uri string) wsFlowResult {
	t.Helper()
	conn := openWebSocket(t, port)
	defer conn.Close()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      "ws-add",
		"method":  "aria2.addUri",
		"params": []any{
			[]string{uri},
			map[string]string{"gid": gid},
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal ws request: %v", err)
	}
	writeWSClientText(t, conn, payload)

	deadline := time.Now().Add(5 * time.Second)
	result := wsFlowResult{}
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		opcode, msg, err := readWSServerFrame(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatalf("read ws frame: %v", err)
		}
		if opcode != 0x1 {
			continue
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(msg, &envelope); err != nil {
			t.Fatalf("unmarshal ws message: %v raw=%s", err, string(msg))
		}
		if rawID, ok := envelope["id"]; ok && string(rawID) == `"ws-add"` {
			var resp rpcDeepWireResponse
			if err := json.Unmarshal(msg, &resp); err != nil {
				t.Fatalf("unmarshal ws response: %v raw=%s", err, string(msg))
			}
			if resp.Error != nil {
				t.Fatalf("ws addUri error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
			}
			if err := json.Unmarshal(resp.Result, &result.ResultGID); err != nil {
				t.Fatalf("unmarshal ws addUri result: %v raw=%s", err, string(resp.Result))
			}
			continue
		}
		var notif struct {
			Method string `json:"method"`
			Params []struct {
				GID string `json:"gid"`
			} `json:"params"`
		}
		if err := json.Unmarshal(msg, &notif); err != nil {
			t.Fatalf("unmarshal ws notification: %v raw=%s", err, string(msg))
		}
		if notif.Method != "" && len(notif.Params) > 0 {
			result.Notifications = append(result.Notifications, wsNotification{Method: notif.Method, GID: notif.Params[0].GID})
		}
		if result.ResultGID == gid && len(result.Notifications) > 0 {
			return result
		}
	}
	t.Fatalf("timed out waiting for ws response and notification; got %#v", result)
	return result
}

func openWebSocket(t *testing.T, port int) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		t.Fatalf("websocket key rand: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	req := fmt.Sprintf("GET /jsonrpc HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", port, key)
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		t.Fatalf("write websocket handshake: %v", err)
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		t.Fatalf("read websocket status: %v", err)
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		t.Fatalf("websocket status got %q want 101", strings.TrimSpace(status))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			t.Fatalf("read websocket header: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	if br.Buffered() != 0 {
		conn.Close()
		t.Fatalf("websocket handshake left %d buffered bytes", br.Buffered())
	}
	return conn
}

func openSecureWebSocket(t *testing.T, port int) net.Conn {
	t.Helper()
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), &tls.Config{
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("dial secure websocket: %v", err)
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		t.Fatalf("secure websocket key rand: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	req := fmt.Sprintf("GET /jsonrpc HTTP/1.1\r\nHost: 127.0.0.1:%d\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", port, key)
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		t.Fatalf("write secure websocket handshake: %v", err)
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		t.Fatalf("read secure websocket status: %v", err)
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		t.Fatalf("secure websocket status got %q want 101", strings.TrimSpace(status))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			t.Fatalf("read secure websocket header: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	if br.Buffered() != 0 {
		conn.Close()
		t.Fatalf("secure websocket handshake left %d buffered bytes", br.Buffered())
	}
	return conn
}

func wsCall(t *testing.T, conn net.Conn, payload []byte) rpcDeepWireResponse {
	t.Helper()
	defer conn.Close()

	writeWSClientText(t, conn, payload)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	opcode, msg, err := readWSServerFrame(conn)
	if err != nil {
		t.Fatalf("read ws response: %v", err)
	}
	if opcode != 0x1 {
		t.Fatalf("ws opcode got %#x want text", opcode)
	}
	return decodeDeepRPCResponse(t, "ws response", msg)
}

func wsFragmentedCall(t *testing.T, conn net.Conn, opcode byte, payload []byte) rpcDeepWireResponse {
	t.Helper()
	defer conn.Close()

	split := len(payload) / 2
	writeWSClientFrame(t, conn, opcode, false, payload[:split])
	writeWSClientFrame(t, conn, 0x0, true, payload[split:])
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	opcode, msg, err := readWSServerFrame(conn)
	if err != nil {
		t.Fatalf("read fragmented ws response: %v", err)
	}
	if opcode != 0x1 {
		t.Fatalf("fragmented ws opcode got %#x want text", opcode)
	}
	return decodeDeepRPCResponse(t, "fragmented ws response", msg)
}

func wsBatchCall(t *testing.T, conn net.Conn, payload []byte) []rpcDeepWireResponse {
	t.Helper()
	defer conn.Close()

	writeWSClientText(t, conn, payload)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	opcode, msg, err := readWSServerFrame(conn)
	if err != nil {
		t.Fatalf("read ws batch response: %v", err)
	}
	if opcode != 0x1 {
		t.Fatalf("ws batch opcode got %#x want text", opcode)
	}
	return decodeDeepRPCBatch(t, "ws batch response", msg)
}

func insecureTLSHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func waitReadyHTTPS(t *testing.T, port int) {
	t.Helper()
	client := insecureTLSHTTPClient()
	body := []byte(`{"jsonrpc":"2.0","id":"1","method":"system.listMethods","params":[]}`)
	for i := 0; i < 50; i++ {
		resp, err := client.Post("https://127.0.0.1:"+strconv.Itoa(port)+"/jsonrpc", "application/json", bytes.NewReader(body))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("secure rpc server on port %d did not become ready", port)
}

func writeRPCSecureTestMaterial(t *testing.T) (certFile, keyFile, pkcs12File string) {
	t.Helper()
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not available")
	}

	dir := t.TempDir()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	certFile = filepath.Join(dir, "rpc-cert.pem")
	keyFile = filepath.Join(dir, "rpc-key.pem")
	pkcs12File = filepath.Join(dir, "rpc-cert.p12")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	cmd := exec.Command("openssl", "pkcs12", "-export", "-out", pkcs12File, "-inkey", keyFile, "-in", certFile, "-passout", "pass:")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("openssl pkcs12 export: %v output=%s", err, string(out))
	}
	return certFile, keyFile, pkcs12File
}

func refUsesAppleTLS(t *testing.T) bool {
	t.Helper()
	result, err := RunRef(t, []string{"-v"}, "")
	if err != nil {
		t.Fatalf("run ref -v: %v", err)
	}
	return strings.Contains(result.Stdout, "AppleTLS") || strings.Contains(result.Stderr, "AppleTLS")
}

func writeWSClientText(t *testing.T, w io.Writer, payload []byte) {
	t.Helper()
	writeWSClientFrame(t, w, 0x1, true, payload)
}

func writeWSClientFrame(t *testing.T, w io.Writer, opcode byte, fin bool, payload []byte) {
	t.Helper()
	mask := [4]byte{}
	if _, err := rand.Read(mask[:]); err != nil {
		t.Fatalf("websocket mask rand: %v", err)
	}
	var frame bytes.Buffer
	first := opcode & 0x0f
	if fin {
		first |= 0x80
	}
	frame.WriteByte(first)
	n := len(payload)
	switch {
	case n <= 125:
		frame.WriteByte(0x80 | byte(n))
	case n <= 65535:
		frame.WriteByte(0x80 | 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		frame.Write(ext[:])
	default:
		frame.WriteByte(0x80 | 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		frame.Write(ext[:])
	}
	frame.Write(mask[:])
	for i, b := range payload {
		frame.WriteByte(b ^ mask[i%4])
	}
	if _, err := w.Write(frame.Bytes()); err != nil {
		t.Fatalf("write websocket frame: %v", err)
	}
}

func readWSServerFrame(r io.Reader) (byte, []byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	opcode := hdr[0] & 0x0f
	length := uint64(hdr[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if hdr[1]&0x80 != 0 {
		var mask [4]byte
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		return opcode, payload, nil
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return opcode, payload, nil
}

type sessionEntry struct {
	URIs    []string
	Options map[string]string
}

func readSessionEntries(t *testing.T, path string) []sessionEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read session %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Fatalf("session %s is empty", path)
	}
	var entries []sessionEntry
	var current *sessionEntry
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
			if current == nil {
				continue
			}
			optLine := strings.TrimLeft(line, " \t")
			key, value, ok := strings.Cut(optLine, "=")
			if ok {
				current.Options[key] = value
			}
			continue
		}
		parts := strings.Split(strings.TrimRight(line, "\t"), "\t")
		uris := parts[:0]
		for _, part := range parts {
			if part != "" {
				uris = append(uris, part)
			}
		}
		entries = append(entries, sessionEntry{URIs: uris, Options: map[string]string{}})
		current = &entries[len(entries)-1]
	}
	return entries
}

func requireSessionEntry(t *testing.T, label string, entries []sessionEntry, uri string) sessionEntry {
	t.Helper()
	for _, entry := range entries {
		for _, got := range entry.URIs {
			if got == uri {
				return entry
			}
		}
	}
	t.Fatalf("%s missing URI %q in %#v", label, uri, entries)
	return sessionEntry{}
}

func requireOptionValues(t *testing.T, label string, got map[string]string, want map[string]string) {
	t.Helper()
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("%s: option %s got %q want %q (all=%#v)", label, key, got[key], wantValue, got)
		}
	}
}

type activeFileValue struct {
	Index    string
	Length   string
	Selected string
	URI      string
	Status   string
}

func waitForActiveFiles(t *testing.T, port int, gid string, uri string, wantLength int) []activeFileValue {
	t.Helper()
	var last []activeFileValue
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rr := rpcDeepCallOK(t, port, "aria2.getFiles", []any{gid})
		last = normalizeActiveFiles(t, rr.Result)
		if len(last) == 1 && last[0].URI == uri && last[0].Status == "used" && last[0].Length == strconv.Itoa(wantLength) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("active getFiles on port %d did not stabilize; last=%#v", port, last)
	return nil
}

func normalizeActiveFiles(t *testing.T, raw json.RawMessage) []activeFileValue {
	t.Helper()
	var files []struct {
		Index           string `json:"index"`
		Length          string `json:"length"`
		CompletedLength string `json:"completedLength"`
		Selected        string `json:"selected"`
		URIs            []struct {
			URI    string `json:"uri"`
			Status string `json:"status"`
		} `json:"uris"`
	}
	if err := json.Unmarshal(raw, &files); err != nil {
		t.Fatalf("unmarshal active files: %v raw=%s", err, string(raw))
	}
	out := make([]activeFileValue, 0, len(files))
	for _, file := range files {
		if len(file.URIs) == 0 {
			out = append(out, activeFileValue{Index: file.Index, Length: file.Length, Selected: file.Selected})
			continue
		}
		completed, _ := strconv.ParseInt(file.CompletedLength, 10, 64)
		length, _ := strconv.ParseInt(file.Length, 10, 64)
		if completed < 0 || completed > length {
			t.Fatalf("completedLength out of range: completed=%d length=%d raw=%s", completed, length, string(raw))
		}
		out = append(out, activeFileValue{
			Index:    file.Index,
			Length:   file.Length,
			Selected: file.Selected,
			URI:      file.URIs[0].URI,
			Status:   file.URIs[0].Status,
		})
	}
	return out
}

type activeServerValue struct {
	Index      string
	URI        string
	CurrentURI string
}

func waitForActiveServers(t *testing.T, port int, gid string, uri string) []activeServerValue {
	t.Helper()
	var last []activeServerValue
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rr := rpcDeepCallOK(t, port, "aria2.getServers", []any{gid})
		last = normalizeActiveServers(t, rr.Result)
		if len(last) == 1 && last[0].URI == uri && last[0].CurrentURI == uri {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("active getServers on port %d did not stabilize; last=%#v", port, last)
	return nil
}

func normalizeActiveServers(t *testing.T, raw json.RawMessage) []activeServerValue {
	t.Helper()
	var groups []struct {
		Index   string `json:"index"`
		Servers []struct {
			URI           string `json:"uri"`
			CurrentURI    string `json:"currentUri"`
			DownloadSpeed string `json:"downloadSpeed"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatalf("unmarshal active servers: %v raw=%s", err, string(raw))
	}
	out := make([]activeServerValue, 0, len(groups))
	for _, group := range groups {
		if len(group.Servers) == 0 {
			out = append(out, activeServerValue{Index: group.Index})
			continue
		}
		if _, err := strconv.ParseInt(group.Servers[0].DownloadSpeed, 10, 64); err != nil {
			t.Fatalf("downloadSpeed is not decimal string: %q raw=%s", group.Servers[0].DownloadSpeed, string(raw))
		}
		out = append(out, activeServerValue{
			Index:      group.Index,
			URI:        group.Servers[0].URI,
			CurrentURI: group.Servers[0].CurrentURI,
		})
	}
	return out
}

func rpcDeepCallOK(t *testing.T, port int, method string, params []any) rpcResponse {
	t.Helper()
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal %s request: %v", method, err)
	}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/jsonrpc"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new %s request: %v", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Close = true
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		t.Fatalf("post %s: %v", method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", method, err)
	}
	var rr rpcResponse
	if err := json.Unmarshal(raw, &rr); err != nil {
		t.Fatalf("decode %s response: %v status=%d raw=%s", method, err, resp.StatusCode, string(raw))
	}
	if rr.Error != nil {
		t.Fatalf("%s returned error: code=%d msg=%s", method, rr.Error.Code, rr.Error.Message)
	}
	return rr
}
