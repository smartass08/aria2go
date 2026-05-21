# aria2c CLI Flags — Byte-Compatible Specification

> **Target:** aria2 1.37.0 CLI byte-for-byte compatibility.
> **Scope:** Every flag accepted by `aria2c`, including value syntax, short forms, categories, defaults, and behavioral rules.
> **Derived from:** `plans/contracts/config-keys.md` (T016) and `aria2c.rst` manual (clean-room reading).

---

## 1. CLI Conventions

### 1.1 Flag Syntax

```
aria2c [OPTIONS] [URI|MAGNET|TORRENT_FILE|METALINK_FILE] ...
```

All long-form options use the `--key=VALUE` syntax. The `=` delimiter is required for long-form values. Short-form options use `-X VALUE` or `-XVALUE` (concatenated, no space). For short-form booleans with an argument, concatenation is required: `-Vfalse`, `-ctrue`.

### 1.2 Boolean Flags

Boolean flags have **optional arguments** indicated by `[true|false]` in `--help` output. The convention:

| Form | Meaning |
|------|---------|
| `--flag` (no argument) | Equivalent to `--flag=true` |
| `--flag=true` | Enable |
| `--flag=false` | Disable |
| `-X` (short form, no argument) | Equivalent to `-Xtrue` |
| `-Xfalse` (concatenated) | Disable |

There is **no** `--no-flag` negation style in aria2. The flag `--enable-color` is the canonical form; `--no-color` does not exist (disable via `--enable-color=false`).

### 1.3 Units

| Suffix | Meaning | Case-Insensitive? |
|--------|---------|-------------------|
| `K` / `k` | ×1024 | Yes |
| `M` / `m` | ×1048576 | Yes |

Unit suffixes apply to `Size`, `Speed`, and select integer options (documented per-flag). For `--piece-length`, `--min-split-size`, `--disk-cache`, etc., suffixes `K` and `M` are accepted.

### 1.4 Value Grammar Summary

| Grammar | Format | Examples |
|---------|--------|----------|
| `STRING` | Unicode text; `""` for empty; `*` for wildcard on some flags | `"file.zip"`, `""`, `"*"` |
| `INTEGER` | Non-negative decimal | `5`, `60`, `0` |
| `FLOAT` | Decimal float (seed-ratio, seed-time) | `1.0`, `0.5`, `10.25` |
| `SIZE` | Int + optional K/M suffix | `16M`, `1024K`, `0` |
| `SPEED` | Same as SIZE, used for byte/sec limits | `50K`, `1M`, `0` |
| `DURATION` | Integer seconds | `60`, `86400`, `10` |
| `ENUM` | One of a fixed string set, case-sensitive | `debug`, `prealloc`, `inorder` |
| `PROXY` | `[http://][USER:PASSWORD@]HOST[:PORT]`; `""` clears | `http://user:pass@host:8080` |
| `CHECKSUM` | `HASH_TYPE=HEX_DIGEST` | `sha-1=0192ba11326f...` |
| `HOSTPORT` | `HOST:PORT` | `router.bittorrent.com:6881` |
| `RANGE` | Comma-separated ints/ranges | `6881-6999`, `1-5,8,9` |
| `SPECIAL` | Flag-specific syntax | `head[=1M],tail[=2M]`, `true\|A:B` |
| `FLAG` | No argument | `--version`, `--help` |

### 1.5 Optional Argument Convention

Options documented as taking `[true|false]` have optional arguments. If the argument is omitted, it defaults to `true`. This is signalled in `--help` output by the value being in square brackets (e.g., `--daemon[=true|false]`). Note: in the reference implementation, the square brackets in `--help` output indicate optional argument; the user must NOT type the brackets on the command line.

### 1.6 $HOME Expansion

The following flag values expand `$HOME` (and `~` where the shell passes it through) to the user's home directory, even when specified on the CLI:

`--ca-certificate`, `--certificate`, `--dht-file-path`, `--dht-file-path6`, `--dir`, `--input-file`, `--load-cookies`, `--log`, `--metalink-file`, `--netrc-path`, `--on-bt-download-complete`, `--on-download-complete`, `--on-download-error`, `--on-download-start`, `--on-download-stop`, `--on-download-pause`, `--out`, `--private-key`, `--rpc-certificate`, `--rpc-private-key`, `--save-cookies`, `--save-session`, `--server-stat-if`, `--server-stat-of`, `--torrent-file`

### 1.7 $VERSION Substitution

Two flags perform `$VERSION` substitution at build time:

- `--user-agent`: Default `aria2/$VERSION` — `$VERSION` replaced by package version string (e.g., `1.37.0`).
- `--peer-agent`: Default `aria2/$MAJOR.$MINOR.$PATCH` — each component replaced (e.g., `aria2/1.37.0`).
- `--peer-id-prefix`: Default `A2-$MAJOR-$MINOR-$PATCH-` — each component replaced (e.g., `A2-1-37-0-`).

---

## 2. Special/Operational Flags

These flags govern CLI operation; they are not configuration options and do not appear in aria2.conf.

| # | Flag | Short | Value Syntax | Default | Category | Description |
|---|------|-------|-------------|---------|----------|-------------|
| 1 | `--help` | `-h` | `--help[=TAG\|KEYWORD]` | `#basic` | Basic | Print usage. TAG starts with `#`. Available tags: `#basic`, `#advanced`, `#http`, `#https`, `#ftp`, `#metalink`, `#bittorrent`, `#cookie`, `#hook`, `#file`, `#rpc`, `#checksum`, `#experimental`, `#deprecated`, `#help`, `#all`. Non-tag KEYWORD matches option names containing that word. |
| 2 | `--version` | `-v` | (no argument) | — | Basic | Print version number, copyright, configuration info, supported hash algorithms, and exit. Takes no value. |

---

## 3. Category: Basic Options

All table columns:

