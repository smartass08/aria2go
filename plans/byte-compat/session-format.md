# aria2 Session File Format — Byte-Compatible Specification

> **Target:** aria2 1.37.0 session file byte-for-byte compatibility.
> **Scope:** This document specifies the exact file format emitted by `SessionSerializer` and consumed by `UriListParser`/`OptionParser`. All behavior described is canonical aria2 1.37.0.

---

## 1. File Format Overview

The aria2 session file is a **line-oriented UTF-8 text file**. Each download entry is a block of consecutive lines: one **URI line** followed by zero or more **option lines**. Entries are separated by newlines; blank lines and lines starting with `#` are skipped by the parser.

### 1.1 Atomic Write Pattern

When saving, aria2 writes to a temporary file named `<session-filename>__temp` (the original filename with the literal suffix `__temp` appended). Upon successful write and close, the temporary file is renamed to the target filename atomically via the OS rename system call.

```
write:  sessions/aria2.session__temp
rename: sessions/aria2.session__temp -> sessions/aria2.session
```

### 1.2 Gzip Compression

| Aspect | Behavior |
|--------|----------|
| **On write** | If the session filename ends with `.gz`, the output is gzip-compressed. Otherwise, plain text. |
| **On read** | The parser always uses a transparent gzip decompressor — it handles both compressed and uncompressed files regardless of filename. |
| **Magic bytes** | Gzip format magic: `0x1f` `0x8b`. These are the first two bytes of any `.gz` session file. |
| **Detection** | Detection is by filename suffix on write; transparent auto-detection on read (the GZip reader attempts decompression; if the data is not gzip, it falls through to plain-text). |

### 1.3 Hash-Based Skip

Before each periodic save, aria2 computes a SHA-1 hash of the serialized content. If the hash matches the hash from the previous save or from startup, the save is skipped entirely. This prevents unnecessary disk writes when nothing has changed.

---

## 2. Entry Structure

### 2.1 URI Line (First Line of Each Entry)

The first line of an entry contains zero or more URIs separated by the tab character (`\t`, byte `0x09`). No leading whitespace, no trailing tab before the newline.

```
<uri-1>\t<uri-2>\t...\t<uri-N>\n
```

**URI ordering within the line:** Already-tried ("spent") URIs are emitted first, followed by untried ("remaining") URIs. Duplicates across both sets are removed (deduplication uses the URI string as the unique key; the first occurrence wins, which means spent URIs take priority over remaining URIs).

If the download has no URIs (e.g., no remaining and no spent URIs exist), the entry is skipped entirely and not written.

For metadata-driven downloads (BitTorrent magnet links, `.torrent` file URIs, `.metalink` file URIs), the URI line is the metadata source URI (the magnet link or the `.torrent`/`.metalink` URL). The GID written in the option lines for these entries is the **metadata download's** GID, not the content download's GID.

### 2.2 Option Lines (Subsequent Lines of Each Entry)

Each option line begins with the tab character (`\t`) followed by a key, an equals sign (`=`), and the value. The line is terminated by a newline (`\n`).

```
\t<key>=<value>\n
```

**Grammar:**
- The leading `\t` is the sole indentation character.
- `<key>` is the option name (ASCII alphanumeric plus hyphens).
- `=` separates key from value (no surrounding whitespace).
- `<value>` is the option value, which may contain any byte except `\n` and `\0`.
- The line ends with a single `\n` (LF, byte `0x0a`).

On the parser side, lines beginning with either a space (` `, byte `0x20`) or a tab (`\t`, byte `0x09`) are treated as option-lines and accumulated into the option block for that entry.

---

## 3. Key Ordering — Canonical Write Sequence

The keys are emitted in a fixed, deterministic order corresponding to the internal option identifier (option ID) numbering. The option ID mapping is defined by the registration order in aria2's preference table.

### 3.1 Special First Keys

The following keys are emitted **before** the general option iteration and appear in this exact position at the top of the option block:

| Position | Key | Emitted when |
|----------|-----|-------------|
| 1 | `gid` | **Always.** The GID serialized as a 16-character lowercase hexadecimal string. |
| 2 | `pause` | Only when the download was paused (pause-requested). Value is the literal string `true`. |

After these two special-case keys, all remaining options are emitted in the canonical **Pref ID order** described below.

