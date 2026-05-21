package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/plans/tools/orchestrator/internal/manifest"
)

func ptr(s string) *string { return &s }

func cs() manifest.ContractSurface {
	return manifest.ContractSurface{
		Cli: []string{}, Rpc: []string{}, Session: []string{}, Config: []string{}, Fixtures: []string{},
	}
}

func makeTicket(id string, deps []string, files []string) manifest.Ticket {
	return manifest.Ticket{
		ID:                  id,
		Module:              "00-bootstrap",
		Title:               "Test " + id,
		Path:                "plans/modules/00-bootstrap/tickets/" + id + ".md",
		Status:              "pending",
		DependsOn:           deps,
		BlockedBy:           []string{},
		TargetFiles:         files,
		TestFiles:           []string{},
		ContextFiles:        []string{"ENTRYPOINT.md"},
		ContextBudgetTokens: 1000,
		Complexity:          "S",
		Priority:            2,
		ClaimedBy:           nil,
		ClaimedAt:           nil,
		ClaimTTLSeconds:     3600,
		Gates:               []string{},
		ContractSurface:     cs(),
		Notes:               "",
	}
}

func makeManifest(tickets []manifest.Ticket, modules []manifest.Module) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: "1.0.0",
		Project:       "aria2go",
		GeneratedAt:   "2026-01-01T00:00:00Z",
		Generator:     "test",
		Policy: manifest.Policy{
			MaxArtifactTokens:    10000,
			TargetArtifactTokens: 4000,
			ComplexityBudgets: map[string]manifest.Budget{
				"S":  {Context: 1500, Impl: 700, Test: 300, Total: 2500},
				"M":  {Context: 2500, Impl: 1500, Test: 500, Total: 4500},
				"L":  {Context: 3500, Impl: 2500, Test: 1000, Total: 7000},
				"XL": {Context: 5000, Impl: 3500, Test: 1500, Total: 10000},
			},
			LibraryPath:           "pending",
			ReferenceAria2Version: "1.37.0",
		},
		Modules: modules,
		Tickets: tickets,
	}
}

