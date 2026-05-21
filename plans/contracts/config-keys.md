# Config Keys Contract

**Source:** aria2 1.37.0 manual (`aria2c.rst`)
**Schema:** aria2go must accept every option listed below via CLI (`--key=VALUE`), config file (`key=VALUE`), RPC (`options` struct), and environment variables (where applicable).
**Status:** Spec-authored from clean-room reading of the aria2 manual; zero aria2 LOC.

## Precedence

Options are resolved in this order (highest to lowest):

1. **CLI arguments** (`--key=value`)
2. **Input-file options** (per-URI options in `--input-file` lines)
3. **Environment variables** (specific vars like `http_proxy`, `https_proxy`, `ftp_proxy`, `all_proxy`, `no_proxy`)
4. **Config file** (`aria2.conf`)
5. **Hardcoded defaults**

## Value Types and Grammar

| Type | Grammar | Examples |
|------|---------|----------|
| String | Arbitrary text; `""` is valid empty. Some keys accept `*` as wildcard. | `"myfile.zip"`, `""`, `"*"` |
| Integer | Non-negative decimal integer. | `5`, `60`, `0` |
| Boolean | `true` or `false` (case-sensitive). In CLI short form, option with no argument means `true`. | `true`, `false` |
| Size | Non-negative decimal integer optionally suffixed with `K`/`k` (x1024) or `M`/`m` (x1048576). | `1024`, `10K`, `20M`, `0` |
| Speed | Same grammar as Size, used for bytes/sec limits. | `50K`, `1M`, `0` |
| Duration | Integer in seconds (used for timeouts and intervals). Arbitrary precision allowed for `seed-time` (fractional minutes). | `60`, `86400`, `10` |
| Enum | One of a fixed set of strings. | `none`, `prealloc`, `inorder` |
| Proxy URI | `[http://][USER:PASSWORD@]HOST[:PORT]` | `http://user:pass@proxy:8080` |
| Checksum | `TYPE=DIGEST` where TYPE is hash algorithm name and DIGEST is hex string. | `sha-1=0192ba11326f...` |

## Accumulative Options

The following options may appear multiple times; each appearance appends rather than replaces:

- `--header`
- `--index-out`
- `--bt-tracker`
- `--bt-exclude-tracker`
- `--dht-entry-point`
- `--dht-entry-point6`

## Category: Basic Options