### 3.2 Canonical Pref ID Order (for writeOption Iteration)

The full option enumeration below lists every key in the exact order emitted by the serializer. Only keys whose option handler has `getInitialOption()` returning true are eligible for emission. Within those eligible keys, only options that are `definedLocal` (explicitly set on that download) are actually written.

**Notes on the table:**
- **"Type"** indicates the storage type of the value string.
- **"When emitted"** describes the condition required for this key to appear.
- The first two positions (`gid` and `pause`) appear in the special-first-keys block AND again at their natural Pref ID positions in this enumeration. Duplicate emission is intentional and harmless.

#### Section 1: General Preferences (IDs 1–110)

| ID | Key Name | Value Type | When Emitted |
|----|----------|-----------|-------------|
| 1 | `version` | string | Not emitted (no `getInitialOption`) |
| 2 | `help` | string | Not emitted (no `getInitialOption`) |
| 3 | `timeout` | integer (seconds, 1–600) | Always if locally defined on download |
| 4 | `dns-timeout` | integer (seconds, 1–60) | If locally defined (hidden option) |
| 5 | `connect-timeout` | integer (seconds, 1–600) | If locally defined |
| 6 | `max-tries` | integer (≥0, 0=unlimited) | If locally defined |
| 7 | `auto-save-interval` | integer (seconds, 0–600) | If locally defined (global only) |
| 8 | `log` | file path | If locally defined (global only) |
| 9 | `dir` | directory path | If locally defined on download |
| 10 | `out` | filename | If locally defined on download |
| 11 | `split` | integer (1 to unlimited) | If locally defined |
| 12 | `daemon` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 13 | `referer` | string (HTTP Referer) | If locally defined |
| 14 | `lowest-speed-limit` | integer (bytes/sec) | If locally defined |
| 15 | `piece-length` | integer (bytes, 1M–1G) | If locally defined (global only) |
| 16 | `max-overall-download-limit` | integer (bytes/sec, 0=unlimited) | If locally defined (global only) |
| 17 | `max-download-limit` | integer (bytes/sec, 0=unlimited) | If locally defined on download |
| 18 | `startup-idle-time` | integer (seconds, 1–60) | If locally defined (hidden option) |
| 19 | `file-allocation` | enum: `none` / `prealloc` / `trunc` / `falloc` | If locally defined |
| 20 | `no-file-allocation-limit` | integer (bytes) | If locally defined |
| 21 | `allow-overwrite` | bool (`true`/`false`) | If locally defined |
| 22 | `realtime-chunk-checksum` | bool (`true`/`false`) | If locally defined |
| 23 | `check-integrity` | bool (`true`/`false`) | If locally defined |
| 24 | `netrc-path` | file path | If locally defined |
| 25 | `continue` | bool (`true`/`false`) | If locally defined |
| 26 | `no-netrc` | bool (`true`/`false`) | If locally defined |
| 27 | `max-downloads` | integer | Not emitted (no `getInitialOption` on download) |
| 28 | `input-file` | file path | Not emitted (no `getInitialOption` on download) |
| 29 | `deferred-input` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 30 | `max-concurrent-downloads` | integer (≥1) | Not emitted (no `getInitialOption` on download) |
| 31 | `optimize-concurrent-downloads` | bool | Not emitted (no `getInitialOption` on download) |
| 32 | `optimize-concurrent-downloads-coeffA` | float | Not emitted (no pref registered in OptionHandlerFactory) |
| 33 | `optimize-concurrent-downloads-coeffB` | float | Not emitted (no pref registered in OptionHandlerFactory) |
| 34 | `force-sequential` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 35 | `auto-file-renaming` | bool (`true`/`false`) | If locally defined |
| 36 | `parameterized-uri` | bool (`true`/`false`) | If locally defined |
| 37 | `allow-piece-length-change` | bool (`true`/`false`) | If locally defined |
| 38 | `no-conf` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 39 | `conf-path` | file path | Not emitted (no `getInitialOption`) |
| 40 | `stop` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 41 | `quiet` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 42 | `async-dns` | bool (`true`/`false`) | If locally defined |
| 43 | `summary-interval` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 44 | `log-level` | enum: `debug`/`info`/`notice`/`warn`/`error` | Not emitted (no `getInitialOption` on download) |
| 45 | `console-log-level` | enum: `debug`/`info`/`notice`/`warn`/`error` | Not emitted (no `getInitialOption`) |
| 46 | `uri-selector` | enum: `inorder`/`feedback`/`adaptive` | If locally defined |
| 47 | `server-stat-timeout` | integer (seconds, 0–INT32_MAX) | Not emitted (no `getInitialOption` on download) |
| 48 | `server-stat-if` | file path | Not emitted (no `getInitialOption` on download) |
| 49 | `server-stat-of` | file path | Not emitted (no `getInitialOption` on download) |
| 50 | `remote-time` | bool (`true`/`false`) | If locally defined |
| 51 | `max-file-not-found` | integer (≥0) | If locally defined |
| 52 | `event-poll` | enum: `epoll`/`kqueue`/`port`/`libuv`/`poll`/`select` | Not emitted (no `getInitialOption`) |
| 53 | `enable-rpc` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 54 | `rpc-listen-port` | integer (1024–65535) | Not emitted (no `getInitialOption`) |
| 55 | `rpc-user` | string | Not emitted (erase-after-parse; deprecated) |
| 56 | `rpc-passwd` | string | Not emitted (erase-after-parse; deprecated) |
| 57 | `rpc-max-request-size` | integer (bytes) | Not emitted (no `getInitialOption`) |
| 58 | `rpc-listen-all` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 59 | `rpc-allow-origin-all` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 60 | `rpc-certificate` | file path | Not emitted (no `getInitialOption`) |
| 61 | `rpc-private-key` | file path | Not emitted (no `getInitialOption`) |
| 62 | `rpc-secure` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 63 | `rpc-save-upload-metadata` | bool (`true`/`false`) | Not emitted (no `getInitialOption` on download) |
| 64 | `dry-run` | bool (`true`/`false`) | If locally defined |
| 65 | `reuse-uri` | bool (`true`/`false`) | If locally defined |
| 66 | `on-download-start` | file path (command) | If locally defined |
| 67 | `on-download-pause` | file path (command) | If locally defined |
| 68 | `on-download-stop` | file path (command) | If locally defined |
| 69 | `on-download-complete` | file path (command) | If locally defined |
| 70 | `on-download-error` | file path (command) | If locally defined |
| 71 | `interface` | IP/hostname | Not emitted (no `getInitialOption`) |
| 72 | `multiple-interface` | IP/hostname | Not emitted (no `getInitialOption`) |
| 73 | `disable-ipv6` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 74 | `human-readable` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 75 | `remove-control-file` | bool (`true`/`false`) | If locally defined |
| 76 | `always-resume` | bool (`true`/`false`) | If locally defined |
| 77 | `max-resume-failure-tries` | integer (≥0) | If locally defined |
| 78 | `save-session` | file path | Not emitted (no `getInitialOption`) |
| 79 | `max-connection-per-server` | integer (1–16) | If locally defined |
| 80 | `min-split-size` | integer (bytes, 1M–1G) | If locally defined |
| 81 | `conditional-get` | bool (`true`/`false`) | If locally defined |
| 82 | `select-least-used-host` | bool (`true`/`false`) | If locally defined (hidden option) |
| 83 | `enable-async-dns6` | bool (`true`/`false`) | If locally defined (deprecated hidden option) |
| 84 | `max-download-result` | integer (≥0) | Not emitted (no `getInitialOption` on download) |
| 85 | `retry-wait` | integer (seconds, 0–600) | If locally defined |
| 86 | `async-dns-server` | string (IP list) | Not emitted (no `getInitialOption`) |
| 87 | `show-console-readout` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 88 | `stream-piece-selector` | enum: `default`/`inorder`/`random`/`geom` | If locally defined |
| 89 | `truncate-console-readout` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 90 | `pause` | bool (`true`/`false`) | If locally defined (re-appears here from general iteration if definedLocal) |
| 91 | `download-result` | enum: `default`/`full`/`hide` | Not emitted (no `getInitialOption` on download) |
| 92 | `hash-check-only` | bool (`true`/`false`) | If locally defined |
| 93 | `checksum` | `type=digest` string | If locally defined on download |
| 94 | `stop-with-process` | integer (PID) | Not emitted (no `getInitialOption`) |
| 95 | `enable-mmap` | bool (`true`/`false`) | If locally defined |
| 96 | `force-save` | bool (`true`/`false`) | If locally defined (triggers save of completed/removed downloads) |
| 97 | `save-not-found` | bool (`true`/`false`) | If locally defined |
| 98 | `disk-cache` | integer (bytes) | Not emitted (no `getInitialOption` on download) |
| 99 | `gid` | 16-char hex string | If locally defined (re-appears here from general iteration) |
| 100 | `save-session-interval` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 101 | `enable-color` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 102 | `rpc-secret` | string | Not emitted (erase-after-parse) |
| 103 | `dscp` | integer (0–63) | Not emitted (no `getInitialOption`) |
| 104 | `pause-metadata` | bool (`true`/`false`) | If locally defined |
| 105 | `rlimit-nofile` | integer (≥1) | Not emitted (conditional compile: only if `HAVE_SYS_RESOURCE_H`; no `getInitialOption`) |
| 106 | `min-tls-version` | enum: `TLSv1.1`/`TLSv1.2`/`TLSv1.3` | Not emitted (no `getInitialOption`) |
| 107 | `socket-recv-buffer-size` | integer (bytes, 0–16M) | Not emitted (no `getInitialOption`) |
| 108 | `max-mmap-limit` | integer (bytes) | If locally defined |
| 109 | `stderr` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 110 | `keep-unfinished-download-result` | bool (`true`/`false`) | Not emitted (no `getInitialOption` on download) |

