# RPC Methods Contract

**Source:** aria2 1.37.0 manual `doc/manual-src/en/aria2c.rst` (CC-BY).
**Schema:** Every method name, parameter type, return structure, and edge case described below must be reproduced exactly by the Go implementation.
**Status:** Spec-authored from clean-room reading of the aria2 public manual; zero aria2 C++ source LOC.

---

## Protocol Overview

aria2 implements JSON-RPC 2.0 over HTTP and WebSocket, and XML-RPC over HTTP. All method signatures and response structures are identical across transports except where noted.

- JSON-RPC endpoint: `/jsonrpc` (HTTP POST/GET, WebSocket `ws://`/`wss://` at same path)
- XML-RPC endpoint: `/rpc` (HTTP POST only)
- Encoding: UTF-8 exclusively
- All numeric values in responses are transmitted as **strings** (not JSON numbers)
- Floating point numbers are not supported in JSON-RPC
- WebSocket version: 13 (RFC 6455)

### Secret Token Authorization

When `--rpc-secret` is set, every method call (except `system.listMethods` and `system.listNotifications`) must include the secret as the first positional parameter, prefixed with `token:`. The server strips this prefix before dispatch. For `system.multicall`, each nested call independently provides the token.

For JSON-RPC Batch (array of request objects), each request object may independently include the token in its params.

Timing attacks are mitigated by making token comparison constant-time at the server side.

### GID Format

Download identifiers (GIDs) are 64-bit numeric values. Over RPC they are represented as 16-character lowercase hex strings (e.g., `2089b05ecca3d829`). When querying by GID, any unique prefix is accepted.

### Multi-Value Options

Options that accept multiple values on the command line (e.g., `--header`, `--index-out`) may be passed as either a single string or an array of strings in the options struct.

---

## Methods

### 1. aria2.addUri

Adds a new HTTP/FTP/SFTP/BitTorrent magnet download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token (`token:...`), omitted when not configured |
| `uris` | string[] | Array of URIs pointing to the same resource. For magnet links, exactly one element. |
| `[options]` | struct | Key-value pairs of option names to values. See the Input File options set. |
| `[position]` | integer | 0-based queue insertion index. Append if omitted or beyond queue end. |

**Returns:** string — the GID of the newly created download.

**Errors:**
- `DUPLICATE_DOWNLOAD` (11) — same URIs and options already queued/active
- `DUPLICATE_INFO_HASH` (12) — same info hash already queued/active (magnet)
- `RESOURCE_NOT_FOUND` (3) — all URIs unresolvable (may be deferred, not immediate return)

**Notes:**
- If URIs point to different resources (not mirrors), the download may silently corrupt.
- The `position` parameter can be omitted entirely if no options are specified by passing an empty struct `{}` for options.

---

### 2. aria2.addTorrent

Adds a BitTorrent download from a `.torrent` file uploaded via RPC.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `torrent` | string | Base64-encoded contents of the `.torrent` file |
| `[uris]` | string[] | Web-seeding URIs (optional) |
| `[options]` | struct | Key-value option pairs |
| `[position]` | integer | 0-based queue insertion index |

**Returns:** string — the GID of the newly created download.

**Errors:**
- `DUPLICATE_INFO_HASH` (12) — torrent with same info hash already exists
- Generic parse error if the base64 payload is not a valid torrent

**Notes:**
- For single-file torrents, web-seed URIs may end with `/` (the filename from the torrent is appended).
- For multi-file torrents, the torrent name/path is appended to each URI.
- When `--rpc-save-upload-metadata` is true, the uploaded torrent data is saved to a file named `<sha1-hex>.torrent` in the download directory.
- Downloads added with `--rpc-save-upload-metadata=false` are excluded from session saving.

---

### 3. aria2.addMetalink

Adds downloads from a `.metalink` (Metalink v4 / RFC 5854) file uploaded via RPC.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `metalink` | string | Base64-encoded contents of the `.metalink` file |
| `[options]` | struct | Key-value option pairs |
| `[position]` | integer | 0-based queue insertion index |

**Returns:** string[] — array of GIDs for each download described in the metalink.

**Errors:**
- `METALINK_PARSE_ERROR` (20) — malformed metalink XML
- Duplicate info hash errors for any BT components

