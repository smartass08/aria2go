package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// refRPCVerified tracks whether we have verified that the aria2c reference
// binary responds to RPC calls.
var refRPCVerified atomic.Bool

type refHelpOptionDoc struct {
	Name     string
	Short    string
	Default  string
	Possible string
	Tags     []string
}

// findFreePort returns an available TCP port on localhost.
func findFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// rpcServer holds a running RPC server process.
type rpcServer struct {
	cmd    *exec.Cmd
	port   int
	done   chan struct{}
	cancel context.CancelFunc
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

var serverSeq atomic.Int64

func newBlockingDownloadServer(t *testing.T) *httptest.Server {
	t.Helper()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 1024))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv
}

// startRPCRef starts the reference aria2c with --enable-rpc on the given port.
func startRPCRef(t *testing.T, port int, extraArgs ...string) *rpcServer {
	t.Helper()

	args := []string{
		"--enable-rpc",
		"--rpc-listen-port=" + strconv.Itoa(port),
	}
	args = append(args, extraArgs...)

	bin, err := findRefBinary()
	if err != nil {
		t.Fatalf("find ref binary: %v", err)
	}

	return startProcess(t, bin, args)
}

// startRPCImpl starts aria2go with --enable-rpc on the given port.
func startRPCImpl(t *testing.T, port int, extraArgs ...string) *rpcServer {
	t.Helper()

	bin, err := implBinary()
	if err != nil {
		t.Fatalf("build impl: %v", err)
	}

	args := []string{
		"--enable-rpc",
		"--rpc-listen-port=" + strconv.Itoa(port),
	}
	args = append(args, extraArgs...)

	return startProcess(t, bin, args)
}

func findRefBinary() (string, error) {
	if p, err := exec.LookPath("aria2c"); err == nil {
		return filepath.Abs(p)
	}
	root, err := projectRoot()
	if err != nil {
		return "", err
	}
	names := []string{"aria2c-ref"}
	if runtime.GOOS == "windows" {
		names = append(names, "aria2c-ref.exe")
	}
	for _, name := range names {
		p := filepath.Join(root, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("aria2c not found")
}

func openDevNull(t testing.TB) (*os.File, *os.File) {
	t.Helper()
	stdout, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s for stdout: %v", os.DevNull, err)
	}
	stderr, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		stdout.Close()
		t.Fatalf("open %s for stderr: %v", os.DevNull, err)
	}
	t.Cleanup(func() {
		stdout.Close()
		stderr.Close()
	})
	return stdout, stderr
}

func startProcess(t *testing.T, bin string, args []string) *rpcServer {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	finalArgs := append(args, "--quiet")
	cmd := exec.CommandContext(ctx, bin, finalArgs...)
	cmd.Stdout, cmd.Stderr = openDevNull(t)
	cmd.WaitDelay = 2 * time.Second

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start %s: %v", bin, err)
	}

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	port := parsePort(args)

	srv := &rpcServer{
		cmd:    cmd,
		port:   port,
		done:   done,
		cancel: cancel,
	}

	select {
	case <-done:
		cancel()
		t.Fatalf("%s exited immediately before binding port %d", filepath.Base(bin), port)
	case <-time.After(200 * time.Millisecond):
	}

	return srv
}

func parsePort(args []string) int {
	for i, a := range args {
		if a == "--rpc-listen-port" && i+1 < len(args) {
			p, _ := strconv.Atoi(args[i+1])
			return p
		}
		for _, prefix := range []string{"--rpc-listen-port=", "--rpc-listen-port:"} {
			if len(a) > len(prefix) && a[:len(prefix)] == prefix {
				p, _ := strconv.Atoi(a[len(prefix):])
				return p
			}
		}
	}
	return 6800
}

func (s *rpcServer) Stop(t *testing.T) {
	t.Helper()
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		s.cmd.Process.Kill()
		<-s.done
	}
}

