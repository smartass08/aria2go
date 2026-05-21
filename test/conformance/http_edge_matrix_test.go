package conformance

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPEdge_RedirectLimitMatrix(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("redirect edge payload\n")
	tests := []struct {
		name      string
		redirects int
		wantExit  int
		wantFile  bool
	}{
		{name: "twenty_redirects_allowed", redirects: 20, wantExit: 0, wantFile: true},
		{name: "twenty_one_redirects_rejected", redirects: 21, wantExit: 23},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload, redirects: tt.redirects})
			implFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload, redirects: tt.redirects})
			refDir, implDir := t.TempDir(), t.TempDir()

			tc := CommandMatrixCase{
				Name:    tt.name,
				Timeout: 20 * time.Second,
				Env:     httpEdgeProxylessEnv(),
				ArgsFor: func(target RunnerTarget) []string {
					dir, url := refDir, refFixture.url(httpEdgePathRedirect)
					if target == RunnerImpl {
						dir, url = implDir, implFixture.url(httpEdgePathRedirect)
					}
					return append(httpEdgeBaseArgs(dir, "redirect.bin"), url)
				},
			}
			result := RunCommandPair(t, tc)

			AssertEqualExit(t, result.Ref, result.Impl)
			if result.Ref.ExitCode != tt.wantExit {
				t.Fatalf("reference exit = %d, want %d\nstdout=%s\nstderr=%s", result.Ref.ExitCode, tt.wantExit, result.Ref.Stdout, result.Ref.Stderr)
			}
			if tt.wantFile {
				AssertFileBytes(t, filepath.Join(refDir, "redirect.bin"), payload)
				AssertFileBytes(t, filepath.Join(implDir, "redirect.bin"), payload)
			} else {
				AssertNoFile(t, filepath.Join(refDir, "redirect.bin"))
				AssertNoFile(t, filepath.Join(implDir, "redirect.bin"))
			}
		})
	}
}

func TestHTTPEdge_StatusRetryMatrix(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("status retry payload\n")
	tests := []struct {
		name     string
		statuses []int
		extra    []string
		wantExit int
		wantFile bool
	}{
		{
			name:     "404_without_max_file_not_found_aborts",
			statuses: []int{http.StatusNotFound},
			extra:    []string{"--max-tries=3", "--max-file-not-found=0", "--retry-wait=0"},
			wantExit: 3,
		},
		{
			name:     "404_retries_until_success_when_budget_allows",
			statuses: []int{http.StatusNotFound},
			extra:    []string{"--max-tries=3", "--max-file-not-found=2", "--retry-wait=0"},
			wantExit: 0,
			wantFile: true,
		},
		{
			name:     "404_reaches_max_file_not_found",
			statuses: []int{http.StatusNotFound, http.StatusNotFound},
			extra:    []string{"--max-tries=5", "--max-file-not-found=2", "--retry-wait=0"},
			wantExit: 4,
		},
		{
			name:     "503_without_retry_wait_aborts",
			statuses: []int{http.StatusServiceUnavailable},
			extra:    []string{"--max-tries=3", "--retry-wait=0"},
			wantExit: 29,
		},
		{
			name:     "503_retries_with_retry_wait",
			statuses: []int{http.StatusServiceUnavailable},
			extra:    []string{"--max-tries=3", "--retry-wait=1"},
			wantExit: 0,
			wantFile: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload, statusSequence: tt.statuses})
			implFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload, statusSequence: tt.statuses})
			refDir, implDir := t.TempDir(), t.TempDir()

			tc := CommandMatrixCase{
				Name:    tt.name,
				Timeout: 30 * time.Second,
				Env:     httpEdgeProxylessEnv(),
				ArgsFor: func(target RunnerTarget) []string {
					dir, url := refDir, refFixture.url(httpEdgePathStatus)
					if target == RunnerImpl {
						dir, url = implDir, implFixture.url(httpEdgePathStatus)
					}
					args := append(httpEdgeBaseArgs(dir, "status.bin"), tt.extra...)
					return append(args, url)
				},
			}
			result := RunCommandPair(t, tc)

			AssertEqualExit(t, result.Ref, result.Impl)
			if result.Ref.ExitCode != tt.wantExit {
				t.Fatalf("reference exit = %d, want %d\nstdout=%s\nstderr=%s", result.Ref.ExitCode, tt.wantExit, result.Ref.Stdout, result.Ref.Stderr)
			}
			if tt.wantFile {
				AssertFileBytes(t, filepath.Join(refDir, "status.bin"), payload)
				AssertFileBytes(t, filepath.Join(implDir, "status.bin"), payload)
			}
		})
	}
}

