package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func ptr(s string) *string { return &s }

func makeMinimalManifest() *Manifest {
	return &Manifest{
		SchemaVersion: "1.0.0",
		Project:       "aria2go",
		GeneratedAt:   "2026-05-19T00:00:00Z",
		Generator:     "test",
		Policy: Policy{
			MaxArtifactTokens:    10000,
			TargetArtifactTokens: 4000,
			ComplexityBudgets: map[string]Budget{
				"S":  {Context: 1500, Impl: 700, Test: 300, Total: 2500},
				"M":  {Context: 2500, Impl: 1500, Test: 500, Total: 4500},
				"L":  {Context: 3500, Impl: 2500, Test: 1000, Total: 7000},
				"XL": {Context: 5000, Impl: 3500, Test: 1500, Total: 10000},
			},
			LibraryPath:           "pending",
			ReferenceAria2Version: "1.37.0",
		},
		Modules: []Module{
			{ID: "00-bootstrap", Spec: "plans/modules/00-bootstrap/SPEC.md", DependsOnModules: []string{}},
		},
		Tickets: []Ticket{},
	}
}

func makeTicket(id string, status string, dependsOn []string) Ticket {
	t := Ticket{
		ID:                  id,
		Module:              "00-bootstrap",
		Title:               "Test ticket " + id,
		Path:                "plans/modules/00-bootstrap/tickets/T001-go-mod.md",
		Status:              status,
		DependsOn:           dependsOn,
		BlockedBy:           []string{},
		TargetFiles:         []string{"go.mod"},
		TestFiles:           []string{},
		ContextFiles:        []string{"ENTRYPOINT.md"},
		ContextBudgetTokens: 1000,
		Complexity:          "S",
		Priority:            2,
		ClaimedBy:           nil,
		ClaimedAt:           nil,
		ClaimTTLSeconds:     3600,
		Gates:               []string{"go-vet"},
		ContractSurface: ContractSurface{
			Cli: []string{}, Rpc: []string{}, Session: []string{}, Config: []string{}, Fixtures: []string{},
		},
		Notes: "",
	}
	if status == "done" {
		t.ClaimedBy = ptr("agent-001")
		t.ClaimedAt = ptr("2026-05-18T00:00:00Z")
	}
	return t
}

func repoPath(t *testing.T, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find %s from %s", rel, dir)
		}
		dir = parent
	}
}

// Rule 1: id regex ^T\d{3,}$, no duplicates
func TestRule1_ValidID(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		makeTicket("T001", "pending", nil),
		makeTicket("T002", "pending", nil),
	}
	errs := validateRule1(m)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestRule1_InvalidIDRegex(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		makeTicket("X001", "pending", nil),
	}
	errs := validateRule1(m)
	if len(errs) == 0 {
		t.Error("expected error for invalid id regex")
	}
}

func TestRule1_DuplicateID(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		makeTicket("T001", "pending", nil),
		makeTicket("T001", "pending", nil),
	}
	errs := validateRule1(m)
	if len(errs) == 0 {
		t.Error("expected error for duplicate id")
	}
}

func TestRule1_EmptyTickets(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{}
	errs := validateRule1(m)
	if len(errs) != 0 {
		t.Errorf("empty tickets should pass, got %v", errs)
	}
}

// Rule 2: path/target_files/test_files/context_files exist on disk
func TestRule2_ExistingPath(t *testing.T) {
	m := makeMinimalManifest()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "ticket.md")
	os.WriteFile(tmpFile, []byte("test"), 0644)

	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "done", nil)
			tt.Path = tmpFile
			tt.TargetFiles = []string{filepath.Join(tmpDir, "target.go")}
			tt.TestFiles = []string{}
			tt.ContextFiles = []string{tmpFile}
			return tt
		}(),
	}

	// target_files parent dir must exist but file may not
	errs := validateRule2(m, tmpDir)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestRule2_MissingPath(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Path = "/nonexistent/path/nowhere.md"
			return tt
		}(),
	}
	errs := validateRule2(m, ".")
	if len(errs) == 0 {
		t.Error("expected error for missing path")
	}
}