These control the core download session: output location, concurrency, logging, and integrity.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `dir` | `-d` | String | `.` (cwd) | Path string | No | Directory to store downloaded files. |
| 2 | `input-file` | `-i` | String | — | Path to file, or `-` for stdin. Gzipped files supported. | No | Downloads URIs listed in the file. Multiple sources per entity separated by TAB. Option lines prefixed with whitespace. |
| 3 | `log` | `-l` | String | `""` (no log) | Path to file, or `-` for stdout, or `""` to disable. | No | File name for log output. |
| 4 | `max-concurrent-downloads` | `-j` | Integer | `5` | Non-negative integer | No | Maximum number of parallel download items. |
| 5 | `check-integrity` | `-V` | Boolean | `false` | `true`/`false` | No | Validate piece hashes or whole-file hash. Effective for BT, Metalink with checksums, and HTTP/FTP with `--checksum`. |
| 6 | `continue` | `-c` | Boolean | `false` | `true`/`false` | No | Resume a partially downloaded file from the beginning (HTTP/FTP). |
| 7 | `help` | `-h` | String | `#basic` | Tag (`#http`, `#ftp`, `#bittorrent`, etc.) or keyword. | No | Print usage for matching options. |
| 8 | `log-level` | | Enum | `debug` | `debug`, `info`, `notice`, `warn`, `error` | No | Set log level for file output. |
| 9 | `console-log-level` | | Enum | `notice` | `debug`, `info`, `notice`, `warn`, `error` | No | Set log level for console output. |
| 10 | `daemon` | `-D` | Boolean | `false` | `true`/`false` | No | Run as daemon. Changes cwd to `/`, redirects stdio to `/dev/null`. |
| 11 | `split` | `-s` | Integer | `5` | Non-negative integer | No | Download using N connections per download item. |
| 12 | `max-connection-per-server` | `-x` | Integer | `1` | Non-negative integer | No | Max connections to one server per download. |
| 13 | `min-split-size` | `-k` | Size | `20M` | Size (`1M`–`1024M`) | No | Minimum segment size for splitting. File splits only if 2*SIZE fits. |
| 14 | `max-overall-download-limit` | | Speed | `0` | Speed; `0` = unlimited | No | Global max download speed (bytes/sec). |
| 15 | `max-download-limit` | | Speed | `0` | Speed; `0` = unlimited | No | Max download speed per download (bytes/sec). |
| 16 | `max-overall-upload-limit` | | Speed | `0` | Speed; `0` = unlimited | No | Global max upload speed (bytes/sec). |
| 17 | `max-upload-limit` | `-u` | Speed | `0` | Speed; `0` = unlimited | No | Max upload speed per torrent (bytes/sec). |
| 18 | `out` | `-o` | String | — | File name, relative to `--dir`. | No | Output file name. Ignored with `--force-sequential`. Not applicable to BT/Metalink. |
| 19 | `quiet` | `-q` | Boolean | `false` | `true`/`false` | No | Suppress all console output. |
| 20 | `show-console-readout` | | Boolean | `true` | `true`/`false` | No | Show download progress readout on console. |
| 21 | `truncate-console-readout` | | Boolean | `true` | `true`/`false` | No | Truncate console readout to fit a single line. |
| 22 | `human-readable` | | Boolean | `true` | `true`/`false` | No | Print sizes as 1.2Ki, 3.4Mi etc. |
| 23 | `summary-interval` | | Duration | `60` | Integer seconds; `0` suppresses. | No | Interval in seconds to output download progress summary. |
| 24 | `download-result` | | Enum | `default` | `default`, `full`, `hide` | No | Format of the download result summary. |
| 25 | `optimize-concurrent-downloads` | | Special | `false` | `true`/`false` or `A:B` | No | Adapt concurrent download count to available bandwidth using N = A + B·log10(speed in Mbps). Default coeffs: A=5, B=25. |
| 26 | `force-sequential` | `-Z` | Boolean | `false` | `true`/`false` | No | Fetch URIs sequentially, each in a separate session. |
| 27 | `stderr` | | Boolean | `false` | `true`/`false` | No | Redirect console output to stderr instead of stdout. |
| 28 | `version` | `-v` | Flag | — | No argument; prints version and exits. | No | Print version and configuration info, then exit. |

## Category: HTTP Options

These affect all HTTP(S) downloads. Also applies to SFTP where the option does not name a specific TLS or cookie mechanism.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `http-user` | | String | — | Plain text string | No | HTTP username for all URIs. |
| 2 | `http-passwd` | | String | — | Plain text string | No | HTTP password for all URIs. |
| 3 | `user-agent` | `-U` | String | `aria2/$VERSION` | Arbitrary string | No | User-Agent header for HTTP(S) downloads. `$VERSION` substituted at build time. |
| 4 | `referer` | | String | — | Arbitrary string; `*` uses download URI as referrer. | No | Referer header for HTTP(S) downloads. |
| 5 | `enable-http-keep-alive` | | Boolean | `true` | `true`/`false` | No | Enable HTTP/1.1 persistent connections. |
| 6 | `enable-http-pipelining` | | Boolean | `false` | `true`/`false` | No | Enable HTTP/1.1 pipelining. |
| 7 | `http-accept-gzip` | | Boolean | `false` | `true`/`false` | No | Send `Accept-Encoding: deflate, gzip` and inflate gzip/deflate responses. |
| 8 | `http-auth-challenge` | | Boolean | `false` | `true`/`false` | No | Send Authorization header only when challenged (401). If false, always send (except embedded in URI). |
| 9 | `http-no-cache` | | Boolean | `false` | `true`/`false` | No | Send `Cache-Control: no-cache` and `Pragma: no-cache`. |
| 10 | `no-want-digest-header` | | Boolean | `false` | `true`/`false` | No | Omit `Want-Digest` header from HTTP requests. |
| 11 | `use-head` | | Boolean | `false` | `true`/`false` | No | Use HEAD method for first HTTP request. |
| 12 | `header` | | String | — | `"NAME: VALUE"` | **Yes** | Append custom HTTP header. Repeatable. |
| 13 | `load-cookies` | | String | — | Path to cookies file (Firefox3 SQLite, Chrome SQLite, Netscape format). | No | Load cookies from file. Requires sqlite3 for Firefox3/Chrome format. |
| 14 | `save-cookies` | | String | — | Path to output file (Netscape format). Overwrites existing. | No | Save cookies to file. Session cookies saved with expiry=0. |
| 15 | `ca-certificate` | | String | — | Path to PEM file of CA certificates. | No | CA certificate file for peer verification. |
| 16 | `certificate` | | String | — | Path to PKCS12 (.p12/.pfx) or PEM file. On Apple TLS, SHA-1 fingerprint. | No | Client certificate for HTTP. |
| 17 | `check-certificate` | | Boolean | `true` | `true`/`false` | No | Enable TLS certificate verification against `--ca-certificate`. |
| 18 | `private-key` | | String | — | Path to PEM file (decrypted). | No | Private key used with `--certificate` in PEM mode. |
| 19 | `http-proxy` | | Proxy | — | `[http://][USER:PASSWORD@]HOST[:PORT]`; `""` clears. | No | HTTP proxy. |
| 20 | `http-proxy-user` | | String | — | Plain text string | No | Username for `--http-proxy`. |
| 21 | `http-proxy-passwd` | | String | — | Plain text string | No | Password for `--http-proxy`. |
| 22 | `https-proxy` | | Proxy | — | `[http://][USER:PASSWORD@]HOST[:PORT]`; `""` clears. | No | HTTPS proxy. |
| 23 | `https-proxy-user` | | String | — | Plain text string | No | Username for `--https-proxy`. |
| 24 | `https-proxy-passwd` | | String | — | Plain text string | No | Password for `--https-proxy`. |

