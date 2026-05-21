package conformance

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigBehavior_HTTPRequestOptionsFromConfig(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "conf-path", "header", "user-agent", "referer")

	payload := []byte("config-backed request options\n")
	refSrv := newConfigBehaviorHTTPServer(t, map[string][]byte{"/config.bin": payload}, nil)
	implSrv := newConfigBehaviorHTTPServer(t, map[string][]byte{"/config.bin": payload}, nil)

	refDir := t.TempDir()
	implDir := t.TempDir()
	refConf := writeConfigBehaviorConf(t, refDir, "config.bin", []string{
		"user-agent=aria2go-config-behavior",
		"referer=http://127.0.0.1/config-ref",
		"header=X-Config-Behavior: present",
	})
	implConf := writeConfigBehaviorConf(t, implDir, "config.bin", []string{
		"user-agent=aria2go-config-behavior",
		"referer=http://127.0.0.1/config-ref",
		"header=X-Config-Behavior: present",
	})

	ref := runConfigBehaviorProcess(t, true, []string{"--conf-path=" + refConf, refSrv.URL("/config.bin")}, "", cleanProxyEnv())
	impl := runConfigBehaviorProcess(t, false, []string{"--conf-path=" + implConf, implSrv.URL("/config.bin")}, "", cleanProxyEnv())

	AssertEqualExit(t, ref, impl)
	requireExitSuccess(t, "ref config headers", ref)
	requireExitSuccess(t, "impl config headers", impl)
	requireDownloadedBytes(t, filepath.Join(refDir, "config.bin"), payload)
	requireDownloadedBytes(t, filepath.Join(implDir, "config.bin"), payload)
	refSrv.RequireRequest(t, "/config.bin", map[string]string{
		"User-Agent":        "aria2go-config-behavior",
		"Referer":           "http://127.0.0.1/config-ref",
		"X-Config-Behavior": "present",
	})
	implSrv.RequireRequest(t, "/config.bin", map[string]string{
		"User-Agent":        "aria2go-config-behavior",
		"Referer":           "http://127.0.0.1/config-ref",
		"X-Config-Behavior": "present",
	})
}

func TestConfigBehavior_ProxyEnvAndNoProxy(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "all-proxy", "no-proxy", "proxy-method")

	t.Run("env_all_proxy_routes_through_local_connect_proxy", func(t *testing.T) {
		probeProxyBehavior(t, true, true, false, true)
		probeProxyBehavior(t, false, true, false, true)
	})

	t.Run("no_proxy_bypasses_local_connect_proxy", func(t *testing.T) {
		probeProxyBehavior(t, true, false, true, false)
		probeProxyBehavior(t, false, false, true, false)
	})
}