func TestRule2_MissingTargetParent(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Path = filepath.Join(t.TempDir(), "ticket.md") // can't reuse tempdir in closure
			return tt
		}(),
	}
	// Use a non-existent parent for target_files
	m.Tickets[0].TargetFiles = []string{"/nonexistent/parent/file.go"}
	m.Tickets[0].Path = "."
	errs := validateRule2(m, ".")
	if len(errs) == 0 {
		t.Error("expected error for missing target parent dir")
	}
}

func TestRule2_MissingContextFile(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Path = "."
			tt.TargetFiles = []string{"."}
			tt.ContextFiles = []string{"/nonexistent/context.md"}
			return tt
		}(),
	}
	errs := validateRule2(m, ".")
	if len(errs) == 0 {
		t.Error("expected error for missing context file")
	}
}

// Rule 3: depends_on references are known ids; DAG acyclic
func TestRule3_ValidDag(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		makeTicket("T001", "done", nil),
		func() Ticket {
			tt := makeTicket("T002", "pending", []string{"T001"})
			return tt
		}(),
	}
	errs := validateRule3(m)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid DAG, got %v", errs)
	}
}

func TestRule3_UnknownDep(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		makeTicket("T001", "pending", []string{"T999"}),
	}
	errs := validateRule3(m)
	if len(errs) == 0 {
		t.Error("expected error for unknown dependency")
	}
}

func TestRule3_CyclicDeps(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket { tt := makeTicket("T001", "pending", []string{"T002"}); return tt }(),
		func() Ticket { tt := makeTicket("T002", "pending", []string{"T001"}); return tt }(),
	}
	errs := validateRule3(m)
	hasCycle := false
	for _, e := range errs {
		if e.Rule == 3 {
			hasCycle = true
		}
	}
	if !hasCycle {
		t.Error("expected cycle detection error")
	}
}

// Rule 4: target_files overlap
func TestRule4_NoOverlap(t *testing.T) {
	m := makeMinimalManifest()
	a := makeTicket("T001", "pending", nil)
	a.TargetFiles = []string{"dir/a.go"}
	b := makeTicket("T002", "pending", []string{"T001"})
	b.TargetFiles = []string{"dir/b.go"}
	m.Tickets = []Ticket{a, b}
	errs := validateRule4(m)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestRule4_OverlapWithDep(t *testing.T) {
	m := makeMinimalManifest()
	a := makeTicket("T001", "done", nil)
	a.TargetFiles = []string{"dir/shared.go"}
	b := makeTicket("T002", "pending", []string{"T001"})
	b.TargetFiles = []string{"dir/shared.go"}
	m.Tickets = []Ticket{a, b}
	errs := validateRule4(m)
	if len(errs) != 0 {
		t.Errorf("overlap with dependency should be ok, got %v", errs)
	}
}

func TestRule4_OverlapWithoutDep(t *testing.T) {
	m := makeMinimalManifest()
	a := makeTicket("T001", "pending", nil)
	a.TargetFiles = []string{"dir/shared.go"}
	b := makeTicket("T002", "pending", nil)
	b.TargetFiles = []string{"dir/shared.go"}
	m.Tickets = []Ticket{a, b}
	errs := validateRule4(m)
	if len(errs) == 0 {
		t.Error("expected error for overlapping target_files without dependency")
	}
}

// Rule 5: context_budget_tokens <= policy.complexity_budgets[complexity].context
func TestRule5_WithinBudget(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.ContextBudgetTokens = 1500
			tt.Complexity = "S"
			return tt
		}(),
	}
	errs := validateRule5(m)
	if len(errs) != 0 {
		t.Errorf("expected no errors within budget, got %v", errs)
	}
}

func TestRule5_OverBudget(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.ContextBudgetTokens = 2000
			tt.Complexity = "S"
			return tt
		}(),
	}
	errs := validateRule5(m)
	if len(errs) == 0 {
		t.Error("expected error for over-budget context")
	}
}