func (s *rpcServer) WaitReady(t *testing.T) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/jsonrpc", s.port)
	for i := 0; i < 50; i++ {
		resp, err := httpClient.Post(url, "application/json", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":"1","method":"system.listMethods","params":[]}`)))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("rpc server on port %d did not become ready", s.port)
}

// WaitReadyOrSkip is retained for older call sites, but conformance readiness
// gates must fail when the implementation RPC server is unavailable.
func (s *rpcServer) WaitReadyOrSkip(t *testing.T) {
	t.Helper()
	s.WaitReady(t)
}

// rpcRequest is a JSON-RPC 2.0 request.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcCall makes a JSON-RPC POST call to the server at port and returns the response.
func rpcCall(t *testing.T, port int, method string, params []any) rpcResponse {
	t.Helper()
	return rpcCallURL(t, port, method, params)
}

func rpcCallURL(t *testing.T, port int, method string, params []any) rpcResponse {
	t.Helper()

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      "1",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/jsonrpc", port)
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", method, err)
	}
	defer resp.Body.Close()

	var rr rpcResponse
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read response for %s: %v (status=%d)", method, readErr, resp.StatusCode)
	}
	if err := json.Unmarshal(raw, &rr); err != nil {
		t.Fatalf("decode response for %s: %v (status=%d)", method, err, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK && rr.Error == nil {
		t.Fatalf("post %s: status=%d without JSON-RPC error body=%s", method, resp.StatusCode, string(raw))
	}
	return rr
}

// rpcCallOK is like rpcCall but requires success (no error in response).
func rpcCallOK(t *testing.T, port int, method string, params []any) rpcResponse {
	t.Helper()
	rr := rpcCall(t, port, method, params)
	if rr.Error != nil {
		t.Fatalf("%s returned error: code=%d msg=%s", method, rr.Error.Code, rr.Error.Message)
	}
	return rr
}

func rpcCallExpectError(t *testing.T, port int, method string, params []any) rpcError {
	t.Helper()
	rr := rpcCall(t, port, method, params)
	if rr.Error == nil {
		t.Fatalf("%s returned success, want error result=%s", method, string(rr.Result))
	}
	return *rr.Error
}

// rpcResultString extracts the result as a string (e.g. GID or "OK").
func rpcResultString(t *testing.T, rr rpcResponse) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(rr.Result, &s); err != nil {
		t.Fatalf("unmarshal result as string: %v (raw=%s)", err, string(rr.Result))
	}
	return s
}

func compareJSONValueEqual(t *testing.T, label string, refRaw, implRaw json.RawMessage) {
	t.Helper()

	var refValue, implValue any
	if err := json.Unmarshal(refRaw, &refValue); err != nil {
		t.Fatalf("%s: unmarshal ref: %v (raw=%s)", label, err, string(refRaw))
	}
	if err := json.Unmarshal(implRaw, &implValue); err != nil {
		t.Fatalf("%s: unmarshal impl: %v (raw=%s)", label, err, string(implRaw))
	}
	if !reflect.DeepEqual(refValue, implValue) {
		refJSON, _ := json.MarshalIndent(refValue, "", "  ")
		implJSON, _ := json.MarshalIndent(implValue, "", "  ")
		t.Errorf("%s mismatch:\nref=%s\nimpl=%s", label, refJSON, implJSON)
	}
}

// compareJSONShape compares the top-level keys (shape) of two JSON objects.
// It ignores specific values and only checks that the same keys are present.
func compareJSONShape(t *testing.T, refRaw, implRaw json.RawMessage) {
	t.Helper()

	var refMap, implMap map[string]json.RawMessage
	if err := json.Unmarshal(refRaw, &refMap); err != nil {
		t.Fatalf("unmarshal ref: %v", err)
	}
	if err := json.Unmarshal(implRaw, &implMap); err != nil {
		t.Fatalf("unmarshal impl: %v", err)
	}

	// Check ref keys exist in impl.
	for k := range refMap {
		if _, ok := implMap[k]; !ok {
			// help is an aria2c implementation-specific tag, not a real option.
			if k == "help" {
				continue
			}
			t.Errorf("key %q exists in ref but missing in impl", k)
		}
	}
	// Check impl doesn't have extra keys (warn, not error, since impl may add keys).
	for k := range implMap {
		if _, ok := refMap[k]; !ok {
			t.Logf("extra key %q in impl not present in ref (may be intentional)", k)
		}
	}
}

func compareJSONObjectKeysExact(t *testing.T, refRaw, implRaw json.RawMessage) {
	t.Helper()

	refMap := mustJSONMap(t, "ref", refRaw)
	implMap := mustJSONMap(t, "impl", implRaw)
	compareStringSet(t, "JSON object keys", mapKeys(refMap), mapKeys(implMap))
}

func compareGlobalOptionKeysExact(t *testing.T, refRaw, implRaw json.RawMessage) {
	t.Helper()

	refMap := mustJSONMap(t, "ref getGlobalOption", refRaw)
	implMap := mustJSONMap(t, "impl getGlobalOption", implRaw)
	normalizeBuildDependentGlobalOptionKeys(refMap, implMap)
	compareStringSet(t, "JSON object keys", mapKeys(refMap), mapKeys(implMap))
}

func normalizeBuildDependentGlobalOptionKeys(refMap, implMap map[string]json.RawMessage) {
	// aria2's ca-certificate default is a compile-time CA_BUNDLE when available.
	// Some reference builds expose it and others do not; both follow the C++ source.
	dropIfMismatched(refMap, implMap, "ca-certificate")
	// aria2 registers rlimit-nofile only when HAVE_SYS_RESOURCE_H is available.
	// Windows reference builds omit it, while aria2go exposes the accepted no-op option.
	dropIfMismatched(refMap, implMap, "rlimit-nofile")
}