## Category: FTP/SFTP Options

These affect FTP downloads. Most apply to SFTP as well (exceptions noted). FTP proxy options reuse the same patterns as HTTP.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `ftp-user` | | String | `anonymous` | Plain text string | No | FTP username for all URIs. |
| 2 | `ftp-passwd` | | String | `ARIA2USER@` | Plain text string | No | FTP password for all URIs. If username embedded in URI but no password, falls back to .netrc then this value. |
| 3 | `ftp-pasv` | `-p` | Boolean | `true` | `true`/`false` | No | Use passive FTP mode. If false, active mode. Ignored for SFTP. |
| 4 | `ftp-type` | | Enum | `binary` | `binary`, `ascii` | No | FTP transfer type. Ignored for SFTP. |
| 5 | `ftp-reuse-connection` | | Boolean | `true` | `true`/`false` | No | Reuse FTP control connection. |
| 6 | `ftp-proxy` | | Proxy | — | `[http://][USER:PASSWORD@]HOST[:PORT]`; `""` clears. | No | FTP proxy. |
| 7 | `ftp-proxy-user` | | String | — | Plain text string | No | Username for `--ftp-proxy`. |
| 8 | `ftp-proxy-passwd` | | String | — | Plain text string | No | Password for `--ftp-proxy`. |
| 9 | `ssh-host-key-md` | | Checksum | — | `TYPE=DIGEST` where TYPE is `sha-1` or `md5`, DIGEST is hex. | No | Expected SSH host public key checksum for SFTP validation. |
| 10 | `netrc-path` | | String | `$(HOME)/.netrc` | Path to file (must have mode 600). | No | Path to netrc file for auto-login. |
| 11 | `no-netrc` | `-n` | Boolean | `false` | `true`/`false` | No | Disable netrc support. If true at startup, cannot be re-enabled mid-session. |

## Category: HTTP/FTP/SFTP Shared Options

