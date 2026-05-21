package manifest

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"time"
)

// LintError represents a single lint violation.
type LintError struct {
	TicketID string
	Rule     int
	Severity string // "error" or "warning"
	Message  string
}

func (e LintError) Error() string {
	if e.TicketID != "" {
		return fmt.Sprintf("%s: rule %d violation: %s", e.TicketID, e.Rule, e.Message)
	}
	return fmt.Sprintf("manifest: rule %d violation: %s", e.Rule, e.Message)
}

// LintResult holds the aggregated results of manifest linting.
type LintResult struct {
	Errors       []LintError
	Warnings     []LintError
	NumTickets   int
	NumModules   int
	DAGAcyclic   bool
	CriticalPath int
	schemaOK     bool
}

// LintOptions configures linting behavior.
type LintOptions struct {
	Strict       bool
	Fix          bool
	Rebuild      bool
	ManifestPath string
	SchemaPath   string
}

// Lint runs all validation rules against the manifest.
func Lint(m *Manifest, schema any, opts LintOptions) (*LintResult, error) {
	result := &LintResult{
		NumTickets: len(m.Tickets),
		NumModules: len(m.Modules),
	}

	result.Errors = append(result.Errors, validateSchema(m, schema)...)
	if hasErrors(result.Errors) {
		return result, nil
	}
	result.schemaOK = true

	result.Errors = append(result.Errors, validateRule1(m)...)
	result.Errors = append(result.Errors, validateRule2(m, ".")...)
	result.Errors = append(result.Errors, validateRule3(m)...)
	result.Errors = append(result.Errors, validateRule4(m)...)
	result.Errors = append(result.Errors, validateRule5(m)...)
	result.Errors = append(result.Errors, validateRule6(m)...)
	result.Errors = append(result.Errors, validateRule7(m)...)
	result.Errors = append(result.Errors, validateRule8(m)...)

	r9 := validateRule9(m, opts.Strict)
	for _, e := range r9 {
		if opts.Strict || e.Severity == "error" {
			result.Errors = append(result.Errors, e)
		} else {
			result.Warnings = append(result.Warnings, e)
		}
	}

	r10 := validateRule10(m)
	result.Errors = append(result.Errors, r10...)
	result.DAGAcyclic = isDAGAcyclic(m)
	result.CriticalPath = computeCriticalPath(m)

	return result, nil
}

// jsonType returns the JSON-like type name for a Go value.
func jsonType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case bool:
		return "boolean"
	case float64:
		if math.Trunc(v.(float64)) == v.(float64) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

// validateSchema validates a manifest against a JSON Schema.
func validateSchema(m *Manifest, schema any) []LintError {
	root, ok := schema.(map[string]any)
	if !ok {
		return []LintError{{Rule: 0, Severity: "error", Message: "schema root must be an object"}}
	}
	// Convert manifest to map for schema validation
	instance := manifestToMap(m)
	errs := validateAgainstSchema(root, root, instance, "")
	var out []LintError
	for _, e := range errs {
		out = append(out, LintError{Rule: 0, Severity: "error", Message: "schema violation: " + e})
	}
	return out
}

