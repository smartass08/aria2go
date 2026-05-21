# ADR-0001 — Library Policy

## Status
Accepted

## Date
2026-05-19

## Supersedes
None (initial ADR)

## Related
- ADR-0008 (x/* allowlist policy — empty under this decision)
- ADR-0022 (SSH/SFTP compatibility tests — deferred under this decision)
- ADR-0016 (clean-room process — SSH is our own implementation)

## Context
aria2go is a 100% feature-clone of aria2 (GPLv2+) in pure Go, Apache-2.0 licensed. The project must decide what third-party packages — if any — implementation code is allowed to import. This is the single highest-leverage decision: it determines whether `internal/ssh/` exists (path a) or `golang.org/x/crypto/ssh` is imported (path b), a delta of ~6,000 LOC and ~25 tickets.

The alternatives:

- **Path (a) — Strict stdlib-only:** Use only Go standard library packages. No third-party imports. No `golang.org/x/*`. SSH is written from scratch under `internal/ssh/`. Syscalls go through the `syscall` package directly. IDN/punycode is hand-rolled. Terminal raw mode uses syscall-level termios on Unix and SetConsoleMode on Windows.

- **Path (b) — Curated x/* shortlist:** Four `golang.org/x/*` packages are allowed: `x/sys`, `x/crypto/ssh`, `x/crypto/ssh/agent`, `x/term`, `x/net/idna`. All other third-party imports remain forbidden. Saves ~6,500 LOC and ~30 tickets.

Codex subagent reviewed both paths and strongly recommended path (b), noting that "treating `golang.org/x/*` as 'effectively stdlib' is directionally reasonable" and that SSH from scratch creates "a major compatibility surface" with risks around SFTP edge cases, host-key handling, and auth ordering.

## Decision
**aria2go uses only the Go standard library. No third-party imports of any kind, including no `golang.org/x/*`.** SSH is implemented from scratch under `internal/ssh/`. Syscalls go through the `syscall` package directly. IDN/punycode is hand-rolled under `internal/netx/idn.go`. Terminal raw mode uses syscall-level termios on Unix and SetConsoleMode on Windows.

The human plan owner selected path (a) over Codex's recommendation for path (b), prioritizing zero external dependencies, full auditability, and complete control over the SSH implementation's behavior.

## Consequences

### Positive
1. Zero external dependencies — go.mod stays minimal (no require block under path a); no supply-chain risk from x/* version bumps.
2. Full auditability — every line of code is project-authored; no need to understand third-party library behavior to debug compat issues.
3. Complete control over SSH implementation — SFTP edge cases, host-key handling, auth ordering, proxy interactions, and error messages can be tailored to exactly match aria2's behavior without fighting x/crypto/ssh abstractions.
4. Simplified CI and attribution — no NOTICE file needed; no license compatibility review; `adr-check` is a simple regex scan (disallow any non-stdlib import).
5. Educational and extensible — the SSH implementation is valuable reference code for future protocol work.

### Negative
1. `internal/ssh/` adds ~5,500 LOC and ~25 tickets — estimated ~10 weeks single-engineer effort or ~3 weeks across parallel agents.
2. `internal/netx/idn.go` becomes ~150 LOC of RFC 3492 punycode.
3. `internal/console/readline.go` becomes ~400 LOC of termios via syscall + Windows console-mode equivalent.
4. `internal/platform/` adds ~400 LOC handwriting per-OS syscall constants and `syscall.Syscall6` for fallocate (subtle bug risk on minor OS versions).
5. Increased maintenance burden — we own the SSH stack bugs and security patches directly.

### Neutral
1. Module 05-protocol-sftp's SFTP packet handler is ~2,500 LOC under either path (the SSH transport is separate from the SFTP protocol).
2. All other modules (HTTP, FTP, BT, RPC, Metalink) are unchanged — only SSH-touching packages differ between paths.

## Compliance Notes
- **Tickets affected:** All — every ticket's `target_files` must compile under this policy.
- **Modules affected under path (a):** `internal/ssh/` (created), `internal/netx/idn.go` (hand-rolled), `internal/console/readline.go` (termios via syscall), `internal/platform/fs_*.go` (per-OS fallocate via syscall.Syscall6).
- **Modules NOT affected:** All other modules use stdlib only regardless of path.
- **Detection:** `plans/tools/orchestrator/adr-check --source-truth` parses `go.mod` and every `import` block under `internal/`; any path outside the Go standard library fails CI.
- **Test-only exceptions:** Test rigs under `test/rig/` are allowed to import `golang.org/x/crypto/ssh` as an SSH counterparty for SFTP conformance testing (ADR-0007 explicitly permits this — production code stays stdlib-only, test rigs may use x/crypto/ssh as a known-good counterparty).
