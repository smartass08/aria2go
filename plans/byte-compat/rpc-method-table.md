# aria2 RPC Method Response Shapes — Byte-Compatible Specification

> **Target:** aria2 1.37.0 JSON-RPC output byte-for-byte compatibility.
> **Scope:** This document specifies the exact JSON-RPC response shapes, field ordering, value formats, null/omitted field rules, and error formatting for every method and notification. It is the single source of truth for all 14-rpc-server implementers.
> **License boundary:** Zero aria2 C++ source LOC. Spec derived from the aria2 1.37.0 public manual (CC-BY) RPC Interface section and behavioral observation.

---

## 1. JSON-RPC Envelope

Every response (success and error) follows JSON-RPC 2.0:

```json
{
  "id": "<request-id>",
  "jsonrpc": "2.0",
  "result": <method-result>
}
```

### 1.1 Response Field Ordering

Within JSON objects in both request params and response results, **all keys are sorted alphabetically by key name** (ASCII/lexicographic order). This is the canonical aria2 1.37.0 behavior because the C++ implementation serializes `std::map<std::string, ...>` which iterates in sorted key order.

This applies to:
- Response struct keys within `result`
- Nested struct keys (files entries, peers entries, options maps, etc.)
- Notification payload structs

### 1.2 Value Format Rules

| Domain | Format | Examples |
|--------|--------|---------|
| **GID** | 16-char lowercase hex string | `"2089b05ecca3d829"` |
| **Boolean** | JSON string `"true"` or `"false"` (never numeric 1/0, never JSON boolean) | `"true"`, `"false"` |
| **Integer counts** | JSON string of decimal digits | `"2"`, `"34"` |
| **Byte counts** | JSON string of decimal digits | `"34896138"`, `"0"` |
| **Speeds** | JSON string of decimal digits (bytes/sec) | `"15158"`, `"0"` |
| **Status** | JSON string | `"active"`, `"waiting"`, `"paused"`, `"error"`, `"complete"`, `"removed"` |
| **Bitfield** | Lowercase hex string, no `0x` prefix, MSB = piece 0 | `"0000000000"`, `"ffff80"` |
| **Info hash** | Lowercase hex string for BT info hashes | `"a9b8c7d6e5f4..."` |
| **Session ID** | 40-char lowercase hex string (SHA-1 format) | `"cd6a3bc6a1de28eb5bfa181e5f6b916d44af31a9"` |
| **Version** | String like `"1.37.0"` | `"1.37.0"` |
| **Unix timestamp** | Integer (not string) in seconds since epoch | `1288910677` |
| **changePosition result** | Integer (not string) | `0`, `3` |
| **changeUri result** | Array of two integers (not strings) | `[0, 1]` |
| **URI status** | JSON string | `"used"`, `"waiting"` |
| **Torrent mode** | JSON string | `"single"`, `"multi"` |

### 1.3 Null / Omitted Field Rules

aria2 does **not** output JSON `null` values. Fields are either:

- **Always present** — the field exists in every response of the method. For `aria2.tellStatus`, always-present fields are: `gid`, `status`, `totalLength`, `completedLength`, `uploadLength`, `downloadSpeed`, `uploadSpeed`, `pieceLength`, `numPieces`, `connections`, `dir`, `files`.
- **Conditionally present** — the field exists only when applicable. When not applicable, the key is entirely absent from the JSON object (not `null`). The list of conditionally present fields per method is specified below.
- **Always present but zero-valued** — some fields like `uploadSpeed` or `uploadLength` are always present even when zero.

---

## 2. Method Response Shapes

Fields are listed in alphabetical order (canonical wire order). A "✓" icon marks conditionally-present fields; all others are always present.

### 2.1 aria2.addUri

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.addUri",
  "params": [["http://example.org/file"], {"dir": "/downloads"}]
}
```

**With secret token:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.addUri",
  "params": ["token:mysecret", ["http://example.org/file"]]
}
```

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": "2089b05ecca3d829"
}
```

Result is a **string**: the GID (16-char lowercase hex).

**Error response (e.g., duplicate):**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "error": {
    "code": 1,
    "message": "Duplicate download"
  }
}
```

