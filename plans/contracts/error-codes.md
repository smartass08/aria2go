# Error Codes Contract

**Source:** aria2 1.37.0 `src/error_code.h`, `src/main.cc`
**Schema:** aria2go exit codes in Go must numerically match the values below.
**Status:** Spec-authored from clean-room reading of aria2 source; zero aria2 LOC.

## Process Exit

The aria2c process exit code is the `error_code::Value` returned from `main()`.
The Go rewrite (cmd/aria2go) must `os.Exit(n)` where `n` is the integer shown in the table below.

## Code Table

Code values 0..32 are used as process exit codes. The sentinel value -1 (`UNDEFINED`) is internal only.

| Code | Symbolic Name | Description | Example Trigger |
|------|---------------|-------------|-----------------|
| -1 | UNDEFINED | Sentinel: no error code assigned yet. Never returned as an exit code. | Internal initialization state |
| 0 | FINISHED | All downloads completed successfully. | aria2c terminates after all downloads finish without error |
| 1 | UNKNOWN_ERROR | Catch-all for errors not covered by a specific code. | An unexpected runtime failure in internal logic |
| 2 | TIME_OUT | A connection or request timed out. | Server did not respond to a request within the configured timeout |
| 3 | RESOURCE_NOT_FOUND | The requested resource (URI) returned 404 or equivalent. | HTTP 404, FTP file not found |
| 4 | MAX_FILE_NOT_FOUND | Too many URIs for a download returned "resource not found". The download is abandoned after exhausting configured retry-on-404 limits. | All mirrors return 404 for a multi-URI download |
| 5 | TOO_SLOW_DOWNLOAD_SPEED | Download speed fell below the configured minimum threshold and stayed there longer than allowed. | `--lowest-speed-limit=10K` and speed drops to 5 KB/s persistently |
| 6 | NETWORK_PROBLEM | A TCP/TLS-level network failure occurred. | Connection refused, connection reset, TLS handshake failed |
| 7 | IN_PROGRESS | There are unfinished downloads remaining when sessionFinal is called. For a CLI invocation, this occurs when downloads were in progress and the session was saved/stopped with unfinished work. | aria2c exits with `--stop` or session save with active downloads |
| 8 | CANNOT_RESUME | The remote server does not support HTTP Range requests, so a partial download cannot be resumed. | Server returns no Accept-Ranges header and the file was already partially downloaded |
| 9 | NOT_ENOUGH_DISK_SPACE | Insufficient free disk space to allocate or write the download file(s). | A 2 GB download on a drive with only 500 MB free |
| 10 | PIECE_LENGTH_CHANGED | The piece length in a BitTorrent .torrent file changed during a multi-file torrent where piece boundary crossing matters. | Torrent metadata is inconsistent with the files already allocated on disk |
| 11 | DUPLICATE_DOWNLOAD | An identical download (same URIs, same options) is already queued or active. | Two aria2.addUri RPC calls with the same URLs and no differentiating options |
| 12 | DUPLICATE_INFO_HASH | A download with the same BitTorrent info hash is already queued or active. | Adding the same magnet link twice |
| 13 | FILE_ALREADY_EXISTS | The output file path already exists and `--allow-overwrite=false`. | `aria2c http://example.com/file.zip` and `file.zip` is already on disk |
| 14 | FILE_RENAMING_FAILED | Renaming a temporary file (`.aria2` suffix) to the final filename failed. | Permission denied on rename, or file locked by another process |
| 15 | FILE_OPEN_ERROR | Could not open the output file for writing. | Permission denied, or path component is not a directory |
| 16 | FILE_CREATE_ERROR | Could not create the output file on disk. | Disk is full, or directory is read-only |
| 17 | FILE_IO_ERROR | A read or write operation on a file failed during a transfer. | Disk I/O error, filesystem corruption, EIO from kernel |
| 18 | DIR_CREATE_ERROR | Could not create a directory needed for the download output path. | Parent directory is read-only, or path component exists as a file |
| 19 | NAME_RESOLVE_ERROR | DNS resolution of a hostname failed. | `getaddrinfo` returns no results, or DNS server is unreachable |
| 20 | METALINK_PARSE_ERROR | The Metalink XML file could not be parsed. | Malformed Metalink XML, missing required elements |
| 21 | FTP_PROTOCOL_ERROR | An FTP command failed or the server returned an unexpected response. | FTP server returned 550, FTP connection lost mid-command |
| 22 | HTTP_PROTOCOL_ERROR | The HTTP response headers or status line were malformed or unexpected. | Server sent garbled HTTP headers, or HTTP/0.9 response with no headers |
| 23 | HTTP_TOO_MANY_REDIRECTS | The HTTP redirect chain exceeded the configured maximum. | `--max-tries` reached due to a redirect loop |
| 24 | HTTP_AUTH_FAILED | HTTP authentication (Basic, Digest, NTLM) was rejected by the server. | 401 response after sending credentials |
| 25 | BENCODE_PARSE_ERROR | Bencoded data (used in .torrent files, tracker responses, DHT messages) could not be parsed. | Malformed bencode in a tracker announce response |
| 26 | BITTORRENT_PARSE_ERROR | The .torrent file is corrupt, missing required keys, or has invalid values. | A .torrent file with a missing or zero-length `info` dictionary |
| 27 | MAGNET_PARSE_ERROR | The magnet URI could not be parsed. | `magnet:?xt=urn:btih:INVALID_HEX` or missing info hash |
| 28 | OPTION_ERROR | A command-line option or config file option has an invalid value, unknown key, or unsupported combination. | `--max-connection-per-server=abc` (non-integer), an unknown key in aria2.conf |
| 29 | HTTP_SERVICE_UNAVAILABLE | The HTTP server returned a 503 response and retry limits are exhausted. | A server returns 503 for every mirror, exceeding retry-wait count |
| 30 | JSON_PARSE_ERROR | JSON-RPC request body could not be parsed as valid JSON. | Malformed JSON in an incoming RPC call via XML-RPC (JSON transport) or JSON-RPC endpoint |
| 31 | REMOVED | The download was explicitly removed by user action (e.g., aria2.remove RPC) before completing. The download did not fail — it was cancelled. | `aria2.remove(gid)` RPC call while download is active |
| 32 | CHECKSUM_ERROR | The SHA-1 or MD5 checksum of a completed download did not match the expected value. | `--checksum=sha-1=abcdef...` and the downloaded file has a different hash |

## Contract: aria2go `*pkg.Error`

Every aria2go package that defines errors must use a typed error with a `Code` field whose value matches one of the numeric values above. The `Code` is the single source of truth for mapping to aria2 RPC error codes and CLI exit codes.

### Non-Exit Codes (Internal Only)

The following codes appear in the `error_code.h` enum but are never returned as process exit codes:
- `-1` (`UNDEFINED`): Sentinel used during initialization. No behavior contract.
- `31` (`REMOVED`): Returned periodically within `RequestGroupMan` to indicate a command associated with a removed download should be dropped. The process exit code 31 is only returned if the user cancelled all downloads.

### When Multiple Downloads Exist

If aria2c has N active downloads, the highest (worst) non-zero error code among them is returned as the exit code. Success (0) is returned only if every download completed with code 0.

### RPC Environment

When aria2 is run through the library API (not CLI), `sessionFinal` returns the exit code as an `int`. In the aria2 RPC protocol, these codes appear as the `errorCode` field in several status responses (`aria2.tellStatus`, download finish events). The RPC integer values are identical to the process exit codes documented above.
