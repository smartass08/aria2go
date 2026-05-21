package adrcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestScanCleanFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)
	writeFile(t, dir, "util.go", `package main

func add(a, b int) int {
	return a + b
}
`)

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d: %v", len(result.Violations), result.Violations)
	}
	if result.FilesScanned != 2 {
		t.Errorf("expected 2 files scanned, got %d", result.FilesScanned)
	}
}

func TestScanGPLHeader(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.go", `// GNU GENERAL PUBLIC LICENSE
package main
`)

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Violations) == 0 {
		t.Error("expected violations for GPL header")
		return
	}
	v := result.Violations[0]
	if v.Rule != "gpl-header" {
		t.Errorf("expected rule=gpl-header, got %s", v.Rule)
	}
	if v.File != filepath.Join(dir, "bad.go") {
		t.Errorf("expected file=bad.go, got %s", v.File)
	}
}

func TestScanGPLHeaderAlt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.go", `/*
 * GNU Lesser General Public License
 */
package main
`)

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Violations) == 0 {
		t.Error("expected violations for LGPL header")
	}
}

func TestScanGPLHeaderPermittedContext(t *testing.T) {
	dir := t.TempDir()
	// Reference to GPL in describing that something is GPL-licensed should be ok
	// (e.g., source-truth/README.md mentions LICENCE)
	writeFile(t, dir, "safe.go", `// This project is Apache-2.0; aria2 is GPL-licensed.
package main
`)

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations for GPL in permitted context, got %d: %v", len(result.Violations), result.Violations)
	}
}

func TestScanLicenseFingerprints(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.go", `// SPDX-License-Identifier: `+"GPL"+`-2.0-or-later
package main

const copiedNotice = "`+"This program is free software; "+"you can redistribute it and/or modify"+`"
`)

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	hasFingerprint := false
	for _, v := range result.Violations {
		if v.Rule == "license-fingerprint" {
			hasFingerprint = true
		}
	}
	if !hasFingerprint {
		t.Errorf("expected license-fingerprint violation, got %v", result.Violations)
	}
}

func TestScanAria2CopyrightFingerprint(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.go", `// Copyright `+"(C)"+` copied from `+"aria2"+` upstream
package main
`)

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	hasFingerprint := false
	for _, v := range result.Violations {
		if v.Rule == "license-fingerprint" {
			hasFingerprint = true
		}
	}
	if !hasFingerprint {
		t.Errorf("expected aria2 copyright fingerprint violation, got %v", result.Violations)
	}
}

func TestScanAria2Symbol(t *testing.T) {
	dir := t.TempDir()
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "DownloadEngine\nAbstractCommand\n")

	writeFile(t, dir, "bad.go", `package main

type DownloadEngine struct {}
`)

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	hasSymbol := false
	for _, v := range result.Violations {
		if v.Rule == "aria2-symbol" {
			hasSymbol = true
		}
	}
	if !hasSymbol {
		t.Errorf("expected aria2-symbol violation for DownloadEngine, got %v", result.Violations)
	}
}

func TestScanAria2SymbolInCommentPermitted(t *testing.T) {
	dir := t.TempDir()
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "DownloadEngine\n")

	writeFile(t, dir, "safe.go", `package main

// Scan checks for symbols such as DownloadEngine in non-comment code.
func Scan() {}
`)

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, v := range result.Violations {
		if v.Rule == "aria2-symbol" {
			t.Errorf("unexpected aria2-symbol violation in comment: %v", v)
		}
	}
}

func TestScanImportSourceTruth(t *testing.T) {
	dir := t.TempDir()
	srcTruthDir := filepath.Join(dir, "source-truth")
	if err := os.MkdirAll(srcTruthDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "")

	writeFile(t, dir, "bad.go", `package main

import _ "example.com/source-truth"
`)

	result, err := Scan(dir, "source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	hasImport := false
	for _, v := range result.Violations {
		if v.Rule == "import-source-truth" && v.Detail == "Import references source-truth or aria2/src/" {
			hasImport = true
		}
	}
	if !hasImport {
		violations := make([]string, len(result.Violations))
		for i, v := range result.Violations {
			violations[i] = fmt.Sprintf("%s:%s:%s", v.Rule, v.File, v.Detail)
		}
		t.Errorf("expected import-source-truth violation with 'Import references...', got %v", violations)
	}
}

func TestScanEmbedSourceTruth(t *testing.T) {
	dir := t.TempDir()
	srcTruthDir := filepath.Join(dir, "source-truth")
	if err := os.MkdirAll(srcTruthDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "")

	writeFile(t, dir, "bad.go", `package main

import "embed"

//go:embed source-truth/*
var src embed.FS
`)

	result, err := Scan(dir, "source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	hasEmbed := false
	for _, v := range result.Violations {
		if v.Rule == "import-source-truth" && strings.Contains(v.Detail, "go:embed") {
			hasEmbed = true
		}
	}
	if !hasEmbed {
		t.Errorf("expected go:embed violation, got %v", result.Violations)
	}
}

func TestScanNoFiles(t *testing.T) {
	dir := t.TempDir()
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "")

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.FilesScanned != 0 {
		t.Errorf("expected 0 files, got %d", result.FilesScanned)
	}
}

func TestScanSkipsNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "")

	writeFile(t, dir, "README.md", "DownloadEngine is a class in aria2.")
	writeFile(t, dir, "main.go", "package main\nfunc main() {}\n")

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.FilesScanned != 1 {
		t.Errorf("expected 1 .go file scanned, got %d", result.FilesScanned)
	}
}

func TestScanSkipsVendor(t *testing.T) {
	dir := t.TempDir()
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "")

	vendorDir := filepath.Join(dir, "vendor", "example")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, vendorDir, "bad.go", "package example\n// GNU GENERAL PUBLIC LICENSE\nfunc GPL() {}\n")
	writeFile(t, dir, "main.go", "package main\nfunc main() {}\n")

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Should only scan main.go, not vendor files
	if result.FilesScanned != 1 {
		t.Errorf("expected 1 file scanned (skipping vendor), got %d", result.FilesScanned)
	}
	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
}

func TestScanSkipsSourceTruthDir(t *testing.T) {
	dir := t.TempDir()
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "")

	stDir := filepath.Join(dir, "source-truth")
	if err := os.MkdirAll(stDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, stDir, "bad.go", "package bad\n// GNU GENERAL PUBLIC LICENSE\n")
	writeFile(t, dir, "main.go", "package main\nfunc main() {}\n")

	result, err := Scan(dir, "source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if result.FilesScanned != 1 {
		t.Errorf("expected 1 file scanned (skipping source-truth), got %d", result.FilesScanned)
	}
}

func TestScanMultipleViolations(t *testing.T) {
	dir := t.TempDir()
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "DownloadEngine\nAbstractCommand\n")

	writeFile(t, dir, "bad.go", "package main\n\ntype DownloadEngine struct {}\ntype AbstractCommand struct {}\n")
	writeFile(t, dir, "gpl.go", "// GNU GENERAL PUBLIC LICENSE\npackage main\n")

	result, err := Scan(dir, dir+"/source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Violations) < 2 {
		t.Errorf("expected at least 2 violations, got %d: %v", len(result.Violations), result.Violations)
	}
}

func TestScanNonCommentSourceTruthReference(t *testing.T) {
	dir := t.TempDir()
	symDir := filepath.Join(dir, "plans", "tools", "orchestrator", "adr-check")
	if err := os.MkdirAll(symDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, symDir, "aria2-symbols.txt", "")

	stDir := filepath.Join(dir, "source-truth")
	if err := os.MkdirAll(stDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_ = stDir

	writeFile(t, dir, "bad.go", "package main\n\nvar src = \"source-truth\"\n")

	result, err := Scan(dir, "source-truth")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	hasRef := false
	for _, v := range result.Violations {
		if v.Rule == "import-source-truth" && strings.Contains(v.Detail, "Non-comment") {
			hasRef = true
		}
	}
	if !hasRef {
		t.Errorf("expected non-comment source-truth reference violation, got %v", result.Violations)
	}
}
