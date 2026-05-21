// Package log provides the canonical logging substrate for aria2go.
// All packages log through this module; no package creates its own
// slog.Handler or slog.Logger.
//
// Two handlers are supported:
//   - classic: aria2-compatible human-readable output (T032)
//   - json: structured JSON for ops ingestion (T033)
//
// A single global *slog.Logger is created via New() at startup.
// Convenience wrappers delegate to the global logger.
package log

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
)

// Level is the named log level, mirroring aria2's --log-level values.
type Level int

const (
	LevelDebug  Level = Level(slog.LevelDebug) // -4
	LevelInfo   Level = Level(slog.LevelInfo)  // 0
	LevelNotice Level = 2                      // aria2-specific, between Info and Warn
	LevelWarn   Level = Level(slog.LevelWarn)  // 4
	LevelError  Level = Level(slog.LevelError) // 8
)

// Level implements slog.Leveler so Level can be used directly
// in slog.HandlerOptions.Level. LevelNotice maps to slog.LevelInfo;
// the notice distinction is encoded via attributes in the handler.
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

// String returns the lowercase name of the level.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelNotice:
		return "notice"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// HandlerType selects which slog.Handler to construct in New().
type HandlerType uint8

const (
	HandlerClassic HandlerType = iota
	HandlerJSON
)

// Config holds logger initialization parameters.
type Config struct {
	Level   Level
	Output  io.Writer // destination (os.Stdout or file); required
	Handler HandlerType
}

// Sentinel errors returned by functions in this package.
var (
	ErrInvalidLevel = errors.New("log: invalid log level")
	ErrNoOutput     = errors.New("log: no output writer configured")
)

var globalLogger atomic.Pointer[slog.Logger]

// New creates the root *slog.Logger for the application.
// It must be called once at startup before any logging occurs.
// cfg.Output must be non-nil.
//
// ClassicHandler outputs aria2-compatible log lines.
// JSONHandler uses slog.NewJSONHandler; T033 may provide an extended version.
func New(cfg Config) (*slog.Logger, error) {
	if cfg.Output == nil {
		return nil, ErrNoOutput
	}
	var handler slog.Handler
	switch cfg.Handler {
	case HandlerClassic:
		handler = NewClassicHandler(cfg.Output, &slog.HandlerOptions{
			Level: cfg.Level,
		})
	case HandlerJSON:
		handler = slog.NewJSONHandler(cfg.Output, &slog.HandlerOptions{
			Level: cfg.Level,
		})
	default:
		handler = NewClassicHandler(cfg.Output, &slog.HandlerOptions{
			Level: cfg.Level,
		})
	}
	logger := slog.New(handler)
	globalLogger.Store(logger)
	return logger, nil
}

// Logger returns the configured application logger.
// Returns nil before New() is called.
func Logger() *slog.Logger {
	return globalLogger.Load()
}

// SetLogger replaces the global application logger.
// Primarily for testing; not safe for concurrent calls.
func SetLogger(l *slog.Logger) {
	globalLogger.Store(l)
}

// ParseLevel parses a log level string. It is case-insensitive and trims
// surrounding whitespace. An empty string returns LevelInfo (aria2's default).
// Unrecognized values return ErrInvalidLevel.
func ParseLevel(s string) (Level, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return LevelInfo, nil
	}
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "notice":
		return LevelNotice, nil
	case "warn":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelInfo, ErrInvalidLevel
	}
}

// Debug logs at LevelDebug through the global logger.
func Debug(msg string, args ...any) {
	if l := Logger(); l != nil {
		l.Debug(msg, args...)
	}
}

// Info logs at LevelInfo through the global logger.
func Info(msg string, args ...any) {
	if l := Logger(); l != nil {
		l.Info(msg, args...)
	}
}

// Warn logs at LevelWarn through the global logger.
func Warn(msg string, args ...any) {
	if l := Logger(); l != nil {
		l.Warn(msg, args...)
	}
}

// Error logs at LevelError through the global logger.
func Error(msg string, args ...any) {
	if l := Logger(); l != nil {
		l.Error(msg, args...)
	}
}

// Log logs at the given level with context through the global logger.
func Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if l := Logger(); l != nil {
		l.Log(ctx, level, msg, args...)
	}
}

// With returns a new *slog.Logger derived from the global logger
// that includes the given attributes in each log record.
func With(args ...any) *slog.Logger {
	if l := Logger(); l != nil {
		return l.With(args...)
	}
	return nil
}

// WithGroup returns a new *slog.Logger derived from the global logger
// that qualifies all subsequent keys with the given group name.
func WithGroup(name string) *slog.Logger {
	if l := Logger(); l != nil {
		return l.WithGroup(name)
	}
	return nil
}