func dropIfMismatched(refMap, implMap map[string]json.RawMessage, key string) {
	_, refHas := refMap[key]
	_, implHas := implMap[key]
	if refHas != implHas {
		delete(refMap, key)
		delete(implMap, key)
	}
}

// compareJSONShapeSlice compares the top-level key shapes of two JSON arrays of objects.
func compareJSONShapeSlice(t *testing.T, refRaw, implRaw json.RawMessage) {
	t.Helper()

	var refArr, implArr []json.RawMessage
	if err := json.Unmarshal(refRaw, &refArr); err != nil {
		t.Fatalf("unmarshal ref array: %v", err)
	}
	if err := json.Unmarshal(implRaw, &implArr); err != nil {
		t.Fatalf("unmarshal impl array: %v", err)
	}

	if len(refArr) != len(implArr) {
		t.Errorf("array length differs: ref=%d impl=%d", len(refArr), len(implArr))
	}

	for i := 0; i < len(refArr) && i < len(implArr); i++ {
		compareJSONShape(t, refArr[i], implArr[i])
	}
}

func compareStringSet(t *testing.T, label string, ref, impl []string) {
	t.Helper()

	refSet := make(map[string]struct{}, len(ref))
	for _, s := range ref {
		refSet[s] = struct{}{}
	}
	implSet := make(map[string]struct{}, len(impl))
	for _, s := range impl {
		implSet[s] = struct{}{}
	}
	for s := range refSet {
		if _, ok := implSet[s]; !ok {
			t.Errorf("%s: %q present in ref but missing in impl", label, s)
		}
	}
	for s := range implSet {
		if _, ok := refSet[s]; !ok {
			t.Errorf("%s: %q present in impl but missing in ref", label, s)
		}
	}
}

func compareStringMapValues(t *testing.T, label string, refRaw, implRaw json.RawMessage, keys []string) {
	t.Helper()

	refMap := mustStringMap(t, "ref "+label, refRaw)
	implMap := mustStringMap(t, "impl "+label, implRaw)
	for _, key := range keys {
		refValue, refOK := refMap[key]
		implValue, implOK := implMap[key]
		if !refOK {
			t.Errorf("%s: ref missing key %q", label, key)
			continue
		}
		if !implOK {
			t.Errorf("%s: impl missing key %q", label, key)
			continue
		}
		if refValue != implValue {
			t.Errorf("%s: key %q mismatch: ref=%q impl=%q", label, key, refValue, implValue)
		}
	}
}

