# ADR-0007 — Test Strategy

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0020 (reference aria2 version)
- ADR-0022 (SSH/SFTP compatibility tests)
- plans/byte-compat/rpc-method-table.md

## Context
aria2go must be a 100% feature clone — every RPC method response, every CLI exit code, every config file parse, and every session file field must match aria2 1.37.0. This demands a rigorous multi-layer test strategy from unit to conformance to stress.

## Decision
**Test pyramid**:

1. **Unit tests** — per-package, fast, covering all exported functions and critical unexported helpers. Target: ≥80% line coverage.
2. **Module-integration tests** — cross-package within a module using mocks/rigs for external dependencies.
3. **Conformance tests (strategic keystone)** — dual-run aria2go and pinned `aria2c` Docker image (1.37.0, Debian `1:1.37.0-1+b1`) on identical input; diff stdout/stderr/exit code/disk output.
4. **End-to-end tests** — full binary `aria2c` test with real network (HTTP server, BT tracker, DHT node).
5. **Benchmarks** — throughput, latency, memory (≤1.5× aria2c on key workloads).
6. **Stress/fuzz/property** — 24h soak with zero panics/races/leaks; fuzz every parser.

Test rig under `test/rig/` is allowed third-party Go imports (e.g. `golang.org/x/crypto/ssh` as known-good SSH counterparty for SFTP conformance tests). Production code remains stdlib-only under path (a).

Fuzzing covers every parser: bencode, magnet, session, config, cookies, netrc, IDN. Use `testing/synctest` for timing-sensitive logic when Go ≥ 1.25.

## Consequences

### Positive
- Dual-run conformance gives byte-level confidence — catches serialization, format, and behavioral deviations.
- Fuzzing + stress + 24h soak catch concurrency and memory bugs early.
- Test rig external dependency exception enables SFTP testing under path (a).

### Negative
- Conformance docker-based tests require Docker in CI and local dev.
- 24h soak tests are expensive and must be scheduled (not per-PR).
- `testing/synctest` is experimental (Go 1.25+); must have fallback.

### Neutral
- Test infra lives under `test/`; separate from production code.

## Compliance Notes
- Tickets affected: All.
- Modules affected: All.
- Detection: CI matrix runs unit, vet, race, fuzz (subset), and conformance (PR gate).