### 2.2 aria2.addTorrent

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.addTorrent",
  "params": ["<base64-torrent-data>", ["http://example.org/webseed"], {"dir": "/downloads"}]
}
```

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": "2089b05ecca3d829"
}
```

Result is a **string**: the GID (16-char lowercase hex).

**Error response:** Same envelope as addUri, with `"code": 1` and an error message like `"Duplicate info hash"`.

### 2.3 aria2.addMetalink

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.addMetalink",
  "params": ["<base64-metalink-data>", {}, 0]
}
```

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": ["2089b05ecca3d829", "d2703803b52216d1"]
}
```

Result is an **array of strings** — one GID per download described in the metalink. Even a single-download metalink returns an array (single-element).

**Error response (e.g., parse error):**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "error": {
    "code": 1,
    "message": "Could not parse metalink XML data."
  }
}
```

### 2.4 aria2.remove

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.remove",
  "params": ["2089b05ecca3d829"]
}
```

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": "2089b05ecca3d829"
}
```

Result is a **string**: the GID of the removed download.

**Error response (GID not found):**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "error": {
    "code": 1,
    "message": "Active Download not found for GID#2089b05ecca3d829"
  }
}
```

### 2.5 aria2.forceRemove

**Request:** Same shape as `aria2.remove`.
**Success response:** Same shape as `aria2.remove` — result is the GID **string**.

### 2.6 aria2.pause

**Request:** `["<gid>"]` (+ optional secret).
**Success response:** Result is the GID **string**.
**Error response (GID not found):** Same as `aria2.remove` error shape.

### 2.7 aria2.pauseAll

**Request:** `[]` (+ optional secret).
**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": "OK"
}
```

Result is the **string** `"OK"`.

### 2.8 aria2.forcePause

Same response shape as `aria2.pause`.

### 2.9 aria2.forcePauseAll

Same response shape as `aria2.pauseAll`: `"OK"`.

### 2.10 aria2.unpause

**Request:** `["<gid>"]` (+ optional secret).
**Success response:** Result is the GID **string**.

### 2.11 aria2.unpauseAll

Same response shape as `aria2.pauseAll`: `"OK"`.

### 2.12 aria2.tellStatus

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.tellStatus",
  "params": ["2089b05ecca3d829"]
}
```

