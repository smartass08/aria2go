// adr-check runs the ADR-0016 source-truth contamination scanner.
//
// It walks all Go source files under a scope directory and checks for
// prohibited GPL content leakage from the source-truth/aria2/ directory.
//
// Usage:
//
//	adr-check [flags]
//
// Flags:
//
//	--source-truth  path to source-truth directory (default: "source-truth")
//	--scope         path to Go code root to scan (default: ".")
//	--strict        fail on warnings too
//	--json          output as JSON
//
// Exit codes:
//
//	0: clean — no violations found
//	1: violations found
//	2: scanner error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/smartass08/aria2go/plans/tools/orchestrator/internal/adrcheck"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("adr-check", flag.ContinueOnError)
	sourceTruth := fs.String("source-truth", "source-truth", "Path to source-truth directory")
	scope := fs.String("scope", ".", "Path to Go code root to scan")
	strict := fs.Bool("strict", false, "Fail on warnings too")
	jsonOut := fs.Bool("json", false, "Output as JSON")
	_ = fs.Parse(args)

	result, err := adrcheck.Scan(*scope, *sourceTruth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "adr-check: %v\n", err)
		return 2
	}

	if *jsonOut {
		renderJSON(result)
	} else {
		renderHuman(result)
	}

	if len(result.Violations) > 0 {
		return 1
	}

	if *strict {
		// Under strict mode, it's an error if no files were scanned
		// (suggests scope was wrong or empty tree).
		if result.FilesScanned == 0 {
			fmt.Fprintln(os.Stderr, "adr-check: strict mode: no .go files scanned")
			return 2
		}
	}

	fmt.Printf("adr-check OK: %d files scanned, 0 violations\n", result.FilesScanned)
	return 0
}

func renderHuman(result *Result) {
	for _, v := range result.Violations {
		fmt.Printf("ADRNNN: rule=%s file=%s:%d %s\n", v.Rule, v.File, v.Line, v.Detail)
	}
}

func renderJSON(result *Result) {
	type jsonViolation struct {
		Rule   string `json:"rule"`
		File   string `json:"file"`
		Line   int    `json:"line"`
		Detail string `json:"detail"`
	}
	type jsonResult struct {
		Violations   []jsonViolation `json:"violations"`
		FilesScanned int             `json:"files_scanned"`
	}
	jr := jsonResult{FilesScanned: result.FilesScanned}
	for _, v := range result.Violations {
		jr.Violations = append(jr.Violations, jsonViolation{
			Rule:   v.Rule,
			File:   v.File,
			Line:   v.Line,
			Detail: v.Detail,
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(jr)
}

// Result aliased for local use to avoid conflict with adrcheck.Result.
type Result = adrcheck.Result