func manifestToMap(m *Manifest) map[string]any {
	tickets := make([]any, len(m.Tickets))
	for i, t := range m.Tickets {
		ti := map[string]any{
			"id":                    t.ID,
			"module":                t.Module,
			"title":                 t.Title,
			"path":                  t.Path,
			"status":                t.Status,
			"depends_on":            strSliceToAnySlice(t.DependsOn),
			"blocked_by":            strSliceToAnySlice(t.BlockedBy),
			"target_files":          strSliceToAnySlice(t.TargetFiles),
			"test_files":            strSliceToAnySlice(t.TestFiles),
			"context_files":         strSliceToAnySlice(t.ContextFiles),
			"context_budget_tokens": float64(t.ContextBudgetTokens),
			"complexity":            t.Complexity,
			"priority":              float64(t.Priority),
			"gates":                 strSliceToAnySlice(t.Gates),
			"contract_surface": map[string]any{
				"cli":      strSliceToAnySlice(t.ContractSurface.Cli),
				"rpc":      strSliceToAnySlice(t.ContractSurface.Rpc),
				"session":  strSliceToAnySlice(t.ContractSurface.Session),
				"config":   strSliceToAnySlice(t.ContractSurface.Config),
				"fixtures": strSliceToAnySlice(t.ContractSurface.Fixtures),
			},
			"notes": t.Notes,
		}
		if t.ClaimedBy != nil {
			ti["claimed_by"] = *t.ClaimedBy
		} else {
			ti["claimed_by"] = nil
		}
		if t.ClaimedAt != nil {
			ti["claimed_at"] = *t.ClaimedAt
		} else {
			ti["claimed_at"] = nil
		}
		if t.ClaimTTLSeconds > 0 {
			ti["claim_ttl_seconds"] = float64(t.ClaimTTLSeconds)
		}
		tickets[i] = ti
	}

	modules := make([]any, len(m.Modules))
	for i, mod := range m.Modules {
		modules[i] = map[string]any{
			"id":                 mod.ID,
			"spec":               mod.Spec,
			"depends_on_modules": strSliceToAnySlice(mod.DependsOnModules),
		}
	}

	budgets := make(map[string]any)
	for k, b := range m.Policy.ComplexityBudgets {
		budgets[k] = map[string]any{
			"context": float64(b.Context),
			"impl":    float64(b.Impl),
			"test":    float64(b.Test),
			"total":   float64(b.Total),
		}
	}

	return map[string]any{
		"schema_version": m.SchemaVersion,
		"project":        m.Project,
		"generated_at":   m.GeneratedAt,
		"generator":      m.Generator,
		"policy": map[string]any{
			"max_artifact_tokens":     float64(m.Policy.MaxArtifactTokens),
			"target_artifact_tokens":  float64(m.Policy.TargetArtifactTokens),
			"complexity_budgets":      budgets,
			"library_path":            m.Policy.LibraryPath,
			"reference_aria2_version": m.Policy.ReferenceAria2Version,
		},
		"modules": modules,
		"tickets": tickets,
	}
}

func strSliceToAnySlice(s []string) []any {
	a := make([]any, len(s))
	for i, v := range s {
		a[i] = v
	}
	return a
}

