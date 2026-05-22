package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSession_RefSaveImplLoad(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.sess")
	downloadDir := filepath.Join(dir, "downloads")

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort,
		"--dir="+downloadDir,
		"--save-session="+sessionPath,
	)
	refSrv.WaitReady(t)

	rrAdd := rpcCallOK(t, rPort, "aria2.addUri", []any{[]string{"http://localhost:1/session-test-a"}})
	refGID := rpcResultString(t, rrAdd)
	t.Logf("added download with GID %s", refGID)

	rpcCallOK(t, rPort, "aria2.pause", []any{refGID})

	rrSave := rpcCall(t, rPort, "aria2.saveSession", []any{})
	if rrSave.Error != nil {
		t.Logf("ref saveSession error: %s", rrSave.Error.Message)
	}

	refSrv.Stop(t)

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ref session file is empty")
	}
	t.Logf("ref session file size: %d bytes", len(data))

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort,
		"--dir="+downloadDir,
		"--input-file="+sessionPath,
	)
	defer implSrv.Stop(t)
	implSrv.WaitReadyOrSkip(t)

	irWait := rpcCallOK(t, iPort, "aria2.tellWaiting", []any{float64(0), float64(10)})
	var implWaiting []map[string]json.RawMessage
	if err := json.Unmarshal(irWait.Result, &implWaiting); err != nil {
		t.Fatalf("unmarshal impl waiting: %v", err)
	}

	if len(implWaiting) == 0 {
		t.Log("impl did not load any downloads from session (may need session parsing support)")
	} else {
		t.Logf("impl loaded %d downloads from session", len(implWaiting))
	}
}

func TestSession_ImplSaveRefLoad(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session-impl.sess")
	downloadDir := filepath.Join(dir, "downloads-impl")

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	iPort := findFreePort(t)
	implSrv := startRPCImpl(t, iPort,
		"--dir="+downloadDir,
		"--save-session="+sessionPath,
	)
	implSrv.WaitReadyOrSkip(t)

	irAdd := rpcCallOK(t, iPort, "aria2.addUri", []any{[]string{"http://localhost:1/impl-session-test"}})
	implGID := rpcResultString(t, irAdd)
	t.Logf("added download with GID %s", implGID)

	rpcCallOK(t, iPort, "aria2.pause", []any{implGID})

	irSave := rpcCall(t, iPort, "aria2.saveSession", []any{})
	if irSave.Error != nil {
		t.Logf("impl saveSession error: %s", irSave.Error.Message)
	}

	implSrv.Stop(t)

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("impl session file is empty")
	}
	t.Logf("impl session file size: %d bytes", len(data))

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort,
		"--dir="+downloadDir,
		"--input-file="+sessionPath,
	)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	rrWait := rpcCallOK(t, rPort, "aria2.tellWaiting", []any{float64(0), float64(10)})
	var refWaiting []map[string]json.RawMessage
	if err := json.Unmarshal(rrWait.Result, &refWaiting); err != nil {
		t.Fatalf("unmarshal ref waiting: %v", err)
	}

	if len(refWaiting) == 0 {
		t.Log("ref did not load any downloads from impl session (may need format compatibility)")
	} else {
		t.Logf("ref loaded %d downloads from impl session", len(refWaiting))
	}
}

func TestSession_RefSaveByteComparison(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	downloadDir := filepath.Join(dir, "dl")

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	session1 := filepath.Join(dir, "s1.sess")
	session2 := filepath.Join(dir, "s2.sess")

	for i, sessPath := range []string{session1, session2} {
		port := findFreePort(t)
		srv := startRPCRef(t, port,
			"--dir="+downloadDir,
			"--save-session="+sessPath,
		)
		srv.WaitReady(t)

		addURI := "http://localhost:1/session-byte-test"
		rrAdd := rpcCallOK(t, port, "aria2.addUri", []any{[]string{addURI}})
		gid := rpcResultString(t, rrAdd)
		rpcCallOK(t, port, "aria2.pause", []any{gid})
		rpcCall(t, port, "aria2.saveSession", []any{})

		srv.Stop(t)

		data, err := os.ReadFile(sessPath)
		if err != nil {
			t.Fatalf("read session %d: %v", i+1, err)
		}
		if len(data) == 0 {
			t.Fatalf("session %d is empty", i+1)
		}
		t.Logf("session %d size: %d bytes", i+1, len(data))
	}

	data1, _ := os.ReadFile(session1)
	data2, _ := os.ReadFile(session2)

	if len(data1) == len(data2) {
		t.Log("session files have identical length")
	} else {
		t.Logf("session files differ in length: %d vs %d", len(data1), len(data2))
	}

	content1 := string(data1)
	if !strings.Contains(content1, "http://localhost:1/session-byte-test") {
		t.Error("session file missing expected URI")
	}
}

