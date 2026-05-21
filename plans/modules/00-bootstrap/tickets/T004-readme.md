---
id: T004
module: 00-bootstrap
complexity: S
priority: 2
depends_on: []
target_files: [README.md]
test_files: []
context_files:
  - ENTRYPOINT.md
context_budget_tokens: 1500
gates: []
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T004: Write project README.md

## Goal
Create the project's top-level `README.md`. It is the front door for humans visiting the repo (GitHub renders it). It must direct AI agents to `ENTRYPOINT.md` and humans to `plans/PLAN.md`.

## Why This Matters
The `README.md` is the first file shown on GitHub and is referenced by aggregators (go.dev, pkg.go.dev for `pkg/aria2go`). Keep it short and pointer-heavy; the real content lives elsewhere.

## Acceptance Criteria
1. `README.md` exists at the project root.
2. Contains, in order:
   - H1: `# aria2go`
   - A one-paragraph project description (100% feature clone of aria2, pure Go, Apache-2.0, etc.).
   - A status section noting "Pre-Phase-0 scaffold; not yet functional."
   - A "Start here" section with three bullets:
     - For coding agents: `ENTRYPOINT.md`
     - For humans: `plans/PLAN.md`
     - For orchestrators spawning agents: `PROMPT_TEMPLATES.md`
   - A "License" section noting Apache-2.0 (with link to `LICENSE`) and the clean-room boundary against aria2's GPLv2+ (with link to `source-truth/README.md`).
   - A "Reference" section noting aria2 1.37.0 is the behavior reference.
3. ≤ 2 KB total. No badges (CI/coverage badges added later by ticket T-future).
4. CommonMark; renders cleanly on GitHub.

## Contract Surface
- CLI: none
- RPC: none
- Session: none
- Config: none
- Fixtures: none

## Context
- `ENTRYPOINT.md` — project context.

## Implementation Notes
Keep it short. The `plans/PLAN.md` is for humans who want the full master plan; the `README.md` should NOT duplicate it.

Suggested skeleton:
```markdown
# aria2go

A 100%-feature clone of [aria2](https://aria2.github.io) (the C++ multi-protocol download utility) in pure Go. Apache-2.0, clean-room rewrite targeting aria2 1.37.0.

## Status
Pre-Phase-0 scaffold. Not yet functional. See `plans/PLAN.md` for the master plan and `plans/TRACKING.md` for live ticket status.

## Start here

- **Coding agent?** Read [`ENTRYPOINT.md`](./ENTRYPOINT.md) first.
- **Human contributor?** Read [`plans/PLAN.md`](./plans/PLAN.md).
- **Orchestrator spawning agents?** See [`PROMPT_TEMPLATES.md`](./PROMPT_TEMPLATES.md).

## License

aria2go is licensed under Apache-2.0 — see [`LICENSE`](./LICENSE).

aria2go is a clean-room rewrite. aria2's GPLv2+ C++ source under [`source-truth/aria2/`](./source-truth/README.md) is for behavior reference only; no source text may be copied. See ADR-0016 (post-T012) for the full clean-room policy.

## Reference

Behavioral parity is measured against **aria2 1.37.0** (Debian package `1:1.37.0-1+b1`). All conformance tests dual-run against a pinned aria2c container in CI.
```

## Error Cases
None — static file.

## Out of Scope
- CI badges, contributor lists, build status (deferred).
- Quickstart / installation instructions (Phase 6).

## References
- ENTRYPOINT.md
- Master plan §1, §23

## Estimated Tokens
- Context: 800   Implementation: 400   Tests: 0   Total: 1200
