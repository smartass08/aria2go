# 16-logging-metrics — Agent Contract

## What this module is

`internal/log/` — the canonical logging substrate for aria2go. ~400 LOC across 3 tickets (T031–T033). All other modules depend on this.

## Rules

1. **slog only.** Everything passes through `log/slog`. No `fmt.Print`, no `log.Print`, no custom logging paths.
2. **ADR-0011 is law.** Two handlers only: classic (aria2 format) and json (structured). No third handler.
3. **No panics.** Log write failures are silently discarded (slog contract). Errors during `New()` are returned.
4. **Concurrency.** Handler `Handle()` and `WithAttrs()`/`WithGroup()` must be safe for concurrent calls. Use `sync.Mutex` on underlying `io.Writer` if the caller hasn't provided a safe writer.
5. **Buffer pools.** Classic handler MUST use `internal/ioutilx.Pool4K` for message assembly. Return buffers after `Write` via `defer`.
6. **Source file.** `runtime.Caller(skip)` skip MUST be calibrated per call depth. Test that `log.Info("msg")` records the caller's file:line, not log.go.
7. **LevelNotice.** Map `LevelNotice` to `slog.LevelInfo` with `aria2_level=notice` attribute. Classic handler formats it as `[NOTICE]`. JSON handler adds the key.
8. **Format fidelity.** Classic output is golden-tested against captured aria2c output. Any format deviation requires a golden update and SPEC amendment.
9. **No dynamic reconfiguration.** `SetLogger` is test-only. Production bootstrap calls `New()` once; the return value is injected into engine.
10. **Tickets are ordered.** T031 → T032 → T033. Each depends on the prior. T031 defines Level/Config/New(); T032 implements classic handler; T033 implements json handler.

## Tests

- Golden files live in `test/golden/log/` — captured from aria2c 1.37.0.
- Every handler test must include a concurrent-write scenario (≥10 goroutines).
- Fuzz: `FuzzParseLevel` for level string parsing.

## Dependencies

- `log/slog` (stdlib), `internal/core`, `internal/ioutilx`.
- Imports NOT allowed: `fmt` (for output), `log` (stdlib `log` package).
- Use `sync/atomic` for the global logger pointer.
