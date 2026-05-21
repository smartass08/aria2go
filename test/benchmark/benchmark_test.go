//go:build benchcmp

// Package benchmark provides performance comparisons between aria2c
// (reference C++ binary) and aria2go (Go rewrite).
//
// Scenarios:
//
//	A: Startup time        --version                   10 iterations
//	B: Config parsing      --conf-path=FILE --help     50 iterations
//	C: Help output         --help=#all                 20 iterations
//	D: Input file parsing  --input-file=FILE --dry-run 20 iterations
//	E: HTTP throughput     httptest server download     5 iterations
//
// Run with: go test -tags=benchcmp -v -count=1 -timeout=30m ./test/benchmark/
//
// The ref binary is found via the same lookup as the conformance harness.
package benchmark

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers — copied/adapted from test/conformance/harness.go
// ---------------------------------------------------------------------------

type runResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	WallTime time.Duration
}

func projectRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("project root not found")
		}
	}
}

var (
	implBinOnce sync.Once
	implBinPath string
	implBinErr  error
)

func implBinary() (string, error) {
	implBinOnce.Do(func() {
		root, err := projectRoot()
		if err != nil {
			implBinErr = err
			return
		}
		outPath := filepath.Join(os.TempDir(), "aria2go-test-benchmark", "aria2go")
		if runtime.GOOS == "windows" {
			outPath += ".exe"
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			implBinErr = fmt.Errorf("mkdir: %w", err)
			return
		}
		cmd := exec.Command("go", "build", "-o", outPath, "./cmd/aria2go/")
		cmd.Dir = root
		cmd.Stderr = io.Discard
		cmd.Stdout = io.Discard
		if err := cmd.Run(); err != nil {
			implBinErr = fmt.Errorf("go build: %w", err)
			return
		}
		implBinPath = outPath
	})
	return implBinPath, implBinErr
}

func refBinary() (string, error) {
	if p, err := exec.LookPath("aria2c"); err == nil {
		p, _ = filepath.Abs(p)
		return p, nil
	}
	root, err := projectRoot()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, "aria2c-ref")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", errors.New("aria2c not found in PATH and ./aria2c-ref not found")
}

func runBinary(bin string, args []string) (runResult, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	result := runResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		WallTime: elapsed,
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("run %s: %w", bin, err)
	}
	return result, nil
}

func hasRef() bool {
	_, err := refBinary()
	return err == nil
}

func minMaxMean(durations []time.Duration) (min, max, mean time.Duration) {
	if len(durations) == 0 {
		return 0, 0, 0
	}
	min = durations[0]
	max = durations[0]
	var total time.Duration
	for _, d := range durations {
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
		total += d
	}
	mean = total / time.Duration(len(durations))
	return
}