#### Section 2: FTP Preferences (IDs 111–116)

| ID | Key Name | Value Type | When Emitted |
|----|----------|-----------|-------------|
| 111 | `ftp-user` | string | If locally defined |
| 112 | `ftp-passwd` | string | If locally defined |
| 113 | `ftp-type` | enum: `binary`/`ascii` | If locally defined |
| 114 | `ftp-pasv` | bool (`true`/`false`) | If locally defined |
| 115 | `ftp-reuse-connection` | bool (`true`/`false`) | If locally defined |
| 116 | `ssh-host-key-md` | `type=digest` string (`sha-1` or `md5`) | If locally defined |

#### Section 3: HTTP Preferences (IDs 117–135)

| ID | Key Name | Value Type | When Emitted |
|----|----------|-----------|-------------|
| 117 | `http-user` | string | If locally defined |
| 118 | `http-passwd` | string | If locally defined |
| 119 | `user-agent` | string | If locally defined |
| 120 | `load-cookies` | file path | Not emitted (no `getInitialOption` on download) |
| 121 | `save-cookies` | file path | Not emitted (no `getInitialOption` on download) |
| 122 | `enable-http-keep-alive` | bool (`true`/`false`) | If locally defined |
| 123 | `enable-http-pipelining` | bool (`true`/`false`) | If locally defined |
| 124 | `max-http-pipelining` | integer (1–8) | If locally defined (hidden option) |
| 125 | `header` | string (cumulative, newline-joined) | If locally defined; **cumulative** — each value is written on its own `\theader=` line |
| 126 | `certificate` | file path | Not emitted (no `getInitialOption` on download) |
| 127 | `private-key` | file path | Not emitted (no `getInitialOption` on download) |
| 128 | `ca-certificate` | file path | Not emitted (no `getInitialOption`) |
| 129 | `check-certificate` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 130 | `use-head` | bool (`true`/`false`) | If locally defined |
| 131 | `http-auth-challenge` | bool (`true`/`false`) | If locally defined |
| 132 | `http-no-cache` | bool (`true`/`false`) | If locally defined |
| 133 | `http-accept-gzip` | bool (`true`/`false`) | If locally defined |
| 134 | `content-disposition-default-utf8` | bool (`true`/`false`) | If locally defined |
| 135 | `no-want-digest-header` | bool (`true`/`false`) | If locally defined |

