// manifest-lint validates plans/manifest.json against its JSON Schema and
// enforces the 10 manifest invariants from ENTRYPOINT.md §10.
//
// Usage:
//
//	manifest-lint [flags]
//
// Flags:
//
//	--manifest  path to manifest.json (default: plans/manifest.json)
//	--schema    path to manifest.schema.json (default: plans/manifest.schema.json)
//	--strict    treat warnings as errors
//	--fix       apply auto-fixable corrections (not yet implemented)
//	--rebuild   regenerate tickets from frontmatter (not yet implemented)
//
// Exit codes:
//
//	0: all validations passed
//	1: validation errors found
//	2: invocation error (missing file, bad args, etc.)
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/smartass08/aria2go/plans/tools/orchestrator/internal/manifest"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("manifest-lint", flag.ContinueOnError)
	strict := fs.Bool("strict", false, "Treat warnings as errors")
	fix := fs.Bool("fix", false, "Apply auto-fixable corrections")
	rebuild := fs.Bool("rebuild", false, "Regenerate tickets from frontmatter")
	manifestPath := fs.String("manifest", "plans/manifest.json", "Path to manifest.json")
	schemaPath := fs.String("schema", "plans/manifest.schema.json", "Path to manifest.schema.json")
	_ = fs.Parse(args)

	if *fix && *strict {
		fmt.Fprintln(os.Stderr, "manifest-lint: --fix is incompatible with --strict")
		return 2
	}
	if *rebuild {
		fmt.Fprintln(os.Stderr, "manifest-lint: --rebuild is not yet implemented")
		return 2
	}
	if *fix {
		fmt.Fprintln(os.Stderr, "manifest-lint: --fix is not yet implemented")
		return 2
	}

	m, err := manifest.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "manifest-lint: %v\n", err)
		return 2
	}

	schema, err := manifest.LoadSchema(*schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "manifest-lint: %v\n", err)
		return 2
	}

	result, err := manifest.Lint(m, schema, manifest.LintOptions{
		Strict:       *strict,
		Fix:          *fix,
		Rebuild:      *rebuild,
		ManifestPath: *manifestPath,
		SchemaPath:   *schemaPath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "manifest-lint: %v\n", err)
		return 2
	}

	// Print warnings
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "%s\n", w.Error())
	}

	// Print errors
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "%s\n", e.Error())
	}

	if len(result.Errors) > 0 {
		return 1
	}

	// Success output
	acyclic := "acyclic"
	if !result.DAGAcyclic {
		acyclic = "CYCLIC"
	}
	fmt.Printf("manifest OK: %d tickets, %d modules, DAG %s, longest critical path: %d tickets\n",
		result.NumTickets, result.NumModules, acyclic, result.CriticalPath)
	return 0
}