- **Flag**: Long (`--name`) and short (`-X`) forms
- **Syntax**: Command-line invocation pattern
- **Type/Grammar**: Value type and format
- **Default**: Behavior when omitted
- **Category**: `--help` tag
- **Accum**: Can appear multiple times?
- **Bool Style**: For booleans, the style (`--flag[=true\|false]` with optional arg, or `--flag=VALUE` with required arg)

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--dir` | `-d` | `--dir=DIR` | String (path) | `.` (cwd) | `#basic` `#file` | No | — | Directory to store downloaded files. |
| 2 | `--input-file` | `-i` | `--input-file=FILE` | String (path; `-` for stdin) | — | `#basic` `#file` | No | — | Download URIs from FILE. Gzip compressed input supported transparently. |
| 3 | `--log` | `-l` | `--log=LOG` | String (path; `-` for stdout; `""` to disable) | `""` | `#basic` `#file` | No | — | Log output file. |
| 4 | `--max-concurrent-downloads` | `-j` | `--max-concurrent-downloads=N` | Integer | `5` | `#basic` | No | — | Parallel download items. |
| 5 | `--check-integrity` | `-V` | `--check-integrity[=true\|false]` | Boolean | `false` | `#basic` `#checksum` | No | Optional Arg | Validate piece hashes or whole-file hash. Effective for BT, Metalink with checksums, HTTP/FTP with `--checksum`. |
| 6 | `--continue` | `-c` | `--continue[=true\|false]` | Boolean | `false` | `#basic` | No | Optional Arg | Resume partially downloaded file (HTTP/FTP). |
| 7 | `--log-level` | | `--log-level=LEVEL` | Enum: `debug`, `info`, `notice`, `warn`, `error` | `debug` | `#basic` | No | — | Log level for file output. |
| 8 | `--console-log-level` | | `--console-log-level=LEVEL` | Enum: `debug`, `info`, `notice`, `warn`, `error` | `notice` | `#basic` | No | — | Log level for console output. |
| 9 | `--daemon` | `-D` | `--daemon[=true\|false]` | Boolean | `false` | `#basic` | No | Optional Arg | Run as daemon. Changes cwd to `/`, redirects stdio to `/dev/null`. |
| 10 | `--split` | `-s` | `--split=N` | Integer | `5` | `#basic` | No | — | Connections per download item. |
| 11 | `--max-connection-per-server` | `-x` | `--max-connection-per-server=NUM` | Integer | `1` | `#basic` | No | — | Max connections to one server per download. |
| 12 | `--min-split-size` | `-k` | `--min-split-size=SIZE` | Size (1M–1024M) | `20M` | `#basic` | No | — | Minimum segment size for splitting. File splits only if ≥2×SIZE fits. |
| 13 | `--max-overall-download-limit` | | `--max-overall-download-limit=SPEED` | Speed; `0` = unlimited | `0` | `#basic` | No | — | Global max download speed (bytes/sec). |
| 14 | `--max-download-limit` | | `--max-download-limit=SPEED` | Speed; `0` = unlimited | `0` | `#basic` | No | — | Max download speed per download (bytes/sec). |
| 15 | `--max-overall-upload-limit` | | `--max-overall-upload-limit=SPEED` | Speed; `0` = unlimited | `0` | `#basic` | No | — | Global max upload speed (bytes/sec). |
| 16 | `--max-upload-limit` | `-u` | `--max-upload-limit=SPEED` | Speed; `0` = unlimited | `0` | `#basic` | No | — | Max upload speed per torrent (bytes/sec). |
| 17 | `--out` | `-o` | `--out=FILE` | String (filename relative to `--dir`) | — | `#basic` `#file` | No | — | Output file name. Ignored with `--force-sequential`. Not applicable to BT/Metalink. |
| 18 | `--quiet` | `-q` | `--quiet[=true\|false]` | Boolean | `false` | `#basic` | No | Optional Arg | Suppress all console output. |
| 19 | `--show-console-readout` | | `--show-console-readout[=true\|false]` | Boolean | `true` | `#basic` | No | Optional Arg | Show download progress readout on console. |
| 20 | `--truncate-console-readout` | | `--truncate-console-readout[=true\|false]` | Boolean | `true` | `#basic` | No | Optional Arg | Truncate console readout to fit a single line. |
| 21 | `--human-readable` | | `--human-readable[=true\|false]` | Boolean | `true` | `#basic` | No | Optional Arg | Print sizes as 1.2Ki, 3.4Mi. |
| 22 | `--summary-interval` | | `--summary-interval=SEC` | Duration; `0` suppresses | `60` | `#basic` | No | — | Download progress summary interval. |
| 23 | `--download-result` | | `--download-result=OPT` | Enum: `default`, `full`, `hide` | `default` | `#basic` | No | — | Format of download result summary. |
| 24 | `--optimize-concurrent-downloads` | | `--optimize-concurrent-downloads[=true\|false\|A:B]` | Special: bool or `A:B` coeffs | `false` | `#basic` | No | Optional Arg | Adapt concurrent downloads to bandwidth. N = A + B·log₁₀(Mbps). Default coeffs: A=5, B=25. |
| 25 | `--force-sequential` | `-Z` | `--force-sequential[=true\|false]` | Boolean | `false` | `#basic` | No | Optional Arg | Fetch each URI sequentially, each in separate session. |
| 26 | `--stderr` | | `--stderr[=true\|false]` | Boolean | `false` | `#basic` | No | Optional Arg | Redirect console output to stderr. |

---

