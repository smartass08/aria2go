---
id: T001
module: 00-bootstrap
complexity: S
priority: 1
depends_on: []
target_files: [go.mod]
test_files: []
context_files:
  - ENTRYPOINT.md
context_budget_tokens: 1000
gates: [go-build]
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T001: Create go.mod with module path, go directive, toolchain pin

## Goal
Create the initial `go.mod` file at the repo root that declares the module path, the minimum Go language version, and a toolchain pin for reproducible builds. After this ticket, `go build ./...` is well-defined at the project root (even though there is no Go code yet — only an empty module).

## Why This Matters
Every subsequent Go-creating ticket depends on the module declaration. The toolchain pin (per ADR-0014) guarantees every CI runner and developer machine uses the same Go version, which is essential for reproducible builds and for `testing/synctest` once it graduates in Go 1.25.

## Acceptance Criteria
1. `go.mod` exists at the project root with exactly these two declarations:
   - `module github.com/smartass08/aria2go`
   - `go 1.24`
2. A `toolchain` directive is present: `toolchain go1.25.3` (current stable). If a newer 1.25.x patch is available, use it.
3. `require` block is empty (no third-party imports yet). Under library path (a) it remains empty; under path (b) it will be populated by later tickets with the four curated `x/*` modules.
4. Running `go build ./...` at the repo root exits 0 (no errors, no warnings about missing packages — there are no packages yet, which is fine).
5. Running `go mod verify` exits 0.

## Contract Surface
- CLI: none
- RPC: none
- Session: none
- Config: none
- Fixtures: none

## Context (≤3 files)
- `ENTRYPOINT.md` — project context, module path, Go version target.
- ADR-0014 (when it exists post-T012): the rationale for the module path and toolchain pin.

## Implementation Notes
Use exactly the following content (do not include any other directives, comments, or stub `require` lines):

```
module github.com/smartass08/aria2go

go 1.24

toolchain go1.25.3
```

If `go1.25.3` is no longer the current Go release at execution time, use the highest 1.25.x available via `go env GOTOOLCHAIN` or the official Go release index. Do not select 1.26+ — `go 1.24` directive caps the language version and `toolchain` ≥ `go 1.25` is intended.

Do NOT run `go mod tidy` — there are no imports yet; `tidy` will add nothing and is unnecessary noise.

Do NOT add a `replace` directive.

Do NOT add `require` entries; later tickets (notably T012's path b ADR work) will add them.

## Error Cases & Validation
- If `go.mod` already exists from a prior aborted attempt, overwrite it cleanly with the exact content above.
- If the host's Go toolchain is < 1.25, `go build ./...` may print a warning about needing to download a newer toolchain; that is acceptable and resolves on first build.

## Out of Scope
- `go.sum` (created lazily by Go on first `go get`).
- `vendor/` directory (we do not vendor; ADR-0014).
- Any `.go` source files.

## References
- ADR-0014 (go.mod versioning) — master plan §2.
- Master plan §3 (top-level module layout).

## Estimated Tokens
- Context: 800   Implementation: 100   Tests: 0   Total: 900