Options that apply to HTTP, FTP, and SFTP downloads equally.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `connect-timeout` | | Duration | `60` | Integer seconds | No | Connection timeout to HTTP/FTP/proxy server. |
| 2 | `timeout` | `-t` | Duration | `60` | Integer seconds | No | Read timeout after connection is established. |
| 3 | `max-tries` | `-m` | Integer | `5` | Non-negative integer; `0` = unlimited. | No | Maximum number of retry attempts per download. |
| 4 | `retry-wait` | | Duration | `0` | Integer seconds; `0` = retry only on 503. When >0, retries for any failure. | No | Seconds to wait between retries. |
| 5 | `max-file-not-found` | | Integer | `0` | Non-negative integer; `0` = disabled. | No | Abort download after NUM "file not found" responses without any data. Counted toward `--max-tries`. |
| 6 | `lowest-speed-limit` | | Speed | `0` | Speed; `0` = disabled. | No | Close connection if download speed ≤ this value. Does not affect BT. |
| 7 | `remote-time` | `-R` | Boolean | `false` | `true`/`false` | No | Apply remote file's timestamp to the local file. |
| 8 | `reuse-uri` | | Boolean | `true` | `true`/`false` | No | Reuse already-used URIs when no unused URIs remain. |
| 9 | `uri-selector` | | Enum | `feedback` | `inorder`, `feedback`, `adaptive` | No | URI selection algorithm. `feedback` uses server performance profile. |
| 10 | `stream-piece-selector` | | Enum | `default` | `default`, `inorder`, `random`, `geom` | No | Piece selection algorithm for segmented HTTP/FTP downloads. |
| 11 | `server-stat-of` | | String | — | Path to file | No | File to which server performance profile is saved. |
| 12 | `server-stat-if` | | String | — | Path to file | No | File from which server performance profile is loaded. |
| 13 | `server-stat-timeout` | | Duration | `86400` | Integer seconds | No | Seconds before server performance profile entry expires. |
| 14 | `proxy-method` | | Enum | `get` | `get`, `tunnel` | No | Proxy request method. HTTPS always uses `tunnel`. |
| 15 | `all-proxy` | | Proxy | — | `[http://][USER:PASSWORD@]HOST[:PORT]`; `""` clears. | No | Proxy server for all protocols. Overridden by protocol-specific proxy options. |
| 16 | `all-proxy-user` | | String | — | Plain text string | No | Username for `--all-proxy`. |
| 17 | `all-proxy-passwd` | | String | — | Plain text string | No | Password for `--all-proxy`. |
| 18 | `no-proxy` | | String | — | Comma-separated host/domain/CIDR list. | No | Hosts/domains/network addresses that bypass proxy. |
| 19 | `dry-run` | | Boolean | `false` | `true`/`false` | No | Check remote file availability only; do not download. Cancels BT. |
| 20 | `parameterized-uri` | `-P` | Boolean | `false` | `true`/`false` | No | Enable parameterized URI expansion (`{sv1,sv2}`, `[000-100:2]`). |

## Category: Checksum Options

Options related to data integrity verification outside BT piece hashes.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `checksum` | | Checksum | — | `TYPE=HEXDIGEST` (sha-1, sha-256, etc.) | No | Expected checksum for HTTP/FTP downloads. |
| 2 | `realtime-chunk-checksum` | | Boolean | `true` | `true`/`false` | No | Validate chunk checksums while downloading (if provided). |

## Category: BitTorrent Options

