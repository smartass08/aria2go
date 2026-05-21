# aria2go Wire Formats — Contract Specification

> **Version:** 1.0.0  
> **Role:** Canonical grammar reference for every wire format consumed or produced by aria2go.  
> **Sources:** BEP 3, BEP 9, BEP 10 (public domain); RFC 6455 (IETF); aria2 1.37.0 byte-compat session spec.

## Table of Contents

1. [Bencode Encoding](#1-bencode-encoding)
2. [.torrent Metainfo File Format](#2-torrent-metainfo-file-format)
3. [Magnet URI Scheme](#3-magnet-uri-scheme)
4. [WebSocket Framing](#4-websocket-framing-rfc-6455)
5. [aria2.session Format](#5-aria2session-format)

---

## 1. Bencode Encoding

Bencode (pronounced "B-encode") is a binary serialization format with four data types. It is the native encoding used in `.torrent` metainfo files and in the BitTorrent tracker protocol.

### 1.1 Strings

**Grammar:** `<decimal-length>:<raw-bytes>`

A bencoded string consists of a base-10 length prefix followed by a colon, then the raw byte sequence of exactly that length. There is no null terminator and length zero is valid.

| Encoded Form | Decoded Value | Notes |
|---|---|---|
| `4:spam` | `spam` | 4 bytes |
| `0:` | `""` (empty) | Zero-length string |
| `12:hello world!` | `hello world!` | Space is a raw byte |
| `3:\xFF\xFE\xFD` | three arbitrary bytes | Non-UTF-8 is legal in bencode |

**Constraints:**
- The length prefix must be decimal digits only (`[0-9]+`).
- Leading zeros are not allowed in the length prefix except for the literal `0:`.
- Strings are binary-safe — any byte value including `0x00` is permitted.

### 1.2 Integers

**Grammar:** `i<base10-integer>e`

| Encoded Form | Decoded Value | Notes |
|---|---|---|
| `i3e` | 3 | |
| `i-42e` | -42 | Negative sign immediately after `i` |
| `i0e` | 0 | Only valid leading-zero form |
| `i99999999e` | 99999999 | No practical size limit |

**Constraints:**
- `i-0e` is **invalid** (negative zero forbidden).
- Leading zeros are **invalid** except for `i0e`. `i03e` is illegal.
- The integer must fit within whatever numeric range the implementation chooses; BEP 3 does not cap integer size at 64 bits.

### 1.3 Lists

**Grammar:** `l<bencoded-element>*e`

Elements are bencoded values of any type, concatenated without delimiters. Nested lists are allowed.

| Encoded Form | Decoded Value |
|---|---|
| `l4:spam4:eggse` | `["spam", "eggs"]` |
| `le` | `[]` (empty list) |
| `li1ei2ei3ee` | `[1, 2, 3]` |
| `ll1:a1:bel2:xyee` | `[["a", "b"], ["xy"]]` |

### 1.4 Dictionaries

**Grammar:** `d<bencoded-string><bencoded-value>...e`

A dictionary is a sequence of alternating key-value pairs enclosed by `d` and `e`. Every key must be a bencoded string. Values may be any bencoded type.

| Encoded Form | Decoded Value |
|---|---|
| `d3:cow3:moo4:spam4:eggse` | `{"cow": "moo", "spam": "eggs"}` |
| `de` | `{}` (empty dict) |
| `d4:spaml1:a1:bee` | `{"spam": ["a", "b"]}` |
| `d3:keyi42ee` | `{"key": 42}` |

**Critical constraint — key ordering:** Keys must appear in **lexicographic byte order** sorted by the raw key bytes. The sort is performed on the unencoded key strings, not on the bencoded form. For example, the keys `"cow"` and `"spam"` are ordered by comparing the bytes of `"cow"` against `"spam"` — `c` (0x63) < `s` (0x73), so `cow` comes first.

This ordering requirement is mandatory for `.torrent` files and tracker responses. A bencode parser SHOULD validate key ordering in dictionaries when parsing metainfo files.

### 1.5 Round-Trip Invariance

For any valid bencoded input `x`, the following must hold:

```
bencode(bdecode(x)) == x
```

This means the encoded output must be byte-identical to the input. In practice:

- Dictionary key order must be preserved in the original byte order found in the input (not re-sorted on output) — the decoder saves the substring, and re-encoding reproduces it exactly.
- Integer encoding must reproduce the same digit string (no re-formatting).

**Info-hash implication:** The info-hash is the SHA-1 digest of the bencoded info dictionary exactly as it appears in the `.torrent` file. If a decoder re-encodes the info dict (e.g. re-sorting keys), the hash will change. Implementations must either:
- Extract the raw byte substring corresponding to the info dictionary from the `.torrent` file and hash that, OR
- Fully validate key ordering during decode so that a re-encode is guaranteed to produce identical bytes.

---

## 2. .torrent Metainfo File Format

A `.torrent` file (metainfo file) is a bencoded dictionary at the top level. The standard MIME type is `application/x-bittorrent`.

### 2.1 Top-Level Dictionary Keys

#### Required Keys

| Key | Type | Description |
|---|---|---|
| `announce` | string | The URL of the tracker, UTF-8 encoded. E.g. `http://tracker.example.com:6969/announce` |
| `info` | dict | The info dictionary describing the file(s). See §2.2. |

#### Optional Keys

| Key | Type | Description |
|---|---|---|
| `announce-list` | list of lists of strings | Multi-tracker extension (BEP 12). Each inner list is a tier of tracker URLs. |
| `creation date` | integer | UNIX timestamp (seconds since epoch) when the torrent was created. |
| `comment` | string | Free-form comment, UTF-8 encoded. |
| `created by` | string | Name and version of the software that created the torrent, UTF-8 encoded. |
| `encoding` | string | Encoding hint for the file system. UTF-8 encoded string (e.g. `UTF-8`, `CP1252`). |

**All text-bearing strings in a `.torrent` file must be valid UTF-8.**

### 2.2 The Info Dictionary

The `info` dictionary is the core of the torrent — it describes the content being distributed. The SHA-1 hash of its bencoded form is the torrent's **info-hash**.

#### Required Keys (Always Present)

| Key | Type | Description |
|---|---|---|
| `name` | string (UTF-8) | Suggested filename (single-file mode) or directory name (multi-file mode). Purely advisory. |
| `piece length` | integer | Number of bytes per piece. Typically a power of two — common values are 256 KiB (262144), 512 KiB (524288), 1 MiB (1048576). |
| `pieces` | string (binary) | Concatenated 20-byte SHA-1 hashes, one per piece. Total byte length is a multiple of 20. The number of pieces is `len(pieces) / 20`. |

#### File Description — Exactly One of the Following

**Single-file mode:**

| Key | Type | Description |
|---|---|---|
| `length` | integer | Total size of the file in bytes. |

**Multi-file mode:**

| Key | Type | Description |
|---|---|---|
| `files` | list of dicts | Ordered list of file descriptors. Each entry is a dict with: |

Each file dictionary contains:

| Key | Type | Description |
|---|---|---|
| `length` | integer | Size of the file in bytes. |
| `path` | list of strings (UTF-8) | Path components. The last element is the filename; preceding elements are subdirectory names. Must contain at least one element (zero-length list is an error). |

In multi-file mode, the logical file for piece hashing is the **concatenation of all files in the order they appear in the `files` list**.

#### Optional Keys

| Key | Type | Description |
|---|---|---|
| `md5sum` | string | Per-file MD5 checksum. A 32-character lowercase hex string. Deprecated in modern usage; SHA-1 piece hashes are the primary integrity check. Only valid as a `files` entry key. |
| `private` | integer (1 or 0) | Private flag (BEP 27). If `1`, the torrent is private and clients should only use the trackers listed in `announce`/`announce-list` (no DHT, no PEX). |
| `source` | string | Optional source identifier (BEP 27). |

### 2.3 Info-Hash Computation

The **info-hash** is the 20-byte SHA-1 digest of the bencoded representation of the `info` dictionary, exactly as it appears in the `.torrent` file:

```
info_hash = SHA1(bencode(info_dict_as_found_in_torrent_file))
```

This is the value used in:
- Tracker announce URLs (hex-encoded, URL-escaped)
- The BitTorrent peer handshake (raw 20 bytes)
- Magnet links (hex or base32 encoded)
- DHT lookups

**Round-trip caution:** If a bencode decoder reconstructs the info dictionary by decoding and re-encoding, the key ordering may change, altering the hash. Implementations must either extract the raw byte substring from the original `.torrent` file or ensure their bencode implementation produces byte-identical re-encodings.

### 2.4 Example Structures

**Single-file torrent (minimal):**

```
d
  8:announce 30:http://tracker.example.com/announce
  4:info
    d
      4:name 9:README.md
      12:piece length i262144e
      6:pieces 20:<sha1-hash-of-piece-0>
      6:length i4096e
    e
e
```

**Multi-file torrent:**

```
d
  8:announce 30:http://tracker.example.com/announce
  4:info
    d
      4:name 9:my_project
      12:piece length i262144e
      6:pieces 40:<sha1-piece0><sha1-piece1>
      5:files
        l
          d
            6:length i1024e
            4:path l10:README.mde
          e
          d
            6:length i2048e
            4:path l3:src6:main.goe
          e
        e
    e
e
```

---

## 3. Magnet URI Scheme

The magnet URI scheme enables BitTorrent downloads without a `.torrent` file by encoding the info-hash and optional metadata directly in a URI. BEP 9 defines the format; BEP 9's metadata extension protocol allows downloading the info dictionary from peers.

### 3.1 URI Structure

```
magnet:?<parameter>=<value>&<parameter>=<value>...
```

### 3.2 Required Parameter

| Parameter | Format | Description |
|---|---|---|
| `xt` | `urn:btih:<info-hash>` (v1) or `urn:btmh:<tagged-info-hash>` (v2) | Exact Topic — identifies the content. **This is the only mandatory parameter.** |

Both v1 and v2 `xt` values may coexist in a single magnet URI for hybrid torrents.

### 3.3 Info-Hash Encoding in `xt`

| Encoding | Length | Characters | Example |
|---|---|---|---|
| **Hex** (v1 primary) | 40 chars | `[0-9a-fA-F]` | `magnet:?xt=urn:btih:a12b3c4d5e6f7890...` |
| **Base32** (v1 legacy) | 32 chars | RFC 3548 Base32 alphabet | `magnet:?xt=urn:btih:A2B3C4D5...` |
| **Multihash hex** (v2) | variable, hex-encoded | multihash + hex digits | `magnet:?xt=urn:btmh:1220<64-hex-chars>` |

Clients MUST support hex encoding (40 chars). Clients SHOULD also support base32 encoding (32 chars) for compatibility with existing magnet links.

### 3.4 Optional Parameters

| Parameter | Value Format | Description |
|---|---|---|
| `dn` (display name) | Percent-encoded string | Suggested filename displayed while metadata is being fetched. |
| `xl` (exact length) | Integer (decimal) | Exact size of the download in bytes. |
| `tr` (tracker URL) | Percent-encoded URL | Tracker announce URL. May appear **multiple times** for multi-tracker torrents. |
| `xs` (exact source) | Percent-encoded URL | URL to a `.torrent` file or other source that provides the exact same content. |
| `as` (acceptable source) | Percent-encoded URL | URL to a source that may provide similar content (weaker guarantee than `xs`). |
| `x.pe` (peer address) | `host:port` or `[ipv6]:port` | Peer address for direct metadata transfer. May appear **multiple times**. |
| `so` (select only) | Comma-separated file indices | Only download specified files (0-indexed). |
| `kt` (keyword topic) | String | Search-based topic (less precise than `xt`). |

### 3.5 Multi-Tracker and Multi-Peer Forms

Multiple trackers:
```
magnet:?xt=urn:btih:HASH&tr=http://tracker1.example.com/announce&tr=udp://tracker2.example.com:6881/announce
```

Multiple peer addresses:
```
x.pe=192.168.1.1:6881&x.pe=[2001:db8::1]:6881
```

### 3.6 URL Encoding Rules

- Parameter names are case-sensitive lowercase.
- Parameter values containing reserved URI characters (`=`, `&`, `#`, space, non-ASCII) must be percent-encoded.
- The info-hash in `xt` uses **UPPERCASE** hex by convention, but parsers should accept case-insensitive hex.
- Base32 info-hashes should use uppercase per RFC 3548.

---

## 4. WebSocket Framing (RFC 6455)

aria2 provides a WebSocket endpoint for RPC. This section defines the frame structure and rules all aria2go WebSocket implementations must follow.

### 4.1 Frame Structure

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-------+-+-------------+-------------------------------+
|F|R|R|R| opcode|M| Payload len |    Extended payload length    |
|I|S|S|S|  (4)  |A|     (7)     |             (16/64)           |
|N|V|V|V|       |S|             |   (if payload len==126/127)   |
| |1|2|3|       |K|             |                               |
+-+-+-+-+-------+-+-------------+ - - - - - - - - - - - - - - - +
|     Extended payload length continued, if payload len == 127  |
+ - - - - - - - - - - - - - - - +-------------------------------+
|                               |Masking-key, if MASK set to 1  |
+-------------------------------+-------------------------------+
|    Masking-key (continued)    |          Payload Data         |
+-------------------------------- - - - - - - - - - - - - - - - +
:                    Payload Data (masked if M=1)               :
+ - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - +
```

### 4.2 Field Definitions

| Field | Bits | Description |
|---|---|---|
| **FIN** | 1 | `1` = final fragment in a message, `0` = more fragments follow. |
| **RSV1/2/3** | 3 | Reserved bits. Must be `0` unless a negotiated extension defines them. |
| **Opcode** | 4 | Frame type. See §4.3. |
| **MASK** | 1 | `1` = payload is masked (see §4.5). |
| **Payload Length** | 7, 7+16, or 7+64 | Length of the payload data. See §4.4. |
| **Masking Key** | 0 or 32 | Present only when MASK=1. 4 bytes of random masking key. |
| **Payload Data** | N bytes | May be masked. Length determined by Payload Length field. |

### 4.3 Opcodes

| Value | Mnemonic | Description |
|---|---|---|
| `0x0` | Continuation | Continuation frame of a fragmented message. |
| `0x1` | Text | Payload is UTF-8 text. |
| `0x2` | Binary | Payload is arbitrary binary data. |
| `0x3` – `0x7` | — | Reserved for further non-control frames. |
| `0x8` | Close | Connection close frame. See §4.6. |
| `0x9` | Ping | A keep-alive or latency check. See §4.7. |
| `0xA` | Pong | Response to a Ping. See §4.7. |
| `0xB` – `0xF` | — | Reserved for further control frames. |

### 4.4 Payload Length Encoding

| Payload Length (7-bit) | Meaning |
|---|---|
| `0` – `125` | Actual payload length in bytes. |
| `126` | The next 2 bytes (16-bit unsigned big-endian) hold the true payload length. |
| `127` | The next 8 bytes (64-bit unsigned big-endian) hold the true payload length. The most significant bit MUST be `0`. |

### 4.5 Masking Rules

| Direction | MASK Bit | Behavior |
|---|---|---|
| **Client → Server** | **MUST be 1** | Payload XOR'd with 4-byte masking key. Server MUST close connection (status 1002) if unmasked frame received. |
| **Server → Client** | **MUST be 0** | Payload is unmasked. Client SHOULD close connection (status 1002) if masked frame received. |

**Masking algorithm** (applied to each byte `i` of payload):
```
masked_byte[i] = payload_byte[i] XOR masking_key[i % 4]
```

### 4.6 Close Frame

A close frame has opcode `0x8`. It may contain a payload:

| Bytes | Field | Description |
|---|---|---|
| 0–1 | Status Code | 2-byte unsigned integer (big-endian). See below. |
| 2+ | Reason | Optional UTF-8 text describing why the connection is closed. |

**Rules:**
- Control frames (Close, Ping, Pong) MUST have payload length ≤ 125 bytes.
- Control frames MUST NOT be fragmented.
- When sending an unsolicited close, use status `1000` (normal closure).
- When responding to an abnormal close caused by a protocol error, use status `1002` (protocol error).
- If a received close frame contains no status code payload, treat it as `1005` (no status received).

**Common close status codes:**

| Code | Name | Meaning |
|---|---|---|
| `1000` | Normal Closure | Clean shutdown. |
| `1001` | Going Away | Endpoint is going away (e.g. server shutdown). |
| `1002` | Protocol Error | Received frame violates the protocol. |
| `1003` | Unsupported Data | Received a frame type the endpoint cannot process. |
| `1005` | No Status Rcvd | Sent by implementation when no close code was provided. |
| `1006` | Abnormal Closure | Connection closed without a proper close frame. |
| `1007` | Invalid Payload | Text frame payload is not valid UTF-8. |
| `1008` | Policy Violation | Connection violated an application policy. |
| `1009` | Message Too Big | Message exceeded the maximum allowed size. |
| `1011` | Internal Error | Unexpected server-side error. |

### 4.7 Ping/Pong

- **Ping** (opcode `0x9`): Sent to check the connection is alive or measure latency.
- **Pong** (opcode `0xA`): **MUST be sent** in response to a Ping frame, with the **same payload** as the Ping.
- Ping and Pong frames may be sent at any time after the opening handshake.
- A Pong unsolicited by a Ping (unidirectional heartbeat) is valid.
- Ping payload (if any) is application data; max 125 bytes (control frame limit).

### 4.8 Fragmentation

- Unfragmented message: single frame with FIN=1 and opcode ≠ `0x0`.
- Fragmented message: first frame with FIN=0 and an opcode of `0x1` or `0x2`, followed by zero or more continuation frames (opcode `0x0`, FIN=0), terminated by a final continuation frame (opcode `0x0`, FIN=1).
- Control frames (Close, Ping, Pong) MUST NOT be fragmented.

---

## 5. aria2.session Format

> **Full byte-compatible specification:** `plans/byte-compat/session-format.md`  
> **This section is a summary only.** The byte-compat spec is authoritative.

### 5.1 Overview

The aria2 session file persists download state across restarts. It is a **line-oriented UTF-8 text file** with one download entry per block of consecutive lines: a URI line followed by option lines.

### 5.2 URI Line

```
<uri-1>\t<uri-2>\t...\t<uri-N>\n
```

Tab-separated URIs with no leading whitespace. Spent (already-tried) URIs appear first, followed by remaining URIs. Download entries with zero URIs are skipped.

### 5.3 Option Lines

```
\t<key>=<value>\n
```

Each option line starts with a tab character (`0x09`), followed by key, equals sign, and value, terminated by a newline (`0x0A`). Parsers accept either tab (`0x09`) or space (`0x20`) as the leading indentation.

### 5.4 Gzip Compression

| Aspect | Behavior |
|---|---|
| **Magic bytes** | `0x1F 0x8B` (standard gzip) |
| **On write** | If filename ends with `.gz`, output is gzip-compressed. |
| **On read** | Transparent decompression attempted; falls through to plain-text automatically. |

### 5.5 Atomic Write Pattern

```
1. Write to:    <session-filename>__temp
2. Close temp file.
3. rename() to: <session-filename>
```

The suffix `__temp` is literal (two underscores followed by `temp`). On POSIX, `rename` is atomic.

### 5.6 Key Canonical Order

The first two option lines are always:
```
\tgid=<16-char-lowercase-hex>\n
\tpause=true\n    (if paused)
```

Remaining options are emitted in canonical Pref ID order. Only options that are locally defined (explicitly set on the download) and have `getInitialOption()` returning true are written.

### 5.7 Hash-Based Skip

Before saving, aria2 computes SHA-1 of the serialized content. If it matches the hash from the previous save, the entire write is skipped to avoid unnecessary I/O.

---

## Appendix A: Bencode ABNF Grammar (Informative)

```
bencode     = string / integer / list / dict

string      = positive-integer ":" *OCTET
positive-integer = "0" / (%x31-39 *DIGIT)   ; no leading zeros except "0"

integer     = "i" ( "0" / [ "-" ] positive-integer ) "e"
              ; i-0e is invalid; i03e is invalid

list        = "l" *bencode "e"

dict        = "d" *( string bencode ) "e"
              ; keys MUST appear in lexicographic byte order

DIGIT       = %x30-39
OCTET       = %x00-FF
```

## Appendix B: Info-Hash Formats Reference

| Context | Format | Length | Example |
|---|---|---|---|
| Tracker announce URL | Hex, URL-escaped | 40 chars (+ `%` for each hex pair if URL-encoding) | `info_hash=%A1%B2%...` |
| Peer handshake | Raw bytes | 20 bytes | `<binary>` |
| Magnet `xt=urn:btih:` | Hex (RFC preferred) | 40 chars | `A1B2C3D4...` |
| Magnet `xt=urn:btih:` | Base32 (legacy) | 32 chars | `A2B3C4...` |
| DHT `target` | Raw bytes | 20 bytes | `<binary>` |

---

*End of wire-formats.md*
