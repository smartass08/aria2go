# aria2go — Master Plan

A 100% feature-clone, pure-Go, Apache-2.0 clean-room rewrite of aria2 (the C++ download utility, https://github.com/aria2/aria2), targeting Go 1.24+ in May 2026.

This single file is the *master* plan (plan-mode constraint: only this file is editable). After your approval via ExitPlanMode, this content is decomposed into the 3-tier hierarchical artifact tree under `aria2go/plans/` described in §14 — those sub-files become the day-to-day work surface for downstream coding agents.

---

## 0. Context — why this plan exists

You asked for a complete rewrite of aria2 / aria2c in modern Go that:
1. Is a 100% feature clone (not a port; not "aria2-inspired").
2. Uses 2026 Go idioms — generics, range-over-func, weak.Pointer, unique.Handle, slog, context, sync.Pool, atomic.Pointer, fuzzing, testing/synctest — and avoids the C++ style baggage of aria2.
3. Can be implemented by many AI coding agents (deepseek-coder, qwen-coder, gpt-5-mini, claude-haiku) with small context windows (16K–200K) and rate limits, so the plan must be structured as small, self-contained, file-tracked work units rather than one big document.
4. Is **strictly stdlib-only** as your library policy. (See §2.1 for the cost data surfaced by research and a fully-specified path-(b) alternative if you choose to revise this at review time.)
5. Is byte-compatible on all four of: RPC API, CLI flags & exit codes, `aria2.conf` config, `aria2.session` session file.
6. Reviews major decisions through codex subagents — Codex's verdicts are recorded inline (§2 ADRs and §13 risks).

The aria2go project workspace is `/Users/smartass08/projects/aria2go/`. It is empty (no git, no files). This plan is greenfield.

Confirmed answers from you on 2026-05-18:
- Library policy: **Strict pure (stdlib only)**.
- Compat targets: **RPC API + CLI flags & exit codes + Config file (aria2.conf) + Session file (aria2.session)** — all four.
- License: **Apache-2.0** (clean-room rewrite; reading aria2 C++ for reference is OK, copying is not).
- Plan layout: **3-tier hierarchical** (master plan + per-module SPEC + per-ticket files; manifest.json; each artifact ≤10K tokens, target ≤4K).

This master plan integrates: research findings on aria2 features and Go stdlib (§§A–C appendices), an architecture design (§§3–11), a test/conformance strategy (§12), a 3-tier planning playbook (§§14–17), and Codex's independent review (§§2.D and §13).

---

## 1. Scope and goals

### 1.1 In scope (functional)
- Protocols: HTTP/1.0/1.1/2 (TLS 1.3), FTP/FTPS (active+passive), SFTP (SSH2), BitTorrent v1, Metalink v3 + v4.
- BitTorrent BEPs (parity with aria2 1.37): BEP 3, 5, 6, 9, 10, 11, 12, 14 (LPD), 15 (UDP tracker), 19 (HTTP/FTP webseed), 20 (peer id), 21 (extension), 23 (compact peers), 27 (private torrents), 29 (uTP — gated behind `--enable-utp`), 32, 33 (DHT), 41, 47, plus BEP 52 (BTv2 hybrid) at MVP+1.
- RPC: JSON-RPC 2.0, XML-RPC, WebSocket. 35 methods + 6 notifications. Auth via `--rpc-secret` token (first positional param), basic auth fallback, HTTPS optional.
- CLI: ~140 flags with short forms, aliases, defaults, value grammar (`10K`, `1M`, `5s`), and `--help=#all` category output.
- Config: `aria2.conf` (key=value, `#` comments, `include` directive), with documented precedence: CLI > input-file > env > config > defaults.
- Session: aria2.session text format (line-oriented, optional gzip, atomic write).
- Files: cookies (Netscape format), `.netrc`, multi-source segmented downloads (`-x`, `-s`, `-k`), file allocation modes (none / prealloc / falloc / trunc), disk cache (`--disk-cache`), piece selectors (default / inorder / random / geom).
- Hooks: `--on-download-{start,pause,complete,stop,error,bt-complete}` with documented env vars.
- Async DNS via `net.Resolver` + context (`c-ares` equivalent is unnecessary).
- Proxies: HTTP CONNECT, HTTPS, SOCKS5 (aria2 supports these; SOCKS4 deferred).
- IPv6 dual-stack everywhere. UPnP IGD + NAT-PMP + PCP port mapping.
- Checksums: MD5, SHA-1, SHA-224, SHA-256, SHA-384, SHA-512 (BLAKE2/3 deferred — aria2 supports via mhash but they're rare).
- Crypto: TLS 1.2/1.3, BT MSE/PE (DH RC4), BT BEP 8 negotiation.

### 1.2 Out of scope (explicit non-goals)
- Metalink GPG signature verification — stdlib has no OpenPGP, and shipping our own is out of scope. Waiver documented in `plans/CONFORMANCE.md`. (Aria2 also accepts this asymmetry depending on build flags.)
- Public Suffix List in cookie domain matching — aria2 does not enforce PSL, so we match aria2.
- BT BLAKE2/BLAKE3/SHA3-as-piece-hash for unusual torrents — Metalink only, BEP 30 deferred.
- HTTP/3 / QUIC — aria2 does not implement it; we don't either.
- libsqlite cookie database parsing (Firefox/Chrome). Aria2 supports it via libsqlite3; we support Netscape text format only. Documented in CHANGELOG; if you want sqlite parsing later it's a feature-add not a parity item.
- Solaris / Haiku / 32-bit. Aria2 supports more platforms; we support amd64+arm64 on Linux/macOS/Windows/FreeBSD/OpenBSD.

### 1.3 Non-functional targets
- Single static binary per OS+arch (no cgo).
- HTTP throughput within ±10% of aria2c on loopback 1GB.
- BT seed throughput within ±15% of aria2c on a 50-peer rig.
- Memory ≤1.5× aria2c at 10K active peers.
- 24h soak with zero panics, zero races, zero goroutine leaks (`runtime.NumGoroutine` stable ±2).
- All `Fuzz*` targets clean for ≥24 CPU-hours.

### 1.4 Project size estimates (strict stdlib, path a)
- ~46,000 LOC across ~40 packages.
- ~250–400 tickets across 14 modules.
- 30 hand-written exemplar tickets (Week 0), the rest generated by an LLM expander against per-module SPECs and human-reviewed (Week 1).

---

## 2. Decisions register (ADRs)

ADRs live under `plans/decisions/ADR-NNNN-<slug>.md` post-approval. Inline summaries follow. Status convention: **Accepted** = baseline decision; **Proposed** = open for your review during plan approval; **Open** = needs Codex / human follow-up.

### ADR-0001 — Library policy (BASELINE: strict stdlib-only; ALTERNATIVE provided)

**Status:** Accepted — strict stdlib-only (path a) per your decision on 2026-05-18.
**Alternative ADR-0001b (Proposed):** Curated `golang.org/x/*` shortlist. Switching to this is a single-ADR change; the plan documents both deltas so the swap is mechanical.

**Decision (path a, baseline):** Use only Go standard library packages. No third-party imports; no `golang.org/x/*`. SSH is written from scratch under `internal/ssh/`. Syscalls go through `syscall` package directly. IDN/punycode is hand-rolled. Terminal raw mode uses syscall-level termios on Unix and SetConsoleMode on Windows.

**Cost surfaced by research:**
- `internal/ssh/` adds ~5,500 LOC and ~25 tickets (KEX curve25519-sha256 + dh-group14, userauth password+publickey RSA+ed25519, transport, channels, ciphers aes128-ctr/aes256-ctr/chacha20-poly1305, hmac-sha2-256, known_hosts parser). Estimated ~10 weeks single-engineer effort or ~3 weeks across parallel agents.
- `internal/netx/idn.go` becomes ~150 LOC of RFC 3492 punycode.
- `internal/console/readline.go` becomes ~400 LOC of termios via syscall + Windows console-mode equivalent.
- `internal/platform/` adds ~400 LOC handwriting per-OS syscall constants and `syscall.Syscall6` for fallocate (subtle bug risk on minor OS versions).

**Codex review (path a vs b) — verdict on b: SOUND**, with one caveat: "Treating `golang.org/x/*` as 'effectively stdlib' is directionally reasonable but not identical to depending only on the Go standard library… SSH dependency also becomes a major compatibility surface, because aria2 behavior may include SFTP edge cases, host-key handling, auth ordering, proxy interaction, and error messages that `x/crypto/ssh` does not match naturally." **Concrete change to consider (if path b adopted): freeze exact allowed package list and versions; define SFTP/SSH behavior compatibility tests before any ticket assumes `x/crypto/ssh`.**

**Path (b) alternative shortlist (if you flip the decision):**
- `golang.org/x/sys` (windows + unix subpackages) — for fallocate, mmap, ifname binding, raw socket setup.
- `golang.org/x/crypto/ssh` + `golang.org/x/crypto/ssh/agent` — for SFTP.
- `golang.org/x/term` — for raw-mode interactive console.
- `golang.org/x/net/idna` — for IDN/punycode.

All others (`x/sync`, `x/time`, `x/net/*` beyond idna, `x/text`, `x/exp/*`, etc.) remain **forbidden** under both paths. Path (b) saves ~6,500 LOC and ~30 tickets; path (a) saves zero external dependencies and a license-attribution NOTICE file. Both are Apache-2.0 compatible (x/* is BSD-3-Clause, which Apache absorbs).

**My recommendation (for your review):** path (b). It honors the spirit of your "pure rewrite — not ancient crap" framing while not re-implementing battle-tested cryptography. But path (a) is the baseline in this plan because that is what you literally chose. To switch at review time, edit ADR-0001 status and update the SPEC for module 05-protocol-sftp and the platform package; nothing else changes.

### ADR-0002 — Concurrency model

**Status:** Accepted. **Codex verdict: RISKY** — validate with scalability milestone.

**Decision:** Per-connection goroutines (one reader + one writer per BT peer; segment workers for HTTP/FTP). One scheduler goroutine consuming a `notify` channel. No central event loop; Go's netpoller already does that. Context cancellation hierarchy: `Daemon → Engine → {Scheduler, RPC, Portmap}` and per-RequestGroup `Engine → RequestGroup → Source → per-Conn ctx`. Bandwidth throttling via our own token bucket. Buffer pools via `sync.Pool` (`Pool4K`, `Pool16K`, `Pool64K`).

**At 10K peers ≈ 20K goroutines.** Within Go's runtime envelope (2KB initial stacks, ~160MB stack memory budget). Reader goroutines are blocked on `read()`; their stacks stay small. Peer registries are sharded per-torrent (no global peer map). DNS uses singleflight cache.

**Codex's strongest counter:** "20K goroutines is acceptable only under the assumption that most peers are I/O-blocked and buffers/timers are tightly controlled. Per-connection read/write goroutines can inflate stack, timer, channel, and buffer pressure under BitTorrent peer churn and slow peers. A hand-rolled token bucket is deceptively hard if it must match aria2's global, per-download, and per-server throttling semantics under bursty I/O."

**Mitigation applied to the plan:** A **Scalability Validation Milestone** is added before any protocol-implementation ticket assumes the concurrency model (see §21 Phase 1.5). It includes: synthetic 10K-peer test, GC pause budget (≤100ms p99), RSS budget (≤1 GiB), and throttling accuracy tests against captured aria2c per-second byte counts.

### ADR-0003 — RPC framework

**Status:** Accepted. **Codex verdict: RISKY** — golden-test-first.

**Decision:** Hand-write JSON-RPC 2.0, XML-RPC, and WebSocket (RFC 6455) server transports on top of `net/http`. Share a transport-neutral dispatcher with method registry. Auth: `--rpc-secret` as first positional param ("token:<value>"), constant-time compare. Basic auth as fallback. HTTPS via `tlsx.ServerConfig`.

**Codex's strongest counter:** "JSON-RPC 2.0 is manageable; XML-RPC has awkward type, fault, encoding, datetime conventions; WebSocket has masking, fragmentation, close codes, ping/pong, origin/header behavior, and proxy interactions that can fail subtly. Auth as first positional param is necessary for aria2 compat, but the dispatcher must preserve method-specific parameter quirks rather than normalize too aggressively."

**Mitigation:** Treat RPC as **golden-test-first**: before writing the JSON-RPC server, capture aria2c's response for every method (success, error, auth failure, batch, XML serialization, WebSocket framing). Goldens become the test fixture set; tickets reference them by path. See `plans/test-plans/rpc-goldens.md` post-approval.

### ADR-0004 — BT engine boundary

**Status:** Accepted with one revision per Codex. **Codex verdict: RISKY**.

**Original decision:** BT lives entirely in `internal/protocol/bittorrent/*`, exposing only `engine.Source`. Engine has zero knowledge of pieces/peers/trackers. Cross-cutting events flow through `engine.Bus`.

**Codex's strongest counter:** "Zero knowledge may be too strict. BitTorrent affects file selection, piece completion, metadata arrival, seeding state, peer counts, tracker status, DHT state, speed accounting, and user-visible RPC fields. If all cross-cutting behavior goes through a generic bus, the system can drift into implicit contracts harder to reason about than a small explicit BT-facing capability interface."

**Revised decision:** Engine remains BT-agnostic at compile time, but defines four explicit typed interfaces in `internal/contracts/`:
- `TorrentStatusProjector` — produces the BT-specific subset of `tellStatus` (bitfield, numSeeders, numPieces, etc.).
- `FilePieceMap` — maps torrent files to piece ranges (used by `getFiles`/`changePosition`).
- `TorrentLifecycleControl` — `Pause()`, `Stop()`, `RehashAll()`, `Verify()`.
- `TorrentRPCProjection` — adapts BT state to RPC fields without leaking BT types into RPC packages.

BT subsystems implement these; engine consumes them. Bus is still used for events but no longer carries cross-cutting state.

### ADR-0005 — Session storage byte-compat

**Status:** Accepted. **Codex verdict: SOUND** with platform-and-format-scope caveats.

**Decision:** Replicate aria2's `aria2.session` line-oriented text format byte-for-byte. Optional gzip detected by magic `0x1f 0x8b`. Atomic write via temp file + `os.Rename`. The exact `\t<key>=<value>` line order matches aria2's `RequestGroupOptionHandlerHolder` iteration order, captured in `plans/byte-compat/session-format.md`.

**Codex's caveat:** Define byte-compat as a testable contract; preserve unknown / unsupported lines; test round-trip across Linux, macOS, and Windows. **Action: testable contract is in §12; cross-OS golden round-trip is a release gate.**

### ADR-0006 — Scheduler model

**Status:** Accepted with spec expansion per Codex. **Codex verdict: SOUND**.

**Decision:** Single scheduler goroutine; per-group state machine (`Waiting → Active → Paused → Complete/Error/Removed`, plus `Seeding` for BT); semaphores enforce `--max-connection-per-server`, `--bt-max-peers`, `--max-concurrent-downloads`.

**Codex's spec-expansion:** scheduler ticket must enumerate invariants for (i) fairness — wake order under contention; (ii) dynamic option changes — `changeGlobalOption` mid-flight; (iii) retry — backoff, `--max-tries`, `--max-file-not-found`; (iv) priority ordering — `--bt-prioritize-piece`, `aria2.changePosition`; (v) resource release on every terminal/paused state. **Action: scheduler SPEC has a dedicated `## Invariants` block enumerating all five; tickets reference them.**

### ADR-0007 — Test strategy

**Status:** Accepted.

**Decision (summary):** Test pyramid: unit → module-integration → conformance (dual-run against pinned `aria2c` Docker image) → end-to-end → benchmarks → stress/fuzz/property. Conformance dual-run is the strategic keystone: invoke `aria2c` reference and `aria2go` on identical input, diff stdout/stderr/exit/disk. Pin `aria2c` to **1.37.0** (Debian package `1:1.37.0-1+b1`). Test rig is allowed third-party Go imports (e.g. `golang.org/x/crypto/ssh` to be a known-good SSH counterparty for SFTP tests); production code is not. Fuzzing covers every parser. testing/synctest for timing-sensitive logic when Go ≥ 1.25. Full details in §12.

### ADR-0008 — x/* allowlist policy

**Status:** Accepted. Path (a): empty allowlist. Path (b): the four packages in ADR-0001. `plans/tools/orchestrator/adr-check` parses `go.mod` and `internal/` imports; any non-allowlisted path fails CI.

### ADR-0009 — Build tags strategy

**Status:** Accepted.

**Decision:** Per-OS files only inside `internal/platform/`. Above that layer, runtime feature detection via `platform.Caps()`. `//go:build` constraints only; no legacy `+build`.

### ADR-0010 — Error policy

**Status:** Accepted.

**Decision:** Errors are explicit `error` values; `core.ErrorCode` (1..32 matching aria2's exit codes) is the *display* code mapped from sentinel errors via `errors.Is`. Wrap with `%w`. No panics in library code; only `main` may call `os.Exit`. `errors.Join` for multi-error completion (e.g., failed multi-tracker tier).

### ADR-0011 — Logging policy

**Status:** Accepted.

**Decision:** `log/slog` is the substrate. A `classic` handler emits aria2-style human lines for compat; a `json` handler for ops. `--log-level` maps to slog levels (`debug`, `info`, `notice`, `warn`, `error`).

### ADR-0012 — Channel vs mutex policy

**Status:** Accepted.

**Decision:** Channels for ownership transfer and lifecycle (events, cancellation, hand-off). Mutexes (RWMutex/atomic) for shared *state* (read 100×/sec → behind a mutex or atomic, not a channel). Peer reader/writer pair is the canonical model: one input channel + one mutex-protected state struct.

### ADR-0013 — Package layout (internal vs pkg)

**Status:** Accepted.

**Decision:** Only `pkg/aria2go` is public, frozen at `Daemon`, `Config`, `Status`. All else under `internal/` for refactor freedom. Public surface justification: aria2 has demonstrated library demand (libaria2).

### ADR-0014 — go.mod versioning

**Status:** Accepted.

**Decision:** Module path `github.com/smartass08/aria2go`. Go directive `1.24`. `toolchain go1.25.x` for deterministic builds. No vendoring. For path (b), pin x/* to exact versions in `go.mod`; refresh via dependabot equivalent.

### ADR-0015 — Versioning scheme

**Status:** Accepted.

**Decision:** CalVer `vYYYY.MM.PATCH` (e.g., `v2026.05.0`). Rationale: this is a feature-clone; SemVer of a clone tracks aria2's behavior changes, not our API. `pkg/aria2go` API stability tracked separately via module path `/v2` when needed.

### ADR-0016 — Clean-room process (HARDENED per Codex)

**Status:** Accepted, hardened 2026-05-18 after Codex flagged contamination risk in the multi-agent workflow.

**Decision:** Implementers may read aria2 C++ source under `source-truth/aria2/` for behavior reference, but must produce code from English specifications under `plans/byte-compat/`. The flow is one-directional: `source-truth/` → English spec in `plans/byte-compat/` → ticket → implementation. Same flow for BEP specs and RFC text.

**Hardened enforcement rules** (CI gate via `plans/tools/orchestrator/adr-check --source-truth`):

1. **Zero aria2 source LOC in implementation artifacts.** No verbatim source quotation in tickets, commits, or generated code. Hard cap: zero.
2. **At most 3 isolated source lines** in private analysis notes (e.g., a spec author's working notes under `plans/byte-compat/_notes/`), and only when the English spec is ambiguous and a literal byte sequence is required (e.g., a session-file separator). Notes containing source quotes are never imported into tickets.
3. **No copied comments, no copied tables, no translated function structures.** "If the resulting prose still reads like a translation of the C++, redo it" is the test.
4. **Author separation for high-risk areas.** The same person (and ideally the same agent) does NOT both author a spec from source AND implement the spec for the same area when the area is high-risk (BT peer wire, RPC dispatcher, session format, config parser). Review log tracks authorship — `plans/tools/orchestrator/adr-check` correlates spec-author git history with ticket-claimer manifest entries.
5. **CI scanner heuristics** (conservative; false positives reviewed by human):
   - GPL-header strings in any file under `internal/`, `pkg/`, `cmd/`.
   - Distinctive aria2 symbols: `DownloadEngine::`, `AbstractCommand::`, `BtRuntime::`, `RpcMethodFactory::`, `OptionHandlerFactory::`, `SessionSerializer::`, `MultiFileAllocationIterator::`, and ~50 more enumerated in `plans/tools/orchestrator/adr-check/aria2-symbols.txt`.
   - Diff-similarity > 30% between any added function in our code and any function in `source-truth/aria2/src/` (token-level, ignoring identifier names) — flag for review.
   - Verbatim aria2 comment text (top-K most distinctive comments fingerprinted).
6. **Violation = revert.** Any rule-1 or rule-3 violation requires the commit to be reverted, the affected area re-implemented from a fresh English spec, and a scanner regression test added.

Two-stage authorship audit: every ticket touching CLI/RPC/session/config records (in its frontmatter) `spec_author` and `implementer`. `plans/tools/orchestrator/adr-check` cross-checks that these are different agent_ids (or different humans) for the high-risk areas listed above.

### ADR-0017 — Memory and goroutine ceilings

**Status:** Accepted.

**Decision:** Documented soft caps and behaviors when hit: 10K peer goroutines (close oldest peer), 256 WS clients (reject new with 503), 1024 in-flight hooks (`ErrHookQueueFull`), etc. Each ceiling is an `--max-*` option (matching aria2 where one exists, ours otherwise).

### ADR-0018 — Endianness and integer width

**Status:** Accepted.

**Decision:** All wire-format math uses fixed-width `binary.BigEndian` / `binary.LittleEndian`. No reliance on `int` width. Targets are 64-bit (amd64, arm64).

### ADR-0019 — uTP (BEP 29) scope

**Status:** Proposed — please confirm.

**Decision (proposed):** uTP is in MVP+1, gated behind `--enable-utp=false` default until cross-validated against `libutp` packet captures. Reason: ~2,500 LOC + LEDBAT congestion control is high-risk for the first MVP, and aria2 itself defaults `--enable-utp` to true but our correctness gate is stricter. Flip to MVP if you prefer day-1 parity.

### ADR-0020 — Reference aria2 version

**Status:** Accepted.

**Decision:** Pin all conformance tests to **aria2 1.37.0** (the last upstream release, late 2024). Document any deviation as a feature-add or waiver. Future minor upstream releases trigger a bump-ADR.

### ADR-0021 — Ticket contract-surface field

**Status:** Accepted (per Codex Decision 7).

**Decision:** Every ticket has a mandatory `## Contract Surface` section listing affected CLI flag(s), RPC method(s), session-file field(s), config option(s), and required fixture file(s). Tickets touching any compat-critical surface trigger a mandatory human review gate before coding begins (status `pending` → cannot go `in_progress` until human approves the contract surface).

### ADR-0022 — SSH/SFTP compatibility tests (path b only; deferred under path a)

**Status:** Proposed (active only under path b).

**Decision:** If path (b) is adopted, before any SFTP implementation ticket starts, write a dedicated compatibility test that exercises: host-key callbacks, password auth, publickey auth (RSA + ed25519), authentication-method ordering, proxy interaction (CONNECT through HTTP proxy), and aria2's specific SFTP error messages. Pin `golang.org/x/crypto/ssh` version exactly in `go.mod`.

### ADR-0023 — Source-truth folder boundary + CI scanner gate

**Status:** Accepted 2026-05-18 (after Codex flagged contamination risk).

**Context:** Cloning aria2 (GPLv2+) source into `source-truth/aria2/` so coding agents can read it offline is useful for behavior reference but increases contamination risk vs. the prior plan where source-reading happened on the web with no local copy. Small-context LLMs especially can accidentally paraphrase source they've read.

**Decision:**
- `source-truth/` lives outside the Go build tree and is **never** imported, embedded, or referenced from `internal/`, `pkg/`, `cmd/aria2go/`, or anything that ships.
- Only `plans/tools/orchestrator/adr-check --source-truth` (the license scanner) reads `source-truth/` programmatically.
- `plans/tools/orchestrator/spec-author` is the only tooling allowed to *generate* content using `source-truth/` as input — its output goes to `plans/byte-compat/*.md`. Agents do not run this; the plan owner does.
- A `.gitignore`-style exclusion list ensures the source-truth tree is not packaged in releases (`make ship-check` verifies absence).
- Source-truth root contains a `README.md` (already authored) declaring license boundaries and the hardened rules from ADR-0016. Every agent is required to read this README during boot (cached for session).
- CI scanner (`adr-check --source-truth`) runs on every PR. Triggers per ADR-0016's enumerated heuristics. Blocking on PR merge.
- Refresh procedure documented in the README. Version bumps require an ADR (e.g., a future ADR-0024-aria2-v1.38-bump).

**Consequences:**

**Positive:**
- Coding agents can grep/read aria2 source offline, removing WebFetch dependency.
- BEP and RFC reference text in one place, versioned with the project.
- License boundary is auditable: one tree, one scanner, one set of rules.

**Negative:**
- ~45 MB on disk; ~1 MB ongoing repo growth per upstream-version-bump (small).
- New contamination vector that didn't exist when source-reading happened on the web — mitigated by ADR-0016 enforcement.
- CI scanner adds ~30 s to every PR run.

**Compliance Notes:**
- All tickets affected (the scanner runs on every PR).
- Module 17-test-fixtures must NOT pull from `source-truth/aria2/test/`; regenerate fresh fixtures (per ADR-0016).
- Detection: `plans/tools/orchestrator/adr-check --source-truth` (CI gate); reviewer spot-check on contract-surface tickets.

---

## 3. Module / Package layout

The literal `aria2go/` tree (path a baseline; path b deltas noted with † on affected packages). Approximate LOC and ticket counts per package.

```
aria2go/
├── go.mod                              # module github.com/smartass08/aria2go; go 1.24
├── go.sum                              # path b only
├── LICENSE                             # Apache-2.0
├── NOTICE                              # path b only — BSD-3 attributions
├── README.md
├── CHANGELOG.md
├── AGENTS.md                           # root agent contract (§18)
├── Makefile                            # build, test, vet, cross-compile, ship-check
├── .github/workflows/                  # CI matrix
│
├── cmd/
│   ├── aria2c/                         # main binary, byte-compat CLI  ~600 LOC,  5 tickets
│   │   ├── main.go
│   │   ├── help.go                     # help text matching aria2 --help=#all
│   │   ├── version.go                  # build-stamp banner
│   │   └── exit.go                     # ErrorCode (0..32) ↔ os.Exit
│   │
│   └── orchestrator/                   # plan tooling, all stdlib-only Go ~1500 LOC, 8 tickets
│       ├── manifest-lint/main.go       # validates manifest.json
│       ├── dag-validate/main.go        # ticket DAG, file DAG, module DAG
│       ├── claim-sweep/main.go         # stale TTL recovery
│       ├── tracking-render/main.go     # writes plans/TRACKING.md
│       ├── conformance-score/main.go   # writes plans/CONFORMANCE.md
│       ├── adr-check/main.go           # enforces ADR-0001 / 0008 / 0021
│       ├── ticket-expand/main.go       # LLM expander harness (Phase 1)
│       └── internal/                   # shared library code
│
├── internal/                           # all implementation packages (closed)
│   │
│   ├── core/                           # domain types, errors        ~900 LOC,  6 tickets
│   ├── config/                         # flags + aria2.conf parser   ~2400 LOC, 14 tickets
│   ├── sessionfile/                    # aria2.session round-trip    ~700 LOC,  4 tickets
│   ├── cookies/                        # Netscape cookies.txt jar    ~500 LOC,  3 tickets
│   ├── netrc/                          # ~/.netrc parser             ~250 LOC,  2 tickets
│   ├── netx/                           # dialer + DNS + proxies      ~1500 LOC, 9 tickets
│   │   ├── idn.go                      # punycode (path a: ~150 LOC; path b: 30 LOC wrapper)
│   ├── tlsx/                           # crypto/tls wrappers         ~600 LOC,  4 tickets
│   ├── hookrunner/                     # --on-download-* hooks       ~400 LOC,  3 tickets
│   ├── console/                        # progress, signals, term     ~800 LOC,  5 tickets  †
│   ├── hash/                           # md5/sha1/256/384/512        ~400 LOC,  3 tickets
│   ├── bencode/                        # BEP 3 codec + raw extract   ~600 LOC,  3 tickets
│   ├── magnet/                         # magnet: URI parser          ~250 LOC,  2 tickets
│   ├── ioutilx/                        # buffer pools, atomic write  ~250 LOC,  2 tickets
│   ├── contracts/                      # cross-module interfaces     ~400 LOC,  3 tickets
│   ├── platform/                       # per-OS syscall glue        ~700 LOC,  5 tickets  †
│   │   ├── fs_linux.go     //go:build linux    # fallocate
│   │   ├── fs_darwin.go    //go:build darwin   # F_PREALLOCATE
│   │   ├── fs_freebsd.go   //go:build freebsd  # posix_fallocate
│   │   ├── fs_openbsd.go   //go:build openbsd  # ftruncate fallback
│   │   ├── fs_windows.go   //go:build windows  # SetFileInformationByHandle
│   │   ├── mmap_unix.go    //go:build unix
│   │   ├── mmap_windows.go //go:build windows
│   │   └── signal_*.go     # per-OS signal mapping
│   │
│   ├── disk/                           # alloc + multi-file + verify ~2200 LOC, 11 tickets
│   ├── engine/                         # the orchestrator            ~2800 LOC, 14 tickets
│   │   ├── engine.go                   # Engine + Run loop
│   │   ├── request_group.go            # one logical download
│   │   ├── request_group_man.go        # collection + persistence
│   │   ├── scheduler.go                # single goroutine, notify chan
│   │   ├── segment.go                  # HTTP/FTP range bookkeeping
│   │   ├── segment_man.go              # split/merge per --split, --min-split-size
│   │   ├── piece_storage.go            # BT piece state + endgame
│   │   ├── stat.go                     # speed EMA, ETA
│   │   ├── ticker.go                   # 1s aggregator + autosave
│   │   ├── ratelimit.go                # token bucket (write our own)
│   │   └── bus.go                      # in-process pub/sub for events
│   │
│   ├── portmap/                        # UPnP + NAT-PMP + PCP        ~1400 LOC, 7 tickets
│   │
│   ├── protocol/
│   │   ├── http/                       # HTTP/1.1 + HTTP/2           ~1800 LOC, 9 tickets
│   │   ├── ftp/                        # FTP + FTPS                  ~1600 LOC, 8 tickets
│   │   ├── sftp/                       # SFTP packets                ~2500 LOC, 8 tickets
│   │   ├── metalink/                   # Metalink v3 + v4            ~900 LOC,  5 tickets
│   │   └── bittorrent/
│   │       ├── session.go              # BT-wide port + mse hash
│   │       ├── torrent/                # .torrent model, v1 + v2     ~700 LOC,  4 tickets
│   │       ├── peer/                   # peer wire + LTEP ext        ~3000 LOC, 15 tickets
│   │       ├── tracker/                # HTTP + UDP + multi          ~1100 LOC, 6 tickets
│   │       ├── dht/                    # mainline DHT (BEP 5)        ~2000 LOC, 10 tickets
│   │       ├── utp/                    # BEP 29 (MVP+1)              ~2500 LOC, 11 tickets
│   │       ├── mse/                    # BEP 8 / PE                  ~900 LOC,  5 tickets
│   │       └── lpd/                    # BEP 14 LPD                  ~250 LOC,  2 tickets
│   │
│   ├── ssh/                            # PATH (a) ONLY  — SSH-2 stack  ~5500 LOC, 25 tickets
│   │   ├── transport/                  # binary protocol, KEX
│   │   ├── kex/                        # curve25519-sha256, dh-group14
│   │   ├── userauth/                   # password, publickey RSA+ed25519
│   │   ├── channel/                    # mux, session + direct-tcpip
│   │   ├── cipher/                     # aes-ctr, chacha20-poly1305
│   │   ├── mac/                        # hmac-sha2-256
│   │   ├── knownhosts/                 # ~/.ssh/known_hosts parser
│   │   └── agent/                      # local agent IPC
│   │
│   ├── rpc/
│   │   ├── jsonrpc/                    # JSON-RPC 2.0 codec          ~600 LOC,  4 tickets
│   │   ├── xmlrpc/                     # XML-RPC codec               ~700 LOC,  4 tickets
│   │   ├── dispatcher/                 # method registry             ~900 LOC,  7 tickets
│   │   ├── transport/                  # HTTP + HTTPS + WS server   ~1300 LOC, 8 tickets
│   │   └── token/                      # secret auth, throttle       ~200 LOC,  1 ticket
│   │
│   └── log/                            # slog setup + classic format ~400 LOC,  3 tickets
│
├── pkg/
│   └── aria2c/                         # PUBLIC frozen API           ~300 LOC,  2 tickets
│       └── aria2c.go                   # Daemon, Config, Status
│
├── plans/                              # all plan artifacts (§14)
│   ├── PLAN.md                         # this file, post-split
│   ├── manifest.json
│   ├── manifest.schema.json
│   ├── manifest.lock
│   ├── TRACKING.md                     # auto-generated
│   ├── CONFORMANCE.md                  # auto-generated
│   ├── GLOSSARY.md
│   ├── AGENTS.md → ../AGENTS.md
│   ├── decisions/ADR-NNNN-*.md
│   ├── contracts/                      # human-readable contracts
│   │   ├── interfaces.md
│   │   ├── error-codes.md
│   │   ├── rpc-methods.md
│   │   ├── config-keys.md
│   │   └── wire-formats.md
│   ├── byte-compat/
│   │   ├── session-format.md
│   │   ├── rpc-method-table.md
│   │   ├── cli-flags-table.md
│   │   ├── dht-file-format.md
│   │   └── conf-file-format.md
│   ├── modules/NN-name/
│   │   ├── SPEC.md
│   │   ├── AGENTS.md
│   │   └── tickets/TXXX-*.md
│   └── test-plans/
│       ├── conformance-matrix.md
│       ├── corpus/
│       ├── fuzz-targets.md
│       ├── interop-aria2c.md
│       ├── rpc-goldens.md
│       ├── perf-bench.md
│       └── scalability-validation.md
│
└── test/
    ├── golden/                         # frozen .torrent, .session, .conf
    ├── e2e/                            # full-binary tests
    ├── compat/                         # dualrun against aria2c
    ├── fuzz/                           # seed corpora
    ├── stress/                         # 10K-peer, 24h soak
    ├── rig/                            # in-process FTP/SFTP/BT/DHT mocks
    └── conformance/                    # method-by-method matrix
```

**Totals (path a, strict stdlib):** ~46,000 LOC across ~250–400 tickets (path b: ~40,500 LOC, ~25 fewer tickets).

---

## 4. Cross-module dependency DAG

Direction is top-down (arrows = "imports"). Acyclic. `platform` and `ioutilx` are leaf utilities; `core` and `log` are near-leaves.

```
cmd/aria2go ──► pkg/aria2go ──► engine ──► protocol/* + disk + sessionfile + config + magnet + hookrunner + portmap + bus
                                  │
                                  ├─► rpc/transport ──► rpc/dispatcher ──► engine (via Subscriber) + jsonrpc + xmlrpc + tlsx + token
                                  └─► console

protocol/http      ──► core, log, hash, netx, tlsx, cookies, disk, hookrunner
protocol/ftp       ──► core, log, netx, tlsx, netrc, disk
protocol/sftp      ──► core, log, netx, disk, internal/ssh (path a) | x/crypto/ssh (path b)
protocol/metalink  ──► core, http, hash, encoding/xml (stdlib)
protocol/bittorrent/peer    ──► torrent, mse, bencode, hash, netx, disk, log, ioutilx, contracts
protocol/bittorrent/dht     ──► bencode, netx, hash, log, ioutilx
protocol/bittorrent/tracker ──► bencode, netx, http, log
protocol/bittorrent/utp     ──► netx, log
protocol/bittorrent/mse     ──► hash (sha1)
protocol/bittorrent/lpd     ──► core, netx
protocol/bittorrent/torrent ──► core, bencode, hash

core               ──► (stdlib only)
ioutilx            ──► (stdlib only)
platform           ──► syscall
log                ──► core, ioutilx, log/slog (stdlib)
hash               ──► crypto/{md5,sha1,sha256,sha512} (stdlib)
bencode            ──► (stdlib only)
magnet             ──► hash, core, net/url, encoding/{hex,base32}
cookies            ──► core, ioutilx, net/http, net/url
netrc              ──► core, bufio
tlsx               ──► core, log, crypto/tls, crypto/x509
netx               ──► core, tlsx, log, platform, net, context (path a: + own idna; path b: + x/net/idna)
hookrunner         ──► core, log, os/exec
console            ──► core, log, os, os/signal (path a: termios via syscall; path b: + x/term)
config             ──► core, netrc
sessionfile        ──► core, config, bencode, compress/gzip
disk               ──► core, log, ioutilx, platform, hash
contracts          ──► core (interfaces only; no impl)
engine             ──► core, log, config, disk, sessionfile, hash, magnet,
                       protocol/*, portmap, hookrunner, contracts
portmap            ──► core, netx, log, net/http, encoding/xml

rpc/jsonrpc        ──► core, encoding/json
rpc/xmlrpc         ──► core, encoding/xml
rpc/dispatcher     ──► core, engine, jsonrpc, xmlrpc, log, contracts
rpc/transport      ──► core, dispatcher, tlsx, token, log, net/http, crypto/sha1, encoding/base64
rpc/token          ──► crypto/subtle
```

**Critical path** for project completion: `core → ioutilx → platform → log → hash → bencode → torrent → peer → engine → rpc/transport → cmd/aria2go`. Module 16-logging (our `log` package) and module 12-disk are deeply load-bearing.

---

## 5. Per-module SPECs (inline summary)

For each module: path, responsibility, exported API signatures, dependencies, LOC, ticket count, invariants, concurrency contract. (Detailed per-module SPECs live at `plans/modules/NN-name/SPEC.md` post-approval; what follows is the summary that fits in this master plan.)

### 5.1 `internal/core` — domain primitives. ~900 LOC, 6 tickets.

```go
type GID uint64
func ParseGID(s string) (GID, error)
func (g GID) String() string

type Status uint8  // Waiting/Paused/Active/Complete/Error/Removed (byte-compat with aria2)
type ErrorCode int // 0..32 mapping aria2's exit codes
type URI string
type InfoHashV1 [20]byte
type InfoHashV2 [32]byte
type Event struct { Kind EventKind; GID GID; Time time.Time; Extra any }
type EventKind uint8 // EvStart, EvPause, EvStop, EvComplete, EvError, EvBTComplete
```

**Invariants:** no mutable globals; GID monotonic per process unless restored from session. **Concurrency:** value types; safe to copy across goroutines.

### 5.2 `internal/config` — flags + aria2.conf. ~2400 LOC, 14 tickets.

```go
type Options struct { /* ~140 typed fields */ }
func Default() *Options
func ParseArgs(argv []string) (*Options, []string, error)
func ParseConf(r io.Reader, out *Options) error
func ParseInputFile(r io.Reader, global *Options) ([]InputEntry, error)
func Merge(layers ...*Options) *Options // defaults < conf < env < argv (last wins)
func Validate(o *Options) error
```

**Invariants:** Option registry is single source of truth for help/conf/RPC `getOption`/`changeOption`. Round-trip: any `Options` re-emittable as conf and re-parsed equally. **Concurrency:** immutable after `Validate`; engine takes shallow copies under its lock.

### 5.3 `internal/sessionfile` — byte-compat session round-trip. ~700 LOC, 4 tickets.

```go
type Entry struct { URIs []string; GID core.GID; Status core.Status; OutDir, Out string; Torrent []byte; /* full set */ }
func Read(r io.Reader) ([]Entry, error)
func Write(w io.Writer, entries []Entry) error
func AtomicSave(path string, entries []Entry, opt SaveOpt) error
type SaveOpt struct{ Gzip bool; Mode os.FileMode }
```

**Invariants:** `Write(Read(b))` byte-identical to `b` for every fixture in `test/golden/sessions/`. **Concurrency:** single-writer; engine.ticker serializes.

### 5.4 `internal/cookies` — Netscape jar. ~500 LOC, 3 tickets.

```go
type Jar struct{ /* RWMutex-protected */ }
func New() *Jar
func (j *Jar) LoadNetscape(r io.Reader) error
func (j *Jar) SaveNetscape(w io.Writer) error
// satisfies http.CookieJar
func (j *Jar) Cookies(u *url.URL) []*http.Cookie
func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie)
```

**Note:** no Public Suffix List (matches aria2). **Invariants:** round-trip stable; sort by (domain, path, name) on save.

### 5.5 `internal/netrc` — `~/.netrc` parser. ~250 LOC, 2 tickets.

```go
type Entry struct{ Machine, Login, Password, Account string }
func Parse(r io.Reader) (map[string]Entry, error)
func LoadDefault() (map[string]Entry, error)
```

### 5.6 `internal/netx` — dialing + DNS + proxies. ~1500 LOC, 9 tickets.

```go
type DialerConfig struct { Timeout, KeepAlive time.Duration; Interface, LocalAddr string;
    PreferIPv4, PreferIPv6 bool; ProxyURL, ProxyUser, ProxyPass string }
type Dialer struct{ /* immutable after New */ }
func NewDialer(cfg DialerConfig) (*Dialer, error)
func (d *Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error)
func (d *Dialer) DialUDP(ctx context.Context, addr string) (*net.UDPConn, error)

type Resolver struct{ /* singleflight + TTL cache */ }
func (r *Resolver) LookupHost(ctx context.Context, host string) ([]string, error)

func ToASCII(host string) (string, error)  // IDN punycode
```

**Invariants:** every dial respects ctx; no goroutines started by DialContext outlive the returned conn.

### 5.7 `internal/tlsx` — TLS wrappers. ~600 LOC, 4 tickets.

```go
func ClientConfig(o ClientOpts) (*tls.Config, error)
func ServerConfig(o ServerOpts) (*tls.Config, error)
// ClientOpts: CACerts, ClientCert, ClientKey, SkipVerify, Pinned []byte, ALPNProtocols []string
// ServerOpts: CertFile, KeyFile, ClientAuth tls.ClientAuthType
```

### 5.8 `internal/hookrunner` — `--on-download-*`. ~400 LOC, 3 tickets.

```go
type Runner struct{ /* bounded worker pool */ }
func NewRunner(maxConcurrent int) *Runner
func (r *Runner) Run(ctx context.Context, exec string, env map[string]string, args []string) error
func (r *Runner) Close() error
```

Errors: `ErrHookQueueFull` (queue overflow), `ErrHookTimeout`, `ErrHookNonZeroExit`.

### 5.9 `internal/console` — progress, signals, raw mode. ~800 LOC, 5 tickets.

```go
type Console struct{ /* ... */ }
func New(opts Options) (*Console, error)
type Options struct{ NoColor, Quiet, Summary, Interactive bool }
func (c *Console) Render(snapshot []DownloadStat)
func (c *Console) Println(s string)
func (c *Console) RunInteractive(ctx context.Context, dispatch func(cmd string) string) error
func (c *Console) Signals(ctx context.Context) <-chan os.Signal
```

**Path (a) cost:** termios via syscall on Unix + SetConsoleMode on Windows = ~400 LOC. **Path (b):** `golang.org/x/term` wrapper, ~100 LOC.

### 5.10 `internal/hash` — checksum API. ~400 LOC, 3 tickets.

```go
type Kind string  // "md5","sha-1","sha-224","sha-256","sha-384","sha-512"
func New(k Kind) (hash.Hash, error)
func Parse(s string) (Kind, error)  // accepts "sha-1", "SHA1", "sha1"
```

### 5.11 `internal/bencode` — BEP 3 codec. ~600 LOC, 3 tickets.

```go
func Marshal(v any) ([]byte, error)
func Unmarshal(data []byte, v any) error
type Decoder struct{ /* ... */ }
func NewDecoder(r io.Reader) *Decoder
func (d *Decoder) Decode(v any) error
// CRITICAL for BT infohash correctness:
func ExtractRaw(data []byte, path ...string) (start, end int, err error)
```

**Invariants:** for every fixture, `Marshal(Unmarshal(x)) == x`. The `info` dict bytes are preserved via `ExtractRaw` — we *never* re-encode the info dict to compute infohash.

### 5.12 `internal/magnet` — magnet URI parser. ~250 LOC, 2 tickets.

```go
type Magnet struct {
    InfoHashV1 *core.InfoHashV1
    InfoHashV2 *core.InfoHashV2
    Length     int64       // xl=
    DisplayName string     // dn=
    Trackers    []string   // tr=
    Peers       []string   // x.pe=
    AcceptableSources []string // as=
    ExactSource       []string // xs=
}
func Parse(s string) (*Magnet, error)
func (m *Magnet) String() string
```

### 5.13 `internal/disk` — file I/O. ~2200 LOC, 11 tickets.

```go
type Adaptor interface {
    OpenForWrite() error
    WriteAt(p []byte, offset int64) (int, error)
    ReadAt(p []byte, offset int64) (int, error)
    Size() int64
    Sync() error
    Close() error
    // BT-aware piece helpers
    SetPieceCount(n int)
    MarkPiece(i int, ok bool)
    Have(i int) bool
    Bitfield() []byte
    Missing() []int
}
func NewSingleFile(path string, size int64, alloc Allocator) (*SingleFile, error)
func NewMultiFile(dir string, files []FileEntry, pieceLen int64, alloc Allocator) (*MultiFile, error)
type Allocator interface { Allocate(f *os.File, size int64) error; Name() string }
func AllocatorNone() Allocator
func AllocatorTrunc() Allocator
func AllocatorFalloc() Allocator
func AllocatorPrealloc() Allocator
type Verifier struct{ /* ... */ }
func NewVerifier(a Adaptor, pieceHashes [][]byte, hashKind hash.Kind) *Verifier
func (v *Verifier) Verify(ctx context.Context) ([]int, error)  // returns bad piece indexes
```

**Invariants:** piece marked Have ⇒ data on disk and previously hash-verified. **Concurrency:** WriteAt/ReadAt safe; per-piece locks (16K granularity); Close idempotent.

### 5.14 `internal/engine` — the orchestrator. ~2800 LOC, 14 tickets.

```go
type Engine struct{ /* see internal struct in §7.1 */ }
func New(cfg *config.Options) (*Engine, error)
func (e *Engine) Run(ctx context.Context) error
func (e *Engine) Add(spec AddSpec) (core.GID, error)
func (e *Engine) Pause(gid core.GID, force bool) error
func (e *Engine) Resume(gid core.GID) error
func (e *Engine) Remove(gid core.GID, force bool) error
func (e *Engine) PurgeStopped() int
func (e *Engine) ChangePosition(gid core.GID, pos int, how PosHow) (int, error)
func (e *Engine) TellStatus(gid core.GID, keys []string) (map[string]any, error)
func (e *Engine) TellActive(keys []string) []map[string]any
func (e *Engine) TellWaiting(offset, num int, keys []string) []map[string]any
func (e *Engine) TellStopped(offset, num int, keys []string) []map[string]any
func (e *Engine) GetGlobalStat() GlobalStat
func (e *Engine) ChangeOption(gid core.GID, opt *config.Options) error
func (e *Engine) ChangeGlobalOption(opt *config.Options) error
func (e *Engine) SaveSession() error
func (e *Engine) LoadSession(path string) error
func (e *Engine) Shutdown(force bool) error
type Subscriber interface { OnEvent(ev core.Event) }
func (e *Engine) Subscribe(s Subscriber) (unsubscribe func())
```

**Invariants:** exactly one Run goroutine per active RequestGroup; state transitions through transition(); `SaveSession` is idempotent and concurrent calls coalesce. **Boot order:** load session → start RPC → start portmap → start scheduler → start ticker.

### 5.15 `internal/portmap` — UPnP/NAT-PMP/PCP. ~1400 LOC, 7 tickets.

```go
type Mapper struct{ /* ... */ }
func New(cfg Config) (*Mapper, error)
type Config struct{ InternalPort, ExternalPort int; Protocols []string; Lifetime time.Duration; Interface string }
func (m *Mapper) Run(ctx context.Context) error
func (m *Mapper) ExternalAddr() (ip net.IP, port int, ok bool)
```

### 5.16 `internal/protocol/http` — HTTP downloader. ~1800 LOC, 9 tickets.

```go
type Driver struct{ /* one *http.Client per driver */ }
func NewDriver(opts Opts) *Driver
type Opts struct {
    Dialer    *netx.Dialer
    TLS       *tls.Config
    Jar       http.CookieJar
    UA        string
    Headers   http.Header
    Timeout   time.Duration
    MaxRedirs int
    Limit     *engine.Throttle
}
type Job struct { URIs []core.URI; Out disk.Adaptor; TotalSize int64; Auth AuthSpec }
func (d *Driver) Probe(ctx, j Job) (size int64, etag string, accepted bool, err error)
func (d *Driver) Run(ctx, j Job, on Progress) error
```

**Invariants:** all HTTP I/O through one `*http.Client`; segmented downloads request `Accept-Encoding: identity` (gzip+Range is forbidden).

### 5.17 `internal/protocol/ftp` — FTP/FTPS. ~1600 LOC, 8 tickets.

```go
type Driver struct{ /* ... */ }
func Dial(ctx, dialer *netx.Dialer, addr string, opt Opt) (*Conn, error)
type Opt struct{ User, Pass string; TLSMode TLSMode; TLSConfig *tls.Config; Passive bool }
func (c *Conn) Size(ctx, path string) (int64, error)
func (c *Conn) Retrieve(ctx, path string, offset int64) (io.ReadCloser, error)
func (c *Conn) Close() error
```

### 5.18 `internal/protocol/sftp` — SFTP. ~2500 LOC (path a) / ~2500 LOC + 0 internal SSH (path b).

```go
type Driver struct{ /* ... */ }
func NewDriver(opts Opts) *Driver
type Session struct{ /* ... */ }
func (d *Driver) Open(ctx, addr string, auth AuthMethods) (*Session, error)
func (s *Session) Stat(ctx, path string) (Info, error)
func (s *Session) Read(ctx, path string, offset int64) (io.ReadCloser, error)
func (s *Session) Close() error
```

**Path (a):** depends on `internal/ssh`. **Path (b):** depends on `golang.org/x/crypto/ssh`. Either way, SFTP packet handler is ours (the protocols are layered).

### 5.19 `internal/protocol/bittorrent/peer` — peer wire. ~3000 LOC, 15 tickets.

```go
type Conn struct{ /* one reader + one writer goroutine */ }
type Config struct {
    InfoHash    [20]byte
    InfoHashV2  *[32]byte    // BEP 52 hybrid
    LocalPeerID [20]byte
    Reserved    [8]byte      // ext flags
    Pieces      PieceSource
    OnEvent     func(Event)
    Encrypt     mse.Mode     // off, allow, prefer, require
}
type PieceSource interface {
    NumPieces() int
    Have(i int) bool
    Bitfield() []byte
    GetBlock(piece int, off, n int) ([]byte, error)
    SetWanted(b []byte)
}
func Dial(ctx, dialer *netx.Dialer, addr string, cfg Config) (*Conn, error)
func Accept(ctx, c net.Conn, cfg Config) (*Conn, error)
func (c *Conn) Run(ctx context.Context) error  // blocks
func (c *Conn) Choke(bool) ; Interested(bool) ; Request(p,o,n int) error ; Cancel(p,o,n int)
func (c *Conn) HaveAll() error ; Bitfield(b []byte) error
func (c *Conn) Snapshot() Stat ; Close() error
```

**Invariants:** exactly two goroutines (reader, writer) while Run active; block payload buffers from `ioutilx.Pool16K`; keep-alive every 90s of no traffic. **Concurrency:** public methods are safe from any goroutine; they enqueue into writer channel.

### 5.20 `internal/protocol/bittorrent/dht` — mainline DHT. ~2000 LOC, 10 tickets.

```go
type Server struct{ /* ... */ }
type Config struct{ NodeID [20]byte; Addr string; Bootstrap []string; PersistTo string }
func New(cfg Config) (*Server, error)
func (s *Server) Run(ctx context.Context) error
func (s *Server) GetPeers(ctx, infoHash [20]byte) (<-chan net.Addr, error)
func (s *Server) Announce(ctx, infoHash [20]byte, port int) error
func (s *Server) NodeCount() int
```

**Concurrency:** one UDP read goroutine + one query dispatcher + one bucket refresh ticker.

### 5.21 `internal/protocol/bittorrent/tracker` — HTTP+UDP+multi. ~1100 LOC, 6 tickets.

```go
type Tracker interface {
    Announce(ctx, a Announce) (*Response, error)
    Scrape(ctx, hashes [][20]byte) (map[[20]byte]Scrape, error)
    String() string
}
func New(url string, dialer *netx.Dialer, httpClient *http.Driver) (Tracker, error)
type Tiered struct{ /* ... */ }
func NewTiered(tiers [][]string, ...) *Tiered
```

### 5.22 `internal/protocol/bittorrent/utp` — uTP (BEP 29). ~2500 LOC, 11 tickets.

MVP+1 gated. `net.Conn` semantics over UDP with LEDBAT congestion control.

### 5.23 `internal/protocol/bittorrent/mse` — encryption. ~900 LOC, 5 tickets.

```go
type Mode uint8  // Off, Allow, Prefer, Require
type Conn struct{ net.Conn }
func Initiate(c net.Conn, infoHash [20]byte, mode Mode) (*Conn, []byte, error)
func Receive(c net.Conn, infoHashes [][20]byte, mode Mode) (*Conn, [20]byte, []byte, error)
```

**Dependencies:** `crypto/rc4`, `crypto/sha1`, `math/big`, `crypto/rand`.

### 5.24 `internal/protocol/bittorrent/lpd` — BEP 14 LPD. ~250 LOC, 2 tickets.

Multicast announce on `239.192.152.143:6771` (IPv4) / `[ff15::efc0:988f]:6771` (IPv6).

### 5.25 `internal/protocol/bittorrent/torrent` — .torrent + magnet hybrid model. ~700 LOC, 4 tickets.

```go
type MetaInfo struct { /* ... */ }
func Load(b []byte) (*MetaInfo, error)
func InfoHashV1(b []byte) ([20]byte, error)
func InfoHashV2(b []byte) ([32]byte, error)
```

### 5.26 `internal/protocol/metalink` — v3+v4. ~900 LOC, 5 tickets.

```go
type Doc struct { Files []File }
type File struct { Name string; Size int64; URLs []URLEntry; Hashes map[hash.Kind][]byte; Pieces [][]byte; PieceLength int64; Lang, OS, Version string }
type URLEntry struct { URL string; Type string; Priority int; Location string }
func Parse(r io.Reader) (*Doc, error)
func ParseV3(r io.Reader) (*Doc, error)
func ParseV4(r io.Reader) (*Doc, error)
```

Signature verification waived (no OpenPGP in stdlib).

### 5.27 `internal/rpc/jsonrpc` — JSON-RPC codec. ~600 LOC, 4 tickets.

```go
type Request struct { JSONRPC string; ID json.RawMessage; Method string; Params json.RawMessage }
type Response struct { JSONRPC string; ID json.RawMessage; Result any; Error *Error }
type Error struct { Code int; Message string; Data any }
func Decode(data []byte) (single *Request, batch []Request, err error)
func Encode(resp Response) ([]byte, error)
func EncodeBatch(resps []Response) ([]byte, error)
```

### 5.28 `internal/rpc/xmlrpc` — XML-RPC codec. ~700 LOC, 4 tickets.

```go
type Call struct { MethodName string; Params []any }
type Reply struct { Result any; Fault *Fault }
type Fault struct { Code int; String string }
func DecodeCall(r io.Reader) (Call, error)
func EncodeReply(w io.Writer, r Reply) error
```

### 5.29 `internal/rpc/dispatcher` — method registry. ~900 LOC, 7 tickets.

```go
type Dispatcher struct{ /* ... */ }
func New(e *engine.Engine, cfg DispatchCfg) *Dispatcher
type DispatchCfg struct{ Secret string; ReadOnly bool }
func (d *Dispatcher) Call(ctx, token, method string, params []any) (any, error)
type NotifySink interface{ SendNotification(method string, params []any) }
func (d *Dispatcher) SubscribeNotifications(s NotifySink) (cancel func())
func (d *Dispatcher) ListMethods() []string
func (d *Dispatcher) ListNotifications() []string
```

**Invariants:** `ListMethods()` returns exactly aria2's method names — golden-tested. Token verification is constant-time.

### 5.30 `internal/rpc/transport` — HTTP/HTTPS/WS server. ~1300 LOC, 8 tickets.

```go
type Server struct{ /* ... */ }
type Config struct { Listen string; TLS *tls.Config; AllowedOrigins []string; Dispatcher *dispatcher.Dispatcher; Secret string; ListenAll bool }
func New(cfg Config) (*Server, error)
func (s *Server) Run(ctx context.Context) error
```

WebSocket frame structure handwritten: fin, opcode, mask, payload. Ping/pong inline. **Concurrency:** per WS conn: 1 reader goroutine + 1 writer goroutine + bounded outbox channel.

### 5.31 `internal/contracts` — cross-module interfaces. ~400 LOC, 3 tickets.

Per Codex Decision 4: explicit typed surface between engine and BT.

```go
package contracts

// TorrentStatusProjector renders BT-specific tellStatus fields.
type TorrentStatusProjector interface {
    Project(gid core.GID, keys []string) map[string]any
}

// FilePieceMap maps a torrent's files to piece ranges (for changePosition, getFiles).
type FilePieceMap interface {
    Files(gid core.GID) []FileSlice
    PiecesForFile(gid core.GID, idx int) (firstPiece, lastPiece int)
}

// TorrentLifecycleControl is the BT-side handle the engine uses.
type TorrentLifecycleControl interface {
    Pause() error
    Stop(force bool) error
    RehashAll(ctx context.Context) error
    Verify(ctx context.Context) ([]int, error)
}

// TorrentRPCProjection adapts BT state to RPC fields.
type TorrentRPCProjection interface {
    Peers(gid core.GID) []map[string]any
    Servers(gid core.GID) []map[string]any
}
```

### 5.32 `cmd/aria2go` — main binary. ~600 LOC, 5 tickets.

argv → `pkg/aria2go.Daemon`. Print help. Exit code per `core.ErrorCode`.

### 5.33 `pkg/aria2go` — frozen public API. ~300 LOC, 2 tickets.

```go
package aria2c

type Daemon struct{ /* ... */ }
type Config struct{ /* mirrors config.Options subset */ }
type Status int
func New(cfg Config) (*Daemon, error)
func (d *Daemon) Run(ctx context.Context) error
func (d *Daemon) RPCAddr() string
func (d *Daemon) Shutdown(force bool) error
```

### 5.34 `plans/tools/orchestrator/*` — plan tooling. ~1500 LOC, 8 tickets.

Multi-subcommand binary: `manifest-lint`, `dag-validate`, `claim-sweep`, `tracking-render`, `conformance-score`, `adr-check`, `ticket-expand`. Pure-Go stdlib.

---

## 6. Concurrency model (detail)

### 6.1 Goroutine tree per RequestGroup

```
RequestGroup.Run(ctx)                        1 goroutine
├── source.Run(ctx)                          1 goroutine driving the protocol
│   ├── HTTP/FTP/SFTP: segment workers [1..N]  N = clamp(--split, --max-connection-per-server)
│   ├── BT:
│   │   ├── tracker poller                    1 ticker-driven
│   │   ├── DHT peer source                   ephemeral per get_peers
│   │   ├── LPD listener                      shared (engine-level)
│   │   ├── peer dialer                       1 consumer of candidate Addrs
│   │   ├── peer.Conn[i] reader               1 per peer
│   │   ├── peer.Conn[i] writer               1 per peer
│   │   ├── choke algorithm                   1 ticker (10s)
│   │   └── piece picker                      inline on writer goroutine
│   └── Metalink:
│       └── delegates to sub-sources          same shape per file
├── stat aggregator                           engine-ticker-driven (no goroutine)
└── disk verifier                             ephemeral when piece completes
```

For 10K total peers ≈ ~20K goroutines (+~2K bookkeeping). Per goroutine ~2KB stack initially → ~40 MB stack memory. **§21 Phase 1.5 Scalability Validation Milestone verifies these numbers before any protocol-implementation ticket starts.**

### 6.2 Cancellation hierarchy

```
ctx = pkg/aria2go.Daemon.Run(ctx)
  └─ engineCtx
       ├─ schedulerCtx
       ├─ rpcCtx
       ├─ portmapCtx
       └─ groupCtx[gid] (per RequestGroup)
            ├─ source.Run(groupCtx)
            │    └─ connCtx (per conn/peer/segment)
            │         ├─ reader goroutine
            │         └─ writer goroutine
            └─ verifier (one-shot, takes groupCtx)
```

`context.Cause` propagates reasons: `errPause`, `errUserStop`, `errNoSpace`, `errCompleted`, `errTrackerKilled`.

- Pause = `groupCancel()` + record state, leave in scheduler.
- Remove = `groupCancel()` + move to stopped list.
- Shutdown(false) = announce stopped to trackers, wait, then engineCancel.
- Shutdown(true) = engineCancel immediately.

### 6.3 Buffer pools (`internal/ioutilx`)

```go
var (
    Pool4K  = newPool(4 << 10)
    Pool16K = newPool(16 << 10) // BT block size
    Pool64K = newPool(64 << 10) // HTTP body chunks
)
type Buf struct{ B []byte }
func (p *pool) Get() *Buf
func (p *pool) Put(b *Buf)
```

`Buf` wrapper prevents double-put and slice-length-mismatch bugs. Buffers come back via `defer p.Put(b)`.

### 6.4 Bandwidth throttling

Our own token-bucket under `internal/engine/ratelimit.go`:

```go
type Throttle struct {
    rate    atomic.Int64  // bytes/sec, 0 = unlimited
    bucket  atomic.Int64
    refill  int64
    mu      sync.Mutex
    waiters list.List
    ticker  *time.Ticker  // 100ms
}
func New(bytesPerSec int64) *Throttle
func (t *Throttle) SetRate(bytesPerSec int64)
func (t *Throttle) Wait(ctx context.Context, n int) error
```

Two instances: global (`--max-overall-download-limit`), per-group (`--max-download-limit`). 100ms tick is intentional. **Codex's caveat:** matching aria2's per-server semantics is hard — Phase 1.5 includes throttling accuracy tests against captured aria2c byte counts.

### 6.5 Disk I/O coalescing

```go
type pieceBuffer struct {
    piece    int
    received roaringRanges
    blocks   [][]byte
    mu       sync.Mutex
}
```

On full piece: verify hash → `WriteAt` once → return block buffers. HTTP segment writers hold a 256K (configurable) staging buffer; flush contiguously.

### 6.6 10K-peer regression mitigations

- Per-torrent peer-set mutex (no global peer map).
- DNS singleflight + TTL cache.
- TLS session resumption.
- Buffer pool monitoring (counters in `tellStatus`-style stats).
- `GOGC=50` recommended default for streaming workloads.

---

## 7. Engine / Scheduler (detail)

### 7.1 Engine internals

```go
type Engine struct {
    cfg       atomic.Pointer[config.Options]
    cfgMu     sync.Mutex

    rg        map[core.GID]*RequestGroup
    waiting   []*RequestGroup
    active    map[core.GID]*RequestGroup
    stopped   *ringBuffer
    rgMu      sync.RWMutex

    scheduler *scheduler
    ticker    *ticker
    bus       *bus

    httpDriver *http.Driver
    sftpDriver *sftp.Driver
    btSession  *bittorrent.Session
    portmap    *portmap.Mapper

    gidCounter   atomic.Uint64
    sessionPath  string
    sessionMu    sync.Mutex

    rateGlobal   *Throttle
    rateGlobalUp *Throttle

    hooks *hookrunner.Runner
    log   *slog.Logger
}
```

### 7.2 RequestGroup state machine

```
       Add()
         │
         ▼
   ┌──────────┐    Resume/scheduler
   │ Waiting  │ ──────────────────────┐
   └────┬─────┘                       │
        │ Pause()                     ▼
        │                       ┌──────────┐
        ▼                       │  Active  │ ──► source.Run()
   ┌──────────┐                 └────┬─────┘
   │  Paused  │ ◄── Pause() ─────────┤
   └────┬─────┘                      │
        │ Remove()                   │ run returns nil
        ▼                            ▼
   ┌──────────┐                ┌──────────┐
   │ Removed  │                │ Complete │
   └──────────┘                └────┬─────┘
                                    │
                                    ▼ if BT && seed condition unmet
                               ┌──────────┐
                               │ Seeding  │ ──► Complete on done
                               └──────────┘

   Any state → Error on protocol error.
   Transition table encoded in request_group.go:transition().
```

### 7.3 Scheduler invariants (per Codex Decision 6)

The scheduler SPEC has a dedicated `## Invariants` block enumerating:

1. **Fairness:** when N tickets are eligible for the same slot, FIFO by `add_time`.
2. **Dynamic options:** `changeGlobalOption` mid-flight only affects newly-scheduled work; in-flight `RequestGroup`s keep their snapshot until the next state transition.
3. **Retries:** `--max-tries` is per-group; `--max-file-not-found` is per-source. Backoff is exponential capped at `--retry-wait * 16`.
4. **Priority ordering:** `aria2.changePosition(gid, pos, "POS_SET" | "POS_CUR" | "POS_END")` mutates the `waiting` slice index; takes the rg mutex.
5. **Resource release:** every terminal state (Complete, Error, Removed) calls `releaseSlots()` which returns semaphore tokens and decrements active counters.

### 7.4 Segment / piece coordination

- HTTP/FTP: `segment_man.go` owns `[]segment{start, end, written}`. Workers pull "next undone segment" via channel; on partial completion update; on idle subdivide slowest remaining segment.
- BT: `piece_storage.go` owns `pieceState{have, requested, blocks}`. Peer-wire calls `pieceStorage.Reserve(peer, k)` for up to k rarest-first block requests; on completion `pieceStorage.Verify(piece) (bool, []byte)` hashes and accepts or evicts. Endgame triggers at >97% complete.

### 7.5 Auto-save tick

`ticker` fires every `--save-session-interval` (default 0 = off). Snapshots all RequestGroup state → `sessionfile.Write` (atomic) and DHT routing table → `dht.PersistTo`.

---

## 8. Persistence

### 8.1 aria2.session byte-compat

aria2 emits:

```
http://example.com/foo.iso
\tgid=2089b05ecca3d829
\tdir=/var/downloads
\tout=foo.iso
\tsplit=5
\t...
```

We replicate exactly. The `\t<key>=<value>` line order matches aria2's `RequestGroupOptionHandlerHolder` iteration — captured in `plans/byte-compat/session-format.md` and hard-coded in `sessionfile/writer.go`. Gzip detected by `0x1f 0x8b` magic. Write to `<path>.tmp` then `os.Rename` then `os.Chmod` to preserve owner mode.

Test contract: for every fixture in `test/golden/sessions/*.session`, `Read` then `Write` produces byte-identical output. CI gate. **Codex's caveat (preserve unknown lines):** decoder passes through any unknown `\t<key>=<value>` lines unchanged into the `Entry.Unknown` map.

### 8.2 DHT routing table

`--dht-file-path` (default `$XDG_DATA_HOME/aria2/dht.dat`). aria2's binary format. ~20 byte header + N×38 byte node entries. Documented at `plans/byte-compat/dht-file-format.md`.

### 8.3 BtAnnounce state

Per-torrent: announce key + tier shuffles persisted in session entry. Avoids re-shuffle on resume (matters for private trackers that key on session continuity).

---

## 9. RPC stack (detail)

### 9.1 Transport-neutral dispatcher

```go
type call struct {
    token  string
    method string
    args   []any
}
type reply struct {
    result any
    err    *rpcError
}
```

Codec packages handle wire decoding into `call` and wire encoding of `reply`. Dispatcher is transport-agnostic.

### 9.2 Transport

Single `net/http` server. Path routing:
- `POST /jsonrpc` → JSON-RPC 2.0 (single or batch).
- `POST /rpc` or `/` with `Content-Type: text/xml` → XML-RPC.
- `GET /jsonrpc` with `Upgrade: websocket` → homegrown WS upgrade.

HTTPS via `tlsx.ServerConfig`. CORS via `--rpc-allow-origin-all`.

### 9.3 Auth

`--rpc-secret=<token>`: first positional parameter must be `"token:<value>"`. Constant-time compare via `crypto/subtle.ConstantTimeCompare`. Basic auth via `--rpc-user`/`--rpc-passwd` accepted as fallback.

### 9.4 system.multicall + listMethods + listNotifications

Concrete methods in the registry. Multicall iterates calls and aggregates results; errors become `mapErr(err)` entries.

### 9.5 WebSocket notifications

Engine.Bus publishes events. Dispatcher.NotifySink adapts to method names: `aria2.onDownloadStart`, `onDownloadPause`, `onDownloadStop`, `onDownloadComplete`, `onDownloadError`, `onBtDownloadComplete` — params `[{gid}]`.

Each WS session has its own outbox channel (capacity 256). On overflow: drop notification + log warning. We **never** block engine on a slow client.

### 9.6 WebSocket framing (RFC 6455, handwritten)

```go
type frame struct {
    fin    bool
    opcode opcode  // continuation, text, binary, close, ping, pong
    mask   [4]byte
    masked bool
    payload []byte
}
func (c *wsConn) readFrame() (frame, error) // unmask if masked
func (c *wsConn) writeFrame(opcode opcode, payload []byte) error
```

Ping/pong inline. Close frame status code echoed. permessage-deflate skipped (aria2 doesn't use it).

**Codex's caveat (golden-first):** before writing any RPC implementation ticket, capture aria2c's responses for all 35 methods × happy + error + auth-failure paths into `test/golden/rpc/`. The capture script is `plans/tools/orchestrator/internal/rpccapture/`.

### 9.7 RPC method matrix

The 35 aria2 methods + 6 notifications:

| Method | Description |
|---|---|
| aria2.addUri, addTorrent, addMetalink | enqueue downloads |
| aria2.remove, forceRemove | cancel |
| aria2.pause, pauseAll, forcePause, forcePauseAll | pause |
| aria2.unpause, unpauseAll | resume |
| aria2.tellStatus, tellActive, tellWaiting, tellStopped | inspection |
| aria2.getUris, getFiles, getPeers, getServers | inspection |
| aria2.getOption, changeOption, getGlobalOption, changeGlobalOption | options |
| aria2.changePosition, changeUri | queue mutation |
| aria2.purgeDownloadResult, removeDownloadResult | cleanup |
| aria2.getGlobalStat, getVersion, getSessionInfo | global |
| aria2.shutdown, forceShutdown, saveSession | lifecycle |
| system.multicall, system.listMethods, system.listNotifications | system |
| (notifications) onDownloadStart, onDownloadPause, onDownloadStop, onDownloadComplete, onDownloadError, onBtDownloadComplete | events |

All listed in `plans/contracts/rpc-methods.md` with parameter shapes, result fields, error codes.

---

## 10. Stdlib-only gap closure inventory

| Gap | Owning package | LOC (a strict) | LOC (b curated) | Complexity |
|---|---|---|---|---|
| SSH transport/KEX/userauth | `internal/ssh` (a) / `x/crypto/ssh` (b) | ~5500 | ~600 | XL / M |
| SFTP packet handler | `protocol/sftp` | ~2500 | ~2500 | M |
| Bencode codec + raw extract | `bencode` | ~600 | ~600 | S |
| Magnet URI parser | `magnet` | ~250 | ~250 | S |
| Mainline DHT | `bittorrent/dht` | ~2000 | ~2000 | M |
| BT peer wire + LTEP | `bittorrent/peer` | ~3000 | ~3000 | L |
| MSE / PE | `bittorrent/mse` | ~900 | ~900 | M |
| UDP tracker (BEP 15) | `bittorrent/tracker` | ~400 | ~400 | S |
| uTP (BEP 29) | `bittorrent/utp` | ~2500 | ~2500 | L |
| UPnP SSDP + SOAP | `portmap/upnp*` | ~700 | ~700 | M |
| NAT-PMP / PCP | `portmap` | ~700 | ~700 | M |
| Netscape cookie jar | `cookies` | ~500 | ~500 | S |
| .netrc parser | `netrc` | ~250 | ~250 | S |
| SOCKS5 client | `netx/proxy_socks5.go` | ~400 | ~400 | M |
| HTTP CONNECT proxy | `netx/proxy_http.go` | ~150 | ~150 | S |
| WebSocket server (RFC 6455) | `rpc/transport/ws_*` | ~700 | ~700 | M |
| XML-RPC codec | `rpc/xmlrpc` | ~700 | ~700 | M |
| Token-bucket rate limiter | `engine/ratelimit.go` | ~250 | ~250 | S |
| IDN/punycode | `netx/idn.go` | ~150 | 30 (wrapper) | S |
| Terminal raw-mode/readline | `console/readline.go` | ~400 | 100 (wrapper) | S |
| Async signal handling | `console/signals_*.go` | ~100 | ~100 | S |
| fallocate / F_PREALLOCATE / SetEndOfFile | `platform/fs_*.go` | ~250 | ~100 (with x/sys) | M / S |
| Metalink v3 / v4 XML | `metalink` | ~700 | ~700 | M |
| Local Peer Discovery (BEP 14) | `bittorrent/lpd` | ~250 | ~250 | S |
| HTTP digest auth (RFC 7616) | `protocol/http/auth.go` | ~250 | ~250 | S |
| RFC 6266 Content-Disposition | `protocol/http/content_dispo.go` | ~200 | ~200 | S |

**Net deltas a vs b:** +5500 SSH, +120 IDN, +300 termios, +150 platform = **+6,000 LOC, +~28 tickets, +~10 weeks single-eng (or +~3 weeks parallel-agents).**

---

## 11. Cross-platform

### 11.1 Build tags

`//go:build` only (no legacy `+build`). Per-OS files exist only inside `internal/platform/`.

### 11.2 Syscall divergence table

| Operation | Linux | macOS | FreeBSD | OpenBSD | Windows |
|---|---|---|---|---|---|
| Preallocate | `fallocate(fd, 0, off, len)` | `fcntl(F_PREALLOCATE)` then `ftruncate` | `posix_fallocate(2)` | `ftruncate` only | `SetFileInformationByHandle(FileAllocationInfo)` |
| mmap | `mmap` | `mmap` | `mmap` | `mmap` | `CreateFileMapping + MapViewOfFile` |
| File lock | `flock` | `flock` | `flock` | `flock` | `LockFileEx` |
| Interface bind | `SO_BINDTODEVICE` | `IP_BOUND_IF` | `IP_BOUND_IF` | n/a | n/a |
| Reuseport | `SO_REUSEPORT` | `SO_REUSEPORT` | `SO_REUSEPORT_LB` | `SO_REUSEPORT` | not used |
| IPv6 only | `IPV6_V6ONLY` | same | same | same | same |
| getpwuid | yes | yes | yes | yes | n/a (use USERPROFILE) |

**Path (a):** all of these via `syscall` package (manual `Syscall6` for fallocate; risk on minor OS versions).
**Path (b):** `golang.org/x/sys/unix` and `golang.org/x/sys/windows` (much safer).

### 11.3 Feature degradation matrix

| Feature | Linux | macOS | FreeBSD | OpenBSD | Windows |
|---|---|---|---|---|---|
| `--allow-overwrite` | full | full | full | full | full |
| `--file-allocation=falloc` | full | full (slower) | full | trunc fallback (warn) | full |
| `--interface=` | full | name & IP | name & IP | IP only | not supported (warn) |
| Unix-domain RPC socket | yes | yes | yes | yes | not supported (warn at startup) |
| SIGUSR1 ("reload") | yes | yes | yes | yes | mapped to console event |
| FS case sensitivity | sensitive | insensitive (warn on dupes) | sensitive | sensitive | insensitive |

`platform.Caps()` exposes capability flags to higher layers:

```go
type Cap struct {
    Fallocate, MMapAnon, InterfaceBind, UnixSocket, Signals bool
    Pagesize int
}
func Caps() Cap
```

---

## 12. Test & conformance strategy

### 12.1 The "100% feature parity" gate (mechanical, binary)

Every claim is reduced to a binary mechanically-checkable assertion. Ship-ready = every line GREEN.

**CLI parity:** every flag in `aria2c --help=#all` is accepted by aria2go (no "unknown flag" error); every flag with an observable side-effect (e.g. `--dir`, `--out`, `--max-connection-per-server`) is dual-run tested; short forms and aliases identical; 32 exit codes exercised in a table-driven test.

**Config parity:** `aria2.conf` round-trip semantic equivalence; every option documented in aria2 manpage exposed with same value grammar; option precedence (CLI > input-file > env > config > defaults) verified.

**Session parity:** `aria2.session` byte-identical round-trip; cross-write (aria2c→aria2go and back) successful; session with every URI kind (HTTP, HTTPS, FTP, SFTP, magnet, .torrent path, .metalink path) round-trips cleanly.

**RPC parity:** all 35 methods + 6 notifications return same JSON-RPC envelope and field set/types; XML-RPC same structure; WebSocket notifications fire at same lifecycle moments with same payload shape; batch JSON-RPC returns in order.

**Protocol parity:** HTTP/1.1, HTTPS, Basic+Digest auth, cookies, proxies, gzip/deflate, Range, redirects, ETag/If-Match. FTP PASV/EPSV/REST/RETR/TLS. SFTP SSH-2 with RSA/ECDSA/Ed25519, password+pubkey, parallel REQUEST_DATA. BT: BEP-3/5/6/9/10/11/12/15/19/20/23/27/29 (gated)/32/33/47. Metalink v3+v4 (signature waived). DHT: ping/find_node/get_peers/announce_peer, IPv4+IPv6.

### 12.2 Test pyramid

| Layer | Location | Tools | Coverage | Runner |
|---|---|---|---|---|
| Unit | `internal/*` colocated `*_test.go` | `testing`, `testing/quick` | ≥85% line, ≥75% branch | every PR, every OS |
| Module/integration | `internal/<mod>/integration_test.go` `//go:build integration` | `httptest`, in-process listeners | ≥70% cross-package | linux/amd64, every PR |
| Conformance | `test/conformance/<area>/*_test.go` | aria2c Docker rig + dualrun | 35/35 RPC, 100% CLI flags | linux/amd64, every PR |
| End-to-end | `test/e2e/*_test.go` | full aria2go binary + rig | all BEPs exercised | linux/amd64, nightly |
| Benchmarks | `*_bench_test.go` colocated | `testing.B -benchmem`, benchstat | tracked | nightly + manual |
| Stress | `test/stress/*_test.go` `//go:build stress` | generated swarms | 10K peers / 100K pieces / 24h soak | weekly |
| Fuzz | `Fuzz*` in `*_test.go` | `testing.F` | all listed targets ≥1h/run | every PR (short), nightly (long) |
| Property | `*_property_test.go` | `testing/quick` | all invariants | every PR |

### 12.3 Conformance dualrun rig

`test/conformance/Dockerfile.aria2c`:
```
FROM debian:bookworm-slim
ARG ARIA2_VERSION=1.37.0
RUN apt-get install -y --no-install-recommends aria2=1:1.37.0-1+b1 && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["aria2c"]
```

`test/conformance/aria2cref/aria2cref.go` exposes:
```go
func Run(t *testing.T, args []string, stdin io.Reader, workdir string) (stdout, stderr []byte, exit int)
```

Spawns the pinned container with workdir bind-mounted. Default `--network=none`; networked tests use docker-compose with isolated bridge.

Dual-run abstraction:
```go
type DualResult struct{ Ref, Impl Run }
type Run struct{ Stdout, Stderr []byte; Exit int; Disk map[string][]byte; Duration time.Duration }
func Both(t *testing.T, args []string, fixtureDir string) DualResult
assertEqualStdout(t, d)   // normalized
assertEqualExit(t, d)
assertEqualDisk(t, d, ignorePatterns)
assertEqualJSONShape(t, refJSON, implJSON)  // recursive structural equality with allowlisted differences (gid, version)
```

### 12.4 Per-protocol conformance

- **HTTP/HTTPS:** `httptest.NewServer`, `NewTLSServer`. Custom `.http` ASCII replay format (multi-response, easy for AI agents to read). Wire-level header order test (capture aria2c's bytes vs ours).
- **FTP/SFTP:** in-process FTP server (~600 LOC, under `test/rig/ftp/`). In-process SSH-2 server using `golang.org/x/crypto/ssh` **as test-only** counterparty (production code stays stdlib-only; this asymmetry is intentional — see ADR-0007 rationale).
- **BT local swarm:** `test/rig/btswarm/` — N in-process peers + 1 tracker + 1 DHT bootstrap. Knobs: "be honest", "send invalid pieces", "drop after N bytes". Cross-check rig uses libtorrent-rasterbar (Python binding in sidecar container) to verify aria2go-as-leecher and aria2go-as-seeder interop with a known-good third party.
- **DHT:** mock bootstrap at `127.0.0.1:6881` with N synthetic neighbors; assert routing table state after M pings.
- **Trackers:** mock HTTP + UDP trackers (BEP 15 connect/announce/scrape).
- **MSE/PE:** wire-trace test from captured qBittorrent handshake (sanitized; provenance archived).
- **Metalink:** RFC 5854 examples directly (spec text, license-clean).

### 12.5 Fuzzing inventory (mandatory)

| Target | Path | Round-trip property |
|---|---|---|
| `FuzzBencodeDecode` | `internal/bencode/fuzz_test.go` | decode→encode equals input |
| `FuzzBencodeEncode` | same | encode→decode equals input |
| `FuzzMagnetParse` | `internal/magnet/fuzz_test.go` | parse→serialize equivalent |
| `FuzzTorrentParse` | `internal/protocol/bittorrent/torrent/fuzz_test.go` | reserialize equals input |
| `FuzzMetalinkParse` | `internal/protocol/metalink/fuzz_test.go` | reserialize equivalent |
| `FuzzAria2ConfParse` | `internal/config/fuzz_test.go` | parse→serialize→parse stable |
| `FuzzSessionParse` | `internal/sessionfile/fuzz_test.go` | parse→serialize equals input |
| `FuzzJSONRPCRequest` | `internal/rpc/jsonrpc/fuzz_test.go` | parse→re-marshal structurally equal |
| `FuzzXMLRPCRequest` | `internal/rpc/xmlrpc/fuzz_test.go` | same |
| `FuzzHTTPResponseHead` | `internal/protocol/http/fuzz_test.go` | parse→serialize equivalent |
| `FuzzFTPResponse` | `internal/protocol/ftp/fuzz_test.go` | extract code + message |
| `FuzzSSHPacket` | `internal/ssh/transport/fuzz_test.go` (a) / `internal/protocol/sftp/fuzz_test.go` (b) | round-trip |
| `FuzzDHTKRPC` | `internal/protocol/bittorrent/dht/fuzz_test.go` | round-trip |
| `FuzzPeerWire` | `internal/protocol/bittorrent/peer/fuzz_test.go` | round-trip |
| `FuzzWSFrame` | `internal/rpc/transport/fuzz_test.go` | round-trip + masking |
| `FuzzHTTPRange` | `internal/protocol/http/range_fuzz_test.go` | parse→serialize→parse stable |
| `FuzzNetscapeCookies` | `internal/cookies/fuzz_test.go` | parse→serialize→parse stable |
| `FuzzNetrc` | `internal/netrc/fuzz_test.go` | parse→serialize equivalent |
| `FuzzContentDisposition` | `internal/protocol/http/content_dispo_fuzz_test.go` | parse→serialize equivalent |
| `FuzzDigestChallenge` | `internal/protocol/http/auth_fuzz_test.go` | parse→serialize equivalent |

Corpus management: seeds in `testdata/fuzz/Fuzz<X>/`. Long fuzz runs commit new corpus entries via cron.

### 12.6 Property-based tests (testing/quick)

| Subject | Property |
|---|---|
| Piece picker | `RarestFirst().Pick()` returns min-count piece; endgame triggers at threshold |
| Bencode | round-trip on random `Value` trees |
| HTTP Range merge/split | `merge(split(merge(r))) == merge(r)`; total bytes preserved |
| Bitfield | Set→Get true; Clear→Get false; PopCount; Union commutative/associative |
| DHT XOR distance | symmetric; identity self; triangle inequality |
| Token bucket fairness | N consumers under fake clock within ±1% of share |
| Storage hash verify | random bytes → write → read → hash matches |

### 12.7 testing/synctest deterministic concurrency (Go ≥ 1.25)

- Rate limiter under fake clock (exact expected duration).
- Scheduler timeouts (tasks fire at exactly the right tick).
- Peer keepalive (drop at exactly 120s idle).
- Tracker announce backoff (exact backoff curve).
- DHT bucket refresh timer (exactly 15min inactivity).
- File allocation cooperative cancel (one-yield exit).

### 12.8 Performance benchmarks

| Bench | Target |
|---|---|
| `BenchmarkHTTPDownload_1GB_Loopback` | ≥ 8 Gb/s |
| `BenchmarkHTTPSDownload_1GB_Loopback` | ≥ 4 Gb/s |
| `BenchmarkFTPDownload_1GB_Loopback` | ≥ 6 Gb/s |
| `BenchmarkSFTPDownload_1GB_Loopback` | ≥ 1 Gb/s |
| `BenchmarkBTSeed_50Peers_1GB` | saturate loopback |
| `BenchmarkBTLeech_50Peers_1GB` | saturate loopback |
| `BenchmarkPieceVerify_4MB_SHA1` | ≥ 2 GB/s |
| `BenchmarkDHTLookup_8NodesPerHop` | ≤ 250 ms p50 |
| `BenchmarkMemFootprint_10KPeers` | ≤ 200 MB RSS delta |
| `BenchmarkDiskCoalesce_1KSmallWrites` | ≤ N/100 syscalls vs naive |
| `BenchmarkStartup_10KDownloads` | ≤ 2 s |

`benchstat` compares HEAD vs main; ≥10% regression fails CI.

### 12.9 Stress tests

- `TestStress_10KPeers` — 10K in-process peers, assert no goroutine leak, ≤1 GB RSS, no race hits.
- `TestStress_100KPieces` — 400 GB torrent (sparse), 50 peers, completes within expected, picker doesn't degrade O(n²).
- `TestStress_HTTPSegment_LoopbackGigPerSec` — 16 segments saturating loopback.
- `TestSoak_24h` — 100 mixed downloads (HTTP + BT) loop for 24h; ≤5% mem growth, no goroutine growth, no panics, no races.

### 12.10 CI matrix

| Job | OS | Arch | Go | Race | Runs |
|---|---|---|---|---|---|
| unit-linux-amd64 | ubuntu-22.04 | amd64 | 1.25 | yes | `go test ./...` |
| unit-linux-arm64 | ubuntu-22.04-arm | arm64 | 1.25 | yes | same |
| unit-darwin-amd64 | macos-13 | amd64 | 1.25 | yes | same |
| unit-darwin-arm64 | macos-14 | arm64 | 1.25 | yes | same |
| unit-windows-amd64 | windows-2022 | amd64 | 1.25 | yes | same |
| unit-tip | ubuntu-22.04 | amd64 | tip | yes | same |
| integration | ubuntu-22.04 | amd64 | 1.25 | yes | `go test -tags=integration ./...` |
| conformance | ubuntu-22.04 | amd64 | 1.25 | yes | docker compose + `go test -tags=conformance ./test/conformance/...` |
| fuzz-short | ubuntu-22.04 | amd64 | 1.25 | no | each `Fuzz*` for 30 s |
| bench | ubuntu-22.04 | amd64 | 1.25 | no | `go test -bench=. -benchmem`; benchstat vs main |

Per PR: unit (all OS/arch), integration, conformance, fuzz-short, bench (path-filtered).
Nightly: long-fuzz (1h per target), full bench with profile upload.
Weekly: stress.

### 12.11 Reproducibility

- `go.mod` declares `go 1.24` and `toolchain go1.25.x`.
- `GOTOOLCHAIN=local` in CI to fail-fast on runner drift.
- aria2c pinned by Debian package version and image digest verified at runtime.
- Default `ARIA2GO_OFFLINE=1`: any non-loopback dial panics.
- Release builds: `go build -trimpath -buildvcs=false -ldflags='-s -w -buildid='`. `make verify-reproducible` rebuilds and diffs.

### 12.12 Fixture management

- `internal/<x>/testdata/` per-package (Go convention).
- `test/fixtures/_provenance/`: every captured-from-wild fixture documented with source, capture command, sanitization.
- aria2 upstream fixtures: **regenerate** rather than copy (license caution). RFC 5854 Metalink examples are spec text and OK to use directly.
- Session fixtures generated from the pinned aria2c container at test-build time.
- Replay format custom ASCII (diff-friendly for AI agents). pcap only in `_provenance/`.

### 12.13 RPC compat test plan

`test/conformance/rpc/methods.go` enumerates all 35 methods. One focused test per method in `method_<name>_test.go`. Shape-equality helper: same JSON keys recursively, same scalar types, per-key tolerances (`version` differs, `gid` is any 16-hex). 6 notifications similarly.

End-to-end with AriaNg: Playwright (Node, run via Docker) loads AriaNg pointed at aria2go and exercises add/monitor/change/pause/remove/save. Pass/fail binary.

### 12.14 CLI compat test plan

`test/conformance/cli/extract_flags.go` runs in pinned aria2c container, extracts `--help=#all` flags into `flags.json` (committed). Per-flag tests hand-written using dualrun. Help text shape: same categories, same flag-name set, descriptions allowed to differ. Exit-code table with one fixture per code.

### 12.15 Conformance scorecard

`plans/CONFORMANCE.md` auto-generated by `plans/tools/orchestrator/conformance-score`. Per-area passed/total + percent + last_check. Sample row:

| Area | Passed | Total | % | Last check |
|---|---|---|---|---|
| HTTP / HTTPS download | 118 | 120 | 98.3% | 2026-05-18 |
| FTP / FTPS download | 44 | 46 | 95.7% | 2026-05-18 |
| SFTP download | 19 | 22 | 86.4% | 2026-05-17 |
| BitTorrent core | 210 | 248 | 84.7% | 2026-05-18 |
| BT peer wire | 96 | 118 | 81.4% | 2026-05-18 |
| BT tracker (HTTP+UDP) | 34 | 34 | 100.0% | 2026-05-18 |
| BT DHT (mainline) | 51 | 62 | 82.3% | 2026-05-18 |
| Metalink v3 | 18 | 18 | 100.0% | 2026-05-15 |
| Metalink v4 | 21 | 24 | 87.5% | 2026-05-15 |
| JSON-RPC server | 62 | 62 | 100.0% | 2026-05-18 |
| XML-RPC server | 18 | 18 | 100.0% | 2026-05-18 |
| WebSocket RPC | 9 | 12 | 75.0% | 2026-05-18 |
| Config + CLI options | 141 | 150 | 94.0% | 2026-05-18 |
| Signal handling / dump | 9 | 9 | 100.0% | 2026-05-18 |
| **Overall** | 850 | 943 | 90.1% | 2026-05-18 |

### 12.16 Ship-1.0 gates

- Conformance: every area ≥99% (RPC, Session, Config must be 100%); aggregate ≥99.5%. Every failure has an issue link in `docs/parity-waivers.md`.
- Fuzz: every target clean for ≥24 CPU-hours on nightly long-fuzz. Critical parsers ≥96 CPU-hours.
- Race: zero hits across the full suite (including 24h soak).
- Perf: within ±10% of aria2c on HTTP loopback 1GB; ±15% on BT seed; DHT p50 ≤1.2× aria2c; mem ≤1.5× aria2c at 10K peers; startup ≤1.5× aria2c with 10K-download session.
- Stability: 24h soak zero panics, zero goroutine leaks (±2), zero FD leaks (±5).
- Code coverage: ≥85% across `internal/`, ≥92% on critical paths.
- Operational: AriaNg works end-to-end against aria2go; ≥1 third-party RPC client (pyrosimple or aria2p) green.
- Single `make ship-check` target runs every gate; exit 0 ⇒ shippable.

---

## 13. Risks and mitigations (consolidated, with Codex feedback)

1. **SFTP without `x/crypto/ssh` (path a) blows up the schedule.** Severity HIGH. Mitigation: default to path (a) per your choice but document path (b) as a 1-ADR flip; schedule `internal/ssh/*` as a parallel work-stream; defer SFTP to release v2026.06 if it slips. **Codex caveat (path b):** freeze x/crypto/ssh version + add SSH/SFTP behavior-compat tests before any SFTP ticket starts.
2. **Bencode info-dict round-trip → wrong infohash.** Mitigation: `bencode.ExtractRaw` returns original bytes; never re-encode info dict; CI golden-test 50-torrent corpus before any peer-wire ticket starts.
3. **RPC subtle behaviors diverge.** Mitigation: golden-test-first per Codex Decision 3; `plans/byte-compat/rpc-method-table.md` enumerates every method × field; goldens captured from aria2c 1.37.0 across all 35 methods and 6 notifications **before** any implementation ticket starts.
4. **WebSocket edge cases hang real clients (AriaNg).** Mitigation: include AriaNg in nightly e2e (Playwright); RFC 6455 section-by-section unit tests; one-writer goroutine pattern.
5. **uTP correctness / CC mistakes.** Mitigation: ADR-0019 places uTP behind `--enable-utp=false` default until cross-validated against `libutp` packet captures; defer to MVP+1.
6. **10K-peer goroutine explosion / GC pauses.** Mitigation: Phase 1.5 Scalability Validation Milestone (per Codex Decision 2); simulated 5K-peer corpus on 4-core runner; alert on >100ms GC or >1GB RSS; aggressive sync.Pool; per-torrent peer-set sharding.
7. **Byte-compat creep across aria2 patches.** Mitigation: pin to **aria2 1.37.0** (ADR-0020); deviations documented in CHANGELOG; future bumps trigger ADRs.
8. **Cross-platform falloc / FAT32 quirks.** Mitigation: capability detection at file create; fallback to truncate + warn; CI matrix includes Windows Server 2019 + 2022 + macOS 13/14.
9. **Cookie semantics without PSL → subtle leaks.** Mitigation: match aria2 exactly (no PSL); security note in CHANGELOG.
10. **AI agents drift from `plans/` specs.** Mitigation: every PR references its `plans/.../TNNN.md` ticket; CI verifies ticket exists; `plans/tools/orchestrator/adr-check` enforces ADR-0001/0008/0021; LOC budget per ticket enforced.
11. **(Per Codex Decision 4) BT zero-knowledge boundary too strict.** Mitigation: ADR-0004 revised to add explicit typed interfaces in `internal/contracts/` (`TorrentStatusProjector`, `FilePieceMap`, `TorrentLifecycleControl`, `TorrentRPCProjection`).
12. **(Per Codex Decision 5) aria2.session format ambiguities cross-platform.** Mitigation: defined as testable contract; CI runs round-trip across Linux/macOS/Windows; preserve unknown lines through `Entry.Unknown`.
13. **(Per Codex Decision 6) Scheduler missing fairness/dynamic-option/retry edge cases.** Mitigation: scheduler SPEC has dedicated `## Invariants` block enumerating fairness, dynamic options, retries, priority ordering, resource release.
14. **(Per Codex Decision 7) 250+ tickets become their own product.** Mitigation: ADR-0021 mandates `## Contract Surface` field per ticket; human-review gate on any ticket touching CLI/RPC/session/config; orchestrator binaries kept minimal (~1500 LOC total).

---

## 14. 3-tier plan structure (artifact specification)

After your approval of this master plan, the content above + the templates below are decomposed into the following file tree under `aria2go/`. The single editable artifact in plan-mode is *this file*; the decomposition is the first task post-approval.

### 14.1 Filesystem layout (planning artifacts)

```
aria2go/
├── AGENTS.md                              # root contract (§18) — short, always-loaded
├── plans/
│   ├── PLAN.md                            # extracted master plan (this file, post-split)
│   ├── manifest.json                      # machine-readable ticket queue (§14.2)
│   ├── manifest.schema.json               # JSON Schema, CI-validated
│   ├── manifest.lock                      # optimistic write-lock (PID+timestamp)
│   ├── TRACKING.md                        # auto-regenerated live status (§16)
│   ├── CONFORMANCE.md                     # auto-regenerated parity scorecard
│   ├── GLOSSARY.md                        # aria2 terms (BT piece, DHT, MSE, …)
│   ├── decisions/
│   │   ├── ADR-0001-library-policy.md    (… through ADR-0022 …)
│   │   └── INDEX.md
│   ├── contracts/
│   │   ├── interfaces.md
│   │   ├── error-codes.md
│   │   ├── rpc-methods.md
│   │   ├── config-keys.md
│   │   └── wire-formats.md
│   ├── byte-compat/
│   │   ├── session-format.md
│   │   ├── rpc-method-table.md
│   │   ├── cli-flags-table.md
│   │   ├── dht-file-format.md
│   │   └── conf-file-format.md
│   ├── modules/                           # one dir per module 00..16
│   │   └── NN-<name>/
│   │       ├── SPEC.md                    # module spec (§14.5)
│   │       ├── AGENTS.md                  # module-specific rules (§19)
│   │       └── tickets/TXXX-<slug>.md     # one file per ticket (§14.3)
│   └── test-plans/
│       ├── conformance-matrix.md
│       ├── corpus/                        # license-clean .torrent/.metalink
│       ├── fuzz-targets.md
│       ├── interop-aria2c.md
│       ├── rpc-goldens.md
│       ├── perf-bench.md
│       └── scalability-validation.md
```

Module numbering (00–16):
```
00-cmd-aria2c            (~5 tickets — main binary)
01-core-engine           (~25 tickets — engine, scheduler, request_group)
02-uri-parser            (~4 tickets — magnet, URI normalize)
03-protocol-http         (~9 tickets)
04-protocol-ftp          (~8 tickets)
05-protocol-sftp         (~8 tickets path b, ~33 tickets path a incl. internal/ssh)
06-bittorrent-core       (~22 tickets — bencode, torrent, mse, lpd)
07-bittorrent-peer       (~15 tickets — peer wire + LTEP)
08-bittorrent-tracker    (~6 tickets — HTTP + UDP + multi)
09-bittorrent-dht        (~10 tickets — Kademlia)
10-bittorrent-extensions (~11 tickets — uTP behind --enable-utp, BTv2)
11-metalink              (~5 tickets — v3 + v4)
12-disk-storage          (~11 tickets — alloc, multi-file, verify)
13-net-shared            (~13 tickets — netx + tlsx + portmap)
14-rpc-server            (~23 tickets — jsonrpc + xmlrpc + dispatcher + transport)
15-config-options        (~17 tickets — config + cookies + netrc + sessionfile)
16-logging-metrics       (~3 tickets — slog + classic + rotator)
17-test-fixtures         (~varies — shared fixtures + rigs)
```

Total ~190 tickets (path b) / ~215 tickets (path a). Headroom for split-ups → ~250–400.

### 14.2 manifest.json schema (full)

```json
{
  "schema_version": "1.0.0",
  "project": "aria2go",
  "generated_at": "2026-05-18T09:00:00Z",
  "generator": "plans/tools/orchestrator/manifest-lint",
  "policy": {
    "max_artifact_tokens": 10000,
    "target_artifact_tokens": 4000,
    "complexity_budgets": {
      "S":  {"context": 1500, "impl": 700,  "test": 300,  "total": 2500},
      "M":  {"context": 2500, "impl": 1500, "test": 500,  "total": 4500},
      "L":  {"context": 3500, "impl": 2500, "test": 1000, "total": 7000},
      "XL": {"context": 5000, "impl": 3500, "test": 1500, "total": 10000}
    },
    "library_path": "a"
  },
  "modules": [
    { "id": "03-protocol-http",
      "spec": "plans/modules/03-protocol-http/SPEC.md",
      "depends_on_modules": ["13-net-shared", "15-config-options", "16-logging-metrics"] }
  ],
  "tickets": [ /* see schema below */ ]
}
```

**Per-ticket schema** (validated by `plans/tools/orchestrator/manifest-lint`):

| Field | Type | Rule |
|---|---|---|
| `id` | string | `T\d{3}`, globally unique |
| `module` | string | matches a module id |
| `title` | string | ≤80 chars, imperative |
| `path` | string | repo-relative path to the .md ticket file |
| `status` | enum | `pending` / `in_progress` / `in_review` / `done` / `blocked` |
| `depends_on` | string[] | ticket ids; must form a DAG |
| `blocked_by` | string[] | non-ticket blockers ("ADR-0005 update", "spec gap") |
| `target_files` | string[] | files this ticket creates/modifies; no two non-dependent tickets may share |
| `test_files` | string[] | `_test.go` files |
| `context_files` | string[] | ≤6 entries, sum tokens ≤ context budget for the complexity tier |
| `context_budget_tokens` | int | ≤ `complexity_budgets[complexity].context` |
| `complexity` | enum | S / M / L / XL |
| `priority` | int | 1..5 |
| `claimed_by` | string\|null | agent id |
| `claimed_at` | RFC3339\|null | |
| `claim_ttl_seconds` | int | default 7200 |
| `gates` | string[] | from enum: `go-vet`, `go-test`, `race`, `fuzz-<name>-<duration>`, `bench`, `interop-aria2c` |
| `contract_surface` | object | per ADR-0021: `{cli:[], rpc:[], session:[], config:[], fixtures:[]}` |
| `notes` | string | ≤500 chars |

**Validation rules:**

1. `id` regex match, no duplicates.
2. Every `path`, `target_files`, `test_files`, `context_files` entry exists (or for `target_files` is creatable — parent dir exists).
3. `depends_on` only known ids; DAG acyclic (Tarjan SCC singletons).
4. `target_files` overlap rule: if A and B share any `target_files` entry, A ∈ deps(B) or B ∈ deps(A).
5. `context_budget_tokens` ≤ `complexity_budgets[complexity].context`; sum file sizes (via repo-internal tokenizer) ≤ budget.
6. `gates` drawn from finite enum.
7. Module references resolve.
8. `status=done` requires `claimed_by != null` and ≥1 CI run referenced in `notes`.
9. `status=in_progress` past `claimed_at + claim_ttl_seconds` is swept to `pending`.
10. `priority=1` tickets must be on the longest DAG path.
11. **(ADR-0021)** Tickets with non-empty `contract_surface.cli/rpc/session/config` cannot go `in_progress` without a human-review gate.

**Three-ticket worked example:**

```json
{
  "tickets": [
    { "id": "T019", "module": "06-bittorrent-core",
      "title": "Define bencode value type and parser surface",
      "path": "plans/modules/06-bittorrent-core/tickets/T019-bencode-types.md",
      "status": "done", "depends_on": [], "blocked_by": [],
      "target_files": ["internal/bencode/types.go"],
      "test_files":   ["internal/bencode/types_test.go"],
      "context_files": ["plans/contracts/wire-formats.md", "plans/modules/06-bittorrent-core/SPEC.md"],
      "context_budget_tokens": 1300, "complexity": "S", "priority": 1,
      "claimed_by": "qwen-coder-2026-05-15-0901", "claimed_at": "2026-05-15T09:01:00Z",
      "claim_ttl_seconds": 7200, "gates": ["go-vet","go-test"],
      "contract_surface": {"cli":[], "rpc":[], "session":[], "config":[], "fixtures":[]},
      "notes": "Added Value.Kind() and stringer." },
    { "id": "T020", "module": "06-bittorrent-core",
      "title": "Implement bencode decoder",
      "path": "plans/modules/06-bittorrent-core/tickets/T020-bencode-decoder.md",
      "status": "in_progress", "depends_on": ["T019"], "blocked_by": [],
      "target_files": ["internal/bencode/decode.go"],
      "test_files":   ["internal/bencode/decode_test.go", "internal/bencode/fuzz_test.go"],
      "context_files": [ "plans/modules/06-bittorrent-core/tickets/T019-bencode-types.md",
                         "plans/contracts/wire-formats.md", "internal/bencode/types.go" ],
      "context_budget_tokens": 2400, "complexity": "M", "priority": 1,
      "claimed_by": "deepseek-coder-2026-05-18-1043", "claimed_at": "2026-05-18T10:43:00Z",
      "claim_ttl_seconds": 7200, "gates": ["go-vet","go-test","race","fuzz-bencode-5m"],
      "contract_surface": {"cli":[], "rpc":[], "session":[], "config":[], "fixtures":["testdata/fuzz/FuzzBencodeDecode/seed.bin"]},
      "notes": "" },
    { "id": "T021", "module": "06-bittorrent-core",
      "title": "Implement bencode encoder + round-trip property tests",
      "path": "plans/modules/06-bittorrent-core/tickets/T021-bencode-encoder.md",
      "status": "pending", "depends_on": ["T019","T020"], "blocked_by": [],
      "target_files": ["internal/bencode/encode.go"],
      "test_files":   ["internal/bencode/encode_test.go"],
      "context_files": [ "plans/modules/06-bittorrent-core/tickets/T019-bencode-types.md",
                         "plans/modules/06-bittorrent-core/tickets/T020-bencode-decoder.md",
                         "internal/bencode/types.go" ],
      "context_budget_tokens": 2200, "complexity": "S", "priority": 1,
      "claimed_by": null, "claimed_at": null, "claim_ttl_seconds": 7200,
      "gates": ["go-vet","go-test","race"],
      "contract_surface": {"cli":[], "rpc":[], "session":[], "config":[], "fixtures":[]},
      "notes": "" }
  ]
}
```

### 14.3 Ticket template

Tickets are markdown with YAML frontmatter for manifest-rebuild. Hard cap 10K tokens; target ≤4K.

```markdown
---
id: T<NNN>
module: <NN-name>
complexity: S | M | L | XL
priority: 1..5
depends_on: [T<NNN>, ...]
target_files: [internal/<pkg>/<file>.go]
test_files:   [internal/<pkg>/<file>_test.go]
context_files:
  - <path>
  - <path>
