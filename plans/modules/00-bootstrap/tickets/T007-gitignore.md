---
id: T007
module: 00-bootstrap
complexity: S
priority: 2
depends_on: []
target_files: [.gitignore]
test_files: []
context_files:
  - ENTRYPOINT.md
context_budget_tokens: 500
gates: []
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T007: .gitignore for Go project + project-specific volatile state

## Goal
Create the `.gitignore` at the repo root with standard Go patterns plus aria2go-specific volatile file patterns (manifest.lock body, pprof outputs, test artifacts, fuzz crashers).

## Acceptance Criteria
1. `.gitignore` exists at repo root.
2. Includes (in order, with section comments):
   - Standard Go: `*.exe`, `*.dll`, `*.so`, `*.dylib`, `*.test`, `*.out`, `coverage.out`, `coverage.html`, `*.prof`, `__debug_bin*`, `*.swp`, `*~`, `.DS_Store`, `Thumbs.db`.
   - Build artifacts: `/dist/`, `/build/`, `/aria2c` (the built binary), `/aria2go-orch` (orchestrator multi-binary), `bin/`.
   - Vendoring: `/vendor/` (we do not vendor; ADR-0014).
   - Coverage: `/cover/`, `coverage*.txt`.
   - Profiling: `cpu.prof`, `mem.prof`, `trace.out`, `block.prof`, `mutex.prof`, `goroutine.prof`.
   - Manifest lock body (the file persists in git as an empty marker, but its body is volatile): NOT in gitignore — handled by .gitattributes ignoring content changes (or git-lfs-like mechanism; ticket T-future).
   - Test outputs and fuzz: `**/testdata/fuzz/Fuzz*/seedXXX*` (anything not committed via `go test -fuzz` corpus minimization), `crashers/`, `suppressions/`.
   - IDE: `.idea/`, `.vscode/settings.json` (allow `.vscode/extensions.json`), `*.code-workspace`.
3. Does NOT ignore: `LICENSE`, `NOTICE`, `README.md`, `go.mod`, `go.sum`, `plans/**`, `source-truth/**`, `ENTRYPOINT.md`, `AGENTS.md`, `PROMPT_TEMPLATES.md`.

## Implementation Notes
Reference: github.com/github/gitignore/blob/main/Go.gitignore for the standard Go patterns (NOT for copy-paste — just to verify completeness).

`.DS_Store` and `Thumbs.db` cover macOS and Windows respectively. `*~` and `*.swp` cover Vim. `.idea/` covers JetBrains.

The `vendor/` directory line is to defend against accidental `go mod vendor`; ADR-0014 forbids vendoring.

## Out of Scope
- `.gitattributes` for line-ending normalization (separate ticket).
- `.editorconfig` (separate ticket).

## References
- ADR-0014 (no vendoring)
- ENTRYPOINT.md

## Estimated Tokens
- Context: 300   Implementation: 150   Tests: 0   Total: 450
