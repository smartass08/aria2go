# aria2go Engine ↔ BitTorrent Interface Contracts

> **Version:** 1.0.0
> **Role:** Canonical typed interface surface between `internal/engine` (BT-agnostic) and `internal/protocol/bittorrent/*` subsystems.
> **Authority:** ADR-0004 (BT Engine Boundary, Revised)
> **Package:** `internal/contracts`

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Supporting Types](#2-supporting-types)
3. [TorrentStatusProjector](#3-torrentstatusprojector)
4. [FilePieceMap](#4-filepiecemap)
5. [TorrentLifecycleControl](#5-torrentlifecyclecontrol)
6. [TorrentRPCProjection](#6-torrentrpcprojection)
7. [Concurrency Contract](#7-concurrency-contract)
8. [Implementation Module Map](#8-implementation-module-map)

---

## 1. Architecture Overview

The engine (`internal/engine`) is BT-agnostic at compile time — it never imports any `internal/protocol/bittorrent/*` package. Instead, the four interfaces below form an explicit, typed contract surface. BT subsystems implement one or more of these interfaces; the engine consumes them through dependency injection at `RequestGroup` construction time.

```
┌────────────────────────────┐
│   internal/engine          │  (BT-agnostic, consumes contracts)
│   - RequestGroup           │
│   - RPC dispatcher         │
│   - Scheduler              │
└──────────┬─────────────────┘
           │ typed interface calls (no bus messages for state)
           ▼
┌────────────────────────────┐
│   internal/contracts       │  (this file — the contract surface)
│   TorrentStatusProjector   │
│   FilePieceMap             │
│   TorrentLifecycleControl  │
│   TorrentRPCProjection     │
└──────────▲─────────────────┘
           │ implements
┌──────────┴─────────────────┐
│   internal/protocol/       │
│   bittorrent/*             │  (BT subsystems, supply implementations)
│   - core                   │
│   - peer                   │
│   - tracker                │
│   - dht                    │
│   - extensions             │
└────────────────────────────┘
```

The event bus (`internal/event`) remains for fire-and-forget events (EvStart, EvPause, EvComplete, etc.) but no longer carries cross-cutting state.

---

## 2. Supporting Types

### 2.1 `core.GID` — Global Download Identifier

```go
package core

type GID uint64
```

A monotonically assigned unsigned 64-bit integer identifying a single download. Serialized as a 16-character hex string in RPC and session formats. GIDs persist across engine restarts via the session file.

**Invariants:**
- Never zero (0 is the invalid/reserved sentinel).
- Monotonic per process unless restored from a session file.
- Value type; safe to copy across goroutines.

### 2.2 `FileSlice` — Per-File Piece Range

```go
package contracts

type FileSlice struct {
    Index      int    // 0-based file index within the torrent
    Path       string // relative path as listed in torrent info.files (UTF-8)
    Length     int64  // file size in bytes
    FirstPiece int    // inclusive — the piece index where this file begins
    LastPiece  int    // exclusive — the piece index after the last piece of this file
    Selected   bool   // whether this file is selected for download (respects --select-file)
}
```

**Used by:** `FilePieceMap.Files()` and engine's `getFiles` / `changePosition` RPC handling.

**Invariants:**
- `FirstPiece` is always ≤ the torrent's total piece count.
- For a file that ends mid-piece, `LastPiece` is the index of the piece after the last byte (i.e. the piece that contains the file's last byte, plus one). This matches BT convention: the file may share that boundary piece with the next file.

### 2.3 `InfoHashV1` — V1 BitTorrent Info Hash

```go
package core

type InfoHashV1 [20]byte
```

SHA-1 hash of the bencoded `info` dictionary from a `.torrent` file. Used as the primary content identifier for BTv1 torrents.

### 2.4 `InfoHashV2` — V2 BitTorrent Info Hash

```go
package core

type InfoHashV2 [32]byte
```

SHA-256 hash of the bencoded `info` dictionary for BTv2 (BEP 52). May coexist with `InfoHashV1` in hybrid torrents.

### 2.5 Result Constants for `Verify()`

```go
package contracts

const (
    VerifyOK      = 0  // piece verified successfully (hash matches)
    VerifyMissing = -1 // piece not yet downloaded
    VerifyBad     = -2 // piece downloaded but hash mismatch
)
```

Returned by `TorrentLifecycleControl.Verify()` as values in the returned `[]int` slice. The slice index corresponds to piece index; the value indicates verification outcome.

---

## 3. TorrentStatusProjector

### Package

`internal/contracts`

### Signature

```go
type TorrentStatusProjector interface {
    Project(gid core.GID, keys []string) map[string]any
}
```

### Purpose

Renders the BT-specific subset of `aria2.tellStatus` response fields. The engine calls this when `tellStatus` is invoked on a torrent download and the caller requested BT-specific keys (e.g. `bitfield`, `infoHash`, `numSeeders`, `numPieces`, `pieceLength`).

The engine's `TellStatus` handler merges the generic status map (GID, status, totalLength, completedLength, uploadLength, downloadSpeed, uploadSpeed, etc.) with the map returned by `Project()`.

### Method Contract

**`Project(gid core.GID, keys []string) map[string]any`**

| Parameter | Type | Description |
|-----------|------|-------------|
| `gid` | `core.GID` | Download identifier. Must refer to an active or completed torrent download. |
| `keys` | `[]string` | The BT-specific keys requested by the caller. Empty or nil means "all BT keys." |

**Returns:**
- `map[string]any` — Populated keys from the following domain (keys not applicable to the current state are omitted, not set to null):

| Key | Go type | BT source | Always present? |
|-----|---------|-----------|----------------|
| `infoHash` | `string` (40-char hex) | torrent metadata | Yes, after metadata received |
| `bitfield` | `string` (hex-encoded bytes) | piece completion bitmap | Yes |
| `numPieces` | `string` (decimal) | torrent metadata | Yes |
| `pieceLength` | `string` (decimal) | torrent metadata | Yes |
| `numSeeders` | `string` (decimal) | tracker/DHT aggregation | Yes |
| `seeder` | `string` (`"true"` or `"false"`) | local seeder state | Yes |
| `bittorrent` | `map[string]interface{}` | composite BT state | Yes |
| `bittorrent.announceList` | `[][]string` | tracker metadata | Yes |
| `bittorrent.comment` | `string` | torrent metadata | If present in torrent |
| `bittorrent.creationDate` | `int64` | torrent metadata | If present in torrent |
| `bittorrent.info` | `map[string]interface{}` | torrent metadata | Yes |
| `bittorrent.mode` | `string` | local mode | `"single"` or `"multi"` |
| `bittorrent.name` | `string` | torrent metadata | Yes |
| `belongsTo` | `string` (InfoHashV1 hex) | download grouping | If part of multi-file torrent |
| `files` | `[]map[string]interface{}` | file selection state | If multi-file torrent |
| `verifiedLength` | `string` (decimal) | verify/rehash progress | During/after verify |
| `verifyIntegrityPending` | `string` (`"true"`/`"false"`) | verify status | Yes |

All numeric values are serialized as **decimal strings** (not JSON numbers) to match aria2 RPC byte-compat.

**Errors:**
- No errors from this method — it must always succeed for a valid GID. If the torrent data is not yet available (metadata not received), keys that depend on metadata are simply omitted from the map.

### Thread Safety

**Safe for concurrent use.** The engine may call `Project()` from the RPC dispatcher goroutine while BT subsystems are updating internal state from peer goroutines. The implementation must use internal synchronization (mutex or atomic) to produce a consistent snapshot.

### Implementation Notes

- **Implemented by:** `internal/protocol/bittorrent/core` — the `Torrent` or `BtContext` type.
- The engine holds a `TorrentStatusProjector` per torrent download; the implementation maps `gid` to the internal torrent instance.
- `keys: nil` → return all BT keys. `keys: ["bitfield", "numSeeders"]` → return only those two.
- Unrecognized keys are silently ignored (not an error).

### Example (Engine Perspective)

```go
func (e *Engine) TellStatus(gid core.GID, keys []string) (map[string]any, error) {
    rg := e.getRequestGroup(gid)
    if rg == nil {
        return nil, core.ErrInvalidGID
    }
    status := e.buildGenericStatus(rg)
    if rg.IsTorrent() && rg.TorrentStatusProjector != nil {
        btKeys := extractBTKeys(keys) // filters keys to BT domain
        btMap := rg.TorrentStatusProjector.Project(gid, btKeys)
        for k, v := range btMap {
            status[k] = v
        }
    }
    return status, nil
}
```

---

## 4. FilePieceMap

### Package

`internal/contracts`

### Signature

```go
type FilePieceMap interface {
    Files(gid core.GID) []FileSlice
    PiecesForFile(gid core.GID, idx int) (firstPiece, lastPiece int)
}
```

### Purpose

Maps a torrent's file list to piece ranges, enabling the engine to:
- Respond to `aria2.getFiles` RPC with per-file metadata and selection state.
- Respond to `aria2.changePosition` / `aria2.changeUri` calls that operate on file indices.
- Compute which pieces to download when `--select-file` restricts the download to a subset of files.

### Method Contracts

#### `Files(gid core.GID) []FileSlice`

Returns the ordered list of files in this torrent, each annotated with its piece range and selection state.

| Parameter | Type | Description |
|-----------|------|-------------|
| `gid` | `core.GID` | Download identifier for the torrent. |

**Returns:**
- `[]FileSlice` — Ordered slice of file descriptors. The order matches the torrent's `info.files` list.
- Returns `nil` if metadata has not been received yet.
- The calling code must not mutate the returned slice or its elements.

#### `PiecesForFile(gid core.GID, idx int) (firstPiece, lastPiece int)`

Returns the piece range covering the file at index `idx`.

| Parameter | Type | Description |
|-----------|------|-------------|
| `gid` | `core.GID` | Download identifier for the torrent. |
| `idx` | `int` | 0-based file index, must be in `[0, len(Files(gid)))`. |

**Returns:**
- `firstPiece` — inclusive, the first piece that contains bytes of this file.
- `lastPiece` — exclusive, the piece index after the last piece that contains bytes of this file.
- Behavior is undefined if `idx` is out of range. Safe implementations panic or return `(0, 0)`.

### Thread Safety

**Safe for concurrent use.** `Files()` and `PiecesForFile()` must be goroutine-safe. The returned `[]FileSlice` slice and `FileSlice` structs are immutable snapshots.

### Implementation Notes

- **Implemented by:** `internal/protocol/bittorrent/core` — the same type that implements `TorrentStatusProjector`.
- Piece ranges are computed once when torrent metadata is parsed and cached thereafter.
- For single-file torrents, `Files()` returns a single-element slice with `Index: 0`.
- `FirstPiece` / `LastPiece` are derived from `file.offset / pieceLength` and `ceil((file.offset + file.size) / pieceLength)`.

### Example (Engine Perspective)

```go
// getFiles RPC handler
func (e *Engine) GetFiles(gid core.GID) ([]FileSlice, error) {
    rg := e.getRequestGroup(gid)
    if rg == nil {
        return nil, core.ErrInvalidGID
    }
    if rg.FilePieceMap == nil {
        return nil, core.ErrNotTorrent // not a torrent download
    }
    return rg.FilePieceMap.Files(gid), nil
}

// changePosition RPC handler
func (e *Engine) ChangePosition(gid core.GID, pos, how int, fileIdx int) (int, error) {
    rg := e.getRequestGroup(gid)
    if rg == nil {
        return 0, core.ErrInvalidGID
    }
    if rg.FilePieceMap != nil {
        firstPiece, lastPiece := rg.FilePieceMap.PiecesForFile(gid, fileIdx)
        // validate against available pieces...
    }
    // ...
}
```

---

## 5. TorrentLifecycleControl

### Package

`internal/contracts`

### Signature

```go
type TorrentLifecycleControl interface {
    Pause() error
    Stop(force bool) error
    RehashAll(ctx context.Context) error
    Verify(ctx context.Context) ([]int, error)
}
```

### Purpose

Provides the engine with a handle to control the torrent download lifecycle. BT subsystems return an implementation of this interface at torrent construction time; the engine stores it and calls into it for pause, stop, rehash, and verify operations triggered by RPC commands or scheduler events.

### Method Contracts

#### `Pause() error`

Temporarily suspends all torrent activity: closes peer connections (TCP/µTP), stops tracker announces, halts DHT queries, and pauses piece downloading. Does **not** modify piece selection state or discard downloaded data.

**Returns:**
- `nil` on success.
- Non-nil error if the torrent is already in a terminal state (stopped/completed/error) or internal I/O fails during peer shutdown.

**Idempotent:** calling `Pause()` on an already-paused torrent is a no-op and returns `nil`.

#### `Stop(force bool) error`

Permanently stops the torrent download.

| Parameter | Type | Description |
|-----------|------|-------------|
| `force` | `bool` | If `false`, perform graceful shutdown: announce "stopped" event to trackers, flush piece data to disk. If `true`, immediate shutdown — drop all connections, skip tracker announcements, best-effort data flush. |

**Returns:**
- `nil` on success.
- Non-nil error if disk flush fails (force=false) or if the torrent is already stopped.

**Post-conditions:**
- All peer connections are closed.
- The torrent's internal state machine transitions to `Stopped`.
- A `EvStop` event is emitted on the event bus (unless force=true, where emission is best-effort).

#### `RehashAll(ctx context.Context) error`

Recomputes SHA-1 (and optionally SHA-256 for BTv2) hashes for every piece currently on disk and updates the internal bitfield to reflect what's actually stored. This is the equivalent of aria2's `--check-integrity` flag used at startup.

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Cancellation context. If cancelled, the rehash operation aborts at the earliest safe boundary and returns `ctx.Err()`. |

**Returns:**
- `nil` on success — all pieces on disk have been rehashed and the bitfield updated.
- `ctx.Err()` if cancelled.
- Runtime error if disk I/O fails or a piece file is missing/corrupt.

**Side effects:**
- The internal piece completion bitfield is rewritten based on disk state.
- Any pieces previously marked "done" but now missing on disk are cleared.
- On completion, the torrent transitions back to `Active` state and resumes downloading missing pieces.

**Concurrency:** While rehashing is in progress, peer connections should remain idle (no new piece requests). DHT and tracker announces may continue.

#### `Verify(ctx context.Context) ([]int, error)`

Verifies piece hashes against expected values for download integrity checking.

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Cancellation context. |

**Returns:**
- `[]int` — Slice of verification results indexed by piece number. Each value is one of:
  - `VerifyOK` (0) — piece hash matches.
  - `VerifyMissing` (-1) — piece not yet downloaded.
  - `VerifyBad` (-2) — piece downloaded but hash mismatch.
- `ctx.Err()` if cancelled.
- Runtime error if critical I/O failure occurs.

**Note:** Unlike `RehashAll()`, this method does **not** modify the bitfield. It is purely diagnostic.

### Thread Safety

**Safe for concurrent use.** The engine may call these methods from the RPC dispatcher, the scheduler ticker, or the shutdown path — potentially concurrently. The implementation must serialize state transitions internally.

### Implementation Notes

- **Implemented by:** `internal/protocol/bittorrent/core` — the `Torrent` type.
- `Pause()` and `Stop()` are the primary lifecycle hooks; `RehashAll()` and `Verify()` are on-demand integrity operations.
- `RehashAll()` may be long-running (minutes for large torrents). The context parameter allows the engine to cancel it.
- The engine stores exactly one `TorrentLifecycleControl` per torrent download, obtained at `RequestGroup` construction.

### Example (Engine Perspective)

```go
// RPC: aria2.pause
func (e *Engine) Pause(gid core.GID, force bool) error {
    rg := e.getRequestGroup(gid)
    if rg == nil {
        return core.ErrInvalidGID
    }
    if rg.Status() != core.StatusActive {
        return nil // already not active
    }
    if rg.LifecycleControl != nil { // torrent path
        return rg.LifecycleControl.Pause()
    }
    return rg.protocolSource.Pause() // HTTP/FTP path
}

// RPC: aria2.forceRemove
func (e *Engine) Remove(gid core.GID, force bool) error {
    rg := e.getRequestGroup(gid)
    if rg == nil {
        return core.ErrInvalidGID
    }
    if rg.LifecycleControl != nil {
        return rg.LifecycleControl.Stop(force)
    }
    return rg.protocolSource.Stop(force)
}
```

---

## 6. TorrentRPCProjection

### Package

`internal/contracts`

### Signature

```go
type TorrentRPCProjection interface {
    Peers(gid core.GID) []map[string]any
    Servers(gid core.GID) []map[string]any
}
```

### Purpose

Adapts BT peer and server state to the RPC response format without leaking BT-specific types (e.g. `peer.Peer`, `tracker.Tracker`) into the RPC packages. The engine calls these methods when handling `aria2.getPeers` and `aria2.getServers` for torrent downloads.

### Method Contracts

#### `Peers(gid core.GID) []map[string]any`

Returns a snapshot of the current BT peer set, each peer rendered as a map suitable for JSON marshalling in the aria2 RPC response.

| Parameter | Type | Description |
|-----------|------|-------------|
| `gid` | `core.GID` | Download identifier for the torrent. |

**Returns:**
- `[]map[string]any` — One map per connected peer (both active and choked). Returns `nil` if no peers are connected or metadata is not yet available.

Each peer map contains the following keys:

| Key | Go type | Serialized as | Description |
|-----|---------|---------------|-------------|
| `peerId` | `string` | URL-encoded bytes (percent-encoding) | BT peer ID (20 bytes, `%XX` encoded) |
| `ip` | `string` | dotted-quad or bracketed IPv6 | Peer IP address |
| `port` | `string` (decimal) | decimal string | Peer TCP port |
| `bitfield` | `string` | hex-encoded bytes | Peer's piece availability bitfield |
| `amChoking` | `string` | `"true"`/`"false"` | Am I choking this peer? |
| `peerChoking` | `string` | `"true"`/`"false"` | Is this peer choking me? |
| `downloadSpeed` | `string` | decimal string, bytes/sec | Download speed from this peer |
| `uploadSpeed` | `string` | decimal string, bytes/sec | Upload speed to this peer |
| `seeder` | `string` | `"true"`/`"false"` | Is this peer a seeder (has all pieces)? |
| `client` | `string` | UTF-8 truncated at 64 chars | Client name decoded from peer ID prefix |

**Invariants:**
- All numeric values are decimal strings (not JSON numbers) for aria2 byte-compat.
- Peer maps are shallow copies; the engine's RPC layer may modify them without affecting BT internals.

#### `Servers(gid core.GID) []map[string]any`

Returns the current tracker and DHT server state for the torrent.

| Parameter | Type | Description |
|-----------|------|-------------|
| `gid` | `core.GID` | Download identifier for the torrent. |

**Returns:**
- `[]map[string]any` — One map per tracker node gathered from all tracker tiers. Returns `nil` if no tracker data is available.

Each server map contains:

| Key | Go type | Serialized as | Description |
|-----|---------|---------------|-------------|
| `index` | `string` | hex-encoded InfoHashV1 | Tracker tier identifier (the info hash) |
| `servers` | `[]map[string]any` | array of server objects | Servers within this tracker tier |
| `servers[].uri` | `string` | literal URL | Tracker announce URL |
| `servers[].currentUri` | `string` | literal URL | Currently active announce URL (may differ after redirect) |
| `servers[].downloadSpeed` | `string` | decimal string, bytes/sec | Aggregate download speed (always `"0"` for trackers) |

### Thread Safety

**Safe for concurrent use.** The engine may call `Peers()` and `Servers()` from RPC handler goroutines while peer connections come and go. The implementation must take a consistent snapshot under internal synchronization.

### Implementation Notes

- **Implemented by:** `internal/protocol/bittorrent/core` or a dedicated projection adapter in the bittorrent package group.
- `Peers()` iterates the active peer set and projects each `Peer` struct into a generic map. DHT-only peers (known from DHT but not connected) may optionally be included.
- `Servers()` aggregates all announce tiers (including DHT and UDP tracker) into the aria2 response format. DHT nodes are reported as a single virtual server entry with the info hash as identifier.
- The returned maps must not contain any BT-internal pointers — they must be fully serializable by `encoding/json`.

### Example (Engine Perspective)

```go
// RPC: aria2.getPeers
func (e *Engine) GetPeers(gid core.GID) ([]map[string]any, error) {
    rg := e.getRequestGroup(gid)
    if rg == nil {
        return nil, core.ErrInvalidGID
    }
    if rg.TorrentRPCProjection == nil {
        return nil, core.ErrNotTorrent
    }
    return rg.TorrentRPCProjection.Peers(gid), nil
}

// RPC: aria2.getServers
func (e *Engine) GetServers(gid core.GID) ([]map[string]any, error) {
    rg := e.getRequestGroup(gid)
    if rg == nil {
        return nil, core.ErrInvalidGID
    }
    if rg.TorrentRPCProjection == nil {
        return nil, core.ErrNotTorrent
    }
    return rg.TorrentRPCProjection.Servers(gid), nil
}
```

---

## 7. Concurrency Contract

All four interfaces are **concurrency-safe by contract**. Implementations must guarantee:

1. **No data races** — all methods use internal synchronization (mutex, atomic, or channel-based serialization). ADR-0012 governs mutex vs. channel selection.
2. **Consistent snapshots** — methods that return slices or maps return coherent snapshots; the caller's view does not mutate after return.
3. **No blocking on callers** — methods must not block indefinitely waiting on external I/O. Cancellation via `context.Context` (where provided) is the only acceptable cancellation mechanism.
4. **Reentrancy** — it is valid for the engine to call `Project()` and `Peers()` concurrently. If the implementation uses a single mutex, it must not hold it across I/O.

---

## 8. Implementation Module Map

| Interface | Implementing Module | Required By |
|-----------|-------------------|-------------|
| `TorrentStatusProjector` | `06-bittorrent-core` | `01-core-engine` (TellStatus handler) |
| `FilePieceMap` | `06-bittorrent-core` | `01-core-engine` (getFiles, changePosition handlers) |
| `TorrentLifecycleControl` | `06-bittorrent-core` | `01-core-engine` (pause, stop, remove, check-integrity handlers) |
| `TorrentRPCProjection` | `06-bittorrent-core` | `01-core-engine` (getPeers, getServers handlers), `14-rpc-server` |

The engine's `RequestGroup` struct holds one optional field per interface:

```go
type RequestGroup struct {
    // ...
    TorrentStatusProjector   contracts.TorrentStatusProjector   // nil if non-torrent
    FilePieceMap             contracts.FilePieceMap             // nil if non-torrent
    LifecycleControl         contracts.TorrentLifecycleControl  // nil if non-torrent
    TorrentRPCProjection     contracts.TorrentRPCProjection     // nil if non-torrent
}
```

An engine goroutine checks `!= nil` before calling any BT interface method — this is the only BT awareness the engine has.

---

## References

- **ADR-0004** — BT Engine Boundary (this contract's authority).
- **ADR-0005** — Session Storage (GID persistence contract).
- **ADR-0010** — Error Policy (error wrapping and codes).
- **ADR-0012** — Channel vs. Mutex Policy (implementation guidance for thread safety).
- **ADR-0013** — Package Layout (`internal/contracts` placement).
- **`plans/contracts/error-codes.md`** — Error code table (0..32).
- **`plans/contracts/rpc-methods.md`** — RPC method signatures that consume these interfaces.
