---
id: T011
module: 00-bootstrap
complexity: M
priority: 1
depends_on: []
blocked_by: ["human decision on §24 Q1 of master plan"]
target_files: [plans/decisions/ADR-0001-library-policy.md]
test_files: []
context_files:
  - /Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md
context_budget_tokens: 3500
gates: []
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T011: Write ADR-0001-library-policy.md (resolves path a vs b)

## Goal
Author the canonical `plans/decisions/ADR-0001-library-policy.md` capturing the project's library policy decision. This ADR fixes which third-party packages — if any — the project is allowed to import. Once accepted it is referenced by every implementation ticket and enforced by `plans/tools/orchestrator/adr-check`.

## Why This Matters
Library policy is the highest-leverage decision in the project. It determines whether `internal/ssh/` exists (path a) or `golang.org/x/crypto/ssh` is imported (path b), which moves the project size by ~6,000 LOC and ~25 tickets. Every other ADR that mentions imports (0008, 0022) cascades from this one.

## BLOCKER — REQUIRES HUMAN DECISION FIRST

This ticket is **blocked** until the human plan owner resolves master-plan §24 Q1. Codex strongly recommended path (b) (curated `x/*` shortlist). Do **not** unblock or claim until the manifest entry's `blocked_by` field is cleared by the human.

When unblocked, the human will indicate which path is final (a or b) in the ticket's `## Implementation Log` block OR in the manifest's `notes` field. Trust that signal; do not infer from elsewhere.

## Acceptance Criteria
1. `plans/decisions/ADR-0001-library-policy.md` exists.
2. Front matter follows the ADR template (`## Status`, `## Date`, `## Supersedes`, `## Related`, `## Context`, `## Decision`, `## Consequences`, `## Compliance Notes`).
3. `## Status` is exactly `Accepted` (not "Proposed" — by the time this ticket runs, the human has decided).
4. `## Decision` makes the chosen path unambiguous in declarative present tense.
   - If path (a) chosen: "aria2go uses only the Go standard library. No third-party imports of any kind, including no `golang.org/x/*`. SSH is implemented from scratch under `internal/ssh/`."
   - If path (b) chosen: "aria2go uses the Go standard library plus a curated `golang.org/x/*` shortlist: `golang.org/x/sys` (both unix and windows subpackages), `golang.org/x/crypto/ssh`, `golang.org/x/crypto/ssh/agent`, `golang.org/x/term`, `golang.org/x/net/idna`. No other third-party imports are permitted. Each `x/*` module is pinned to an exact version in `go.mod`."
5. `## Consequences` lists at least 4 positive, 3 negative, and 2 neutral consequences specific to the chosen path. Copy directly from master plan ADR-0001 §2.
6. `## Compliance Notes` cite:
   - Tickets affected: "all of them — every ticket's `target_files` must compile under the policy."
   - Modules affected: list under chosen path.
   - Detection: "`plans/tools/orchestrator/adr-check` parses `go.mod` and every `import` block under `internal/`; any path not on the shortlist fails CI."

## Contract Surface
- CLI: none
- RPC: none
- Session: none
- Config: none
- Fixtures: none

## Context (≤3 files)
- `/Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md` — master plan; §2 ADR-0001 has the full text for both paths plus the Codex recommendation; §24 Q1 has the verbatim Codex verbatim recommendation.

## Implementation Notes
This is a transcription ticket: the master plan §2 already contains the full text for both paths. Your job is to (a) read the human decision (path a or b) from the ticket's `blocked_by` clearance, (b) extract the corresponding text from master plan ADR-0001, (c) write it into `plans/decisions/ADR-0001-library-policy.md` using the standard ADR template, (d) ensure `## Status` says `Accepted` and the date is today (UTC).

Do not invent rationale, consequences, or compliance notes beyond what master plan §2 ADR-0001 provides. If you find yourself adding new reasoning, stop and consult the master plan.

If the human's clearance message is ambiguous (e.g., the `blocked_by` was cleared but no path indicated), re-block with `blocked_by: ["unclear which library path: a or b"]` and submit.

## Error Cases & Validation
- If the ADR template file at `plans/templates/adr-template.md` does not yet exist, use the inline template from master plan §14.4.
- If the `plans/decisions/` directory does not yet exist, create it.

## Out of Scope
- ADR-0002 through ADR-0023 (ticket T012).
- `plans/decisions/INDEX.md` (ticket T013).
- Updating `go.mod` (separate downstream work).

## References
- Master plan §2 (ADR-0001 full text for both paths)
- Master plan §24 Q1 (Codex's recommendation: pick path b)
- Master plan §14.4 (ADR template)

## Estimated Tokens
- Context: 2500   Implementation: 700   Tests: 0   Total: 3200