**With keys filter:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.tellStatus",
  "params": ["2089b05ecca3d829", ["gid", "status", "totalLength"]]
}
```

**Success response (active download):**

Fields in alphabetical order (canonical wire order):

```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": {
    "bitfield": "0000000000",
    "completedLength": "901120",
    "connections": "1",
    "dir": "/downloads",
    "downloadSpeed": "15158",
    "files": [
      {
        "completedLength": "34896138",
        "index": "1",
        "length": "34896138",
        "path": "/downloads/file",
        "selected": "true",
        "uris": [
          {
            "status": "used",
            "uri": "http://example.org/file"
          }
        ]
      }
    ],
    "gid": "2089b05ecca3d829",
    "numPieces": "34",
    "pieceLength": "1048576",
    "status": "active",
    "totalLength": "34896138",
    "uploadLength": "0",
    "uploadSpeed": "0"
  }
}
```

**Field table:**

| Key | Type | Always Present? | Notes |
|-----|------|-----------------|-------|
| `bitfield` | string | ✗ | Hex bitfield. Absent if download not started. |
| `bittorrent` | struct | ✗ | BT metadata. Only present for BT downloads. |
| `belongsTo` | string | ✗ | GID of parent download. Absent if none. |
| `completedLength` | string | ✓ | Bytes completed (includes partial pieces). |
| `connections` | string | ✓ | Number of connections. |
| `dir` | string | ✓ | Download directory path. |
| `downloadSpeed` | string | ✓ | Speed in bytes/sec, as string. |
| `errorCode` | string | ✗ | Error code 0-32. Present only for stopped/completed downloads. |
| `errorMessage` | string | ✗ | Human-readable error. Present when errorCode is present. |
| `files` | struct[] | ✓ | Array of file objects. Empty array for magnet downloads before metadata resolved. |
| `followedBy` | string[] | ✗ | List of child download GIDs. Absent if none. |
| `following` | string | ✗ | GID of parent download. Absent if none. |
| `gid` | string | ✓ | 16-char lowercase hex. |
| `infoHash` | string | ✗ | Lowercase hex info hash. BT only. Absent for non-BT. |
| `numPieces` | string | ✓ | Total number of pieces. |
| `numSeeders` | string | ✗ | Connected seeder count. BT only. Absent for non-BT. |
| `pieceLength` | string | ✓ | Piece size in bytes. |
| `seeder` | string | ✗ | `"true"` or `"false"`. BT only. Absent for non-BT. |
| `status` | string | ✓ | One of: `active`, `waiting`, `paused`, `error`, `complete`, `removed`. |
| `totalLength` | string | ✓ | Total size in bytes. |
| `uploadLength` | string | ✓ | Bytes uploaded. |
| `uploadSpeed` | string | ✓ | Upload speed in bytes/sec as string. |
| `verifiedLength` | string | ✗ | Bytes verified. Present only during hash check. |
| `verifyIntegrityPending` | string | ✗ | `"true"`. Present only when waiting for hash check in queue. |

**bittorrent sub-struct (alphabetical order):**

| Key | Type | Notes |
|-----|------|-------|
| `announceList` | string[][] | List of lists of announce URIs. |
| `comment` | string | Torrent comment. |
| `creationDate` | integer | Unix timestamp (seconds since epoch). **Integer, not string.** |
| `info` | struct | Contains `name` (string). |
| `mode` | string | `"single"` or `"multi"`. |

**files entry (alphabetical order):**

| Key | Type | Notes |
|-----|------|-------|
| `completedLength` | string | Completed bytes for this file. |
| `index` | string | 1-based file index. |
| `length` | string | File size in bytes. |
| `path` | string | Full file path. |
| `selected` | string | `"true"` or `"false"`. |
| `uris` | struct[] | Array of `{status, uri}` objects. |

**URI entry (within files and getUris):**

| Key | Type | Notes |
|-----|------|-------|
| `status` | string | `"used"` or `"waiting"`. |
| `uri` | string | The URI. |

### 2.13 aria2.getUris

**Request:** `["<gid>"]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": [
    {
      "status": "used",
      "uri": "http://example.org/file"
    }
  ]
}
```

Result is an **array of structs**. Each struct has exactly two keys: `status` (before `uri` alphabetically).

### 2.14 aria2.getFiles

**Request:** `["<gid>"]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": [
    {
      "completedLength": "34896138",
      "index": "1",
      "length": "34896138",
      "path": "/downloads/file",
      "selected": "true",
      "uris": [
        {
          "status": "used",
          "uri": "http://example.org/file"
        }
      ]
    }
  ]
}
```

Result is an **array of structs**. Each struct has 6 keys, all strings. The `uris` sub-array uses the same struct shape as `aria2.getUris`.

### 2.15 aria2.getPeers

**Request:** `["<gid>"]` (+ optional secret).

**Success response (BT download):**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": [
    {
      "amChoking": "true",
      "bitfield": "ffffffffffffffffffffffffffffffffffffffff",
      "downloadSpeed": "10602",
      "ip": "10.0.0.9",
      "peerChoking": "false",
      "peerId": "aria2%2F1%2E10%2E5%2D%87%2A%EDz%2F%F7%E6",
      "port": "6881",
      "seeder": "true",
      "uploadSpeed": "0"
    }
  ]
}
```

Result is an **array of structs**. All 9 keys are always present for each connected peer. For non-BT downloads, returns an empty array `[]`. All values are strings.

| Key | Type | Notes |
|-----|------|-------|
| `amChoking` | string | `"true"` / `"false"` |
| `bitfield` | string | Hex bitfield of peer availability |
| `downloadSpeed` | string | Speed from this peer, bytes/sec |
| `ip` | string | IP address |
| `peerChoking` | string | `"true"` / `"false"` |
| `peerId` | string | Percent-encoded peer ID (printable ASCII + percent-encoded bytes) |
| `port` | string | Port number |
| `seeder` | string | `"true"` / `"false"` |
| `uploadSpeed` | string | Speed to this peer, bytes/sec |

### 2.16 aria2.getServers

**Request:** `["<gid>"]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": [
    {
      "index": "1",
      "servers": [
        {
          "currentUri": "http://example.org/file",
          "downloadSpeed": "10467",
          "uri": "http://example.org/file"
        }
      ]
    }
  ]
}
```

