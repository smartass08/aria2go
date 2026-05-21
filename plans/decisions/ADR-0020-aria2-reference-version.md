# ADR-0020 — Reference Aria2 Version

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0007 (test strategy — conformance dual-run against pinned aria2c)
- ADR-0015 (versioning scheme)
- ADR-0023 (source-truth boundary — version bump procedures)

## Context
aria2go is a feature-clone of aria2. Conformance tests must target a specific aria2 version. As aria2 upstream evolves, aria2go may add features or tweak behavior to match newer releases, but the baseline must be pinned to avoid a moving target.

## Decision
**Pin all conformance tests to aria2 1.37.0** — the last upstream release (late 2024). The reference binary is the Debian package `1:1.37.0-1+b1`, used as a Docker image in conformance dual-run tests.

Any deviation from aria2 1.37.0 behavior is documented as either:
- A **feature-add** (something aria2 doesn't do that we do — e.g., better JSON formatting).
- A **waiver** (something aria2 does that we intentionally don't — documented in `plans/CONFORMANCE.md`).

Future minor upstream releases (e.g., aria2 1.38.0) trigger a bump-ADR to re-pin and re-evaluate conformance targets.

## Consequences

### Positive
- Single stable target for all conformance work — no chasing upstream changes during MVP.
- Debian package pin ensures deterministic binary behavior across CI runs.
- Waiver mechanism gives explicit permission to deviate with documentation.

### Negative
- If aria2 releases security-critical fixes post-1.37.0, we must decide whether to backport them before MVP+1.
- Bump-ADR process is manual and adds release overhead.

### Neutral
- Conformance target is documented in `plans/CONFORMANCE.md` (auto-generated).

## Compliance Notes
- Tickets affected: All conformance test tickets.
- Modules affected: None (conformance infra).
- Detection: CI conformance matrix pins aria2c version; deviation tracking in `plans/CONFORMANCE.md`.