func TestHTTPEdge_RangeMatrix(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("0123456789abcdef")
	tests := []struct {
		name     string
		header   string
		wantExit int
		wantBody []byte
	}{
		{name: "suffix_range", header: "Range: bytes=-6", wantExit: 8},
		{name: "open_ended_range", header: "Range: bytes=4-", wantExit: 8},
		{name: "invalid_range", header: "Range: bytes=99-120", wantExit: 22},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload})
			implFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload})
			refDir, implDir := t.TempDir(), t.TempDir()

			tc := CommandMatrixCase{
				Name:    tt.name,
				Timeout: 20 * time.Second,
				Env:     httpEdgeProxylessEnv(),
				ArgsFor: func(target RunnerTarget) []string {
					dir, url := refDir, refFixture.url(httpEdgePathRange)
					if target == RunnerImpl {
						dir, url = implDir, implFixture.url(httpEdgePathRange)
					}
					args := append(httpEdgeBaseArgs(dir, "range.bin"), "--header="+tt.header)
					return append(args, url)
				},
			}
			result := RunCommandPair(t, tc)

			AssertEqualExit(t, result.Ref, result.Impl)
			if result.Ref.ExitCode != tt.wantExit {
				t.Fatalf("reference exit = %d, want %d\nstdout=%s\nstderr=%s", result.Ref.ExitCode, tt.wantExit, result.Ref.Stdout, result.Ref.Stderr)
			}
			if tt.wantExit == 0 {
				AssertFileBytes(t, filepath.Join(refDir, "range.bin"), tt.wantBody)
				AssertFileBytes(t, filepath.Join(implDir, "range.bin"), tt.wantBody)
			}
		})
	}
}

func TestHTTPEdge_GzipAcceptedOutput(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("gzip accepted output\n")
	refFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload})
	implFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload})
	refDir, implDir := t.TempDir(), t.TempDir()

	tc := CommandMatrixCase{
		Name:    "gzip_accepted_output",
		Timeout: 20 * time.Second,
		Env:     httpEdgeProxylessEnv(),
		ArgsFor: func(target RunnerTarget) []string {
			dir, url := refDir, refFixture.url(httpEdgePathGzip)
			if target == RunnerImpl {
				dir, url = implDir, implFixture.url(httpEdgePathGzip)
			}
			args := append(httpEdgeBaseArgs(dir, "gzip.bin"), "--http-accept-gzip=true")
			return append(args, url)
		},
	}
	result := RunCommandPair(t, tc)

	AssertEqualExit(t, result.Ref, result.Impl)
	requireExitSuccess(t, "ref gzip", result.Ref)
	requireExitSuccess(t, "impl gzip", result.Impl)
	AssertFileBytes(t, filepath.Join(refDir, "gzip.bin"), payload)
	AssertFileBytes(t, filepath.Join(implDir, "gzip.bin"), payload)
	if !refFixture.sawAcceptEncoding(httpEdgePathGzip, "gzip") {
		t.Fatalf("reference did not send Accept-Encoding: gzip; records=%#v", refFixture.recordsFor(httpEdgePathGzip))
	}
	if !implFixture.sawAcceptEncoding(httpEdgePathGzip, "gzip") {
		t.Fatalf("implementation did not send Accept-Encoding: gzip; records=%#v", implFixture.recordsFor(httpEdgePathGzip))
	}
}