Result is an **array of structs**, one per file index. Each file struct has `index` (string) and `servers` (array). Each server in `servers` has 3 keys (alphabetical order: `currentUri`, `downloadSpeed`, `uri`), all strings.

### 2.17 aria2.tellActive

**Request:** `[]` or `[["gid", "status"]]` (+ optional secret).

**Success response:** Same shape as `aria2.tellStatus` per element. Result is an **array of structs** — each struct is a full tellStatus result. An empty array `[]` if no active downloads.

### 2.18 aria2.tellWaiting

**Request:** `[offset, num]` or `[offset, num, ["gid", "status"]]` (+ optional secret). `offset` and `num` are **integers** (not strings).

**Success response:** Same shape as `aria2.tellStatus` per element. Result is an **array of structs**.

### 2.19 aria2.tellStopped

**Request:** `[offset, num]` or `[offset, num, ["gid", "status"]]` (+ optional secret). Same semantics as tellWaiting.

**Success response:** Same shape as `aria2.tellStatus` per element. Result is an **array of structs**. Note: stopped downloads include `errorCode` and `errorMessage` keys when applicable.

### 2.20 aria2.changePosition

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.changePosition",
  "params": ["2089b05ecca3d829", 0, "POS_SET"]
}
```

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": 0
}
```

Result is an **integer** (JSON number, not string). This is one of the few methods that returns a non-string result.

### 2.21 aria2.changeUri

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.changeUri",
  "params": ["2089b05ecca3d829", 1, [], ["http://example.org/file"]]
}
```

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": [0, 1]
}
```

Result is an **array of two integers** (JSON numbers, not strings): `[numDeleted, numAdded]`.

### 2.22 aria2.getOption

**Request:** `["<gid>"]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": {
    "allow-overwrite": "false",
    "allow-piece-length-change": "false",
    "always-resume": "true",
    "async-dns": "true"
  }
}
```

Result is a **struct** mapping option names (strings) to option values (strings). Keys are in alphabetical order. Only explicitly set options are returned; options at their default values are omitted.

### 2.23 aria2.changeOption

**Request:** `["<gid>", {"max-download-limit": "10K"}]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": "OK"
}
```

Result is the **string** `"OK"`.

**Error response (invalid option):**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "error": {
    "code": 1,
    "message": "We cannot change the option for the existing download: dry-run"
  }
}
```

### 2.24 aria2.getGlobalOption

**Request:** `[]` (+ optional secret).

**Success response:** Same shape as `aria2.getOption` — a struct of option-name to string-value, alphabetical key order, only explicitly set options.

### 2.25 aria2.changeGlobalOption

**Request:** `[{"max-concurrent-downloads": "5"}]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": "OK"
}
```

### 2.26 aria2.getGlobalStat

**Request:** `[]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": {
    "downloadSpeed": "21846",
    "numActive": "2",
    "numStopped": "0",
    "numStoppedTotal": "5",
    "numWaiting": "0",
    "uploadSpeed": "0"
  }
}
```

Result is a struct with 6 keys, all string values, alphabetical order.

| Key | Type | Notes |
|-----|------|-------|
| `downloadSpeed` | string | Aggregate download speed, bytes/sec. |
| `numActive` | string | Number of active downloads. |
| `numStopped` | string | Stopped downloads (capped by `--max-download-result`). |
| `numStoppedTotal` | string | Total stopped downloads this session (uncapped). Always present; may be `"0"`. |
| `numWaiting` | string | Number of waiting downloads (includes paused). |
| `uploadSpeed` | string | Aggregate upload speed, bytes/sec. |

### 2.27 aria2.purgeDownloadResult

**Request:** `[]` (+ optional secret).
**Success response:** Result is the **string** `"OK"`.

### 2.28 aria2.removeDownloadResult

**Request:** `["<gid>"]` (+ optional secret).
**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": "OK"
}
```

### 2.29 aria2.getVersion

**Request:** `[]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": {
    "enabledFeatures": [
      "Async DNS",
      "BitTorrent",
      "Firefox3 Cookie",
      "GZip",
      "HTTPS",
      "Message Digest",
      "Metalink",
      "SFTP",
      "WebSocket",
      "XML-RPC"
    ],
    "version": "1.37.0"
  }
}
```