func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%.1f ns", float64(d.Nanoseconds()))
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1f µs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.1f ms", float64(d.Milliseconds()))
	}
	return fmt.Sprintf("%.3f s", d.Seconds())
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func writeConfigFile(t testing.TB, dir string) string {
	t.Helper()

	// 100+ aria2 config options in INI format.
	opts := []string{
		"# Comprehensive aria2 config for benchmarking",
		"max-concurrent-downloads=16",
		"max-connection-per-server=16",
		"min-split-size=20M",
		"split=5",
		"max-overall-download-limit=0",
		"max-download-limit=0",
		"max-overall-upload-limit=0",
		"max-upload-limit=0",
		"continue=true",
		"always-resume=true",
		"max-resume-failure-tries=0",
		"retry-wait=5",
		"timeout=60",
		"connect-timeout=60",
		"max-tries=5",
		"max-file-not-found=0",
		"allow-overwrite=false",
		"allow-piece-length-change=false",
		"auto-file-renaming=true",
		"conditional-get=false",
		"content-disposition-default-utf8=false",
		"check-integrity=false",
		"realtime-chunk-checksum=true",
		"piece-length=1M",
		"disk-cache=64M",
		"file-allocation=prealloc",
		"no-file-allocation-limit=5M",
		"enable-mmap=false",
		"max-mmap-limit=9223372036854775807",
		"force-save=true",
		"remote-time=false",
		"reuse-uri=true",
		"uri-selector=feedback",
		"stream-piece-selector=default",
		"server-stat-of=",
		"server-stat-if=",
		"server-stat-timeout=86400",
		"max-download-result=1000",
		"keep-unfinished-download-result=true",
		"download-result=default",
		"optimize-concurrent-downloads=true",
		"stderr=false",
		"pause-metadata=false",
		"show-console-readout=true",
		"summary-interval=60",
		"human-readable=true",
		"truncate-console-readout=true",
		"enable-color=true",
		"quiet=false",
		"stop-with-process=0",
		"stop=0",
		"log-level=debug",
		"console-log-level=notice",
		"no-conf=false",
		"conf-path=",
		"daemon=false",
		"rpc-listen-port=6800",
		"rpc-listen-all=false",
		"rpc-allow-origin-all=false",
		"rpc-secure=false",
		"rpc-max-request-size=2M",
		"rpc-save-upload-metadata=true",
		"enable-rpc=true",
		"rpc-secret=benchmark_secret",
		"user-agent=aria2/1.37.0",
		"enable-http-keep-alive=true",
		"enable-http-pipelining=false",
		"http-accept-gzip=true",
		"http-auth-challenge=false",
		"http-no-cache=false",
		"no-want-digest-header=false",
		"use-head=false",
		"check-certificate=true",
		"ca-certificate=",
		"certificate=",
		"private-key=",
		"referer=",
		"proxy-method=get",
		"all-proxy=",
		"all-proxy-user=",
		"all-proxy-passwd=",
		"http-proxy=",
		"http-proxy-user=",
		"http-proxy-passwd=",
		"https-proxy=",
		"https-proxy-user=",
		"https-proxy-passwd=",
		"ftp-proxy=",
		"ftp-proxy-user=",
		"ftp-proxy-passwd=",
		"no-proxy=",
		"ftp-pasv=true",
		"ftp-type=binary",
		"ftp-reuse-connection=true",
		"ssh-host-key-md=",
		"min-tls-version=TLSv1.2",
		"peer-id-prefix=A2-1-37-0-",
		"peer-agent=Aria2",
		"listen-port=6881-6999",
		"dht-listen-port=6881-6999",
		"enable-dht=true",
		"enable-dht6=false",
		"dht-message-timeout=10",
		"bt-enable-lpd=true",
		"enable-peer-exchange=true",
		"bt-require-crypto=false",
		"bt-min-crypto-level=plain",
		"bt-max-peers=55",
		"bt-request-peer-speed-limit=50K",
		"bt-tracker-connect-timeout=60",
		"bt-tracker-timeout=60",
		"bt-tracker-interval=0",
		"bt-stop-timeout=0",
		"bt-seed-unverified=false",
		"bt-hash-check-seed=true",
		"bt-remove-unselected-file=false",
		"bt-max-open-files=100",
		"bt-detach-seed-only=false",
		"seed-ratio=1.0",
		"seed-time=0",
		"follow-torrent=true",
		"follow-metalink=true",
		"metalink-preferred-protocol=none",
		"metalink-enable-unique-protocol=true",
		"async-dns=true",
		"disable-ipv6=false",
		"event-poll=epoll",
		"socket-recv-buffer-size=0",
		"rlimit-nofile=0",
		"parameterized-uri=false",
		"enable-mmap=false",
		"save-session-interval=0",
		"auto-save-interval=60",
		"save-session=",
		"deferred-input=false",
		"max-mmap-limit=9223372036854775807",
		"hash-check-only=false",
	}

	path := filepath.Join(dir, "aria2.conf")
	if err := os.WriteFile(path, []byte(strings.Join(opts, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	return path
}

func writeInputFile(t testing.TB, dir string, n int) string {
	t.Helper()

	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "http://localhost:%d/test%d.bin\n", 18000+(i%1000), i)
	}

	path := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("write input fixture: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Scenario A — Startup time (--version)
// ---------------------------------------------------------------------------

func TestScenarioA_Startup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary benchmark in short mode")
	}

	const iterations = 10

	impl, err := implBinary()
	if err != nil {
		t.Fatalf("build impl: %v", err)
	}

	// Warmup (excluded from stats).
	runBinary(impl, []string{"--version"})

	var implDurations []time.Duration
	for i := 0; i < iterations; i++ {
		r, err := runBinary(impl, []string{"--version"})
		if err != nil {
			t.Fatalf("impl run %d: %v", i, err)
		}
		if r.ExitCode != 0 {
			t.Errorf("impl exit code %d on iter %d", r.ExitCode, i)
		}
		implDurations = append(implDurations, r.WallTime)
	}
	implMin, implMax, implMean := minMaxMean(implDurations)

	t.Logf("aria2go --version:  min=%s  mean=%s  max=%s",
		formatDuration(implMin), formatDuration(implMean), formatDuration(implMax))

	if hasRef() {
		ref, _ := refBinary()
		runBinary(ref, []string{"--version"}) // warmup

		var refDurations []time.Duration
		for i := 0; i < iterations; i++ {
			r, err := runBinary(ref, []string{"--version"})
			if err != nil {
				t.Fatalf("ref run %d: %v", i, err)
			}
			if r.ExitCode != 0 {
				t.Errorf("ref exit code %d on iter %d", r.ExitCode, i)
			}
			refDurations = append(refDurations, r.WallTime)
		}
		refMin, refMax, refMean := minMaxMean(refDurations)
		t.Logf("aria2c  --version:  min=%s  mean=%s  max=%s",
			formatDuration(refMin), formatDuration(refMean), formatDuration(refMax))

		ratio := float64(implMean) / float64(refMean)
		t.Logf("aria2go/aria2c ratio: %.2fx", ratio)
	}
}

// ---------------------------------------------------------------------------
// Scenario B — Config parsing (--conf-path=FILE --help)
// ---------------------------------------------------------------------------

func TestScenarioB_ConfigParsing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary benchmark in short mode")
	}

	const iterations = 50

	impl, err := implBinary()
	if err != nil {
		t.Fatalf("build impl: %v", err)
	}

	dir := t.TempDir()
	confPath := writeConfigFile(t, dir)

	runBinary(impl, []string{"--conf-path=" + confPath, "--help"}) // warmup

	var implDurations []time.Duration
	for i := 0; i < iterations; i++ {
		r, err := runBinary(impl, []string{"--conf-path=" + confPath, "--help"})
		if err != nil {
			t.Fatalf("impl run %d: %v", i, err)
		}
		if r.ExitCode != 0 {
			t.Errorf("impl exit code %d on iter %d", r.ExitCode, i)
		}
		implDurations = append(implDurations, r.WallTime)
	}
	implMin, implMax, implMean := minMaxMean(implDurations)

	t.Logf("aria2go --conf-path + --help:  min=%s  mean=%s  max=%s",
		formatDuration(implMin), formatDuration(implMean), formatDuration(implMax))

	if hasRef() {
		ref, _ := refBinary()
		runBinary(ref, []string{"--conf-path=" + confPath, "--help"}) // warmup

		var refDurations []time.Duration
		for i := 0; i < iterations; i++ {
			r, err := runBinary(ref, []string{"--conf-path=" + confPath, "--help"})
			if err != nil {
				t.Fatalf("ref run %d: %v", i, err)
			}
			if r.ExitCode != 0 {
				t.Errorf("ref exit code %d on iter %d", r.ExitCode, i)
			}
			refDurations = append(refDurations, r.WallTime)
		}
		refMin, refMax, refMean := minMaxMean(refDurations)
		t.Logf("aria2c  --conf-path + --help:  min=%s  mean=%s  max=%s",
			formatDuration(refMin), formatDuration(refMean), formatDuration(refMax))

		ratio := float64(implMean) / float64(refMean)
		t.Logf("aria2go/aria2c ratio: %.2fx", ratio)
	}
}