func TestConfigBehavior_NetrcHTTPCredentialPrecedence(t *testing.T) {
	SkipIfNoRef(t)
	if runtime.GOOS == "windows" {
		t.Skip("netrc permission behavior differs on Windows")
	}
	requireRefHelpOptions(t, "netrc-path", "no-netrc", "http-user", "http-passwd")

	payload := []byte("netrc credential payload\n")
	for _, tt := range []struct {
		name      string
		args      []string
		wantUser  string
		wantPass  string
		netrcUser string
		netrcPass string
	}{
		{
			name:      "netrc supplies HTTP credentials",
			wantUser:  "netrc-user",
			wantPass:  "netrc-pass",
			netrcUser: "netrc-user",
			netrcPass: "netrc-pass",
		},
		{
			name:      "explicit HTTP credentials override netrc",
			args:      []string{"--http-user=cli-user", "--http-passwd=cli-pass"},
			wantUser:  "cli-user",
			wantPass:  "cli-pass",
			netrcUser: "netrc-user",
			netrcPass: "netrc-pass",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			refDir := t.TempDir()
			implDir := t.TempDir()
			refSrv := newConfigBehaviorHTTPServer(t, map[string][]byte{"/auth.bin": payload}, &basicAuthExpectation{user: tt.wantUser, pass: tt.wantPass})
			implSrv := newConfigBehaviorHTTPServer(t, map[string][]byte{"/auth.bin": payload}, &basicAuthExpectation{user: tt.wantUser, pass: tt.wantPass})
			refNetrc := writeNetrcForHost(t, refSrv.Hostname(), tt.netrcUser, tt.netrcPass)
			implNetrc := writeNetrcForHost(t, implSrv.Hostname(), tt.netrcUser, tt.netrcPass)

			refArgs := append(configBehaviorDownloadArgs(refDir, "auth.bin"),
				"--netrc-path="+refNetrc,
				"--no-netrc=false",
			)
			refArgs = append(refArgs, tt.args...)
			refArgs = append(refArgs, refSrv.URL("/auth.bin"))

			implArgs := append(configBehaviorDownloadArgs(implDir, "auth.bin"),
				"--netrc-path="+implNetrc,
				"--no-netrc=false",
			)
			implArgs = append(implArgs, tt.args...)
			implArgs = append(implArgs, implSrv.URL("/auth.bin"))

			ref := runConfigBehaviorProcess(t, true, refArgs, "", cleanProxyEnv())
			impl := runConfigBehaviorProcess(t, false, implArgs, "", cleanProxyEnv())

			AssertEqualExit(t, ref, impl)
			requireExitSuccess(t, "ref netrc", ref)
			requireExitSuccess(t, "impl netrc", impl)
			requireDownloadedBytes(t, filepath.Join(refDir, "auth.bin"), payload)
			requireDownloadedBytes(t, filepath.Join(implDir, "auth.bin"), payload)
			refSrv.RequireBasicAuth(t, "/auth.bin", tt.wantUser)
			implSrv.RequireBasicAuth(t, "/auth.bin", tt.wantUser)
		})
	}
}

func TestConfigBehavior_InputFilePerEntryOptions(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "input-file", "dir", "out", "header", "user-agent", "referer")

	payloads := map[string][]byte{
		"/entry-one.bin": []byte("input entry one\n"),
		"/entry-two.bin": []byte("input entry two\n"),
	}
	refSrv := newConfigBehaviorHTTPServer(t, payloads, nil)
	implSrv := newConfigBehaviorHTTPServer(t, payloads, nil)
	refDir := t.TempDir()
	implDir := t.TempDir()
	refInput := writeConfigBehaviorInputFile(t, refDir, refSrv)
	implInput := writeConfigBehaviorInputFile(t, implDir, implSrv)

	ref := runConfigBehaviorProcess(t, true, append(inputFileArgs(), "--input-file="+refInput), "", cleanProxyEnv())
	impl := runConfigBehaviorProcess(t, false, append(inputFileArgs(), "--input-file="+implInput), "", cleanProxyEnv())

	AssertEqualExit(t, ref, impl)
	requireExitSuccess(t, "ref input-file per entry", ref)
	requireExitSuccess(t, "impl input-file per entry", impl)
	requireDownloadedBytes(t, filepath.Join(refDir, "entry-one.out"), payloads["/entry-one.bin"])
	requireDownloadedBytes(t, filepath.Join(refDir, "entry-two.out"), payloads["/entry-two.bin"])
	requireDownloadedBytes(t, filepath.Join(implDir, "entry-one.out"), payloads["/entry-one.bin"])
	requireDownloadedBytes(t, filepath.Join(implDir, "entry-two.out"), payloads["/entry-two.bin"])
	requireInputEntryRequest(t, refSrv, "/entry-one.bin", "one")
	requireInputEntryRequest(t, refSrv, "/entry-two.bin", "two")
	requireInputEntryRequest(t, implSrv, "/entry-one.bin", "one")
	requireInputEntryRequest(t, implSrv, "/entry-two.bin", "two")
}

func TestConfigBehavior_SaveSessionReloadsPausedDownload(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "save-session", "input-file", "pause", "dir", "out")

	payload := []byte("session reload payload\n")
	refSrv := newConfigBehaviorHTTPServer(t, map[string][]byte{"/session.bin": payload}, nil)
	implSrv := newConfigBehaviorHTTPServer(t, map[string][]byte{"/session.bin": payload}, nil)

	refDir := t.TempDir()
	implDir := t.TempDir()
	refSession := filepath.Join(t.TempDir(), "ref.session")
	implSession := filepath.Join(t.TempDir(), "impl.session")

	savePausedSessionViaRPC(t, true, refSession, refDir, refSrv.URL("/session.bin"), "session.bin")
	savePausedSessionViaRPC(t, false, implSession, implDir, implSrv.URL("/session.bin"), "session.bin")

	reloadPausedSessionAndComplete(t, true, refSession, refDir, "session.bin", payload)
	reloadPausedSessionAndComplete(t, false, implSession, implDir, "session.bin", payload)
}

