# aria2go Feature Matrix

This is the current parity ledger for `aria2go` against aria2 1.37.0. The
machine-readable source of truth is
[`test/conformance/feature_matrix.json`](../test/conformance/feature_matrix.json).

The conformance suite enforces the important rule: a feature may only be marked
`implemented` if it has explicit `test/conformance` coverage. If a feature only
has unit tests, parser coverage, or unexercised code, it must be marked
`partial`, `missing`, or `tests-only`. The matrix also owns the Go option
inventory, RPC method inventory, RPC notification inventory, and the small set
of source-truth prefs that still have no Go option owner.

Current count:

- `implemented`: 30
- `partial`: 45
- `missing`: 1
- `tests-only`: 0

## Status Definitions

| Status | Meaning |
| --- | --- |
| `implemented` | Runtime behavior is wired and covered by conformance tests. The feature name should be narrow enough that this is an honest claim. |
| `partial` | Some meaningful behavior exists, but aria2 behavior is incomplete, not fully wired, or not fully tested. |
| `missing` | The feature is advertised by aria2 and may parse in aria2go, but the runtime behavior is not implemented. |
| `tests-only` | Code or parser pieces exist and may have unit tests, but no runtime feature is wired. |
| `not-applicable` | Deliberately outside this port's scope. |

## Matrix

