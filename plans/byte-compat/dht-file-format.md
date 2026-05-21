# DHT Routing Table File Format (`dht.dat`)

Aria2 persists its DHT routing table to a binary file (default `dht.dat`) so that
previously discovered DHT nodes survive restarts. This file is read on startup
(loaded into the routing table) and written on graceful shutdown.

---

## 1. File-level structure

The file is a single contiguous binary blob with no framing or trailer. The
overall layout is:

```
┌──────────────────────────────────────────────────────────┐
│  Header (8 bytes)                                        │
├──────────────────────────────────────────────────────────┤
│  Timestamp (8 bytes, or 4 bytes for version-2)           │
│  (+ 4 bytes reserved after timestamp for version-2 only) │
├──────────────────────────────────────────────────────────┤
│  Local node record (32 bytes)                            │
├──────────────────────────────────────────────────────────┤
│  Node count (4 bytes)                                    │
│  + 4 bytes reserved (zero)                               │
├──────────────────────────────────────────────────────────┤
│  Node entry 1 (56 bytes)                                 │
├──────────────────────────────────────────────────────────┤
│  Node entry 2 (56 bytes)                                 │
├──────────────────────────────────────────────────────────┤
│  ...                                                     │
├──────────────────────────────────────────────────────────┤
│  Node entry N (56 bytes)                                 │
└──────────────────────────────────────────────────────────┘
```

Total file size = 8 + 8 + 32 + 4 + 4 + (N × 56) = 56 + (N × 56) bytes for version-3.

---

## 2. Header (bytes 0–7)

| Offset | Size | Field     | Value / Description                              |
|--------|------|-----------|--------------------------------------------------|
| 0      | 1    | Magic 0   | `0xa1`                                           |
| 1      | 1    | Magic 1   | `0xa2`                                           |
| 2      | 1    | Format ID | `0x02` (identifies this as a DHT routing table)  |
| 3      | 1    | Reserved  | `0x00`                                           |
| 4      | 1    | Reserved  | `0x00`                                           |
| 5      | 1    | Reserved  | `0x00`                                           |
| 6      | 1    | Version hi| `0x00` (upper byte of version, always 0)         |
| 7      | 1    | Version lo| `0x03` for version 3; historically `0x02` for v2 |

Aria2 produces two valid header byte sequences:

| Version | Byte sequence (hex)                |
|---------|------------------------------------|
| 2       | `a1 a2 02 00 00 00 00 02`        |
| 3       | `a1 a2 02 00 00 00 00 03`        |

Any other byte sequence in the first 8 bytes must be treated as a corrupt or
unknown-format file.

---

## 3. Timestamp (version-dependent layout)

### Version 3 (current)

| Offset (from start) | Size | Field     | Endian      |
|---------------------|------|-----------|-------------|
| 8                   | 8    | timestamp | big-endian  |

The timestamp is a 64-bit unsigned integer representing Unix epoch seconds, in
network byte order (`hton64` / `ntoh64`).

### Version 2 (legacy)

| Offset (from start) | Size | Field            | Endian      |
|---------------------|------|------------------|-------------|
| 8                   | 4    | timestamp (u32)  | big-endian  |
| 12                  | 4    | reserved (zero)  | —           |

The version-2 timestamp is a 32-bit unsigned integer in network byte order.
Implementations supporting version-3 should fall back to this 32-bit read when
the header indicates version 2.

---

## 4. Local node record (always 32 bytes)

| Offset in record | Size | Field           | Description                        |
|------------------|------|-----------------|------------------------------------|
| 0                | 8    | reserved        | All zero bytes                     |
| 8                | 20   | local node ID   | 160-bit DHT node ID, raw bytes     |
| 28               | 4    | reserved        | All zero bytes                     |

The local node ID is the same 20-byte `DHT_ID_LENGTH` value used throughout the
Kademlia protocol. Fields marked "reserved" are always written as zeros by aria2
and must be consumed (and ignored) by any reader.

---

## 5. Node count

| Offset (after local node record) | Size | Field        | Endian      |
|----------------------------------|------|--------------|-------------|
| +0                               | 4    | node count   | big-endian  |
| +4                               | 4    | reserved     | zero        |

Node count is a 32-bit unsigned integer in network byte order (`htonl` / `ntohl`).
This is the number of node entries that follow.

---

## 6. Node entry (56 bytes each)

Every node entry is exactly 56 bytes long regardless of the IP address family
(IPv4 or IPv6). This fixed size is achieved through internal padding.

| Offset in entry | Size  | Field                         | Description                                          |
|-----------------|-------|-------------------------------|------------------------------------------------------|
| 0               | 1     | compact peer length           | `6` for IPv4, `18` for IPv6                          |
| 1               | 7     | reserved                      | All zero bytes                                       |
| 8               | clen  | compact peer info             | IP address + port (see §6.1)                        |
| 8 + clen        | 24–clen | reserved                   | Zero padding; total IP/port area is always 24 bytes  |
| 32              | 20    | node ID                       | 160-bit DHT node ID, raw bytes                       |
| 52              | 4     | reserved                      | All zero bytes                                       |

Where `clen` = the value in the first byte of the entry (always 6 or 18).

### 6.1 Compact peer info format

The compact peer info is the standard BitTorrent "compact IP-address/port info"
form, identical to that used in tracker responses and DHT `find_node` replies:

| Family | clen | Layout                                             |
|--------|------|----------------------------------------------------|
| IPv4   | 6    | 4 bytes raw IPv4 address (big-endian) + 2 bytes port (big-endian, `htons`) |
| IPv6   | 18   | 16 bytes raw IPv6 address (big-endian) + 2 bytes port (big-endian, `htons`) |

