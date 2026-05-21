# ADR-0003 — RPC Framework

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- ADR-0010 (error policy)
- ADR-0011 (logging policy)

## Context
aria2 exposes a dual JSON-RPC 2.0 and XML-RPC interface plus a WebSocket transport. Our RPC must be byte-compatible — every method, every parameter shape, every error format, every XML serialization quirk must match aria2 1.37.0. Hand-writing these transports on `net/http` avoids third-party RPC libraries and gives full control over compat edge cases.

Codex flagged this as **RISKY** due to XML-RPC type/fault/datetime conventions and WebSocket framing complexities, recommending golden-test-first.

## Decision
**Hand-write JSON-RPC 2.0, XML-RPC, and WebSocket (RFC 6455) server transports** on top of `net/http`. All transports share a transport-neutral dispatcher with a method registry.

**Auth**: `--rpc-secret` as first positional param (`"token:<value>"`), compared with constant-time compare. Basic auth as fallback. HTTPS via `tlsx.ServerConfig`.

**Mitigation**: Golden-test-first. Before writing the JSON-RPC server, capture aria2c's response for every method (success, error, auth failure, batch, XML serialization, WebSocket framing). Goldens become the test fixture set; tickets reference them by path. See `plans/test-plans/rpc-goldens.md`.

## Consequences

### Positive
- Full control over serialization — can match aria2's exact JSON field ordering, XML namespaces, and datetime encoding.
- Shared dispatcher means method logic is written once for all three transports.
- No dependency on third-party RPC libraries.
- Golden tests provide regression safety for compat-sensitive edge cases.

### Negative
- XML-RPC type system, fault conventions, and datetime encoding are underspecified and require careful replication from captured goldens.
- WebSocket masking, fragmentation, close codes, ping/pong, origin/header behavior, and proxy interactions can fail in subtle ways.
- Auth as first positional param must be extracted before the dispatcher processes method-specific parameters.

### Neutral
- Transport-neutral dispatcher adds ~900 LOC in `internal/rpc/dispatcher/`.

## Compliance Notes
- Tickets affected: All RPC tickets (jsonrpc, xmlrpc, dispatcher, transport, token).
- Modules affected: `internal/rpc/jsonrpc`, `internal/rpc/xmlrpc`, `internal/rpc/dispatcher`, `internal/rpc/transport`, `internal/rpc/token`.
- Detection: golden tests in `test/golden/rpc/`; conformance dual-run against aria2c Docker.
