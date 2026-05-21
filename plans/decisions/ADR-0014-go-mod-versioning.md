# ADR-0014 — go.mod Versioning

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0015 (versioning scheme)
- ADR-0001 (library policy — affects require block)

## Context
The module must be versioned for `go get` compatibility with the Go ecosystem. Module path, Go directive, toolchain pin, and vendoring strategy must be decided.

## Decision
**Module path**: `github.com/smartass08/aria2go`.

**Go directive**: `1.24` (minimum Go version required to build).

**Toolchain**: `go1.25.x` for deterministic builds across environments.

**No vendoring** — `go.sum` is sufficient for dependency verification (under path b for x/* packages). Under path (a), there are no external dependencies, so `go.mod` has no `require` block and `go.sum` is minimal or absent.

For path (b): pin `golang.org/x/*` to exact versions in `go.mod`; refresh via dependabot equivalent.

## Consequences

### Positive
- Standard module path for `go get`.
- Toolchain pin ensures reproducible builds regardless of developer's local Go version.
- No vendoring reduces repo size and merge conflicts.

### Negative
- `go1.25.x` requires Go 1.25 toolchain to be available in CI and dev environments.
- Under path (b), version pins must be maintained — stale pins risk security issues.

### Neutral
- Module path is the standard `github.com/<org>/<repo>` convention.

## Compliance Notes
- Tickets affected: T001 (go.mod creation).
- Modules affected: Root `go.mod`.
- Detection: `go mod verify` (CI gate); `go vet ./...` ensures Go directive compat.
