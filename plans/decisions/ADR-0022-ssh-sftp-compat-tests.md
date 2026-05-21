# ADR-0022 — SSH/SFTP Compatibility Tests

## Status
Deferred (active only under path b)

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0001 (library policy — path a defers this ADR)
- ADR-0007 (test strategy — test rig allowed x/crypto/ssh)

## Context
Under path (a), SSH is implemented from scratch (`internal/ssh/`) and the SSH client used in SFTP conformance tests is `golang.org/x/crypto/ssh` (permitted in `test/rig/` by ADR-0007). Under path (b), `x/crypto/ssh` is the production SFTP transport and its behavior must be validated against aria2's expected SSH/SFTP semantics before any implementation ticket starts.

## Decision
Under path (a) — the current baseline — this ADR is **DEFERRED**. SSH/SFTP compatibility tests are defined when `internal/ssh/` is implemented. At that time, the tests will exercise: host-key callbacks, password auth, publickey auth (RSA + ed25519), authentication-method ordering, proxy interaction (CONNECT through HTTP proxy), and aria2's specific SFTP error messages.

Under path (b), this ADR would be **active**: before any SFTP implementation ticket starts, a dedicated compatibility test must be written exercising the above scenarios. `golang.org/x/crypto/ssh` version must be pinned exactly in `go.mod`.

## Consequences

### Positive
- Under path (a): No premature test design — tests are written when the implementation exists.
- Under path (b): Compatibility tests gate implementation, catching x/crypto/ssh behavior mismatches early.

### Negative
- Under path (a): Compatibility test definition is deferred, creating a gap between SSH implementation and validation.
- Tests exercise SSH-level behavior (host-key callbacks, auth ordering) that is independent of SFTP — this requires the SSH implementation to be complete before SFTP testing begins.

### Neutral
- This ADR's status is a single-line change if path (b) is adopted.

## Compliance Notes
- Tickets affected under path (a): None (deferred). Under path (b): All SSH/SFTP implementation tickets.
- Modules affected under path (a): None. Under path (b): `internal/ssh/`, `internal/protocol/sftp/`.
- Detection: Test files in `test/compat/ssh/` must exist and pass under path (b).
