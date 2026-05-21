// Package adrcheck implements the ARR-0016 source-truth contamination scanner.
//
// It checks Go source code for prohibited GPL content leakage from the
// source-truth/aria2/ directory.
package adrcheck

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Result holds the outcome of a scan.
type Result struct {
	Violations   []Violation
	FilesScanned int
}

// Violation represents a single contamination finding.
type Violation struct {
	File   string
	Line   int
	Rule   string // "gpl-header", "license-fingerprint", "aria2-symbol", "import-source-truth", "comment-fingerprint"
	Detail string
}

// Scan runs all contamination heuristics against Go code under root,
// using symbols loaded from the symbols text file and checking against
// the source-truth directory at sourceTruth.
func Scan(root string, sourceTruth string) (*Result, error) {
	var symbols []string
	if s, err := loadSymbols(filepath.Join(root, "plans", "tools", "orchestrator", "adr-check", "aria2-symbols.txt")); err == nil {
		symbols = s
	} else {
		// Try alternative paths; if none work, continue without symbols scan.
		symbolsPath := filepath.Join(sourceTruth, "..", "plans", "tools", "orchestrator", "adr-check", "aria2-symbols.txt")
		if s, e2 := loadSymbols(symbolsPath); e2 == nil {
			symbols = s
		}
	}

	result := &Result{}
	if err := walkGoFiles(root, result, symbols, sourceTruth); err != nil {
		return nil, err
	}
	return result, nil
}

func walkGoFiles(root string, result *Result, symbols []string, sourceTruth string) error {
	var goFiles []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			// Skip source-truth, vendor, .git
			if base == "source-truth" || base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			goFiles = append(goFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking tree: %w", err)
	}

	result.FilesScanned = len(goFiles)

	for _, path := range goFiles {
		if err := scanFile(path, result, symbols, sourceTruth); err != nil {
			return fmt.Errorf("scanning %s: %w", path, err)
		}
	}
	return nil
}

func scanFile(path string, result *Result, symbols []string, sourceTruth string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(content), "\n")

	if isScannerPackageFile(path) {
		return nil
	}

	// Heuristic 1: GPL header and distinctive license fingerprint scans
	scanForGPL(path, lines, result)
	scanForLicenseFingerprints(path, lines, result)

	// Heuristic 2: Distinctive aria2 symbol scan
	scanForSymbols(path, lines, result, symbols)

	// Heuristic 3: Check imports for source-truth
	scanImportsForSourceTruth(path, lines, result, sourceTruth)

	// Heuristic 4: Check go:embed directives
	scanForEmbedSourceTruth(path, lines, result, sourceTruth)

	// Heuristic 5: Check source-truth directory patterns
	scanForSourceTruthReference(path, lines, result, sourceTruth)

	return nil
}

func isScannerPackageFile(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return strings.Contains(clean, "/plans/tools/orchestrator/adr-check/") ||
		strings.Contains(clean, "/plans/tools/orchestrator/internal/adrcheck/") ||
		strings.HasPrefix(clean, "plans/tools/orchestrator/adr-check/") ||
		strings.HasPrefix(clean, "plans/tools/orchestrator/internal/adrcheck/")
}

var gplPatterns = []string{
	"GNU GENERAL PUBLIC LICENSE",
	"General Public License",
	"GNU GPL",
	"GNU Lesser General Public License",
	"GNU LGPL",
}

var licenseFingerprintPatterns = []string{
	"SPDX-License-Identifier: " + "GPL",
	"SPDX-License-Identifier: " + "LGPL",
	"This program is free software; " + "you can redistribute it and/or modify",
	"either version 2 of the License, " + "or (at your option) any later version",
	"WITHOUT ANY WARRANTY; " + "without even the implied warranty of MERCHANTABILITY",
	"Copyright (C) 2006, 2019 " + "Tatsuhiro Tsujikawa",
	"Copyright (C) 2006, 2023 " + "Tatsuhiro Tsujikawa",
}