| ID | Area | Status | Feature |
| --- | --- | --- | --- |
| `bt.local-torrent-download` | BitTorrent | `implemented` | Local .torrent single-file download over TCP peers |
| `bt.local-peer-discovery` | BitTorrent | `partial` | Local Peer Discovery announcements and peer intake |
| `bt.show-files.torrent` | BitTorrent | `implemented` | Torrent --show-files output |
| `bt.select-file-index-out` | BitTorrent | `implemented` | Multi-file torrent --select-file and --index-out |
| `bt.magnet-runtime` | BitTorrent | `partial` | Magnet URI download |
| `bt.pause-metadata-runtime` | BitTorrent | `implemented` | pause-metadata runtime behavior for metadata downloads |
| `bt.ut-metadata` | BitTorrent | `implemented` | BEP 9 / ut_metadata torrent metadata exchange |
| `bt.dht-peer-discovery` | BitTorrent | `partial` | DHT peer discovery feeding BitTorrent swarms |
| `config.dht-entry-point-split-host-port` | Config | `partial` | Internal split dht-entry-point host/port prefs |
| `bt.peer-exchange` | BitTorrent | `partial` | Peer exchange / ut_pex |
| `bt.tracker-http` | BitTorrent | `implemented` | HTTP tracker announce for local .torrent downloads |
| `bt.tracker-udp-runtime` | BitTorrent | `partial` | UDP tracker runtime use |
| `bt.tracker-policy` | BitTorrent | `partial` | Tracker overrides, exclusion, intervals, tiers, completed/stopped announces |
| `bt.webseed-runtime` | BitTorrent | `partial` | Web seed / url-list data source |
| `bt.seeding-runtime` | BitTorrent | `partial` | Post-completion seeding, incoming peer listener, seed-ratio and seed-time |
| `bt.encryption-runtime` | BitTorrent | `partial` | BitTorrent MSE/PE runtime behavior |
| `bt.utp-runtime` | BitTorrent | `partial` | uTP transport |
| `bt.remove-unselected-file` | BitTorrent | `implemented` | bt-remove-unselected-file |
| `download.http-basic` | HTTP | `implemented` | HTTP single-file download |
| `download.https-tls` | HTTP | `partial` | HTTPS download and TLS certificate option behavior |
| `download.http-range-split` | HTTP | `partial` | HTTP range resume and split downloads |
| `download.http-headers-auth` | HTTP | `implemented` | HTTP headers, user-agent, referer, and basic auth |
| `download.http-cookies-gzip-redirect` | HTTP | `implemented` | HTTP cookies, gzip, redirects, status retry, no-proxy edge behavior |
| `download.http-output-routing` | HTTP | `implemented` | HTTP output routing, overwrite, rename, resume, conditional, remote-time and content-disposition |
| `download.remote-time-non-http` | Download | `implemented` | remote-time behavior for FTP and SFTP transfers |
| `download.ftp-passive` | FTP | `implemented` | FTP passive single-file download |
| `download.sftp-password-hostkey` | SFTP | `implemented` | SFTP password authentication and host-key digest checks |
| `download.input-file-basic` | Input | `implemented` | Eager --input-file, stdin input, and per-entry options |
| `download.parameterized-uri` | Input | `implemented` | Parameterized URI expansion |
| `download.input-file-errors` | Input | `partial` | --input-file error behavior |
| `download.deferred-input` | Input | `implemented` | --deferred-input incremental parser |
| `input.metadata-file-classification` | Input | `partial` | Local .torrent and .metalink path classification inside input/session files |
| `download.retry-policy` | Download | `partial` | max-tries, retry-wait, and max-file-not-found across protocols |
| `download.checksum-integrity` | Download | `partial` | --checksum, --check-integrity, realtime chunk checksum, and Metalink hash verification |
| `download.http-pipelining` | HTTP | `missing` | HTTP pipelining |
| `download.lowest-speed-limit` | Download | `partial` | lowest-speed-limit enforcement |
| `download.uri-selector-server-stat` | Download | `partial` | URI selector, reuse-uri, stream-piece-selector, server-stat feedback |
| `download.connection-limits` | Download | `partial` | max-concurrent-downloads and max-connection-per-server |
| `download.file-allocation` | Download | `partial` | File allocation modes |
| `download.rate-limits` | Download | `partial` | Download and upload rate limit options |
| `download.proxy-routing` | Download | `partial` | Proxy option routing and no-proxy bypass behavior |
| `download.network-socket-options` | Network | `partial` | Interface binding, IPv6 disable, DSCP, and socket receive buffer options |
| `net.async-dns-runtime` | Network | `partial` | async-dns, enable-async-dns6, and async-dns-server runtime behavior |
| `download.dry-run` | Download | `partial` | dry-run probe behavior without writing payload files |
| `ftp.advanced-options` | FTP | `partial` | ftp-type, ftp-reuse-connection, proxy-method, and advanced FTP behavior |
| `sftp.keys-agent` | SFTP | `partial` | SFTP private-key formats, encrypted keys, and SSH agent |
| `metalink.single-http-download` | Metalink | `implemented` | CLI --metalink-file HTTP-backed download |
| `metalink.filters-mirrors` | Metalink | `partial` | Metalink filters, base URI, mirror fallback, server count, and unique protocol |
| `metalink.hash-verification` | Metalink | `partial` | Metalink file and piece hash verification |
| `metalink.show-files` | Metalink | `implemented` | Metalink --show-files output |
| `session.basic-save-load` | Session | `partial` | Basic save-session/load-session lifecycle |
| `session.save-option-surface` | Session | `partial` | Complete aria2 option preservation in session files |
| `progress.console-readout` | Progress | `partial` | Console progress and result display controls |
| `progress.summary-interval` | Progress | `partial` | --summary-interval timing |
| `cli.help-version` | CLI | `implemented` | CLI help and version output |
| `cli.config-file-basics` | CLI | `implemented` | conf-path, no-conf, config parsing, CLI precedence |
| `cli.stop-timers` | CLI | `implemented` | --stop and --stop-with-process |
| `engine.lifecycle-options` | Runtime | `partial` | daemon, startup-idle-time, event-poll, and rlimit-nofile lifecycle options |
| `lifecycle.shutdown-session-signals` | Runtime | `partial` | Signal shutdown, force escalation, and save-session on exit |
| `hooks.basic-events` | Hooks | `implemented` | on-download-start/complete/error hook argument behavior |
| `hooks.pause-stop-bt-complete` | Hooks | `implemented` | on-download-pause, on-download-stop, and on-bt-download-complete hooks |
| `rpc.notifications` | RPC | `implemented` | RPC notification name registry and WebSocket delivery |
| `rpc.method-registry` | RPC | `implemented` | Advertised RPC method registry |
| `rpc.add-uri-status-queue` | RPC | `partial` | addUri, tellStatus, tellActive/tellWaiting/tellStopped, queue controls |
| `rpc.files-uris-servers` | RPC | `partial` | getFiles, getUris, getServers for non-BT/HTTP active downloads |
| `rpc.uploaded-metadata` | RPC | `implemented` | addTorrent/addMetalink uploaded metadata validation and save behavior |
| `rpc.add-metalink-multi-gid` | RPC | `implemented` | aria2.addMetalink multi-file enqueue and multi-GID return semantics |
| `rpc.auth-readonly` | RPC | `implemented` | RPC secret auth and read-only method gating |
| `rpc.secure-transport` | RPC | `partial` | RPC TLS transport using rpc-secure, rpc-certificate, and rpc-private-key |
| `rpc.transports` | RPC | `partial` | JSON-RPC HTTP POST/GET JSONP, XML-RPC, batch, multicall, WebSocket notifications |
| `rpc.batch-error-shapes` | RPC | `implemented` | JSON-RPC and WebSocket batch per-entry error behavior |
| `rpc.stopped-result-fidelity` | RPC | `partial` | Stopped-result file, URI, bitfield, and status fidelity |
| `rpc.parameter-validation-edges` | RPC | `partial` | RPC edge-case parameter validation |
| `rpc.option-validation` | RPC | `partial` | RPC option validation parity |
| `rpc.change-global-runtime-effects` | RPC | `partial` | changeGlobalOption runtime side effects |
| `rpc.get-peers` | RPC | `partial` | aria2.getPeers with active BitTorrent peer details |

## How To Update This

When a feature is fixed, update `feature_matrix.json` in the same change:

1. Confirm the behavior against `source-truth/aria2/src`.
2. Add or expand a conformance test that runs against both `aria2c` and
   `aria2go`.
3. Move the feature to `implemented` only when the conformance test proves the
   runtime behavior, not just parser or unit behavior.

The guard test is
`TestFeatureMatrixClaimsAreCovered` and the inventory coverage tests in
[`test/conformance/feature_matrix_test.go`](../test/conformance/feature_matrix_test.go).
