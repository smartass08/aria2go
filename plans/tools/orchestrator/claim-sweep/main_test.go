package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartass08/aria2go/plans/tools/orchestrator/internal/manifest"
)

func ptr(s string) *string { return &s }

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

func baseManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: "1.0.0",
		Project:       "aria2go",
		GeneratedAt:   "2026-05-19T00:00:00Z",
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
		Modules: []manifest.Module{
			{ID: "00-bootstrap", Spec: "plans/modules/00-bootstrap/SPEC.md", DependsOnModules: []string{}},
		},
		Tickets: []manifest.Ticket{},
	}
}

func emptyContractSurface() manifest.ContractSurface {
	return manifest.ContractSurface{
		Cli:      []string{},
		Rpc:      []string{},
		Session:  []string{},
		Config:   []string{},
		Fixtures: []string{},
	}
}

func baseTicket(id, status string) manifest.Ticket {
	return manifest.Ticket{
		ID:                  id,
		Module:              "00-bootstrap",
		Title:               "Test " + id,
		Path:                "plans/modules/00-bootstrap/tickets/" + id + ".md",
		Status:              status,
		DependsOn:           []string{},
		BlockedBy:           []string{},
		TargetFiles:         []string{"go.mod"},
		TestFiles:           []string{},
		ContextFiles:        []string{"ENTRYPOINT.md"},
		ContextBudgetTokens: 1000,
		Complexity:          "S",
		Priority:            1,
		ClaimedBy:           nil,
		ClaimedAt:           nil,
		ClaimTTLSeconds:     3600,
		Gates:               []string{"go-build"},
		ContractSurface:     emptyContractSurface(),
		Notes:               "",
	}
}

func TestSweep_NoInProgressTickets(t *testing.T) {
	m := baseManifest()
	t1 := baseTicket("T001", "done")
	t2 := baseTicket("T002", "pending")
	m.Tickets = []manifest.Ticket{t1, t2}

	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	swept := sweep(m, now, false)
	if len(swept) != 0 {
		t.Errorf("expected 0 swept, got %d: %v", len(swept), swept)
	}
}

func TestSweep_WithinTTL(t *testing.T) {
	m := baseManifest()
	t1 := baseTicket("T001", "in_progress")
	claimedAt := "2026-05-19T11:30:00Z"
	t1.ClaimedBy = ptr("agent-001")
	t1.ClaimedAt = &claimedAt
	t1.ClaimTTLSeconds = 3600
	m.Tickets = []manifest.Ticket{t1}

	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	swept := sweep(m, now, false)
	if len(swept) != 0 {
		t.Errorf("expected 0 swept (within TTL), got %d", len(swept))
	}
}

func TestSweep_ExpiredTTL(t *testing.T) {
	m := baseManifest()
	t1 := baseTicket("T001", "in_progress")
	claimedAt := "2026-05-19T10:00:00Z"
	t1.ClaimedBy = ptr("agent-001")
	t1.ClaimedAt = &claimedAt
	t1.ClaimTTLSeconds = 3600
	m.Tickets = []manifest.Ticket{t1}

	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	swept := sweep(m, now, false)
	if len(swept) != 1 {
		t.Fatalf("expected 1 swept, got %d", len(swept))
	}
	if swept[0] != "T001" {
		t.Errorf("expected T001 swept, got %s", swept[0])
	}
	if m.Tickets[0].Status != "pending" {
		t.Errorf("expected status pending, got %s", m.Tickets[0].Status)
	}
	if m.Tickets[0].ClaimedBy != nil {
		t.Errorf("expected claimed_by nil, got %v", *m.Tickets[0].ClaimedBy)
	}
	if m.Tickets[0].ClaimedAt != nil {
		t.Errorf("expected claimed_at nil, got %v", *m.Tickets[0].ClaimedAt)
	}
	expectedNote := "swept: exceeded TTL at 2026-05-19T12:00:00Z"
	if m.Tickets[0].Notes != expectedNote {
		t.Errorf("expected note %q, got %q", expectedNote, m.Tickets[0].Notes)
	}
}

func TestSweep_ExpiredTTLAppendsToExistingNotes(t *testing.T) {
	m := baseManifest()
	t1 := baseTicket("T001", "in_progress")
	claimedAt := "2026-05-19T10:00:00Z"
	t1.ClaimedBy = ptr("agent-001")
	t1.ClaimedAt = &claimedAt
	t1.ClaimTTLSeconds = 3600
	t1.Notes = "prior note"
	m.Tickets = []manifest.Ticket{t1}

	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	swept := sweep(m, now, false)
	if len(swept) != 1 {
		t.Fatalf("expected 1 swept, got %d", len(swept))
	}
	expectedNote := "prior note; swept: exceeded TTL at 2026-05-19T12:00:00Z"
	if m.Tickets[0].Notes != expectedNote {
		t.Errorf("expected note %q, got %q", expectedNote, m.Tickets[0].Notes)
	}
}

func TestSweep_DryRun(t *testing.T) {
	m := baseManifest()
	t1 := baseTicket("T001", "in_progress")
	claimedAt := "2026-05-19T10:00:00Z"
	t1.ClaimedBy = ptr("agent-001")
	t1.ClaimedAt = &claimedAt
	t1.ClaimTTLSeconds = 3600
	m.Tickets = []manifest.Ticket{t1}

	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	before := m.Tickets[0].Status
	swept := sweep(m, now, true)
	if len(swept) != 1 {
		t.Fatalf("expected 1 swept, got %d", len(swept))
	}
	if swept[0] != "T001" {
		t.Errorf("expected T001 swept, got %s", swept[0])
	}
	if m.Tickets[0].Status != before {
		t.Error("dry-run should not mutate ticket status")
	}
	if m.Tickets[0].ClaimedBy == nil {
		t.Error("dry-run should not mutate claimed_by")
	}
	if m.Tickets[0].Notes != "" {
		t.Errorf("dry-run should not mutate notes, got %q", m.Tickets[0].Notes)
	}
}

