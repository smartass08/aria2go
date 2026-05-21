# Module 16: Logging & Metrics SPEC

## Package
`internal/log/`

## Responsibility

Logging and metrics substrate for the entire aria2go application. Provides a single `*slog.Logger` instance with a custom `classic` handler for aria2-compatible human-readable output and a `json` handler for structured ops ingestion. All packages log through this module; no package creates its own `slog.Handler` or `slog.Logger`.

## Scope

- `slog.Logger` bootstrap with configurable level and output destination
- Classic handler: emits `YYYY-MM-DD HH:MM:SS.NNN [LEVEL] [FILE:LINE] message` lines matching aria2's log format
- JSON handler: emits structured `slog.Record` as JSON for ops/monitoring
- Level mapping: `--log-level` values (debug, info, notice, warn, error) → `slog.Level`
- Output routing: stdout (console) or file (`--log=FILE`)
- Thread-safe: logger and handlers safe for concurrent use across all goroutines
- Context propagation via `slog`'s standard context-based API

### Out of Scope

- Log rotation (aria2 does not rotate logs; handled externally)
- Metrics collection API (future module; this module's `json` handler is the metrics substrate only)
- Log shipping/forwarding (external tools read the JSON or file output)
- Dynamic log level changes at runtime (aria2 does not support `--log-level` change mid-flight via RPC)
- Rate-limiting or sampling (slog provides `Enabled` which handlers may use for level-gating only)

---

## API Surface

```go
package log

import (
    "context"
    "io"
    "log/slog"
    "time"

    "github.com/smartass08/aria2go/internal/core"
)

// Level is our named log level, mirroring aria2's --log-level values.
type Level int

const (
    LevelDebug  Level = iota // slog.LevelDebug
    LevelInfo                // slog.LevelInfo
    LevelNotice              // slog.LevelInfo + custom attribute
    LevelWarn                // slog.LevelWarn
    LevelError               // slog.LevelError
)

func ParseLevel(s string) (Level, error)

// Config holds logger initialization parameters.
type Config struct {
    Level   Level      // minimum level for classic handler
    Output  io.Writer  // destination (os.Stdout or file); required
    Handler HandlerType
}

type HandlerType uint8
const (
    ClassicHandler HandlerType = iota
    JSONHandler
)

// New creates the root *slog.Logger for the application.
// Called once at startup from cmd/aria2go or pkg/aria2go.
func New(cfg Config) *slog.Logger

// ClassicHandlerConfig provides fine-grained classic format options.
// Defaults produce aria2-compatible output.
type ClassicHandlerConfig struct {
    Level       Level        // minimum level to emit
    Output      io.Writer    // required
    TimeFormat  string       // default: "2006-01-02 15:04:05.000" (matches aria2)
    AddSource   bool         // always true for classic handler
    ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
}

func NewClassicHandler(cfg ClassicHandlerConfig) slog.Handler

// JSONHandlerConfig mirrors slog.HandlerOptions.
type JSONHandlerConfig struct {
    Level       slog.Leveler
    Output      io.Writer
    AddSource   bool
    ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
}

func NewJSONHandler(cfg JSONHandlerConfig) slog.Handler

// Logger returns the configured application logger.
// Returns nil before New() is called.
func Logger() *slog.Logger

// SetLogger replaces the global application logger.
// Primarily for testing; not used in production after bootstrap.
func SetLogger(l *slog.Logger)

// --- Convenience wrappers matching slog patterns ---

func Debug(msg string, args ...any)
func Info(msg string, args ...any)
func Warn(msg string, args ...any)
func Error(msg string, args ...any)
func Log(ctx context.Context, level slog.Level, msg string, args ...any)
func With(args ...any) *slog.Logger
func WithGroup(name string) *slog.Logger

// ErrorAttr returns a standardized slog.Attr from a core.Error.
func ErrorAttr(err error) slog.Attr
```

**`New(cfg Config)`** is the single initialization entry point. It selects the handler based on `cfg.Handler`, wraps it in `slog.New()`, stores it globally, and returns it. The caller (e.g., `cmd/aria2go/main.go`) wires `os.Stdout` or an `*os.File` to `cfg.Output`.

**`ClassicHandlerConfig.TimeFormat`** defaults to `"2006-01-02 15:04:05.000"` — Go's reference time producing `YYYY-MM-DD HH:MM:SS.NNN` with exactly 3 millisecond digits and space separator between date and time (matches aria2c output). No timezone suffix.

The convenience wrappers (`Debug`, `Info`, `Warn`, `Error`) are package-level functions that delegate to the global logger. They avoid importers needing to import `log/slog` themselves. Packages that need structured fields use `log.With("key", val).Info(...)`.

---

## Dependencies

| Package | Role |
|---|---|
| `log/slog` (stdlib) | Logging substrate; Handler interface, Logger, Record, Attr, Level |
| `internal/core` | `core.Error` sentinel errors for structured error attributes |
| `internal/ioutilx` | Buffer pools (`Pool4K`, `Pool16K`) for handler byte buffers in hot paths |
| `os`, `io` | Output destination plumbing |
| `sync`, `sync/atomic` | Concurrency safety in handler implementations |
| `time` | Timestamp formatting in classic handler |
| `runtime` | Source file resolution via `runtime.Caller` in classic handler |
| `path/filepath` | Source file path trimming |

Upward consumers: every module in `internal/` and `cmd/`. This is a leaf in the dependency DAG — nothing below `log`.

---

## Invariants

1. **Single root logger.** Exactly one `*slog.Logger` is created via `New()`. It is stored globally and accessed via `Logger()`. All packages log through it. No package calls `slog.New()` directly.

2. **Classic format stability.** Output of the classic handler is byte-equivalent to aria2c's log format for the same messages. Format is `YYYY-MM-DD HH:MM:SS.NNN [LEVEL] [FILE:LINE] message` followed by `\n`. Extra attributes are appended in `key=value` pairs.

3. **Level ordering.** `LevelDebug < LevelInfo < LevelNotice < LevelWarn < LevelError`. The classic handler filters messages below the configured level. `LevelNotice` maps to `slog.LevelInfo` with a `"aria2_level=notice"` attribute set by the classic handler's `ReplaceAttr` (ADR-0011).

4. **Thread safety.** `slog.Logger` is inherently safe for concurrent use. Handler implementations must also be safe: shared state (buffers, writers) is serialized. File output uses a single `io.Writer` wrapped with a mutex if the underlying writer is not goroutine-safe.

5. **No panics.** Library code never panics. Handler write errors are silently dropped (matching slog's contract). Configuration errors are returned as `error` values.

6. **Zero allocation in disabled levels.** When the configured level filters out a message, no allocation occurs beyond slog's own `Enabled()` check.

7. **Source file correctness.** When `AddSource` is true (always for classic, default for json), `runtime.Caller` identifies the actual caller, not the convenience wrapper. The call-skip is calibrated so that `log.Info("msg")` records the correct source location.

8. **Context propagation.** `log.Log(ctx, ...)` passes context to slog's `Handle()` via `slog.NewRecord`. Handlers that implement `slog.HandlerWithContext` may extract context values (future use).

---

## Concurrency Contract

- `slog.Logger` is safe for concurrent use by multiple goroutines without external synchronization.
- Handler implementations (`ClassicHandler`, `JSONHandler`) serialize writes to the underlying `io.Writer` via an internal mutex when the writer is `os.Stdout` or an `*os.File`. If the caller provides a custom goroutine-safe `io.Writer`, the mutex can be bypassed.
- `New()` is not safe for concurrent calls with `Logger()` or convenience functions. It must be called once before any logging occurs (startup serialization point).
- `SetLogger()` is not safe for concurrent calls. Intended for test setup only.
- Package-level convenience functions (`Debug`, `Info`, etc.) are safe for concurrent use once `New()` has completed.
- Global state: a single `atomic.Pointer[slog.Logger]` holds the current logger. Nil until `New()` completes. `Logger()` returns nil if logging is not initialized.

---

## Error Handling

Handler write errors follow slog's contract: errors are silently discarded. The rationale is that a logging failure should not abort the application's primary work (downloading files).

Startup errors from `New()` are returned as standard Go errors:
- `ErrInvalidLevel` — `ParseLevel` received an unrecognized string
- `ErrNoOutput` — `Config.Output` is nil

These are wrapped in `*core.Error` with an appropriate `ErrorCode` from `plans/contracts/error-codes.md`.

---

## Configuration

### `--log-level` values → `Level`

| CLI Value | Log Level | slog.Level | Notes |
|---|---|---|---|
| `debug` | `LevelDebug` | `slog.LevelDebug` (-4) | Verbose; includes all messages |
| `info` | `LevelInfo` | `slog.LevelInfo` (0) | Default in aria2 |
| `notice` | `LevelNotice` | `slog.LevelInfo` (0) | Extra attribute on classic handler |
| `warn` | `LevelWarn` | `slog.LevelWarn` (4) | Warnings and above |
| `error` | `LevelError` | `slog.LevelError` (8) | Errors only |

`ParseLevel` is case-insensitive and trims whitespace. Unrecognized values return `ErrInvalidLevel`.

### `--log=FILE`

When `--log` is specified, `Config.Output` is set to an `*os.File` opened with `os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644`. When `--log` is absent or `-`, `Config.Output` is `os.Stdout` and `Handler` is `ClassicHandler`.

When `--log` is a file, `Handler` is `JSONHandler` (per ADR-0011: structured JSON for file output, classic for console).

### `--console-log-level`

Sets the classic handler's minimum level for console output. When `--log` is also set, the console handler and file handler may have different levels. This requires two separate `slog.Logger` instances or a multi-handler that routes to both with distinct level gating. If aria2 semantics require only one logger with one level, this dual-output scenario is gated behind a later ticket.

---

## Handler Specifications

### Classic Handler

**Format:**
```
YYYY-MM-DD HH:MM:SS.NNN [LEVEL] [FILE:LINE] message key=value key=value\n
```

- `YYYY-MM-DD` — 4-digit year, 2-digit zero-padded month, 2-digit zero-padded day
- `HH:MM:SS.NNN` — 24-hour time with exactly 3 millisecond digits
- `[LEVEL]` — uppercase level name padded to 5 characters right-justified: `[DEBUG]`, `[INFO ]`, `[NOTICE]`, `[WARN ]`, `[ERROR]`
- `[FILE:LINE]` — source file name (basename only) and line number, separated by colon
- `message` — the log message string
- `key=value` — additional structured attributes in `string(key)=fmt.Sprint(value)` format, space-separated

**Example:**
```
2026-05-19 02:30:45.123 [INFO ] [engine.go:142] Download started gid=2089b05ecca3d829 status=Active
```

**Implementation notes:**
- Uses `time.Now().Format("2006-01-02 15:04:05.000")` for timestamp
- `runtime.Caller(skip)` with skip calibrated so convenience wrappers don't appear as source
- Source file is `filepath.Base(file)` for compactness
- Buffer from `ioutilx.Pool4K` for message assembly; returned after `Write`
- Attributes are sorted by key for deterministic output
- Groups are flattened with `.` separators: `group.key=value`
- Empty/nil attributes are omitted

### JSON Handler

Emits one JSON object per line (JSON Lines format). Uses `encoding/json` with `slog.HandlerOptions` patterns.

**Format (per line):**
```json
{"time":"2026-05-19T02:30:45.123Z","level":"INFO","source":{"file":"engine.go","line":142},"msg":"Download started","gid":"2089b05ecca3d829","status":"Active"}
```

**Implementation notes:**
- Timestamps in RFC 3339 with millisecond precision and UTC zone
- Level names match slog convention: `DEBUG`, `INFO`, `WARN`, `ERROR`; `NOTICE` maps to `INFO` with `"aria2_level":"notice"` attribute
- Source is a nested object `{"file":"...","line":N}` when `AddSource` is true
- Each message is one JSON line terminated by `\n`
- Pretty-printing is NOT supported (aria2 JSON format is compact)

---

## Log Levels

### Level type

```go
type Level int

const (
    LevelDebug  Level = -4  // maps to slog.LevelDebug
    LevelInfo   Level = 0   // maps to slog.LevelInfo
    LevelNotice Level = 1   // maps to slog.LevelInfo + attribute
    LevelWarn   Level = 4   // maps to slog.LevelWarn
    LevelError  Level = 8   // maps to slog.LevelError
)
```

`LevelNotice` is the unique aria2 level. It is more important than `info` but less critical than `warn`. Since `slog` has no `Notice` level, we map it to `slog.LevelInfo` with a distinguishing attribute. The classic handler automatically adds `aria2_level=notice` to every record at `LevelNotice`. The JSON handler adds `"aria2_level":"notice"` as an extra key.

### Level comparison

`Level` implements `slog.Leveler` so it can be used directly in `slog.HandlerOptions.Level`.

```go
func (l Level) Level() slog.Level {
    switch l {
    case LevelDebug:
        return slog.LevelDebug
    case LevelInfo, LevelNotice:
        return slog.LevelInfo
    case LevelWarn:
        return slog.LevelWarn
    case LevelError:
        return slog.LevelError
    default:
        return slog.LevelInfo
    }
}
```

### ParseLevel

```go
func ParseLevel(s string) (Level, error)
```

Case-insensitive. Accepts: `debug`, `info`, `notice`, `warn`, `error`. Returns `LevelInfo` as default if empty string (matches aria2's `--log-level` default). Returns error for unrecognized values.

---

## Output Destinations

Two output modes per ADR-0011:

1. **Console (stdout):** Classic handler. Used when `--log` is absent or `-`.
2. **File:** JSON handler. Used when `--log=FILE` is specified.

When both console and file output are needed (e.g., `--console-log-level` is set alongside `--log`), the module creates a multi-handler writing to two destinations with independent level gating. This is a ticket-level concern (T031 or later).

### File opening

```go
func OpenLogFile(path string) (io.WriteCloser, error)
```

Opens the file with `os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644`. Caller is responsible for closing on shutdown. File is opened at engine boot time.

---

## Bootstrapping

Order of initialization (from PLAN.md §7.1 engine boot order):

```
cmd/aria2go/main.go:
  1. config.ParseArgs(argv) → config.Options
  2. config.Options.LogLevel → log.ParseLevel → log.Level
  3. config.Options.LogPath → os.OpenFile (or os.Stdout)
  4. log.New(log.Config{Level, Output, HandlerType}) → *slog.Logger
  5. Pass *slog.Logger to engine.New() (engine holds it in Engine.log field)
  6. All subsequent initialization logs through Logger()
```

The logger is created before the engine. Engine and all subsystems receive the logger explicitly (dependency injection) through their constructor, or access the global `Logger()` for convenience wrappers. Packages that start goroutines capture the `*slog.Logger` in a closure or struct field.

For testing, `SetLogger(slog.New(handler))` replaces the global logger with a test-specific instance. The `slog.NewJSONHandler` and `slog.NewTextHandler` from stdlib can be used directly in tests; the custom handlers are tested separately.

---

## Tickets Overview

Three implementation tickets targeting ~400 LOC total:

| Ticket | Title | Target Files | LOC est. |
|---|---|---|---|
| T031 | Logger bootstrap, Level type, ParseLevel, New(), package-level wrappers | `internal/log/log.go` | ~80 |
| T032 | Classic handler: aria2-format output, source file resolution, ioutilx buffers | `internal/log/classic.go` | ~180 |
| T033 | JSON handler: structured output, level mapping, ErrorAttr | `internal/log/json.go` | ~140 |

Test files per ticket: `internal/log/log_test.go`, `internal/log/classic_test.go`, `internal/log/json_test.go`.

All tests must verify:
- Format byte-equivalence against captured aria2c log output (golden files in `test/golden/log/`)
- Concurrent safety: 100 goroutines logging simultaneously, output not interleaved
- Level filtering correctness
- Zero-allocation path when level filters out a message
- ErrorAttr produces correct slog.Attr from core.Error sentinels

---

## References

- ADR-0011 (logging policy)
- ADR-0002 (concurrency model — context propagation)
- ADR-0010 (error policy — ErrorAttr wrapping)
- ADR-0003 (RPC — RPC-accessible log output, future ticket)
- `plans/contracts/error-codes.md` — error codes for ErrInvalidLevel, ErrNoOutput
- aria2 source: `src/Logger.cc`, `src/Logger.h` (behavior reference only; no source copied)
- Go stdlib: `log/slog` package documentation (Handler interface contract)