#### Section 4: Proxy Preferences (IDs 136–149)

| ID | Key Name | Value Type | When Emitted |
|----|----------|-----------|-------------|
| 136 | `http-proxy` | URL string | If locally defined |
| 137 | `https-proxy` | URL string | If locally defined |
| 138 | `ftp-proxy` | URL string | If locally defined |
| 139 | `all-proxy` | URL string | If locally defined |
| 140 | `no-proxy` | comma-separated list | Not emitted (no `getInitialOption` on download) |
| 141 | `proxy-method` | enum: `get`/`tunnel` | If locally defined |
| 142 | `http-proxy-user` | string | If locally defined |
| 143 | `http-proxy-passwd` | string | If locally defined |
| 144 | `https-proxy-user` | string | If locally defined |
| 145 | `https-proxy-passwd` | string | If locally defined |
| 146 | `ftp-proxy-user` | string | If locally defined |
| 147 | `ftp-proxy-passwd` | string | If locally defined |
| 148 | `all-proxy-user` | string | If locally defined |
| 149 | `all-proxy-passwd` | string | If locally defined |

#### Section 5: BitTorrent Preferences (IDs 150–205)

| ID | Key Name | Value Type | When Emitted |
|----|----------|-----------|-------------|
| 150 | `peer-connection-timeout` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 151 | `bt-timeout` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 152 | `bt-request-timeout` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 153 | `show-files` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 154 | `max-overall-upload-limit` | integer (bytes/sec, 0=unlimited) | Not emitted (no `getInitialOption` on download) |
| 155 | `max-upload-limit` | integer (bytes/sec, 0=unlimited) | If locally defined |
| 156 | `torrent-file` | file path | Not emitted (no `getInitialOption`) |
| 157 | `listen-port` | integer (port range, e.g. `6881-6999`) | Not emitted (no `getInitialOption` on download) |
| 158 | `follow-torrent` | enum: `true`/`false`/`mem` | Not emitted (no `getInitialOption` on download) |
| 159 | `select-file` | integer range (e.g. `1,3-5`) | If locally defined on download |
| 160 | `seed-time` | float (minutes, 0=unlimited) | If locally defined |
| 161 | `seed-ratio` | float (0.0=unlimited) | If locally defined |
| 162 | `bt-keep-alive-interval` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 163 | `peer-id-prefix` | string (≤20 bytes) | Not emitted (no `getInitialOption`) |
| 164 | `peer-agent` | string | Not emitted (no `getInitialOption`) |
| 165 | `enable-peer-exchange` | bool (`true`/`false`) | If locally defined |
| 166 | `enable-dht` | bool (`true`/`false`) | Not emitted (no `getInitialOption` on download) |
| 167 | `dht-listen-addr` | IP string | Not emitted (no `getInitialOption`) |
| 168 | `dht-listen-port` | integer | Not emitted (no `getInitialOption`) |
| 169 | `dht-entry-point-host` | hostname | Not emitted (no `getInitialOption`) |
| 170 | `dht-entry-point-port` | integer | Not emitted (no `getInitialOption`) |
| 171 | `dht-entry-point` | `host:port` string | Not emitted (no `getInitialOption`) |
| 172 | `dht-file-path` | file path | Not emitted (no `getInitialOption`) |
| 173 | `enable-dht6` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 174 | `dht-listen-addr6` | IP string | Not emitted (no `getInitialOption`) |
| 175 | `dht-entry-point-host6` | hostname | Not emitted (no `getInitialOption`) |
| 176 | `dht-entry-point-port6` | integer | Not emitted (no `getInitialOption`) |
| 177 | `dht-entry-point6` | `host:port` string | Not emitted (no `getInitialOption`) |
| 178 | `dht-file-path6` | file path | Not emitted (no `getInitialOption`) |
| 179 | `bt-min-crypto-level` | enum: `plain`/`arc4` | Not emitted (no `getInitialOption`) |
| 180 | `bt-require-crypto` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 181 | `bt-request-peer-speed-limit` | integer (bytes/sec) | Not emitted (no `getInitialOption`) |
| 182 | `bt-max-open-files` | integer | Not emitted (no `getInitialOption`) |
| 183 | `bt-seed-unverified` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 184 | `bt-hash-check-seed` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 185 | `bt-max-peers` | integer | Not emitted (no `getInitialOption` on download) |
| 186 | `bt-external-ip` | IP string | Not emitted (no `getInitialOption`) |
| 187 | `index-out` | `index=path` cumulative string | If locally defined on download; **cumulative** — each value is written on its own `\tindex-out=` line |
| 188 | `bt-tracker-interval` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 189 | `bt-stop-timeout` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 190 | `bt-prioritize-piece` | string (e.g. `head=1M,tail=1M`) | Not emitted (no `getInitialOption`) |
| 191 | `bt-save-metadata` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 192 | `bt-metadata-only` | bool (`true`/`false`) | If locally defined |
| 193 | `bt-enable-lpd` | bool (`true`/`false`) | If locally defined |
| 194 | `bt-lpd-interface` | IP string | Not emitted (no `getInitialOption`) |
| 195 | `bt-tracker-timeout` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 196 | `bt-tracker-connect-timeout` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 197 | `dht-message-timeout` | integer (seconds) | Not emitted (no `getInitialOption`) |
| 198 | `on-bt-download-complete` | file path (command) | If locally defined |
| 199 | `bt-tracker` | cumulative comma-separated URIs | If locally defined; **cumulative** — each URI is written on its own `\tbt-tracker=` line |
| 200 | `bt-exclude-tracker` | cumulative comma-separated URIs | If locally defined; **cumulative** |
| 201 | `bt-remove-unselected-file` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 202 | `bt-detach-seed-only` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 203 | `bt-force-encryption` | bool (`true`/`false`) | Not emitted (no `getInitialOption`) |
| 204 | `bt-enable-hook-after-hash-check` | bool (`true`/`false`) | If locally defined |
| 205 | `bt-load-saved-metadata` | bool (`true`/`false`) | Not emitted (no `getInitialOption` on download) |

