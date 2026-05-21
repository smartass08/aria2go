# ADR-0016 — Clean-Room Process (HARDENED)

## Status
Accepted (hardened 2026-05-18 after Codex flagged contamination risk)

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0001 (library policy)
- ADR-0023 (source-truth boundary)
- plans/decisions/INDEX.md

## Context
aria2 is GPLv2+ licensed. aria2go is Apache-2.0 licensed, implemented as a clean-room rewrite. Multi-agent workflows increase contamination risk — small-context LLMs may accidentally paraphrase source they've read. Codex flagged this as a critical risk requiring hardening beyond basic "don't copy" guidance.

## Decision
Implementers may read aria2 C++ source under `source-truth/aria2/` for **behavior reference only**, but must produce code from English specifications under `plans/byte-compat/`. The flow is one-directional: `source-truth/` → English spec in `plans/byte-compat/` → ticket → implementation. Same flow for BEP specs and RFC text.

### Hardened Enforcement Rules (CI gate via `plans/tools/orchestrator/adr-check --source-truth`)

1. **Zero aria2 source LOC in implementation artifacts.** No verbatim source quotation in tickets, commits, or generated code. Hard cap: zero.

2. **At most 3 isolated source lines** in private analysis notes (e.g., a spec author's working notes under `plans/byte-compat/_notes/`), and only when the English spec is ambiguous and a literal byte sequence is required (e.g., a session-file separator). Notes containing source quotes are never imported into tickets.

3. **No copied comments, no copied tables, no translated function structures.** "If the resulting prose still reads like a translation of the C++, redo it" is the test.

4. **Author separation for high-risk areas.** The same person (and ideally the same agent) does NOT both author a spec from source AND implement the spec for the same area when the area is high-risk (BT peer wire, RPC dispatcher, session format, config parser). Review log tracks authorship — `plans/tools/orchestrator/adr-check` correlates spec-author git history with ticket-claimer manifest entries.

5. **CI scanner heuristics** (conservative; false positives reviewed by human):
   - GPL-header strings in any file under `internal/`, `pkg/`, `cmd/`.
   - Distinctive aria2 symbols: `DownloadEngine::`, `AbstractCommand::`, `BtRuntime::`, `RpcMethodFactory::`, `OptionHandlerFactory::`, `SessionSerializer::`, `MultiFileAllocationIterator::`, and ~50 more enumerated in `plans/tools/orchestrator/adr-check/aria2-symbols.txt`.
   - Diff-similarity > 30% between any added function in our code and any function in `source-truth/aria2/src/` (token-level, ignoring identifier names) — flag for review.
   - Verbatim aria2 comment text (top-K most distinctive comments fingerprinted).

6. **Violation = revert.** Any rule-1 or rule-3 violation requires the commit to be reverted, the affected area re-implemented from a fresh English spec, and a scanner regression test added.

**Two-stage authorship audit**: every ticket touching CLI/RPC/session/config records (in its frontmatter) `spec_author` and `implementer`. `plans/tools/orchestrator/adr-check` cross-checks that these are different agent_ids (or different humans) for the high-risk areas listed above.

## Consequences

### Positive
- Hard enforcement eliminates ambiguity — no judgment calls about "how much is too much."
- CI scanner catches contamination before it reaches human review.
- Author separation creates an audit trail and prevents self-review contamination.

### Negative
- CI scanner adds ~30 s to every PR run.
- False positives from CI scanner require human triage.
- Author separation increases coordination overhead — a spec author must hand off before implementation.

### Neutral
- Scanner heuristics are documented as conservative; false positive workflow includes human override.

## Compliance Notes
- Tickets affected: All.
- Modules affected: All.
- Detection: `plans/tools/orchestrator/adr-check --source-truth` (CI gate, blocking on PR merge); reviewer spot-check on contract-surface tickets.
