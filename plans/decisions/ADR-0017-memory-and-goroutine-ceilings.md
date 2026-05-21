# ADR-0017 — Memory and Goroutine Ceilings

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0002 (concurrency model — 20K goroutine budget)
- ADR-0006 (scheduler model — semaphore enforcement)

## Context
aria2go, like aria2, is a long-running daemon. Without resource ceilings, a malicious or misconfigured setup could exhaust memory or goroutines, causing OOM kills or scheduler starvation. Some ceilings map to aria2's existing `--max-*` options; others are our own additions for Go-specific resources (goroutines, WS connections).

## Decision
Documented **soft caps** with defined behaviors when hit:

| Resource | Ceiling | Behavior when hit |
|---|---|---|
| Peer goroutines | 10K | Close oldest idle peer |
| WebSocket clients | 256 | Reject new with HTTP 503 |
| In-flight hooks | 1024 | Return `ErrHookQueueFull` |
| Concurrent downloads | `--max-concurrent-downloads` (default 5) | Queue as Waiting |
| Connections per server | `--max-connection-per-server` (default 1) | Queue segments |

Each ceiling is an `--max-*` option, matching aria2 where one exists, or our own option where aria2 has no equivalent (goroutines, WS clients).

## Consequences

### Positive
- Predictable resource usage — OOM and goroutine exhaustion are prevented by design.
- aria2-compatible ceilings for download and connection limits maintain behavioral parity.
- Go-specific ceilings (goroutines, WS) close gaps aria2 doesn't address.

### Negative
- Hard ceilings require careful tuning for high-load users. Defaults are conservative.
- "Close oldest idle peer" may drop active peers if all peers are active — needs peer-selection heuristic.

### Neutral
- Ceilings documented in one place; engine enforces via scheduler semaphores.

## Compliance Notes
- Tickets affected: All engine, BT peer, RPC transport, hookrunner tickets.
- Modules affected: `internal/engine`, `internal/protocol/bittorrent/peer`, `internal/rpc/transport`, `internal/hookrunner`.
- Detection: unit tests verify ceiling behaviors; stress tests verify 10K peer soak.