context_budget_tokens: <int>
gates: [go-vet, go-test, race, fuzz-<name>-<duration>]
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: []
---

# T<NNN>: <Imperative title>

## Goal
<≤120 words. What state of the world exists when this ticket is done?>

## Why This Matters
<≤1 paragraph. How does this slot into the aria2go feature-clone goal? Reference aria2 behavior being mirrored. Cite ADRs.>

## Acceptance Criteria
1. <Numbered, individually testable. Each is verifiable by one Go test / vet / fuzz.>
2. …
N. All gates listed in the frontmatter pass.

## Contract Surface (mandatory, per ADR-0021)
- CLI: <list of aria2c flags affected, or "none">
- RPC: <list of methods affected, or "none">
- Session: <list of session fields affected, or "none">
- Config: <list of options affected, or "none">
- Fixtures: <list of fixture files required, or "none">

## Context (≤3 files)
- `<path>` — <why this file matters; what to look for>

## Implementation Notes
<≤400 words. Decision-level prose, algorithms, edge cases, names of exported symbols. Cite ADRs by id. Do NOT include full code — that is the agent's job.>

## Error Cases & Validation
- <Input X → Error E (specific error type from contracts/error-codes.md)>
- <Boundary: zero-length → …>
- <Resource limit: input > N bytes → …>

