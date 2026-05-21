# ADR-0013 — Package Layout (internal vs pkg)

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0004 (BT engine boundary — contracts in internal/contracts/)

## Context
Go's `internal/` convention restricts import to the module's own packages; `pkg/` is the public surface. aria2 has demonstrated library demand (libaria2), so we expose a minimal frozen API. Everything else stays internal for refactor freedom.

## Decision
**Only `pkg/aria2go` is public**, with a frozen API surface: `Daemon`, `Config`, `Status`. This matches the Go convention for "library-first" modules: a single public package, minimal surface.

**All other packages under `internal/`** — engine, protocols, config, sessionfile, RPC, disk, etc. These can be refactored freely without breaking external consumers.

Public surface justification: aria2's C API (libaria2) demonstrates real demand for programmatic integration. `pkg/aria2go` satisfies that use case with a Go-idiomatic surface.

## Consequences

### Positive
- Refactor freedom for 40+ internal packages — no semver constraints, no deprecation process.
- Public API is intentionally minimal and reviewable (3 types + constructor).
- Go toolchain enforces the `internal/` boundary — external packages literally cannot import internal code.

### Negative
- Library consumers can't access internal types (e.g., torrent metadata, peer stats) — but those are unstable by design.
- If internal packages prove useful externally, they must be moved to `pkg/` with API stability review.

### Neutral
- Package count is ~40 under `internal/`; 1 under `pkg/`.

## Compliance Notes
- Tickets affected: All.
- Modules affected: All under `internal/`, `pkg/aria2go`.
- Detection: `go build ./...` verifies no external package can import `internal/`; reviewer checks `pkg/` API freezes.