#### Section 6: Metalink Preferences (IDs 206–214)

| ID | Key Name | Value Type | When Emitted |
|----|----------|-----------|-------------|
| 206 | `metalink-file` | file path | Not emitted (no `getInitialOption`) |
| 207 | `metalink-version` | string (version filter) | Not emitted (no `getInitialOption` on download) |
| 208 | `metalink-language` | string (language filter) | Not emitted (no `getInitialOption` on download) |
| 209 | `metalink-os` | string (OS filter) | Not emitted (no `getInitialOption` on download) |
| 210 | `metalink-location` | string (location preference) | If locally defined |
| 211 | `follow-metalink` | enum: `true`/`false`/`mem` | Not emitted (no `getInitialOption` on download) |
| 212 | `metalink-preferred-protocol` | enum: `http`/`https`/`ftp`/`none` | Not emitted (no `getInitialOption` on download) |
| 213 | `metalink-enable-unique-protocol` | bool (`true`/`false`) | Not emitted (no `getInitialOption` on download) |
| 214 | `metalink-base-uri` | URL string | If locally defined on download |

---

## 4. Cumulative Options

Some options are marked as **cumulative** — they accept multiple values. For these, each individual value is written on a separate `\t<key>=<value>` line. The values are split by newline internally.

| Key Name | How Written |
|----------|-------------|
| `header` | Each header line on a separate `\theader=<value>` line |
| `bt-tracker` | Each tracker URI on a separate `\tbt-tracker=<uri>` line |
| `bt-exclude-tracker` | Each exclusion on a separate `\tbt-exclude-tracker=<uri>` line |
| `index-out` | Each `index=path` mapping on a separate `\tindex-out=<mapping>` line |