## Out of Scope
- <Explicitly NOT in this ticket. Reference future ticket id when known.>

## References
- aria2 source: <file:line or function in upstream aria2 1.37.0 if relevant>
- BEP-<NN>, RFC-<NNNN>
- ADR-<NNNN>

## Estimated Tokens
- Context: <int>   Implementation: <int>   Tests: <int>   Total: <int>
```

**Worked example** — `plans/modules/06-bittorrent-core/tickets/T020-bencode-decoder.md`:

```markdown
---
id: T020
module: 06-bittorrent-core
complexity: M
priority: 1
depends_on: [T019]
target_files: [internal/bencode/decode.go]
test_files:   [internal/bencode/decode_test.go, internal/bencode/fuzz_test.go]
context_files:
  - plans/modules/06-bittorrent-core/tickets/T019-bencode-types.md
  - plans/contracts/wire-formats.md
  - internal/bencode/types.go
context_budget_tokens: 2400
gates: [go-vet, go-test, race, fuzz-bencode-5m]
contract_surface:
  cli: []
  rpc: []
  session: []
  config: []
  fixtures: [testdata/fuzz/FuzzBencodeDecode/seed.bin]
---

# T020: Implement bencode decoder (encoding/decoding for BT)

## Goal
Implement a streaming bencode decoder producing the `bencode.Value` tree defined in T019. The decoder reads from an `io.Reader`, allocates O(input), and rejects malformed input deterministically with typed errors. After this ticket, `bencode.Decode(r io.Reader) (bencode.Value, error)` is the canonical entrypoint used by torrent-file parsing, tracker responses, DHT messages, and the encoder's round-trip tests in T021.

