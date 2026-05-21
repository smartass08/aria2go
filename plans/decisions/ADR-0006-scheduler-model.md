# ADR-0006 — Scheduler Model

## Status
Accepted (with spec expansion per Codex)

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0002 (concurrency model)
- ADR-0012 (channel vs mutex policy)

## Context
The scheduler governs which downloads are active, paused, or waiting, enforcing concurrency limits (`--max-connection-per-server`, `--bt-max-peers`, `--max-concurrent-downloads`). It must handle dynamic option changes, priority ordering, retries with backoff, and fair wake ordering under resource contention.

Codex reviewed the model as **SOUND** but required the scheduler SPEC to enumerate five explicit invariants.

## Decision
**Single scheduler goroutine** consuming a `notify` channel. Per-group state machine with states: `Waiting → Active → Paused → Complete/Error/Removed`, plus `Seeding` for BitTorrent. Semaphores enforce concurrency limits.

**Codex-required invariants** (enumerated in scheduler SPEC `## Invariants` block):

1. **Fairness** — wake order under resource contention is FIFO within priority bands.
2. **Dynamic option changes** — `changeGlobalOption` mid-flight propagates to affected groups without restarting the scheduler.
3. **Retry** — exponential backoff, respecting `--max-tries` and `--max-file-not-found`.
4. **Priority ordering** — `--bt-prioritize-piece` and `aria2.changePosition` requests are honored without deadlocking.
5. **Resource release** — every terminal/paused state releases all semaphore tokens and closes owned connections.

## Consequences

### Positive
- Single goroutine eliminates scheduling races and makes state transitions deterministic.
- Semaphore model naturally enforces all aria2 concurrency limits.
- Explicit invariants give tickets clear correctness criteria.

### Negative
- Single scheduler goroutine is a bottleneck if download count grows very large — mitigated by the fact aria2 rarely has more than a few hundred active downloads.
- Invariants 3 and 4 interact non-trivially (retry timing vs. priority ordering).

### Neutral
- Scheduler SPEC has a dedicated `## Invariants` block; all scheduler tickets reference it.

## Compliance Notes
- Tickets affected: All engine scheduler tickets.
- Modules affected: `internal/engine/scheduler.go`.
- Detection: scheduler unit tests must verify all five invariants.