**Notes:**
- A single metalink file can produce multiple downloads. The return value is always an array.
- When `--rpc-save-upload-metadata` is true, the uploaded metalink data is saved as `<sha1-hex>.metalink`.

---

### 4. aria2.remove

Removes a download. If active, stops it first.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) of the download |

**Returns:** string — the GID of the removed download.

**Errors:**
- Method returns an error if the GID is not found.

**Notes:**
- Status becomes `removed`.
- Graceful: contacts trackers to unregister, closes connections cleanly.

---

### 5. aria2.forceRemove

Removes a download immediately without graceful cleanup.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) of the download |

**Returns:** string — the GID of the removed download.

**Notes:**
- Skips tracker unregistration and other time-consuming cleanup. Faster than `aria2.remove`.

---

### 6. aria2.pause

Pauses a download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) of the download |

**Returns:** string — the GID of the paused download.

**Notes:**
- Status becomes `paused`.
- If the download was active, it is moved to the front of the waiting queue.
- A paused download will not start until `aria2.unpause` is called.
- Graceful: contacts trackers first.

---

### 7. aria2.pauseAll

Pauses every active and waiting download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** string — `"OK"`.

---

### 8. aria2.forcePause

Pauses a download without graceful cleanup.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |

**Returns:** string — the GID of the paused download.

**Notes:**
- Same as `aria2.pause` but skips tracker unregistration.

---

### 9. aria2.forcePauseAll

Pauses all active/waiting downloads without graceful cleanup.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** string — `"OK"`.

---

### 10. aria2.unpause

Changes a download's status from `paused` to `waiting`.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |

**Returns:** string — the GID of the unpaused download.

**Notes:**
- Makes the download eligible for restart. Does not guarantee immediate start.

---

### 11. aria2.unpauseAll

Unpauses all paused downloads.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** string — `"OK"`.

---

### 12. aria2.tellStatus

Returns detailed progress information for a download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |
| `[keys]` | string[] | Optional whitelist of keys to return; all keys if omitted |

**Returns:** struct with the following keys. All values are strings unless noted.

| Key | Type | Description |
|-----|------|-------------|
| `gid` | string | GID of this download |
| `status` | string | One of: `active`, `waiting`, `paused`, `error`, `complete`, `removed` |
| `totalLength` | string | Total size in bytes |
| `completedLength` | string | Bytes completed |
| `uploadLength` | string | Bytes uploaded |
| `bitfield` | string | Hex bitfield of piece completion. Most significant bit = piece 0. May be absent if download not started. |
| `downloadSpeed` | string | Download speed in bytes/sec |
| `uploadSpeed` | string | Upload speed in bytes/sec |
| `infoHash` | string | BitTorrent info hash (hex). BT only. May be absent. |
| `numSeeders` | string | Connected seeders. BT only. |
| `seeder` | string | `"true"` if local is seeder. BT only. |
| `pieceLength` | string | Piece size in bytes |
| `numPieces` | string | Total number of pieces |
| `connections` | string | Number of connections |
| `errorCode` | string | Last error code (0-32) if stopped/completed. Absent otherwise. |
| `errorMessage` | string | Human-readable error description |
| `followedBy` | string[] | List of GIDs generated from this download (e.g., metalink sub-downloads). Absent if none. |
| `following` | string | Reverse of `followedBy` — GID of the parent download. Absent if none. |
| `belongsTo` | string | GID of the containing download (e.g., torrent download that owns a file). Absent if none. |
| `dir` | string | Download directory |
| `files` | struct[] | Array of file objects, same structure as `aria2.getFiles` return value |
| `bittorrent` | struct | BT metadata. Contains `announceList`, `comment`, `creationDate`, `mode`, `info` (with `name`). BT only. Absent for non-BT. |
| `verifiedLength` | string | Bytes verified during hash checking. Exists only during hash check. |
| `verifyIntegrityPending` | string | `"true"` if waiting for hash check in queue. Exists only when queued. |

---

### 13. aria2.getUris

Returns the URIs associated with a download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |

**Returns:** struct[] — array of URI objects:

| Key | Type | Description |
|-----|------|-------------|
| `uri` | string | The URI string |
| `status` | string | `"used"` if the URI is actively in use; `"waiting"` if still queued |