Result is a struct with 2 keys in alphabetical order: `enabledFeatures` (array of strings) then `version` (string).

Known feature names in aria2 1.37.0:
- `"Async DNS"`
- `"BitTorrent"`
- `"Firefox3 Cookie"`
- `"GZip"`
- `"HTTPS"`
- `"Message Digest"`
- `"Metalink"`
- `"SFTP"`
- `"WebSocket"`
- `"XML-RPC"`

These must appear in the exact case and spacing shown.

### 2.30 aria2.getSessionInfo

**Request:** `[]` (+ optional secret).

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": {
    "sessionId": "cd6a3bc6a1de28eb5bfa181e5f6b916d44af31a9"
  }
}
```

Result is a struct with 1 key: `sessionId` — a 40-char lowercase hex string (SHA-1 hash format).

### 2.31 aria2.shutdown

**Request:** `[]` (+ optional secret).
**Success response:** Result is the **string** `"OK"`.

### 2.32 aria2.forceShutdown

**Request:** `[]` (+ optional secret).
**Success response:** Result is the **string** `"OK"`.

### 2.33 aria2.saveSession

**Request:** `[]` (+ optional secret).
**Success response:** Result is the **string** `"OK"`.

### 2.34 system.multicall

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "system.multicall",
  "params": [
    [
      {"methodName": "aria2.addUri", "params": [["http://example.org"]]},
      {"methodName": "aria2.addTorrent", "params": ["base64data"]}
    ]
  ]
}
```

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": [["2089b05ecca3d829"], ["d2703803b52216d1"]]
}
```

Result is an **array**. Each element corresponds to one nested method call:

- **Success:** A single-element array `[<result>]` wrapping the method's return value. If the nested method returns an array (e.g., `addMetalink`), it becomes a nested array: `[["gid1", "gid2"]]`.

- **Failure:** A struct (object) `{"faultCode": 1, "faultString": "<message>"}`. The faultCode is always 1 (integer). The faultString is always a string. Keys are in alphabetical order: `faultCode` before `faultString`.

**Multicall with secret token:**
Each nested call's params array independently includes the token as its first element:
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "system.multicall",
  "params": [
    [
      {"methodName": "aria2.addUri", "params": ["token:mysecret", ["http://example.org"]]},
      {"methodName": "aria2.getGlobalStat", "params": ["token:mysecret"]}
    ]
  ]
}
```

**JSON-RPC Batch alternative:**
Instead of `system.multicall`, aria2 also supports JSON-RPC Batch (array of request objects as the top-level JSON value). In batch mode, each request is a full JSON-RPC request object with its own `jsonrpc`, `method`, `id`, and `params`. The response is an array of full JSON-RPC response objects, each with its own `id`, `jsonrpc`, and `result` (or `error`):

```json
[
  {"id": "qwer", "jsonrpc": "2.0", "result": "2089b05ecca3d829"},
  {"id": "asdf", "jsonrpc": "2.0", "result": "d2703803b52216d1"}
]
```

In batch mode, errors use the standard JSON-RPC error envelope per element.

### 2.35 system.listMethods