---

## 5. Status Representation

### 5.1 Internal Error Codes (Determining What Gets Saved)

The `error_code::Value` enum is used internally to decide **whether** a download is saved to the session file. These integer values are **not written** as key-value pairs in the session file itself. They are listed here because they gate save eligibility.

| Integer | Symbol | Meaning | Session Save Behavior |
|---------|--------|---------|----------------------|
| 0 | `FINISHED` | Download completed successfully | Saved only if `force-save=true` is set on that download |
| 1 | `UNKNOWN_ERROR` | Unspecified error | Saved if `saveError=true` (default) |
| 2 | `TIME_OUT` | Connection or transfer timed out | Saved if `saveError=true` |
| 3 | `RESOURCE_NOT_FOUND` | HTTP 404 or equivalent | Saved if `saveError=true` AND `save-not-found=true` |
| 4 | `MAX_FILE_NOT_FOUND` | Reached `max-file-not-found` limit | Saved if `saveError=true` AND `save-not-found=true` |
| 5 | `TOO_SLOW_DOWNLOAD_SPEED` | Below `lowest-speed-limit` | Saved if `saveError=true` |
| 6 | `NETWORK_PROBLEM` | General network failure | Saved if `saveError=true` |
| 7 | `IN_PROGRESS` | Download is currently active | Saved if `saveInProgress=true` (default) |
| 8 | `CANNOT_RESUME` | Server does not support resume | Saved if `saveError=true` |
| 9 | `NOT_ENOUGH_DISK_SPACE` | Insufficient disk space | Saved if `saveError=true` |
| 10 | `PIECE_LENGTH_CHANGED` | BitTorrent piece length mismatch | Saved if `saveError=true` |
| 11 | `DUPLICATE_DOWNLOAD` | Same URI already queued | Saved if `saveError=true` |
| 12 | `DUPLICATE_INFO_HASH` | Same info-hash already queued | Saved if `saveError=true` |
| 13 | `FILE_ALREADY_EXISTS` | Output file exists (and overwrite disabled) | Saved if `saveError=true` |
| 14 | `FILE_RENAMING_FAILED` | Could not rename file | Saved if `saveError=true` |
| 15–18 | `FILE_OPEN_ERROR`, `FILE_CREATE_ERROR`, `FILE_IO_ERROR`, `DIR_CREATE_ERROR` | File system errors | Saved if `saveError=true` |
| 19 | `NAME_RESOLVE_ERROR` | DNS resolution failure | Saved if `saveError=true` |
| 20–27 | `METALINK_PARSE_ERROR`, `FTP_PROTOCOL_ERROR`, `HTTP_PROTOCOL_ERROR`, `HTTP_TOO_MANY_REDIRECTS`, `HTTP_AUTH_FAILED`, `BENCODE_PARSE_ERROR`, `BITTORRENT_PARSE_ERROR`, `MAGNET_PARSE_ERROR` | Protocol/format errors | Saved if `saveError=true` |
| 28 | `OPTION_ERROR` | Invalid option value | Saved if `saveError=true` |
| 29 | `HTTP_SERVICE_UNAVAILABLE` | HTTP 503 | Saved if `saveError=true` |
| 30 | `JSON_PARSE_ERROR` | RPC JSON parse failure | Saved if `saveError=true` |
| 31 | `REMOVED` | Download removed by user | Saved only if `force-save=true` |
| 32 | `CHECKSUM_ERROR` | Checksum mismatch | Saved if `saveError=true` |

