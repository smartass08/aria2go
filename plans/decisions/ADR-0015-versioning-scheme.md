# ADR-0015 — Versioning Scheme

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0014 (go.mod versioning)
- ADR-0020 (reference aria2 version)

## Context
aria2go is a feature-clone of aria2. Semantic versioning (SemVer) tracks API changes, but our API surface (`pkg/aria2go`) is intentionally minimal and frozen. The primary version signal users care about is "which aria2 feature level do we match."

## Decision
**Calendar versioning (CalVer): `vYYYY.MM.PATCH`** (e.g., `v2026.05.0`).

- Major `YYYY.MM` — approximate release date, aligned with aria2 feature level.
- `PATCH` — bug fix increment within a month.

Rationale: This is a feature-clone. SemVer of a clone tracks aria2's feature changes, not our API. Users ask "does this match aria2 1.37?" — the answer is in the changelog and conformance score, not the version number.

**`pkg/aria2go` API stability** tracked separately via module path `/v2` when an incompatible API change is needed (rare, given the frozen surface).

## Consequences

### Positive
- Version encodes approximate recency without implying semver semantics.
- Avoids the awkwardness of "aria2go v3.2.1 matches aria2 1.37.0 features."
- Patch increments are clear: just bug fixes.

### Negative
- CalVer doesn't communicate breaking changes — requires explicit CHANGELOG entries.
- Go module versioning expects semver-like `vX.Y.Z` — CalVer is compatible (still `v<number>.<number>.<number>`) but may confuse tools that parse major version bumps.

### Neutral
- Same format as Ubuntu, many Go projects (e.g., `gopls`).

## Compliance Notes
- Tickets affected: None (versioning is a release-time concern).
- Modules affected: None.
- Detection: `git tag` naming enforced in CI release pipeline.