func validateAgainstSchema(root map[string]any, schema any, instance any, path string) []string {
	if schema == nil {
		return nil
	}

	if isBool(schema) {
		b, _ := schema.(bool)
		if !b {
			return []string{fmt.Sprintf("%s is rejected by false schema", path)}
		}
		return nil
	}

	s, ok := schema.(map[string]any)
	if !ok {
		return nil
	}

	// Handle $ref
	if ref, ok := s["$ref"]; ok {
		refStr, _ := ref.(string)
		resolved := resolveRef(root, refStr)
		if resolved == nil {
			return []string{fmt.Sprintf("%s: unresolved $ref %q", path, refStr)}
		}
		return validateAgainstSchema(root, resolved, instance, path)
	}

	var errs []string

	// type check
	if types, ok := s["type"]; ok {
		match := false
		switch ts := types.(type) {
		case string:
			match = jsonType(instance) == ts
		case []any:
			for _, t := range ts {
				if tStr, ok := t.(string); ok && jsonType(instance) == tStr {
					match = true
					break
				}
			}
		}
		if !match {
			return []string{fmt.Sprintf("%s: expected type %v, got %s", path, types, jsonType(instance))}
		}
	}

	// enum
	if enumVals, ok := s["enum"]; ok {
		enumArr, ok := enumVals.([]any)
		if ok {
			found := false
			for _, ev := range enumArr {
				if equal(instance, ev) {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("%s: value %v not in enum", path, instance))
			}
		}
	}

	// const
	if constVal, ok := s["const"]; ok {
		if !equal(instance, constVal) {
			errs = append(errs, fmt.Sprintf("%s: expected const %v, got %v", path, constVal, instance))
		}
	}

	// pattern
	if pat, ok := s["pattern"]; ok {
		if str, ok := instance.(string); ok {
			re, err := regexp.Compile(pat.(string))
			if err == nil && !re.MatchString(str) {
				errs = append(errs, fmt.Sprintf("%s: %q does not match pattern %q", path, str, pat))
			}
		}
	}

	// format (only date-time is checked)
	if format, ok := s["format"]; ok {
		if format == "date-time" {
			if str, ok := instance.(string); ok {
				_, err := time.Parse(time.RFC3339, str)
				if err != nil {
					errs = append(errs, fmt.Sprintf("%s: %q is not a valid date-time", path, str))
				}
			}
		}
	}

	// numeric constraints
	if v, ok := toFloat(instance); ok {
		if min, ok := s["minimum"]; ok {
			minVal, _ := toFloat(min)
			if v < minVal {
				errs = append(errs, fmt.Sprintf("%s: %v is less than minimum %v", path, v, minVal))
			}
		}
		if max, ok := s["maximum"]; ok {
			maxVal, _ := toFloat(max)
			if v > maxVal {
				errs = append(errs, fmt.Sprintf("%s: %v is greater than maximum %v", path, v, maxVal))
			}
		}
	}

	// string length constraints
	if str, ok := instance.(string); ok {
		if minL, ok := s["minLength"]; ok {
			minLen, _ := toInt(minL)
			if len(str) < minLen {
				errs = append(errs, fmt.Sprintf("%s: string length %d is less than min %d", path, len(str), minLen))
			}
		}
		if maxL, ok := s["maxLength"]; ok {
			maxLen, _ := toInt(maxL)
			if len(str) > maxLen {
				errs = append(errs, fmt.Sprintf("%s: string length %d is greater than max %d", path, len(str), maxLen))
			}
		}
	}

	// array constraints
	if arr, ok := instance.([]any); ok {
		if minI, ok := s["minItems"]; ok {
			minItems, _ := toInt(minI)
			if len(arr) < minItems {
				errs = append(errs, fmt.Sprintf("%s: array length %d is less than min %d", path, len(arr), minItems))
			}
		}
		if maxI, ok := s["maxItems"]; ok {
			maxItems, _ := toInt(maxI)
			if len(arr) > maxItems {
				errs = append(errs, fmt.Sprintf("%s: array length %d is greater than max %d", path, len(arr), maxItems))
			}
		}
	}

	// required
	if required, ok := s["required"]; ok {
		reqArr, ok := required.([]any)
		if ok {
			if obj, ok := instance.(map[string]any); ok {
				for _, req := range reqArr {
					reqStr, _ := req.(string)
					if _, exists := obj[reqStr]; !exists {
						errs = append(errs, fmt.Sprintf("%s: missing required property %q", path, reqStr))
					}
				}
			}
		}
	}

	// properties
	if props, ok := s["properties"]; ok {
		propsMap, ok := props.(map[string]any)
		if ok {
			if obj, ok := instance.(map[string]any); ok {
				for key, propSchema := range propsMap {
					if val, exists := obj[key]; exists {
						subErrs := validateAgainstSchema(root, propSchema, val, path+"/"+key)
						errs = append(errs, subErrs...)
					}
				}
			}
		}
	}

	// items
	if itemsSchema, ok := s["items"]; ok {
		if arr, ok := instance.([]any); ok {
			for i, item := range arr {
				subErrs := validateAgainstSchema(root, itemsSchema, item, fmt.Sprintf("%s[%d]", path, i))
				errs = append(errs, subErrs...)
			}
		}
	}

	return errs
}

func resolveRef(root map[string]any, ref string) any {
	if !strings.HasPrefix(ref, "#/") {
		return root
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	current := any(root)
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		val, exists := m[part]
		if !exists {
			return nil
		}
		current = val
	}
	return current
}