func writeTestManifest(t *testing.T, dir string, m *manifest.Manifest) string {
	t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestRunAcyclicDAG(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
			makeTicket("T003", []string{"T001"}, []string{"c.go"}),
			makeTicket("T004", []string{"T002", "T003"}, []string{"d.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
			{ID: "01-core", DependsOnModules: []string{"00-bootstrap"}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 0 {
		t.Errorf("expected exit 0, got %d", exit)
	}
}

func TestRunTicketCycle(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{"T003"}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
			makeTicket("T003", []string{"T002"}, []string{"c.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 1 {
		t.Errorf("expected exit 1 for cycle, got %d", exit)
	}
}

func TestRunModuleCycle(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{"01-core"}},
			{ID: "01-core", DependsOnModules: []string{"00-bootstrap"}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 1 {
		t.Errorf("expected exit 1 for module cycle, got %d", exit)
	}
}

func TestRunUnknownDep(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{"T999"}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 1 {
		t.Errorf("expected exit 1 for unknown dep, got %d", exit)
	}
}

func TestRunTargetFileConflict(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 1 {
		t.Errorf("expected exit 1 for target_file conflict, got %d", exit)
	}
}

func TestRunTargetFileConflictResolved(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 0 {
		t.Errorf("expected exit 0 for resolved target_file conflict, got %d", exit)
	}
}

func TestRunTransitiveTargetFileConflict(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
			makeTicket("T003", []string{"T002"}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 0 {
		t.Errorf("expected exit 0 for transitive target_file resolution, got %d", exit)
	}
}

func TestRunMultipleViolations(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{"T002"}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
			makeTicket("T003", []string{}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 1 {
		t.Errorf("expected exit 1 for multiple violations, got %d", exit)
	}
}

func TestRunCriticalPath(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
			makeTicket("T003", []string{"T001"}, []string{"c.go"}),
			makeTicket("T004", []string{"T002", "T003"}, []string{"d.go"}),
			makeTicket("T005", []string{"T004"}, []string{"e.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 0 {
		t.Errorf("expected exit 0, got %d", exit)
	}

	result := manifest.ValidateDAG(m)
	if result.CriticalPath != 4 {
		t.Errorf("expected critical path 4, got %d", result.CriticalPath)
	}
}

func TestRunMissingManifest(t *testing.T) {
	exit := run([]string{"--manifest", "/nonexistent/manifest.json"})
	if exit != 2 {
		t.Errorf("expected exit 2 for missing manifest, got %d", exit)
	}
}

func TestRunEmptyManifest(t *testing.T) {
	dir := t.TempDir()
	m := makeManifest(nil, nil)
	path := writeTestManifest(t, dir, m)

	exit := run([]string{"--manifest", path})
	if exit != 0 {
		t.Errorf("expected exit 0 for empty manifest, got %d", exit)
	}
}

func TestValidateDAGAcyclic(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	if len(result.Violations) > 0 {
		t.Errorf("expected no violations, got %d: %v", len(result.Violations), result.Violations)
	}
	if result.NumTickets != 2 {
		t.Errorf("expected 2 tickets, got %d", result.NumTickets)
	}
}

func TestValidateDAGTicketCycle(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{"T002"}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	hasCycle := false
	for _, v := range result.Violations {
		if v.Type == "ticket_cycle" {
			hasCycle = true
		}
	}
	if !hasCycle {
		t.Errorf("expected ticket_cycle violation, got %v", result.Violations)
	}
}

func TestValidateDAGModuleCycle(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "M1", DependsOnModules: []string{"M2"}},
			{ID: "M2", DependsOnModules: []string{"M1"}},
		},
	)
	result := manifest.ValidateDAG(m)
	hasCycle := false
	for _, v := range result.Violations {
		if v.Type == "module_cycle" {
			hasCycle = true
		}
	}
	if !hasCycle {
		t.Errorf("expected module_cycle violation, got %v", result.Violations)
	}
}

func TestValidateDAGUnknownDep(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{"T999"}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	hasUnknown := false
	for _, v := range result.Violations {
		if v.Type == "unknown_dep" {
			hasUnknown = true
		}
	}
	if !hasUnknown {
		t.Errorf("expected unknown_dep violation, got %v", result.Violations)
	}
}

func TestValidateDAGTargetFileConflict(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	hasConflict := false
	for _, v := range result.Violations {
		if v.Type == "target_file_conflict" {
			hasConflict = true
		}
	}
	if !hasConflict {
		t.Errorf("expected target_file_conflict violation, got %v", result.Violations)
	}
}

func TestValidateDAGNoTargetFileConflict(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{}, []string{"b.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	if len(result.Violations) > 0 {
		t.Errorf("expected no violations, got %d: %v", len(result.Violations), result.Violations)
	}
}

func TestValidateDAGSelfLoop(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{"T001"}, []string{"a.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	hasCycle := false
	for _, v := range result.Violations {
		if v.Type == "ticket_cycle" {
			hasCycle = true
		}
	}
	if !hasCycle {
		t.Errorf("expected ticket_cycle violation for self-loop, got %v", result.Violations)
	}
}

func TestCriticalPathLinear(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
			makeTicket("T003", []string{"T002"}, []string{"c.go"}),
			makeTicket("T004", []string{"T003"}, []string{"d.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	if result.CriticalPath != 4 {
		t.Errorf("expected critical path 4, got %d", result.CriticalPath)
	}
}

func TestCriticalPathDiamond(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{"T001"}, []string{"b.go"}),
			makeTicket("T003", []string{"T001"}, []string{"c.go"}),
			makeTicket("T004", []string{"T002", "T003"}, []string{"d.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	if result.CriticalPath != 3 {
		t.Errorf("expected critical path 3, got %d", result.CriticalPath)
	}
}

func TestCriticalPathDisconnected(t *testing.T) {
	m := makeManifest(
		[]manifest.Ticket{
			makeTicket("T001", []string{}, []string{"a.go"}),
			makeTicket("T002", []string{}, []string{"b.go"}),
		},
		[]manifest.Module{
			{ID: "00-bootstrap", DependsOnModules: []string{}},
		},
	)
	result := manifest.ValidateDAG(m)
	if result.CriticalPath != 1 {
		t.Errorf("expected critical path 1, got %d", result.CriticalPath)
	}
}

func TestDAGViolationErrorFormat(t *testing.T) {
	v := manifest.DAGViolation{Type: "ticket_cycle", Message: "cycle detected"}
	errStr := v.Error()
	if !strings.Contains(errStr, "ticket_cycle") || !strings.Contains(errStr, "cycle detected") {
		t.Errorf("unexpected error format: %s", errStr)
	}
}
