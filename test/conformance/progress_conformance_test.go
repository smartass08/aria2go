package conformance

// progress_conformance_test.go
//
// Conformance tests that PROVE parity between aria2c 1.37.0 (ref) and aria2go
// for the following feature_matrix rows:
//   - progress.summary-interval (summary banner presence/absence)
//   - progress.console-readout  (download-result=hide/full formatting)
//
// These tests MUST NOT edit any non-test production code.

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newProgressBlockingServer returns an httptest.Server that accepts a connection,
// sends back Content-Length: 1MiB + 1KiB of body, then blocks until the test
// cleanup releases it. This simulates an in-progress download that never
// completes within the test window.
func newProgressBlockingServer(t *testing.T) *httptest.Server {
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

// newProgressQuickServer returns an httptest.Server that immediately delivers a
// small payload and closes the connection. It supports byte-range requests so
// aria2's range-split logic is handled correctly.
func newProgressQuickServer(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		start, end, partial, ok := parseRangeHeader(r.Header.Get("Range"), int64(len(payload)))
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := payload[start : end+1]
		if partial {
			w.Header().Set("Content-Range", http.StatusText(0))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
		}
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(func() { srv.Close() })
	return srv
}

// progressTestBaseArgs returns the base CLI args for progress/console readout
// tests, all noise-suppressing defaults turned off so the output behaviour
// under test is visible.
func progressTestBaseArgs(dir, out string) []string {
	return []string{
		"--dir=" + dir,
		"--out=" + out,
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--max-connection-per-server=1",
		"--split=1",
		"--enable-dht=false",
		"--enable-dht6=false",
	}
}

// ============================================================================
// Test 1 – progress.summary-interval
// ============================================================================

// TestProgressSummary_BannerAppearsWithInterval1 verifies that both aria2c and
// aria2go emit the periodic "*** Download Progress Summary as of ..." banner
// when --summary-interval=1 is set and a download is in progress for at least
// 3 seconds (via --stop=3).
//
// Empirically confirmed aria2c 1.37.0 output:
//
//	" *** Download Progress Summary as of Sat May 23 02:39:30 2026 *** "
func TestProgressSummary_BannerAppearsWithInterval1(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "summary-interval", "show-console-readout", "stop")

	srv := newProgressBlockingServer(t)

	for _, tc := range []struct {
		label string
		ref   bool
	}{
		{"ref", true},
		{"impl", false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			args := append(progressTestBaseArgs(dir, "prog.bin"),
				// Progress output on
				"--quiet=false",
				"--console-log-level=notice",
				"--show-console-readout=true",
				"--summary-interval=1",
				// Kill after 3 s so summary fires at least twice
				"--stop=3",
				srv.URL+"/prog.bin",
			)
			result, err := RunRefWithOptions(t, args, "", RunOptions{Timeout: 12 * time.Second})
			if tc.label == "impl" {
				result, err = RunImplWithOptions(t, args, "", RunOptions{Timeout: 12 * time.Second})
			}
			if err != nil {
				t.Fatalf("%s run error: %v\nstdout=%s\nstderr=%s", tc.label, err, result.Stdout, result.Stderr)
			}
			// --stop exits with code 7 (download in-progress)
			if result.ExitCode != 7 {
				t.Fatalf("%s expected exit=7, got %d\nstdout=%s\nstderr=%s",
					tc.label, result.ExitCode, result.Stdout, result.Stderr)
			}
			combined := result.Stdout + result.Stderr
			if !strings.Contains(combined, "*** Download Progress Summary as of") {
				t.Errorf("%s: output missing summary banner\nstdout=%s\nstderr=%s",
					tc.label, result.Stdout, result.Stderr)
			}
		})
	}
}

// TestProgressSummary_NoBannerWhenInterval0ReadoutOff verifies that when
// --summary-interval=0 and --show-console-readout=false are both set,
// neither aria2c nor aria2go emit:
//   - the "*** Download Progress Summary as of ..." banner
//   - progress readout lines of the form "[#..."
func TestProgressSummary_NoBannerWhenInterval0ReadoutOff(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "summary-interval", "show-console-readout", "stop")

	srv := newProgressBlockingServer(t)

	for _, tc := range []struct {
		label string
		ref   bool
	}{
		{"ref", true},
		{"impl", false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			args := append(progressTestBaseArgs(dir, "noprog.bin"),
				"--quiet=false",
				"--console-log-level=notice",
				"--show-console-readout=false",
				"--summary-interval=0",
				"--stop=2",
				srv.URL+"/noprog.bin",
			)
			result, err := RunRefWithOptions(t, args, "", RunOptions{Timeout: 12 * time.Second})
			if tc.label == "impl" {
				result, err = RunImplWithOptions(t, args, "", RunOptions{Timeout: 12 * time.Second})
			}
			if err != nil {
				t.Fatalf("%s run error: %v\nstdout=%s\nstderr=%s", tc.label, err, result.Stdout, result.Stderr)
			}
			combined := result.Stdout + result.Stderr
			if strings.Contains(combined, "*** Download Progress Summary as of") {
				t.Errorf("%s: output contains summary banner despite summary-interval=0:\n%s", tc.label, combined)
			}
			// Progress readout lines start with " [#" (after ANSI clear).
			// Without ANSI, aria2 emits lines that start "[#..." or " [#..."
			for _, line := range strings.Split(combined, "\n") {
				stripped := strings.TrimSpace(line)
				if strings.HasPrefix(stripped, "[#") {
					t.Errorf("%s: output contains readout line with readout=false: %q", tc.label, line)
				}
			}
		})
	}
}

