package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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

func writeTestSchema(t *testing.T, dir string) string {
	t.Helper()
	schema := map[string]any{
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"title":    "test",
		"type":     "object",
		"required": []any{"schema_version", "project", "policy", "modules", "tickets"},
		"properties": map[string]any{
			"schema_version": map[string]any{"type": "string"},
			"project":        map[string]any{"type": "string"},
			"generated_at":   map[string]any{"type": "string"},
			"generator":      map[string]any{"type": "string"},
			"policy": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"max_artifact_tokens":    map[string]any{"type": "integer"},
					"target_artifact_tokens": map[string]any{"type": "integer"},
					"complexity_budgets": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"S": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"context": map[string]any{"type": "integer"},
									"impl":    map[string]any{"type": "integer"},
									"test":    map[string]any{"type": "integer"},
									"total":   map[string]any{"type": "integer"},
								},
							},
						},
					},
					"library_path": map[string]any{"type": "string"},
				},
			},
			"modules": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":   map[string]any{"type": "string"},
						"spec": map[string]any{"type": "string"},
					},
				},
			},
			"tickets": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"required": []any{"id", "module", "title", "path", "status", "depends_on", "blocked_by",
						"target_files", "test_files", "context_files", "context_budget_tokens",
						"complexity", "priority", "gates", "contract_surface"},
					"properties": map[string]any{
						"id":                    map[string]any{"type": "string"},
						"module":                map[string]any{"type": "string"},
						"title":                 map[string]any{"type": "string"},
						"path":                  map[string]any{"type": "string"},
						"status":                map[string]any{"type": "string"},
						"depends_on":            map[string]any{"type": "array"},
						"blocked_by":            map[string]any{"type": "array"},
						"target_files":          map[string]any{"type": "array"},
						"test_files":            map[string]any{"type": "array"},
						"context_files":         map[string]any{"type": "array"},
						"context_budget_tokens": map[string]any{"type": "integer"},
						"complexity":            map[string]any{"type": "string"},
						"priority":              map[string]any{"type": "integer"},
						"gates":                 map[string]any{"type": "array"},
						"contract_surface": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"cli":      map[string]any{"type": "array"},
								"rpc":      map[string]any{"type": "array"},
								"session":  map[string]any{"type": "array"},
								"config":   map[string]any{"type": "array"},
								"fixtures": map[string]any{"type": "array"},
							},
						},
						"notes": map[string]any{"type": "string"},
					},
				},
			},
		},
		"$defs": map[string]any{},
	}
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	path := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	return path
}

func makeMinimalManifest() map[string]any {
	return map[string]any{
		"schema_version": "1.0.0",
		"project":        "aria2go",
		"generated_at":   "2026-05-19T00:00:00Z",
		"generator":      "test",
		"policy": map[string]any{
			"max_artifact_tokens":    10000,
			"target_artifact_tokens": 4000,
			"complexity_budgets": map[string]any{
				"S":  map[string]any{"context": 1500, "impl": 700, "test": 300, "total": 2500},
				"M":  map[string]any{"context": 2500, "impl": 1500, "test": 500, "total": 4500},
				"L":  map[string]any{"context": 3500, "impl": 2500, "test": 1000, "total": 7000},
				"XL": map[string]any{"context": 5000, "impl": 3500, "test": 1500, "total": 10000},
			},
			"library_path":            "pending",
			"reference_aria2_version": "1.37.0",
		},
		"modules": []any{
			map[string]any{"id": "00-bootstrap", "spec": "plans/modules/00-bootstrap/SPEC.md", "depends_on_modules": []any{}},
		},
		"tickets": []any{
			map[string]any{
				"id":                    "T001",
				"module":                "00-bootstrap",
				"title":                 "Test ticket",
				"path":                  "plans/modules/00-bootstrap/tickets/T001-go-mod.md",
				"status":                "done",
				"depends_on":            []any{},
				"blocked_by":            []any{},
				"target_files":          []any{"go.mod"},
				"test_files":            []any{},
				"context_files":         []any{"ENTRYPOINT.md"},
				"context_budget_tokens": 1000,
				"complexity":            "S",
				"priority":              1,
				"claimed_by":            "agent-001",
				"claimed_at":            "2026-05-18T00:00:00Z",
				"claim_ttl_seconds":     7200,
				"gates":                 []any{"go-build"},
				"contract_surface": map[string]any{
					"cli": []any{}, "rpc": []any{}, "session": []any{}, "config": []any{}, "fixtures": []any{},
				},
				"notes": "",
			},
		},
	}
}

func TestMain_ValidManifest(t *testing.T) {
	dir := t.TempDir()
	// Create files referenced by the manifest
	os.MkdirAll(filepath.Join(dir, "plans/modules/00-bootstrap/tickets"), 0755)
	os.WriteFile(filepath.Join(dir, "plans/modules/00-bootstrap/tickets/T001-go-mod.md"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(dir, "ENTRYPOINT.md"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	manifestPath := writeTestManifest(t, dir, makeMinimalManifest())
	schemaPath := writeTestSchema(t, dir)

	// Change dir to verify relative paths work
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	m, err := loadAndLint(manifestPath, schemaPath, false)
	if err != nil {
		t.Fatalf("loadAndLint: %v", err)
	}
	if !m.DAGAcyclic {
		t.Error("expected DAG acyclic")
	}
	if m.NumTickets != 1 {
		t.Errorf("expected 1 ticket, got %d", m.NumTickets)
	}
}

func TestMain_MissingManifest(t *testing.T) {
	code := run([]string{"--manifest", "/nonexistent/manifest.json", "--schema", "/nonexistent/schema.json"})
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestMain_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")
	os.WriteFile(manifestPath, []byte("{invalid json"), 0644)
	schemaPath := writeTestSchema(t, dir)

	code := run([]string{"--manifest", manifestPath, "--schema", schemaPath})
	if code != 2 {
		t.Errorf("expected exit code 2 for invalid JSON, got %d", code)
	}
}

func TestMain_StrictAndFixIncompatible(t *testing.T) {
	code := run([]string{"--strict", "--fix", "--manifest", "plans/manifest.json", "--schema", "plans/manifest.schema.json"})
	if code != 2 {
		t.Errorf("expected exit code 2 for --strict --fix, got %d", code)
	}
}

func TestMain_RebuildNotImplemented(t *testing.T) {
	code := run([]string{"--rebuild", "--manifest", "plans/manifest.json", "--schema", "plans/manifest.schema.json"})
	if code != 2 {
		t.Errorf("expected exit code 2 for --rebuild, got %d", code)
	}
}

func loadAndLint(manifestPath, schemaPath string, strict bool) (*manifest.LintResult, error) {
	m, err := manifest.LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	schema, err := manifest.LoadSchema(schemaPath)
	if err != nil {
		return nil, err
	}
	return manifest.Lint(m, schema, manifest.LintOptions{
		Strict:       strict,
		ManifestPath: manifestPath,
		SchemaPath:   schemaPath,
	})
}