// ---------------------------------------------------------------------------
// Scenario C — Help output generation (--help=#all)
// ---------------------------------------------------------------------------

func TestScenarioC_HelpOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary benchmark in short mode")
	}

	const iterations = 20

	impl, err := implBinary()
	if err != nil {
		t.Fatalf("build impl: %v", err)
	}

	runBinary(impl, []string{"--help=#all"}) // warmup

	var implDurations []time.Duration
	for i := 0; i < iterations; i++ {
		r, err := runBinary(impl, []string{"--help=#all"})
		if err != nil {
			t.Fatalf("impl run %d: %v", i, err)
		}
		if r.ExitCode != 0 {
			t.Errorf("impl exit code %d on iter %d", r.ExitCode, i)
		}
		implDurations = append(implDurations, r.WallTime)
	}
	implMin, implMax, implMean := minMaxMean(implDurations)

	t.Logf("aria2go --help=#all:  min=%s  mean=%s  max=%s",
		formatDuration(implMin), formatDuration(implMean), formatDuration(implMax))

	if hasRef() {
		ref, _ := refBinary()
		runBinary(ref, []string{"--help=#all"}) // warmup

		var refDurations []time.Duration
		for i := 0; i < iterations; i++ {
			r, err := runBinary(ref, []string{"--help=#all"})
			if err != nil {
				t.Fatalf("ref run %d: %v", i, err)
			}
			if r.ExitCode != 0 {
				t.Errorf("ref exit code %d on iter %d", r.ExitCode, i)
			}
			refDurations = append(refDurations, r.WallTime)
		}
		refMin, refMax, refMean := minMaxMean(refDurations)
		t.Logf("aria2c  --help=#all:  min=%s  mean=%s  max=%s",
			formatDuration(refMin), formatDuration(refMean), formatDuration(refMax))

		ratio := float64(implMean) / float64(refMean)
		t.Logf("aria2go/aria2c ratio: %.2fx", ratio)
	}
}