### 5.2 Session-Load Status Inference

When the session file is read back, there is **no explicit status key** in the file. Status is inferred as follows:

| Session Key Present | Loaded Status |
|--------------------|---------------|
| `pause=true` | Download is loaded as **paused** |
| No `pause` key or `pause=false` | Download is loaded as **waiting** (queued) |

Active downloads (those currently in the download engine's RequestGroup list) are serialized with their current options. When re-loaded, they become active or waiting depending on concurrency limits. Completed and removed downloads are only present in the session file if `force-save=true` was set; when re-loaded, they will re-download (the `force-save` flag is for saving the queue state, not for preserving terminal state).

---

## 6. Metadata-Driven Downloads (BitTorrent / Metalink)

### 6.1 Entry Representation

For a download generated from metadata (a `.torrent` file, a magnet link, or a `.metalink` file), the session file stores the **metadata source**:

- **URI line:** The URI of the metadata source (magnet URI, `.torrent` file URL, `.metalink` file URL).
- **GID:** The GID written is the **metadata download's** GID (not the content download's GID).

For local `.torrent` or `.metalink` files, the content download's own GID is persisted directly.

### 6.2 BitTorrent-Specific Keys

These are the keys from the BitTorrent preference section (IDs 150–205) that are most relevant for session persistence:

| Key | Description |
|-----|-------------|
| `bt-tracker` | Additional tracker announce URIs (cumulative) |
| `bt-exclude-tracker` | Tracker URIs to skip (cumulative) |
| `select-file` | Which files to download from a multi-file torrent |
| `index-out` | Per-file output path overrides (cumulative) |
| `seed-time` | Seeding duration in minutes |
| `seed-ratio` | Share ratio target |
| `bt-metadata-only` | If `true`, only fetch metadata, not content |
| `bt-save-metadata` | Save `.torrent` file after metadata download |
| `bt-enable-lpd` | Enable Local Peer Discovery |
| `enable-peer-exchange` | Enable PEX extension |
| `bt-enable-hook-after-hash-check` | Run hook commands after hash check |