func TestConfigBehavior_HookArgumentsForStartCompleteError(t *testing.T) {
	SkipIfNoRef(t)
	if runtime.GOOS == "windows" {
		t.Skip("hook script uses POSIX shell")
	}
	requireRefHelpOptions(t, "on-download-start", "on-download-complete", "on-download-error")

	payload := []byte("hook success payload\n")
	for _, ref := range []bool{true, false} {
		label := "impl"
		if ref {
			label = "ref"
		}
		t.Run(label+"_success_hooks", func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "hooks.log")
			startHook := writeHookScript(t, dir, "start", logPath)
			completeHook := writeHookScript(t, dir, "complete", logPath)
			srv := newConfigBehaviorHTTPServer(t, map[string][]byte{"/hook-ok.bin": payload}, nil)

			result := runConfigBehaviorProcess(t, ref, append(configBehaviorDownloadArgs(dir, "hook-ok.bin"),
				"--on-download-start="+startHook,
				"--on-download-complete="+completeHook,
				srv.URL("/hook-ok.bin"),
			), "", cleanProxyEnv())
			requireExitSuccess(t, label+" hook success", result)
			requireDownloadedBytes(t, filepath.Join(dir, "hook-ok.bin"), payload)

			events := waitHookEvents(t, logPath, "start", "complete")
			requireHookEvent(t, events["start"], filepath.Join(dir, "hook-ok.bin"))
			requireHookEvent(t, events["complete"], filepath.Join(dir, "hook-ok.bin"))
		})

		t.Run(label+"_error_hook", func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "hooks.log")
			startHook := writeHookScript(t, dir, "start", logPath)
			errorHook := writeHookScript(t, dir, "error", logPath)
			srv := newConfigBehaviorHTTPServer(t, map[string][]byte{}, nil)

			result := runConfigBehaviorProcess(t, ref, append(configBehaviorDownloadArgs(dir, "hook-error.bin"),
				"--max-tries=1",
				"--retry-wait=0",
				"--on-download-start="+startHook,
				"--on-download-error="+errorHook,
				srv.URL("/missing.bin"),
			), "", cleanProxyEnv())
			if result.ExitCode == 0 {
				t.Fatalf("%s hook error download succeeded unexpectedly\nstdout=%s\nstderr=%s", label, result.Stdout, result.Stderr)
			}

			events := waitHookEvents(t, logPath, "start", "error")
			requireHookEvent(t, events["start"], filepath.Join(dir, "hook-error.bin"))
			requireHookEvent(t, events["error"], filepath.Join(dir, "hook-error.bin"))
		})
	}
}

type basicAuthExpectation struct {
	user string
	pass string
}

type configBehaviorHTTPServer struct {
	server *httptest.Server
	auth   *basicAuthExpectation

	mu      sync.Mutex
	records []configBehaviorHTTPRequest
	payload map[string][]byte
}

type configBehaviorHTTPRequest struct {
	Path          string
	Header        http.Header
	HasBasicAuth  bool
	BasicAuthUser string
}

