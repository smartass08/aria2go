---
id: T024
module: 00-bootstrap
complexity: L
priority: 1
depends_on: [T001]
target_files:
  - plans/tools/orchestrator/manifest-lint/main.go
  - plans/tools/orchestrator/internal/manifest/manifest.go
  - plans/tools/orchestrator/internal/manifest/lint.go
test_files:
  - plans/tools/orchestrator/manifest-lint/main_test.go
  - plans/tools/orchestrator/internal/manifest/lint_test.go
context_files:
  - plans/manifest.schema.json
  - ENTRYPOINT.md
context_budget_tokens: 4000
gates: [go-vet, go-test, race]
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T024: Build plans/tools/orchestrator/manifest-lint/main.go

## Goal
Implement the `manifest-lint` orchestrator binary that validates `plans/manifest.json` against `plans/manifest.schema.json` and enforces the 10 manifest invariants documented in `ENTRYPOINT.md` §10. After this ticket, CI (and every agent's pre-claim check) can rely on `./plans/tools/orchestrator/manifest-lint --strict` exiting non-zero on any rule violation.

## Why This Matters
The manifest is the single source of truth for the queue. If it becomes inconsistent, every agent's selection logic breaks. `manifest-lint` is the guardrail. Without it, ticket-expand and human edits can corrupt the manifest silently.

## Acceptance Criteria
1. `plans/tools/orchestrator/manifest-lint/main.go` declares `package main`, parses CLI flags via `flag`, and exits 0 on success and 1 on any violation (printing one violation per line on stderr with the ticket id + rule number).
2. Supported flags:
   - `--strict` (default false): fail on warnings too.
   - `--fix` (default false): apply auto-fixable corrections (e.g., sort tickets by id, normalize whitespace in `notes`) and print the diff.
   - `--rebuild` (default false): regenerate the `tickets` array from ticket frontmatter under `plans/modules/*/tickets/*.md`, preserving runtime state (`status`, `claimed_by`, `claimed_at`).
   - `--manifest path` (default `plans/manifest.json`).
   - `--schema path` (default `plans/manifest.schema.json`).
3. Implements all 10 ENTRYPOINT.md §10 rules:
   1. `id` regex match (`^T\d{3,}$`), no duplicates.
   2. All `path`, `target_files`, `test_files`, `context_files` exist on disk OR (for `target_files` only) parent directory exists.
   3. `depends_on` references are known ids; DAG acyclic (Tarjan SCC singletons).
   4. `target_files` overlap rule: if A and B share any entry, A must be in deps(B) or vice-versa.
   5. `context_budget_tokens` ≤ `policy.complexity_budgets[complexity].context`.
   6. `gates` drawn from a finite enum (`go-vet`, `go-test`, `race`, `fuzz-<name>-<duration>`, `bench`, `interop-aria2c`, `go-build`, `go-vet-adr-check`).
   7. Module references resolve.
   8. `status=done` requires `claimed_by != null` and at least one CI reference in `notes`.
   9. No `in_progress` ticket past `claimed_at + claim_ttl_seconds` (warns in default mode, errors in `--strict`).
   10. `priority=1` tickets are on the longest DAG path (computed via topological longest-path with weight = complexity-budget total).
4. JSON Schema validation step uses the standard library only. Implement a minimal JSON Schema draft-2020-12 validator covering `type`, `enum`, `const`, `pattern`, `format`, `required`, `properties`, `items`, `$ref`, `$defs`, `minimum`, `maximum`, `minLength`, `maxLength`, `minItems`, `maxItems`. Full draft-2020-12 not required — only the subset used by `manifest.schema.json`.
5. Tokenizer for budget enforcement (rule 5): approximate token count = `ceil(len(bytes) / 4)`. Document this approximation in the binary's README comment; later tickets may swap in a tiktoken-style estimator.
6. Tests:
   - Unit tests for each of the 10 rules with a small set of pass/fail manifest fixtures under `plans/tools/orchestrator/manifest-lint/testdata/`.
   - Race-detector clean.
   - `go test -cover ./plans/tools/orchestrator/manifest-lint/...` ≥ 85%.
7. Output of a successful run (no flags): single line `manifest OK: <N> tickets, <M> modules, DAG acyclic, longest critical path: <P> tickets`. Stderr empty.

## Contract Surface
- CLI: none (this is a developer tool, not the aria2c CLI)
- RPC: none
- Session: none
- Config: none
- Fixtures: `plans/tools/orchestrator/manifest-lint/testdata/*.json`

## Context (≤3 files)
- `plans/manifest.schema.json` — the JSON Schema this binary validates against.
- `ENTRYPOINT.md` — the 10 invariant rules in §10 are the source of truth.

## Implementation Notes
- **No third-party imports.** ADR-0001 applies even to orchestrator tooling. Use only the Go stdlib.
- Package layout:
  ```
  plans/tools/orchestrator/manifest-lint/main.go        — flag parsing, exit codes
  plans/tools/orchestrator/internal/manifest/manifest.go — types + (de)serialization
  plans/tools/orchestrator/internal/manifest/lint.go     — the 10 rules + helpers
  ```
- `manifest.go` defines:
  ```go
  type Manifest struct {
      SchemaVersion string  `json:"schema_version"`
      Project       string  `json:"project"`
      GeneratedAt   string  `json:"generated_at"`
      Generator     string  `json:"generator"`
      Policy        Policy  `json:"policy"`
      Modules       []Module `json:"modules"`
      Tickets       []Ticket `json:"tickets"`
  }
  // ... Ticket, Module, Policy, ContractSurface, Budget types ...
  ```
- For the JSON Schema validator: a hand-rolled walker is fine. ~400 LOC.
- DAG check: Tarjan SCC. Standard algorithm; treat any SCC with size ≥ 2 as a cycle.
- Longest path on a DAG: topological sort + DP relaxation. O(V+E).
- Rule 4 (target_files overlap): for each pair (A, B) sharing any target_file, check reachability via `depends_on` in either direction. Build reachability map once via transitive closure (Floyd-Warshall is fine at this size; ~400 tickets).
- Rule 9 (TTL): take `time.Now().UTC()` minus `claimed_at`; if > `claim_ttl_seconds`, warn (default) or fail (--strict).
- Errors should reference the ticket id and the rule number for grep-ability:
  ```
  T037: rule 4 violation: shares target_file "internal/foo.go" with T088 without a depends_on edge
  ```

## Error Cases & Validation
- If `plans/manifest.json` is missing or invalid JSON, exit 2 with a single error line on stderr.
- If `plans/manifest.schema.json` is missing, exit 2 likewise.
- If `--fix` would alter the manifest but `--strict` is also set, refuse (`--fix` is incompatible with `--strict`); exit 2.
- If `--rebuild` is set but ticket files reference frontmatter fields not in the schema, list the unknown fields and exit 2.

## Out of Scope
- The `dag-validate` standalone binary (ticket T025). manifest-lint runs the DAG rule inline; dag-validate is a thinner standalone for use as a CI gate without other rules.
- The `tracking-render` binary (T028).
- LLM-driven `ticket-expand` (Phase 1).

## References
- ENTRYPOINT.md §10 (manifest schema + validation rules).
- Master plan §14.2 (manifest schema worked example).
- Master plan §15.4 (orchestrator subcommand table).

## Estimated Tokens
- Context: 2800   Implementation: 2200   Tests: 1000   Total: 6000