The `info-hash`, `piece-length`, `total-length`, and `bitfield` are **not** serialized as option keys in the session file. They are reconstructed from the metadata or control files when the download resumes.

### 6.3 Metalink-Specific Keys

| Key | Description |
|-----|-------------|
| `metalink-location` | Preferred mirror location |
| `metalink-base-uri` | Base URI for resolving relative URIs in the metalink |

### 6.4 Follow Relationships

For downloads that spawn follow-up downloads (e.g., a `.metalink` download spawns its file downloads), the relationship is tracked internally via `followedBy`/`belongsTo`/`following` on the `DownloadResult`:

- A metadata download whose `followedBy` list is non-empty **is not serialized** (its content downloads are serialized instead).
- A content download whose `belongsTo` field is non-zero **is not serialized** (it belongs to its parent).
- A metadata download with `dataOnly()` **is not serialized**.

A metainfo GID cache prevents the same GID from being written multiple times when `--force-save` causes both a metadata download and its content download to be saved.

---

## 7. Gzip Detection and Handling

### 7.1 Write Path

When saving (`SessionSerializer::save`):

```
if filename ends with ".gz":
    use GZipFile wrapper (write mode)
else:
    use plain BufferedFile (write mode)
```

### 7.2 Read Path

When loading (`UriListParser`):

```
always use GZipFile wrapper (read mode)
```

`GZipFile` transparently handles:
- **Compressed data:** Detects gzip magic (`0x1f` `0x8b`) at the start and decompresses.
- **Uncompressed data:** Falls through to plain-text reading if no gzip magic is detected.

This means a session file saved as `.gz` is always read back correctly, and a session file saved without compression can also be read back — even if its filename ends with `.gz` (though aria2 never produces such a file).

### 7.3 Magic Bytes

Gzip magic bytes at file start: `0x1f` `0x8b`.

---

## 8. Preservation Rule — Unknown Keys

**Critical for forward compatibility:** A parser of the session file format MUST preserve unknown `\t<key>=<value>` lines on a round-trip read-modify-write.

When aria2 encounters a key that it does not recognize:
- The `UriListParser` passes all `\t<key>=<value>` lines to the `OptionParser`.
- The `OptionParser` only copies options whose key is registered and whose handler has `getInitialOption()` returning true.
- Unknown or unregistered keys are silently dropped by the parser.

A conforming aria2go implementation MUST preserve the full set of key-value pairs, including unrecognized keys, so that a session file generated by a newer version of aria2 can be safely read, modified (e.g., to update the GID of a queued download), and written back without data loss.

---

## 9. Complete Example

Here is a minimal synthetic example of two entries in a session file:

```
https://example.com/file1.zip\thttps://mirror.example.com/file1.zip
\tgid=0123456789abcdef
\tdir=/home/user/downloads
\tout=file1.zip
\tsplit=5
\tmax-connection-per-server=1
\tpause=true
https://example.com/file2.iso
\tgid=fedcba9876543210
\tdir=/home/user/downloads
\tout=file2.iso
\tmax-connection-per-server=1
```

**Entry 1:**
- Two URIs (tab-separated)
- GID `0123456789abcdef`
- Directory `/home/user/downloads`
- Output filename `file1.zip`
- Split 5 connections
- Max 1 connection per server
- Paused on load

**Entry 2:**
- One URI
- GID `fedcba9876543210`
- Directory `/home/user/downloads`
- Output filename `file2.iso`
- Max 1 connection per server
- Will be loaded as waiting (no `pause=true`)

---

## 10. Validation Notes

- **Option order:** The canonical order is the Pref ID order listed in §3.2. Implementations that produce option lines in a different order may not be byte-compatible with aria2.
- **`gid` duplication:** The `gid` key appears twice in each entry — once at the top as a special-first-key, and again at its natural Pref ID position (99). Parsers must tolerate this.
- **`pause` duplication:** If the download has `pause=true` AND the option is `definedLocal`, the `pause` key appears at position 2 AND at position 90. Both values will be `true`.
- **Blank and comment lines:** Lines that are empty or start with `#` are ignored by the parser. The serializer never emits such lines.
- **No trailing whitespace:** Neither the URI line nor option lines have trailing whitespace characters. Each line ends with `\n`.
- **Character encoding:** The entire file is UTF-8.