func isBool(v any) bool {
	_, ok := v.(bool)
	return ok
}

func equal(a, b any) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	}
	return 0, false
}

func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	}
	return 0, false
}

func hasErrors(errs []LintError) bool {
	for _, e := range errs {
		if e.Severity == "error" {
			return true
		}
	}
	return false
}

var ticketIDPattern = regexp.MustCompile(`^T\d{3,}$`)

// Rule 1: id regex ^T\d{3,}$, no duplicates
func validateRule1(m *Manifest) []LintError {
	var errs []LintError
	seen := make(map[string]string) // id -> first ticket index for duplicate

	for _, t := range m.Tickets {
		if !ticketIDPattern.MatchString(t.ID) {
			errs = append(errs, LintError{
				TicketID: t.ID,
				Rule:     1,
				Severity: "error",
				Message:  fmt.Sprintf("id %q does not match pattern ^T\\d{3,}$", t.ID),
			})
		}
		if prev, exists := seen[t.ID]; exists {
			errs = append(errs, LintError{
				TicketID: t.ID,
				Rule:     1,
				Severity: "error",
				Message:  fmt.Sprintf("duplicate id %q (first seen at %s)", t.ID, prev),
			})
		} else {
			seen[t.ID] = t.ID
		}
	}
	return errs
}

// Rule 2: path/target_files/test_files/context_files exist on disk
func validateRule2(m *Manifest, baseDir string) []LintError {
	var errs []LintError

	checkFile := func(ticketID string, field string, path string, checkParentDir bool) {
		if path == "" {
			return
		}
		info, err := os.Stat(path)
		if err == nil {
			if checkParentDir {
				return // file exists; for target_files, that's fine
			}
			if info.IsDir() {
				errs = append(errs, LintError{
					TicketID: ticketID, Rule: 2, Severity: "error",
					Message: fmt.Sprintf("%s %q is a directory, not a file", field, path),
				})
			}
			return
		}
		if checkParentDir {
			// target_files: check that parent dir exists
			parent := path
			for i := len(parent) - 1; i >= 0; i-- {
				if parent[i] == '/' {
					parent = parent[:i]
					break
				}
			}
			if parent == path {
				return // no parent to check
			}
			if _, err := os.Stat(parent); os.IsNotExist(err) {
				errs = append(errs, LintError{
					TicketID: ticketID, Rule: 2, Severity: "error",
					Message: fmt.Sprintf("%s parent directory %q does not exist", field, parent),
				})
			}
		} else {
			errs = append(errs, LintError{
				TicketID: ticketID, Rule: 2, Severity: "error",
				Message: fmt.Sprintf("%s %q does not exist", field, path),
			})
		}
	}

	for _, t := range m.Tickets {
		checkFile(t.ID, "path", t.Path, false)
		for _, f := range t.TargetFiles {
			checkFile(t.ID, "target_file", f, true)
		}
		for _, f := range t.TestFiles {
			checkFile(t.ID, "test_file", f, true)
		}
		for _, f := range t.ContextFiles {
			checkFile(t.ID, "context_file", f, false)
		}
	}
	return errs
}

// Rule 3: depends_on references are known ids; DAG acyclic
func validateRule3(m *Manifest) []LintError {
	var errs []LintError
	idSet := make(map[string]bool)
	for _, t := range m.Tickets {
		idSet[t.ID] = true
	}

	for _, t := range m.Tickets {
		for _, dep := range t.DependsOn {
			if !idSet[dep] {
				errs = append(errs, LintError{
					TicketID: t.ID, Rule: 3, Severity: "error",
					Message: fmt.Sprintf("depends_on %q is not a known ticket id", dep),
				})
			}
		}
	}

	// Check for cycles using Tarjan SCC
	cycles := findCycles(m)
	if len(cycles) > 0 {
		for _, cycle := range cycles {
			errs = append(errs, LintError{
				TicketID: "",
				Rule:     3,
				Severity: "error",
				Message:  fmt.Sprintf("DAG cycle detected: %v", cycle),
			})
		}
	}
	return errs
}

