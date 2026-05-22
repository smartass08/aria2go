package conformance

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCLI_VersionOutput(t *testing.T) {
	SkipIfNoRef(t)

	ref, err := RunRef(t, []string{"--version"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--version"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)

	if !strings.Contains(ref.Stdout, "aria2 version") {
		t.Error("ref version output missing 'aria2 version'")
	}
	if !strings.Contains(impl.Stdout, "aria2 version") {
		t.Error("impl version output missing 'aria2 version'")
	}

	if !strings.Contains(strings.ToLower(ref.Stdout), "copyright") &&
		!strings.Contains(strings.ToLower(ref.Stdout), "license") &&
		!strings.Contains(strings.ToLower(ref.Stdout), "gnu") {
		t.Error("ref version output missing copyright/license info")
	}
	if !strings.Contains(strings.ToLower(impl.Stdout), "copyright") &&
		!strings.Contains(strings.ToLower(impl.Stdout), "license") &&
		!strings.Contains(strings.ToLower(impl.Stdout), "gnu") {
		t.Error("impl version output missing copyright/license info")
	}

	if !strings.Contains(ref.Stdout, "1.37.0") {
		t.Error("ref version does not contain 1.37.0")
	}
	if !strings.Contains(impl.Stdout, "1.37.0") {
		t.Error("impl version does not contain 1.37.0")
	}
}

func TestCLI_HelpAllOutput(t *testing.T) {
	SkipIfNoRef(t)

	ref, err := RunRef(t, []string{"--help=#all"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--help=#all"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)

	flagRE := regexp.MustCompile(`(--[a-zA-Z][a-zA-Z0-9_-]*(?:\[[^\]]*\])?)`)
	refFlags := canonicalFlags(flagRE.FindAllString(ref.Stdout, -1))
	implFlags := canonicalFlags(flagRE.FindAllString(impl.Stdout, -1))
	refFlags, implFlags = normalizeBuildDependentHelpFlags(refFlags, implFlags)

	compareStringSet(t, "--help=#all flags", refFlags, implFlags)

	t.Logf("ref flags: %d, impl flags: %d", len(refFlags), len(implFlags))
}

func TestCLI_HelpBasicOutput(t *testing.T) {
	SkipIfNoRef(t)

	ref, err := RunRef(t, []string{"--help"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--help"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)

	if !strings.Contains(ref.Stdout, "Options") {
		t.Error("ref basic help missing 'Options' heading")
	}
	if !strings.Contains(impl.Stdout, "Options") {
		t.Error("impl basic help missing 'Options' heading")
	}

	for _, flag := range []string{"--dir", "--max-concurrent-downloads", "--split", "--continue"} {
		if !strings.Contains(ref.Stdout, flag) {
			t.Errorf("ref basic help missing flag %q", flag)
		}
		if !strings.Contains(impl.Stdout, flag) {
			t.Errorf("impl basic help missing flag %q", flag)
		}
	}
}

func TestCLI_HelpShortFlag(t *testing.T) {
	SkipIfNoRef(t)

	ref, err := RunRef(t, []string{"-h"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"-h"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)

	if !strings.Contains(ref.Stdout, "Usage:") {
		t.Error("ref -h output missing 'Usage:'")
	}
	if !strings.Contains(impl.Stdout, "Usage:") {
		t.Error("impl -h output missing 'Usage:'")
	}
}

func TestCLI_HelpTags(t *testing.T) {
	SkipIfNoRef(t)

	tags := []string{"#basic", "#advanced", "#http", "#ftp", "#bittorrent", "#rpc", "#help"}

	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			ref, err := RunRef(t, []string{"--help=" + tag}, "")
			if err != nil {
				t.Fatalf("RunRef %s: %v", tag, err)
			}
			impl, err := RunImpl(t, []string{"--help=" + tag}, "")
			if err != nil {
				t.Fatalf("RunImpl %s: %v", tag, err)
			}

			AssertEqualExit(t, ref, impl)

			if !strings.Contains(ref.Stdout, "Options:") && !strings.Contains(ref.Stdout, "Available tags") {
				t.Logf("ref %s output: %s", tag, strings.TrimSpace(ref.Stdout))
			}
		})
	}
}

func TestCLI_ConfPathWithNonexistentConfig(t *testing.T) {
	SkipIfNoRef(t)

	nonexistent := "/tmp/aria2-nonexistent-config-test-" + t.Name() + ".conf"

	ref, err := RunRef(t, []string{"--conf-path=" + nonexistent}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--conf-path=" + nonexistent}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)

	if ref.ExitCode == 0 {
		t.Error("ref should have non-zero exit for nonexistent --conf-path")
	}
}

func TestCLI_NoConfFlag(t *testing.T) {
	SkipIfNoRef(t)

	ref, err := RunRef(t, []string{"--no-conf", "--version"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--no-conf", "--version"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)
	if !strings.Contains(ref.Stdout, "aria2 version") {
		t.Error("ref --no-conf --version missing version info")
	}
	if !strings.Contains(impl.Stdout, "aria2 version") {
		t.Error("impl --no-conf --version missing version info")
	}
}

func TestCLI_NoDownloadsError(t *testing.T) {
	SkipIfNoRef(t)

	ref, err := RunRef(t, []string{"--dir=/tmp"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--dir=/tmp"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	if ref.ExitCode == 0 {
		t.Error("ref should have non-zero exit when no downloads and no RPC")
	}
	if impl.ExitCode == 0 {
		t.Error("impl should have non-zero exit when no downloads and no RPC")
	}
	AssertEqualExit(t, ref, impl)

	combined := strings.ToLower(ref.Stderr + ref.Stdout)
	if !strings.Contains(combined, "url") &&
		!strings.Contains(combined, "specify") &&
		!strings.Contains(combined, "usage") {
		t.Logf("ref error output: %s%s", ref.Stderr, ref.Stdout)
	}
}

func TestCLI_InvalidOptionExitCode(t *testing.T) {
	SkipIfNoRef(t)

	ref, err := RunRef(t, []string{"--nonexistent-flag-xyzzy"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--nonexistent-flag-xyzzy"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	if ref.ExitCode == 0 {
		t.Error("ref should have non-zero exit for invalid option")
	}
	if impl.ExitCode == 0 {
		t.Error("impl should have non-zero exit for invalid option")
	}
	AssertEqualExit(t, ref, impl)
}

func TestCLI_ConfPathWithValidConfig(t *testing.T) {
	SkipIfNoRef(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "aria2.conf")
	configContent := "max-concurrent-downloads=3\nsplit=5\n"
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ref, err := RunRef(t, []string{"--conf-path=" + confPath, "--version"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"--conf-path=" + confPath, "--version"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)
	if !strings.Contains(ref.Stdout, "aria2 version") {
		t.Error("ref should display version even with valid config")
	}
	if !strings.Contains(impl.Stdout, "aria2 version") {
		t.Error("impl should display version even with valid config")
	}
}

func TestCLI_ShortVersionFlag(t *testing.T) {
	SkipIfNoRef(t)

	ref, err := RunRef(t, []string{"-v"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	impl, err := RunImpl(t, []string{"-v"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}

	AssertEqualExit(t, ref, impl)
	if !strings.Contains(ref.Stdout, "aria2 version") {
		t.Error("ref -v missing version")
	}
	if !strings.Contains(impl.Stdout, "aria2 version") {
		t.Error("impl -v missing version")
	}
}

func TestCLI_StopTimerHaltsSlowDownload(t *testing.T) {
	SkipIfNoRef(t)
	requireRefHelpOptions(t, "stop", "dir", "out")

	srv := newBlockingDownloadServer(t)
	refDir, implDir := t.TempDir(), t.TempDir()

	ref, refElapsed := runLifecycleCLIProbe(t, true, append(stopDownloadArgs(refDir, "stop.bin"),
		"--stop=1",
		srv.URL+"/stop.bin",
	))
	impl, implElapsed := runLifecycleCLIProbe(t, false, append(stopDownloadArgs(implDir, "stop.bin"),
		"--stop=1",
		srv.URL+"/stop.bin",
	))

	AssertEqualExit(t, ref, impl)
	requireLifecycleInProgressExit(t, "ref --stop", ref)
	requireLifecycleInProgressExit(t, "impl --stop", impl)
	requireLifecycleElapsed(t, "ref --stop", refElapsed, 700*time.Millisecond, 5*time.Second)
	requireLifecycleElapsed(t, "impl --stop", implElapsed, 700*time.Millisecond, 5*time.Second)
	t.Logf("stop elapsed: ref=%s impl=%s exit(ref=%d impl=%d)", refElapsed, implElapsed, ref.ExitCode, impl.ExitCode)
}

func TestCLI_StopWithProcessHaltsAfterHelperExit(t *testing.T) {
	SkipIfNoRef(t)
	if runtime.GOOS == "windows" {
		t.Skip("helper process probe uses POSIX shell")
	}
	requireRefHelpOptions(t, "stop-with-process", "dir", "out")

	srv := newBlockingDownloadServer(t)
	refDir, implDir := t.TempDir(), t.TempDir()

	ref, refElapsed := runStopWithProcessProbe(t, true, srv.URL+"/watch.bin", refDir, "watch.bin")
	impl, implElapsed := runStopWithProcessProbe(t, false, srv.URL+"/watch.bin", implDir, "watch.bin")

	AssertEqualExit(t, ref, impl)
	requireLifecycleInProgressExit(t, "ref --stop-with-process", ref)
	requireLifecycleInProgressExit(t, "impl --stop-with-process", impl)
	requireLifecycleElapsed(t, "ref --stop-with-process", refElapsed, time.Second, 8*time.Second)
	requireLifecycleElapsed(t, "impl --stop-with-process", implElapsed, time.Second, 8*time.Second)
	t.Logf("stop-with-process elapsed: ref=%s impl=%s exit(ref=%d impl=%d)", refElapsed, implElapsed, ref.ExitCode, impl.ExitCode)
}

func uniqueMatches(matches []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			result = append(result, m)
		}
	}
	return result
}

func canonicalFlags(matches []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		if idx := strings.IndexByte(m, '['); idx >= 0 {
			m = m[:idx]
		}
		if strings.HasSuffix(m, "-") {
			continue
		}
		if !seen[m] {
			seen[m] = true
			result = append(result, m)
		}
	}
	return result
}

func normalizeBuildDependentHelpFlags(ref, impl []string) ([]string, []string) {
	return removeMismatchedString(ref, impl, "--rlimit-nofile")
}

func removeMismatchedString(ref, impl []string, value string) ([]string, []string) {
	refHas := stringSliceContains(ref, value)
	implHas := stringSliceContains(impl, value)
	if refHas == implHas {
		return ref, impl
	}
	return removeString(ref, value), removeString(impl, value)
}

func stringSliceContains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func removeString(values []string, value string) []string {
	out := values[:0]
	for _, v := range values {
		if v != value {
			out = append(out, v)
		}
	}
	return out
}

func stopDownloadArgs(dir, out string) []string {
	return []string{
		"--dir=" + dir,
		"--out=" + out,
		"--allow-overwrite=true",
		"--auto-file-renaming=false",
		"--file-allocation=none",
		"--max-connection-per-server=1",
		"--split=1",
		"--quiet=true",
		"--show-console-readout=false",
		"--summary-interval=0",
		"--enable-dht=false",
		"--enable-dht6=false",
	}
}

func runLifecycleCLIProbe(t *testing.T, ref bool, args []string) (RunResult, time.Duration) {
	t.Helper()

	start := time.Now()
	var (
		result RunResult
		err    error
	)
	opts := RunOptions{Timeout: 12 * time.Second}
	if ref {
		result, err = RunRefWithOptions(t, args, "", opts)
	} else {
		result, err = RunImplWithOptions(t, args, "", opts)
	}
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("run lifecycle probe ref=%v: %v\nargs=%v\nstdout=%s\nstderr=%s", ref, err, args, result.Stdout, result.Stderr)
	}
	return result, elapsed
}

func runStopWithProcessProbe(t *testing.T, ref bool, url, dir, out string) (RunResult, time.Duration) {
	t.Helper()

	helper := startSleepHelper(t, 2*time.Second)
	args := append(stopDownloadArgs(dir, out),
		fmt.Sprintf("--stop-with-process=%d", helper.cmd.Process.Pid),
		url,
	)
	result, elapsed := runLifecycleCLIProbe(t, ref, args)
	if err := <-helper.done; err != nil {
		t.Fatalf("wait helper process: %v", err)
	}
	return result, elapsed
}

type sleepHelper struct {
	cmd  *exec.Cmd
	done chan error
}

func startSleepHelper(t *testing.T, sleepFor time.Duration) *sleepHelper {
	t.Helper()

	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell not available for helper process")
	}
	cmd := exec.Command(shPath, "-c", fmt.Sprintf("sleep %d", int(sleepFor/time.Second)))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	helper := &sleepHelper{
		cmd:  cmd,
		done: make(chan error, 1),
	}
	go func() {
		helper.done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			_ = cmd.Process.Kill()
			<-helper.done
		}
	})
	return helper
}

func requireLifecycleElapsed(t *testing.T, label string, elapsed, min, max time.Duration) {
	t.Helper()
	if elapsed < min || elapsed > max {
		t.Fatalf("%s elapsed=%s want between %s and %s", label, elapsed, min, max)
	}
}

func requireLifecycleInProgressExit(t *testing.T, label string, result RunResult) {
	t.Helper()
	if result.ExitCode != 7 {
		t.Fatalf("%s exit=%d, want 7\nstdout=%s\nstderr=%s", label, result.ExitCode, result.Stdout, result.Stderr)
	}
}
