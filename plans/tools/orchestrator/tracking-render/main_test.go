package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartass08/aria2go/plans/tools/orchestrator/internal/manifest"
)

func ptr(s string) *string { return &s }

func writeTestManifest(t *testing.T, dir string, m any) string {
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

func makeBaseManifest() map[string]any {
	return map[string]any{
		"schema_version": "1.0.0",
		"project":        "aria2go",
		"generated_at":   "2026-05-19T00:00:00Z",
		"generator":      "test",
		"policy": map[string]any{
			"max_artifact_tokens":    10000,
			"target_artifact_tokens": 4000,
			"complexity_budgets": map[string]any{
				"S": map[string]any{"context": 1500, "impl": 700, "test": 300, "total": 2500},
			},
			"library_path":            "pending",
			"reference_aria2_version": "1.37.0",
		},
		"modules": []any{
			map[string]any{"id": "00-bootstrap", "spec": "spec.md", "depends_on_modules": []any{}},
		},
		"tickets": []any{},
	}
}

func makeTicket(id, module, status string, blockedBy []string, dependsOn []string, complexity string, priority int, claimedAt *string) map[string]any {
	if blockedBy == nil {
		blockedBy = []string{}
	}
	if dependsOn == nil {
		dependsOn = []string{}
	}
	return map[string]any{
		"id":                    id,
		"module":                module,
		"title":                 "Test ticket " + id,
		"path":                  "tickets/" + id + ".md",
		"status":                status,
		"depends_on":            dependsOn,
		"blocked_by":            blockedBy,
		"target_files":          []any{"file.go"},
		"test_files":            []any{},
		"context_files":         []any{},
		"context_budget_tokens": 1000,
		"complexity":            complexity,
		"priority":              priority,
		"claimed_by":            nil,
		"claimed_at":            claimedAt,
		"claim_ttl_seconds":     3600,
		"gates":                 []any{},
		"contract_surface": map[string]any{
			"cli": []any{}, "rpc": []any{}, "session": []any{}, "config": []any{}, "fixtures": []any{},
		},
		"notes": "",
	}
}

func TestMain_ValidManifest(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	recent := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)

	m := makeBaseManifest()
	m["tickets"] = []any{
		makeTicket("T001", "00-bootstrap", "done", nil, nil, "S", 1, ptr(recent)),
		makeTicket("T002", "00-bootstrap", "in_progress", nil, nil, "M", 2, ptr(now)),
		makeTicket("T003", "00-bootstrap", "in_review", nil, nil, "S", 1, ptr(recent)),
		makeTicket("T004", "00-bootstrap", "pending", nil, []string{"T001"}, "L", 3, nil),
		makeTicket("T005", "00-bootstrap", "pending", []string{"human decision"}, nil, "M", 2, ptr(old)),
	}

	manifestPath := writeTestManifest(t, dir, m)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	report, err := generateReport(loaded)
	if err != nil {
		t.Fatalf("generateReport: %v", err)
	}

	// Verify header
	if !strings.Contains(report, "# aria2go — Ticket Tracking") {
		t.Error("report missing title")
	}
	if !strings.Contains(report, "Generated:") {
		t.Error("report missing generated timestamp")
	}

	// Verify total line
	if !strings.Contains(report, "Total tickets: 5") {
		t.Error("report missing total tickets")
	}

	// Verify summary table
	if !strings.Contains(report, "Done | 1") {
		t.Error("report missing Done count")
	}
	if !strings.Contains(report, "In Progress | 1") {
		t.Error("report missing In Progress count")
	}
	if !strings.Contains(report, "In Review | 1") {
		t.Error("report missing In Review count")
	}
	if !strings.Contains(report, "Pending | 1") {
		t.Error("report missing Pending count")
	}
	if !strings.Contains(report, "Blocked | 1") {
		t.Error("report missing Blocked count")
	}

	// Verify module table
	if !strings.Contains(report, "## By Module") {
		t.Error("report missing By Module section")
	}

	// Verify blocked tickets section
	if !strings.Contains(report, "## Blocked Tickets") {
		t.Error("report missing Blocked Tickets section")
	}
	if !strings.Contains(report, "T005 | human decision") {
		t.Error("report missing blocked ticket T005")
	}

	// Verify ready to claim (T004: pending, deps done=T001)
	if !strings.Contains(report, "## Ready to Claim") {
		t.Error("report missing Ready to Claim section")
	}
	if !strings.Contains(report, "T004") {
		t.Error("report missing ready ticket T004")
	}

	// Verify recent activity (T001, T002, T003 claimed within 24h; T005 is 48h old)
	if !strings.Contains(report, "## Recent Activity") {
		t.Error("report missing Recent Activity section")
	}
	if strings.Contains(report, "T005") && strings.Contains(report, "Recent Activity") {
		// T005 should NOT be in recent activity (48h old)
		// Check that the Recent Activity section doesn't contain T005
		idx := strings.Index(report, "## Recent Activity")
		after := report[idx:]
		if strings.Contains(after, "T005") {
			t.Error("T005 should not appear in recent activity (claimed 48h ago)")
		}
	}
}

func TestMain_MissingManifest(t *testing.T) {
	code := run([]string{"--manifest", "/nonexistent/manifest.json"})
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestMain_WriteFlag(t *testing.T) {
	dir := t.TempDir()

	m := makeBaseManifest()
	m["tickets"] = []any{
		makeTicket("T001", "00-bootstrap", "done", nil, nil, "S", 1, nil),
	}

	manifestPath := writeTestManifest(t, dir, m)
	outputPath := filepath.Join(dir, "TRACKING.md")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := run([]string{"--manifest", manifestPath, "--output", outputPath, "--write"})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "# aria2go — Ticket Tracking") {
		t.Error("output file missing title")
	}
}

