package conformance

// rpc_parity_messages_test.go — parity/rpc branch conformance tests.
//
// B5. TestRPCDeep_SecureTransportPEMSeparateKeyMatrix
//     Separate PEM cert + PEM key (--rpc-certificate=cert.pem --rpc-private-key=key.pem).
//     Asserts HTTPS + WSS system.listMethods work on both binaries.
//
// B6. TestRPCDeep_XMLRPCUploadMatrix
//     XML-RPC aria2.addTorrent with a real torrent payload as <base64>.
//     Asserts both binaries return a valid 16-hex GID and subsequent tellStatus succeeds.
//
// B7. TestRPCConformance_OptionValidationEdges
//     Tests option validation error codes + messages and silent-ignore behaviour.

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// B5. PEM separate cert + key
// ---------------------------------------------------------------------------

// TestRPCDeep_SecureTransportPEMSeparateKeyMatrix tests that both the
// reference aria2c and aria2go correctly serve HTTPS and WSS when started
// with a separate PEM certificate file and a separate PEM private-key file.
func TestRPCDeep_SecureTransportPEMSeparateKeyMatrix(t *testing.T) {
	SkipIfNoRef(t)
	if refUsesAppleTLS(t) {
		t.Skip("reference aria2c uses AppleTLS; secure RPC needs KeyChain-backed certificate setup")
	}

	// Confirm the reference binary supports HTTPS before proceeding.
	insecureRefPort, _ := startPairedRPCServers(t, "--no-conf")
	if !rpcFeatureEnabled(t, insecureRefPort, "HTTPS") {
		t.Skip("reference aria2c does not advertise HTTPS support")
	}

	certFile, keyFile, _ := writeRPCSecureTestMaterial(t)

	refPort := findFreePort(t)
	implPort := findFreePort(t)

	refSrv := startRPCRef(t, refPort,
		"--no-conf",
		"--rpc-secure=true",
		"--rpc-certificate="+certFile,
		"--rpc-private-key="+keyFile,
	)
	defer refSrv.Stop(t)
	waitReadyHTTPS(t, refPort)

	implSrv := startRPCImpl(t, implPort,
		"--no-conf",
		"--rpc-secure=true",
		"--rpc-certificate="+certFile,
		"--rpc-private-key="+keyFile,
	)
	defer implSrv.Stop(t)
	waitReadyHTTPS(t, implPort)

	client := insecureTLSHTTPClient()
	reqBody := []byte(`{"jsonrpc":"2.0","id":"methods","method":"system.listMethods","params":[]}`)

	// ---- HTTPS ----
	refStatus, refRaw := rpcPostRaw(t, client, "https", refPort, reqBody)
	implStatus, implRaw := rpcPostRaw(t, client, "https", implPort, reqBody)
	if refStatus != http.StatusOK {
		t.Fatalf("ref PEM HTTPS status got %d want 200", refStatus)
	}
	if implStatus != refStatus {
		t.Fatalf("impl PEM HTTPS status got %d want ref status %d", implStatus, refStatus)
	}
	refResp := decodeDeepRPCResponse(t, "ref PEM HTTPS", refRaw)
	implResp := decodeDeepRPCResponse(t, "impl PEM HTTPS", implRaw)
	if refResp.Error != nil || implResp.Error != nil {
		t.Fatalf("PEM HTTPS returned errors: ref=%#v impl=%#v", refResp.Error, implResp.Error)
	}
	compareStringSet(t, "PEM HTTPS listMethods",
		mustRPCStringSlice(t, "ref PEM HTTPS listMethods", refResp.Result),
		mustRPCStringSlice(t, "impl PEM HTTPS listMethods", implResp.Result))

	// ---- WSS ----
	if !rpcFeatureEnabled(t, insecureRefPort, "WebSocket") {
		t.Log("reference does not advertise WebSocket; skipping WSS sub-check")
		return
	}
	refWS := wsCall(t, openSecureWebSocket(t, refPort),
		[]byte(`{"jsonrpc":"2.0","id":"ws","method":"system.listMethods","params":[]}`))
	implWS := wsCall(t, openSecureWebSocket(t, implPort),
		[]byte(`{"jsonrpc":"2.0","id":"ws","method":"system.listMethods","params":[]}`))
	if refWS.Error != nil || implWS.Error != nil {
		t.Fatalf("PEM WSS returned errors: ref=%#v impl=%#v", refWS.Error, implWS.Error)
	}
	compareStringSet(t, "PEM WSS listMethods",
		mustRPCStringSlice(t, "ref PEM WSS listMethods", refWS.Result),
		mustRPCStringSlice(t, "impl PEM WSS listMethods", implWS.Result))
}

// ---------------------------------------------------------------------------
// B6. XML-RPC addTorrent upload
// ---------------------------------------------------------------------------

// rpcXMLBase64Val causes rpcXMLWriteValue to emit a <base64> element rather
// than a <string> element.
type rpcXMLBase64Val struct{ Encoded string }