## Why This Matters
Bencode is the substrate for every BitTorrent wire payload aria2 reads: .torrent files, tracker responses, DHT KRPC. Without it, modules 06–10 cannot proceed. Encoder/decoder boundary is a primary fuzz surface — aria2's history shows malformed bencode is the most common crash vector in BT clients. See ADR-0005 (BT extension coverage).

## Acceptance Criteria
1. `Decode(r io.Reader) (Value, error)` consumes the entire stream; trailing bytes → `bencode.ErrTrailingData`.
2. Handles all four bencode kinds (int, byte-string, list, dict) at arbitrary nesting depth up to `MaxDepth` (default 64); exceeding it → `bencode.ErrDepthExceeded`.
3. Dict keys must be byte-strings and appear in lexicographic order; out-of-order or duplicate keys → `bencode.ErrDictOrder` / `bencode.ErrDictDuplicate`.
4. Integer parsing accepts only `i<digits>e` with no leading zeros except `i0e`; `-0` rejected as `bencode.ErrIntLeadingZero`.
5. Byte-string parsing rejects negative or missing length prefix (`bencode.ErrLengthFormat`) and lengths exceeding `MaxStringLen` (default 64MiB).
6. Decoder is allocation-bounded: 4MiB input decodes in ≤8MiB heap allocation (`BenchmarkDecode4MB`).
7. `FuzzDecode` runs ≥5 minutes in CI with zero panics, zero races (`-race`), zero unbounded memory growth.
8. `go vet ./internal/bencode/...` clean.
9. All decoder errors implement `error` via a single `*bencode.SyntaxError` with `Offset int64` and `Kind ErrKind`, per `contracts/error-codes.md`.