func TestMain_WriteFlagDisabled(t *testing.T) {
	dir := t.TempDir()

	m := makeBaseManifest()
	m["tickets"] = []any{
		makeTicket("T001", "00-bootstrap", "done", nil, nil, "S", 1, nil),
	}

	manifestPath := writeTestManifest(t, dir, m)
	outputPath := filepath.Join(dir, "TRACKING.md")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	code := run([]string{"--manifest", manifestPath, "--output", outputPath})
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	if _, err := os.Stat(outputPath); err == nil {
		t.Error("output file should not exist when --write is not set")
	}
}

func TestMain_NoTickets(t *testing.T) {
	dir := t.TempDir()
	m := makeBaseManifest()
	manifestPath := writeTestManifest(t, dir, m)

	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	report, err := generateReport(loaded)
	if err != nil {
		t.Fatalf("generateReport: %v", err)
	}

	if !strings.Contains(report, "Total tickets: 0") {
		t.Error("expected Total tickets: 0")
	}
	if strings.Contains(report, "## Blocked Tickets") {
		t.Error("should not have blocked tickets section when none exist")
	}
}

func TestMain_BlockedOnly(t *testing.T) {
	dir := t.TempDir()

	m := makeBaseManifest()
	m["tickets"] = []any{
		makeTicket("T001", "00-bootstrap", "pending", []string{"blocked by design"}, nil, "M", 1, nil),
		makeTicket("T002", "00-bootstrap", "pending", []string{"waiting for X"}, nil, "L", 2, nil),
	}

	manifestPath := writeTestManifest(t, dir, m)

	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	report, err := generateReport(loaded)
	if err != nil {
		t.Fatalf("generateReport: %v", err)
	}

	if !strings.Contains(report, "Blocked | 2") {
		t.Error("expected 2 blocked")
	}
	if !strings.Contains(report, "Done | 0") {
		t.Error("expected 0 done")
	}
	if !strings.Contains(report, "## Blocked Tickets") {
		t.Error("expected blocked tickets section")
	}
}

func TestMain_ReadyToClaimDepsIncomplete(t *testing.T) {
	dir := t.TempDir()

	m := makeBaseManifest()
	m["tickets"] = []any{
		makeTicket("T001", "00-bootstrap", "pending", nil, nil, "S", 1, nil),
		makeTicket("T002", "00-bootstrap", "pending", nil, []string{"T003"}, "M", 2, nil),
		makeTicket("T003", "00-bootstrap", "in_progress", nil, nil, "S", 3, nil),
	}

	manifestPath := writeTestManifest(t, dir, m)

	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	report, err := generateReport(loaded)
	if err != nil {
		t.Fatalf("generateReport: %v", err)
	}

	// T001 should be ready (no deps), T002 should NOT (T003 is not done), T003 not eligible
	if !strings.Contains(report, "T001") || !strings.Contains(report, "## Ready to Claim") {
		t.Error("T001 should be in Ready to Claim")
	}
	// T002 depends on T003 which is in_progress (not done)
	idx := strings.Index(report, "## Ready to Claim")
	after := report[idx:]
	if strings.Contains(after, "T002") {
		t.Error("T002 should not be in Ready to Claim (dep T003 not done)")
	}
}

func TestMain_RecentActivityOrdering(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	t1 := now.Add(-30 * time.Minute).Format(time.RFC3339)
	t2 := now.Add(-1 * time.Hour).Format(time.RFC3339)
	t3 := now.Add(-2 * time.Hour).Format(time.RFC3339)

	m := makeBaseManifest()
	m["tickets"] = []any{
		makeTicket("T001", "00-bootstrap", "done", nil, nil, "S", 1, ptr(t1)),
		makeTicket("T002", "00-bootstrap", "done", nil, nil, "S", 1, ptr(t3)),
		makeTicket("T003", "00-bootstrap", "done", nil, nil, "S", 1, ptr(t2)),
	}

	manifestPath := writeTestManifest(t, dir, m)

	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	report, err := generateReport(loaded)
	if err != nil {
		t.Fatalf("generateReport: %v", err)
	}

	// Find Recent Activity section and verify ordering
	idx := strings.Index(report, "## Recent Activity")
	if idx < 0 {
		t.Fatal("missing Recent Activity section")
	}
	after := report[idx:]

	pos1 := strings.Index(after, "T001")
	pos2 := strings.Index(after, "T002")
	pos3 := strings.Index(after, "T003")

	if pos1 < 0 || pos2 < 0 || pos3 < 0 {
		t.Fatal("missing ticket entries in Recent Activity")
	}

	// T001 (30 min ago) should be first, T002 (2h ago) should be last
	if pos1 >= pos2 || pos1 >= pos3 {
		t.Error("T001 (most recent) should appear first")
	}
	if pos2 <= pos1 || pos2 <= pos3 {
		t.Error("T002 (oldest) should appear last")
	}
}

func TestMain_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	os.WriteFile(manifestPath, []byte("{invalid json"), 0644)

	code := run([]string{"--manifest", manifestPath})
	if code != 2 {
		t.Errorf("expected exit code 2 for invalid JSON, got %d", code)
	}
}

func TestMain_EmptyReport(t *testing.T) {
	dir := t.TempDir()
	m := makeBaseManifest()
	manifestPath := writeTestManifest(t, dir, m)

	loaded, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	report, err := generateReport(loaded)
	if err != nil {
		t.Fatalf("generateReport: %v", err)
	}

	// All standard sections should be present
	sections := []string{
		"## Summary",
		"## By Module",
		"## Recent Activity",
	}
	for _, s := range sections {
		if !strings.Contains(report, s) {
			t.Errorf("missing section: %s", s)
		}
	}
}
