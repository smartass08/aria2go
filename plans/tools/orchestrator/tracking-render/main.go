// tracking-render reads plans/manifest.json and generates a markdown tracking
// report to stdout (and optionally writes it to a file).
//
// Usage:
//
//	tracking-render [flags]
//
// Flags:
//
//	--manifest  path to manifest.json (default: plans/manifest.json)
//	--output    path to write tracking report (default: plans/TRACKING.md)
//	--write     if true, write output to --output file
//
// Exit codes:
//
//	0: success
//	2: invocation error (missing file, bad args, etc.)
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/smartass08/aria2go/plans/tools/orchestrator/internal/manifest"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("tracking-render", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "plans/manifest.json", "Path to manifest.json")
	outputPath := fs.String("output", "plans/TRACKING.md", "Path to write output")
	write := fs.Bool("write", false, "Write output to file")
	_ = fs.Parse(args)

	m, err := manifest.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tracking-render: %v\n", err)
		return 2
	}

	report, err := generateReport(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tracking-render: %v\n", err)
		return 2
	}

	fmt.Print(report)

	if *write {
		if err := os.WriteFile(*outputPath, []byte(report), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "tracking-render: writing output: %v\n", err)
			return 2
		}
	}

	return 0
}

func generateReport(m *manifest.Manifest) (string, error) {
	var b strings.Builder
	b.WriteString("# aria2go — Ticket Tracking\n\n")
	b.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().UTC().Format(time.RFC3339)))

	// Compute stats
	doneIDs := make(map[string]bool)
	statusCounts := map[string]int{
		"done":        0,
		"in_progress": 0,
		"in_review":   0,
		"pending":     0,
		"blocked":     0,
	}

	for _, t := range m.Tickets {
		if t.Status == "done" {
			doneIDs[t.ID] = true
		}
	}

	var blockedTickets []blockedEntry
	var readyTickets []readyEntry
	var recentTickets []recentEntry

	now := time.Now().UTC()

	for _, t := range m.Tickets {
		isBlocked := len(t.BlockedBy) > 0

		switch {
		case isBlocked:
			statusCounts["blocked"]++
			blockedTickets = append(blockedTickets, blockedEntry{
				ID:      t.ID,
				Blocker: strings.Join(t.BlockedBy, "; "),
			})
		case t.Status == "done":
			statusCounts["done"]++
		case t.Status == "in_progress":
			statusCounts["in_progress"]++
		case t.Status == "in_review":
			statusCounts["in_review"]++
		case t.Status == "pending":
			statusCounts["pending"]++
		default:
			statusCounts["pending"]++
		}

		// Ready to claim: pending, not blocked, all deps done
		if t.Status == "pending" && len(t.BlockedBy) == 0 {
			depsDone := true
			for _, dep := range t.DependsOn {
				if !doneIDs[dep] {
					depsDone = false
					break
				}
			}
			if depsDone {
				readyTickets = append(readyTickets, readyEntry{
					ID:         t.ID,
					Module:     t.Module,
					Priority:   t.Priority,
					Complexity: t.Complexity,
				})
			}
		}

		// Recent activity: claimed_at within last 24h
		if t.ClaimedAt != nil {
			claimedAt, err := time.Parse(time.RFC3339, *t.ClaimedAt)
			if err == nil && now.Sub(claimedAt) < 24*time.Hour {
				recentTickets = append(recentTickets, recentEntry{
					ID:        t.ID,
					Status:    t.Status,
					ClaimedAt: claimedAt,
				})
			}
		}
	}

	// Sort recent by claimed_at descending
	sort.Slice(recentTickets, func(i, j int) bool {
		return recentTickets[i].ClaimedAt.After(recentTickets[j].ClaimedAt)
	})

	// Sort ready by priority ascending, then by ID
	sort.Slice(readyTickets, func(i, j int) bool {
		if readyTickets[i].Priority != readyTickets[j].Priority {
			return readyTickets[i].Priority < readyTickets[j].Priority
		}
		return readyTickets[i].ID < readyTickets[j].ID
	})

	// Sort blocked by ID
	sort.Slice(blockedTickets, func(i, j int) bool {
		return blockedTickets[i].ID < blockedTickets[j].ID
	})

	total := len(m.Tickets)
	done := statusCounts["done"]
	inProgress := statusCounts["in_progress"]
	pending := statusCounts["pending"]
	blocked := statusCounts["blocked"]

	b.WriteString(fmt.Sprintf("Total tickets: %d | Done: %d | In Progress: %d | Pending: %d | Blocked: %d\n\n",
		total, done, inProgress, pending, blocked))

	// Summary table
	writeSummaryTable(&b, statusCounts)

	// By Module table
	writeModuleTable(&b, m)

	// Blocked Tickets table
	writeBlockedTable(&b, blockedTickets)

	// Ready to Claim table
	writeReadyTable(&b, readyTickets)

	// Recent Activity
	writeRecentSection(&b, recentTickets)

	return b.String(), nil
}