**Request:** `[]` — no params, **no secret token required**.

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": [
    "aria2.addMetalink",
    "aria2.addTorrent",
    "aria2.addUri",
    "aria2.changeGlobalOption",
    "aria2.changeOption",
    "aria2.changePosition",
    "aria2.changeUri",
    "aria2.forcePause",
    "aria2.forcePauseAll",
    "aria2.forceRemove",
    "aria2.forceShutdown",
    "aria2.getFiles",
    "aria2.getGlobalOption",
    "aria2.getGlobalStat",
    "aria2.getOption",
    "aria2.getPeers",
    "aria2.getServers",
    "aria2.getSessionInfo",
    "aria2.getUris",
    "aria2.getVersion",
    "aria2.pause",
    "aria2.pauseAll",
    "aria2.purgeDownloadResult",
    "aria2.remove",
    "aria2.removeDownloadResult",
    "aria2.saveSession",
    "aria2.shutdown",
    "aria2.tellActive",
    "aria2.tellStopped",
    "aria2.tellStatus",
    "aria2.tellWaiting",
    "aria2.unpause",
    "aria2.unpauseAll",
    "system.listMethods",
    "system.listNotifications",
    "system.multicall"
  ]
}
```

Result is an **array of strings** — all 36 method names. **Method names are sorted alphabetically in the response.**

### 2.36 system.listNotifications

**Request:** `[]` — no params, **no secret token required**.

**Success response:**
```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "result": [
    "aria2.onBtDownloadComplete",
    "aria2.onDownloadComplete",
    "aria2.onDownloadError",
    "aria2.onDownloadPause",
    "aria2.onDownloadStart",
    "aria2.onDownloadStop"
  ]
}
```

Result is an **array of strings** — all 6 notification names, alphabetically sorted.

---

## 3. Notification Payloads (WebSocket Only)

Notifications are server-to-client messages sent over WebSocket only. They lack an `id` field. The client must not respond.

### 3.1 Common Event Struct

All 6 notifications share the same event struct shape:

```json
{
  "gid": "2089b05ecca3d829"
}
```

Single key `gid` (string, 16-char lowercase hex). The event struct is wrapped in a JSON-RPC notification envelope.

### 3.2 aria2.onDownloadStart

Sent when a download starts.

```json
{
  "jsonrpc": "2.0",
  "method": "aria2.onDownloadStart",
  "params": [{"gid": "2089b05ecca3d829"}]
}
```

### 3.3 aria2.onDownloadPause

Sent when a download is paused.

```json
{
  "jsonrpc": "2.0",
  "method": "aria2.onDownloadPause",
  "params": [{"gid": "2089b05ecca3d829"}]
}
```

### 3.4 aria2.onDownloadStop

Sent when a download is stopped by user action.

```json
{
  "jsonrpc": "2.0",
  "method": "aria2.onDownloadStop",
  "params": [{"gid": "2089b05ecca3d829"}]
}
```

### 3.5 aria2.onDownloadComplete

Sent when a download completes (for BitTorrent, after seeding ends).

```json
{
  "jsonrpc": "2.0",
  "method": "aria2.onDownloadComplete",
  "params": [{"gid": "2089b05ecca3d829"}]
}
```

### 3.6 aria2.onDownloadError

Sent when a download stops due to an error.

```json
{
  "jsonrpc": "2.0",
  "method": "aria2.onDownloadError",
  "params": [{"gid": "2089b05ecca3d829"}]
}
```

### 3.7 aria2.onBtDownloadComplete

Sent when a torrent download finishes downloading but is still seeding.

```json
{
  "jsonrpc": "2.0",
  "method": "aria2.onBtDownloadComplete",
  "params": [{"gid": "2089b05ecca3d829"}]
}
```

---

## 4. Error Response Format

### 4.1 Standard JSON-RPC Errors

By JSON-RPC 2.0 specification:

```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "error": {
    "code": -32601,
    "message": "Method not found"
  }
}
```

Standard error codes used:
| Code | Meaning |
|------|---------|
| `-32700` | Parse error — invalid JSON |
| `-32600` | Invalid Request — not a valid JSON-RPC request |
| `-32601` | Method not found |
| `-32602` | Invalid params — wrong parameter types or count |
| `-32603` | Internal error |

`code` is a JSON integer (not a string). `message` is a JSON string.

### 4.2 Aria2-Specific Errors

All aria2-specific errors use `code: 1` with a descriptive message:

```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "error": {
    "code": 1,
    "message": "Active Download not found for GID#2089b05ecca3d829"
  }
}
```

Common error messages:
| Message | Trigger |
|---------|---------|
| `"Duplicate download"` | addUri with same URIs+options already queued |
| `"Duplicate info hash"` | addTorrent/addUri(magnet) with same info hash |
| `"Active Download not found for GID#<gid>"` | remove/pause/unpause/tellStatus/getFiles/getUris/getPeers/getServers/getOption/changeOption with unknown GID |
| `"GID#<gid> is not complete/error/removed"` | removeDownloadResult targeting active/waiting/paused download |
| `"Could not parse metalink XML data."` | addMetalink with malformed metalink |
| `"We cannot change the option for the existing download: <name>"` | changeOption with a forbidden option name |
| `"Could not parse bencoded data"` | addTorrent with invalid base64 or non-torrent data |

### 4.3 Error in Multicall

When a nested call in `system.multicall` fails, the corresponding result element is:

```json
{"faultCode": 1, "faultString": "Active Download not found for GID#abcd"}
```

Keys are in alphabetical order: `faultCode` (integer, not string), `faultString` (string). This is NOT the standard JSON-RPC error envelope.

### 4.4 When Secret Token is Required

If `--rpc-secret` is configured and a request omits the secret token:

```json
{
  "id": "qwer",
  "jsonrpc": "2.0",
  "error": {
    "code": 1,
    "message": "Unauthorized"
  }
}
```

The secret token is NOT required for `system.listMethods` and `system.listNotifications`.

---

## 5. Secret Token Format

In the JSON-RPC params array, the secret token is the **first element** and follows this format:

```
"token:<actual-secret>"
```

Example:
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.getGlobalStat",
  "params": ["token:mysecret"]
}
```

