package conformance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