---

### 14. aria2.getFiles

Returns the file list of a download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |

**Returns:** struct[] — array of file objects:

| Key | Type | Description |
|-----|------|-------------|
| `index` | string | 1-based file index, matching order in multi-file torrent |
| `path` | string | Full file path |
| `length` | string | File size in bytes |
| `completedLength` | string | Completed bytes for this file (complete pieces only — less than or equal to `tellStatus.completedLength` which counts partial pieces) |
| `selected` | string | `"true"` if selected by `--select-file`; always `"true"` for single-file or non-torrent downloads |
| `uris` | struct[] | Array of `{uri, status}` objects (same as `getUris` return) |

---

### 15. aria2.getPeers

Returns connected peers for a BitTorrent download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |

**Returns:** struct[] — array of peer objects (BT only; empty for non-BT):

| Key | Type | Description |
|-----|------|-------------|
| `peerId` | string | Percent-encoded peer ID |
| `ip` | string | IP address of the peer |
| `port` | string | Port number |
| `bitfield` | string | Hex bitfield of peer's piece availability |
| `amChoking` | string | `"true"` if aria2 is choking this peer |
| `peerChoking` | string | `"true"` if the peer is choking aria2 |
| `downloadSpeed` | string | Download speed from this peer (bytes/sec) |
| `uploadSpeed` | string | Upload speed to this peer (bytes/sec) |
| `seeder` | string | `"true"` if the peer is a seeder |

---

### 16. aria2.getServers

Returns connected HTTP(S)/FTP/SFTP servers for a download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |

**Returns:** struct[] — array of file-indexed server groups:

| Key | Type | Description |
|-----|------|-------------|
| `index` | string | 1-based file index in the multi-file metalink order |
| `servers` | struct[] | Array of server objects: `{uri, currentUri, downloadSpeed}` |
| `servers[].uri` | string | Original URI |
| `servers[].currentUri` | string | Currently connected URI (may differ after redirection) |
| `servers[].downloadSpeed` | string | Download speed from this server (bytes/sec) |

---

### 17. aria2.tellActive

Returns all active downloads.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `[keys]` | string[] | Optional key whitelist, same semantics as `aria2.tellStatus` |

**Returns:** struct[] — array of download status structs (same structure as `aria2.tellStatus` return).

---

### 18. aria2.tellWaiting

Returns waiting downloads (including paused ones).

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `offset` | integer | Starting offset. 0 = front. Negative values index from the end (-1 = last, -2 = second to last, etc.) |
| `num` | integer | Maximum number of downloads to return |
| `[keys]` | string[] | Optional key whitelist |

**Returns:** struct[] — array of download status structs (same structure as `aria2.tellStatus` return).

**Notes:**
- The range is `[offset, offset + num)`. If offset is negative, results are in reversed order.
- Example: With queue ["A","B","C"], `tellWaiting(0,1)` → `["A"]`, `tellWaiting(1,2)` → `["B","C"]`, `tellWaiting(-1,2)` → `["C","B"]`.

---

### 19. aria2.tellStopped

Returns stopped downloads (completed, error, removed).

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `offset` | integer | Starting offset from the least recently stopped download. Same negative-index semantics as `tellWaiting`. |
| `num` | integer | Maximum number of downloads to return |
| `[keys]` | string[] | Optional key whitelist |

**Returns:** struct[] — array of download status structs (same structure as `aria2.tellStatus` return).

---

### 20. aria2.changePosition

Moves a download within the queue.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |
| `pos` | integer | Position delta or absolute position |
| `how` | string | `"POS_SET"` (absolute from front), `"POS_CUR"` (relative to current), `"POS_END"` (relative to end) |

**Returns:** integer — the resulting position in the queue.

**Notes:**
- If the computed destination is less than 0, the download moves to position 0.
- If beyond the end, it moves to the end of the queue.

---

### 21. aria2.changeUri

Modifies the URI list for a specific file within a download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |
| `fileIndex` | integer | 1-based file index within the download |
| `delUris` | string[] | URIs to remove |
| `addUris` | string[] | URIs to append |
| `[position]` | integer | 0-based insertion position within the file's URI list (after deletions). Append if omitted. |

