# ADR-0019 — uTP (BEP 29) Scope

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0002 (concurrency model)
- ADR-0020 (reference aria2 version — aria2 1.37.0 defaults `--enable-utp` to true)

## Context
uTP (BEP 29) is a UDP-based transport protocol for BitTorrent with LEDBAT congestion control. aria2 1.37.0 supports uTP and enables it by default (`--enable-utp=true`). Implementing uTP requires ~2,500 LOC and correct LEDBAT behavior, which is timing-sensitive and difficult to get right without cross-validation against `libutp`.

## Decision
**uTP is deferred to MVP+1**, gated behind `--enable-utp=false` default until cross-validated against `libutp` packet captures.

Reasoning: ~2,500 LOC plus LEDBAT congestion control is high-risk for the first MVP. aria2 itself defaults `--enable-utp` to true, but our correctness gate is stricter — we require packet-capture cross-validation before enabling by default. Users who need uTP at MVP can opt in with `--enable-utp=true`, accepting beta-quality LEDBAT behavior.

## Consequences

### Positive
- Reduces MVP scope by ~2,500 LOC and ~11 tickets.
- Avoids LEDBAT timing bugs that could affect TCP throughput (uTP and TCP share the same bottleneck link under LEDBAT).
- Allows focused testing of uTP against libutp captures at MVP+1.

### Negative
- Does not match aria2's default (`--enable-utp=true`). Users expecting uTP must opt in.
- uTP provides better NAT traversal and lower latency for BT — deferring it may reduce peer connectivity for some users at MVP.

### Neutral
- `--enable-utp` flag exists from MVP; just defaults to `false` instead of `true`.

## Compliance Notes
- Tickets affected: All uTP tickets (deferred until MVP+1).
- Modules affected: `internal/protocol/bittorrent/utp/` (deferred until MVP+1).
- Detection: `--enable-utp` default in `internal/config/defaults.go` must be `false`.