func TestRule5_UnknownComplexity(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Complexity = "UNKNOWN"
			return tt
		}(),
	}
	errs := validateRule5(m)
	if len(errs) == 0 {
		t.Error("expected error for unknown complexity")
	}
}

// Rule 6: gates drawn from enum
func TestRule6_ValidGates(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Gates = []string{"go-vet", "go-test", "race", "fuzz-http-30s", "bench", "interop-aria2c", "go-build", "go-vet-adr-check"}
			return tt
		}(),
	}
	errs := validateRule6(m)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid gates, got %v", errs)
	}
}

func TestRule6_InvalidGate(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Gates = []string{"invalid-gate"}
			return tt
		}(),
	}
	errs := validateRule6(m)
	if len(errs) == 0 {
		t.Error("expected error for invalid gate")
	}
}

func TestRule6_EmptyGates(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Gates = []string{}
			return tt
		}(),
	}
	errs := validateRule6(m)
	if len(errs) != 0 {
		t.Errorf("empty gates should be ok, got %v", errs)
	}
}

// Rule 7: module references resolve
func TestRule7_ValidModule(t *testing.T) {
	m := makeMinimalManifest()
	m.Modules = []Module{
		{ID: "00-bootstrap"},
	}
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Module = "00-bootstrap"
			return tt
		}(),
	}
	errs := validateRule7(m)
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid module, got %v", errs)
	}
}

func TestRule7_UnknownModule(t *testing.T) {
	m := makeMinimalManifest()
	m.Modules = []Module{
		{ID: "00-bootstrap"},
	}
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", nil)
			tt.Module = "99-nonexistent"
			return tt
		}(),
	}
	errs := validateRule7(m)
	if len(errs) == 0 {
		t.Error("expected error for unknown module")
	}
}

// Rule 8: status=done requires claimed_by != null
func TestRule8_DoneHasClaimedBy(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "done", nil)
			tt.ClaimedBy = ptr("agent-001")
			tt.ClaimedAt = ptr("2026-05-18T00:00:00Z")
			return tt
		}(),
	}
	errs := validateRule8(m)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestRule8_DoneMissingClaimedBy(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "done", nil)
			tt.ClaimedBy = nil
			return tt
		}(),
	}
	errs := validateRule8(m)
	if len(errs) == 0 {
		t.Error("expected error for done without claimed_by")
	}
}

// Rule 9: in_progress tickets past claimed_at + claim_ttl_seconds warn/error
func TestRule9_NotStale(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "in_progress", nil)
			tt.ClaimedBy = ptr("agent-001")
			tt.ClaimedAt = ptr("2099-01-01T00:00:00Z")
			tt.ClaimTTLSeconds = 3600
			return tt
		}(),
	}
	errs := validateRule9(m, false)
	if len(errs) != 0 {
		t.Errorf("expected no errors for recent claim, got %v", errs)
	}
}

func TestRule9_StaleClaim(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "in_progress", nil)
			tt.ClaimedBy = ptr("agent-001")
			tt.ClaimedAt = ptr("2020-01-01T00:00:00Z")
			tt.ClaimTTLSeconds = 3600
			return tt
		}(),
	}
	// non-strict: should warn (which is still reported)
	errs := validateRule9(m, false)
	if len(errs) == 0 {
		t.Error("expected warning for stale claim")
	}
	// strict: should error
	errs = validateRule9(m, true)
	if len(errs) == 0 {
		t.Error("expected error for stale claim in strict mode")
	}
}

func TestRule9_NullClaimedAt(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "in_progress", nil)
			tt.ClaimedBy = ptr("agent-001")
			tt.ClaimedAt = nil
			return tt
		}(),
	}
	errs := validateRule9(m, false)
	if len(errs) != 0 {
		t.Errorf("null claimed_at should not trigger TTL, got %v", errs)
	}
}

