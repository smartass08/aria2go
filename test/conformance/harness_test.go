package conformance

import (
	"strings"
	"testing"
)

func TestRunImplVersion(t *testing.T) {
	SkipIfNoRef(t)

	result, err := RunImpl(t, []string{"--version"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "aria2 version 1.37.0") {
		t.Error("stdout should contain version string")
	}
	if !strings.Contains(result.Stdout, "Copyright") {
		t.Error("stdout should contain copyright")
	}
}

func TestRunImplVersionWithoutReference(t *testing.T) {
	result, err := RunImpl(t, []string{"--version"}, "")
	if err != nil {
		t.Fatalf("RunImpl: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code %d, stderr=%s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "aria2 version 1.37.0") {
		t.Fatalf("stdout missing aria2go compatibility version: %s", result.Stdout)
	}
}

func TestRunImplRPCReadyWithoutReference(t *testing.T) {
	port := findFreePort(t)
	srv := startRPCImpl(t, port)
	defer srv.Stop(t)
	srv.WaitReady(t)
}

func TestRunRefAvailableSmoke(t *testing.T) {
	SkipIfNoRef(t)

	// If we get here, a reference binary is available.  Run a no-op smoke
	// probe to confirm it launches.
	result, err := RunRef(t, []string{"--version"}, "")
	if err != nil {
		t.Fatalf("RunRef: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ref exit code %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "aria2 version") {
		t.Error("ref stdout should contain aria2 version line")
	}
}

func TestNormalizeOutput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "no change",
			input: "hello\nworld\n",
			want:  "hello\nworld\n",
		},
		{
			name:  "strip trailing whitespace",
			input: "hello   \nworld\t\n",
			want:  "hello\nworld\n",
		},
		{
			name:  "convert crlf",
			input: "hello\r\nworld\r\n",
			want:  "hello\nworld\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeOutput(tt.input)
			if got != tt.want {
				t.Errorf("normalizeOutput(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAssertEqualExit(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		ref := RunResult{ExitCode: 1}
		impl := RunResult{ExitCode: 1}
		AssertEqualExit(t, ref, impl)
	})
	// Mismatch case is tested by AssertEqualExit calling t.Errorf internally.
}

func TestProjectRoot(t *testing.T) {
	root, err := projectRoot()
	if err != nil {
		t.Fatalf("projectRoot: %v", err)
	}
	if root == "" {
		t.Error("project root should not be empty")
	}
	t.Logf("project root: %s", root)
}