## Contract Surface
- CLI: none
- RPC: none (indirectly affects `addTorrent`)
- Session: none
- Config: none
- Fixtures: `testdata/fuzz/FuzzBencodeDecode/seed.bin` (40 hand-crafted bencode inputs incl. malformed)

## Context (≤3 files)
- `plans/modules/06-bittorrent-core/tickets/T019-bencode-types.md` — defines `Value`, `Kind`, `MaxDepth`/`MaxStringLen` knobs.
- `plans/contracts/wire-formats.md` — bencode grammar reference; authoritative.
- `internal/bencode/types.go` — already-merged type definitions; do not modify.

## Implementation Notes
Use `bufio.Reader` so single-byte peeks are cheap. Recursive descent acceptable up to `MaxDepth`; convert to explicit stack only if a fuzz crash with stack overflow at depth 64 appears (would indicate runtime stack misconfig, report as blocker).

Read lengths via `strconv.ParseInt` after manual digit scanning (avoid `fmt.Sscanf` — slow + allocates). For byte-strings, allocate destination exactly once per token; use `io.ReadFull`. `MaxStringLen` check happens BEFORE allocation — otherwise a hostile tracker can OOM with a 2GiB length declaration that never materializes (CVE-2019-3500 class).

Errors via private helper `s.errf(kind, format, args...)` capturing byte offset.

