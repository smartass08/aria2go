# Module 12: Disk Storage SPEC

## Package
`internal/disk/`

## Responsibility

File I/O substrate for downloads and seeding. Provides a unified `Adaptor` interface for reading/writing/verifying data on disk across single-file and multi-file (torrent) scenarios. Manages per-piece completion tracking, sparse file support, and OS-specific file allocation strategies.

## Scope

- `Adaptor` interface: common surface for all file layouts (single-file, multi-file torrent)
- `SingleFile` adaptor: maps a download to a single file on disk
- `MultiFile` adaptor: maps a multi-file torrent to multiple files with piece-boundary routing
- `FileEntry` type: per-file name, length, and offset for multi-file layout
- `Allocator` interface and four concrete strategies: none, trunc, falloc, prealloc
- Per-OS allocation: `fallocate` (Linux), `F_PREALLOCATE` (macOS), `posix_fallocate` (FreeBSD), `ftruncate` (OpenBSD), `SetFileInformationByHandle` (Windows) — all behind `platform.Caps()` per ADR-0009
- `Verifier`: piece-level hash verification with context cancellation, returns bad piece indices
- Piece map: bitfield tracking of completed/missing pieces with `Have`/`MarkPiece`/`Missing`
- Sparse file support: when the filesystem supports it, holes are not pre-filled
- Write coalescing: batching of small contiguous writes into larger I/O operations

### Out of Scope

