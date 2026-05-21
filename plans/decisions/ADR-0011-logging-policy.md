# ADR-0011 — Logging Policy

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0003 (RPC framework — RPC-accessible log output)
- ADR-0010 (error policy)

## Context
aria2 has a specific log line format (date, level, source file, message) that users and tools parse. We need to match that format for compat while also supporting structured logging for ops use cases. Go 1.24+ provides `log/slog` as the standard structured logging substrate.

## Decision
**`log/slog` is the substrate.** All logging passes through `slog.Logger`.

Two handlers:

- **`classic` handler** — emits aria2-style human-readable lines (`YYYY-MM-DD HH:MM:SS [LEVEL] message`) matching aria2's output format exactly. Used for `--console-log-level` output.
- **`json` handler** — emits structured JSON for ops ingestion. Used when `--log=FILE` is provided.

`--log-level` maps to slog levels: `debug`, `info`, `notice`, `warn`, `error`. The classic handler filters below the configured level; the json handler includes all messages and lets collectors filter.

## Consequences

### Positive
- Single logging substrate (`slog`) — all packages log the same way.
- Two handlers cover both compat (classic) and ops (json) use cases.
- `slog` supports context propagation, which integrates with the context cancellation hierarchy (ADR-0002).

### Negative
- Classic handler must replicate aria2's exact date format, level names, and line layout — a formatting bug breaks user tooling.
- `slog.Level` has only 4 canonical levels (Debug, Info, Warn, Error); notice must be mapped to Info with a custom attribute.

### Neutral
- Log package is ~400 LOC in `internal/log/`.

## Compliance Notes
- Tickets affected: All that emit log output.
- Modules affected: `internal/log/` (primary), all other modules (consumers).
- Detection: visual diff of log output against aria2c reference in conformance tests.
