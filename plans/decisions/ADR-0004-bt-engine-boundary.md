# ADR-0004 — BitTorrent Engine Boundary

## Status
Accepted (with one revision per Codex)

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0013 (package layout)
- plans/contracts/interfaces.md (BT contract surface)

## Context
BitTorrent has deep cross-cutting concerns: file selection, piece completion, metadata arrival, seeding state, peer counts, tracker status, DHT state, speed accounting, and user-visible RPC fields. The original decision was to keep the engine completely BT-agnostic, with all cross-cutting events flowing through a generic bus.

Codex reviewed this and flagged it as **RISKY**, arguing zero knowledge is too strict — a generic bus can drift into implicit contracts harder to reason about than explicit typed interfaces.

## Decision (Revised)
Engine remains BT-agnostic at compile time, but defines **four explicit typed interfaces** in `internal/contracts/`:

- **`TorrentStatusProjector`** — produces the BT-specific subset of `tellStatus` (bitfield, numSeeders, numPieces, etc.).
- **`FilePieceMap`** — maps torrent files to piece ranges (used by `getFiles`/`changePosition`).
- **`TorrentLifecycleControl`** — `Pause()`, `Stop()`, `RehashAll()`, `Verify()`.
- **`TorrentRPCProjection`** — adapts BT state to RPC fields without leaking BT types into RPC packages.

BT subsystems implement these interfaces; engine consumes them. The event bus is still used for events but no longer carries cross-cutting state.

## Consequences

### Positive
- Explicit typed contracts replace implicit bus-message conventions — easier to test, reason about, and refactor.
- Engine stays BT-agnostic at compile time; no BT types leak into engine or RPC packages.
- Interface segregation: each concern (status, file mapping, lifecycle, RPC projection) gets its own narrow contract.

### Negative
- Four interfaces add ~400 LOC in `internal/contracts/`.
- Requires BT subsystems to implement multiple interfaces rather than sending bus messages.
- Interface versioning: adding a BT feature may require updating contracts and all implementations.

### Neutral
- Bus remains for fire-and-forget events but is demoted from primary state-carrier.

## Compliance Notes
- Tickets affected: All engine tickets, all BT subsystem tickets.
- Modules affected: `internal/engine`, `internal/protocol/bittorrent/*`, `internal/contracts`, `internal/rpc/dispatcher`.
- Detection: compile-time — engine must compile without importing any `internal/protocol/bittorrent/*` package.