func scanForGPL(path string, lines []string, result *Result) {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") && !strings.HasPrefix(trimmed, "*") {
			continue
		}
		upper := strings.ToUpper(trimmed)
		for _, pat := range gplPatterns {
			if strings.Contains(upper, strings.ToUpper(pat)) {
				// Allow GPL mentions in license files and documentation
				if strings.Contains(strings.ToLower(trimmed), "gpl-licensed") ||
					strings.Contains(strings.ToLower(trimmed), "apache-2.0") ||
					strings.Contains(strings.ToLower(trimmed), "gpl'd") ||
					strings.Contains(strings.ToLower(trimmed), "licensed gpl") {
					continue
				}
				result.Violations = append(result.Violations, Violation{
					File:   path,
					Line:   i + 1,
					Rule:   "gpl-header",
					Detail: fmt.Sprintf("GPL header string found: %q", pat),
				})
				return
			}
		}
	}
}

func scanForLicenseFingerprints(path string, lines []string, result *Result) {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "gpl-licensed") ||
			strings.Contains(lower, "apache-2.0") ||
			strings.Contains(lower, "gpl'd") ||
			strings.Contains(lower, "licensed gpl") {
			continue
		}
		for _, pat := range licenseFingerprintPatterns {
			if strings.Contains(trimmed, pat) {
				result.Violations = append(result.Violations, Violation{
					File:   path,
					Line:   i + 1,
					Rule:   "license-fingerprint",
					Detail: fmt.Sprintf("Prohibited license/source fingerprint found: %q", pat),
				})
			}
		}
		if strings.Contains(trimmed, "Copyright "+"(C)") &&
			strings.Contains(strings.ToLower(trimmed), "aria"+"2") {
			result.Violations = append(result.Violations, Violation{
				File:   path,
				Line:   i + 1,
				Rule:   "license-fingerprint",
				Detail: "aria2 copyright fingerprint found",
			})
		}
	}
}

func scanForSymbols(path string, lines []string, result *Result, symbols []string) {
	for _, sym := range symbols {
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(sym) + `\b`)
		for i, line := range lines {
			// Skip comments — symbols in comments about the scanner itself are expected
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			if pattern.MatchString(line) {
				result.Violations = append(result.Violations, Violation{
					File:   path,
					Line:   i + 1,
					Rule:   "aria2-symbol",
					Detail: fmt.Sprintf("Distinctive aria2 symbol found: %s", sym),
				})
			}
		}
	}
}

func scanImportsForSourceTruth(path string, lines []string, result *Result, sourceTruth string) {
	sourceTruthBase := filepath.Base(sourceTruth)
	importPattern := regexp.MustCompile(`"(.*` + regexp.QuoteMeta(sourceTruthBase) + `.*)"`)
	aria2SrcPattern := regexp.MustCompile(`"(.*aria2/src/.*)"`)
	for i, line := range lines {
		if importPattern.MatchString(line) || aria2SrcPattern.MatchString(line) {
			result.Violations = append(result.Violations, Violation{
				File:   path,
				Line:   i + 1,
				Rule:   "import-source-truth",
				Detail: "Import references source-truth or aria2/src/",
			})
		}
	}
}

func scanForEmbedSourceTruth(path string, lines []string, result *Result, sourceTruth string) {
	sourceTruthBase := filepath.Base(sourceTruth)
	for i, line := range lines {
		if strings.Contains(line, "go:embed") &&
			(strings.Contains(line, sourceTruthBase) || strings.Contains(line, "aria2/src")) {
			result.Violations = append(result.Violations, Violation{
				File:   path,
				Line:   i + 1,
				Rule:   "import-source-truth",
				Detail: "go:embed directive references source-truth",
			})
		}
	}
}

func scanForSourceTruthReference(path string, lines []string, result *Result, sourceTruth string) {
	sourceTruthBase := filepath.Base(sourceTruth)
	inMulti := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "/*") {
			inMulti = true
		}
		if inMulti && strings.Contains(trimmed, "*/") {
			inMulti = false
			continue
		}
		if inMulti {
			continue
		}
		if strings.Contains(trimmed, sourceTruthBase) {
			// Only flag if it looks like a reference to be used at build time
			// (not in a comment explaining the scanner)
			if !strings.HasPrefix(trimmed, "//") {
				result.Violations = append(result.Violations, Violation{
					File:   path,
					Line:   i + 1,
					Rule:   "import-source-truth",
					Detail: fmt.Sprintf("Non-comment reference to %s", sourceTruthBase),
				})
			}
		}
	}
}

func loadSymbols(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var symbols []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		symbols = append(symbols, line)
	}
	return symbols, scanner.Err()
}