// Tarjan SCC algorithm to find cycles (SCCs with size > 1 or self-loops).
func findCycles(m *Manifest) [][]string {
	index := 0
	stack := []string{}
	onStack := make(map[string]bool)
	indices := make(map[string]int)
	lowlink := make(map[string]int)

	var cycles [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		// Find neighbors from depends_on
		for _, t := range m.Tickets {
			if t.ID == v {
				for _, dep := range t.DependsOn {
					if _, exists := indices[dep]; !exists {
						strongconnect(dep)
						if lowlink[dep] < lowlink[v] {
							lowlink[v] = lowlink[dep]
						}
					} else if onStack[dep] {
						if indices[dep] < lowlink[v] {
							lowlink[v] = indices[dep]
						}
					}
				}
				break
			}
		}

		if lowlink[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			if len(scc) > 1 {
				cycles = append(cycles, scc)
			}
		}
	}

	for _, t := range m.Tickets {
		if _, exists := indices[t.ID]; !exists {
			strongconnect(t.ID)
		}
	}
	return cycles
}

// Rule 4: target_files overlap rule — if A and B share any target_files entry,
// one must be in the transitive depends_on of the other.
func validateRule4(m *Manifest) []LintError {
	var errs []LintError

	// Build file->tickets map
	fileOwners := make(map[string][]string)
	for _, t := range m.Tickets {
		for _, f := range t.TargetFiles {
			fileOwners[f] = append(fileOwners[f], t.ID)
		}
	}

	// Build reachability map via Floyd-Warshall
	reachable := buildReachability(m)

	for _, owners := range fileOwners {
		if len(owners) < 2 {
			continue
		}
		for i := 0; i < len(owners); i++ {
			for j := i + 1; j < len(owners); j++ {
				a, b := owners[i], owners[j]
				if reachable[a][b] || reachable[b][a] {
					continue
				}
				errs = append(errs, LintError{
					TicketID: a,
					Rule:     4,
					Severity: "error",
					Message:  fmt.Sprintf("shares target_file with %s without a depends_on edge (path: %s)", b, findSharedFile(a, b, m)),
				})
			}
		}
	}
	return errs
}

func findSharedFile(a, b string, m *Manifest) string {
	afiles := make(map[string]bool)
	for _, t := range m.Tickets {
		if t.ID == a {
			for _, f := range t.TargetFiles {
				afiles[f] = true
			}
		}
	}
	for _, t := range m.Tickets {
		if t.ID == b {
			for _, f := range t.TargetFiles {
				if afiles[f] {
					return f
				}
			}
		}
	}
	return "?"
}

func buildReachability(m *Manifest) map[string]map[string]bool {
	ids := make([]string, len(m.Tickets))
	idIdx := make(map[string]int)
	for i, t := range m.Tickets {
		ids[i] = t.ID
		idIdx[t.ID] = i
	}
	n := len(ids)
	// Initialize matrix
	reach := make([][]bool, n)
	for i := 0; i < n; i++ {
		reach[i] = make([]bool, n)
		reach[i][i] = true
	}
	// Direct edges
	for _, t := range m.Tickets {
		src := idIdx[t.ID]
		for _, dep := range t.DependsOn {
			if dst, ok := idIdx[dep]; ok {
				reach[src][dst] = true
			}
		}
	}
	// Floyd-Warshall
	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				if reach[i][k] && reach[k][j] {
					reach[i][j] = true
				}
			}
		}
	}

	result := make(map[string]map[string]bool, n)
	for i, id := range ids {
		result[id] = make(map[string]bool, n)
		for j := 0; j < n; j++ {
			result[id][ids[j]] = reach[i][j]
		}
	}
	return result
}