// Rule 10: priority=1 tickets on longest critical path
func TestRule10_Priority1OnCriticalPath(t *testing.T) {
	m := makeMinimalManifest()
	a := makeTicket("T001", "done", nil)
	a.Priority = 1
	a.Complexity = "S"
	b := makeTicket("T002", "done", []string{"T001"})
	b.Priority = 1
	b.Complexity = "S"
	c := makeTicket("T003", "pending", []string{"T001"})
	c.Priority = 2
	c.Complexity = "S"
	m.Tickets = []Ticket{a, b, c}
	errs := validateRule10(m)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestRule10_Priority1NotOnCriticalPath(t *testing.T) {
	m := makeMinimalManifest()
	a := makeTicket("T001", "done", nil)
	a.Priority = 1
	a.Complexity = "S"
	b := makeTicket("T002", "pending", []string{"T001"})
	b.Priority = 2
	b.Complexity = "S" // not on critical path
	c := makeTicket("T003", "pending", nil)
	c.Priority = 1 // priority 1 but not on the only critical path
	c.Complexity = "S"
	m.Tickets = []Ticket{a, b, c}
	errs := validateRule10(m)
	if len(errs) == 0 {
		t.Error("expected error for priority=1 ticket not on critical path")
	}
}

func TestRule10_UsesComplexityBudgetWeights(t *testing.T) {
	m := makeMinimalManifest()
	a := makeTicket("T001", "done", nil)
	a.Complexity = "S"
	a.Priority = 2
	b := makeTicket("T002", "done", []string{"T001"})
	b.Complexity = "S"
	b.Priority = 2
	c := makeTicket("T003", "done", []string{"T002"})
	c.Complexity = "S"
	c.Priority = 2
	d := makeTicket("T004", "done", nil)
	d.Complexity = "XL"
	d.Priority = 1
	m.Tickets = []Ticket{a, b, c, d}

	errs := validateRule10(m)
	if len(errs) != 0 {
		t.Errorf("expected XL ticket to be on weighted critical path, got %v", errs)
	}
}

// JSON Schema validation
func TestSchemaValidation_Valid(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"id", "name"},
		"properties": map[string]any{
			"id":   map[string]any{"type": "string", "pattern": "^T\\d{3,}$"},
			"name": map[string]any{"type": "string", "minLength": float64(1)},
		},
	}
	root := map[string]any{
		"$defs": map[string]any{},
	}
	instance := map[string]any{
		"id":   "T001",
		"name": "test",
	}
	errs := validateAgainstSchema(root, schema, instance, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestSchemaValidation_InvalidType(t *testing.T) {
	schema := map[string]any{
		"type": "string",
	}
	root := map[string]any{"$defs": map[string]any{}}
	instance := float64(42)
	errs := validateAgainstSchema(root, schema, instance, "")
	if len(errs) == 0 {
		t.Error("expected type error")
	}
}

func TestSchemaValidation_Enum(t *testing.T) {
	schema := map[string]any{
		"type": "string",
		"enum": []any{"a", "b"},
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, "c", "")
	if len(errs) == 0 {
		t.Error("expected enum error")
	}
	errs = validateAgainstSchema(root, schema, "a", "")
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid enum, got %v", errs)
	}
}

func TestSchemaValidation_Const(t *testing.T) {
	schema := map[string]any{
		"const": "aria2go",
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, "wrong", "")
	if len(errs) == 0 {
		t.Error("expected const error")
	}
	errs = validateAgainstSchema(root, schema, "aria2go", "")
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid const, got %v", errs)
	}
}

func TestSchemaValidation_Pattern(t *testing.T) {
	schema := map[string]any{
		"type":    "string",
		"pattern": "^T\\d{3,}$",
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, "X001", "")
	if len(errs) == 0 {
		t.Error("expected pattern error")
	}
	errs = validateAgainstSchema(root, schema, "T001", "")
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid pattern, got %v", errs)
	}
}

