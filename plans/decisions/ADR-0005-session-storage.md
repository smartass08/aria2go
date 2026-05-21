# ADR-0005 — Session Storage Byte-Compat

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- plans/byte-compat/session-format.md
- plans/contracts/wire-formats.md

## Context
aria2's `aria2.session` file persists download state across restarts, including GIDs, URIs, status, output paths, and torrent metadata. The format is line-oriented text with optional gzip compression. Byte-compatible round-tripping is required — existing aria2 users migrating to aria2go must have their sessions preserved.

Codex reviewed the approach as **SOUND** with platform-and-format-scope caveats: byte-compat must be a testable contract, unknown lines must be preserved, and round-trip must be tested across all target platforms.

## Decision
Replicate aria2's `aria2.session` line-oriented text format byte-for-byte. Optional gzip detected by magic bytes `0x1f 0x8b`. Atomic write via temp file + `os.Rename`. The exact `\t<key>=<value>` line order matches aria2's `RequestGroupOptionHandlerHolder` iteration order, captured in `plans/byte-compat/session-format.md`.

Preserve unknown/unsupported lines to ensure forward-compatibility with session files written by future aria2 versions.

## Consequences

### Positive
- Drop-in compatible with existing aria2 session files.
- Atomic write ensures no corruption on crash.
- Gzip support matches aria2's behavior exactly.

### Negative
- Line order dependency on aria2's internal iteration order requires careful replication.
- Unknown-line preservation adds state-tracking complexity.
- Cross-OS golden round-trip is a release gate — must test on Linux, macOS, and Windows.

### Neutral
- Session format spec lives in `plans/byte-compat/session-format.md`.

## Compliance Notes
- Tickets affected: T019 (session format spec), sessionfile implementation tickets.
- Modules affected: `internal/sessionfile`.
- Detection: round-trip tests in `test/golden/sessions/`; cross-OS CI gate.