All options prefixed `bt-*` and DHT-related options governing BitTorrent behavior.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `bt-metadata-only` | | Boolean | `false` | `true`/`false` | No | Download torrent metadata only (magnet link). |
| 2 | `bt-save-metadata` | | Boolean | `false` | `true`/`false` | No | Save metadata as .torrent file (magnet link). File named by hex info hash. |
| 3 | `bt-load-saved-metadata` | | Boolean | `false` | `true`/`false` | No | Try saved metadata before fetching from DHT for magnet links. |
| 4 | `bt-enable-lpd` | | Boolean | `false` | `true`/`false` | No | Enable Local Peer Discovery. Disabled for private torrents. |
| 5 | `bt-lpd-interface` | | String | — | Interface name or IP address. | No | Bind interface for LPD. |
| 6 | `bt-tracker` | | String | — | Comma-separated tracker URIs. | **Yes** | Additional announce URIs. Added after `--bt-exclude-tracker` filtering. |
| 7 | `bt-exclude-tracker` | | String | — | Comma-separated tracker URIs; `*` removes all. | **Yes** | Tracker URIs to remove. |
| 8 | `bt-tracker-connect-timeout` | | Duration | `60` | Integer seconds | No | Tracker connection timeout. |
| 9 | `bt-tracker-timeout` | | Duration | `60` | Integer seconds | No | Tracker read timeout. |
| 10 | `bt-tracker-interval` | | Duration | `0` | Integer seconds; `0` = auto from tracker response. | No | Force tracker announce interval. |
| 11 | `bt-max-peers` | | Integer | `55` | Non-negative integer; `0` = unlimited | No | Max peers per torrent. |
| 12 | `bt-request-peer-speed-limit` | | Speed | `50K` | Speed suffix | No | If total download speed drops below this, temporarily increase peer count. |
| 13 | `bt-stop-timeout` | | Duration | `0` | Integer seconds; `0` = disabled. | No | Stop BT download if speed is 0 for N consecutive seconds. |
| 14 | `bt-prioritize-piece` | | Special | — | `head[=SIZE],tail[=SIZE]`; SIZE defaults to 1M. | No | Prioritize first/last pieces of each file. |
| 15 | `bt-hash-check-seed` | | Boolean | `true` | `true`/`false` | No | After hash check, continue seeding if file is complete. |
| 16 | `bt-seed-unverified` | | Boolean | `false` | `true`/`false` | No | Seed without verifying piece hashes first. |
| 17 | `bt-remove-unselected-file` | | Boolean | `false` | `true`/`false` | No | Remove unselected files after download completes. |
| 18 | `bt-max-open-files` | | Integer | `100` | Non-negative integer | No | Maximum number of files to open in multi-file BT/Metalink globally. |
| 19 | `bt-detach-seed-only` | | Boolean | `false` | `true`/`false` | No | Exclude seeding downloads from `--max-concurrent-downloads` count. |
| 20 | `bt-enable-hook-after-hash-check` | | Boolean | `true` | `true`/`false` | No | Run `--on-bt-download-complete` hook after hash check succeeds. |
| 21 | `bt-force-encryption` | | Boolean | `false` | `true`/`false` | No | Require encryption: deny legacy handshake, enforce arc4 payload encryption. Shorthand for `--bt-require-crypto --bt-min-crypto-level=arc4`. |
| 22 | `bt-require-crypto` | | Boolean | `false` | `true`/`false` | No | Require Obfuscation handshake; reject legacy BitTorrent handshake. |
| 23 | `bt-min-crypto-level` | | Enum | `plain` | `plain`, `arc4` | No | Minimum acceptable encryption level from peers. |
| 24 | `bt-external-ip` | | String | — | IP address (IPv4 or IPv6) | No | External IP address reported to tracker and DHT. |
| 25 | `peer-id-prefix` | | String | `A2-$MAJOR-$MINOR-$PATCH-` | String; truncated to 20 bytes, padded with random if shorter. | No | Prefix for BT peer ID (20 bytes total). |
| 26 | `peer-agent` | | String | `aria2/$MAJOR.$MINOR.$PATCH` | Arbitrary string | No | Peer client version string sent in extended handshake. |
| 27 | `seed-ratio` | | Float | `1.0` | Non-negative float; `0.0` = seed indefinitely. | No | Share ratio threshold. Seeding stops when ratio or `--seed-time` condition met. |
| 28 | `seed-time` | | Float | — | Fractional minutes; `0` disables seeding. | No | Seeding duration threshold. |
| 29 | `listen-port` | | Range | `6881-6999` | Comma-separated ports and ranges (`6881-6889,6999`). | No | TCP port range for BT downloads. |
| 30 | `torrent-file` | `-T` | String | — | Path to .torrent file. | No | Explicit .torrent file path. Optional; .torrent files can be specified without it. |
| 31 | `follow-torrent` | | Enum | `true` | `true`, `false`, `mem` | No | If `true`/`mem`, parse downloaded .torrent files and download contents. `mem` keeps torrent in memory only. |
| 32 | `select-file` | | Range | — | Comma-separated indices and ranges (`1-5,8,9`). | No | Select files by index within a torrent/metalink. Adjacent files sharing pieces may also download. |
| 33 | `show-files` | `-S` | Boolean | `false` | `true`/`false` | No | Print file listing of .torrent/.metalink and exit. |
| 34 | `index-out` | `-O` | Special | — | `INDEX=PATH` (path relative to `--dir`). | **Yes** | Set output path for a specific file index within a BT download. |
| 35 | `dscp` | | Integer | — | DSCP value for TOS field of IP packets. | No | Set DSCP value for BT traffic QoS. |
| 36 | `enable-dht` | | Boolean | `true` | `true`/`false` | No | Enable IPv4 DHT. Disabled for private torrents. |
| 37 | `enable-dht6` | | Boolean | — | `true`/`false` | No | Enable IPv6 DHT. Disabled for private torrents. |
| 38 | `dht-listen-port` | | Range | `6881-6999` | Comma-separated ports and ranges. | No | UDP port range for DHT (IPv4+IPv6) and UDP tracker. |
| 39 | `dht-listen-addr6` | | String | — | Global unicast IPv6 address. | No | Bind address for IPv6 DHT socket. |
| 40 | `dht-entry-point` | | HostPort | — | `HOST:PORT` | **Yes** | Bootstrap entry point for IPv4 DHT network. |
| 41 | `dht-entry-point6` | | HostPort | — | `HOST:PORT` | **Yes** | Bootstrap entry point for IPv6 DHT network. |
| 42 | `dht-file-path` | | String | `$HOME/.aria2/dht.dat` or `$XDG_CACHE_HOME/aria2/dht.dat` | Path to file. | No | IPv4 DHT routing table file path. |
| 43 | `dht-file-path6` | | String | `$HOME/.aria2/dht6.dat` or `$XDG_CACHE_HOME/aria2/dht6.dat` | Path to file. | No | IPv6 DHT routing table file path. |
| 44 | `dht-message-timeout` | | Duration | `10` | Integer seconds | No | DHT message timeout. |
| 45 | `enable-peer-exchange` | | Boolean | `true` | `true`/`false` | No | Enable PEX extension. Disabled for private torrents. |