// TestProgressSummary_BothBinariesMatchBannerPresence asserts that ref and impl
// agree on WHETHER the summary banner appears, for both the "on" and "off" cases.
func TestProgressSummary_BothBinariesMatchBannerPresence(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "summary-interval", "show-console-readout", "stop")

	srv := newProgressBlockingServer(t)

	for _, tc := range []struct {
		name            string
		extraArgs       []string
		wantBanner      bool
		wantExitCode    int
	}{
		{
			name: "interval1_readout_on",
			extraArgs: []string{
				"--show-console-readout=true",
				"--summary-interval=1",
				"--quiet=false",
			},
			wantBanner:   true,
			wantExitCode: 7,
		},
		{
			name: "interval0_readout_off",
			extraArgs: []string{
				"--show-console-readout=false",
				"--summary-interval=0",
				"--quiet=false",
			},
			wantBanner:   false,
			wantExitCode: 7,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refDir, implDir := t.TempDir(), t.TempDir()
			common := append([]string{
				"--console-log-level=notice",
				"--stop=3",
			}, tc.extraArgs...)

			refArgs := append(progressTestBaseArgs(refDir, "match.bin"), append(common, srv.URL+"/match.bin")...)
			implArgs := append(progressTestBaseArgs(implDir, "match.bin"), append(common, srv.URL+"/match.bin")...)

			ref, refErr := RunRefWithOptions(t, refArgs, "", RunOptions{Timeout: 15 * time.Second})
			impl, implErr := RunImplWithOptions(t, implArgs, "", RunOptions{Timeout: 15 * time.Second})
			if refErr != nil {
				t.Fatalf("ref run error: %v\nstdout=%s\nstderr=%s", refErr, ref.Stdout, ref.Stderr)
			}
			if implErr != nil {
				t.Fatalf("impl run error: %v\nstdout=%s\nstderr=%s", implErr, impl.Stdout, impl.Stderr)
			}

			AssertEqualExit(t, ref, impl)
			if ref.ExitCode != tc.wantExitCode {
				t.Fatalf("ref exit=%d want %d\nstdout=%s\nstderr=%s",
					ref.ExitCode, tc.wantExitCode, ref.Stdout, ref.Stderr)
			}

			refHas := strings.Contains(ref.Stdout+ref.Stderr, "*** Download Progress Summary as of")
			implHas := strings.Contains(impl.Stdout+impl.Stderr, "*** Download Progress Summary as of")
			if refHas != tc.wantBanner {
				t.Errorf("ref banner presence=%v want=%v\nstdout=%s\nstderr=%s",
					refHas, tc.wantBanner, ref.Stdout, ref.Stderr)
			}
			if implHas != tc.wantBanner {
				t.Errorf("impl banner presence=%v want=%v\nstdout=%s\nstderr=%s",
					implHas, tc.wantBanner, impl.Stdout, impl.Stderr)
			}
			if refHas != implHas {
				t.Errorf("ref/impl banner presence mismatch: ref=%v impl=%v", refHas, implHas)
			}
		})
	}
}

// ============================================================================
// Test 2 – progress.console-readout (download-result formatting)
// ============================================================================

// TestConsoleReadout_DownloadResultHide verifies that --download-result=hide
// suppresses the "Download Results:" section from both aria2c and aria2go output.
//
// Empirically confirmed: with hide the output contains nothing (or only log
// lines if --console-log-level is not error), while the Download Results block
// is absent.
func TestConsoleReadout_DownloadResultHide(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "download-result")

	payload := bytes.Repeat([]byte("download-result-hide-payload\n"), 128)
	srv := newProgressQuickServer(t, payload)

	refDir, implDir := t.TempDir(), t.TempDir()

	commonArgs := func(dir string) []string {
		return append(progressTestBaseArgs(dir, "hide.bin"),
			"--quiet=false",
			"--console-log-level=error",
			"--show-console-readout=false",
			"--summary-interval=0",
			"--download-result=hide",
			srv.URL+"/hide.bin",
		)
	}
	ref, refErr := RunRefWithOptions(t, commonArgs(refDir), "", RunOptions{Timeout: 10 * time.Second})
	impl, implErr := RunImplWithOptions(t, commonArgs(implDir), "", RunOptions{Timeout: 10 * time.Second})
	if refErr != nil {
		t.Fatalf("ref run error: %v\nstdout=%s\nstderr=%s", refErr, ref.Stdout, ref.Stderr)
	}
	if implErr != nil {
		t.Fatalf("impl run error: %v\nstdout=%s\nstderr=%s", implErr, impl.Stdout, impl.Stderr)
	}

	AssertEqualExit(t, ref, impl)
	if ref.ExitCode != 0 {
		t.Fatalf("ref exit=%d, want 0\nstdout=%s\nstderr=%s", ref.ExitCode, ref.Stdout, ref.Stderr)
	}

	for label, result := range map[string]RunResult{"ref": ref, "impl": impl} {
		combined := result.Stdout + result.Stderr
		if strings.Contains(combined, "Download Results:") {
			t.Errorf("%s: output contains 'Download Results:' despite --download-result=hide:\n%s",
				label, combined)
		}
	}
}

