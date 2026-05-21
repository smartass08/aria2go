# ADR-0012 — Channel vs Mutex Policy

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0002 (concurrency model)
- ADR-0006 (scheduler model)

## Context
Go offers two concurrency primitives: channels (for communication) and mutexes/atomics (for protecting shared state). A consistent policy prevents design drift where some packages use channels for everything and others use mutexes for everything.

## Decision
**Channels for ownership transfer and lifecycle** — events, cancellation signals, work hand-off. When a goroutine produces something another goroutine will consume, use a channel.

**Mutexes (`sync.RWMutex` / `sync.Mutex`) and atomics (`sync/atomic.*`) for shared *state*** — when two goroutines read/write the same struct concurrently, protect it with a mutex. The litmus test: if a value is read 100×/sec, put it behind a mutex or atomic, not a channel.

**Canonical model for peer connections**: one input channel (for incoming control messages — choke, interested, request, etc.) + one mutex-protected state struct (peer stats, buffer window, piece map).

## Consequences

### Positive
- Clear rule: ownership transfer → channel; shared state → mutex/atomic.
- Matches Go's "share memory by communicating" philosophy without over-rotating on channels for hot-path state reads.
- Peer model is proven in production Go BT implementations.

### Negative
- Dual-primitive design means each struct must be documented as channel-owned or mutex-protected.
- Mixing channels and mutexes incorrectly can deadlock (e.g., holding a mutex while blocking on a channel send).

### Neutral
- Reviewers check for the pattern: read-heavy shared state behind `RWMutex`, hand-off events through buffered channels.

## Compliance Notes
- Tickets affected: All concurrent code tickets.
- Modules affected: `internal/engine`, `internal/protocol/bittorrent/peer`, `internal/protocol/bittorrent/dht`, `internal/rpc/transport`.
- Detection: `go test -race` catches data races; code review checks concurrency pattern conformance.