## Category: Metalink Options

Options controlling Metalink XML interpretation and mirror selection.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `follow-metalink` | | Enum | `true` | `true`, `false`, `mem` | No | If `true`/`mem`, parse downloaded .metalink/.meta4 files and download contents. `mem` keeps metalink in memory. |
| 2 | `metalink-base-uri` | | String | — | URI; directory URIs must end with `/`. | No | Base URI for resolving relative URIs in local metalink files. |
| 3 | `metalink-file` | `-M` | String | — | Path to .meta4/.metalink file; `-` for stdin. | No | Explicit metalink file path. Optional; metalink files can be specified without it. |
| 4 | `metalink-language` | | String | — | Language code string | No | Preferred language for file selection. |
| 5 | `metalink-location` | | String | — | Comma-separated location codes (`jp,us`). | No | Preferred server location. |
| 6 | `metalink-os` | | String | — | OS name string | No | Preferred operating system for file selection. |
| 7 | `metalink-version` | | String | — | Version string | No | Preferred file version. |
| 8 | `metalink-preferred-protocol` | | Enum | `none` | `http`, `https`, `ftp`, `none` | No | Preferred protocol. `none` disables protocol preference. |
| 9 | `metalink-enable-unique-protocol` | | Boolean | `true` | `true`/`false` | No | Use only one protocol per mirror when multiple are available. |

## Category: RPC Options

Options controlling the JSON-RPC/XML-RPC server and WebSocket transport.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `enable-rpc` | | Boolean | `false` | `true`/`false` | No | Enable JSON-RPC/XML-RPC server. |
| 2 | `rpc-listen-port` | | Integer | `6800` | `1024`–`65535` | No | Port for RPC server to listen on. |
| 3 | `rpc-listen-all` | | Boolean | `false` | `true`/`false` | No | Listen on all network interfaces. If false, loopback only. |
| 4 | `rpc-allow-origin-all` | | Boolean | `false` | `true`/`false` | No | Add `Access-Control-Allow-Origin: *` header to RPC responses. |
| 5 | `rpc-secret` | | String | — | Arbitrary string | No | RPC secret authorization token (method-level auth). |
| 6 | `rpc-user` | | String | — | Plain text string | No | RPC basic-auth username. **DEPRECATED** — migrate to `--rpc-secret`. |
| 7 | `rpc-passwd` | | String | — | Plain text string | No | RPC basic-auth password. **DEPRECATED** — migrate to `--rpc-secret`. |
| 8 | `rpc-secure` | | Boolean | `false` | `true`/`false` | No | Encrypt RPC transport with SSL/TLS. Clients use `https://` or `wss://`. |
| 9 | `rpc-certificate` | | String | — | PKCS12 or PEM file path; on Apple TLS, SHA-1 fingerprint. | No | Certificate for RPC server TLS. |
| 10 | `rpc-private-key` | | String | — | Path to PEM file (decrypted). | No | Private key for RPC server TLS. |
| 11 | `rpc-max-request-size` | | Size | `2M` | Size suffix | No | Maximum RPC request body size. Exceeding requests are dropped. |
| 12 | `rpc-save-upload-metadata` | | Boolean | `true` | `true`/`false` | No | Save uploaded torrent/metalink metadata to disk. If false, such downloads not saved by `--save-session`. |
| 13 | `pause` | | Boolean | `false` | `true`/`false` | No | Pause downloads after they are added (RPC mode only). |
| 14 | `pause-metadata` | | Boolean | `false` | `true`/`false` | No | Pause downloads generated from metadata (RPC mode only). |

## Category: Advanced Options

Options controlling disk behavior, networking internals, hooks, session management, and fine-tuning.

