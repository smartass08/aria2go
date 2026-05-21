package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/plans/tools/orchestrator/internal/adrcheck"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func writeSymbols(t *testing.T, dir string) {
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir symbols: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "DownloadEngine\nAbstractCommand\n")
}

func TestRunClean(t *testing.T) {
	dir := t.TempDir()
	writeSymbols(t, dir)
	writeFile(t, dir, "main.go", "package main\nfunc main() {}\n")

	exit := run([]string{"--scope", dir, "--source-truth", dir + "/source-truth"})
	if exit != 0 {
		t.Errorf("expected exit 0, got %d", exit)
	}
}

func TestRunViolations(t *testing.T) {
	dir := t.TempDir()
	writeSymbols(t, dir)
	writeFile(t, dir, "bad.go", "package main\n\ntype DownloadEngine struct {}\n")

	exit := run([]string{"--scope", dir, "--source-truth", dir + "/source-truth"})
	if exit != 1 {
		t.Errorf("expected exit 1 for violations, got %d", exit)
	}
}

func TestRunError(t *testing.T) {
	exit := run([]string{"--scope", "/nonexistent/path/does/not/exist", "--source-truth", "/also/nonexistent"})
	if exit != 2 {
		t.Errorf("expected exit 2 for error, got %d", exit)
	}
}

func TestRunJSONOutput(t *testing.T) {
	dir := t.TempDir()
	writeSymbols(t, dir)
	writeFile(t, dir, "bad.go", "package main\n\ntype DownloadEngine struct {}\n")

	// Capture stdout by redirecting.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exit := run([]string{"--scope", dir, "--source-truth", dir + "/source-truth", "--json"})

	w.Close()
	os.Stdout = oldStdout

	if exit != 1 {
		t.Errorf("expected exit 1, got %d", exit)
	}

	var result struct {
		Violations   []adrcheck.Violation `json:"violations"`
		FilesScanned int                  `json:"files_scanned"`
	}
	if err := json.NewDecoder(r).Decode(&result); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if len(result.Violations) == 0 {
		t.Error("expected violations in JSON output")
	}
	if result.FilesScanned == 0 {
		t.Error("expected files_scanned > 0")
	}
}

func TestRunStrictNoFiles(t *testing.T) {
	dir := t.TempDir()
	writeSymbols(t, dir)
	// Do not create any .go files.

	exit := run([]string{"--scope", dir, "--source-truth", dir + "/source-truth", "--strict"})
	if exit != 2 {
		t.Errorf("expected exit 2 for strict with no files, got %d", exit)
	}
}

func TestRunGPLViolation(t *testing.T) {
	dir := t.TempDir()
	writeSymbols(t, dir)
	writeFile(t, dir, "bad.go", "// GNU GENERAL PUBLIC LICENSE\npackage main\n")

	exit := run([]string{"--scope", dir, "--source-truth", dir + "/source-truth"})
	if exit != 1 {
		t.Errorf("expected exit 1 for GPL violation, got %d", exit)
	}
}

func TestRunImportSourceTruth(t *testing.T) {
	dir := t.TempDir()
	writeSymbols(t, dir)
	writeFile(t, dir, "bad.go", "package main\nimport _ \"example.com/source-truth\"\n")

	exit := run([]string{"--scope", dir, "--source-truth", "source-truth"})
	if exit != 1 {
		t.Errorf("expected exit 1 for source-truth import, got %d", exit)
	}
}

func TestHumanOutputFormat(t *testing.T) {
	dir := t.TempDir()
	writeSymbols(t, dir)
	writeFile(t, dir, "bad.go", "// GNU GENERAL PUBLIC LICENSE\npackage main\n")

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	exit := run([]string{"--scope", dir, "--source-truth", dir + "/source-truth"})

	w.Close()
	os.Stdout = oldStdout

	if exit != 1 {
		t.Errorf("expected exit 1, got %d", exit)
	}

	var output strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	out := output.String()
	if !strings.Contains(out, "ADRNNN:") {
		t.Errorf("expected 'ADRNNN:' prefix in human output, got: %s", out)
	}
	if !strings.Contains(out, "rule=gpl-header") {
		t.Errorf("expected 'rule=gpl-header' in output, got: %s", out)
	}
}

func TestRunDefaultFlags(t *testing.T) {
	// Test with explicit flags pointing to a clean tree.
	dir := t.TempDir()
	writeSymbols(t, dir)
	writeFile(t, dir, "main.go", "package main\nfunc main() {}\n")

	exit := run([]string{"--scope", dir, "--source-truth", dir + "/source-truth"})
	if exit != 0 {
		t.Errorf("expected exit 0 for clean tree, got %d", exit)
	}
}