func newConfigBehaviorHTTPServer(t *testing.T, payload map[string][]byte, auth *basicAuthExpectation) *configBehaviorHTTPServer {
	t.Helper()

	s := &configBehaviorHTTPServer{
		auth:    auth,
		payload: payload,
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *configBehaviorHTTPServer) URL(path string) string {
	return s.server.URL + path
}

func (s *configBehaviorHTTPServer) Hostname() string {
	u := strings.TrimPrefix(s.server.URL, "http://")
	host, _, err := net.SplitHostPort(u)
	if err != nil {
		return u
	}
	return host
}

func (s *configBehaviorHTTPServer) handle(w http.ResponseWriter, r *http.Request) {
	user, pass, ok := r.BasicAuth()
	s.mu.Lock()
	s.records = append(s.records, configBehaviorHTTPRequest{
		Path:          r.URL.Path,
		Header:        r.Header.Clone(),
		HasBasicAuth:  ok,
		BasicAuthUser: user,
	})
	s.mu.Unlock()

	if s.auth != nil && (!ok || user != s.auth.user || pass != s.auth.pass) {
		w.Header().Set("WWW-Authenticate", `Basic realm="config-behavior"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	payload, ok := s.payload[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}

func (s *configBehaviorHTTPServer) Records(path string) []configBehaviorHTTPRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []configBehaviorHTTPRequest
	for _, record := range s.records {
		if record.Path == path {
			out = append(out, record)
		}
	}
	return out
}

func (s *configBehaviorHTTPServer) RequireRequest(t *testing.T, path string, headers map[string]string) {
	t.Helper()
	for _, record := range s.Records(path) {
		matched := true
		for key, want := range headers {
			if got := record.Header.Get(key); got != want {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("server saw no %s request with headers %#v; records=%#v", path, headers, s.Records(path))
}

func (s *configBehaviorHTTPServer) RequireBasicAuth(t *testing.T, path, user string) {
	t.Helper()
	for _, record := range s.Records(path) {
		if record.HasBasicAuth && record.BasicAuthUser == user {
			return
		}
	}
	t.Fatalf("server saw no %s request with basic auth user %q; records=%#v", path, user, s.Records(path))
}

func writeConfigBehaviorConf(t *testing.T, dir, out string, extra []string) string {
	t.Helper()
	confPath := filepath.Join(t.TempDir(), "aria2.conf")
	lines := []string{
		"dir=" + dir,
		"out=" + out,
		"allow-overwrite=true",
		"file-allocation=none",
		"quiet=true",
		"show-console-readout=false",
		"summary-interval=0",
		"enable-dht=false",
		"enable-dht6=false",
	}
	lines = append(lines, extra...)
	lines = append(lines, "")
	if err := os.WriteFile(confPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return confPath
}

func configBehaviorDownloadArgs(dir, out string) []string {
	return append(baseDownloadArgs(dir, out),
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--max-connection-per-server=1",
		"--split=1",
	)
}

func runConfigBehaviorProcess(t *testing.T, ref bool, args []string, stdin string, env []string) RunResult {
	t.Helper()

	opts := RunOptions{Timeout: 25 * time.Second, Env: env}
	var result RunResult
	var err error
	if ref {
		result, err = RunRefWithOptions(t, args, stdin, opts)
	} else {
		result, err = RunImplWithOptions(t, args, stdin, opts)
	}
	if err != nil {
		t.Fatalf("run config behavior ref=%v: %v\nargs=%v\nstdout=%s\nstderr=%s", ref, err, args, result.Stdout, result.Stderr)
	}
	return result
}

func cleanProxyEnv() []string {
	return []string{
		"http_proxy=",
		"https_proxy=",
		"ftp_proxy=",
		"all_proxy=",
		"no_proxy=",
		"HTTP_PROXY=",
		"HTTPS_PROXY=",
		"FTP_PROXY=",
		"ALL_PROXY=",
		"NO_PROXY=",
	}
}

type connectProxy struct {
	ln      net.Listener
	records chan string
	count   atomic.Int64
}

func newConnectProxy(t *testing.T) *connectProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen proxy: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &connectProxy{ln: ln, records: make(chan string, 32)}
	go p.serve(ctx)
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
	})
	return p
}

func (p *connectProxy) URL() string {
	return "http://" + p.ln.Addr().String()
}

func (p *connectProxy) Count() int64 {
	return p.count.Load()
}

func (p *connectProxy) serve(ctx context.Context) {
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
		go p.handle(ctx, conn)
	}
}

func (p *connectProxy) handle(parent context.Context, client net.Conn) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer client.Close()
	br := bufio.NewReader(client)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return
	}
	method, target := fields[0], fields[1]
	for {
		h, err := br.ReadString('\n')
		if err != nil || h == "\r\n" || h == "\n" {
			break
		}
	}
	if method != http.MethodConnect {
		_, _ = io.WriteString(client, "HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")
		return
	}
	p.count.Add(1)
	select {
	case p.records <- target:
	default:
	}
	upstream, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer upstream.Close()
	_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
	done := make(chan struct{}, 2)
	go func(ctx context.Context) {
		<-ctx.Done()
		_ = client.Close()
		_ = upstream.Close()
	}(ctx)
	go func(ctx context.Context) {
		select {
		case <-ctx.Done():
			done <- struct{}{}
			return
		default:
		}
		_, _ = io.Copy(upstream, br)
		_ = upstream.Close()
		cancel()
		done <- struct{}{}
	}(ctx)
	go func(ctx context.Context) {
		select {
		case <-ctx.Done():
			done <- struct{}{}
			return
		default:
		}
		_, _ = io.Copy(client, upstream)
		cancel()
		done <- struct{}{}
	}(ctx)
	<-done
}

func probeProxyBehavior(t *testing.T, ref bool, useEnvProxy bool, useNoProxy bool, wantProxy bool) {
	t.Helper()

	payload := []byte("proxy behavior payload\n")
	srv := newConfigBehaviorHTTPServer(t, map[string][]byte{"/proxy.bin": payload}, nil)
	proxy := newConnectProxy(t)
	dir := t.TempDir()

	args := append(configBehaviorDownloadArgs(dir, "proxy.bin"), "--proxy-method=tunnel")
	env := cleanProxyEnv()
	if useEnvProxy {
		env = append(env, "all_proxy="+proxy.URL())
	} else {
		args = append(args, "--all-proxy="+proxy.URL())
	}
	if useNoProxy {
		args = append(args, "--no-proxy="+srv.Hostname())
	}
	args = append(args, srv.URL("/proxy.bin"))

	result := runConfigBehaviorProcess(t, ref, args, "", env)
	requireExitSuccess(t, fmt.Sprintf("proxy ref=%v", ref), result)
	requireDownloadedBytes(t, filepath.Join(dir, "proxy.bin"), payload)
	gotProxy := proxy.Count() > 0
	if gotProxy != wantProxy {
		t.Fatalf("proxy use ref=%v got %v want %v (count=%d)", ref, gotProxy, wantProxy, proxy.Count())
	}
}

func writeNetrcForHost(t *testing.T, host, user, pass string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".netrc")
	data := fmt.Sprintf("machine %s login %s password %s\n", host, user, pass)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write netrc: %v", err)
	}
	return path
}

func writeConfigBehaviorInputFile(t *testing.T, dir string, srv *configBehaviorHTTPServer) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.txt")
	data := strings.Join([]string{
		srv.URL("/entry-one.bin"),
		"  dir=" + dir,
		"  out=entry-one.out",
		"  allow-overwrite=true",
		"  file-allocation=none",
		"  user-agent=input-agent-one",
		"  referer=http://127.0.0.1/input-one",
		"  header=X-Input-Entry: one",
		srv.URL("/entry-two.bin"),
		"  dir=" + dir,
		"  out=entry-two.out",
		"  allow-overwrite=true",
		"  file-allocation=none",
		"  user-agent=input-agent-two",
		"  referer=http://127.0.0.1/input-two",
		"  header=X-Input-Entry: two",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write input file: %v", err)
	}
	return path
}

func requireInputEntryRequest(t *testing.T, srv *configBehaviorHTTPServer, path, suffix string) {
	t.Helper()
	srv.RequireRequest(t, path, map[string]string{
		"User-Agent":    "input-agent-" + suffix,
		"Referer":       "http://127.0.0.1/input-" + suffix,
		"X-Input-Entry": suffix,
	})
}

func savePausedSessionViaRPC(t *testing.T, ref bool, sessionPath, dir, uri, out string) {
	t.Helper()
	port := findFreePort(t)
	var srv *rpcServer
	args := []string{
		"--no-conf",
		"--dir=" + dir,
		"--save-session=" + sessionPath,
		"--pause=true",
		"--file-allocation=none",
		"--enable-dht=false",
		"--enable-dht6=false",
	}
	if ref {
		srv = startRPCRef(t, port, args...)
	} else {
		srv = startRPCImpl(t, port, args...)
	}
	srv.WaitReady(t)
	rr := rpcCallOK(t, port, "aria2.addUri", []any{
		[]string{uri},
		map[string]string{
			"dir":             dir,
			"out":             out,
			"allow-overwrite": "true",
			"file-allocation": "none",
			"pause":           "true",
		},
	})
	gid := rpcResultString(t, rr)
	waitForConfigBehaviorRPCStatus(t, port, gid, "paused")
	save := rpcCall(t, port, "aria2.saveSession", []any{})
	if save.Error != nil {
		t.Fatalf("saveSession ref=%v: code=%d msg=%s", ref, save.Error.Code, save.Error.Message)
	}
	srv.Stop(t)
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read saved session ref=%v: %v", ref, err)
	}
	if len(data) == 0 {
		t.Fatalf("saved session ref=%v is empty", ref)
	}
}

func reloadPausedSessionAndComplete(t *testing.T, ref bool, sessionPath, dir, out string, payload []byte) {
	t.Helper()
	port := findFreePort(t)
	var srv *rpcServer
	args := []string{
		"--no-conf",
		"--input-file=" + sessionPath,
		"--file-allocation=none",
		"--enable-dht=false",
		"--enable-dht6=false",
	}
	if ref {
		srv = startRPCRef(t, port, args...)
	} else {
		srv = startRPCImpl(t, port, args...)
	}
	defer srv.Stop(t)
	srv.WaitReady(t)

	gid := waitForOneWaitingGID(t, port)
	rpcCallOK(t, port, "aria2.unpause", []any{gid})
	waitForConfigBehaviorRPCStatus(t, port, gid, "complete")
	requireDownloadedBytes(t, filepath.Join(dir, out), payload)
}

func waitForOneWaitingGID(t *testing.T, port int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rr := rpcCallOK(t, port, "aria2.tellWaiting", []any{float64(0), float64(10), []string{"gid"}})
		var values []map[string]string
		if err := json.Unmarshal(rr.Result, &values); err != nil {
			t.Fatalf("unmarshal tellWaiting: %v", err)
		}
		if len(values) == 1 && values[0]["gid"] != "" {
			return values[0]["gid"]
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for one queued session entry")
	return ""
}

func waitForConfigBehaviorRPCStatus(t *testing.T, port int, gid string, want string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		rr := rpcCall(t, port, "aria2.tellStatus", []any{gid, []string{"status"}})
		if rr.Error == nil {
			values := mustStringMap(t, "config behavior tellStatus", rr.Result)
			if values["status"] == want {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("GID %s on port %d did not reach status %q", gid, port, want)
}

func writeHookScript(t *testing.T, dir, event, logPath string) string {
	t.Helper()
	path := filepath.Join(dir, "hook-"+event+".sh")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s|%%s|%%s|%%s\\n' '%s' \"$1\" \"$2\" \"$3\" >> %s\n", event, shellQuote(logPath))
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write hook script: %v", err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type hookEvent struct {
	name     string
	gid      string
	numFiles string
	path     string
}

func waitHookEvents(t *testing.T, logPath string, names ...string) map[string]hookEvent {
	t.Helper()
	want := make(map[string]struct{}, len(names))
	for _, name := range names {
		want[name] = struct{}{}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events := readHookEvents(t, logPath)
		for name := range want {
			if _, ok := events[name]; !ok {
				goto wait
			}
		}
		return events
	wait:
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for hook events %v; got %#v", names, readHookEvents(t, logPath))
	return nil
}

func readHookEvents(t *testing.T, logPath string) map[string]hookEvent {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]hookEvent{}
		}
		t.Fatalf("read hook log: %v", err)
	}
	events := make(map[string]hookEvent)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			t.Fatalf("malformed hook log line %q", line)
		}
		events[parts[0]] = hookEvent{name: parts[0], gid: parts[1], numFiles: parts[2], path: parts[3]}
	}
	return events
}

func requireHookEvent(t *testing.T, ev hookEvent, wantPath string) {
	t.Helper()
	if ev.gid == "" {
		t.Fatalf("%s hook gid is empty", ev.name)
	}
	if ev.numFiles != "1" {
		t.Fatalf("%s hook numFiles got %q want 1", ev.name, ev.numFiles)
	}
	if ev.path != wantPath {
		t.Fatalf("%s hook path got %q want %q", ev.name, ev.path, wantPath)
	}
}