## 4. Category: HTTP Options

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--http-user` | | `--http-user=USER` | String | — | `#http` | No | — | HTTP username for all URIs. |
| 2 | `--http-passwd` | | `--http-passwd=PASSWD` | String | — | `#http` | No | — | HTTP password for all URIs. |
| 3 | `--user-agent` | `-U` | `--user-agent=USER_AGENT` | String | `aria2/$VERSION` | `#http` | No | — | User-Agent header. `$VERSION` is substituted at build time. |
| 4 | `--referer` | | `--referer=REFERER` | String; `*` = use download URI | — | `#http` | No | — | Referer header for HTTP(S). |
| 5 | `--enable-http-keep-alive` | | `--enable-http-keep-alive[=true\|false]` | Boolean | `true` | `#http` | No | Optional Arg | Enable HTTP/1.1 persistent connections. |
| 6 | `--enable-http-pipelining` | | `--enable-http-pipelining[=true\|false]` | Boolean | `false` | `#http` | No | Optional Arg | Enable HTTP/1.1 pipelining. |
| 7 | `--http-accept-gzip` | | `--http-accept-gzip[=true\|false]` | Boolean | `false` | `#http` | No | Optional Arg | Send `Accept-Encoding: deflate, gzip` and inflate responses. |
| 8 | `--http-auth-challenge` | | `--http-auth-challenge[=true\|false]` | Boolean | `false` | `#http` | No | Optional Arg | Send Authorization only when challenged (401). If false, always send. |
| 9 | `--http-no-cache` | | `--http-no-cache[=true\|false]` | Boolean | `false` | `#http` | No | Optional Arg | Send `Cache-Control: no-cache` and `Pragma: no-cache`. |
| 10 | `--no-want-digest-header` | | `--no-want-digest-header[=true\|false]` | Boolean | `false` | `#http` | No | Optional Arg | Omit `Want-Digest` header. |
| 11 | `--use-head` | | `--use-head[=true\|false]` | Boolean | `false` | `#http` | No | Optional Arg | Use HEAD method for first HTTP request. |
| 12 | `--header` | | `--header=HEADER` | String: `"NAME: VALUE"` | — | `#http` | **Yes** | — | Append custom HTTP header. Repeatable. Each call appends. |
| 13 | `--load-cookies` | | `--load-cookies=FILE` | String (path) | — | `#http` `#cookie` `#file` | No | — | Load cookies from Firefox3/Chrome SQLite or Netscape format. Requires sqlite3 for Firefox3/Chrome. |
| 14 | `--save-cookies` | | `--save-cookies=FILE` | String (path) | — | `#http` `#cookie` `#file` | No | — | Save cookies to Netscape format. Overwrites existing. Session cookies saved with expiry=0. |
| 15 | `--ca-certificate` | | `--ca-certificate=FILE` | String (path to PEM) | — | `#http` | No | — | CA certificate for peer verification. |
| 16 | `--certificate` | | `--certificate=FILE` | String (path to PKCS12/.p12/.pfx or PEM; on Apple TLS, SHA-1 fingerprint) | — | `#http` | No | — | Client certificate. PKCS12 files must have blank import password. |
| 17 | `--check-certificate` | | `--check-certificate[=true\|false]` | Boolean | `true` | `#http` | No | Optional Arg | Verify peer certificate against `--ca-certificate`. |
| 18 | `--private-key` | | `--private-key=FILE` | String (path to decrypted PEM) | — | `#http` | No | — | Private key used with `--certificate` in PEM mode. |
| 19 | `--http-proxy` | | `--http-proxy=PROXY` | Proxy URI; `""` clears | — | `#http` | No | — | HTTP proxy. Format: `[http://][USER:PASSWORD@]HOST[:PORT]`. |
| 20 | `--http-proxy-user` | | `--http-proxy-user=USER` | String | — | `#http` | No | — | Username for `--http-proxy`. |
| 21 | `--http-proxy-passwd` | | `--http-proxy-passwd=PASSWD` | String | — | `#http` | No | — | Password for `--http-proxy`. |
| 22 | `--https-proxy` | | `--https-proxy=PROXY` | Proxy URI; `""` clears | — | `#https` | No | — | HTTPS proxy. |
| 23 | `--https-proxy-user` | | `--https-proxy-user=USER` | String | — | `#https` | No | — | Username for `--https-proxy`. |
| 24 | `--https-proxy-passwd` | | `--https-proxy-passwd=PASSWD` | String | — | `#https` | No | — | Password for `--https-proxy`. |

---

## 5. Category: FTP/SFTP Options

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--ftp-user` | | `--ftp-user=USER` | String | `anonymous` | `#ftp` | No | — | FTP username for all URIs. |
| 2 | `--ftp-passwd` | | `--ftp-passwd=PASSWD` | String | `ARIA2USER@` | `#ftp` | No | — | FTP password. If username in URI but no password, falls back to .netrc then this value. |
| 3 | `--ftp-pasv` | `-p` | `--ftp-pasv[=true\|false]` | Boolean | `true` | `#ftp` | No | Optional Arg | Use passive FTP mode. Ignored for SFTP. |
| 4 | `--ftp-type` | | `--ftp-type=TYPE` | Enum: `binary`, `ascii` | `binary` | `#ftp` | No | — | FTP transfer type. Ignored for SFTP. |
| 5 | `--ftp-reuse-connection` | | `--ftp-reuse-connection[=true\|false]` | Boolean | `true` | `#ftp` | No | Optional Arg | Reuse FTP control connection. |
| 6 | `--ftp-proxy` | | `--ftp-proxy=PROXY` | Proxy URI; `""` clears | — | `#ftp` | No | — | FTP proxy. |
| 7 | `--ftp-proxy-user` | | `--ftp-proxy-user=USER` | String | — | `#ftp` | No | — | Username for `--ftp-proxy`. |
| 8 | `--ftp-proxy-passwd` | | `--ftp-proxy-passwd=PASSWD` | String | — | `#ftp` | No | — | Password for `--ftp-proxy`. |
| 9 | `--ssh-host-key-md` | | `--ssh-host-key-md=TYPE=DIGEST` | Checksum: `sha-1` or `md5` | — | `#ftp` | No | — | Expected SSH host public key checksum for SFTP. |
| 10 | `--netrc-path` | | `--netrc-path=FILE` | String (path; must have mode 600) | `$(HOME)/.netrc` | `#ftp` | No | — | Path to netrc file for auto-login. |
| 11 | `--no-netrc` | `-n` | `--no-netrc[=true\|false]` | Boolean | `false` | `#ftp` | No | Optional Arg | Disable netrc support. If true at startup, cannot be re-enabled mid-session. |

---