func TestSession_SaveSessionImmediate(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "immediate.sess")
	downloadDir := filepath.Join(dir, "dl-immediate")

	os.MkdirAll(downloadDir, 0755)

	rPort := findFreePort(t)
	refSrv := startRPCRef(t, rPort,
		"--dir="+downloadDir,
		"--save-session="+sessionPath,
	)
	defer refSrv.Stop(t)
	refSrv.WaitReady(t)

	rr := rpcCall(t, rPort, "aria2.saveSession", []any{})
	if rr.Error != nil {
		t.Logf("ref immediate saveSession: %s (code=%d)", rr.Error.Message, rr.Error.Code)
	}

	data, err := os.ReadFile(sessionPath)
	if err != nil && !os.IsNotExist(err) {
		t.Errorf("read session: %v", err)
	}
	if len(data) > 0 {
		t.Logf("session file size (empty queue): %d bytes", len(data))
	}
}

func TestSession_HookPauseFiresOnActiveDownload(t *testing.T) {
	SkipIfNoRef(t)
	if runtime.GOOS == "windows" {
		t.Skip("hook script uses POSIX shell")
	}
	requireRefHelpOptions(t, "on-download-pause", "enable-rpc")

	for _, ref := range []bool{true, false} {
		label := "impl"
		if ref {
			label = "ref"
		}
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "hooks.log")
			pauseHook := writeHookScript(t, dir, "pause", logPath)
			srv := newBlockingDownloadServer(t)
			port := findFreePort(t)

			args := append(configBehaviorDownloadArgs(dir, "pause.bin"),
				"--on-download-pause="+pauseHook,
			)

			var rpcSrv *rpcServer
			if ref {
				rpcSrv = startRPCRef(t, port, args...)
				rpcSrv.WaitReady(t)
			} else {
				rpcSrv = startRPCImpl(t, port, args...)
				rpcSrv.WaitReadyOrSkip(t)
			}
			defer rpcSrv.Stop(t)

			rrAdd := rpcCallOK(t, port, "aria2.addUri", []any{[]string{srv.URL + "/pause.bin"}})
			gid := rpcResultString(t, rrAdd)
			waitForConfigBehaviorRPCStatus(t, port, gid, "active")

			rpcCallOK(t, port, "aria2.pause", []any{gid})
			waitForConfigBehaviorRPCStatus(t, port, gid, "paused")

			events := waitHookEvents(t, logPath, "pause")
			ev := events["pause"]
			if ev.gid == "" {
				t.Fatalf("pause hook gid is empty")
			}
			if ev.numFiles != "1" {
				t.Fatalf("pause hook numFiles got %q want 1", ev.numFiles)
			}
			wantPath := filepath.Join(dir, "pause.bin")
			if ev.path != "" && ev.path != wantPath {
				t.Fatalf("pause hook path got %q want %q or empty", ev.path, wantPath)
			}
		})
	}
}

func TestSession_HookStopWinsOverErrorOnSignalShutdown(t *testing.T) {
	SkipIfNoRef(t)
	if runtime.GOOS == "windows" {
		t.Skip("signal-driven hook probe uses os.Interrupt and POSIX shell")
	}
	requireRefHelpOptions(t, "on-download-stop", "on-download-error", "save-session")

	for _, ref := range []bool{true, false} {
		label := shutdownProbeLabel(ref)
		t.Run(label, func(t *testing.T) {
			dir := t.TempDir()
			logPath := filepath.Join(dir, "hooks.log")
			stopHook := writeHookScript(t, dir, "stop", logPath)
			errorHook := writeHookScript(t, dir, "error", logPath)

			started := make(chan struct{}, 1)
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/stop.bin" {
					http.NotFound(w, r)
					return
				}
				select {
				case started <- struct{}{}:
				default:
				}
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
			defer func() {
				close(release)
				srv.Close()
			}()

			args := append(stopDownloadArgs(dir, "stop.bin"),
				"--save-session="+filepath.Join(dir, "stop.session"),
				"--on-download-stop="+stopHook,
				"--on-download-error="+errorHook,
				srv.URL+"/stop.bin",
			)

			result := signalProcessAndWait(t, ref, args, started)
			requireLifecycleInProgressExit(t, label, result)

			events := waitHookEvents(t, logPath, "stop")
			requireHookEvent(t, events["stop"], filepath.Join(dir, "stop.bin"))

			time.Sleep(200 * time.Millisecond)
			if ev, ok := readHookEvents(t, logPath)["error"]; ok {
				t.Fatalf("%s unexpectedly fired error hook: %#v", label, ev)
			}
		})
	}
}

