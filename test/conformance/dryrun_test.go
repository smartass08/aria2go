package conformance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDryRun_HTTPProbeSuccessNoFile verifies that --dry-run probes the server
// and reports success without writing the payload file, on both binaries.
// aria2's dry-run still performs a network probe (HTTP HEAD) but writes nothing.
func TestDryRun_HTTPProbeSuccessNoFile(t *testing.T) {
	SkipIfNoRef(t)

	var headSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			headSeen = true
		}
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(make([]byte, 4096))
		}
	}))
	defer srv.Close()

	run := func(ref bool, dir string) RunResult {
		t.Helper()
		args := []string{
			"--dry-run=true",
			"--dir=" + dir,
			"--out=probe.bin",
			"--quiet=true",
			"--file-allocation=none",
			srv.URL + "/probe.bin",
		}
		var res RunResult
		var err error
		if ref {
			res, err = RunRefWithOptions(t, args, "", RunOptions{Timeout: 20 * time.Second})
		} else {
			res, err = RunImplWithOptions(t, args, "", RunOptions{Timeout: 20 * time.Second})
		}
		if err != nil {
			t.Fatalf("run dry-run ref=%v: %v\nstderr=%s", ref, err, res.Stderr)
		}
		return res
	}

	refDir, implDir := t.TempDir(), t.TempDir()
	ref := run(true, refDir)
	impl := run(false, implDir)

	AssertEqualExit(t, ref, impl)
	if ref.ExitCode != 0 {
		t.Fatalf("expected dry-run success, ref exit=%d stderr=%s", ref.ExitCode, ref.Stderr)
	}
	if !headSeen {
		t.Errorf("expected at least one HEAD probe request during dry-run")
	}
	for _, dir := range []string{refDir, implDir} {
		if _, err := os.Stat(filepath.Join(dir, "probe.bin")); !os.IsNotExist(err) {
			t.Errorf("dry-run wrote a payload file in %s (err=%v), want no file", dir, err)
		}
	}
}

// TestDryRun_HTTPDeadURLFailsParity verifies that --dry-run against an
// unreachable server fails on BOTH binaries with the same exit code. This is
// the behavior that distinguishes a real probe from a no-op: aria2 contacts the
// server during dry-run, so a dead endpoint must fail rather than succeed.
func TestDryRun_HTTPDeadURLFailsParity(t *testing.T) {
	SkipIfNoRef(t)

	// A free port with nothing listening -> connection refused.
	deadPort := findFreePort(t)
	deadURL := fmt.Sprintf("http://127.0.0.1:%d/missing.bin", deadPort)

	run := func(ref bool) RunResult {
		t.Helper()
		args := []string{
			"--dry-run=true",
			"--dir=" + t.TempDir(),
			"--quiet=true",
			"--file-allocation=none",
			"--connect-timeout=2",
			"--max-tries=1",
			"--retry-wait=0",
			deadURL,
		}
		var res RunResult
		var err error
		if ref {
			res, err = RunRefWithOptions(t, args, "", RunOptions{Timeout: 20 * time.Second})
		} else {
			res, err = RunImplWithOptions(t, args, "", RunOptions{Timeout: 20 * time.Second})
		}
		if err != nil {
			t.Fatalf("run dry-run ref=%v: %v\nstderr=%s", ref, err, res.Stderr)
		}
		return res
	}

	ref := run(true)
	impl := run(false)

	if ref.ExitCode == 0 {
		t.Fatalf("expected aria2c dry-run on dead URL to fail, got exit 0")
	}
	if impl.ExitCode == 0 {
		t.Fatalf("aria2go dry-run on dead URL returned success; it must probe the server like aria2c")
	}
	AssertEqualExit(t, ref, impl)
}
