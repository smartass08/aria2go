# ADR-0008 — x/* Allowlist Policy

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0001 (library policy — path a makes this ADR simple)
- ADR-0023 (source-truth boundary)

## Context
Under path (a) (strict stdlib-only), no `golang.org/x/*` packages are permitted in production code. This ADR formalizes the enforcement mechanism. Under path (b), this ADR would enumerate the four allowed x/* packages (`x/sys`, `x/crypto/ssh`, `x/crypto/ssh/agent`, `x/term`, `x/net/idna`).

## Decision
**Path (a) baseline**: The x/* allowlist is empty. No `golang.org/x/*` imports are permitted in any production code under `internal/`, `pkg/`, or `cmd/aria2go/`.

`plans/tools/orchestrator/adr-check` parses `go.mod` and every `import` block under `internal/`; any import path not in the Go standard library fails CI.

**Path (b) alternative**: If the library policy flips to path (b), the allowlist becomes the four packages specified in ADR-0001. `go.mod` must pin exact x/* versions. All other `golang.org/x/*` packages remain forbidden.

## Consequences

### Positive
- Binary enforcement: `adr-check` makes the policy a CI gate, not a human convention.
- Empty allowlist simplifies everything — no NOTICE file, no license review, no version tracking.

### Negative
- Test rigs under `test/rig/` need explicit exemption for x/crypto/ssh (per ADR-0007).
- Path (b) users must maintain exact version pins and dependabot equivalents.

### Neutral
- Same enforcement tooling serves both paths.

## Compliance Notes
- Tickets affected: All.
- Modules affected: All under `internal/`, `pkg/`, `cmd/aria2go/` (production code).
- Detection: `plans/tools/orchestrator/adr-check` (CI gate, blocking on PR merge).