type blockedEntry struct {
	ID      string
	Blocker string
}

type readyEntry struct {
	ID         string
	Module     string
	Priority   int
	Complexity string
}

type recentEntry struct {
	ID        string
	Status    string
	ClaimedAt time.Time
}

func writeSummaryTable(b *strings.Builder, counts map[string]int) {
	_ = counts // unused in body, but we read specific keys
	b.WriteString("## Summary\n\n")
	b.WriteString("| Status | Count |\n")
	b.WriteString("|--------|-------|\n")
	order := []string{"done", "in_progress", "in_review", "pending", "blocked"}
	labels := map[string]string{
		"done":        "Done",
		"in_progress": "In Progress",
		"in_review":   "In Review",
		"pending":     "Pending",
		"blocked":     "Blocked",
	}
	for _, s := range order {
		b.WriteString(fmt.Sprintf("| %s | %d |\n", labels[s], counts[s]))
	}
	b.WriteString("\n")
}

func writeModuleTable(b *strings.Builder, m *manifest.Manifest) {
	b.WriteString("## By Module\n\n")
	b.WriteString("| Module | Total | Done | Pending | Blocked |\n")
	b.WriteString("|--------|-------|------|---------|----------|\n")

	for _, mod := range m.Modules {
		total := 0
		done := 0
		pending := 0
		blocked := 0
		for _, t := range m.Tickets {
			if t.Module != mod.ID {
				continue
			}
			total++
			if len(t.BlockedBy) > 0 {
				blocked++
			} else if t.Status == "done" {
				done++
			} else {
				pending++
			}
		}
		b.WriteString(fmt.Sprintf("| %s | %d | %d | %d | %d |\n", mod.ID, total, done, pending, blocked))
	}
	b.WriteString("\n")
}

func writeBlockedTable(b *strings.Builder, entries []blockedEntry) {
	if len(entries) == 0 {
		return
	}
	b.WriteString("## Blocked Tickets\n\n")
	b.WriteString("| Ticket | Blocker |\n")
	b.WriteString("|--------|----------|\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("| %s | %s |\n", e.ID, e.Blocker))
	}
	b.WriteString("\n")
}

func writeReadyTable(b *strings.Builder, entries []readyEntry) {
	if len(entries) == 0 {
		return
	}
	b.WriteString("## Ready to Claim\n\n")
	b.WriteString("| Ticket | Module | Priority | Complexity |\n")
	b.WriteString("|--------|--------|----------|------------|\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("| %s | %s | %d | %s |\n", e.ID, e.Module, e.Priority, e.Complexity))
	}
	b.WriteString("\n")
}

func writeRecentSection(b *strings.Builder, entries []recentEntry) {
	b.WriteString("## Recent Activity\n\n")
	if len(entries) == 0 {
		b.WriteString("_(none)_\n\n")
		return
	}
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("- %s `%s` — claimed %s\n",
			e.ID, e.Status, e.ClaimedAt.Format(time.RFC3339)))
	}
	b.WriteString("\n")
}
