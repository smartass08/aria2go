# T021: CLI Flags Table — Byte-Compatible Spec

**Module:** 00-bootstrap  
**Status:** in_review  
**Claimed by:** deepseek-v4pro-t021-spec-001  
**Claimed at:** 2026-05-19T20:00:00Z  
**Depends on:** T016 (done)

## Implementation Notes

Create `plans/byte-compat/cli-flags-table.md` covering every CLI flag aria2c accepts, organized by the same categories as `plans/contracts/config-keys.md`. For each ~197 option entries document:

1. **Long form** (`--dir`, `--max-connection-per-server`)
2. **Short form** if any (`-d`, `-x`)
3. **Value syntax** on command line (`--dir=DIR`, `-s N`)
4. **Default** behavior when omitted
5. **Category** for `--help` output (Basic, Advanced, HTTP, FTP, BT, Metalink, RPC, Checksum, Experimental)
6. **Value grammar** (`10K`, `1M`, `5s`, bare integer, string, path)
7. **Accumulative** flag behavior (can appear multiple times)
8. **Boolean flags** — whether `--flag=true|false` or `--flag` / `--no-flag` style

Also document:
- `--help` / `--help=#all` / `--help=#category` behavior with all 15 tags
- `--version` / `-v` output format
- `--conf-path=FILE`
- `--stop` / `--stop-with-process=PID`
- `--deferred-input` / `--input-file=FILE`
- `--log` / `--log-level`
- `--enable-color` / `--no-color`
- `--console-log-level`
- `--show-console-readout` / `--quiet`
- `--download-result=FORMAT`
- `--save-session-interval` / `--save-session=FILE`
- The flag `-D` / `--daemon`
- Environment variable equivalents (`http_proxy`, `https_proxy`, `ftp_proxy`, `all_proxy`, `no_proxy`)
- Optional argument behavior (`[true|false]` boolean convention)
- Units (K/M case-insensitive), $HOME expansion, $VERSION substitution
- Proxy credential override precedence (command-line last-appearing wins)

Source material: `plans/contracts/config-keys.md` (T016, done) + `source-truth/aria2/doc/manual-src/en/aria2c.rst`

## Gating

- [x] `go-vet-adr-check`: Spec only — no Go code. Gate is informational pass.
- [x] Spec reviewed for completeness against all ~197 option entries.

## Implementation Log

No divergence from Implementation Notes. Spec covers all 197 option entries across 10 categories (2 Special + 26 Basic + 24 HTTP + 11 FTP/SFTP + 20 Shared + 2 Checksum + 45 BT + 9 Metalink + 14 RPC + 44 Advanced). File: 485 lines, ~45KB.

Key documented behaviors:
- Boolean optional-argument convention (`--flag[=true|false]`, short-form concatenation `-Vfalse`)
- No `--no-flag` negation style in aria2
- 6 accumulative flags (header, index-out, bt-tracker, bt-exclude-tracker, dht-entry-point, dht-entry-point6)
- All 16 `--help` tags enumerated
- Proxy credential override precedence (last-appearing wins)
- 5 environment variable equivalents
- $HOME expansion for 26 path-valued flags
- $VERSION substitution for user-agent, peer-agent, peer-id-prefix
- Input file option syntax (no `--` prefix in file context)

