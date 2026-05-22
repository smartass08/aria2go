package runner

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/smartass08/aria2go/bench/internal/types"
)

func TestNewRunner(t *testing.T) {
	r, err := New(Config{
		Aria2cPath:     "/usr/bin/aria2c",
		Aria2goPath:    "/usr/bin/aria2go",
		OutputDir:      "/tmp",
		TmpDir:         "/tmp",
		RAMDiskSizeMB:  1024,
		SampleInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("nil runner")
	}
}

func TestRunWithMetricsForceKillsProcessAfterDuration(t *testing.T) {
	if dispatchRunnerTestHelper() {
		return
	}

	r := &Runner{cfg: Config{
		Duration:       50 * time.Millisecond,
		SampleInterval: 10 * time.Millisecond,
		StopTimeout:    100 * time.Millisecond,
	}}
	args := []string{"-test.run=TestRunWithMetricsForceKillsProcessAfterDuration", "--", "ignore-interrupt-helper"}

	started := time.Now()
	_, err := runWithMetrics(context.Background(), r, types.BinaryAria2go, os.Args[0], args, nil)
	if err != nil {
		t.Fatalf("runWithMetrics returned error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 750*time.Millisecond {
		t.Fatalf("runWithMetrics took %s; child ignored SIGINT and was not force-killed", elapsed)
	}
}

func TestRunWithMetricsKillsProcessGroupAfterDuration(t *testing.T) {
	if dispatchRunnerTestHelper() {
		return
	}

	pidPath := filepath.Join(t.TempDir(), "grandchild.pid")
	t.Setenv("RUNNER_GRANDCHILD_PID_FILE", pidPath)

	r := &Runner{cfg: Config{
		Duration:       50 * time.Millisecond,
		SampleInterval: 10 * time.Millisecond,
		StopTimeout:    100 * time.Millisecond,
	}}
	args := []string{"-test.run=TestRunWithMetricsKillsProcessGroupAfterDuration", "--", "spawn-grandchild-helper"}

	_, err := runWithMetrics(context.Background(), r, types.BinaryAria2go, os.Args[0], args, nil)
	if err != nil {
		t.Fatalf("runWithMetrics returned error: %v", err)
	}

	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read grandchild pid: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("parse grandchild pid %q: %v", data, err)
	}
	if processAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("grandchild pid %d survived runner shutdown", pid)
	}
}

func dispatchRunnerTestHelper() bool {
	if len(os.Args) < 2 {
		return false
	}
	switch os.Args[len(os.Args)-1] {
	case "ignore-interrupt-helper":
		runIgnoreInterruptHelper()
		return true
	case "spawn-grandchild-helper":
		runSpawnGrandchildHelper()
		return true
	default:
		return false
	}
}

func runIgnoreInterruptHelper() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-sigCh:
			// Intentionally ignore graceful shutdown to exercise the runner's hard kill path.
		case <-deadline.C:
			return
		}
	}
}

func runSpawnGrandchildHelper() {
	pidPath := os.Getenv("RUNNER_GRANDCHILD_PID_FILE")
	cmd := exec.Command(os.Args[0], "-test.run=TestRunWithMetricsKillsProcessGroupAfterDuration", "--", "ignore-interrupt-helper")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		_ = cmd.Process.Kill()
		os.Exit(2)
	}
	runIgnoreInterruptHelper()
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
