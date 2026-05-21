package conformance

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStableFileAssertions(t *testing.T) {
	dir := t.TempDir()
	refPath := filepath.Join(dir, "ref.bin")
	implPath := filepath.Join(dir, "impl.bin")
	payload := []byte("abc")

	if err := os.WriteFile(refPath, payload, 0o644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	if err := os.WriteFile(implPath, payload, 0o644); err != nil {
		t.Fatalf("write impl: %v", err)
	}

	AssertFileBytes(t, refPath, payload)
	AssertFilesEqual(t, "same payload", refPath, implPath)
	AssertFileSHA256(t, refPath, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
	AssertDirectoryEntries(t, dir, []string{"impl.bin", "ref.bin"})
	AssertNoFile(t, filepath.Join(dir, "missing.bin"))
}

func TestRunResultParitySuccess(t *testing.T) {
	ref := RunResult{Stdout: "alpha  \r\nbeta\n", Stderr: "warn\t\n", ExitCode: 3}
	impl := RunResult{Stdout: "alpha\nbeta\n", Stderr: "warn\n", ExitCode: 3}

	AssertRunResultParity(t, "normalized", ref, impl, RunResultAssertions{
		ExitCode: true,
		Stdout:   true,
		Stderr:   true,
	})
	AssertStableStdout(t, "stdout", ref, impl)
	AssertStableStderr(t, "stderr", ref, impl)
}

func TestCommandMatrixCaseTargetOverrides(t *testing.T) {
	refDir := filepath.Join(t.TempDir(), "ref")
	implDir := filepath.Join(t.TempDir(), "impl")
	tc := CommandMatrixCase{
		Name: "override",
		Args: []string{"shared"},
		ArgsFor: func(target RunnerTarget) []string {
			return []string{string(target), "arg"}
		},
		DirFor: func(target RunnerTarget) string {
			if target == RunnerRef {
				return refDir
			}
			return implDir
		},
		Env:     []string{"A=B"},
		Timeout: time.Second,
	}

	if got := tc.testName(); got != "override" {
		t.Fatalf("testName got %q", got)
	}
	if got := tc.argsFor(RunnerRef); !reflect.DeepEqual(got, []string{"ref", "arg"}) {
		t.Fatalf("ref args got %#v", got)
	}
	if got := tc.argsFor(RunnerImpl); !reflect.DeepEqual(got, []string{"impl", "arg"}) {
		t.Fatalf("impl args got %#v", got)
	}
	if got := tc.dirFor(RunnerRef); got != refDir {
		t.Fatalf("ref dir got %q want %q", got, refDir)
	}
	if got := tc.dirFor(RunnerImpl); got != implDir {
		t.Fatalf("impl dir got %q want %q", got, implDir)
	}

	opts := RunOptions{
		Env:     append([]string(nil), tc.Env...),
		Dir:     tc.dirFor(RunnerRef),
		Timeout: tc.Timeout,
	}
	if !reflect.DeepEqual(opts.Env, []string{"A=B"}) || opts.Dir != refDir || opts.Timeout != time.Second {
		t.Fatalf("derived options got %#v", opts)
	}
}