The IP address is stored as the raw binary address (e.g., `0xc0 0xa8 0x01 0x01`
for `192.168.1.1`). The port is a 16-bit unsigned integer in network byte order.

---

## 7. Endianness summary

| Data type            | Endianness  |
|----------------------|-------------|
| Timestamp (u64)      | big-endian  |
| Timestamp v2 (u32)   | big-endian  |
| Node count (u32)     | big-endian  |
| Port in compact info | big-endian  |
| IP address in compact| raw binary (network order / big-endian) |
| Node IDs             | raw bytes (no endianness concern) |
| Magic / version / reserved | raw bytes |

---

## 8. Serialization order

Nodes are written to the file in the same order they appear in aria2's internal
`std::vector<std::shared_ptr<DHTNode>>`. Aria2's DHT routing table collects
nodes from its internal buckets in no particular order — implementations should
reproduce the same non-deterministic order (any iteration through the routing
table buckets is acceptable; compat tests should not depend on node order).

The local node is always written before the node entries, in its own dedicated
record.

---

## 9. Atomic write protocol

Aria2 does not write directly to the target file. Instead:

1. Write all data to a temporary file with suffix `__temp` appended to the
   target filename (e.g., `dht.dat` → `dht.dat__temp`).
2. If the write completes successfully, atomically rename the temporary file
   over the target file.
3. If any step fails, the temporary file is abandoned (not renamed) and the
   existing `dht.dat` remains untouched.

This prevents corruption from partial writes (e.g., process killed mid-write).

---

## 10. Versioning

### Format version negotiation on read

The reader checks the 8-byte header against two known patterns:

1. If the header matches the version-3 pattern (magic `a1 a2`, format ID `02`,
   version `00 03`), the file is parsed as a version-3 file with a 64-bit
   timestamp.
2. If the header matches the version-2 pattern (magic `a1 a2`, format ID `02`,
   version `00 02`), the file is parsed as a version-2 file with a 32-bit
   timestamp (followed by 4 reserved bytes).
3. If the header matches neither pattern, the file is rejected with a "bad
   header" error.

There is no version-1 header recognized by aria2's deserializer; version 2 is
the earliest supported.

### Version determination on write

Aria2 always writes the version-3 header. The writer does not consult any
configuration option; the output format version is fixed.

### No version migration logic

Aria2 does not rewrite old files on disk. If a version-2 file exists, it reads
it in-place and writes a version-3 file on the next shutdown. No explicit
migration step is performed.

---

## 11. Error handling on read

Implementations must handle the following error conditions:

| Condition                                    | Behaviour                                      |
|----------------------------------------------|------------------------------------------------|
| File does not exist                          | Return empty routing table (no error thrown; silent skip) |
| File exists but first 8 bytes ≠ any known header | Reject file; log "bad header"; return empty table |
| File too short for expected record count     | Reject file; log read failure                  |
| Any `read()` returns fewer bytes than requested | Reject file; log read failure                |
| Node entry: compact peer length ≠ expected length for family | Skip that node entry (read and discard its 55 remaining bytes); continue processing remaining entries |
| Node entry: compact peer info is all zeros   | Skip that node entry; continue processing      |
| Node entry: compact peer info decodes to an empty address string | Skip that node entry; continue processing |

On any unrecoverable error (bad header, truncated file, I/O failure), the
implementation must return an empty routing table — never a partial table — and
must not crash or panic.

### Error handling on write

| Condition                                     | Behaviour                             |
|-----------------------------------------------|---------------------------------------|
| Cannot open temp file for writing             | Log failure; abandon write            |
| Any `write()` returns fewer bytes than requested | Log failure; abandon write; remove temp file |
| `close()` on temp file returns EOF/error      | Log failure; abandon write; remove temp file |
| `rename()` from temp to target fails          | Log failure; temp file remains on disk (not renamed) |

---

## 12. Compatibility notes

### Between versions

- **v2 → v3 change**: Timestamp widened from 32-bit to 64-bit to avoid Y2038
  overflow. No structural change to node entries.
- **Version stored only in header**: The version determines the timestamp
  encoding for the entire file. All other fields are identical between v2 and v3.
- **Format ID `0x02`**: This value identifies the file as a DHT routing table
  (as opposed to other binary on-disk artifacts like the session file, which
  uses different magic/format bytes).

### Between IPv4 and IPv6

- A single `dht.dat` file always corresponds to one address family (`AF_INET` or
  `AF_INET6`). There is no mixed-family file.
- The `family_` member of the serializer (passed at construction time)
  determines `clen` (6 for IPv4, 18 for IPv6) for all node entries in the file.
- On read, the deserializer's family determines the expected `clen`. If the
  family does not match the data in a node entry (the entry's compact length
  byte differs from expected), that entry is skipped.

### Field stability

All "reserved" fields are written as zeros in aria2 1.37.0. Future format
versions may repurpose these bytes. A reader must ignore reserved fields; a
writer must always fill them with zeros when writing the current (version-3)
format.

---

## 13. Reference sizes (summary table)

| Named constant       | Value | Meaning                        |
|----------------------|-------|--------------------------------|
| `DHT_ID_LENGTH`      | 20    | Node ID size in bytes (160 bits) |
| `COMPACT_LEN_IPV4`   | 6     | Compact IPv4 address + port (4+2) |
| `COMPACT_LEN_IPV6`   | 18    | Compact IPv6 address + port (16+2) |
| Per-node entry size  | 56    | Fixed regardless of address family |
| Header size          | 8     | Magic + format + reserved + version |
| Local node record    | 32    | Reserved + ID + reserved        |