## Error Cases & Validation
- Empty stream → `SyntaxError{Kind: ErrEmpty, Offset: 0}`
- `i-0e` → `ErrIntLeadingZero`
- `i01e` → `ErrIntLeadingZero`
- `4:ab` (short read) → `io.ErrUnexpectedEOF` wrapped
- `d1:b1:x1:a1:ye` → `ErrDictOrder`
- `d1:a1:x1:a1:ye` → `ErrDictDuplicate`
- depth 65 nested list → `ErrDepthExceeded`
- 65MiB string declaration → `ErrStringTooLarge` (no alloc observed)
- trailing `e` after complete value → `ErrTrailingData`

## Out of Scope
- Bencode encoding (T021).
- Mapping `Value` into typed Go structs via reflection (T024).
- Streaming partial decode (not needed; aria2 buffers full .torrent files).

## References
- aria2 source: `src/bencode2.h`, `src/bencode2.cc` (upstream aria2 1.37.0)
- BEP-3 "bencoding" section
- ADR-0005, ADR-0006

## Estimated Tokens
- Context: 2400   Implementation: 1400   Tests: 500   Total: 4300
```

### 14.4 ADR template (worked example: ADR-0001 above; template below)

```markdown
# ADR-<NNNN>: <Short title>

**Status:** Proposed | Accepted | Superseded by ADR-<NNNN> | Deprecated
**Date:** YYYY-MM-DD
**Supersedes:** ADR-<NNNN> (or "none")
**Related:** ADR-<NNNN>, contracts/<file>.md

## Context
<2-4 paragraphs. The problem, observed aria2 behavior, constraints, alternatives.>

## Decision
<1 paragraph. The decision, declarative, present tense.>

## Consequences
### Positive
- ...
### Negative
- ...
### Neutral
- ...

## Compliance Notes
- Tickets affected: <list of ticket ids whose AC depends on this ADR>
- Modules affected: <list of module ids>
- Detection: <how plans/tools/orchestrator/adr-check verifies compliance>
```

### 14.5 Module SPEC.md template (worked example summary: §5; full template below)

```markdown
# Module <NN-name> — <Human title>

## Purpose
<1 paragraph: feature surface this module owns>

## Exported API
```go
package <pkg>
// Top-level declarations only — no bodies.
type Foo interface { ... }
func New(opts Options) (*Server, error)
```

## File Structure
| Path | LOC budget | Owning ticket(s) |
|---|---|---|
| internal/<pkg>/types.go | 150 | T019 |
| ... | | |