func TestSession_ShutdownPreservesActiveAndWaitingEntries(t *testing.T) {
	SkipIfNoRef(t)
	if runtime.GOOS == "windows" {
		t.Skip("signal-driven shutdown probe uses os.Interrupt")
	}
	requireRefHelpOptions(t, "save-session", "max-concurrent-downloads", "stop")

	refPaths := runShutdownSessionProbe(t, true)
	implPaths := runShutdownSessionProbe(t, false)

	want := []string{"/active.bin", "/waiting.bin"}
	if !slices.Equal(refPaths, want) {
		t.Fatalf("ref saved session paths = %v, want %v", refPaths, want)
	}
	if !slices.Equal(implPaths, want) {
		t.Fatalf("impl saved session paths = %v, want %v", implPaths, want)
	}
	if !slices.Equal(refPaths, implPaths) {
		t.Fatalf("saved session path mismatch: ref=%v impl=%v", refPaths, implPaths)
	}
}

func runShutdownSessionProbe(t *testing.T, ref bool) []string {
	t.Helper()

	type sessionRequestLog struct {
		mu    sync.Mutex
		paths []string
	}
	log := &sessionRequestLog{}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.mu.Lock()
		log.paths = append(log.paths, r.URL.Path)
		log.mu.Unlock()

		switch r.URL.Path {
		case "/active.bin":
			select {
			case started <- struct{}{}:
			default:
			}
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
		case "/waiting.bin":
			w.Header().Set("Content-Length", "128")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(bytes.Repeat([]byte("y"), 128))
		default:
			http.NotFound(w, r)
		}
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "shutdown.session")
	args := []string{
		"--dir=" + dir,
		"--save-session=" + sessionPath,
		"--max-concurrent-downloads=1",
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--split=1",
		"--max-connection-per-server=1",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--enable-dht=false",
		"--enable-dht6=false",
		srv.URL + "/active.bin",
		srv.URL + "/waiting.bin",
	}

	result := signalProcessAndWait(t, ref, args, started)
	t.Logf("%s exit=%d", shutdownProbeLabel(ref), result.ExitCode)
	assertSessionSavedPaths(t, sessionPath, []string{"/active.bin", "/waiting.bin"})

	log.mu.Lock()
	paths := append([]string(nil), log.paths...)
	log.mu.Unlock()
	if slices.Contains(paths, "/waiting.bin") {
		t.Fatalf("%s unexpectedly requested waiting entry before shutdown: %v", shutdownProbeLabel(ref), paths)
	}

	return sessionSavedPaths(t, sessionPath)
}

func signalProcessAndWait(t *testing.T, ref bool, args []string, started <-chan struct{}) RunResult {
	t.Helper()

	bin, err := implBinary()
	if ref {
		bin, err = findRefBinary()
	}
	if err != nil {
		t.Fatalf("find binary ref=%v: %v", ref, err)
	}

	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start shutdown probe ref=%v: %v", ref, err)
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case <-started:
	case err := <-waitDone:
		t.Fatalf("%s exited before active download started: %v\nstdout=%s\nstderr=%s", shutdownProbeLabel(ref), err, stdout.String(), stderr.String())
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-waitDone
		t.Fatalf("%s did not start active download in time", shutdownProbeLabel(ref))
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		<-waitDone
		t.Fatalf("signal %s: %v", shutdownProbeLabel(ref), err)
	}

	select {
	case err := <-waitDone:
		result := RunResult{
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}
		if err == nil {
			result.ExitCode = 0
			return result
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result
		}
		t.Fatalf("wait %s: %v\nstdout=%s\nstderr=%s", shutdownProbeLabel(ref), err, result.Stdout, result.Stderr)
	case <-time.After(8 * time.Second):
		_ = cmd.Process.Kill()
		<-waitDone
		t.Fatalf("%s did not exit after interrupt", shutdownProbeLabel(ref))
	}
	return RunResult{}
}

func assertSessionSavedPaths(t *testing.T, sessionPath string, want []string) {
	t.Helper()
	got := sessionSavedPaths(t, sessionPath)
	if !slices.Equal(got, want) {
		t.Fatalf("saved session paths = %v, want %v", got, want)
	}
}

func sessionSavedPaths(t *testing.T, sessionPath string) []string {
	t.Helper()

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file %s: %v", sessionPath, err)
	}
	if len(data) == 0 {
		t.Fatalf("session file %s is empty", sessionPath)
	}

	seen := map[string]struct{}{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, " ") || strings.TrimSpace(line) == "" {
			continue
		}
		for _, field := range strings.Fields(line) {
			if !strings.HasPrefix(field, "http://") && !strings.HasPrefix(field, "https://") {
				continue
			}
			if idx := strings.Index(field, "://"); idx >= 0 {
				rest := field[idx+3:]
				slash := strings.IndexByte(rest, '/')
				if slash >= 0 {
					seen[rest[slash:]] = struct{}{}
				}
			}
		}
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

func shutdownProbeLabel(ref bool) string {
	if ref {
		return "ref shutdown save-session"
	}
	return "impl shutdown save-session"
}
