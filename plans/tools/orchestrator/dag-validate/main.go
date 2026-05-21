// dag-validate validates the ticket and module DAGs in plans/manifest.json.
//
// It verifies:
//   - Ticket depends_on graph is acyclic (Tarjan SCC)
//   - Module depends_on_modules graph is acyclic (Tarjan SCC)
//   - All depends_on references are known ticket IDs
//   - target_files sharing requires a depends_on edge (transitive closure)
//
// Usage:
//
//	dag-validate [flags]
//
// Flags:
//
//	--manifest  path to manifest.json (default: plans/manifest.json)
//	--strict    reserved for future use
//
// Exit codes:
//
//	0: DAG validates successfully
//	1: DAG violations found
//	2: invocation error (missing file, bad args)
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
	fs := flag.NewFlagSet("dag-validate", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "plans/manifest.json", "Path to manifest.json")
	_ = fs.Bool("strict", false, "Reserved for future use")
	_ = fs.Parse(args)

	m, err := manifest.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dag-validate: %v\n", err)
		return 2
	}

	result := manifest.ValidateDAG(m)

	for _, v := range result.Violations {
		fmt.Fprintf(os.Stderr, "%s\n", v.Error())
	}

	if len(result.Violations) > 0 {
		return 1
	}

	fmt.Printf("DAG OK: %d tickets, %d modules, longest critical path: %d tickets\n",
		result.NumTickets, result.NumModules, result.CriticalPath)
	return 0
}
