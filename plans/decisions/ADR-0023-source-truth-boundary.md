# ADR-0023 — Source-Truth Boundary

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0016 (clean-room process — enforcement)
- ADR-0001 (library policy)
- source-truth/README.md

## Context
Cloning aria2 (GPLv2+) source into `source-truth/aria2/` so coding agents can read it offline is useful for behavior reference but increases contamination risk vs. the prior plan where source-reading happened on the web with no local copy. Small-context LLMs especially can accidentally paraphrase source they've read. A strict boundary between source-truth and production code is needed.

## Decision
**`source-truth/` lives outside the Go build tree** and is **never** imported, embedded, or referenced from `internal/`, `pkg/`, `cmd/aria2go/`, or anything that ships.

- Only `plans/tools/orchestrator/adr-check --source-truth` (the license scanner) reads `source-truth/` programmatically.
- `plans/tools/orchestrator/spec-author` is the only tooling allowed to *generate* content using `source-truth/` as input — its output goes to `plans/byte-compat/*.md`. Agents do not run this; the plan owner does.
- A `.gitignore`-style exclusion list ensures the source-truth tree is not packaged in releases (`make ship-check` verifies absence).
- Source-truth root contains a `README.md` (already authored) declaring license boundaries and the hardened rules from ADR-0016. Every agent is **required** to read this README during boot (cached for session).
- CI scanner (`adr-check --source-truth`) runs on every PR. Triggers per ADR-0016's enumerated heuristics. **Blocking on PR merge.**
- Refresh procedure documented in the README. Version bumps require an ADR (e.g., a future ADR-0024-aria2-v1.38-bump).

## Consequences

### Positive
- Coding agents can grep/read aria2 source offline, removing WebFetch dependency.
- BEP and RFC reference text in one place, versioned with the project.
- License boundary is auditable: one tree, one scanner, one set of rules.

### Negative
- ~45 MB on disk; ~1 MB ongoing repo growth per upstream-version-bump (small).
- New contamination vector that didn't exist when source-reading happened on the web — mitigated by ADR-0016 enforcement.
- CI scanner adds ~30 s to every PR run.

### Neutral
- Source-truth is gitignored from release packaging but tracked in repo for agent access.

## Compliance Notes
- Tickets affected: All (the scanner runs on every PR).
- Modules affected: `plans/tools/orchestrator/adr-check`, `plans/tools/orchestrator/spec-author`.
- Module 17-test-fixtures must NOT pull from `source-truth/aria2/test/`; regenerate fresh fixtures per ADR-0016.
- Detection: `plans/tools/orchestrator/adr-check --source-truth` (CI gate); `make ship-check` verifies absence from release artifacts.
