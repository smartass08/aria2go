# ADR-0009 — Build Tags Strategy

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0018 (endianness and integer width)
- ADR-0013 (package layout)

## Context
aria2go targets five OS/arch pairs (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, freebsd/amd64, freebsd/arm64, openbsd/amd64, openbsd/arm64). Filesystem operations (fallocate, mmap) have different syscall interfaces per OS. The build tag strategy must isolate OS-specific code while keeping the rest of the codebase platform-agnostic.

## Decision
**Per-OS files only inside `internal/platform/`.** Above that layer, runtime feature detection via `platform.Caps()` returns a capability struct — callers check capabilities, not build tags.

Use `//go:build` constraints only; **no legacy `+build` comments.**

Example constraint: `//go:build linux` (not `// +build linux`).

## Consequences

### Positive
- OS-specific code is contained in a single well-known package.
- Runtime capability detection means higher-level code never needs build tags.
- `//go:build` is the modern Go convention; `+build` is deprecated.

### Negative
- `platform.Caps()` must be kept in sync with actual per-OS implementations — a capability returning true for a platform that doesn't support it is a runtime bug.
- Windows platform code uses `syscall.Syscall6` with different constants than Unix.

### Neutral
- `internal/platform/` contains: `fs_linux.go`, `fs_darwin.go`, `fs_freebsd.go`, `fs_openbsd.go`, `fs_windows.go`, `mmap_unix.go`, `mmap_windows.go`, `signal_*.go`.

## Compliance Notes
- Tickets affected: All platform, disk, and engine tickets.
- Modules affected: `internal/platform/`, `internal/disk/`.
- Detection: `go vet` and `go build` on all target OS/arch pairs in CI.