func TestSweep_MixedTickets(t *testing.T) {
	m := baseManifest()
	expired := baseTicket("T001", "in_progress")
	ca1 := "2026-05-19T10:00:00Z"
	expired.ClaimedBy = ptr("agent-001")
	expired.ClaimedAt = &ca1
	expired.ClaimTTLSeconds = 3600

	valid := baseTicket("T002", "in_progress")
	ca2 := "2026-05-19T11:30:00Z"
	valid.ClaimedBy = ptr("agent-002")
	valid.ClaimedAt = &ca2
	valid.ClaimTTLSeconds = 3600

	doneTick := baseTicket("T003", "done")
	pendingTick := baseTicket("T004", "pending")
	noClaimedAt := baseTicket("T005", "in_progress")
	noClaimedAt.ClaimedBy = ptr("agent-003")

	m.Tickets = []manifest.Ticket{expired, valid, doneTick, pendingTick, noClaimedAt}

	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	swept := sweep(m, now, false)
	if len(swept) != 1 {
		t.Fatalf("expected 1 swept, got %d: %v", len(swept), swept)
	}
	if swept[0] != "T001" {
		t.Errorf("expected T001 swept, got %s", swept[0])
	}
}

func TestSweep_ExpiredExactlyAtTTL(t *testing.T) {
	m := baseManifest()
	t1 := baseTicket("T001", "in_progress")
	claimedAt := "2026-05-19T11:00:00Z"
	t1.ClaimedBy = ptr("agent-001")
	t1.ClaimedAt = &claimedAt
	t1.ClaimTTLSeconds = 3600
	m.Tickets = []manifest.Ticket{t1}

	now := time.Date(2026, 5, 19, 12, 0, 0, 1, time.UTC)
	swept := sweep(m, now, false)
	if len(swept) != 1 {
		t.Errorf("expected 1 swept (1s past TTL), got %d", len(swept))
	}
	now = time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	m.Tickets[0].Status = "in_progress"
	m.Tickets[0].ClaimedBy = ptr("agent-001")
	m.Tickets[0].ClaimedAt = &claimedAt
	m.Tickets[0].Notes = ""
	swept2 := sweep(m, now, false)
	if len(swept2) != 0 {
		t.Errorf("expected 0 swept (at exact TTL boundary), got %d", len(swept2))
	}
}

func TestSweep_UnknownTimestamp(t *testing.T) {
	m := baseManifest()
	t1 := baseTicket("T001", "in_progress")
	badTS := "not-a-timestamp"
	t1.ClaimedBy = ptr("agent-001")
	t1.ClaimedAt = &badTS
	t1.ClaimTTLSeconds = 3600
	m.Tickets = []manifest.Ticket{t1}

	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	swept := sweep(m, now, false)
	if len(swept) != 0 {
		t.Errorf("expected 0 swept (unparseable timestamp), got %d", len(swept))
	}
}

func TestRun_DryRunFlag(t *testing.T) {
	dir := t.TempDir()
	m := baseManifest()
	t1 := baseTicket("T001", "in_progress")
	claimedAt := "2026-05-19T10:00:00Z"
	t1.ClaimedBy = ptr("agent-001")
	t1.ClaimedAt = &claimedAt
	t1.ClaimTTLSeconds = 3600
	m.Tickets = []manifest.Ticket{t1}
	manifestPath := writeTestManifest(t, dir, m)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := run([]string{"--manifest", manifestPath, "--dry-run"})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	reloaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Tickets[0].Status != "in_progress" {
		t.Error("dry-run should not persist changes")
	}
}

func TestRun_MissingManifest(t *testing.T) {
	code := run([]string{"--manifest", "/nonexistent/manifest.json"})
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestRun_SweepPersists(t *testing.T) {
	dir := t.TempDir()
	m := baseManifest()
	t1 := baseTicket("T001", "in_progress")
	claimedAt := "2026-05-19T10:00:00Z"
	t1.ClaimedBy = ptr("agent-001")
	t1.ClaimedAt = &claimedAt
	t1.ClaimTTLSeconds = 3600
	m.Tickets = []manifest.Ticket{t1}
	manifestPath := writeTestManifest(t, dir, m)

	mockNow := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	origTimeNow := timeNow
	timeNow = func() time.Time { return mockNow }
	defer func() { timeNow = origTimeNow }()

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := run([]string{"--manifest", manifestPath})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	reloaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Tickets[0].Status != "pending" {
		t.Errorf("expected pending after sweep, got %s", reloaded.Tickets[0].Status)
	}
	if reloaded.Tickets[0].ClaimedBy != nil {
		t.Errorf("expected nil claimed_by, got %v", *reloaded.Tickets[0].ClaimedBy)
	}
	expectedNote := "swept: exceeded TTL at"
	if len(reloaded.Tickets[0].Notes) < len(expectedNote) || reloaded.Tickets[0].Notes[:len(expectedNote)] != expectedNote {
		t.Errorf("expected notes to start with %q, got %q", expectedNote, reloaded.Tickets[0].Notes)
	}
}
