# 12-disk-storage — Agent Contract

## What this module is

`internal/disk/` — the file I/O substrate for downloads and seeding. ~2200 LOC across 11 tickets (T034–T045). Consumed by engine, BT core, and HTTP protocol.

## Rules

1. **ADR-0009 is law.** All OS-specific syscalls live in `internal/platform/` behind `//go:build` constraints. `internal/disk/` queries `platform.Caps()` at runtime — never uses build tags directly.
2. **Allocator interface is sealed.** Four strategies only: none/trunc/falloc/prealloc. New strategies require SPEC amendment.
3. **Piece map invariant is absolute.** `Have(i)=true` ⇒ data on disk AND hash-verified. Never mark a piece without verification. `MarkPiece(i, false)` clears; `MarkPiece(i, true)` is commit.
4. **Concurrency model.** WriteAt/ReadAt use per-piece `sync.RWMutex` (pieceLen granularity). Disjoint ranges run concurrently; overlapping ranges serialize. `Close()` acquires full exclusive lock; after close, all methods return `ErrClosed`.
5. **Close is idempotent.** First call closes files and marks state; subsequent calls return nil.
6. **MultiFile offset contract.** `files[]` must be Offset-sorted ascending, non-overlapping. Gaps are zero-filled holes. Violations return error from `NewMultiFile`.
7. **Allocator runs during OpenForWrite, not construction.** `NewSingleFile`/`NewMultiFile` validate; `OpenForWrite()` creates files and allocates.
8. **No panics in library code.** Out-of-range piece indices are logged at error level then ignored (or return error). Only programmer errors in test code may panic.
9. **Buffer pools are mandatory.** Write coalescing and prealloc MUST use `internal/ioutilx.Pool4K`/`Pool16K`/`Pool64K`. Return buffers after use via `defer`.
10. **Tickets are ordered T034→T045.** T034 (errors + alloc interface) → T035 (coalesce) → T036 (SingleFile) → T037 (MultiFile) → T038–T040 (per-OS falloc) → T041 (Falloc integration) → T042 (Prealloc) → T043 (sparse) → T044 (Verifier) → T045 (integration tests). Per-OS tickets T038–T040 are independent of each other.

## Dependencies

- `internal/platform` (per-OS syscalls), `internal/core` (error codes), `internal/log` (slog), `internal/ioutilx` (buffer pools), `internal/hash` (hash.Kind).
- Imports NOT allowed: `golang.org/x/*`, any third-party I/O library.
- Use `os`, `io`, `sync`, `sync/atomic`, `context`, `errors`, `path/filepath`, `time`.

## Tests

- Every adaptor must pass concurrent-write scenarios (≥50 goroutines, disjoint offsets) under `-race`.
- MultiFile must test cross-file-boundary writes (byte at offset where one file ends and next begins).
- Verifier must test context cancellation mid-hash, partial progress retained.
- Falloc degradation on OpenBSD (no-op fallocate) must be a logged warning, not an error.
- Golden bitfield encoding: MSB-first per byte, matching BEP-3.
