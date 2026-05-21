// claim-sweep scans in_progress tickets in plans/manifest.json and reverts
// any whose claim TTL has expired back to pending, freeing them for other
// agents to claim.
//
// Usage:
//
//	claim-sweep [flags]
//
// Flags:
//
//	--manifest  path to manifest.json (default: plans/manifest.json)
//	--dry-run   print what would be swept without modifying the file
//
// Exit codes:
//
//	0: sweep completed (may be zero tickets)
//	2: invocation error (missing file, bad args, etc.)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/smartass08/aria2go/plans/tools/orchestrator/internal/manifest"
)

var timeNow = time.Now

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("claim-sweep", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "Print what would be swept without modifying the file")
	manifestPath := fs.String("manifest", "plans/manifest.json", "Path to manifest.json")
	_ = fs.Parse(args)

	m, err := manifest.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "claim-sweep: %v\n", err)
		return 2
	}

	swept := sweep(m, timeNow().UTC(), *dryRun)

	if *dryRun {
		for _, id := range swept {
			fmt.Printf("WOULD sweep %s\n", id)
		}
		return 0
	}

	if len(swept) > 0 {
		if err := saveManifest(*manifestPath, m); err != nil {
			fmt.Fprintf(os.Stderr, "claim-sweep: %v\n", err)
			return 2
		}
	}

	for _, id := range swept {
		fmt.Println(id)
	}
	return 0
}

// sweep iterates tickets and reverts any whose claim TTL has expired.
// It mutates the in-memory manifest. Returns the list of swept ticket IDs.
func sweep(m *manifest.Manifest, now time.Time, dryRun bool) []string {
	var swept []string
	for i := range m.Tickets {
		t := &m.Tickets[i]
		if t.Status != "in_progress" {
			continue
		}
		if t.ClaimedAt == nil {
			continue
		}
		claimedAt, err := time.Parse(time.RFC3339, *t.ClaimedAt)
		if err != nil {
			continue
		}
		ttl := time.Duration(t.ClaimTTLSeconds) * time.Second
		if !now.After(claimedAt.Add(ttl)) {
			continue
		}

		if !dryRun {
			t.Status = "pending"
			t.ClaimedBy = nil
			t.ClaimedAt = nil
			note := fmt.Sprintf("swept: exceeded TTL at %s", now.Format(time.RFC3339))
			if t.Notes != "" {
				t.Notes += "; " + note
			} else {
				t.Notes = note
			}
		}
		swept = append(swept, t.ID)
	}
	return swept
}

func saveManifest(path string, m *manifest.Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}
