// Package manifest provides types and validation for the aria2go manifest.json.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
)

// Manifest represents the top-level manifest.json structure.
type Manifest struct {
	SchemaVersion string   `json:"schema_version"`
	Project       string   `json:"project"`
	GeneratedAt   string   `json:"generated_at"`
	Generator     string   `json:"generator"`
	Policy        Policy   `json:"policy"`
	Modules       []Module `json:"modules"`
	Tickets       []Ticket `json:"tickets"`
}

// Policy holds project-wide policy settings.
type Policy struct {
	MaxArtifactTokens     int               `json:"max_artifact_tokens"`
	TargetArtifactTokens  int               `json:"target_artifact_tokens"`
	ComplexityBudgets     map[string]Budget `json:"complexity_budgets"`
	LibraryPath           string            `json:"library_path"`
	ReferenceAria2Version string            `json:"reference_aria2_version"`
}

// Budget defines token budgets for a complexity tier.
type Budget struct {
	Context int `json:"context"`
	Impl    int `json:"impl"`
	Test    int `json:"test"`
	Total   int `json:"total"`
}

// Module represents a module entry.
type Module struct {
	ID               string   `json:"id"`
	Spec             string   `json:"spec"`
	DependsOnModules []string `json:"depends_on_modules"`
}

// Ticket represents a single ticket in the manifest.
type Ticket struct {
	ID                  string          `json:"id"`
	Module              string          `json:"module"`
	Title               string          `json:"title"`
	Path                string          `json:"path"`
	Status              string          `json:"status"`
	DependsOn           []string        `json:"depends_on"`
	BlockedBy           []string        `json:"blocked_by"`
	TargetFiles         []string        `json:"target_files"`
	TestFiles           []string        `json:"test_files"`
	ContextFiles        []string        `json:"context_files"`
	ContextBudgetTokens int             `json:"context_budget_tokens"`
	Complexity          string          `json:"complexity"`
	Priority            int             `json:"priority"`
	ClaimedBy           *string         `json:"claimed_by"`
	ClaimedAt           *string         `json:"claimed_at"`
	ClaimTTLSeconds     int             `json:"claim_ttl_seconds"`
	Gates               []string        `json:"gates"`
	ContractSurface     ContractSurface `json:"contract_surface"`
	Notes               string          `json:"notes"`
}

// ContractSurface describes which API surfaces a ticket touches.
type ContractSurface struct {
	Cli      []string `json:"cli"`
	Rpc      []string `json:"rpc"`
	Session  []string `json:"session"`
	Config   []string `json:"config"`
	Fixtures []string `json:"fixtures"`
}

// LoadManifest reads and parses a manifest.json file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &m, nil
}

// LoadSchema reads a raw JSON schema file. Returns the root as any for the
// minimal schema walker.
func LoadSchema(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading schema: %w", err)
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing schema: %w", err)
	}
	return root, nil
}