## 6. Category: HTTP/FTP/SFTP Shared Options

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--connect-timeout` | | `--connect-timeout=SEC` | Duration | `60` | `#http` `#ftp` | No | — | Connection timeout to HTTP/FTP/proxy server. |
| 2 | `--timeout` | `-t` | `--timeout=SEC` | Duration | `60` | `#http` `#ftp` | No | — | Read timeout after connection established. |
| 3 | `--max-tries` | `-m` | `--max-tries=N` | Integer; `0` = unlimited | `5` | `#http` `#ftp` | No | — | Max retry attempts per download. |
| 4 | `--retry-wait` | | `--retry-wait=SEC` | Duration; `0` = retry only on 503 | `0` | `#http` `#ftp` | No | — | Seconds between retries. When >0, retries for any failure. |
| 5 | `--max-file-not-found` | | `--max-file-not-found=NUM` | Integer; `0` = disabled | `0` | `#http` `#ftp` | No | — | Abort after NUM "file not found" responses without data. Counted toward `--max-tries`. |
| 6 | `--lowest-speed-limit` | | `--lowest-speed-limit=SPEED` | Speed; `0` = disabled | `0` | `#http` `#ftp` | No | — | Close connection if speed ≤ SPEED. Does not affect BT. |
| 7 | `--remote-time` | `-R` | `--remote-time[=true\|false]` | Boolean | `false` | `#http` `#ftp` | No | Optional Arg | Apply remote file's timestamp to local file. |
| 8 | `--reuse-uri` | | `--reuse-uri[=true\|false]` | Boolean | `true` | `#http` `#ftp` | No | Optional Arg | Reuse already-used URIs when none unused remain. |
| 9 | `--uri-selector` | | `--uri-selector=SELECTOR` | Enum: `inorder`, `feedback`, `adaptive` | `feedback` | `#http` `#ftp` | No | — | URI selection algorithm. |
| 10 | `--stream-piece-selector` | | `--stream-piece-selector=SELECTOR` | Enum: `default`, `inorder`, `random`, `geom` | `default` | `#http` `#ftp` | No | — | Piece selection algorithm for segmented HTTP/FTP. |
| 11 | `--server-stat-of` | | `--server-stat-of=FILE` | String (path) | — | `#http` `#ftp` | No | — | Save server performance profile to file. |
| 12 | `--server-stat-if` | | `--server-stat-if=FILE` | String (path) | — | `#http` `#ftp` | No | — | Load server performance profile from file. |
| 13 | `--server-stat-timeout` | | `--server-stat-timeout=SEC` | Duration | `86400` | `#http` `#ftp` | No | — | Seconds before profile entry expires. |
| 14 | `--proxy-method` | | `--proxy-method=METHOD` | Enum: `get`, `tunnel` | `get` | `#http` `#ftp` | No | — | Proxy request method. HTTPS always uses `tunnel`. |
| 15 | `--all-proxy` | | `--all-proxy=PROXY` | Proxy URI; `""` clears | — | `#http` `#ftp` | No | — | Proxy for all protocols. Overridden by protocol-specific proxies. |
| 16 | `--all-proxy-user` | | `--all-proxy-user=USER` | String | — | `#http` `#ftp` | No | — | Username for `--all-proxy`. |
| 17 | `--all-proxy-passwd` | | `--all-proxy-passwd=PASSWD` | String | — | `#http` `#ftp` | No | — | Password for `--all-proxy`. |
| 18 | `--no-proxy` | | `--no-proxy=DOMAINS` | String (comma-separated host/domain/CIDR) | — | `#http` `#ftp` | No | — | Hosts/domains/networks that bypass proxy. |
| 19 | `--dry-run` | | `--dry-run[=true\|false]` | Boolean | `false` | `#http` `#ftp` | No | Optional Arg | Check remote file availability only; do not download. Cancels BT. |
| 20 | `--parameterized-uri` | `-P` | `--parameterized-uri[=true\|false]` | Boolean | `false` | `#http` `#ftp` | No | Optional Arg | Enable parameterized URI expansion (`{sv1,sv2}`, `[000-100:2]`). |

---

## 7. Category: Checksum Options

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--checksum` | | `--checksum=TYPE=DIGEST` | Checksum (sha-1, sha-256, etc.) | — | `#checksum` | No | — | Expected checksum for HTTP/FTP downloads. |
| 2 | `--realtime-chunk-checksum` | | `--realtime-chunk-checksum[=true\|false]` | Boolean | `true` | `#checksum` | No | Optional Arg | Validate chunk checksums while downloading. |

---