func TestSchemaValidation_Required(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"name"},
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	root := map[string]any{"$defs": map[string]any{}}
	instance := map[string]any{}
	errs := validateAgainstSchema(root, schema, instance, "")
	if len(errs) == 0 {
		t.Error("expected required property error")
	}
	instance["name"] = "ok"
	errs = validateAgainstSchema(root, schema, instance, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestSchemaValidation_Items(t *testing.T) {
	schema := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string", "minLength": float64(1)},
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, []any{"ok", 42}, "")
	if len(errs) == 0 {
		t.Error("expected items type error")
	}
	errs = validateAgainstSchema(root, schema, []any{"ok", "also-ok"}, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestSchemaValidation_Ref(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{
			"budget": map[string]any{
				"type":     "object",
				"required": []any{"context", "impl", "test"},
				"properties": map[string]any{
					"context": map[string]any{"type": "integer", "minimum": float64(0)},
					"impl":    map[string]any{"type": "integer", "minimum": float64(0)},
					"test":    map[string]any{"type": "integer", "minimum": float64(0)},
				},
			},
		},
	}
	schema := map[string]any{
		"$ref": "#/$defs/budget",
	}
	instance := map[string]any{
		"context": float64(1500),
		"impl":    float64(700),
		"test":    float64(300),
	}
	errs := validateAgainstSchema(root, schema, instance, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid $ref, got %v", errs)
	}

	badInstance := map[string]any{
		"context": float64(-1),
	}
	errs = validateAgainstSchema(root, schema, badInstance, "")
	if len(errs) == 0 {
		t.Error("expected error for invalid $ref instance")
	}
}

func TestSchemaValidation_MinMax(t *testing.T) {
	schema := map[string]any{
		"type":    "integer",
		"minimum": float64(1),
		"maximum": float64(5),
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, float64(0), "")
	if len(errs) == 0 {
		t.Error("expected minimum error")
	}
	errs = validateAgainstSchema(root, schema, float64(6), "")
	if len(errs) == 0 {
		t.Error("expected maximum error")
	}
	errs = validateAgainstSchema(root, schema, float64(3), "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestSchemaValidation_MinMaxLength(t *testing.T) {
	schema := map[string]any{
		"type":      "string",
		"minLength": float64(2),
		"maxLength": float64(5),
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, "a", "")
	if len(errs) == 0 {
		t.Error("expected minLength error")
	}
	errs = validateAgainstSchema(root, schema, "abcdef", "")
	if len(errs) == 0 {
		t.Error("expected maxLength error")
	}
	errs = validateAgainstSchema(root, schema, "abc", "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestSchemaValidation_MinMaxItems(t *testing.T) {
	schema := map[string]any{
		"type":     "array",
		"minItems": float64(1),
		"maxItems": float64(3),
		"items":    map[string]any{"type": "string"},
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, []any{}, "")
	if len(errs) == 0 {
		t.Error("expected minItems error")
	}
	errs = validateAgainstSchema(root, schema, []any{"a", "b", "c", "d"}, "")
	if len(errs) == 0 {
		t.Error("expected maxItems error")
	}
	errs = validateAgainstSchema(root, schema, []any{"a", "b"}, "")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestSchemaValidation_Nullable(t *testing.T) {
	// ["string", "null"] type allows null
	schema := map[string]any{
		"type": []any{"string", "null"},
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, nil, "")
	if len(errs) != 0 {
		t.Errorf("expected null to pass, got %v", errs)
	}
	errs = validateAgainstSchema(root, schema, "hello", "")
	if len(errs) != 0 {
		t.Errorf("expected string to pass, got %v", errs)
	}
	errs = validateAgainstSchema(root, schema, float64(42), "")
	if len(errs) == 0 {
		t.Error("expected number to fail for string|null type")
	}
}

// Full schema against a valid manifest
func TestFullSchema_ValidManifest(t *testing.T) {
	schema, err := LoadSchema(repoPath(t, "plans/manifest.schema.json"))
	if err != nil {
		t.Fatalf("failed to load schema: %v", err)
	}
	root, ok := schema.(map[string]any)
	if !ok {
		t.Fatal("schema root is not map[string]any")
	}
	m := makeMinimalManifest()
	instance := manifestToMap(m)
	errs := validateAgainstSchema(root, root, instance, "")
	if len(errs) != 0 {
		t.Errorf("expected valid manifest to pass schema, got %v", errs)
	}
}

// Integration test: full Lint() on a valid manifest
func TestLintIntegration_Valid(t *testing.T) {
	schema, err := LoadSchema(repoPath(t, "plans/manifest.schema.json"))
	if err != nil {
		t.Fatalf("failed to load schema: %v", err)
	}
	// Create files referenced by the manifest
	dir := t.TempDir()
	os.MkdirAll(dir+"/plans/modules/00-bootstrap/tickets", 0755)
	os.WriteFile(dir+"/go.mod", []byte("module test"), 0644)
	os.WriteFile(dir+"/ENTRYPOINT.md", []byte("test"), 0644)
	os.WriteFile(dir+"/plans/modules/00-bootstrap/tickets/T001-go-mod.md", []byte("test"), 0644)
	os.WriteFile(dir+"/LICENSE", []byte("test"), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	m := makeMinimalManifest()
	a := makeTicket("T001", "done", nil)
	a.TargetFiles = []string{"go.mod"}
	a.ContextFiles = []string{"ENTRYPOINT.md"}
	a.ClaimedBy = ptr("agent-001")
	a.ClaimedAt = ptr("2026-05-18T00:00:00Z")
	a.Priority = 1
	a.Complexity = "S"
	b := makeTicket("T002", "pending", []string{"T001"})
	b.TargetFiles = []string{"LICENSE"}
	b.ContextFiles = []string{"ENTRYPOINT.md"}
	b.Complexity = "S"
	m.Tickets = []Ticket{a, b}
	result, err := Lint(m, schema, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(result.Errors) != 0 {
		for _, e := range result.Errors {
			t.Logf("unexpected error: %s", e.Error())
		}
		t.Fatalf("expected 0 errors, got %d", len(result.Errors))
	}
	if !result.DAGAcyclic {
		t.Error("expected DAG acyclic")
	}
	if result.NumTickets != 2 {
		t.Errorf("expected 2 tickets, got %d", result.NumTickets)
	}
}

func TestLintError_Error(t *testing.T) {
	e := LintError{TicketID: "T001", Rule: 1, Severity: "error", Message: "bad id"}
	s := e.Error()
	if s != "T001: rule 1 violation: bad id" {
		t.Errorf("unexpected error string: %s", s)
	}

	// Empty ticket id
	e2 := LintError{Rule: 3, Severity: "error", Message: "cycle"}
	s2 := e2.Error()
	if s2 != "manifest: rule 3 violation: cycle" {
		t.Errorf("unexpected error string: %s", s2)
	}
}

func TestHasErrors(t *testing.T) {
	errs := []LintError{
		{Severity: "warning"},
	}
	if hasErrors(errs) {
		t.Error("hasErrors should return false for warnings only")
	}
	errs = append(errs, LintError{Severity: "error"})
	if !hasErrors(errs) {
		t.Error("hasErrors should return true when errors present")
	}
}

func TestComputeCriticalPath(t *testing.T) {
	m := makeMinimalManifest()
	a := makeTicket("T001", "pending", nil)
	a.Complexity = "S"
	b := makeTicket("T002", "pending", []string{"T001"})
	b.Complexity = "S"
	c := makeTicket("T003", "pending", []string{"T002"})
	c.Complexity = "S"
	m.Tickets = []Ticket{a, b, c}
	cp := computeCriticalPath(m)
	if cp != 3 {
		t.Errorf("expected critical path length 3, got %d", cp)
	}
}

func TestIsDAGAcyclic(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		makeTicket("T001", "pending", nil),
		makeTicket("T002", "pending", []string{"T001"}),
	}
	if !isDAGAcyclic(m) {
		t.Error("expected DAG acyclic")
	}

	m2 := makeMinimalManifest()
	m2.Tickets = []Ticket{
		func() Ticket { tt := makeTicket("T001", "pending", []string{"T002"}); return tt }(),
		func() Ticket { tt := makeTicket("T002", "pending", []string{"T001"}); return tt }(),
	}
	if isDAGAcyclic(m2) {
		t.Error("expected DAG cyclic")
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/manifest.json"
	os.WriteFile(path, []byte(`{"schema_version":"1.0.0","project":"aria2go","policy":{},"modules":[],"tickets":[]}`), 0644)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Project != "aria2go" {
		t.Errorf("expected project aria2go, got %s", m.Project)
	}

	_, err = LoadManifest("/nonexistent")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadSchema(t *testing.T) {
	_, err := LoadSchema("/nonexistent/schema.json")
	if err == nil {
		t.Error("expected error for missing schema")
	}
}

func TestSchemaValidation_FalseSchema(t *testing.T) {
	schema := false
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, "anything", "")
	if len(errs) == 0 {
		t.Error("expected rejection for false schema")
	}
}

func TestSchemaValidation_FormatDateTime(t *testing.T) {
	schema := map[string]any{
		"type":   "string",
		"format": "date-time",
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, "not-a-date", "")
	if len(errs) == 0 {
		t.Error("expected date-time format error")
	}
	errs = validateAgainstSchema(root, schema, "2026-05-19T00:00:00Z", "")
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid date-time, got %v", errs)
	}
}

func TestSchemaValidation_UnknownFormat(t *testing.T) {
	schema := map[string]any{
		"type":   "string",
		"format": "email",
	}
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, schema, "anything", "")
	if len(errs) != 0 {
		t.Errorf("unknown format should be ignored, got %v", errs)
	}
}

func TestSchemaValidation_NullSchema(t *testing.T) {
	root := map[string]any{"$defs": map[string]any{}}
	errs := validateAgainstSchema(root, nil, "anything", "")
	if len(errs) != 0 {
		t.Errorf("nil schema should pass, got %v", errs)
	}
}

func TestResolveRef(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{
			"foo": map[string]any{"bar": "baz"},
		},
	}
	result := resolveRef(root, "#/$defs/foo")
	if result == nil {
		t.Error("expected resolved ref")
	}
	result = resolveRef(root, "#/$defs/nonexistent")
	if result != nil {
		t.Error("expected nil for unresolved ref")
	}
	result = resolveRef(root, "http://external/schema")
	if result == nil {
		t.Error("external ref should return root")
	}
}

func TestFindCycles_SelfLoop(t *testing.T) {
	m := makeMinimalManifest()
	m.Tickets = []Ticket{
		func() Ticket {
			tt := makeTicket("T001", "pending", []string{"T001"})
			return tt
		}(),
	}
	cycles := findCycles(m)
	if len(cycles) != 0 {
		t.Errorf("self-loops don't create SCCs of size > 1 in this implementation")
	}
}

func TestLintIntegration_InvalidManifest(t *testing.T) {
	schema, err := LoadSchema(repoPath(t, "plans/manifest.schema.json"))
	if err != nil {
		t.Fatalf("failed to load schema: %v", err)
	}
	// Create temp dir with required files
	dir := t.TempDir()
	os.MkdirAll(dir+"/plans/modules/00-bootstrap/tickets", 0755)
	os.WriteFile(dir+"/go.mod", []byte("module test"), 0644)
	os.WriteFile(dir+"/ENTRYPOINT.md", []byte("test"), 0644)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	m := makeMinimalManifest()
	// Add a ticket with an invalid id
	bad := makeTicket("X001", "pending", nil)
	bad.TargetFiles = []string{"go.mod"}
	bad.ContextFiles = []string{"ENTRYPOINT.md"}
	m.Tickets = append(m.Tickets, bad)
	result, err := Lint(m, schema, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Error("expected errors for invalid manifest")
	}
}

// Tokenizer test
func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"hello world", 3},
		{"0123456789", 3}, // 10 bytes, ceil(10/4) = 3
	}
	for _, tc := range tests {
		got := tokenize([]byte(tc.input))
		if got != tc.expected {
			t.Errorf("tokenize(%q) = %d, want %d", tc.input, got, tc.expected)
		}
	}
}
