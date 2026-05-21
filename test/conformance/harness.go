package conformance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

// RunResult holds the captured output and exit code of a process run.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunOptions controls process execution for conformance probes.
type RunOptions struct {
	Env     []string
	Dir     string
	Timeout time.Duration
}

// projectRoot returns the absolute path to the project root by walking up
// from the current working directory until go.mod is found.
func projectRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findProjectRoot(wd)
}

func findProjectRoot(dir string) (string, error) {
	for {
		modPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(modPath); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("project root not found (no go.mod in ancestor directories)")
		}
		dir = parent
	}
}

// RunRef runs the reference aria2c binary with the given args and optional stdin.
// It tries "aria2c" in PATH first, then "$PROJECT_ROOT/aria2c-ref".
func RunRef(t *testing.T, args []string, stdin string) (RunResult, error) {
	t.Helper()

	bin, err := findRefBinary()
	if err != nil {
		return RunResult{}, err
	}

	return runBinary(t, bin, args, stdin)
}

// RunRefWithOptions runs the reference aria2c binary with execution controls.
func RunRefWithOptions(t *testing.T, args []string, stdin string, opts RunOptions) (RunResult, error) {
	t.Helper()

	bin, err := findRefBinary()
	if err != nil {
		return RunResult{}, err
	}
	return runBinaryWithOptions(t, bin, args, stdin, opts)
}

// RunImpl runs the aria2go binary from ./cmd/aria2go/ with the given args and
// optional stdin.  The binary is built on first call per test process; the
// cached path is reused across tests.
func RunImpl(t *testing.T, args []string, stdin string) (RunResult, error) {
	t.Helper()

	bin, err := implBinary()
	if err != nil {
		return RunResult{}, fmt.Errorf("build aria2go: %w", err)
	}
	return runBinary(t, bin, args, stdin)
}

// RunImplWithOptions runs aria2go with execution controls.
func RunImplWithOptions(t *testing.T, args []string, stdin string, opts RunOptions) (RunResult, error) {
	t.Helper()

	bin, err := implBinary()
	if err != nil {
		return RunResult{}, fmt.Errorf("build aria2go: %w", err)
	}
	return runBinaryWithOptions(t, bin, args, stdin, opts)
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
		outPath := filepath.Join(os.TempDir(), "aria2go-test-conformance", "aria2go")
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
			implBinErr = fmt.Errorf("go build ./cmd/aria2go/: %w", err)
			return
		}
		implBinPath = outPath
	})
	return implBinPath, implBinErr
}

func runBinary(t *testing.T, bin string, args []string, stdin string) (RunResult, error) {
	t.Helper()

	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	runErr := cmd.Run()

	result := RunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("run %s: %w", bin, runErr)
	}
	result.ExitCode = 0
	return result, nil
}

func runBinaryWithOptions(t *testing.T, bin string, args []string, stdin string, opts RunOptions) (RunResult, error) {
	t.Helper()

	ctx := context.Background()
	cancel := func() {}
	if opts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Env, opts.Env...)
	}
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	runErr := cmd.Run()
	result := RunResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.ExitCode = -1
		return result, fmt.Errorf("run %s timed out after %s", bin, opts.Timeout)
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("run %s: %w", bin, runErr)
	}
	result.ExitCode = 0
	return result, nil
}

// AssertEqualStdout normalizes and compares stdout of the reference and
// implementation runs.  Normalization converts \r\n to \n and trims trailing
// whitespace from each line.
func AssertEqualStdout(t *testing.T, ref, impl RunResult) {
	t.Helper()

	refNorm := normalizeOutput(ref.Stdout)
	implNorm := normalizeOutput(impl.Stdout)

	if refNorm != implNorm {
		t.Errorf("stdout mismatch:\n--- ref\n+++ impl\n%s", diff(refNorm, implNorm))
	}
}

// AssertEqualExit compares the exit codes of the reference and implementation
// runs.
func AssertEqualExit(t *testing.T, ref, impl RunResult) {
	t.Helper()

	if ref.ExitCode != impl.ExitCode {
		t.Errorf("exit code mismatch: ref=%d, impl=%d", ref.ExitCode, impl.ExitCode)
	}
}

// SkipIfNoRef calls t.Skip if the aria2c reference binary is not available
// (neither in PATH nor as ./aria2c-ref), or if it is available but does not
// respond to JSON-RPC calls.
func SkipIfNoRef(t *testing.T) {
	t.Helper()

	if _, err := findRefBinary(); err != nil {
		t.Skip("aria2c reference binary not available (aria2c in PATH or ./aria2c-ref)")
	}

	checkRefRPC(t)
}

var refRPCCheckOnce sync.Once

func checkRefRPC(t *testing.T) {
	t.Helper()

	refRPCCheckOnce.Do(func() {
		bin, err := findRefBinary()
		if err != nil {
			return
		}

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, bin,
			"--enable-rpc",
			"--no-conf=true",
			"--disable-ipv6=true",
			"--rpc-listen-all=false",
			"--rpc-listen-port="+strconv.Itoa(port),
			"--quiet",
		)
		cmd.Stdout, cmd.Stderr = openDevNull(t)
		cmd.WaitDelay = 2 * time.Second

		if err := cmd.Start(); err != nil {
			return
		}

		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()

		defer func() {
			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				cmd.Process.Kill()
				<-done
			}
		}()

		url := "http://127.0.0.1:" + strconv.Itoa(port) + "/jsonrpc"
		body := bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":"1","method":"system.listMethods","params":[]}`))
		client := &http.Client{Timeout: 2 * time.Second}

		for i := 0; i < 30; i++ {
			resp, err := client.Post(url, "application/json", body)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					refRPCVerified.Store(true)
					return
				}
			}
			// Reset body reader for retry.
			body = bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":"1","method":"system.listMethods","params":[]}`))
			time.Sleep(100 * time.Millisecond)
		}
	})

	if !refRPCVerified.Load() {
		t.Skip("aria2c RPC not available (binary found but does not respond to JSON-RPC)")
	}
}

func normalizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

func diff(a, b string) string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	var buf strings.Builder
	n := len(al)
	if len(bl) > n {
		n = len(bl)
	}
	for i := 0; i < n; i++ {
		var la, lb string
		if i < len(al) {
			la = al[i]
		}
		if i < len(bl) {
			lb = bl[i]
		}
		if la == lb {
			buf.WriteString(fmt.Sprintf("  %s\n", la))
		} else {
			buf.WriteString(fmt.Sprintf("- %s\n+ %s\n", la, lb))
		}
	}
	return buf.String()
}