## Build Dependencies
- stdlib: encoding/binary, io, bufio, ...
- x/* (path b only): <none | listed>
- internal: <list>
- imported by: <list>

## Invariants
1. <Numbered. Things that must remain true across every ticket in this module.>

## Common Mistakes
- <Pitfalls observed during planning or early tickets>

## Testing Strategy
- Unit: ...
- Property: ...
- Fuzz: <named targets, durations>
- Integration: <named tests in test-plans/>

## Related ADRs
- ADR-<NNNN>, ...
```

### 14.6 Dependency graph encoding

Three orthogonal dependency layers, each authoritative in exactly one place:

1. **Ticket-level deps** — only in `manifest.json` under `depends_on`. Tickets do not cite each other in prose deps — only in `## Context` (with concrete content) and `## References`. `plans/tools/orchestrator/dag-validate` reads `manifest.json` and rejects cycles, dangling references, and ticket pairs sharing `target_files` without a dependency edge.

2. **Module-level deps** — in each SPEC's `## Build Dependencies` mirroring `modules[].depends_on_modules` in `manifest.json`. Manifest is authoritative; SPEC is human-readable redundancy. `manifest-lint` cross-checks.

3. **File-level deps** — derived from the SPEC's `## File Structure` table. `dag-validate` runs `go list -deps` once code exists and reconciles actual imports vs declared.

**Cross-module DAG** (adjacency list, 17 nodes, 0 cycles): see §4. Critical path: `16 → 13 → 06 → 07 → 10 → 01 → 00`.

---

## 15. Agent workflow and orchestrator (sub-plan execution)

### 15.1 Per-agent 8-step loop

```
1. boot():
   manifest   = read("plans/manifest.json")
   adrs       = read_dir("plans/decisions/*.md")    # cache for session
   contracts  = read_dir("plans/contracts/*.md")    # cache for session
   root_agent = read("AGENTS.md")
   agent_id   = f"{model}-{uuid7()}"

2. select_ticket():
   for t in topo_sort(manifest.tickets, key=priority):
       if t.status != "pending":              continue
       if any(d.status != "done" for d in deps(t)): continue
       if t.blocked_by:                       continue
       if t.contract_surface has non-empty fields AND not has_human_review_for(t): continue   # ADR-0021
       return t
   sleep(60); goto 2

3. claim(t):
   acquire_flock("plans/manifest.lock", ttl=30s, exclusive=True)
   reread manifest
   if t.status != "pending": release; goto 2     # race lost
   t.status     = "in_progress"
   t.claimed_by = agent_id
   t.claimed_at = now_utc()
   write manifest atomically (tmpfile + rename)
   release flock

4. load_context(t):
   ticket_md      = read(t.path)
   files          = [read(f) for f in t.context_files]
   assert sum(tokens(files)) + tokens(ticket_md) <= budget(t.complexity).context
   working set: ticket_md + files + cached ADRs/contracts/root AGENTS.md

5. implement(t):
   for f in t.target_files:    write or modify f
   for f in t.test_files:      write _test.go covering AC
   run "go vet ./..."
   run "go test -race ./<package>/..."
   for gate in t.gates:        run gate; collect logs

6. submit(t):
   acquire_flock("plans/manifest.lock")
   reread manifest
   t.status = "in_review" if t.has_human_gate else "done"
   t.notes  = f"gates passed: {','.join(t.gates)}; commit={git_sha}"
   append "## Implementation Log" to t.path IFF behavior diverged from Implementation Notes
   write manifest atomically
   release flock

7. trigger_tracking_render():
   exec("plans/tools/orchestrator/tracking-render --write plans/TRACKING.md")
   # idempotent

8. exit_or_loop():
   if --loop: goto 2
   else: exit 0
```

### 15.2 Lock format

`plans/manifest.lock` contains a single line `<agent_id> <claimed_at_unix> <pid> <hostname>`. POSIX `flock(LOCK_EX)`; Windows: `LockFileEx`. Manifest-write TTL: 30s. Ticket-claim TTL: 7200s (2h) default.

### 15.3 Stale-claim recovery

`claim-sweep` orchestrator binary runs every 10 minutes (cron or `--watch`). Walks `manifest.tickets`; finds any `in_progress` and `now() > claimed_at + claim_ttl_seconds`; rewrites to `pending` with `claimed_by=null`, appends `notes: "swept: prior claim {agent_id} expired at {ts}"`. Safe because target_files are append-only for the agent.

### 15.4 Orchestrator binaries

All under `plans/tools/orchestrator/`, multi-subcommand Go binary:

| Subcommand | Purpose | When |
|---|---|---|
| `manifest-lint` | Validates manifest.json against schema; enforces rules; --fix prints diff | pre-commit + every CI |
| `dag-validate` | Tarjan SCC over depends_on; cross-checks module DAG; reconciles `go list -deps` | every CI |
| `claim-sweep` | TTL expiry of stale in_progress tickets | cron, 10 min |
| `tracking-render` | Regenerates plans/TRACKING.md from manifest.json | after every submit + cron 5 min |
| `conformance-score` | Runs conformance harness; regenerates plans/CONFORMANCE.md | nightly |
| `adr-check` | Parses go.mod + internal/ imports; enforces ADR-0001/0008/0021 | every CI |
| `ticket-expand` | LLM-driven ticket draft generator (Phase 1) | on demand |

Each subcommand exits non-zero on first violation; CI fails fast.

### 15.5 Token budgets per complexity

| Tier | Target model | Context | Impl | Test | Total | Typical scope |
|---|---|---|---|---|---|---|
| S | Haiku, deepseek-coder-lite | ≤1500 | ≤700 | ≤300 | ≤2500 | single file, ≤120 LOC, one AC family |
| M | Haiku, qwen-coder | ≤2500 | ≤1500 | ≤500 | ≤4500 | 1-2 files, ≤300 LOC, ~5 AC, may need fuzz |
| L | Sonnet, deepseek-coder-v3 | ≤3500 | ≤2500 | ≤1000 | ≤7000 | 2-4 files, ≤600 LOC, state machine / protocol |
| XL | Opus, gpt-5 | ≤5000 | ≤3500 | ≤1500 | ≤10000 | new subsystem boundary, ≤900 LOC |

Distribution target across corpus: 40% S, 45% M, 13% L, 2% XL. ~10-15 XL tickets expected.

---

## 16. Status tracking and live board

`plans/TRACKING.md` regenerated after every claim/submit + cron 5 min. Markdown for GitHub readability; orchestrator never reads it (manifest.json is truth).

### Sample

```markdown
# aria2go — Live Status
_Generated 2026-05-18T11:14:00Z by plans/tools/orchestrator/tracking-render_

## Headline
- Tickets total: 287
- Done: 142 (49.5%)
- In review: 6
- In progress: 11 (claim window OK: 9; nearing TTL: 2; stale: 0)
- Blocked: 4
- Pending: 124

## Critical-path progress
priority-1 longest path: 41 tickets, 23 done (56.1%)
ETA at current 12-ticket/day burn rate: 2026-05-30

## Risk
- 2 tickets nearing TTL: T144 (1h54m, TTL 2h), T199 (1h47m, TTL 2h)
- 4 blocked: T210, T211 blocked_by ADR-0005 update; T260 blocked_by spec gap; T273 blocked_by manual review

## In progress
| Ticket | Module | Agent | Claimed | TTL left | Complexity |
|---|---|---|---|---|---|
| T144 | 06-bittorrent-core | qwen-coder-… | 09:20:00Z | 0h05m | M |
| ... |

## Per-module progress
| Module | Done | Total | % |
|---|---|---|---|
| 00-cmd-aria2c | 3/5 | 60% |
| 01-core-engine | 18/25 | 72% |
| 03-protocol-http | 9/9 | 100% |
| ... |
```

**Risk detection** (auto-flagged):
- Overdue claims: any ticket with `now() - claimed_at > 0.9 * claim_ttl_seconds`.
- Stale: `now() - claimed_at > claim_ttl_seconds` (auto-swept).
- Blocked clusters: any module where `blocked/total > 0.2`.
- Critical-path stall: no priority-1 ticket moved in 48h.

---

## 17. Failure modes and recovery

| Failure | Detection | Recovery |
|---|---|---|
| Stale lock on manifest.lock | flock timeout 30s; manifest-lock-sweep cron checks lockfiles >5 min | Delete lock; sweep any in_progress tickets that crossed TTL |
| Agent claims but never finishes | claim-sweep cron | Ticket → pending; partial files on disk: next claimer treats fresh; pre-existing code either compiles (tests catch issues) or replaced |
| Dependency cycle introduced | dag-validate (pre-commit + CI) | Reject change; author edits depends_on to break cycle |
| ADR contradiction discovered | Human spots or adr-check notices import violating policy | New ADR with Supersedes; adr-check walks affected ticket list, sets blocked_by; TRACKING.md surfaces in blocked-clusters section |
| Agent's implementation doesn't compile | CI gate go-build fails on submitted commit | Orchestrator returns ticket to pending, decrement priority, append notes; CI auto-reverts |
| Agent's implementation fails fuzz | same for fuzz-* gate | Same recovery; conformance-score flags area; 3 successive failures by different agents → blocked_by: ["needs human design"] |
| Concurrent edits to manifest.json | flock serializes writers; post-acquisition mtime check | First writer wins; second rereads, replays mutation, retries; trivial-field conflict → human queue |
| Conformance regression | Nightly conformance-score compares vs prior; any percent drop fails CI | Tickets for failing assertions auto-flipped to pending; PR reverted |
| Disk-allocation ticket breaks platform | Per-platform CI matrix | Ticket marked done only if all platforms pass; else stays in_review with follow-up "fix-platform-Y" ticket |

---

## 18. Root AGENTS.md (post-approval content)

```markdown
# AGENTS.md — aria2go root agent contract

## What this repository is
A pure-Go feature clone of aria2 (https://aria2.github.io). Goal: behavioral parity with aria2 1.37.0, not stylistic translation. Use idiomatic Go.

## What every agent MUST do
1. Read plans/manifest.json. Use it — not directory listings — to pick work.
2. Honor the dependency graph: never claim a ticket whose `depends_on` is not `done`.
3. Use manifest.lock for every manifest mutation. See plans/ROLE-AGENT.md for pseudocode.
4. Only modify files in your ticket's `target_files` and `test_files`. Touching anything else is a contract violation.
5. Run, in order, before submitting: `go vet ./...`, `go test -race ./<pkg>/...`, then every gate listed in the ticket's `gates`.
6. If you discover a ticket-vs-SPEC inconsistency, STOP and set status `blocked` with `blocked_by: ["spec gap"]`. Do not invent behavior.

## What every agent MUST NOT do
1. Import anything outside the ADR-0001/0002 shortlist. go.mod is read-only.
2. Add `// nolint` or any linter suppression. Fix the underlying issue or block the ticket.
3. Touch plans/PLAN.md, any SPEC.md, or any ADR. These are human-owned.
4. Touch other agents' in-progress tickets.
5. Skip the lock when writing manifest.json. Filesystem races corrupt the queue.
6. Use cgo, `unsafe.Pointer` arithmetic, or runtime monkey-patching.

## Style
- Go 1.24+ language features allowed; no generics-for-the-sake-of-it.
- Errors: every package defines `*pkg.Error` with `Code` per plans/contracts/error-codes.md. Wrap with %w.
- Concurrency: see ADR-0002. No bare goroutines launched without a context.Context.
- Comments: doc comments on every exported symbol. No "this function does X" restatements; describe contracts and invariants.

## When you finish
Append `## Implementation Log` to the ticket file IFF behavior diverged from `## Implementation Notes`. Set ticket status to `in_review` (or `done` if no human-gated gate). Trigger tracking-render. Exit.
```

---

## 19. Per-module AGENTS.md (sample for `03-protocol-http`)

```markdown
# AGENTS.md — module 03-protocol-http

## Module-specific rules
1. NEVER use http.DefaultClient, http.DefaultTransport, or http.Get/Post. Construct your own client per internal/protocol/http/client.go.
2. NEVER call os.Open/os.Create or anything under os that touches disk. This module's contract is "bytes out via channels"; disk is module 12's job.
3. Per-host parallelism MUST go through netx.DialSemaphore. Do not open net.Conn directly.
4. Range arithmetic uses int64 everywhere; never int.
5. Tests live colocated; integration fixtures live in testdata/http-fixtures/ and are SHA-pinned (see testdata/MANIFEST.sha256).
6. Anything in client.go, request.go, or client_test.go MUST preserve the Engine interface unchanged. Adding methods is a new ticket, not a refactor.

## Common pitfalls
- See SPEC.md "Common Mistakes" — read before submitting.

## Local gates beyond standard
- `gosec-http`: scans for hard-coded credentials in fixtures. `go run ./plans/tools/orchestrator/gosec-http`.
```

---

## 20. Anti-patterns forbidden in tickets

`manifest-lint --strict` and `ticket-expand` validation reject any of:

1. **Load whole tree** — context_files lists a directory, glob, or >6 entries.
2. **Implicit conventions** — "follow the existing patterns" without naming the file.
3. **Unbounded scope** — >300 LOC per file or >4 target_files. Hard fail >300 LOC; soft warn at 200.
4. **Vague AC** — not testable by named Go test/vet/fuzz. Words like "robust", "fast", "clean" without measurable threshold.
5. **Missing Out of Scope** — mandatory; "n/a" allowed only when genuinely no temptations exist.
6. **Cross-ticket reference without inclusion** — "see T015" forbidden; inline or list .md in context_files.
7. **Circular dependencies** — depends_on cycles or mutual context_files.
8. **Target-file collision** — two non-dependent tickets writing same file.
9. **Code in Implementation Notes** — notes describe decisions, not >5 lines of Go.
10. **No gates** — every ticket has at least go-vet + go-test. Protocol/parser tickets must include a fuzz gate.
11. **Token over-budget** — ticket + context_files sum exceeds tier's context budget.
12. **Mutable ADR references** — cite by id, not file path.
13. **Forward references to nonexistent files** — target_files in nonexistent dirs without prior creation ticket.
14. **Status mutations in body** — status lives in manifest.json only.
15. **Linter suppressions** — `// nolint`, `_ = err`, `// TODO(agent)` forbidden.
16. **"Implement as you see fit"** — tickets must constrain; if unconstrainable, add `## Open Questions` + blocked_by ["design"].
17. **Cross-platform amnesia** — disk/signal/socket tickets list every target platform in AC.
18. **Missing References** — protocol tickets cite RFC/BEP; clone tickets cite aria2 upstream file+function.
19. **priority:1 without critical-path proof** — only tickets on longest DAG path may carry priority 1; dag-validate auto-demotes.
20. **(ADR-0021) Missing Contract Surface** — every ticket must have non-empty `## Contract Surface` block (each list may be `none` if truly out of compat surface).

---

## 21. Phased delivery roadmap

### Phase 0 — Plan finalization (Week 0, ~5 days human effort)

- Decompose this master plan file into `aria2go/plans/PLAN.md` + per-section files (ADRs, SPECs, contracts/, byte-compat/, test-plans/).
- Hand-write the 30 exemplar tickets across all modules and complexity tiers (S/M/L/XL) to seed the LLM expander.
- Hand-write the manifest.json policy block and 30 exemplar entries.
- Stand up `plans/tools/orchestrator/` (8 binaries × ~200 LOC each = ~1500 LOC). These are the only Go files written by humans pre-Week-1.
- Capture aria2c reference goldens for RPC (35 methods × 6 cases each = 210 fixtures) into `test/golden/rpc/`.
- Capture aria2c help/version/conf goldens into `test/golden/cli/`.
- Run `manifest-lint --strict` + `dag-validate` until clean.

### Phase 1 — Sub-plan expansion (Week 1)

- Per module, `plans/tools/orchestrator/ticket-expand` invoked with SPEC + 30-exemplar seed set, writing drafts to `plans/modules/NN/tickets/_draft/`.
- Plan owner reviews each draft, edits, `mv`s to live `tickets/`.
- `manifest-lint --rebuild` regenerates manifest entries from frontmatter.
- Final `dag-validate --strict` must pass before any agent claims.

### Phase 1.5 — Scalability validation milestone (Week 2, gated)

Per Codex Decision 2. **No protocol-implementation ticket may go in_progress before this milestone is GREEN.**

Tickets (S/M, hand-written, run on Sonnet/Opus):
- Spike: 10K loopback TCP connections to a httptest.Server. Measure: goroutine count, RSS, GC pause distribution, throughput.
- Spike: 10K UDP connections (DHT-like) on one socket. Measure: same.
- Spike: Throttle correctness — token bucket under fake clock matches captured aria2c per-second byte counts within ±5% (Codex caveat).
- Spike: sync.Pool buffer hit rate at 10K concurrent ops.
- Spike: context cancellation hierarchy correctness (5-deep cancel propagates in <10ms across 10K children).

Exit gate: ≤100ms p99 GC pause, ≤1 GB RSS, throughput within model, throttling within ±5%. If any gate fails → revisit concurrency model (ADR-0002 revision).

### Phase 2 — Foundation (Week 2-4, parallel agents)

Modules 16-logging, 12-disk, 13-net-shared, 15-config, 01-core-engine, 02-uri-parser. 5 Haiku agents + 1 Sonnet reviewer. Gate: all foundation tickets merged before any protocol ticket starts (except RPC).

### Phase 3 — Parallel protocols (Week 4-8)

Modules 03-http, 04-ftp, 11-metalink in parallel. 6 Haiku agents.
Module 05-sftp (path a) runs as a separate stream — `internal/ssh/` is its own 25-ticket subgraph, 3 Sonnet agents.

### Phase 4 — BitTorrent (Week 6-10, overlap)

Modules 06-bittorrent-core, 07-bittorrent-peer, 08-bittorrent-tracker, 09-bittorrent-dht. 8 agents (2 Sonnet, 6 Haiku). Module 10-bittorrent-extensions (uTP, BTv2) is MVP+1 and starts only after BT-v1 is GREEN on conformance.

### Phase 5 — RPC (Week 8-11)

Module 14-rpc-server. 5 agents. Golden-test-first per Codex Decision 3. All 35 methods + 6 notifications complete; AriaNg e2e passing.

### Phase 6 — Integration and ship-1.0 (Week 11-14)

- Conformance scorecard ≥99% in every area.
- 24h soak passes.
- Performance gates met.
- AriaNg + ≥1 third-party RPC client green.
- `make ship-check` exits 0.

**Estimated total:** 12-16 weeks with ~8-12 parallel agents (path b). Path (a) adds ~3 weeks for `internal/ssh/`.

---

## 22. Verification (how YOU verify this plan works)

Once you approve and the master plan is decomposed (Phase 0):

1. **Static validation** — `cd aria2go && plans/tools/orchestrator/manifest-lint --strict && plans/tools/orchestrator/dag-validate --strict`. Both must exit 0.
2. **30 exemplar tickets** — read tickets/T001 through T030. Each must be self-contained: reading just the ticket + listed context files (≤3 of them) must give an agent enough to implement.
3. **Spike implementations** — run Phase 1.5 spikes. Are GC/RSS/throughput within budget?
4. **Walk through one ticket end-to-end** — Pick T020 (bencode decoder). Confirm acceptance criteria are each testable by named Go invocations. Confirm contract surface matches reality.
5. **Run the orchestrator binaries** — `tracking-render --dry-run`, `conformance-score --dry-run`, `adr-check --dry-run`. All exit 0 with sane output.
6. **Trial-fire the LLM expander** — for one module (e.g. 02-uri-parser), feed the SPEC + 3 seed exemplars to ticket-expand, then human-review the drafts. Expansion quality ≥80% (i.e., ≥80% of drafts need only minor edits).

---

## 23. Critical files to be created post-approval

This list is the entry point for Phase 0 (plan-file decomposition). Each item is one Phase-0 deliverable.

**Already created (pre-approval prep, per your direction)**
- `aria2go/source-truth/aria2/` — depth=1 clone of upstream aria2 (~19 MB).
- `aria2go/source-truth/aria2-docs/` — depth=1 clone of aria2 docs site (~24 MB).
- `aria2go/source-truth/beps/` — depth=1 clone of BitTorrent.org BEPs (~2.3 MB).
- `aria2go/source-truth/README.md` — license boundary + hardened ADR-0016 rules + inventory. **Required reading for every agent at boot.**
- `aria2go/ENTRYPOINT.md` — **comprehensive single-entry-point doc for any of the 10-20 concurrent coding agents.** Covers TL;DR, decided policies, repo layout, 8-step loop, concurrent operation rules, token budgets, license boundary, source-truth usage, ticket anatomy, manifest schema, anti-patterns, do/don'ts, "spec gap" exit valve, orchestrator runbook for swarm launch (waves 1-5), CI gates, pre-flight checklist, glossary, pseudocode appendix. **Read this first if you're a new agent.**
- `aria2go/PROMPT_TEMPLATES.md` — **copy-paste-ready kickoff prompts** for the human/orchestrator to spawn agents. Includes: canonical implementer prompt (with `<<<AGENT_ID>>>`, `<<<TIER>>>`, `<<<MODEL>>>`, `<<<STOP>>>` placeholders), wave-2/3/4 variants, separate spec-author prompt (different role under author-separation rule), orchestrator prompt, module-steward reviewer prompt, shell snippets for spawning 1 / 10 / 20 concurrent agents, verification checklist, and a tier→model lookup table.

**Root**
- `aria2go/AGENTS.md` — content from §18 (must reference ENTRYPOINT.md, source-truth/README.md, and ADR-0016 hardened rules). ENTRYPOINT.md is the deep version; AGENTS.md is the always-cached ≤1500-token contract.
- `aria2go/README.md` — short overview, links to plans/PLAN.md.
- `aria2go/LICENSE` — Apache-2.0.
- `aria2go/NOTICE` — path b only; BSD-3 attributions.
- `aria2go/CHANGELOG.md` — initial.
- `aria2go/go.mod` — `module github.com/smartass08/aria2go; go 1.24; toolchain go1.25.x`.
- `aria2go/Makefile` — build, test, vet, lint, cross-compile, ship-check.
- `aria2go/.github/workflows/ci.yml` — matrix per §12.10.

**plans/ root**
- `aria2go/plans/PLAN.md` — extracted master plan, ≤10K tokens.
- `aria2go/plans/manifest.json` — initial 30 tickets + policy block.
- `aria2go/plans/manifest.schema.json` — JSON Schema.
- `aria2go/plans/manifest.lock` — empty file with comment.
- `aria2go/plans/TRACKING.md` — initial, auto-regenerated.
- `aria2go/plans/CONFORMANCE.md` — initial, auto-regenerated.
- `aria2go/plans/GLOSSARY.md` — aria2 terms.

**plans/decisions/** (22 ADRs from §2)
- ADR-0001-library-policy.md (with alternative-b inline)
- ADR-0002 through ADR-0022 (one file each).
- INDEX.md — id, status, supersedes table.

**plans/contracts/**
- interfaces.md — engine ↔ BT contracts surface (per ADR-0004).
- error-codes.md — 0..32 mapping.
- rpc-methods.md — all 35 methods + 6 notifications.
- config-keys.md — all ~140 options.
- wire-formats.md — bencode, magnet, .torrent, .metalink, ws frame, .session.

**plans/byte-compat/**
- session-format.md — exact line ordering, gzip, escaping.
- rpc-method-table.md — per-method params/results/errors.
- cli-flags-table.md — all flags with short/alias/default/grammar.
- dht-file-format.md — binary format.
- conf-file-format.md — comment syntax, include directive.

**plans/modules/NN-name/** for NN in 00..17
- SPEC.md (template §14.5; content from §5).
- AGENTS.md (template §19).
- tickets/T001-... .md through tickets/Txxx-... .md (~190-215 total).

**plans/test-plans/**
- conformance-matrix.md — area × test-case cells (per §12.15).
- corpus/ — license-clean .torrent/.metalink (regenerated, not copied).
- fuzz-targets.md — all `Fuzz*` per §12.5.
- interop-aria2c.md — dualrun harness spec.
- rpc-goldens.md — capture procedure + locations under `test/golden/rpc/`.
- perf-bench.md — benchmarks + budgets per §12.8.
- scalability-validation.md — Phase 1.5 spikes per §21.

**plans/tools/orchestrator/** (each ~200 LOC, pure stdlib Go)
- manifest-lint/main.go
- dag-validate/main.go
- claim-sweep/main.go
- tracking-render/main.go
- conformance-score/main.go
- adr-check/main.go
- ticket-expand/main.go (LLM expander harness — wraps `os/exec` to the local LLM)
- internal/ — shared lib (manifest schema parse, token counting, lock helpers)

**test/conformance/**
- Dockerfile.aria2c — pinned aria2c 1.37.0.
- aria2cref/aria2cref.go — Run() helper.
- dualrun/dualrun.go — DualResult abstraction.
- rpc/methods.go — 35-method enumeration.
- cli/extract_flags.go — flag extraction.

**test/golden/** (captured Phase 0)
- rpc/<method>/<case>.json — 35×6 = 210 RPC fixtures.
- cli/help.txt, version.txt, conf/*.conf.
- sessions/*.session (gzip and plain).
- bt/*.torrent (regenerated, not copied).
- metalink/*.xml (RFC 5854 examples, license-clean).

---

## 24. Open questions for you (please address at ExitPlanMode review)

Each item now includes Codex's opinionated recommendation (from the second review pass on 2026-05-18). The PLAN file's defaults are unchanged from the first pass; Codex's recommendation is shown as a "Codex recommends:" line so you can compare. **You decide.**

1. **Library policy refinement (ADR-0001).** Path-a strict stdlib adds ~5,500 LOC of SSH + ~25 tickets + ~10 weeks single-eng effort + crypto-correctness risk.
   - (a) **Keep path (a)** as you originally chose. Plan is written for this; SSH-from-scratch is its own work-stream.
   - (b) **Flip to path (b)** — adopt the four-package x/* shortlist (`x/sys`, `x/crypto/ssh`, `x/crypto/ssh/agent`, `x/term`, `x/net/idna`). Saves ~6,000 LOC and ~3 weeks of parallel-agent time.

   **Codex recommends: PICK PATH (b) AND PUSH BACK on the original answer.** Verbatim: *"Reimplementing SSH transport, auth, host-key handling, ciphers, MACs, channels, and agent interaction is not 'purity,' it is a cryptographic correctness liability plus a schedule tax. The curated list is defensible because golang.org/x/* is Go-team-maintained, BSD-3-licensed, widely used, and close enough to the Go platform ecosystem to preserve the spirit of a pure-Go rewrite without importing arbitrary third-party application frameworks. … Freeze the exact allowlist and module versions, require NOTICE attribution, and make ADR-0022 mandatory before SFTP work begins, because x/crypto/ssh handles SSH but does not magically guarantee aria2-compatible SFTP behavior, error text, auth ordering, proxy interaction, or host-key semantics. This is a clear case where the architect should tell the user the earlier preference was under-informed and recommend changing it."*

   To switch: edit ADR-0001 status from Accepted to Superseded, mark ADR-0001b Accepted, drop `internal/ssh/` from §3, swap `internal/console/readline.go` to the thin wrapper. ADR-0022 (SSH/SFTP compat tests) becomes mandatory.

2. **uTP scope (ADR-0019).** Default is MVP+1, gated `--enable-utp=false`. Flip to MVP if you want day-1 parity with aria2.

   **Codex recommends: DEFER (keep default).** Verbatim: *"BEP 29 is not just another socket mode: LEDBAT congestion control, timestamp behavior, retransmission dynamics, packet sizing, delay sampling, and lossy-network behavior are all places where a plausible implementation can be interoperable in happy-path tests yet pathological in real swarms. Shipping day-1 uTP only to match the flag default would create a high-risk networking subsystem before the BitTorrent v1 core is green. The better compatibility posture is to accept and document the flag surface early, mark the behavioral gap explicitly in conformance, and flip the default only after libutp trace validation passes. For a feature clone, 'temporarily honest and gated' is less damaging than 'nominally present but congestion-hostile.'"*

3. **Reference aria2 version (ADR-0020).** Default 1.37.0. Confirm.

   **Codex recommends: CONFIRM 1.37.0.** Verbatim: *"1.37.0 remains the latest upstream release, so the plan's default is sound. The project needs one stable behavioral oracle for CLI, RPC, config, session, and protocol conformance; chasing distro patchsets or historical versions would multiply fixture ambiguity without improving clone fidelity. … the behavioral identity should be 'upstream aria2 1.37.0 plus explicitly documented packaging or build deviations,' not 'whatever a distro happens to patch.' Future upstream releases should require a bump ADR and a golden regeneration pass rather than silently changing the target."*

4. **BitTorrent v2 hybrid (BEP 52).** Default MVP+1 (after v1 is GREEN). Confirm.

   **Codex recommends: CONFIRM MVP+1, with an explicit Phase-0 investigation ticket.** Verbatim: *"The plan itself notes aria2's BEP 52 support is uncertain and needs a source dive, so treating BTv2 as day-1 parity would be speculation rather than conformance planning. BTv2 is also structurally invasive: merkle trees, file trees, hybrid metadata, magnet metadata handling, and hash selection cut across torrent parsing, storage, verification, and peer behavior — exactly the kind of subsystem that should not be mixed into the first BT-v1 stabilization loop. The right move is an explicit Phase 0 source-truth investigation ticket that determines whether aria2 1.37.0 actually supports BEP 52 and which subset; only then should BEP 52 become either a required parity workstream or a documented waiver."*

   **Action:** Add Phase-0 ticket `T-INV-001: Investigate aria2 1.37.0 BEP 52 support` (S complexity, owner: plan owner, no implementation, reads `source-truth/aria2/` and writes a finding to `plans/byte-compat/btv2-support.md`).

5. **uTP cross-validation source.** Default: captured `libutp` packet traces.

   **Codex recommends: USE CAPTURED libutp PACKET TRACES (keep default), supplement with quarantined live-interop soak later.** Verbatim: *"Live swarm tests are useful later but are nondeterministic, depend on peer mix and network conditions, and make failures hard to reproduce. Cgo-embedding libutp in the test rig gives stronger oracle proximity but violates the project's no-cgo operational discipline and adds build complexity even in a test-only form. Packet traces give the team deterministic fixtures for SYN/state transitions, timestamp and delay behavior, ACK strategy, retransmission, FIN teardown, and congestion response under controlled loss and jitter. … Supplement traces with a later quarantined live-interop soak, but the release gate should be deterministic trace replay first."*

6. **Phase 1 expander LLM.** Default: same model class as the implementation agents (Sonnet for expansion).

   **Codex recommends: USE A STRONGER MODEL (Opus / GPT-5) FOR EXPANSION ONLY**, wrapped in structured prompts + deterministic validators. Verbatim: *"Ticket expansion is a leverage point: mistakes there become hundreds of downstream implementation errors, duplicated ownership, missing contract surfaces, or false unblock states. A deterministic generator alone can enforce schemas but cannot reliably decompose nuanced protocol work, infer test prerequisites, or preserve cross-module dependencies from the master plan. Using the same model class as implementation agents is acceptable for ordinary tickets, but expansion is architecture translation, and it deserves the best reasoning model available for that bounded pass. The cost difference is trivial compared with a multi-agent rewrite, provided the output is machine-checked by manifest linting, DAG validation, token limits, and human review for compatibility-touching tickets."*

7. **Single-tenant vs multi-agent ownership.** Default: any agent may claim any unblocked unclaimed ticket; tickets touching CLI/RPC/session/config get a human-review gate (ADR-0021).

   **Codex recommends: KEEP OPEN-CLAIM (default) + add lightweight "module steward" reviewer affinity.** Verbatim: *"Exclusive per-module agent affinity sounds tidy but creates bottlenecks when one agent stalls or accumulates backlog in a high-dependency area like BitTorrent or RPC. The open-claim model matches the plan's ticket-DAG structure and lets throughput follow available capacity, while ADR-0021's contract-surface gate protects the parts where arbitrary edits are most dangerous. Add module 'stewards' for review continuity rather than implementation ownership — one reviewer tracks BitTorrent, one tracks RPC/config/session, one tracks platform/networking — but any agent may implement unblocked work if the ticket contract is approved. That gives specialization where it matters without turning the schedule into a set of single-agent queues."*

   **Action (if accepted):** Add a `stewards` block to `plans/manifest.json` policy field listing the three steward roles. ADR-0021 unchanged; ADR-0024 (new) defines steward responsibilities + review SLAs.

**Other clarifications welcome:** RFC inclusion in `source-truth/rfcs/`; any custom telemetry or anti-abuse beyond what the plan covers; deployment artifacts (binary release pipeline, signed builds).

---

## 25. Source-truth folder strategy (per your direction + Codex hardening)

**Status:** Created 2026-05-18 (clones already exist at `/Users/smartass08/projects/aria2go/source-truth/`).

You asked me to "clone things which other llm might need to understand while creating rewrite" and keep them in `source-truth/`. Done. The folder gives coding agents — especially the small-context ones (deepseek-coder, qwen-coder, gpt-5-mini, claude-haiku) — offline access to canonical references that they'd otherwise have to fetch via WebFetch. Codex then flagged a contamination risk because small-context LLMs are prone to accidentally paraphrasing source they've read; ADR-0016 was hardened and ADR-0023 added in response.

### 25.1 What's currently in `source-truth/`

| Path | Origin | License | Size | Purpose |
|---|---|---|---|---|
| `source-truth/aria2/` | https://github.com/aria2/aria2 (depth=1 clone, master at clone time) | GPLv2+ (with OpenSSL exception) | ~19 MB, 951 .cc/.h files | Behavioral reference for the C++ original |
| `source-truth/aria2-docs/` | https://github.com/aria2/aria2.github.io | (verify CC-BY per page) | ~24 MB | User manual, RPC docs, CLI reference |
| `source-truth/beps/` | https://github.com/bittorrent/bittorrent.org | Public spec text | ~2.3 MB | BitTorrent Enhancement Proposals |
| `source-truth/README.md` | this plan | Apache-2.0 (ours) | ~6 KB | License boundary + hardened rules + inventory |

Total ~45 MB. Not in the build tree. Not shipped. `make ship-check` verifies absence in any release artifact.

### 25.2 What Phase 0 will add

- `source-truth/rfcs/` — selected RFCs (HTTP 7230-7235, HTTP/2 7540, TLS 1.3 8446, FTP 959, FTPS 2228, SSH 4251-4254, WebSocket 6455, Digest 7616, Content-Disposition 6266, Metalink 5854, SOCKS5 1928, NAT-PMP 6886, PCP 6887). Public-domain text. Fetched via `curl https://www.rfc-editor.org/rfc/rfcNNNN.txt`. ~5-10 MB.
- Possibly: a *very small* selection of aria2 test fixtures that are clearly license-permissive (evaluated case-by-case). Default position: regenerate fresh fixtures, do not copy.

### 25.3 How agents use `source-truth/` (codified in `AGENTS.md` and per-ticket guidance)

**The one-directional flow:**

```
source-truth/  →  plans/byte-compat/*.md  →  ticket  →  implementation
   (GPL)          (English, ours)            (specs)     (code, Apache)
```

Implementer agents read **only** the English spec embedded in the ticket plus the ticket's `context_files`. They do not read `source-truth/`. The plan owner (a human or, for low-risk areas, a designated specification-author agent run in a separate context) consults `source-truth/`, produces the English spec in `plans/byte-compat/`, and updates the ticket. Author separation is enforced by ADR-0016 rule 4 and audited by `plans/tools/orchestrator/adr-check`.

### 25.4 CI scanner gate (per ADR-0023)

`plans/tools/orchestrator/adr-check --source-truth` runs on every PR. Failures per ADR-0016 rule 5 heuristics:

- GPL header strings present in any added file under `internal/`, `pkg/`, `cmd/aria2go/`.
- Distinctive aria2 symbols (e.g., `DownloadEngine::`, ~50 enumerated) detected verbatim.
- Token-level diff similarity >30% between any added function and any function under `source-truth/aria2/src/`.
- Top-K most distinctive aria2 comments fingerprinted; verbatim matches flagged.

Conservative thresholds; false positives reviewed by a human.

### 25.5 Refreshing `source-truth/`

The pinned aria2 reference version is 1.37.0 (ADR-0020). To refresh on a future bump:

```bash
cd source-truth/
rm -rf aria2 && git clone --depth=1 --branch release-1.38.0 https://github.com/aria2/aria2.git aria2
```

Document version bumps in a new ADR (e.g., `ADR-0024-aria2-v1.38-bump.md`) with a regeneration pass on all goldens in `test/golden/`.

### 25.6 What this changes downstream

- The 30 exemplar tickets written in Phase 0 reference behavior **only** by citing English specs in `plans/byte-compat/`, never by quoting C++.
- The `ticket-expand` LLM expander receives the SPEC and the seeds as inputs, plus `plans/byte-compat/` excerpts — never raw `source-truth/aria2/`.
- The conformance dual-run rig still uses the pinned `aria2c` Docker binary; `source-truth/aria2/` is for *reading*, the running binary is for *testing*.
- A new orchestrator binary `plans/tools/orchestrator/spec-author` is the only tool allowed to take `source-truth/` paths as input arguments. It produces files only under `plans/byte-compat/`. Agents do not run it; the plan owner does.

---

## Appendix A — aria2 feature surface (catalog)

Summary of research findings. Full enumeration lives in `plans/contracts/` post-approval.

**Protocols:** HTTP/1.0/1.1/2 with TLS (`PREF_MIN_TLS_VERSION` configurable, SSLv3/TLSv1.0-1.3); FTP/FTPS active+passive; SFTP via libssh2 (pubkey+password, host-key MD5 verify); BitTorrent v1 (DHT, PEX, UDP+HTTP+HTTPS trackers, web-seeding, magnet, LPD, BEP 9 metadata exchange); Metalink v3 + v4 (RFC 5854); BTv2 (BEP 52) hybrid TBD in aria2.

**BitTorrent BEPs (aria2 source):** Confirmed BEP 3, 5, 6, 10, 11, 12, 15, 17, 19, 20, 23, 27, 29 (uTP), 32, 33, 41, 47. Likely BEP 7, 9, 22. BEP 52 hybrid uncertain.

**Metalink:** XML v3 + v4 (namespace), hashes (MD5, SHA-1, SHA-224, SHA-256, SHA-384, SHA-512, SHA-3, BLAKE2/3 via mhash), signature (RSA/DSA) verification — we waive sig verification.

**RPC (35 methods + 6 notifications):** Full table in §9.7.

**CLI (~140 flags):** General (30), HTTP/FTP (25), Proxy (12), BitTorrent (35), Metalink (6), RPC (8), Speed (4), Advanced (20+). Includes `--help=#all` category dump.

**Config (aria2.conf):** key=value, `#` comments, `include` directive, quoted values, repeated keys for accumulators (e.g. `header=...`).

**Session (aria2.session):** Plain text or gzip. Per-RequestGroup section with `\t<key>=<value>` lines. SHA-1 over file for integrity.

**Misc:** input file (-i), shell hooks, async DNS (c-ares — we use net.Resolver), gzip/deflate, cookies (Netscape + sqlite — we Netscape only), .netrc, CA store, client certs, IPv6, UPnP, NAT-PMP, proxies, checksum verify, file alloc (none/prealloc/falloc/trunc), disk cache, piece selectors (default/inorder/random/geom).

## Appendix B — Codex review verdicts (consolidated, two passes)

### Pass 1 — Architectural decisions (verdicts)

| Decision | Codex verdict | Mitigation applied |
|---|---|---|
| 1 — Library policy | SOUND (for path b) | Path b option documented; if user adopts, add SSH/SFTP compat tests + freeze versions (ADR-0022) |
| 2 — Concurrency model | RISKY | Phase 1.5 Scalability Validation Milestone (§21) |
| 3 — RPC stack | RISKY | Golden-test-first (§9 + §12.13); goldens captured Phase 0 |
| 4 — BT engine boundary | RISKY | Explicit typed interfaces in `internal/contracts/` (ADR-0004 revised, §5.31) |
| 5 — Session storage | SOUND | Cross-OS round-trip in CI gate; preserve unknown lines in `Entry.Unknown` |
| 6 — Scheduler model | SOUND | Scheduler SPEC has explicit `## Invariants` block enumerating 5 invariants (§7.3) |
| 7 — 3-tier plan structure | SOUND | ADR-0021 `## Contract Surface` field mandatory; human-review gate on compat-touching tickets |

### Pass 2 — Open-question recommendations (Codex's picks; final call is yours in §24)

| Open Q | Codex recommendation | Mitigation / action in plan |
|---|---|---|
| Q1 Library policy | **PICK path (b); push back on original** | ADR-0001 documents both; §24 Q1 shows the verbatim recommendation |
| Q2 uTP scope | **Defer (keep MVP+1 default)** | ADR-0019 holds; `--enable-utp=false` until libutp-trace validated |
| Q3 Reference aria2 version | **Confirm 1.37.0** | ADR-0020 holds |
| Q4 BT v2 (BEP 52) | **MVP+1; add Phase-0 investigation ticket T-INV-001** | Added to §24 Q4 action |
| Q5 uTP validation source | **Captured libutp packet traces (primary)** + later quarantined live-interop soak | §21 Phase 1.5 carries the gate |
| Q6 Phase 1 expander LLM | **Stronger model (Opus / GPT-5) for expansion only, with deterministic validators** | §15.4 ticket-expand updated to allow model class override |
| Q7 Agent ownership | **Open-claim + module stewards (review continuity)** | §24 Q7 proposes new ADR-0024 (stewards) |

### Pass 2 — New risk surfaced

| Risk | Source | Mitigation |
|---|---|---|
| Source-truth folder contamination (small-context LLMs paraphrasing aria2 source they've read) | Codex Pass 2 | ADR-0016 hardened with 6 explicit enforcement rules + author separation audit; new ADR-0023 (source-truth boundary + CI scanner gate); §25 documents the strategy; `source-truth/README.md` now exists with the rules |

## Appendix C — Quick reference

- **Plan file** (this master plan, plan-mode editable): `/Users/smartass08/.claude/plans/we-want-to-rewrite-fancy-glade.md`
- **ENTRYPOINT.md for agents** (created pre-approval, per your direction): `/Users/smartass08/projects/aria2go/ENTRYPOINT.md` — the single doc that any of 10-20 concurrent coding agents (Haiku, deepseek-coder, qwen-coder, Sonnet, gpt-5-mini, Opus) reads first.
- **PROMPT_TEMPLATES.md** (created pre-approval): `/Users/smartass08/projects/aria2go/PROMPT_TEMPLATES.md` — copy-paste-ready kickoff prompts for implementer / spec-author / orchestrator / reviewer agents, plus shell snippets for spawning 10–20 concurrently.
- **Project root** (created Phase 0): `/Users/smartass08/projects/aria2go/`
- **Source-truth folder** (created pre-approval, per your direction):
  - `/Users/smartass08/projects/aria2go/source-truth/aria2/` (~19 MB)
  - `/Users/smartass08/projects/aria2go/source-truth/aria2-docs/` (~24 MB)
  - `/Users/smartass08/projects/aria2go/source-truth/beps/` (~2.3 MB)
  - `/Users/smartass08/projects/aria2go/source-truth/README.md` (license boundary + ADR-0016 hardened rules)
- **Manifest** (created Phase 0): `aria2go/plans/manifest.json`
- **Orchestrator binary** (created Phase 0): `aria2go/plans/tools/orchestrator/`
- **Reference aria2 version**: 1.37.0 (`debian:bookworm-slim` package `1:1.37.0-1+b1`)
- **Reference Go version**: 1.24 (target), 1.25.x (toolchain)
- **License**: Apache-2.0 (clean-room rewrite)
- **Module path**: `github.com/smartass08/aria2go`
- **Codex thread (Pass 2)**: agentId `a35bcd82c99aec82e` (resumable via SendMessage)
- **Codex thread (Pass 1)**: agentId `ad0f5660d608a0222` (resumable)

---

*End of master plan. After ExitPlanMode approval, Phase 0 decomposes this single file into the file tree under §23, and the source-truth/ folder (already created pre-approval) becomes the offline reference for all spec-author work.*