// rpcXMLBuildCall returns a complete XML-RPC methodCall body. It supports the
// rpcXMLBase64Val type so callers can embed binary payloads as <base64>.
func rpcXMLBuildCall(method string, params ...any) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	rpcXMLEscapeStr(&b, method)
	b.WriteString(`</methodName><params>`)
	for _, p := range params {
		b.WriteString(`<param>`)
		rpcXMLWriteValue(&b, p)
		b.WriteString(`</param>`)
	}
	b.WriteString(`</params></methodCall>`)
	return b.String()
}

func rpcXMLWriteValue(b *strings.Builder, v any) {
	b.WriteString(`<value>`)
	switch x := v.(type) {
	case rpcXMLBase64Val:
		b.WriteString(`<base64>`)
		b.WriteString(x.Encoded)
		b.WriteString(`</base64>`)
	case string:
		b.WriteString(`<string>`)
		rpcXMLEscapeStr(b, x)
		b.WriteString(`</string>`)
	case []any:
		b.WriteString(`<array><data>`)
		for _, elem := range x {
			rpcXMLWriteValue(b, elem)
		}
		b.WriteString(`</data></array>`)
	case []string:
		b.WriteString(`<array><data>`)
		for _, elem := range x {
			rpcXMLWriteValue(b, elem)
		}
		b.WriteString(`</data></array>`)
	case map[string]string:
		b.WriteString(`<struct>`)
		for key, val := range x {
			b.WriteString(`<member><name>`)
			rpcXMLEscapeStr(b, key)
			b.WriteString(`</name>`)
			rpcXMLWriteValue(b, val)
			b.WriteString(`</member>`)
		}
		b.WriteString(`</struct>`)
	}
	b.WriteString(`</value>`)
}

func rpcXMLEscapeStr(b *strings.Builder, s string) {
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
}

// rpcXMLPostBody posts a hand-crafted XML-RPC body to /rpc and parses the reply.
func rpcXMLPostBody(t *testing.T, port int, label string, body string) xmlRPCReply {
	t.Helper()
	resp, err := httpClient.Post(
		"http://127.0.0.1:"+strconv.Itoa(port)+"/rpc",
		"text/xml",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("%s: post XML-RPC: %v", label, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s: read XML-RPC response: %v", label, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: XML-RPC HTTP status got %d body=%s", label, resp.StatusCode, string(raw))
	}
	return parseXMLRPCReply(t, label, raw)
}

// TestRPCDeep_XMLRPCUploadMatrix verifies aria2.addTorrent via XML-RPC with
// a real torrent payload sent as a <base64> element. Both the reference
// binary and aria2go must return a valid 16-hex GID, and a subsequent
// aria2.tellStatus call must succeed.
func TestRPCDeep_XMLRPCUploadMatrix(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPair(t,
		[]string{"--no-conf", "--pause=true", "--dir=/tmp", "--enable-dht=false", "--bt-enable-lpd=false"},
		[]string{"--no-conf", "--pause=true", "--dir=/tmp", "--enable-dht=false", "--bt-enable-lpd=false"},
	)
	if !rpcFeatureEnabled(t, refPort, "XML-RPC") {
		t.Skip("reference aria2c does not advertise XML-RPC support")
	}

	// Load the single-file torrent fixture.
	root, err := projectRoot()
	if err != nil {
		t.Fatalf("project root: %v", err)
	}
	torrentBytes, err := os.ReadFile(filepath.Join(root, "internal/torrent/testdata/single.torrent"))
	if err != nil {
		t.Fatalf("read torrent fixture: %v", err)
	}
	torrentB64 := base64.StdEncoding.EncodeToString(torrentBytes)

	xmlBody := rpcXMLBuildCall("aria2.addTorrent",
		rpcXMLBase64Val{Encoded: torrentB64},
		[]string{},
		map[string]string{"pause": "true"},
	)

	refReply := rpcXMLPostBody(t, refPort, "ref XML addTorrent", xmlBody)
	implReply := rpcXMLPostBody(t, implPort, "impl XML addTorrent", xmlBody)

	requireNoXMLFault(t, "ref XML addTorrent", refReply)
	requireNoXMLFault(t, "impl XML addTorrent", implReply)

	refGID, _ := refReply.Value.(string)
	implGID, _ := implReply.Value.(string)
	if !rpcGIDPattern.MatchString(refGID) {
		t.Fatalf("ref XML addTorrent GID got %q, want 16-hex", refGID)
	}
	if !rpcGIDPattern.MatchString(implGID) {
		t.Fatalf("impl XML addTorrent GID got %q, want 16-hex", implGID)
	}
	t.Logf("ref GID=%s impl GID=%s", refGID, implGID)

	// Follow-up: both tellStatus calls must succeed.
	refStatus := rpcCallOK(t, refPort, "aria2.tellStatus", []any{refGID})
	implStatus := rpcCallOK(t, implPort, "aria2.tellStatus", []any{implGID})

	var refStatusMap, implStatusMap map[string]json.RawMessage
	if err := json.Unmarshal(refStatus.Result, &refStatusMap); err != nil {
		t.Fatalf("unmarshal ref tellStatus: %v", err)
	}
	if err := json.Unmarshal(implStatus.Result, &implStatusMap); err != nil {
		t.Fatalf("unmarshal impl tellStatus: %v", err)
	}
	if _, ok := refStatusMap["gid"]; !ok {
		t.Fatalf("ref tellStatus missing 'gid' key: %#v", refStatusMap)
	}
	if _, ok := implStatusMap["gid"]; !ok {
		t.Fatalf("impl tellStatus missing 'gid' key: %#v", implStatusMap)
	}
}