// Rule 5: context_budget_tokens <= policy.complexity_budgets[complexity].context
func validateRule5(m *Manifest) []LintError {
	var errs []LintError
	for _, t := range m.Tickets {
		budget, ok := m.Policy.ComplexityBudgets[t.Complexity]
		if !ok {
			errs = append(errs, LintError{
				TicketID: t.ID, Rule: 5, Severity: "error",
				Message: fmt.Sprintf("unknown complexity tier %q", t.Complexity),
			})
			continue
		}
		if t.ContextBudgetTokens > budget.Context {
			errs = append(errs, LintError{
				TicketID: t.ID, Rule: 5, Severity: "error",
				Message: fmt.Sprintf("context_budget_tokens %d exceeds tier %s budget %d", t.ContextBudgetTokens, t.Complexity, budget.Context),
			})
		}
	}
	return errs
}

var exactGates = map[string]bool{
	"go-vet":           true,
	"go-test":          true,
	"race":             true,
	"bench":            true,
	"interop-aria2c":   true,
	"go-build":         true,
	"go-vet-adr-check": true,
}

// Rule 6: gates drawn from enum
func validateRule6(m *Manifest) []LintError {
	var errs []LintError
	for _, t := range m.Tickets {
		for _, gate := range t.Gates {
			if exactGates[gate] {
				continue
			}
			// Check fuzz-<name>-<duration> pattern
			if strings.HasPrefix(gate, "fuzz-") {
				parts := strings.SplitN(gate, "-", 3)
				if len(parts) == 3 && len(parts[1]) > 0 && len(parts[2]) > 0 {
					continue
				}
			}
			errs = append(errs, LintError{
				TicketID: t.ID, Rule: 6, Severity: "error",
				Message: fmt.Sprintf("unknown gate %q", gate),
			})
		}
	}
	return errs
}

// Rule 7: module references resolve
func validateRule7(m *Manifest) []LintError {
	var errs []LintError
	moduleSet := make(map[string]bool)
	for _, mod := range m.Modules {
		moduleSet[mod.ID] = true
	}
	for _, t := range m.Tickets {
		if !moduleSet[t.Module] {
			errs = append(errs, LintError{
				TicketID: t.ID, Rule: 7, Severity: "error",
				Message: fmt.Sprintf("module %q is not listed in modules", t.Module),
			})
		}
	}
	return errs
}

// Rule 8: status=done requires claimed_by != null
func validateRule8(m *Manifest) []LintError {
	var errs []LintError
	for _, t := range m.Tickets {
		if t.Status == "done" && (t.ClaimedBy == nil || *t.ClaimedBy == "") {
			errs = append(errs, LintError{
				TicketID: t.ID, Rule: 8, Severity: "error",
				Message: "status=done but claimed_by is null",
			})
		}
	}
	return errs
}

// Rule 9: in_progress tickets past claimed_at + claim_ttl_seconds warn (error in --strict)
func validateRule9(m *Manifest, strict bool) []LintError {
	var errs []LintError
	severity := "warning"
	if strict {
		severity = "error"
	}
	now := time.Now().UTC()
	for _, t := range m.Tickets {
		if t.Status != "in_progress" {
			continue
		}
		if t.ClaimedAt == nil || *t.ClaimedAt == "" {
			continue
		}
		claimedTime, err := time.Parse(time.RFC3339, *t.ClaimedAt)
		if err != nil {
			continue
		}
		expiry := claimedTime.Add(time.Duration(t.ClaimTTLSeconds) * time.Second)
		if now.After(expiry) {
			errs = append(errs, LintError{
				TicketID: t.ID, Rule: 9, Severity: severity,
				Message: fmt.Sprintf("in_progress ticket claim expired at %s (TTL: %ds)", expiry.Format(time.RFC3339), t.ClaimTTLSeconds),
			})
		}
	}
	return errs
}

// Rule 10: priority=1 tickets must be on the longest critical path
func validateRule10(m *Manifest) []LintError {
	var errs []LintError
	criticalSet := computeCriticalPathSet(m)
	for _, t := range m.Tickets {
		if t.Priority == 1 && !criticalSet[t.ID] {
			errs = append(errs, LintError{
				TicketID: t.ID, Rule: 10, Severity: "error",
				Message: "priority=1 but ticket is not on the longest critical path",
			})
		}
	}
	return errs
}