For methods with additional params, the secret token is always first:
```json
{
  "jsonrpc": "2.0",
  "id": "qwer",
  "method": "aria2.tellStatus",
  "params": ["token:mysecret", "2089b05ecca3d829"]
}
```

When the secret is not configured (no `--rpc-secret` set), the secret token must NOT be present in any params array. The server rejects params arrays that begin with a token-prefixed string when no secret is configured.

---

## 6. HTTP GET Encoding

For JSON-RPC over HTTP GET, the endpoint is `/jsonrpc` with query parameters:

```
/jsonrpc?method=<METHOD>&id=<ID>&params=<BASE64_ENCODED_JSON_PARAMS>
```

- `method` and `id` are URL-encoded strings (UTF-8).
- `params` is base64-encoded JSON array, then percent-encoded. Standard base64 (with `+`, `/`, `=`).
- For Batch requests, omit `method` and `id`; the entire array of request objects is base64-encoded in `params`.
- JSONP support: add `jsoncallback=<function-name>` parameter.

---

## 7. Empty Array vs Missing Key

A critical distinction for byte-compat:

- **Empty array `[]`:** Returned when the result is a list but there are no elements. Examples:
  - `getPeers` for a non-BT download returns `[]`
  - `tellActive` with no active downloads returns `[]`
  
- **Missing key (absent from object):** Returned when a field is not applicable. Examples:
  - `bitfield` absent from tellStatus when download not started
  - `infoHash` absent for non-BT downloads
  - `errorCode` absent for active/waiting/paused downloads
  - `followedBy` absent when no child downloads
  - `bittorrent` absent for non-BT downloads

- **The `files` array:** For magnet downloads where metadata hasn't been resolved, the `files` array in tellStatus is `[]` (empty array, present).

---

## 8. Value Type Summary

| Result Type | format | Methods |
|-------------|--------|---------|
| **GID string** | 16-char lowercase hex | addUri, addTorrent, remove, forceRemove, pause, forcePause, unpause |
| **GID array** | array of 16-char lowercase hex strings | addMetalink |
| **OK string** | `"OK"` | pauseAll, forcePauseAll, unpauseAll, changeOption, changeGlobalOption, purgeDownloadResult, removeDownloadResult, shutdown, forceShutdown, saveSession |
| **Status struct** | struct with 10-22 keys | tellStatus, tellActive, tellWaiting, tellStopped |
| **URI array** | array of `{status, uri}` structs | getUris |
| **Files array** | array of `{completedLength, index, length, path, selected, uris}` structs | getFiles |
| **Peers array** | array of 9-key peer structs | getPeers |
| **Servers array** | array of `{index, servers}` structs | getServers |
| **Integer** | JSON number | changePosition |
| **Integer array** | `[int, int]` JSON numbers | changeUri |
| **Options struct** | mapping of option-name to string-value | getOption, getGlobalOption |
| **Global stat struct** | 6-key struct, all string values | getGlobalStat |
| **Version struct** | `{enabledFeatures, version}` | getVersion |
| **Session struct** | `{sessionId}` | getSessionInfo |
| **Method list** | array of strings | listMethods |
| **Notification list** | array of strings | listNotifications |
| **Multicall results** | array of `[result]` arrays or `{faultCode, faultString}` objects | multicall |
