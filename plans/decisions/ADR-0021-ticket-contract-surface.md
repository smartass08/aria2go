# ADR-0021 — Ticket Contract Surface

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0004 (BT engine boundary — contracts in internal/contracts/)
- ADR-0016 (clean-room process — author separation for high-risk areas)
- plans/manifest.json (contract_surface field on every ticket)

## Context
Codex review identified that tickets touching compatibility-critical surfaces (CLI flags, RPC methods, session fields, config options) are high-risk — a single typo in an RPC method name or a missing config field can break aria2 compatibility. These tickets need a mandatory review gate before implementation begins.

## Decision
Every ticket has a mandatory **`## Contract Surface`** section (manifest field: `contract_surface`) listing:

- **`cli`**: affected CLI flag(s)
- **`rpc`**: affected RPC method(s)
- **`session`**: affected session-file field(s)
- **`config`**: affected config option(s)
- **`fixtures`**: required test fixture file(s)

Tickets where any `contract_surface` field is non-empty trigger a **mandatory human-review gate** before coding begins. Status flow: `pending` → human approval → `in_progress`.

Tickets with all-empty `contract_surface` (internal refactors, utility code, test-only changes) skip the human gate.

## Consequences

### Positive
- Human eye on every compat-critical change before a single line of code is written.
- Contract surface acts as a checklist — ticket authors must think about compat impact up front.
- Manifest enforces the gate: `pending` status with non-empty contract surface blocks `in_progress`.

### Negative
- Adds process overhead — human reviewers are a bottleneck.
- Empty contract surface for non-critical tickets may tempt authors to under-specify.

### Neutral
- Contract surface is part of every ticket template; no extra artifact needed.

## Compliance Notes
- Tickets affected: All.
- Modules affected: All.
- Detection: `plans/tools/orchestrator/manifest-lint` validates contract surface presence; `plans/tools/orchestrator/tracking-render` renders status.
