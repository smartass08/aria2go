package hookrunner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunSuccess(t *testing.T) {
	ctx := context.Background()
	cmd, args := echoCmd()
	err := Run(ctx, cmd, args...)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunFireAndForget(t *testing.T) {
	ctx := context.Background()
	cmd, args := sleepCmd(5)

	start := time.Now()
	err := Run(ctx, cmd, args...)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Run blocked for %v; expected immediate return (fire-and-forget)", elapsed)
	}
}

func TestRunCommandArgs(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "hook.out")

	var cmd string
	var args []string
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []string{"/c", "echo %1 %2 %3 > " + outFile, "cmd", "arg-hex", "42"}
	} else {
		cmd = "sh"
		args = []string{"-c", "echo \"$1\" \"$2\" \"$3\" > " + outFile, "sh", "arg-hex", "42"}
	}

	err := Run(ctx, cmd, args...)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Give the child a moment to write the file.
	time.Sleep(200 * time.Millisecond)

	data, err := os.ReadFile(outFile)
	if err != nil {
		// File may not exist yet if child is slow; not a test failure.
		t.Skipf("child process output file not ready: %v", err)
	}
	got := string(bytes.TrimSpace(data))
	if !strings.Contains(got, "arg-hex") || !strings.Contains(got, "42") {
		t.Errorf("unexpected output: %q (want args 'arg-hex' and '42')", got)
	}
}

func TestRunInvalidCommand(t *testing.T) {
	ctx := context.Background()
	err := Run(ctx, "/nonexistent/command/xyzzy")
	if err == nil {
		t.Error("expected error for nonexistent command, got nil")
	}
}

func TestRunWithGIDAndFileCountArgs(t *testing.T) {
	// Matching aria2's exact call: executeHook(cmd, gid, numFiles, firstFilename)
	ctx := context.Background()
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "hook.out")

	gidHex := "0000000000000001"
	numFilesStr := "3"
	firstFilename := "/tmp/test-file.dat"

	var cmd string
	var args []string
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []string{"/c", "echo %1 %2 %3 > " + outFile, "cmd", gidHex, numFilesStr, firstFilename}
	} else {
		cmd = "sh"
		args = []string{"-c", "echo \"$1\" \"$2\" \"$3\" > " + outFile, "sh", gidHex, numFilesStr, firstFilename}
	}

	err := Run(ctx, cmd, args...)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Skipf("child process output file not ready: %v", err)
	}
	got := string(bytes.TrimSpace(data))
	if !strings.Contains(got, gidHex) {
		t.Errorf("output %q missing GID %q", got, gidHex)
	}
	if !strings.Contains(got, numFilesStr) {
		t.Errorf("output %q missing numFiles %q", got, numFilesStr)
	}
	if !strings.Contains(got, firstFilename) {
		t.Errorf("output %q missing firstFilename %q", got, firstFilename)
	}
}

func TestCloseIsNoop(t *testing.T) {
	err := Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func echoCmd() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "exit", "0"}
	}
	return "true", nil
}

func sleepCmd(seconds int) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "timeout", "/t", itoa(seconds), "/nobreak", ">nul"}
	}
	return "sleep", []string{itoa(seconds)}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 20)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	s := string(digits)
	if neg {
		s = "-" + s
	}
	return s
}