// TestConsoleReadout_DownloadResultFull verifies that --download-result=full
// emits the "Download Results:" table including the percentage column ("  %|")
// in both aria2c and aria2go output.
//
// Empirically confirmed header format:
//   gid   |stat|avg speed  |  %|path/URI
func TestConsoleReadout_DownloadResultFull(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "download-result")

	payload := bytes.Repeat([]byte("download-result-full-payload\n"), 128)
	srv := newProgressQuickServer(t, payload)

	refDir, implDir := t.TempDir(), t.TempDir()

	commonArgs := func(dir string) []string {
		return append(progressTestBaseArgs(dir, "full.bin"),
			"--quiet=false",
			"--console-log-level=error",
			"--show-console-readout=false",
			"--summary-interval=0",
			"--download-result=full",
			srv.URL+"/full.bin",
		)
	}
	ref, refErr := RunRefWithOptions(t, commonArgs(refDir), "", RunOptions{Timeout: 10 * time.Second})
	impl, implErr := RunImplWithOptions(t, commonArgs(implDir), "", RunOptions{Timeout: 10 * time.Second})
	if refErr != nil {
		t.Fatalf("ref run error: %v\nstdout=%s\nstderr=%s", refErr, ref.Stdout, ref.Stderr)
	}
	if implErr != nil {
		t.Fatalf("impl run error: %v\nstdout=%s\nstderr=%s", implErr, impl.Stdout, impl.Stderr)
	}

	AssertEqualExit(t, ref, impl)
	if ref.ExitCode != 0 {
		t.Fatalf("ref exit=%d, want 0\nstdout=%s\nstderr=%s", ref.ExitCode, ref.Stdout, ref.Stderr)
	}

	for label, result := range map[string]RunResult{"ref": ref, "impl": impl} {
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "Download Results:") {
			t.Errorf("%s: output missing 'Download Results:' with --download-result=full:\n%s",
				label, combined)
		}
		// The full format includes the "  %|" column in the header row.
		// aria2c 1.37.0 empirically outputs:
		//   gid   |stat|avg speed  |  %|path/URI
		if !strings.Contains(combined, "|  %|") {
			t.Errorf("%s: output missing percentage column '|  %%|' in download-result=full:\n%s",
				label, combined)
		}
	}
}

// TestConsoleReadout_DownloadResultDefault verifies that --download-result=default
// emits the "Download Results:" table WITHOUT the percentage column, matching
// both binaries' behavior.
//
// Empirically confirmed header format (default):
//   gid   |stat|avg speed  |path/URI
func TestConsoleReadout_DownloadResultDefault(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "download-result")

	payload := bytes.Repeat([]byte("download-result-default-payload\n"), 128)
	srv := newProgressQuickServer(t, payload)

	refDir, implDir := t.TempDir(), t.TempDir()

	commonArgs := func(dir string) []string {
		return append(progressTestBaseArgs(dir, "default.bin"),
			"--quiet=false",
			"--console-log-level=error",
			"--show-console-readout=false",
			"--summary-interval=0",
			"--download-result=default",
			srv.URL+"/default.bin",
		)
	}
	ref, refErr := RunRefWithOptions(t, commonArgs(refDir), "", RunOptions{Timeout: 10 * time.Second})
	impl, implErr := RunImplWithOptions(t, commonArgs(implDir), "", RunOptions{Timeout: 10 * time.Second})
	if refErr != nil {
		t.Fatalf("ref run error: %v\nstdout=%s\nstderr=%s", refErr, ref.Stdout, ref.Stderr)
	}
	if implErr != nil {
		t.Fatalf("impl run error: %v\nstdout=%s\nstderr=%s", implErr, impl.Stdout, impl.Stderr)
	}

	AssertEqualExit(t, ref, impl)
	if ref.ExitCode != 0 {
		t.Fatalf("ref exit=%d, want 0\nstdout=%s\nstderr=%s", ref.ExitCode, ref.Stdout, ref.Stderr)
	}

	for label, result := range map[string]RunResult{"ref": ref, "impl": impl} {
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "Download Results:") {
			t.Errorf("%s: output missing 'Download Results:' with --download-result=default:\n%s",
				label, combined)
		}
		// Default format does NOT include the "  %|" column.
		if strings.Contains(combined, "|  %|") {
			t.Errorf("%s: default output unexpectedly contains percentage column '|  %%|':\n%s",
				label, combined)
		}
	}
}
