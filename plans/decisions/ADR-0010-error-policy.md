# ADR-0010 — Error Policy

## Status
Accepted

## Date
2026-05-19

## Supersedes
None

## Related
- plans/contracts/error-codes.md
- ADR-0003 (RPC framework — error mapping to RPC response)

## Context
aria2 uses integer exit codes (0..32) and distinct error behaviors per subsystem. Go's error handling idiom (explicit `error` values, `errors.Is`/`errors.As`, `%w` wrapping) must be mapped onto aria2's error code model cleanly. Panics in library code are unacceptable for a long-running daemon.

## Decision
**Errors are explicit `error` values.** Define `core.ErrorCode` (int, 1..32 matching aria2's exit codes) as the *display* code. Each package may define sentinel error values (e.g., `ErrNotFound`, `ErrAuthFailed`) with an associated `ErrorCode`. Display code is extracted via `errors.Is`/`errors.As`.

**Wrap with `%w`** — always use `fmt.Errorf("...%w", err)` to preserve the error chain for `errors.Is`.

**No panics in library code.** Only `main` may call `os.Exit`. Library functions return errors; callers propagate or handle them.

Use `errors.Join` for multi-error completion scenarios (e.g., failed multi-tracker tier where each tracker may produce its own error).

## Consequences

### Positive
- Idiomatic Go: `errors.Is`/`errors.As` for typed error checks; `%w` for wrapping.
- Explicit error code mapping gives RPC and CLI exit handlers a single source of truth.
- No panics = no unexpected crashes from library code.

### Negative
- Every package must define its own `*pkg.Error` type with `Code` field — adds boilerplate.
- `errors.Join` returns `error`; extracting individual errors requires `interface{ Unwrap() []error }` — slightly less ergonomic than a custom multi-error type.

### Neutral
- Error code enum (0..32) is defined once in `internal/core/` and used everywhere.

## Compliance Notes
- Tickets affected: All.
- Modules affected: All — every package that defines errors.
- Detection: `go vet` catches missing `%w`; linter rules catch `panic()` in non-main packages.