// computeCriticalPath returns the length of the longest path in the DAG (in tickets).
func computeCriticalPath(m *Manifest) int {
	topo := topologicalSort(m)
	if len(topo) == 0 {
		return 0
	}
	dist := make(map[string]int)
	for _, id := range topo {
		dist[id] = 1 // each ticket counts as at least 1
	}
	maxLen := 1
	for _, id := range topo {
		if dist[id] > maxLen {
			maxLen = dist[id]
		}
		// Find outgoing edges (tickets that depend on this)
		for _, t := range m.Tickets {
			for _, dep := range t.DependsOn {
				if dep == id {
					if dist[id]+1 > dist[t.ID] {
						dist[t.ID] = dist[id] + 1
					}
				}
			}
		}
	}
	return maxLen
}

// computeCriticalPathSet returns the set of ticket IDs on any longest path.
func computeCriticalPathSet(m *Manifest) map[string]bool {
	topo := topologicalSort(m)
	if len(topo) == 0 {
		return nil
	}
	// Forward DP: longest path from any root to each node
	fwd := make(map[string]int)
	for _, id := range topo {
		fwd[id] = complexityWeight(m, id)
	}
	for _, id := range topo {
		for _, t := range m.Tickets {
			for _, dep := range t.DependsOn {
				if dep == id {
					candidate := fwd[id] + complexityWeight(m, t.ID)
					if candidate > fwd[t.ID] {
						fwd[t.ID] = candidate
					}
				}
			}
		}
	}

	// Backward DP: longest path from each node to any leaf
	bwd := make(map[string]int)
	// Reverse toposort
	rev := make([]string, len(topo))
	copy(rev, topo)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	for _, id := range rev {
		bwd[id] = complexityWeight(m, id)
	}
	for _, id := range rev {
		for _, dep := range getDeps(m, id) {
			candidate := bwd[id] + complexityWeight(m, dep)
			if candidate > bwd[dep] {
				bwd[dep] = candidate
			}
		}
	}

	// Total longest path weight
	maxTotal := 0
	for _, id := range topo {
		total := fwd[id] + bwd[id] - complexityWeight(m, id)
		if total > maxTotal {
			maxTotal = total
		}
	}

	// Nodes on a longest path
	criticalSet := make(map[string]bool)
	for _, id := range topo {
		if fwd[id]+bwd[id]-complexityWeight(m, id) == maxTotal {
			criticalSet[id] = true
		}
	}
	return criticalSet
}

func complexityWeight(m *Manifest, id string) int {
	for _, t := range m.Tickets {
		if t.ID != id {
			continue
		}
		budget, ok := m.Policy.ComplexityBudgets[t.Complexity]
		if ok && budget.Total > 0 {
			return budget.Total
		}
		return 1
	}
	return 1
}

func getDeps(m *Manifest, id string) []string {
	for _, t := range m.Tickets {
		if t.ID == id {
			return t.DependsOn
		}
	}
	return nil
}

func topologicalSort(m *Manifest) []string {
	inDegree := make(map[string]int)
	adj := make(map[string][]string)
	for _, t := range m.Tickets {
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		for _, dep := range t.DependsOn {
			adj[dep] = append(adj[dep], t.ID)
			inDegree[t.ID]++
		}
	}

	var q []string
	for id, deg := range inDegree {
		if deg == 0 {
			q = append(q, id)
		}
	}

	var result []string
	for len(q) > 0 {
		u := q[0]
		q = q[1:]
		result = append(result, u)
		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				q = append(q, v)
			}
		}
	}
	return result
}

func isDAGAcyclic(m *Manifest) bool {
	return len(findCycles(m)) == 0
}

// tokenize approximates token count as ceil(len(bytes)/4).
func tokenize(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	return (len(data) + 3) / 4
}