| # | Option Name | Short | Type | Default | Grammar | Accumulative | Description |
|---|------------|-------|------|---------|---------|-------------|-------------|
| 1 | `conf-path` | | String | `$HOME/.aria2/aria2.conf` or `$XDG_CONFIG_HOME/aria2/aria2.conf` | Path to file. | No | Configuration file path. |
| 2 | `no-conf` | | Boolean | `false` | `true`/`false` | No | Disable loading aria2.conf entirely. |
| 3 | `allow-overwrite` | | Boolean | `false` | `true`/`false` | No | Restart download from scratch if control file is missing. See also `--auto-file-renaming`. |
| 4 | `allow-piece-length-change` | | Boolean | `false` | `true`/`false` | No | If false, abort when piece length differs from control file. |
| 5 | `always-resume` | | Boolean | `true` | `true`/`false` | No | Always resume. If false and N URIs don't support resume, restart from scratch. |
| 6 | `max-resume-failure-tries` | | Integer | `0` | Non-negative integer; `0` = all URIs must fail resume. | No | Number of resume failures before restarting from scratch (when `--always-resume=false`). |
| 7 | `auto-file-renaming` | | Boolean | `true` | `true`/`false` | No | Rename file (append `.1` .. `.9999`) if it already exists. HTTP/FTP only. |
| 8 | `conditional-get` | | Boolean | `false` | `true`/`false` | No | Download only if local file is older than remote. Uses `If-Modified-Since`. HTTP(S) only. |
| 9 | `content-disposition-default-utf8` | | Boolean | `false` | `true`/`false` | No | Treat Content-Disposition filename as UTF-8 instead of ISO-8859-1. |
| 10 | `disk-cache` | | Size | `16M` | Size suffix; `0` disables. | No | Enable disk cache. Grows up to SIZE bytes. Shared across downloads. |
| 11 | `file-allocation` | | Enum | `prealloc` | `none`, `prealloc`, `trunc`, `falloc` | No | File allocation method. `none` = no preallocation. `falloc` uses `posix_fallocate`. |
| 12 | `no-file-allocation-limit` | | Size | `5M` | Size suffix | No | Skip file allocation for files smaller than SIZE. |
| 13 | `enable-mmap` | | Boolean | `false` | `true`/`false` | No | Map files into memory. May not work without preallocation. |
| 14 | `max-mmap-limit` | | Size | `9223372036854775807` | Size suffix | No | Maximum file size (total of all files in download) for which mmap is enabled. |
| 15 | `force-save` | | Boolean | `false` | `true`/`false` | No | Save download to session file even if completed or removed. |
| 16 | `save-not-found` | | Boolean | `true` | `true`/`false` | No | Save download to session file even if file not found on server. |
| 17 | `save-session` | | String | — | Path to file; `.gz` suffix compresses. | No | Save error/unfinished downloads to file on exit. |
| 18 | `save-session-interval` | | Duration | `0` | Integer seconds; `0` = save only on exit. | No | Interval for periodic session saving. |
| 19 | `auto-save-interval` | | Duration | `60` | Integer seconds (0–600); `0` disables periodic save. | No | Save .aria2 control files every SEC seconds. |
| 20 | `remove-control-file` | | Boolean | `false` | `true`/`false` | No | Delete control file before download begins. |
| 21 | `hash-check-only` | | Boolean | `false` | `true`/`false` | No | Perform hash check only; abort regardless of result. |
| 22 | `gid` | | String | — | 16-char hex string [0-9a-fA-F]; all-zero reserved. | No | Manually set GID for a download. Must be unique. |
| 23 | `stop` | | Duration | `0` | Integer seconds; `0` = disabled. | No | Stop application after SEC seconds. |
| 24 | `stop-with-process` | | Integer | — | PID of parent process. | No | Stop application when the given PID no longer exists. |
| 25 | `interface` | | String | — | Interface name, IP address, or hostname. | No | Bind sockets to specified interface. |
| 26 | `multiple-interface` | | String | — | Comma-separated interface/IP/hostname list. | No | Split requests across interfaces for link aggregation. Ignored if `--interface` is set. |
| 27 | `disable-ipv6` | | Boolean | `false` | `true`/`false` | No | Disable IPv6 (avoids slow AAAA lookups). |
| 28 | `async-dns` | | Boolean | `true` | `true`/`false` | No | Enable asynchronous DNS resolution. |
| 29 | `async-dns-server` | | String | — | Comma-separated DNS server IP addresses (IPv4/IPv6). | No | Override DNS servers used by async resolver. |
| 30 | `min-tls-version` | | Enum | `TLSv1.2` | `TLSv1.1`, `TLSv1.2`, `TLSv1.3` | No | Minimum SSL/TLS version. |
| 31 | `event-poll` | | Enum | Platform-dependent | `epoll`, `kqueue`, `port`, `poll`, `select` | No | Event polling backend. |
| 32 | `piece-length` | | Size | `1M` | Size suffix | No | Piece length for HTTP/FTP downloads. Ignored for BT and Metalink with piece hashes. |
| 33 | `socket-recv-buffer-size` | | Size | `0` | Bytes; `0` disables. | No | Maximum socket receive buffer (`SO_RCVBUF`). |
| 34 | `rlimit-nofile` | | Integer | — | Positive integer; only increases soft limit (POSIX only). | No | Set soft limit of open file descriptors. |
| 35 | `deferred-input` | | Boolean | `false` | `true`/`false` | No | Read `--input-file` one line at a time rather than all at startup. Disabled when `--save-session` is used. |
| 36 | `max-download-result` | | Integer | `1000` | Non-negative integer; `0` = keep none. | No | Maximum number of completed/error/removed download results kept in memory (FIFO). |
| 37 | `keep-unfinished-download-result` | | Boolean | `true` | `true`/`false` | No | Keep unfinished download results even beyond `--max-download-result` limit. |
| 38 | `enable-color` | | Boolean | `true` | `true`/`false` | No | Enable color output on terminal. |
| 39 | `on-download-start` | | String | — | Path to executable. | No | Command executed after download starts. |
| 40 | `on-download-pause` | | String | — | Path to executable. | No | Command executed after download is paused. |
| 41 | `on-download-stop` | | String | — | Path to executable. | No | Command executed after download stops (any reason). Overridden by `--on-download-complete`/`--on-download-error` if set. |
| 42 | `on-download-complete` | | String | — | Path to executable. | No | Command executed after download completes successfully. |
| 43 | `on-download-error` | | String | — | Path to executable. | No | Command executed after download fails with error. |
| 44 | `on-bt-download-complete` | | String | — | Path to executable. | No | Command executed after BT download completes but before seeding begins. |