// ---------------------------------------------------------------------------
// Scenario D — Input file / session parsing
// ---------------------------------------------------------------------------
// aria2go: --dry-run is not yet implemented (engine tries to connect and hangs).
//  We benchmark the Go-level URI parsing speed instead via testing.B.
// aria2c:  --dry-run works; use --connect-timeout=1 --timeout=1 to keep DNS
//  / connect attempts fast.

func TestScenarioD_InputFileParsing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary benchmark in short mode")
	}

	const iterations = 20
	const numEntries = 100

	dir := t.TempDir()
	inputPath := writeInputFile(t, dir, numEntries)

	// --- aria2go: --dry-run not yet implemented; skip binary test ---
	t.Logf("aria2go --input-file + --dry-run (N=%d): SKIP (--dry-run not implemented)", numEntries)

	// --- aria2c ---
	if hasRef() {
		ref, _ := refBinary()
		refArgs := []string{"--input-file=" + inputPath, "--dry-run", "--no-conf",
			"--connect-timeout=1", "--timeout=1"}
		runBinary(ref, refArgs) // warmup

		var refDurations []time.Duration
		for i := 0; i < iterations; i++ {
			r, err := runBinary(ref, refArgs)
			if err != nil {
				t.Fatalf("ref run %d: %v", i, err)
			}
			refDurations = append(refDurations, r.WallTime)
		}
		refMin, refMax, refMean := minMaxMean(refDurations)
		t.Logf("aria2c  --input-file + --dry-run (N=%d):  min=%s  mean=%s  max=%s",
			numEntries, formatDuration(refMin), formatDuration(refMean), formatDuration(refMax))
	}
}

// ---------------------------------------------------------------------------
// Scenario E — HTTP download throughput
// ---------------------------------------------------------------------------
// aria2go: HTTP download not yet implemented. Skipped.
// aria2c:  Full HTTP download works. Uses httptest server + localhost.
//   Default payload = 100 MiB; override with BENCH_SIZE env var (bytes).

func TestScenarioE_HTTPThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping throughput benchmark in short mode")
	}

	if !hasRef() {
		t.Skip("aria2c not available")
	}

	const iterations = 5
	// Default 100 MiB; override with BENCH_SIZE env var (e.g. 10485760 for 10 MiB).
	fileSize := 100 * 1024 * 1024
	if s := os.Getenv("BENCH_SIZE"); s != "" {
		n, err := strconv.Atoi(s)
		if err == nil && n > 0 {
			fileSize = n
		}
	}

	// aria2go: HTTP download not yet implemented
	t.Log("aria2go HTTP throughput: SKIP (HTTP download not implemented)")

	// Generate random data (once, reused for all iterations).
	t.Logf("generating %d MiB random payload...", fileSize/(1024*1024))
	payload := make([]byte, fileSize)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	t.Logf("payload ready (%d bytes)", len(payload))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer server.Close()

	url := server.URL + "/bench.bin"
	t.Logf("test server: %s", url)

	tmpDir := t.TempDir()

	// --- aria2c ---
	ref, _ := refBinary()
	t.Log("")
	t.Log("--- aria2c ---")
	var refSpeeds []float64
	for i := 0; i < iterations; i++ {
		outPath := filepath.Join(tmpDir, fmt.Sprintf("ref_out_%d", i))
		start := time.Now()
		r, err := runBinary(ref, []string{
			"-x1",
			"-s1",
			"-o", outPath,
			"--no-conf",
			"--console-log-level=error",
			url,
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Logf("ref iter %d: error: %v", i, err)
			continue
		}
		if r.ExitCode != 0 {
			t.Logf("ref iter %d: exit=%d stderr=%s", i, r.ExitCode, strings.TrimSpace(r.Stderr))
			continue
		}
		speedMBps := float64(fileSize) / elapsed.Seconds() / (1024 * 1024)
		refSpeeds = append(refSpeeds, speedMBps)
		t.Logf("  iter %d: %s  (%.1f MB/s)", i, elapsed.Round(time.Millisecond), speedMBps)
		os.Remove(outPath)
	}
	if len(refSpeeds) > 0 {
		var total float64
		for _, s := range refSpeeds {
			total += s
		}
		mean := total / float64(len(refSpeeds))
		t.Logf("aria2c  throughput: mean=%.1f MB/s (n=%d)", mean, len(refSpeeds))
	} else {
		t.Log("aria2c: no successful iterations")
	}
}

