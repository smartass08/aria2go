---
id: T009
module: 00-bootstrap
complexity: L
priority: 1
depends_on: []
target_files: [plans/PLAN.md]
test_files: []
context_files:
  - ENTRYPOINT.md
  - /Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md
context_budget_tokens: 9000
gates: []
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T009: Copy master plan to plans/PLAN.md

## Goal
Replace the pre-Phase-0 placeholder at `plans/PLAN.md` with the **full master plan content** from the canonical location `/Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md`. After this ticket, `plans/PLAN.md` is the in-tree authoritative master plan.

## Why This Matters
Many downstream tickets list `plans/PLAN.md` (or specific sections of it) as `context_files`. Until this ticket lands, those tickets must reference the canonical file at an absolute path outside the project tree, which is fragile (paths leak the plan owner's home directory). Bringing the plan in-tree decouples agents from the plan owner's filesystem.

## Acceptance Criteria
1. `plans/PLAN.md` exists at the project root and contains the full content of `/Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md`.
2. Section numbering (0 through 25) and Appendices A/B/C are preserved.
3. The file content matches the source byte-for-byte except: (a) the trailing line `*End of master plan. After ExitPlanMode approval, Phase 0 decomposes this single file into the file tree under §23, and the source-truth/ folder (already created pre-approval) becomes the offline reference for all spec-author work.*` MAY be retained verbatim or updated to `*This is the in-tree master plan. The original lived at /Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md until T009 landed.*`.
4. The first line of the file remains `# aria2go — Master Plan` (no other top-level title).
5. After this ticket, all manifest tickets that previously listed `/Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md` in their `context_files` have those entries automatically replaced by `plans/PLAN.md` (use `plans/tools/orchestrator/manifest-lint --rewrite-canonical-plan-path` if it exists; otherwise hand-edit `plans/manifest.json` under lock).

## Contract Surface
- CLI: none
- RPC: none
- Session: none
- Config: none
- Fixtures: none

## Context (≤3 files)
- `ENTRYPOINT.md` — repo context.
- `/Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md` — the source content to copy.

## Implementation Notes
This is a mechanical copy, not a transcription with edits. Use `cp` or a single `Read` + `Write`:

```bash
cp "/Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md" "plans/PLAN.md"
```

Then update the trailing line per AC #3 if preferred (recommended for clarity).

After the copy, walk through `plans/manifest.json` under lock and replace every `context_files` entry that contains the absolute path with `plans/PLAN.md`. There are several such entries (T009 itself excepted — its frontmatter still references the absolute path because at execution time the in-tree copy did not yet exist; do not edit T009's frontmatter).

The master plan is ~30K tokens. This ticket pulls the entire file into the working set, which is why it is L-tier (context budget ≤ 3500 tokens for context… wait, that's a problem). See "Caveat" below.

### Caveat about context budget

The L-tier context budget is 3500 tokens, but the master plan alone is ~30K tokens. This ticket therefore is technically over budget. Two ways to handle:

1. **Recommended:** Do not actually load the master plan into context. The ticket's work is a verbatim file copy — `cp` does not require the file content to be loaded into your LLM context. So your effective context is just this ticket file + ENTRYPOINT.md (~10K). Within budget.
2. If your harness insists on loading every `context_files` entry into your context, recategorize this ticket as XL and re-claim.

When you submit, append `## Implementation Log` noting which option you used.

## Error Cases & Validation
- If the master plan file at the absolute path is missing, the plan owner has not yet synced state to the project. Block with `blocked_by: ["master plan absent at canonical path"]`.

## Out of Scope
- Decomposing the master plan into per-section files under `plans/decisions/`, `plans/modules/`, etc. That work happens in subsequent tickets (T012 for ADRs, etc.).
- Updating `ENTRYPOINT.md` references — those already point to `plans/PLAN.md`.

## References
- Master plan §23 (Critical files to be created post-approval) lists `aria2go/plans/PLAN.md` as a Phase-0 deliverable.

## Estimated Tokens
- Context: 1200 (ticket + ENTRYPOINT excerpt; master plan NOT loaded into context per Caveat option 1)
- Implementation: 200 (mechanical copy)
- Tests: 0
- Total: 1400