// ---------------------------------------------------------------------------
// B7. Option-validation edges
// ---------------------------------------------------------------------------

// TestRPCConformance_OptionValidationEdges ensures that option validation
// errors and silent-ignore behaviours match between aria2c and aria2go.
func TestRPCConformance_OptionValidationEdges(t *testing.T) {
	SkipIfNoRef(t)

	refPort, implPort := startRPCPairFocused(t,
		[]string{"--no-conf", "--dir=/tmp"},
		[]string{"--no-conf", "--dir=/tmp"},
	)

	t.Run("invalid numeric max-concurrent-downloads", func(t *testing.T) {
		params := []any{map[string]any{"max-concurrent-downloads": "notnum"}}
		refErr := rpcCallExpectError(t, refPort, "aria2.changeGlobalOption", params)
		implErr := rpcCallExpectError(t, implPort, "aria2.changeGlobalOption", params)
		if refErr.Code != implErr.Code {
			t.Errorf("code mismatch: ref=%d impl=%d", refErr.Code, implErr.Code)
		}
		if refErr.Message != implErr.Message {
			t.Errorf("message mismatch: ref=%q impl=%q", refErr.Message, implErr.Message)
		}
	})

	t.Run("invalid enum bt-min-crypto-level", func(t *testing.T) {
		params := []any{map[string]any{"bt-min-crypto-level": "invalid-level"}}
		refErr := rpcCallExpectError(t, refPort, "aria2.changeGlobalOption", params)
		implErr := rpcCallExpectError(t, implPort, "aria2.changeGlobalOption", params)
		if refErr.Code != implErr.Code {
			t.Errorf("code mismatch: ref=%d impl=%d", refErr.Code, implErr.Code)
		}
		if refErr.Message != implErr.Message {
			t.Errorf("message mismatch: ref=%q impl=%q", refErr.Message, implErr.Message)
		}
	})

	t.Run("invalid enum log-level", func(t *testing.T) {
		params := []any{map[string]any{"log-level": "invalid-level"}}
		refErr := rpcCallExpectError(t, refPort, "aria2.changeGlobalOption", params)
		implErr := rpcCallExpectError(t, implPort, "aria2.changeGlobalOption", params)
		if refErr.Code != implErr.Code {
			t.Errorf("code mismatch: ref=%d impl=%d", refErr.Code, implErr.Code)
		}
		if refErr.Message != implErr.Message {
			t.Errorf("message mismatch: ref=%q impl=%q", refErr.Message, implErr.Message)
		}
	})

	t.Run("split=0 in addUri", func(t *testing.T) {
		params := []any{
			[]string{"http://127.0.0.1:1/test.bin"},
			map[string]any{"split": "0"},
		}
		refErr := rpcCallExpectError(t, refPort, "aria2.addUri", params)
		implErr := rpcCallExpectError(t, implPort, "aria2.addUri", params)
		if refErr.Code != implErr.Code {
			t.Errorf("code mismatch: ref=%d impl=%d", refErr.Code, implErr.Code)
		}
		if refErr.Message != implErr.Message {
			t.Errorf("message mismatch: ref=%q impl=%q", refErr.Message, implErr.Message)
		}
	})

	t.Run("unknown option in changeGlobalOption silently ignored", func(t *testing.T) {
		params := []any{map[string]any{"completely-unknown-aria2go-parity-test-option": "value"}}
		refRR := rpcCallOK(t, refPort, "aria2.changeGlobalOption", params)
		implRR := rpcCallOK(t, implPort, "aria2.changeGlobalOption", params)
		if rpcResultString(t, refRR) != "OK" {
			t.Errorf("ref want OK, got %s", string(refRR.Result))
		}
		if rpcResultString(t, implRR) != "OK" {
			t.Errorf("impl want OK, got %s", string(implRR.Result))
		}
	})

	t.Run("unknown option in addUri silently ignored", func(t *testing.T) {
		params := []any{
			[]string{"http://127.0.0.1:1/test.bin"},
			map[string]any{"completely-unknown-aria2go-parity-test-option": "value"},
		}
		refRR := rpcCallOK(t, refPort, "aria2.addUri", params)
		implRR := rpcCallOK(t, implPort, "aria2.addUri", params)
		if !rpcGIDPattern.MatchString(rpcResultString(t, refRR)) {
			t.Errorf("ref addUri unknown option: want 16-hex GID, got %s", string(refRR.Result))
		}
		if !rpcGIDPattern.MatchString(rpcResultString(t, implRR)) {
			t.Errorf("impl addUri unknown option: want 16-hex GID, got %s", string(implRR.Result))
		}
	})
}