- Direct I/O (`O_DIRECT`) — not in aria2 1.37.0
- mmap for read/write — aria2 uses read/write syscalls; this module uses `os.File.ReadAt`/`WriteAt`
- Disk cache (`--disk-cache`) — managed by the engine, not the disk layer
- File partitioning (aria2's `--allow-overwrite` and `--continue`) — engine concern
- Completed-file seeding read path — handled via the same `ReadAt` surface
- `--hash-check-only` mode — engine invokes `Verifier` directly, no disk adaptor change needed

---

## API Surface

```go
package disk

import (
    "context"
    "os"

    "github.com/smartass08/aria2go/internal/core"
    "github.com/smartass08/aria2go/internal/hash"
)

// Adaptor is the common interface for all on-disk storage layouts.
// All methods are safe for concurrent use by multiple goroutines.
type Adaptor interface {
    OpenForWrite() error
    WriteAt(p []byte, offset int64) (int, error)
    ReadAt(p []byte, offset int64) (int, error)
    Size() int64
    Sync() error
    Close() error

    // BT-aware piece helpers
    SetPieceCount(n int)
    MarkPiece(i int, ok bool)
    Have(i int) bool
    Bitfield() []byte
    Missing() []int
}

// FileEntry describes a single file within a multi-file layout.
type FileEntry struct {
    Name   string // relative path within the download directory
    Length int64  // file size in bytes
    Offset int64  // cumulative byte offset from start of the multi-file region
}

// SingleFile adaptor for single-file downloads.
type SingleFile struct { /* ... */ }
func NewSingleFile(path string, size int64, alloc Allocator) (*SingleFile, error)

// MultiFile adaptor for multi-file torrents.
type MultiFile struct { /* ... */ }
func NewMultiFile(dir string, files []FileEntry, pieceLen int64, alloc Allocator) (*MultiFile, error)

// Allocator controls how file space is pre-allocated on disk.
type Allocator interface {
    Allocate(f *os.File, size int64) error
    Name() string
}

func AllocatorNone() Allocator
func AllocatorTrunc() Allocator
func AllocatorFalloc() Allocator
func AllocatorPrealloc() Allocator

// Verifier checks piece hashes against on-disk data.
type Verifier struct { /* ... */ }
func NewVerifier(a Adaptor, pieceHashes [][]byte, hashKind hash.Kind) *Verifier
func (v *Verifier) Verify(ctx context.Context) ([]int, error)
```

### Adaptor Interface

**`OpenForWrite()`** opens or prepares the underlying files for writing. For `SingleFile`, opens the target path. For `MultiFile`, creates the directory and all files in `files` list. Idempotent after first call; subsequent calls are no-ops. Returns `*disk.Error` on failure (path conflict, permission denied, disk full).

**`WriteAt(p []byte, offset int64)`** writes `len(p)` bytes at absolute byte offset within the logical file region. For `MultiFile`, routes the write across file boundaries — a single call may span two files if the range crosses a `FileEntry` boundary. Returns `(n, error)` where `n` is the number of bytes written. Partial writes are possible when crossing file boundaries and one file fails; the error indicates the first failure encountered.

**`ReadAt(p []byte, offset int64)`** reads `len(p)` bytes at absolute byte offset. For `MultiFile`, routes reads across file boundaries. Returns `(n, error)` matching `io.ReaderAt` semantics: `n < len(p)` with a non-nil error on EOF or boundary errors.

**`Size()`** returns the total logical size in bytes. For `SingleFile`: the configured size. For `MultiFile`: `lastFile.Offset + lastFile.Length`.

**`Sync()`** flushes all buffered writes to disk. Calls `os.File.Sync()` on every open file handle. Returns the first error encountered.

**`Close()`** closes all open file handles. Idempotent: calling `Close()` multiple times is safe and returns nil after the first call. After `Close()`, all other methods return `ErrClosed`.

**BT piece helpers** track which pieces are complete on disk:

- `SetPieceCount(n)` configures the number of pieces. Must be called once before any piece operations. Calling again with the same `n` is a no-op; calling with a different `n` clears the bitfield (re-initialization).
- `MarkPiece(i int, ok bool)` marks piece `i` as complete (`ok=true`) or incomplete (`ok=false`). Panic if `i` is out of range (library invariant — caller validates).
- `Have(i int) bool` returns true if piece `i` is marked complete.
- `Bitfield()` returns a compact bitfield as `[]byte` where each byte holds 8 pieces (MSB-first within each byte). Length is `(pieceCount + 7) / 8`.
- `Missing()` returns a sorted slice of indices for all pieces not yet marked complete. Returns nil if all pieces are complete.

**Piece map invariant:** A piece is marked `Have` only after its data has been fully written to disk AND hash-verified. The caller (engine or BT subsystem) is responsible for verification before calling `MarkPiece(i, true)`. `MarkPiece(i, false)` is used when a hash check fails and the piece must be re-downloaded.

### SingleFile Adaptor

```
NewSingleFile(path string, size int64, alloc Allocator) (*SingleFile, error)
```

Creates a single-file adaptor for the file at `path`. If the file already exists and is shorter than `size`, the `Allocator` extends it; if the file is longer than `size`, it is truncated to `size`. If `path` contains directories, they must already exist (no recursive mkdir). The `Allocator` is invoked during `OpenForWrite()`, not during `NewSingleFile`.

### MultiFile Adaptor

```
NewMultiFile(dir string, files []FileEntry, pieceLen int64, alloc Allocator) (*MultiFile, error)
```

Creates a multi-file adaptor rooted at `dir`. Each `FileEntry` specifies a relative path, length, and cumulative offset. The `files` slice must be sorted by `Offset` ascending and non-overlapping (gaps are permitted — they read as zero-filled holes). `pieceLen` is the torrent piece length used for piece boundary calculations.

On `OpenForWrite()`, creates `dir` if it doesn't exist (including parents), then creates each file at `dir + file.Name`. If a file already exists and is shorter than its `Length`, the allocator extends it. Files longer than their `Length` are truncated.

### FileEntry Type

```go
type FileEntry struct {
    Name   string // relative file path within dir, using OS path separator
    Length int64  // file size in bytes
    Offset int64  // cumulative byte offset from start of multi-file region
}
```

Example: a torrent with three files:
```
files[0] = {Name: "README.txt",    Length: 1024, Offset: 0}
files[1] = {Name: "data/part1.bin", Length: 2048, Offset: 1024}
files[2] = {Name: "data/part2.bin", Length: 4096, Offset: 3072}
```
Total logical size = 3072 + 4096 = 7168 bytes.

A `WriteAt` at offset 1023 spanning 3 bytes would write 1 byte to `README.txt` (at offset 1023) and 2 bytes to `data/part1.bin` (at offset 0).

### Allocator Strategies

All four strategies implement the `Allocator` interface:

```go
type Allocator interface {
    Allocate(f *os.File, size int64) error
    Name() string
}
```

| Strategy | `Name()` | Behavior |
|---|---|---|
| `AllocatorNone()` | `"none"` | No pre-allocation. File grows via writes. |
| `AllocatorTrunc()` | `"trunc"` | Calls `f.Truncate(size)`. Simple, portable, may create sparse files. |
| `AllocatorFalloc()` | `"falloc"` | Filesystem-level allocation via OS-specific syscalls. On filesystems without support, falls back to trunc with a logged warning. |
| `AllocatorPrealloc()` | `"prealloc"` | Legacy preallocation: writes zero-filled blocks sequentially. Slow but guaranteed to allocate real blocks on all filesystems. |

**Selection:** The engine calls `AllocatorFalloc()` when `--file-allocation=falloc` (default), `AllocatorTrunc()` for `trunc`, `AllocatorNone()` for `none`, `AllocatorPrealloc()` for `prealloc`. The allocator is passed to `NewSingleFile`/`NewMultiFile` and invoked during `OpenForWrite()`.

### Per-OS Allocation (ADR-0009)

`AllocatorFalloc()` delegates to `internal/platform/` which exposes per-OS syscalls behind `//go:build` constraints. The disk package calls `platform.Caps().Fallocate` to check availability at runtime.

| OS | Syscall | Notes |
|---|---|---|
| Linux | `fallocate(fd, 0, 0, size)` | Native kernel support on ext4, xfs, btrfs |
| macOS | `fcntl(fd, F_PREALLOCATE, &fstore)` then `ftruncate` | Two-step: allocate blocks, then set length |
| FreeBSD | `posix_fallocate(fd, 0, size)` | libc wrapper; efficient on UFS/ZFS |
| OpenBSD | `ftruncate` only | No block preallocation; silently falls back to trunc (logged) |
| Windows | `SetFileInformationByHandle(h, FileAllocationInfo, ...)` | Requires `SeSecurityPrivilege` on some volumes |

`AllocatorPrealloc()` is a pure-Go fallback: it sequentially writes zero-filled chunks of 64 KiB via `f.WriteAt` in a loop until `size` is reached. It respects context cancellation (the context is threaded through during `OpenForWrite`).

### Verifier

```go
type Verifier struct { /* ... */ }
func NewVerifier(a Adaptor, pieceHashes [][]byte, hashKind hash.Kind) *Verifier
func (v *Verifier) Verify(ctx context.Context) ([]int, error)
```

`NewVerifier` takes an already-open `Adaptor`, the expected piece hashes (one `[]byte` per piece, each the length of the hash output for `hashKind`), and the hash algorithm.

`Verify` iterates over all pieces, reading each piece from disk through `a.ReadAt`, computing its hash, and comparing against `pieceHashes[i]`. Pieces whose hashes match are marked via `a.MarkPiece(i, true)`. Pieces whose hashes mismatch or whose reads fail are collected. Returns the sorted list of bad piece indices. Returns an error only if the context is cancelled (in which case partial progress may have been committed via `MarkPiece`).

**Context cancellation:** On `ctx.Done()`, `Verify` stops processing and returns `ctx.Err()` with whatever pieces were verified up to that point. Incomplete pieces are not marked.

**Piece boundaries:** Piece `i` spans bytes `[i*pieceLen, min((i+1)*pieceLen, a.Size()))`. The last piece may be shorter than `pieceLen`.

**Disk cache interaction:** `Verify` reads through the disk cache layer transparently (the `Adaptor.ReadAt` implementation may be wrapped by the engine).

### Sparse Files

The disk package supports sparse files — file regions that are logically allocated but not physically stored on disk. Sparse file behavior depends on the allocator and platform:

- `AllocatorNone()`: files are always sparse (data blocks allocated only where written)
- `AllocatorTrunc()`: creates sparse files on all platforms (standard `ftruncate` behavior)
- `AllocatorFalloc()`: allocates real blocks on Linux/macOS/FreeBSD; sparse on OpenBSD (trunc fallback)
- `AllocatorPrealloc()`: always writes real blocks (no sparse support by definition)

For multi-file torrents where some files are deselected (via `--select-file`), those files still appear in the `files` slice but with zero `Length` — they occupy no disk space. The `MultiFile` adaptor skips zero-length files during `OpenForWrite()`.

Unwritten regions between wrote pieces read as zero-filled bytes. This is the standard POSIX hole behavior.

### Write Coalescing

To reduce syscall overhead when handling small BitTorrent blocks (typically 16 KiB), the `WriteAt` path may coalesce consecutive small writes into larger operations:

- Writes smaller than `coalesceThreshold` (32 KiB) are buffered
- Consecutive writes to the same file and contiguous offsets are merged
- A buffer flush is triggered when: the batch exceeds `coalesceMaxBatch` (256 KiB), a non-contiguous write arrives, `Sync()` is called, or `Close()` is called
- Coalescing is an optimization; correctness does not depend on it. A write that is buffered is logically committed immediately — `ReadAt` of the just-written range must return the correct data even before the buffer is flushed

Coalescing is transparent to callers. The `SingleFile` and `MultiFile` adaptors use an internal write buffer from `internal/ioutilx.Pool4K`/`Pool16K`/`Pool64K`.

---

## Dependencies

| Package | Role |
|---|---|
| `internal/core` | `core.Error` sentinel errors, GID references in error context |
| `internal/log` | Logging via `slog` (allocation fallback warnings, verification progress) |
| `internal/ioutilx` | Buffer pools (`Pool4K`, `Pool16K`, `Pool64K`) for write coalescing and prealloc |
| `internal/platform` | Per-OS syscalls: `Fallocate`, `Ftruncate`, capabilities query via `platform.Caps()` |
| `internal/hash` | `hash.Kind` enum for Verifier hash algorithm selection |
| `os`, `io` | File I/O, path operations |
| `sync`, `sync/atomic` | Mutexes for concurrent access, atomic state flags |
| `context` | Cancellation propagation in Verifier |
| `path/filepath` | Cross-platform path joining for multi-file paths |
| `errors` | Error wrapping with `%w`, `errors.Is`/`errors.As` for sentinel matching |

Upward consumers: `internal/engine` (download orchestration), `internal/protocol/bittorrent/*` (BT piece I/O), `internal/protocol/http` (single-file download I/O).

---

## Invariants

1. **Piece Have ⇒ on disk + verified.** A piece is marked `Have(i)=true` only after its data has been fully and successfully written to disk AND the piece hash has been verified against the expected value. The Verifier enforces this; direct `MarkPiece` calls from untrusted code paths are invalid.

2. **WriteAt/ReadAt concurrency safety.** Multiple goroutines may call `WriteAt` and `ReadAt` concurrently on the same `Adaptor`. Operations on disjoint byte ranges execute without blocking each other (per-piece granularity: a lock is held for each `pieceLen`-sized region). Operations on overlapping ranges are serialized.

3. **Close idempotency.** `Close()` is safe to call multiple times. After the first call returns, all subsequent calls return nil immediately. All other methods return `ErrClosed` after the first `Close()` completes.

4. **MultiFile offset ordering.** The `files` slice in `NewMultiFile` must be sorted by `Offset` ascending. Gaps between consecutive file offsets are permitted (unwritten regions). Overlapping offsets return an error from `NewMultiFile`.

5. **Bitfield consistency.** `Bitfield()` length is `(pieceCount + 7) / 8`. Within each byte, bit 7 (MSB) represents piece 0 of that byte group, bit 0 (LSB) represents piece 7. A set bit (1) means the piece is complete. This matches standard BitTorrent bitfield encoding (MSB-first within each byte).

6. **Allocator called during OpenForWrite, not construction.** `NewSingleFile` and `NewMultiFile` validate arguments and create the structural objects but do not touch the filesystem. All file creation and allocation happens during `OpenForWrite()`. This allows the caller to inspect/configure the adaptor before files are committed.

7. **Sparse holes read as zero.** Unwritten regions (`AllocatorNone()` holes, gaps between multi-files, pieces not yet downloaded) must return zero-filled bytes via `ReadAt`. The implementation uses the filesystem's natural hole behavior (POSIX filesystems return zeros for holes without explicit handling).

8. **Sync is best-effort for warm path.** `Sync()` flushes all file buffers to disk. Individual file sync failures do not abort the entire operation — all files are synced; the first error is returned after all syncs complete.

9. **No file descriptor leaks.** All `os.File` handles are closed during `Close()` or on construction error cleanup. Temporary files created during allocation are removed on failure.

10. **Platform capability degradation.** When `platform.Caps().Fallocate` is false (OpenBSD), `AllocatorFalloc()` logs a warning and delegates to `AllocatorTrunc()`. This is not an error — the download proceeds correctly with sparse allocation.

---

## Concurrency Contract

- `OpenForWrite()` is not safe for concurrent calls with other methods. Call it once before any `WriteAt`/`ReadAt`/piece operations.
- `WriteAt(p, off)` and `ReadAt(p, off)` are safe for concurrent calls. Per-piece locks (`sync.RWMutex` array, one element per piece) serialize overlapping range access. Disjoint ranges proceed concurrently.
- `Sync()` acquires all write locks (equivalent to a write barrier). Ongoing `WriteAt` calls complete before `Sync` proceeds. New `WriteAt` calls block until `Sync` finishes.
- `Close()` acquires an exclusive lock on the entire adaptor. After `Close()` completes, all methods return `ErrClosed`.
- Piece map operations (`SetPieceCount`, `MarkPiece`, `Have`, `Bitfield`, `Missing`) are protected by a single `sync.RWMutex`. `MarkPiece` takes the write lock; all others take the read lock.
- `SetPieceCount` is not safe for concurrent calls with any piece method. Call it once during initialization.
- `Verify` acquires read locks per-piece as it reads. `MarkPiece` (called internally by `Verify`) acquires the piece map write lock.

---

## Error Handling

The package defines `*disk.Error` with error codes from `plans/contracts/error-codes.md`:

| Sentinel | Code | Trigger |
|---|---|---|
| `ErrClosed` | 0 (no exit) | Method called after `Close()` |
| `ErrOpenFailed` | 19 | `OpenForWrite()` cannot create/open target file |
| `ErrAllocFailed` | 28 | Allocator syscall fails |
| `ErrInvalidOffset` | 0 (no exit) | WriteAt/ReadAt offset exceeds logical size |
| `ErrMultiFileOverlap` | 0 (no exit) | `NewMultiFile` detects overlapping FileEntry offsets |
| `ErrPieceOutOfRange` | 0 (no exit) | Piece index out of bounds (programming error; logged as error) |
| `ErrHashMismatch` | 0 (no exit) | `Verify` found mismatched hashes (returned as bad indices, not error) |
| `ErrWriteFailed` | 28 | Write syscall failed (disk full, I/O error) |
| `ErrReadFailed` | 28 | Read syscall failed (I/O error) |

All sentinel errors wrap `core.Error` for `errors.Is` matching. The engine maps error codes to aria2 exit codes.

---

## Configuration

The disk package itself does not read configuration. Allocation strategy is selected by the caller (engine) based on `--file-allocation` option:

| `--file-allocation` | Allocator | Notes |
|---|---|---|
| `none` | `AllocatorNone()` | No pre-allocation |
| `trunc` | `AllocatorTrunc()` | `ftruncate` to size |
| `falloc` | `AllocatorFalloc()` | OS-specific fast allocation (default) |
| `prealloc` | `AllocatorPrealloc()` | Zero-fill preallocation (slow, legacy) |

The engine passes the allocator directly. The disk package does not import config options.

---

## Tickets Overview

Eleven implementation tickets targeting ~2200 LOC total:

| Ticket | Title | Target Files | LOC est. |
|---|---|---|---|
| T034 | Error type, FileEntry, Allocator interface + none/trunc | `internal/disk/errors.go`, `internal/disk/alloc.go` | ~200 |
| T035 | Write coaelscing buffer and ioutilx integration | `internal/disk/coalesce.go` | ~180 |
| T036 | SingleFile adaptor (open, read, write, sync, close, piece map) | `internal/disk/single.go` | ~300 |
| T037 | MultiFile adaptor (open, multi-file routing, read, write, sync, close, piece map) | `internal/disk/multi.go` | ~400 |
| T038 | Per-OS falloc: Linux fallocate via syscall | `internal/platform/fs_linux.go` (allocation part) | ~80 |
| T039 | Per-OS falloc: macOS F_PREALLOCATE + ftruncate | `internal/platform/fs_darwin.go` (allocation part) | ~80 |
| T040 | Per-OS falloc: FreeBSD posix_fallocate, OpenBSD trunc, Windows SetFileInformationByHandle | `internal/platform/fs_freebsd.go`, `internal/platform/fs_openbsd.go`, `internal/platform/fs_windows.go` (allocation parts) | ~200 |
| T041 | AllocatorFalloc() integration: runtime Caps() check, fallback to trunc | `internal/disk/alloc_falloc.go` | ~100 |
| T042 | AllocatorPrealloc() zero-fill implementation | `internal/disk/alloc_prealloc.go` | ~120 |
| T043 | Sparse file support and hole detection for multi-file | `internal/disk/sparse.go` | ~140 |
| T044 | Verifier: piece-level hash verification with context cancellation | `internal/disk/verify.go` | ~200 |
| T045 | Integration tests: SingleFile/MultiFile round-trip, multi-file boundary crossing, concurrent WriteAt/ReadAt, falloc degradation | `internal/disk/disk_test.go`, `internal/disk/multi_test.go`, `internal/disk/verify_test.go` | ~200 |

Test files per ticket: colocated `*_test.go` files.

All tests must verify:
- SingleFile: WriteAt/ReadAt round-trip, sparse holes read as zero, Close idempotency, concurrent disjoint writes
- MultiFile: cross-file boundary writes, correct piece routing, offset calculation, zero-length file skipping
- Piece map: Bitfield encoding (MSB-first), Have/MarkPiece/Missing consistency, SetPieceCount re-initialization
- Allocator: each strategy produces correct file size, falloc degradations logged but not fatal
- Verifier: correct hash matches, incorrect hash returns bad indices, context cancellation mid-verify, partial progress on cancel
- Concurrency: 100-goroutine stress on disjoint WriteAt regions, no races under `-race`

---

## References

- ADR-0009 (build tags strategy — per-OS files in `internal/platform/`, `platform.Caps()`)
- ADR-0002 (concurrency model — context propagation)
- ADR-0010 (error policy — sentinel errors with `core.Error` wrapping)
- ADR-0011 (logging policy — `slog` substrate)
- ADR-0012 (channel vs mutex — mutex for shared state, per-piece locks)
- PLAN.md §5.13 (disk package API and invariants)
- PLAN.md §11 (cross-platform syscall divergence table, feature degradation matrix)
- `plans/contracts/error-codes.md` — error codes for ErrOpenFailed (19), ErrAllocFailed (28)
- aria2 source: `src/AbstractDiskWriter.cc`, `src/DefaultDiskWriter.cc`, `src/MultiDiskWriter.cc`, `src/FileAllocator.cc`, `src/Piece.cc` (behavior reference only; no source copied)
