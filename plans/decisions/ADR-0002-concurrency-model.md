# ADR-0002 — Concurrency Model

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0012 (channel vs mutex policy)
- ADR-0017 (memory and goroutine ceilings)

## Context
aria2go must handle 10K+ concurrent BitTorrent peers, multiple HTTP/FTP downloads with segmented connections, an RPC server, and background tasks (port mapping, DHT, saving session). The concurrency model must exploit Go's strengths (goroutines, netpoller, channels) without blindly replicating aria2's C++ event-loop architecture.

Codex reviewed the proposed model and flagged it as **RISKY**, recommending a scalability validation milestone before any protocol-implementation ticket assumes the model.

## Decision
**Per-connection goroutines**: one reader + one writer goroutine per BT peer; segment workers for HTTP/FTP downloads. One scheduler goroutine consuming a `notify` channel. No central event loop — Go's netpoller already handles I/O multiplexing.

**Context cancellation hierarchy**:
- `Daemon → Engine → {Scheduler, RPC, Portmap}`
- Per-RequestGroup: `Engine → RequestGroup → Source → per-Conn ctx`

**Bandwidth throttling** via our own token bucket implementation. Buffer pools via `sync.Pool` (`Pool4K`, `Pool16K`, `Pool64K`).

At 10K peers: ~20K goroutines, within Go's runtime envelope (2KB initial stacks, ~160MB stack memory budget). Reader goroutines block on `read()`; their stacks stay small. Peer registries are sharded per-torrent (no global peer map). DNS uses singleflight cache.

**Mitigation**: A Scalability Validation Milestone is added before any protocol-implementation ticket assumes the concurrency model (see Phase 1.5 in PLAN.md). It includes: synthetic 10K-peer test, GC pause budget (≤100ms p99), RSS budget (≤1 GiB), and throttling accuracy tests against captured aria2c per-second byte counts.

## Consequences

### Positive
- Leverages Go's netpoller directly; no epoll/kqueue wrapper needed.
- Per-connection goroutines are idiomatic Go and simplify request lifecycle reasoning.
- Context cancellation hierarchy makes shutdown orderly and testable.
- Sharded peer registries avoid global lock contention.

### Negative
- 20K goroutines acceptable only under the assumption most peers are I/O-blocked — peer churn and slow peers risk stack/timer/channel inflation.
- Hand-rolled token bucket must match aria2's global, per-download, and per-server throttling under bursty I/O — deceptively complex.
- Scalability must be validated before protocol work can confidently proceed.

### Neutral
- Scalability Validation Milestone gates the concurrency model but does not block non-BT modules.

## Compliance Notes
- Tickets affected: All protocol, engine, and RPC tickets.
- Modules affected: `internal/engine` (scheduler, ticker), `internal/protocol/*` (all protocol drivers), `internal/rpc/transport`.
- Detection: scalability tests in `test/stress/`; `go test -race` on every PR.