func TestHTTPEdge_CookieLoadSave(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("cookie payload\n")
	refFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{
		payload:        payload,
		requiredCookie: "session=loaded",
		setCookie:      "saved=from-server; Path=/",
	})
	implFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{
		payload:        payload,
		requiredCookie: "session=loaded",
		setCookie:      "saved=from-server; Path=/",
	})
	refDir, implDir := t.TempDir(), t.TempDir()
	refCookieIn := filepath.Join(refDir, "cookies-in.txt")
	implCookieIn := filepath.Join(implDir, "cookies-in.txt")
	refCookieOut := filepath.Join(refDir, "cookies-out.txt")
	implCookieOut := filepath.Join(implDir, "cookies-out.txt")
	writeNetscapeCookie(t, refCookieIn, refFixture.server.URL, "session", "loaded")
	writeNetscapeCookie(t, implCookieIn, implFixture.server.URL, "session", "loaded")

	tc := CommandMatrixCase{
		Name:    "cookie_load_save",
		Timeout: 20 * time.Second,
		Env:     httpEdgeProxylessEnv(),
		ArgsFor: func(target RunnerTarget) []string {
			dir, in, out, url := refDir, refCookieIn, refCookieOut, refFixture.url(httpEdgePathCookie)
			if target == RunnerImpl {
				dir, in, out, url = implDir, implCookieIn, implCookieOut, implFixture.url(httpEdgePathCookie)
			}
			args := append(httpEdgeBaseArgs(dir, "cookie.bin"),
				"--load-cookies="+in,
				"--save-cookies="+out,
			)
			return append(args, url)
		},
	}
	result := RunCommandPair(t, tc)

	AssertEqualExit(t, result.Ref, result.Impl)
	requireExitSuccess(t, "ref cookies", result.Ref)
	requireExitSuccess(t, "impl cookies", result.Impl)
	AssertFileBytes(t, filepath.Join(refDir, "cookie.bin"), payload)
	AssertFileBytes(t, filepath.Join(implDir, "cookie.bin"), payload)
	if !refFixture.sawCookie(httpEdgePathCookie, "session=loaded") {
		t.Fatalf("reference did not send loaded cookie; records=%#v", refFixture.recordsFor(httpEdgePathCookie))
	}
	if !implFixture.sawCookie(httpEdgePathCookie, "session=loaded") {
		t.Fatalf("implementation did not send loaded cookie; records=%#v", implFixture.recordsFor(httpEdgePathCookie))
	}
	requireCookieFileContains(t, refCookieOut, "saved", "from-server")
	requireCookieFileContains(t, implCookieOut, "saved", "from-server")
}

func TestHTTPEdge_NoProxyBypassesInvalidEnv(t *testing.T) {
	SkipIfNoRef(t)

	payload := []byte("no proxy payload\n")
	refFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload})
	implFixture := newHTTPEdgeFixture(t, httpEdgeFixtureOptions{payload: payload})
	refDir, implDir := t.TempDir(), t.TempDir()

	tc := CommandMatrixCase{
		Name:    "no_proxy_bypasses_invalid_env",
		Timeout: 20 * time.Second,
		Env:     httpEdgeInvalidProxyEnv(),
		ArgsFor: func(target RunnerTarget) []string {
			dir, url := refDir, refFixture.url(httpEdgePathPayload)
			if target == RunnerImpl {
				dir, url = implDir, implFixture.url(httpEdgePathPayload)
			}
			args := append(httpEdgeBaseArgs(dir, "no-proxy.bin"), "--no-proxy=127.0.0.1,localhost")
			return append(args, url)
		},
	}
	result := RunCommandPair(t, tc)

	AssertEqualExit(t, result.Ref, result.Impl)
	requireExitSuccess(t, "ref no-proxy", result.Ref)
	requireExitSuccess(t, "impl no-proxy", result.Impl)
	AssertFileBytes(t, filepath.Join(refDir, "no-proxy.bin"), payload)
	AssertFileBytes(t, filepath.Join(implDir, "no-proxy.bin"), payload)
}

func httpEdgeProxylessEnv() []string {
	return []string{
		"http_proxy=",
		"HTTP_PROXY=",
		"https_proxy=",
		"HTTPS_PROXY=",
		"all_proxy=",
		"ALL_PROXY=",
		"no_proxy=",
		"NO_PROXY=",
	}
}

func httpEdgeInvalidProxyEnv() []string {
	env := httpEdgeProxylessEnv()
	return append(env,
		"http_proxy=http://127.0.0.1:1",
		"HTTP_PROXY=http://127.0.0.1:1",
		"all_proxy=http://127.0.0.1:1",
		"ALL_PROXY=http://127.0.0.1:1",
	)
}

func httpEdgeBaseArgs(dir, out string) []string {
	return []string{
		"--no-conf=true",
		"--dir=" + dir,
		"--out=" + out,
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--enable-dht=false",
		"--enable-dht6=false",
		"--split=1",
		"--max-connection-per-server=1",
		"--connect-timeout=5",
		"--timeout=5",
	}
}

func writeNetscapeCookie(t *testing.T, path, rawURL, name, value string) {
	t.Helper()

	host := strings.TrimPrefix(rawURL, "http://")
	if idx := strings.IndexByte(host, ':'); idx >= 0 {
		host = host[:idx]
	}
	line := fmt.Sprintf("%s\tFALSE\t/\tFALSE\t1893456000\t%s\t%s\n", host, name, value)
	data := "# Netscape HTTP Cookie File\n" + line
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write cookie file %s: %v", path, err)
	}
}

func requireCookieFileContains(t *testing.T, path, name, value string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cookie file %s: %v", path, err)
	}
	want := "\t" + name + "\t" + value
	if !strings.Contains(string(data), want) {
		t.Fatalf("cookie file %s missing %s=%s:\n%s", path, name, value, string(data))
	}
}