**Returns:** integer[2] — `[numDeleted, numAdded]`.

**Notes:**
- Removals execute before additions.
- To remove N identical URIs, specify the URI N times in `delUris`. Each removal removes one instance.

---

### 22. aria2.getOption

Returns the options set on a specific download.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |

**Returns:** struct — key-value pairs of option names to string values.

**Notes:**
- Only returns options that have been explicitly set (via CLI, config file, or RPC). Options that hold default values and were never set are omitted from the response.

---

### 23. aria2.changeOption

Changes options of a download dynamically.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |
| `options` | struct | Key-value pairs of option names to string values |

**Returns:** string — `"OK"`.

**Notes:**
- Most options from the Input File set are available, except: `dry-run`, `metalink-base-uri`, `parameterized-uri`, `pause`, `piece-length`, `rpc-save-upload-metadata`.
- Changing most options of an active download causes it to restart automatically.
- The following options can be changed on active downloads **without** restart: `bt-max-peers`, `bt-request-peer-speed-limit`, `bt-remove-unselected-file`, `force-save`, `max-download-limit`, `max-upload-limit`.

---

### 24. aria2.getGlobalOption

Returns the global options.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** struct — key-value pairs of global option names to string values.

**Notes:**
- Same omission rule as `aria2.getOption`: only explicitly set options are returned.
- Global options serve as templates for newly added downloads.

---

### 25. aria2.changeGlobalOption

Changes global options dynamically.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `options` | struct | Key-value pairs of option names to string values |

**Returns:** string — `"OK"`.

**Notes:**
- Available options: `bt-max-open-files`, `download-result`, `keep-unfinished-download-result`, `log`, `log-level`, `max-concurrent-downloads`, `max-download-result`, `max-overall-download-limit`, `max-overall-upload-limit`, `optimize-concurrent-downloads`, `save-cookies`, `save-session`, `server-stat-of`.
- Additionally, most Input File options are available except: `checksum`, `index-out`, `out`, `pause`, `select-file`.
- Setting `log` to an empty string stops logging. Log files are always opened in append mode.

---

### 26. aria2.getGlobalStat

Returns aggregate download statistics.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** struct:

| Key | Type | Description |
|-----|------|-------------|
| `downloadSpeed` | string | Aggregate download speed in bytes/sec |
| `uploadSpeed` | string | Aggregate upload speed in bytes/sec |
| `numActive` | string | Number of active downloads |
| `numWaiting` | string | Number of waiting downloads |
| `numStopped` | string | Number of stopped downloads (capped by `--max-download-result`) |
| `numStoppedTotal` | string | Total stopped downloads in this session (uncapped) |

---

### 27. aria2.purgeDownloadResult

Removes all completed/error/removed download records from memory.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** string — `"OK"`.

---

### 28. aria2.removeDownloadResult

Removes a single completed/error/removed download from memory.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |
| `gid` | string | GID (or unique prefix) |

**Returns:** string — `"OK"`.

**Notes:**
- Only operates on downloads in `complete`, `error`, or `removed` state. Active or waiting downloads are unaffected.

---

### 29. aria2.getVersion

Returns the aria2 version and enabled features.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** struct:

| Key | Type | Description |
|-----|------|-------------|
| `version` | string | Version string (e.g., `"1.37.0"`) |
| `enabledFeatures` | string[] | List of feature names (e.g., `"Async DNS"`, `"BitTorrent"`, `"HTTPS"`, `"Metalink"`, `"XML-RPC"`, `"GZip"`, `"Message Digest"`, `"Firefox3 Cookie"`, `"WebSocket"`, `"SFTP"`) |

---

### 30. aria2.getSessionInfo

Returns session identification information.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** struct:

| Key | Type | Description |
|-----|------|-------------|
| `sessionId` | string | Randomly generated session ID (hex string). Generated anew each time aria2 starts. |

---

### 31. aria2.shutdown

Shuts down aria2 gracefully.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** string — `"OK"`.

**Notes:**
- Graceful: saves session, contacts trackers, flushes data, then exits.

---

### 32. aria2.forceShutdown

Shuts down aria2 immediately without graceful cleanup.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** string — `"OK"`.

**Notes:**
- Skips tracker unregistration and other time-consuming cleanup. Faster than `aria2.shutdown`.