func refHelpAllOptionDocs(t *testing.T) map[string]refHelpOptionDoc {
	t.Helper()

	result, err := RunRefWithOptions(t, []string{"--help=#all"}, "", RunOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("run reference help: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("reference help exited %d\nstdout=%s\nstderr=%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	return parseRefHelpOptionDocs(result.Stdout)
}

func requireRefHelpOptions(t *testing.T, names ...string) map[string]refHelpOptionDoc {
	t.Helper()

	docs := refHelpAllOptionDocs(t)
	for _, name := range names {
		if _, ok := docs[name]; !ok {
			t.Fatalf("reference --help=#all missing option %q", name)
		}
	}
	return docs
}

func parseRefHelpOptionDocs(out string) map[string]refHelpOptionDoc {
	docs := make(map[string]refHelpOptionDoc)
	var current string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if name, short, ok := parseRefHelpOptionHeader(trimmed); ok {
			doc := docs[name]
			doc.Name = name
			doc.Short = short
			docs[name] = doc
			current = name
			continue
		}
		if current == "" {
			continue
		}
		doc := docs[current]
		switch {
		case strings.HasPrefix(trimmed, "Possible Values:"):
			doc.Possible = strings.TrimSpace(strings.TrimPrefix(trimmed, "Possible Values:"))
		case strings.HasPrefix(trimmed, "Default:"):
			doc.Default = strings.TrimSpace(strings.TrimPrefix(trimmed, "Default:"))
		case strings.HasPrefix(trimmed, "Tags:"):
			tagText := strings.TrimSpace(strings.TrimPrefix(trimmed, "Tags:"))
			if tagText != "" {
				doc.Tags = strings.Fields(strings.ReplaceAll(tagText, ",", ""))
			}
		}
		docs[current] = doc
	}
	return docs
}

func parseRefHelpOptionHeader(trimmed string) (name, short string, ok bool) {
	if !strings.HasPrefix(trimmed, "-") {
		return "", "", false
	}
	idx := strings.Index(trimmed, "--")
	if idx < 0 {
		return "", "", false
	}
	if idx > 0 {
		short = strings.TrimSpace(strings.TrimSuffix(trimmed[:idx], ","))
	}
	rest := trimmed[idx+2:]
	end := 0
	for end < len(rest) {
		c := rest[end]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return "", "", false
	}
	return rest[:end], short, true
}

func compareJSONStringFields(t *testing.T, label string, refRaw, implRaw json.RawMessage, keys []string) {
	t.Helper()

	refMap := mustJSONMap(t, "ref "+label, refRaw)
	implMap := mustJSONMap(t, "impl "+label, implRaw)
	for _, key := range keys {
		refRawValue, refOK := refMap[key]
		implRawValue, implOK := implMap[key]
		if !refOK {
			t.Errorf("%s: ref missing key %q", label, key)
			continue
		}
		if !implOK {
			t.Errorf("%s: impl missing key %q", label, key)
			continue
		}
		var refValue, implValue string
		if err := json.Unmarshal(refRawValue, &refValue); err != nil {
			t.Fatalf("%s: ref key %q is not a string: %v", label, key, err)
		}
		if err := json.Unmarshal(implRawValue, &implValue); err != nil {
			t.Fatalf("%s: impl key %q is not a string: %v", label, key, err)
		}
		if refValue != implValue {
			t.Errorf("%s: key %q mismatch: ref=%q impl=%q", label, key, refValue, implValue)
		}
	}
}

func requireStringMapValues(t *testing.T, label string, raw json.RawMessage, expected map[string]string) {
	t.Helper()

	values := mustStringMap(t, label, raw)
	for key, want := range expected {
		got, ok := values[key]
		if !ok {
			t.Errorf("%s: missing key %q", label, key)
			continue
		}
		if got != want {
			t.Errorf("%s: key %q got %q want %q", label, key, got, want)
		}
	}
}

func requireStringMapAbsent(t *testing.T, label string, raw json.RawMessage, keys []string) {
	t.Helper()

	values := mustStringMap(t, label, raw)
	for _, key := range keys {
		if _, ok := values[key]; ok {
			t.Errorf("%s: key %q present, want absent", label, key)
		}
	}
}

func requireStringMapSliceKeysExact(t *testing.T, label string, raw json.RawMessage, want []string) []map[string]string {
	t.Helper()

	values := mustStringMapSlice(t, label, raw)
	for i, value := range values {
		requireStringKeysExact(t, fmt.Sprintf("%s[%d]", label, i), value, want)
	}
	return values
}

func requireGIDSequence(t *testing.T, label string, raw json.RawMessage, want []string) {
	t.Helper()

	values := requireStringMapSliceKeysExact(t, label, raw, []string{"gid"})
	if len(values) != len(want) {
		t.Fatalf("%s: got %d entries want %d: %#v", label, len(values), len(want), values)
	}
	for i, wantGID := range want {
		if got := values[i]["gid"]; got != wantGID {
			t.Errorf("%s[%d] gid got %q want %q", label, i, got, wantGID)
		}
	}
}

func requireNumberList(t *testing.T, label string, raw json.RawMessage, want []int) {
	t.Helper()

	var got []int
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s: unmarshal number list: %v (raw=%s)", label, err, string(raw))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: got %#v want %#v", label, got, want)
	}
}

func requireNumberValue(t *testing.T, label string, raw json.RawMessage, want int) {
	t.Helper()

	var got int
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s: unmarshal number: %v (raw=%s)", label, err, string(raw))
	}
	if got != want {
		t.Errorf("%s: got %d want %d", label, got, want)
	}
}

func requireStringKeysExact(t *testing.T, label string, got map[string]string, want []string) {
	t.Helper()

	gotSet := make(map[string]struct{}, len(got))
	for key := range got {
		gotSet[key] = struct{}{}
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, key := range want {
		wantSet[key] = struct{}{}
		if _, ok := gotSet[key]; !ok {
			t.Errorf("%s: missing key %q", label, key)
		}
	}
	for key := range gotSet {
		if _, ok := wantSet[key]; !ok {
			t.Errorf("%s: unexpected key %q", label, key)
		}
	}
}

func mustJSONMap(t *testing.T, label string, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()

	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("unmarshal %s object: %v (raw=%s)", label, err, string(raw))
	}
	return values
}

func mustStringMap(t *testing.T, label string, raw json.RawMessage) map[string]string {
	t.Helper()

	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("unmarshal %s string map: %v (raw=%s)", label, err, string(raw))
	}
	return values
}

func mustStringMapSlice(t *testing.T, label string, raw json.RawMessage) []map[string]string {
	t.Helper()

	var values []map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("unmarshal %s string map slice: %v (raw=%s)", label, err, string(raw))
	}
	return values
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
