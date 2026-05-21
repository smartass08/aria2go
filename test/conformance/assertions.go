package conformance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// RunResultAssertions controls stable ref-vs-impl process output comparison.
type RunResultAssertions struct {
	ExitCode bool
	Stdout   bool
	Stderr   bool
}

// AssertRunResultParity compares selected stable fields of paired command output.
func AssertRunResultParity(t *testing.T, label string, ref, impl RunResult, opts RunResultAssertions) {
	t.Helper()

	if opts.ExitCode && ref.ExitCode != impl.ExitCode {
		t.Errorf("%s exit code mismatch: ref=%d impl=%d", label, ref.ExitCode, impl.ExitCode)
	}
	if opts.Stdout {
		assertStableTextEqual(t, label+" stdout", ref.Stdout, impl.Stdout)
	}
	if opts.Stderr {
		assertStableTextEqual(t, label+" stderr", ref.Stderr, impl.Stderr)
	}
}

// AssertStableStdout compares normalized stdout from paired command output.
func AssertStableStdout(t *testing.T, label string, ref, impl RunResult) {
	t.Helper()
	assertStableTextEqual(t, label+" stdout", ref.Stdout, impl.Stdout)
}

// AssertStableStderr compares normalized stderr from paired command output.
func AssertStableStderr(t *testing.T, label string, ref, impl RunResult) {
	t.Helper()
	assertStableTextEqual(t, label+" stderr", ref.Stderr, impl.Stderr)
}

// AssertFileBytes requires path to contain want exactly.
func AssertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s content mismatch: got %d bytes want %d bytes", path, len(got), len(want))
	}
}

// AssertFilesEqual requires two files to have identical bytes.
func AssertFilesEqual(t *testing.T, label string, refPath, implPath string) {
	t.Helper()

	ref, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("%s read ref file %s: %v", label, refPath, err)
	}
	impl, err := os.ReadFile(implPath)
	if err != nil {
		t.Fatalf("%s read impl file %s: %v", label, implPath, err)
	}
	if !bytes.Equal(ref, impl) {
		t.Fatalf("%s file mismatch: ref=%s (%d bytes) impl=%s (%d bytes)", label, refPath, len(ref), implPath, len(impl))
	}
}

// AssertFileSHA256 requires path to have the expected lowercase SHA-256 hex digest.
func AssertFileSHA256(t *testing.T, path string, wantHex string) {
	t.Helper()

	got, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("sha256 %s: %v", path, err)
	}
	if got != strings.ToLower(wantHex) {
		t.Fatalf("%s sha256 mismatch: got %s want %s", path, got, strings.ToLower(wantHex))
	}
}

// FileSHA256 returns the lowercase SHA-256 hex digest for path.
func FileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// AssertDirectoryEntries requires dir to contain exactly the named entries.
func AssertDirectoryEntries(t *testing.T, dir string, want []string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	sort.Strings(got)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("%s entries mismatch: got %#v want %#v", dir, got, want)
	}
}

// AssertNoFile requires path not to exist.
func AssertNoFile(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists, want absent", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

// TempOutputPath returns a deterministic output path under t.TempDir.
func TempOutputPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func assertStableTextEqual(t *testing.T, label string, ref string, impl string) {
	t.Helper()

	refNorm := normalizeOutput(ref)
	implNorm := normalizeOutput(impl)
	if refNorm != implNorm {
		t.Errorf("%s mismatch:\n--- ref\n+++ impl\n%s", label, diff(refNorm, implNorm))
	}
}
