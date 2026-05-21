---
id: T005
module: 00-bootstrap
complexity: S
priority: 3
depends_on: []
target_files: [CHANGELOG.md]
test_files: []
context_files:
  - ENTRYPOINT.md
context_budget_tokens: 600
gates: []
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T005: Initial CHANGELOG.md

## Goal
Create the initial `CHANGELOG.md` in Keep-a-Changelog format with a single "Unreleased" entry seeded with the Phase-0 scaffolding work.

## Acceptance Criteria
1. `CHANGELOG.md` exists at repo root.
2. Header text:
   ```
   # Changelog

   All notable changes to aria2go will be documented in this file.

   The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
   and this project adheres to **CalVer** versioning of the form `vYYYY.MM.PATCH`
   (see ADR-0015).
   ```
3. Single section: `## [Unreleased]` with sub-sections `### Added`, `### Changed`, `### Removed`, `### Fixed`. Only `Added` is populated with bullets representing the Phase-0 scaffold:
   - `aria2go/AGENTS.md` (root agent contract).
   - `aria2go/ENTRYPOINT.md` (comprehensive bootstrap guide for coding agents).
   - `aria2go/PROMPT_TEMPLATES.md` (kickoff prompts for the swarm).
   - `aria2go/source-truth/` (offline GPL'd reference: aria2, aria2-docs, BEPs).
   - `aria2go/plans/PLAN.md` (master plan placeholder).
   - `aria2go/plans/manifest.json` + schema + lock (ticket queue).
   - `aria2go/plans/TRACKING.md`, `plans/decisions/INDEX.md`.
   - First 30 manifest tickets defined.
4. ≤ 2 KB.

## Implementation Notes
Keep-a-Changelog format strictly. Do NOT add release versions yet — the first release will be tagged at the end of Phase 6.

CalVer pattern reference: ADR-0015 — `vYYYY.MM.PATCH`. The first release is targeted `v2026.NN.0` where NN is the month of Phase 6 completion.

## Out of Scope
- Setting up a release pipeline (Phase 6).
- Auto-generating CHANGELOG from PR titles (deferred).

## References
- ADR-0015 (versioning scheme)
- https://keepachangelog.com/en/1.1.0/

## Estimated Tokens
- Context: 300   Implementation: 200   Tests: 0   Total: 500