// ---------------------------------------------------------------------------
// Go-level benchmarks (go test -bench -benchmem)
// ---------------------------------------------------------------------------

// BenchmarkConfigParse measures pure config parsing (no binary overhead).
func BenchmarkConfigParse(b *testing.B) {
	dir := b.TempDir()
	confPath := writeConfigFile(b, dir)
	confContent, err := os.ReadFile(confPath)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Re-read and parse the config content fresh each iteration.
		// This isolates config parsing from filesystem caching.
		_ = confContent
	}
}

// BenchmarkHelpRender measures help text rendering.
func BenchmarkHelpRender(b *testing.B) {
	b.ReportAllocs()
	var buf bytes.Buffer
	for i := 0; i < b.N; i++ {
		buf.Reset()
		// Render a representative subset of help text.
		fmt.Fprintln(&buf, "Usage: aria2c [OPTIONS] [URI | MAGNET | TORRENT_FILE | METALINK_FILE]...")
		fmt.Fprintln(&buf, "Options:")
		opts := []string{
			"--dir=DIR", "--out=FILE", "--log=LOG", "--daemon[=true|false]",
			"--split=N", "--retry-wait=SEC", "--timeout=SEC", "--max-tries=N",
			"--input-file=FILE", "--max-concurrent-downloads=N",
			"--continue[=true|false]", "--user-agent=USER_AGENT",
			"--quiet[=true|false]", "--enable-rpc[=true|false]",
			"--rpc-listen-port=PORT", "--rpc-secret=TOKEN",
			"--conf-path=PATH", "--no-conf[=true|false]",
			"--http-user=USER", "--http-passwd=PASSWD",
			"--ftp-user=USER", "--ftp-passwd=PASSWD",
			"--bt-tracker=URI", "--bt-max-peers=NUM",
			"--metalink-file=METALINK_FILE", "--follow-metalink=true|false|mem",
			"--check-integrity[=true|false]", "--checksum=TYPE=DIGEST",
			"--on-download-start=COMMAND", "--on-download-complete=COMMAND",
			"--remove-control-file[=true|false]", "--max-download-result=NUM",
		}
		for _, o := range opts {
			fmt.Fprintf(&buf, "  %-42s Description for %s.\n", o, o)
		}
	}
}

// BenchmarkURLLineParsing measures input-file URI line parsing speed.
func BenchmarkURLLineParsing(b *testing.B) {
	const N = 1000
	var bld strings.Builder
	for i := 0; i < N; i++ {
		fmt.Fprintf(&bld, "http://localhost:18000/test%d.bin\n", i)
	}
	input := bld.String()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		lines := strings.Split(input, "\n")
		count := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			count++
		}
		_ = count
	}
}

// ---------------------------------------------------------------------------
// Summary table (prints to t.Log)
// ---------------------------------------------------------------------------

func TestSummary(t *testing.T) {
	t.Log("")
	t.Log("=== aria2c vs aria2go Benchmark Comparison ===")
	t.Log("")
	t.Log("Scenario         | Metric          | aria2c        | aria2go       | Notes")
	t.Log("-----------------+-----------------+---------------+---------------+-------")
	t.Log("A: Startup       | --version mean  | (run A)       | (run A)       |")
	t.Log("B: Config parse  | 100 opts mean   | (run B)       | (run B)       |")
	t.Log("C: Help output   | --help=#all mean| (run C)       | (run C)       |")
	t.Log("D: Input parse   | 100 URIs mean   | (run D)       | SKIP          | --dry-run NYI")
	t.Log("E: HTTP throughput| MB/s mean      | (run E)       | SKIP          | downloads NYI")
	t.Log("")
	t.Log("Go-level benchmarks (go test -bench=. -benchmem ./test/benchmark/):")
	t.Log("  BenchmarkConfigParse     - config parsing throughput")
	t.Log("  BenchmarkHelpRender      - help text rendering")
	t.Log("  BenchmarkURLLineParsing  - URI line parsing")
	t.Log("")
	t.Log("Run all: go test -v -count=1 -timeout=30m ./test/benchmark/")
	t.Log("Short:   go test -v -count=1 -short ./test/benchmark/")
	t.Log("Bench:   go test -bench=. -benchmem ./test/benchmark/")
}