## 8. Category: BitTorrent Options

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--bt-metadata-only` | | `--bt-metadata-only[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Download metadata only (magnet). |
| 2 | `--bt-save-metadata` | | `--bt-save-metadata[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Save metadata as .torrent file from magnet. |
| 3 | `--bt-load-saved-metadata` | | `--bt-load-saved-metadata[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Try saved metadata before DHT fetch for magnet. |
| 4 | `--bt-enable-lpd` | | `--bt-enable-lpd[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Enable Local Peer Discovery. Disabled for private torrents. |
| 5 | `--bt-lpd-interface` | | `--bt-lpd-interface=INTERFACE` | String (interface name or IP) | — | `#bittorrent` | No | — | Bind interface for LPD. |
| 6 | `--bt-tracker` | | `--bt-tracker=URI[,...]` | String (comma-separated tracker URIs) | — | `#bittorrent` | **Yes** | — | Additional announce URIs. Added after `--bt-exclude-tracker` filtering. |
| 7 | `--bt-exclude-tracker` | | `--bt-exclude-tracker=URI[,...]` | String; `*` removes all | — | `#bittorrent` | **Yes** | — | Tracker URIs to remove. Repeatable; each call appends. |
| 8 | `--bt-tracker-connect-timeout` | | `--bt-tracker-connect-timeout=SEC` | Duration | `60` | `#bittorrent` | No | — | Tracker connection timeout. |
| 9 | `--bt-tracker-timeout` | | `--bt-tracker-timeout=SEC` | Duration | `60` | `#bittorrent` | No | — | Tracker read timeout. |
| 10 | `--bt-tracker-interval` | | `--bt-tracker-interval=SEC` | Duration; `0` = auto | `0` | `#bittorrent` | No | — | Force tracker announce interval. |
| 11 | `--bt-max-peers` | | `--bt-max-peers=NUM` | Integer; `0` = unlimited | `55` | `#bittorrent` | No | — | Max peers per torrent. |
| 12 | `--bt-request-peer-speed-limit` | | `--bt-request-peer-speed-limit=SPEED` | Speed | `50K` | `#bittorrent` | No | — | If overall speed < SPEED, temporarily increase peer count. |
| 13 | `--bt-stop-timeout` | | `--bt-stop-timeout=SEC` | Duration; `0` = disabled | `0` | `#bittorrent` | No | — | Stop BT download if speed is 0 for N seconds. |
| 14 | `--bt-prioritize-piece` | | `--bt-prioritize-piece=head[=SIZE],tail[=SIZE]` | Special; SIZE defaults to 1M | — | `#bittorrent` | No | — | Prioritize first/last pieces of each file. |
| 15 | `--bt-hash-check-seed` | | `--bt-hash-check-seed[=true\|false]` | Boolean | `true` | `#bittorrent` | No | Optional Arg | After hash check, continue seeding if complete. |
| 16 | `--bt-seed-unverified` | | `--bt-seed-unverified[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Seed without verifying piece hashes. |
| 17 | `--bt-remove-unselected-file` | | `--bt-remove-unselected-file[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Remove unselected files after download completes. |
| 18 | `--bt-max-open-files` | | `--bt-max-open-files=NUM` | Integer | `100` | `#bittorrent` | No | — | Max files to open in multi-file BT/Metalink. |
| 19 | `--bt-detach-seed-only` | | `--bt-detach-seed-only[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Exclude seeding from `--max-concurrent-downloads` count. |
| 20 | `--bt-enable-hook-after-hash-check` | | `--bt-enable-hook-after-hash-check[=true\|false]` | Boolean | `true` | `#bittorrent` | No | Optional Arg | Run `--on-bt-download-complete` after hash check. |
| 21 | `--bt-force-encryption` | | `--bt-force-encryption[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Require encryption: deny legacy handshake, enforce arc4 payload. Shorthand for `--bt-require-crypto --bt-min-crypto-level=arc4`. |
| 22 | `--bt-require-crypto` | | `--bt-require-crypto[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Require Obfuscation handshake; reject legacy BT handshake. |
| 23 | `--bt-min-crypto-level` | | `--bt-min-crypto-level=plain\|arc4` | Enum: `plain`, `arc4` | `plain` | `#bittorrent` | No | — | Minimum encryption level from peers. |
| 24 | `--bt-external-ip` | | `--bt-external-ip=IPADDRESS` | String (IPv4 or IPv6) | — | `#bittorrent` | No | — | External IP reported to tracker and DHT. |
| 25 | `--peer-id-prefix` | | `--peer-id-prefix=PEER_ID_PREFIX` | String (truncated to 20 bytes) | `A2-$MAJOR-$MINOR-$PATCH-` | `#bittorrent` | No | — | Prefix for 20-byte BT peer ID. Padded with random if shorter. |
| 26 | `--peer-agent` | | `--peer-agent=PEER_AGENT` | String | `aria2/$MAJOR.$MINOR.$PATCH` | `#bittorrent` | No | — | Peer client version string in extended handshake. |
| 27 | `--seed-ratio` | | `--seed-ratio=RATIO` | Float; `0.0` = seed indefinitely | `1.0` | `#bittorrent` | No | — | Share ratio threshold. Seeding stops when ratio or seed-time met. |
| 28 | `--seed-time` | | `--seed-time=MINUTES` | Float (fractional minutes); `0` disables seeding | — | `#bittorrent` | No | — | Seeding duration threshold. |
| 29 | `--listen-port` | | `--listen-port=PORT...` | Range (comma/range: `6881-6999,51413`) | `6881-6999` | `#bittorrent` | No | — | TCP port range for BT downloads. |
| 30 | `--torrent-file` | `-T` | `--torrent-file=TORRENT_FILE` | String (path) | — | `#bittorrent` `#file` | No | — | Explicit .torrent file path. Optional; .torrent files can be specified without it. |
| 31 | `--follow-torrent` | | `--follow-torrent=true\|false\|mem` | Enum: `true`, `false`, `mem` | `true` | `#bittorrent` | No | — | Parse downloaded .torrent files. `mem` keeps torrent in memory. |
| 32 | `--select-file` | | `--select-file=INDEX...` | Range (comma/range: `1-5,8,9`) | — | `#bittorrent` | No | — | Select files by index within torrent/metalink. |
| 33 | `--show-files` | `-S` | `--show-files[=true\|false]` | Boolean | `false` | `#bittorrent` | No | Optional Arg | Print file listing of .torrent/.metalink and exit. |
| 34 | `--index-out` | `-O` | `--index-out=INDEX=PATH` | Special | — | `#bittorrent` | **Yes** | — | Set output path for a specific file index. Repeatable. |
| 35 | `--dscp` | | `--dscp=DSCP` | Integer (DSCP value for TOS field) | — | `#bittorrent` | No | — | Set DSCP for BT traffic QoS. |
| 36 | `--enable-dht` | | `--enable-dht[=true\|false]` | Boolean | `true` | `#bittorrent` | No | Optional Arg | Enable IPv4 DHT. Disabled for private torrents. |
| 37 | `--enable-dht6` | | `--enable-dht6[=true\|false]` | Boolean | — | `#bittorrent` | No | Optional Arg | Enable IPv6 DHT. Disabled for private torrents. |
| 38 | `--dht-listen-port` | | `--dht-listen-port=PORT...` | Range | `6881-6999` | `#bittorrent` | No | — | UDP port range for DHT (v4+v6) and UDP tracker. |
| 39 | `--dht-listen-addr6` | | `--dht-listen-addr6=ADDR` | String (global unicast IPv6) | — | `#bittorrent` | No | — | Bind address for IPv6 DHT socket. |
| 40 | `--dht-entry-point` | | `--dht-entry-point=HOST:PORT` | HostPort | — | `#bittorrent` | **Yes** | — | Bootstrap entry point for IPv4 DHT. Repeatable. |
| 41 | `--dht-entry-point6` | | `--dht-entry-point6=HOST:PORT` | HostPort | — | `#bittorrent` | **Yes** | — | Bootstrap entry point for IPv6 DHT. Repeatable. |
| 42 | `--dht-file-path` | | `--dht-file-path=PATH` | String (path) | `$HOME/.aria2/dht.dat` or `$XDG_CACHE_HOME/aria2/dht.dat` | `#bittorrent` | No | — | IPv4 DHT routing table file path. |
| 43 | `--dht-file-path6` | | `--dht-file-path6=PATH` | String (path) | `$HOME/.aria2/dht6.dat` or `$XDG_CACHE_HOME/aria2/dht6.dat` | `#bittorrent` | No | — | IPv6 DHT routing table file path. |
| 44 | `--dht-message-timeout` | | `--dht-message-timeout=SEC` | Duration | `10` | `#bittorrent` | No | — | DHT message timeout. |
| 45 | `--enable-peer-exchange` | | `--enable-peer-exchange[=true\|false]` | Boolean | `true` | `#bittorrent` | No | Optional Arg | Enable PEX extension. Disabled for private torrents. |

---

## 9. Category: Metalink Options

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--follow-metalink` | | `--follow-metalink=true\|false\|mem` | Enum: `true`, `false`, `mem` | `true` | `#metalink` | No | — | Parse downloaded .metalink/.meta4 files. `mem` keeps metalink in memory. |
| 2 | `--metalink-base-uri` | | `--metalink-base-uri=URI` | String (URI; directory URIs end with `/`) | — | `#metalink` | No | — | Base URI for resolving relative URIs in local metalink files. |
| 3 | `--metalink-file` | `-M` | `--metalink-file=METALINK_FILE` | String (path; `-` for stdin) | — | `#metalink` `#file` | No | — | Explicit metalink file path. Optional; metalink files can be specified without it. |
| 4 | `--metalink-language` | | `--metalink-language=LANGUAGE` | String (language code) | — | `#metalink` | No | — | Preferred language for file selection. |
| 5 | `--metalink-location` | | `--metalink-location=LOCATION[,...]` | String (comma-separated location codes) | — | `#metalink` | No | — | Preferred server location. |
| 6 | `--metalink-os` | | `--metalink-os=OS` | String (OS name) | — | `#metalink` | No | — | Preferred operating system for file selection. |
| 7 | `--metalink-version` | | `--metalink-version=VERSION` | String (version string) | — | `#metalink` | No | — | Preferred file version. |
| 8 | `--metalink-preferred-protocol` | | `--metalink-preferred-protocol=PROTO` | Enum: `http`, `https`, `ftp`, `none` | `none` | `#metalink` | No | — | Preferred protocol. `none` disables preference. |
| 9 | `--metalink-enable-unique-protocol` | | `--metalink-enable-unique-protocol[=true\|false]` | Boolean | `true` | `#metalink` | No | Optional Arg | Use one protocol per mirror when multiple available. |

---

## 10. Category: RPC Options

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--enable-rpc` | | `--enable-rpc[=true\|false]` | Boolean | `false` | `#rpc` | No | Optional Arg | Enable JSON-RPC/XML-RPC server. |
| 2 | `--rpc-listen-port` | | `--rpc-listen-port=PORT` | Integer (1024–65535) | `6800` | `#rpc` | No | — | Port for RPC server. |
| 3 | `--rpc-listen-all` | | `--rpc-listen-all[=true\|false]` | Boolean | `false` | `#rpc` | No | Optional Arg | Listen on all interfaces. If false, loopback only. |
| 4 | `--rpc-allow-origin-all` | | `--rpc-allow-origin-all[=true\|false]` | Boolean | `false` | `#rpc` | No | Optional Arg | Add `Access-Control-Allow-Origin: *`. |
| 5 | `--rpc-secret` | | `--rpc-secret=TOKEN` | String | — | `#rpc` | No | — | RPC secret authorization token. |
| 6 | `--rpc-user` | | `--rpc-user=USER` | String | — | `#rpc` `#deprecated` | No | — | RPC basic-auth username. **DEPRECATED.** |
| 7 | `--rpc-passwd` | | `--rpc-passwd=PASSWD` | String | — | `#rpc` `#deprecated` | No | — | RPC basic-auth password. **DEPRECATED.** |
| 8 | `--rpc-secure` | | `--rpc-secure[=true\|false]` | Boolean | `false` | `#rpc` | No | Optional Arg | Encrypt RPC transport with SSL/TLS. |
| 9 | `--rpc-certificate` | | `--rpc-certificate=FILE` | String (PKCS12 or PEM; Apple TLS: SHA-1 fingerprint) | — | `#rpc` | No | — | Certificate for RPC server TLS. |
| 10 | `--rpc-private-key` | | `--rpc-private-key=FILE` | String (path to decrypted PEM) | — | `#rpc` | No | — | Private key for RPC server TLS. |
| 11 | `--rpc-max-request-size` | | `--rpc-max-request-size=SIZE` | Size | `2M` | `#rpc` | No | — | Max RPC request body size. Exceeding requests dropped. |
| 12 | `--rpc-save-upload-metadata` | | `--rpc-save-upload-metadata[=true\|false]` | Boolean | `true` | `#rpc` | No | Optional Arg | Save uploaded torrent/metalink metadata to disk. |
| 13 | `--pause` | | `--pause[=true\|false]` | Boolean | `false` | `#rpc` | No | Optional Arg | Pause downloads after added (RPC mode only). |
| 14 | `--pause-metadata` | | `--pause-metadata[=true\|false]` | Boolean | `false` | `#rpc` | No | Optional Arg | Pause downloads from metadata (RPC mode only). |

---

## 11. Category: Advanced Options

| # | Flag | Short | Syntax | Type | Default | Category | Accum | Bool Style | Description |
|---|------|-------|--------|------|---------|----------|-------|------------|-------------|
| 1 | `--conf-path` | | `--conf-path=PATH` | String (path) | `$HOME/.aria2/aria2.conf` or `$XDG_CONFIG_HOME/aria2/aria2.conf` | `#advanced` `#file` | No | — | Configuration file path. |
| 2 | `--no-conf` | | `--no-conf[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Disable loading aria2.conf entirely. |
| 3 | `--allow-overwrite` | | `--allow-overwrite[=true\|false]` | Boolean | `false` | `#advanced` `#file` | No | Optional Arg | Restart from scratch if control file is missing. |
| 4 | `--allow-piece-length-change` | | `--allow-piece-length-change[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | If false, abort when piece length differs from control file. |
| 5 | `--always-resume` | | `--always-resume[=true\|false]` | Boolean | `true` | `#advanced` | No | Optional Arg | Always resume. If false and N URIs don't support resume, restart from scratch. |
| 6 | `--max-resume-failure-tries` | | `--max-resume-failure-tries=N` | Integer; `0` = all must fail resume | `0` | `#advanced` | No | — | Number of resume failures before restarting from scratch. |
| 7 | `--auto-file-renaming` | | `--auto-file-renaming[=true\|false]` | Boolean | `true` | `#advanced` `#file` | No | Optional Arg | Rename file (append `.1` .. `.9999`) if already exists. HTTP/FTP only. |
| 8 | `--conditional-get` | | `--conditional-get[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Download only if local file is older than remote. Uses If-Modified-Since. |
| 9 | `--content-disposition-default-utf8` | | `--content-disposition-default-utf8[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Treat Content-Disposition filename as UTF-8. |
| 10 | `--disk-cache` | | `--disk-cache=SIZE` | Size; `0` disables | `16M` | `#advanced` | No | — | Enable disk cache. Grows up to SIZE bytes. |
| 11 | `--file-allocation` | | `--file-allocation=METHOD` | Enum: `none`, `prealloc`, `trunc`, `falloc` | `prealloc` | `#advanced` `#file` | No | — | File allocation method. |
| 12 | `--no-file-allocation-limit` | | `--no-file-allocation-limit=SIZE` | Size | `5M` | `#advanced` `#file` | No | — | Skip file allocation for files smaller than SIZE. |
| 13 | `--enable-mmap` | | `--enable-mmap[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Map files into memory. |
| 14 | `--max-mmap-limit` | | `--max-mmap-limit=SIZE` | Size | `9223372036854775807` | `#advanced` | No | — | Max file size for memmapping. Total of all files in download. |
| 15 | `--force-save` | | `--force-save[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Save download to session even if completed or removed. |
| 16 | `--save-not-found` | | `--save-not-found[=true\|false]` | Boolean | `true` | `#advanced` | No | Optional Arg | Save download to session even if file not found on server. |
| 17 | `--save-session` | | `--save-session=FILE` | String (path; `.gz` suffix compresses) | — | `#advanced` `#file` | No | — | Save error/unfinished downloads on exit. |
| 18 | `--save-session-interval` | | `--save-session-interval=SEC` | Duration; `0` = save only on exit | `0` | `#advanced` | No | — | Periodic session saving interval. |
| 19 | `--auto-save-interval` | | `--auto-save-interval=SEC` | Duration (0–600); `0` disables | `60` | `#advanced` | No | — | Save .aria2 control files every SEC seconds. |
| 20 | `--remove-control-file` | | `--remove-control-file[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Delete control file before download begins. |
| 21 | `--hash-check-only` | | `--hash-check-only[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Hash check only; abort regardless of result. |
| 22 | `--gid` | | `--gid=GID` | String (16 hex chars [0-9a-fA-F]; all-zero reserved) | — | `#advanced` | No | — | Manually set GID for a download. Must be unique. |
| 23 | `--stop` | | `--stop=SEC` | Duration; `0` = disabled | `0` | `#advanced` | No | — | Stop application after SEC seconds. |
| 24 | `--stop-with-process` | | `--stop-with-process=PID` | Integer (PID of parent process) | — | `#advanced` | No | — | Stop application when the given PID no longer exists. |
| 25 | `--interface` | | `--interface=INTERFACE` | String (interface name, IP address, hostname) | — | `#advanced` | No | — | Bind sockets to specified interface. |
| 26 | `--multiple-interface` | | `--multiple-interface=INTERFACES` | String (comma-separated interface/IP/hostname list) | — | `#advanced` | No | — | Split requests across interfaces for link aggregation. Ignored if `--interface` set. |
| 27 | `--disable-ipv6` | | `--disable-ipv6[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Disable IPv6. |
| 28 | `--async-dns` | | `--async-dns[=true\|false]` | Boolean | `true` | `#advanced` | No | Optional Arg | Enable asynchronous DNS resolution. |
| 29 | `--async-dns-server` | | `--async-dns-server=IPADDRESS[,...]` | String (comma-separated DNS server IPs) | — | `#advanced` | No | — | Override DNS servers. |
| 30 | `--min-tls-version` | | `--min-tls-version=VERSION` | Enum: `TLSv1.1`, `TLSv1.2`, `TLSv1.3` | `TLSv1.2` | `#advanced` | No | — | Minimum SSL/TLS version. |
| 31 | `--event-poll` | | `--event-poll=POLL` | Enum: `epoll`, `kqueue`, `port`, `poll`, `select` | Platform-dependent | `#advanced` | No | — | Event polling backend. |
| 32 | `--piece-length` | | `--piece-length=LENGTH` | Size | `1M` | `#advanced` | No | — | Piece length for HTTP/FTP. Ignored for BT/Metalink with piece hashes. |
| 33 | `--socket-recv-buffer-size` | | `--socket-recv-buffer-size=SIZE` | Size (bytes); `0` disables | `0` | `#advanced` | No | — | Maximum socket receive buffer (SO_RCVBUF). |
| 34 | `--rlimit-nofile` | | `--rlimit-nofile=NUM` | Integer | — | `#advanced` | No | — | Set soft limit of open file descriptors (POSIX only). Only increases. |
| 35 | `--deferred-input` | | `--deferred-input[=true\|false]` | Boolean | `false` | `#advanced` | No | Optional Arg | Read `--input-file` one line at a time. Disabled when `--save-session` is used. |
| 36 | `--max-download-result` | | `--max-download-result=NUM` | Integer; `0` = keep none | `1000` | `#advanced` | No | — | Max completed/error/removed download results in memory (FIFO). |
| 37 | `--keep-unfinished-download-result` | | `--keep-unfinished-download-result[=true\|false]` | Boolean | `true` | `#advanced` | No | Optional Arg | Keep unfinished download results beyond `--max-download-result` limit. |
| 38 | `--enable-color` | | `--enable-color[=true\|false]` | Boolean | `true` | `#advanced` | No | Optional Arg | Enable color output on terminal. |
| 39 | `--on-download-start` | | `--on-download-start=COMMAND` | String (path to executable) | — | `#advanced` `#hook` | No | — | Command run after download starts. Gets 3 args: GID, file count, first file path. |
| 40 | `--on-download-pause` | | `--on-download-pause=COMMAND` | String (path to executable) | — | `#advanced` `#hook` | No | — | Command run after download paused. |
| 41 | `--on-download-stop` | | `--on-download-stop=COMMAND` | String (path to executable) | — | `#advanced` `#hook` | No | — | Command run after download stops (any reason). Overridden by `--on-download-complete`/`--on-download-error` if set. |
| 42 | `--on-download-complete` | | `--on-download-complete=COMMAND` | String (path to executable) | — | `#advanced` `#hook` | No | — | Command run after download completes successfully. |
| 43 | `--on-download-error` | | `--on-download-error=COMMAND` | String (path to executable) | — | `#advanced` `#hook` | No | — | Command run after download fails with error. |
| 44 | `--on-bt-download-complete` | | `--on-bt-download-complete=COMMAND` | String (path to executable) | — | `#advanced` `#hook` | No | — | Command run after BT download completes but before seeding. |

---

## 12. Accumulative Flags (Summary)

These flags can appear multiple times; each appearance appends to the list rather than overwriting:

| Flag | Appends |
|------|---------|
| `--header` | Each call adds one HTTP header |
| `--index-out` | Each call sets path for one file index |
| `--bt-tracker` | Each call adds tracker URIs |
| `--bt-exclude-tracker` | Each call adds tracker URIs to exclude |
| `--dht-entry-point` | Each call adds an IPv4 DHT bootstrap node |
| `--dht-entry-point6` | Each call adds an IPv6 DHT bootstrap node |

For all other flags, subsequent appearances overwrite the previous value.

---

## 13. Environment Variable Equivalents

aria2 reads proxy settings from environment variables. These override config-file values but are themselves overridden by explicit CLI flags.

| Environment Variable | Equivalent Flag | Format |
|---------------------|----------------|--------|
| `http_proxy` | `--http-proxy` | `[http://][USER:PASSWORD@]HOST[:PORT]` |
| `https_proxy` | `--https-proxy` | `[http://][USER:PASSWORD@]HOST[:PORT]` |
| `ftp_proxy` | `--ftp-proxy` | `[http://][USER:PASSWORD@]HOST[:PORT]` |
| `all_proxy` | `--all-proxy` | `[http://][USER:PASSWORD@]HOST[:PORT]` |
| `no_proxy` | `--no-proxy` | Comma-separated host/domain/CIDR |

Note: aria2 accepts `ftp://` and `https://` schemes in proxy URIs from environment variables but treats them identically to `http://` — it does not change behavior based on scheme.

---

## 14. Proxy Credential Override Precedence

For all proxy options (`--http-proxy`, `--https-proxy`, `--ftp-proxy`, `--all-proxy`), credentials can be specified both inline in the proxy URI and via the companion `--*-proxy-user` / `--*-proxy-passwd` options. The concrete rule:

> **The option appearing later on the command line wins.**

Examples:

1. `--http-proxy="http://proxy" --http-proxy-user=myname --http-proxy-passwd=mypass`
   → Proxy `http://proxy` with user `myname`, password `mypass`.

2. `--http-proxy="http://user:pass@proxy" --http-proxy-user="myname" --http-proxy-passwd="mypass"`
   → Proxy `http://proxy` with user `myname`, password `mypass` (explicit user/passwd override inline creds because they appear later).

3. `--http-proxy-user="myname" --http-proxy-passwd="mypass" --http-proxy="http://user:pass@proxy"`
   → Proxy `http://proxy` with user `user`, password `pass` (inline creds override because the proxy flag appears last).

This same precedence applies when config-file, environment variable, and CLI values interact: later-appearing overrides earlier-appearing.

---

## 15. Option Precedence Order

Options are resolved from lowest to highest precedence:

1. **Hardcoded defaults** (shown in tables above)
2. **Config file** (`aria2.conf`) — loaded from `$HOME/.aria2/aria2.conf` or `$XDG_CONFIG_HOME/aria2/aria2.conf` or `--conf-path`
3. **Environment variables** (proxy-only: `http_proxy`, `https_proxy`, `ftp_proxy`, `all_proxy`, `no_proxy`)
4. **Input-file options** (per-URI options in `--input-file` lines, without `--` prefix)
5. **CLI arguments** (`--key=value`) — highest precedence

---

## 16. `--help` Category Tag Reference

The `--help` flag accepts tags for categorised output. All valid tags:

| Tag | Covers |
|-----|--------|
| `#basic` | Basic Options (default if no argument) |
| `#advanced` | Advanced Options |
| `#http` | HTTP-specific options |
| `#https` | HTTPS-specific options |
| `#ftp` | FTP/SFTP-specific options |
| `#metalink` | Metalink options |
| `#bittorrent` | BitTorrent options |
| `#cookie` | Cookie-related options (`--load-cookies`, `--save-cookies`) |
| `#hook` | Event hook options (`--on-download-*`, `--on-bt-download-complete`) |
| `#file` | File-related options (`--dir`, `--out`, `--input-file`, etc.) |
| `#rpc` | RPC options |
| `#checksum` | Checksum options |
| `#experimental` | Experimental options (if any) |
| `#deprecated` | Deprecated options (`--rpc-user`, `--rpc-passwd`) |
| `#help` | Help tag (includes `--help` itself) |
| `#all` | All options |

When a non-tag keyword is given (e.g., `--help=timeout`), `--help` prints options whose name contains the keyword.

---

## 17. `--version` Output

The `--version` flag (also `-v`) takes no argument and prints:

1. Version number string (e.g., `aria2 version 1.37.0`)
2. Copyright notice
3. Compile-time configuration summary (enabled features)
4. List of supported hash algorithms

The output format is multi-line plain text to stdout. The program exits with code 0 after printing.

---

## 18. Input File Option Restrictions

When using `--input-file`, per-URI options are specified on lines starting with whitespace (SPACE or TAB) after the URI line(s). In this context, the `--` prefix is **omitted** from option names. All options listed in §2 of the aria2 manual Input File subsection (see `config-keys.md` input-file enumerations) are supported in this context.

---

## Option Count Summary

| Category | Count |
|----------|-------|
| Special/Operational | 2 |
| Basic | 26 |
| HTTP | 24 |
| FTP/SFTP | 11 |
| HTTP/FTP/SFTP Shared | 20 |
| Checksum | 2 |
| BitTorrent | 45 |
| Metalink | 9 |
| RPC | 14 |
| Advanced | 44 |
| **Total** | **197** option entries |
