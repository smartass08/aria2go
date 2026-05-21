# ADR-0018 — Endianness and Integer Width

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- plans/contracts/wire-formats.md
- ADR-0009 (build tags strategy — 64-bit targets)

## Context
aria2go handles network wire formats for BitTorrent (bencode with big-endian integers), DHT (compact node info), BT peer wire (big-endian message headers), and other binary protocols. Go's `int` is platform-width-dependent (32-bit on 32-bit, 64-bit on 64-bit). Wire format code must not rely on platform-dependent integer widths.

## Decision
**All wire-format math uses fixed-width types** via `encoding/binary`:
- `binary.BigEndian.PutUint32` / `binary.BigEndian.Uint32` for network-byte-order values.
- `binary.LittleEndian` where protocol specs require (e.g., BT piece indices in some extensions).

**No reliance on `int` width** — always cast to `int64`/`uint32`/`int32` before arithmetic that crosses a function boundary.

**Targets are 64-bit only** (amd64, arm64). This decision avoids 32-bit edge cases entirely while keeping pointers at 8 bytes (important for the 10K-peer memory budget).

## Consequences

### Positive
- Wire-format code is deterministic across platforms.
- Dropping 32-bit support simplifies testing matrix and avoids integer overflow edge cases.
- `binary.BigEndian`/`LittleEndian` is standard library, well-tested, and fast.

### Negative
- 32-bit users (e.g., ARMv7 embedded) cannot run aria2go. Documented as out of scope (§1.2).
- Casting between `int` and fixed-width types adds verbosity in internal code.

### Neutral
- Go 1.24+ on 64-bit platforms is the standard deployment target for modern Go projects.

## Compliance Notes
- Tickets affected: All bencode, DHT, BT peer wire, magnet, sessionfile tickets.
- Modules affected: `internal/bencode`, `internal/magnet`, `internal/protocol/bittorrent/*`, `internal/sessionfile`.
- Detection: `go vet` catches unchecked int-to-int32 truncations; 32-bit build test is a CI gate (must fail cleanly).