---

### 33. aria2.saveSession

Saves the current session to the file specified by `--save-session`.

| Field | Type | Description |
|-------|------|-------------|
| `[secret]` | string | RPC secret token |

**Returns:** string — `"OK"` on success.

---

### 34. system.multicall

Executes multiple RPC methods in a single request.

| Field | Type | Description |
|-------|------|-------------|
| `methods` | struct[] | Array of call descriptors. Each is `{"methodName": string, "params": array}`. |

**Returns:** array — one element per call descriptor. Each element is either `[result]` (a single-element array with the return value) or a fault struct `{faultCode, faultString}` if the sub-call failed.

**Notes:**
- For XML-RPC: `system.multicall` is the standard XML-RPC multi-call method. The secret token must be included as the first parameter of each nested call's params array, NOT as a top-level parameter.
- For JSON-RPC: both `system.multicall` and JSON-RPC Batch (array of request objects as the top-level JSON value) are supported.
- JSON-RPC Batch requests: omit `method` and `id` at the top level; each array element is a full JSON-RPC request object.

---

### 35. system.listMethods

Returns all available RPC method names.

| Field | Type | Description |
|-------|------|-------------|
| *(none)* | | No parameters; no secret token required |

**Returns:** string[] — array of method name strings.

**Notes:**
- Does **not** require the secret token. Safe to call without authentication since it only exposes method names.
- As of aria2 1.37.0, the returned list includes all methods in this document.

---

### 36. system.listNotifications

Returns all available notification names.

| Field | Type | Description |
|-------|------|-------------|
| *(none)* | | No parameters; no secret token required |

**Returns:** string[] — array of notification name strings.

**Notes:**
- Does **not** require the secret token. Safe to call without authentication.
- As of aria2 1.37.0, the returned list includes all notifications in this document.

---

## Notifications

Notifications are server-to-client messages sent over WebSocket only. They lack an `id` field in the JSON-RPC frame and the client must not respond.

### 1. aria2.onDownloadStart

Sent when a download starts.

**Parameters:** struct `event`:

| Key | Type | Description |
|-----|------|-------------|
| `gid` | string | GID of the started download |

---

### 2. aria2.onDownloadPause

Sent when a download is paused.

**Parameters:** struct `event` — same as `aria2.onDownloadStart` (contains `gid`).

---

### 3. aria2.onDownloadStop

Sent when a download is stopped by the user.

**Parameters:** struct `event` — same as `aria2.onDownloadStart` (contains `gid`).

---

### 4. aria2.onDownloadComplete

Sent when a download completes. For BitTorrent, this fires after seeding finishes.

**Parameters:** struct `event` — same as `aria2.onDownloadStart` (contains `gid`).

---

### 5. aria2.onDownloadError

Sent when a download stops due to an error.

**Parameters:** struct `event` — same as `aria2.onDownloadStart` (contains `gid`).

---

### 6. aria2.onBtDownloadComplete

Sent when a torrent download finishes downloading but is still seeding.

**Parameters:** struct `event` — same as `aria2.onDownloadStart` (contains `gid`).

---

## Error Response Format

### JSON-RPC

```json
{
  "jsonrpc": "2.0",
  "id": "<request-id>",
  "error": {
    "code": 1,
    "message": "<error description>"
  }
}
```

JSON-RPC error codes follow the standard: `-32700` (parse error), `-32600` (invalid request), `-32601` (method not found), `-32602` (invalid params), `-32603` (internal error). Aria2-specific errors use code `1` with a human-readable message.

### XML-RPC

XML-RPC faults use `faultCode=1` with the error message in `faultString`.

---

## Transport-Specific Notes

### JSON-RPC over HTTP GET

Parameters are base64-encoded JSON arrays passed as query parameters:
```
/jsonrpc?method=<METHOD>&id=<ID>&params=<BASE64_ENCODED_PARAMS>
```

The `method` and `id` are always treated as JSON strings (UTF-8). For batch requests, omit `method` and `id` and encode the entire array of request objects into `params`.

JSONP is supported via the `jsoncallback` query parameter.

### JSON-RPC over WebSocket