## Environment Variables

aria2 recognizes the following environment variables. They override configuration file values but are overridden by CLI options.

| Variable | Maps to Option | Grammar |
|----------|---------------|---------|
| `http_proxy` | `--http-proxy` | `[http://][USER:PASSWORD@]HOST[:PORT]` |
| `https_proxy` | `--https-proxy` | `[http://][USER:PASSWORD@]HOST[:PORT]` |
| `ftp_proxy` | `--ftp-proxy` | `[http://][USER:PASSWORD@]HOST[:PORT]` |
| `all_proxy` | `--all-proxy` | `[http://][USER:PASSWORD@]HOST[:PORT]` |
| `no_proxy` | `--no-proxy` | Comma-separated host/domain/CIDR list |

## Configuration File Syntax

The config file (`aria2.conf`) uses a flat `KEY=VALUE` format, one per line. The key is the long option name without `--` prefix. Lines starting with `#` are comments. `$HOME` is expanded in file-path options (even when used on the CLI).

## Proxy URI Embedded Credentials

For all proxy options (`--http-proxy`, `--https-proxy`, `--ftp-proxy`, `--all-proxy`), credentials embedded in the URI interact with `--*-proxy-user`/`--*-proxy-passwd` as follows: later-appearing options override earlier ones. CLI overrides config, and explicit user/passwd options override embedded credentials if specified later in the argument order.

## Control File

A `.aria2` file is saved alongside the download to track progress. It is deleted on successful completion. Its absence prevents resume (unless `--check-integrity -V` is used with piece hashes available). The `--auto-save-interval` option controls periodic writes.

## Event Hook Interface

Hook commands (`--on-download-start`, `--on-download-complete`, etc.) receive 3 arguments: the GID (hex string), the number of files (integer string), and the file path of the first file. For BT downloads, the number of files reflects the torrent file count.

## Option Count Summary

| Category | Count |
|----------|-------|
| Basic | 28 |
| HTTP | 24 |
| FTP/SFTP | 11 |
| HTTP/FTP/SFTP Shared | 20 |
| Checksum | 2 |
| BitTorrent | 45 |
| Metalink | 9 |
| RPC | 14 |
| Advanced | 44 |
| **Total** | **~197** rows (individual option entries) |
