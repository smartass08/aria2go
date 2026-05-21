package log

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// Metrics holds per-level atomic log counters for observability.
// All methods are safe for concurrent use.
type Metrics struct {
	debug  atomic.Int64
	info   atomic.Int64
	notice atomic.Int64
	warn   atomic.Int64
	err    atomic.Int64
}

// NewMetrics returns a new zeroed Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// Inc increments the counter for the given level by one.
func (m *Metrics) Inc(level Level) {
	switch level {
	case LevelDebug:
		m.debug.Add(1)
	case LevelInfo:
		m.info.Add(1)
	case LevelNotice:
		m.notice.Add(1)
	case LevelWarn:
		m.warn.Add(1)
	case LevelError:
		m.err.Add(1)
	}
}

// Count returns the current counter value for the given level.
func (m *Metrics) Count(level Level) int64 {
	switch level {
	case LevelDebug:
		return m.debug.Load()
	case LevelInfo:
		return m.info.Load()
	case LevelNotice:
		return m.notice.Load()
	case LevelWarn:
		return m.warn.Load()
	case LevelError:
		return m.err.Load()
	default:
		return 0
	}
}

// Total returns the sum of all level counters.
func (m *Metrics) Total() int64 {
	return m.debug.Load() + m.info.Load() + m.notice.Load() + m.warn.Load() + m.err.Load()
}

// Snapshot returns a map with all five level counters keyed by lowercase level name.
func (m *Metrics) Snapshot() map[string]int64 {
	return map[string]int64{
		"debug":  m.debug.Load(),
		"info":   m.info.Load(),
		"notice": m.notice.Load(),
		"warn":   m.warn.Load(),
		"error":  m.err.Load(),
	}
}

// Reset zeroes all counters atomically.
func (m *Metrics) Reset() {
	m.debug.Store(0)
	m.info.Store(0)
	m.notice.Store(0)
	m.warn.Store(0)
	m.err.Store(0)
}

// CountingHandler wraps an slog.Handler and auto-increments a Metrics on each log call.
type CountingHandler struct {
	slog.Handler
	metrics *Metrics
}

// NewCountingHandler creates a CountingHandler that delegates to inner after incrementing m.
func NewCountingHandler(inner slog.Handler, m *Metrics) *CountingHandler {
	return &CountingHandler{
		Handler: inner,
		metrics: m,
	}
}

// Handle increments the metrics counter for the record's level, then delegates to the inner handler.
func (h *CountingHandler) Handle(ctx context.Context, r slog.Record) error {
	h.metrics.Inc(Level(r.Level))
	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a new CountingHandler that includes the given attributes.
func (h *CountingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &CountingHandler{
		Handler: h.Handler.WithAttrs(attrs),
		metrics: h.metrics,
	}
}

// WithGroup returns a new CountingHandler with the given group name.
func (h *CountingHandler) WithGroup(name string) slog.Handler {
	return &CountingHandler{
		Handler: h.Handler.WithGroup(name),
		metrics: h.metrics,
	}
}