Same method signatures and response format as HTTP. Notifications (unidirectional server-to-client messages) are only available over WebSocket. Client sends requests as Text frames; server sends responses as Text frames. Version 13 per RFC 6455.

---

## Summary Table

| # | Method | Parameters | Returns |
|---|--------|-----------|---------|
| 1 | `aria2.addUri` | `[secret], uris, [options], [position]` | GID string |
| 2 | `aria2.addTorrent` | `[secret], torrent, [uris], [options], [position]` | GID string |
| 3 | `aria2.addMetalink` | `[secret], metalink, [options], [position]` | GID[] |
| 4 | `aria2.remove` | `[secret], gid` | GID string |
| 5 | `aria2.forceRemove` | `[secret], gid` | GID string |
| 6 | `aria2.pause` | `[secret], gid` | GID string |
| 7 | `aria2.pauseAll` | `[secret]` | `"OK"` |
| 8 | `aria2.forcePause` | `[secret], gid` | GID string |
| 9 | `aria2.forcePauseAll` | `[secret]` | `"OK"` |
| 10 | `aria2.unpause` | `[secret], gid` | GID string |
| 11 | `aria2.unpauseAll` | `[secret]` | `"OK"` |
| 12 | `aria2.tellStatus` | `[secret], gid, [keys]` | struct (19+ keys) |
| 13 | `aria2.getUris` | `[secret], gid` | `[{uri, status}]` |
| 14 | `aria2.getFiles` | `[secret], gid` | `[{index, path, length, completedLength, selected, uris}]` |
| 15 | `aria2.getPeers` | `[secret], gid` | `[{peerId, ip, port, bitfield, amChoking, peerChoking, downloadSpeed, uploadSpeed, seeder}]` |
| 16 | `aria2.getServers` | `[secret], gid` | `[{index, servers: [{uri, currentUri, downloadSpeed}]}]` |
| 17 | `aria2.tellActive` | `[secret], [keys]` | struct[] (tellStatus shape) |
| 18 | `aria2.tellWaiting` | `[secret], offset, num, [keys]` | struct[] (tellStatus shape) |
| 19 | `aria2.tellStopped` | `[secret], offset, num, [keys]` | struct[] (tellStatus shape) |
| 20 | `aria2.changePosition` | `[secret], gid, pos, how` | integer |
| 21 | `aria2.changeUri` | `[secret], gid, fileIndex, delUris, addUris, [position]` | `[numDeleted, numAdded]` |
| 22 | `aria2.getOption` | `[secret], gid` | struct (option→value) |
| 23 | `aria2.changeOption` | `[secret], gid, options` | `"OK"` |
| 24 | `aria2.getGlobalOption` | `[secret]` | struct (option→value) |
| 25 | `aria2.changeGlobalOption` | `[secret], options` | `"OK"` |
| 26 | `aria2.getGlobalStat` | `[secret]` | `{downloadSpeed, uploadSpeed, numActive, numWaiting, numStopped, numStoppedTotal}` |
| 27 | `aria2.purgeDownloadResult` | `[secret]` | `"OK"` |
| 28 | `aria2.removeDownloadResult` | `[secret], gid` | `"OK"` |
| 29 | `aria2.getVersion` | `[secret]` | `{version, enabledFeatures}` |
| 30 | `aria2.getSessionInfo` | `[secret]` | `{sessionId}` |
| 31 | `aria2.shutdown` | `[secret]` | `"OK"` |
| 32 | `aria2.forceShutdown` | `[secret]` | `"OK"` |
| 33 | `aria2.saveSession` | `[secret]` | `"OK"` |
| 34 | `system.multicall` | `methods` | array of results/faul |
| 35 | `system.listMethods` | *(none)* | string[] |
| 36 | `system.listNotifications` | *(none)* | string[] |

## Notification Summary

| # | Notification | Event Parameter |
|---|-------------|----------------|
| 1 | `aria2.onDownloadStart` | `{gid}` |
| 2 | `aria2.onDownloadPause` | `{gid}` |
| 3 | `aria2.onDownloadStop` | `{gid}` |
| 4 | `aria2.onDownloadComplete` | `{gid}` |
| 5 | `aria2.onDownloadError` | `{gid}` |
| 6 | `aria2.onBtDownloadComplete` | `{gid}` |
