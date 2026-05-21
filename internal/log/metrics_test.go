package log

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"
)

func TestNewMetrics(t *testing.T) {
	m := NewMetrics()
	if m == nil {
		t.Fatal("NewMetrics() returned nil")
	}
	if m.Total() != 0 {
		t.Errorf("new Metrics total = %d, want 0", m.Total())
	}
}

func TestMetricsInc(t *testing.T) {
	m := NewMetrics()

	m.Inc(LevelDebug)
	if c := m.Count(LevelDebug); c != 1 {
		t.Errorf("Count(LevelDebug) = %d, want 1", c)
	}

	m.Inc(LevelInfo)
	if c := m.Count(LevelInfo); c != 1 {
		t.Errorf("Count(LevelInfo) = %d, want 1", c)
	}

	m.Inc(LevelNotice)
	if c := m.Count(LevelNotice); c != 1 {
		t.Errorf("Count(LevelNotice) = %d, want 1", c)
	}

	m.Inc(LevelWarn)
	if c := m.Count(LevelWarn); c != 1 {
		t.Errorf("Count(LevelWarn) = %d, want 1", c)
	}

	m.Inc(LevelError)
	if c := m.Count(LevelError); c != 1 {
		t.Errorf("Count(LevelError) = %d, want 1", c)
	}
}

func TestMetricsIncMultiple(t *testing.T) {
	m := NewMetrics()

	for i := 0; i < 5; i++ {
		m.Inc(LevelDebug)
	}
	for i := 0; i < 3; i++ {
		m.Inc(LevelWarn)
	}
	for i := 0; i < 7; i++ {
		m.Inc(LevelError)
	}

	if c := m.Count(LevelDebug); c != 5 {
		t.Errorf("Count(LevelDebug) = %d, want 5", c)
	}
	if c := m.Count(LevelInfo); c != 0 {
		t.Errorf("Count(LevelInfo) = %d, want 0", c)
	}
	if c := m.Count(LevelWarn); c != 3 {
		t.Errorf("Count(LevelWarn) = %d, want 3", c)
	}
	if c := m.Count(LevelError); c != 7 {
		t.Errorf("Count(LevelError) = %d, want 7", c)
	}
}

func TestMetricsCountUnknownLevel(t *testing.T) {
	m := NewMetrics()
	if c := m.Count(Level(99)); c != 0 {
		t.Errorf("Count(unknown) = %d, want 0", c)
	}
}

func TestMetricsTotal(t *testing.T) {
	m := NewMetrics()

	m.Inc(LevelDebug)
	m.Inc(LevelInfo)
	m.Inc(LevelInfo)
	m.Inc(LevelWarn)
	m.Inc(LevelError)
	m.Inc(LevelError)
	m.Inc(LevelError)

	if total := m.Total(); total != 7 {
		t.Errorf("Total() = %d, want 7", total)
	}
}

func TestMetricsTotalZero(t *testing.T) {
	m := NewMetrics()
	if total := m.Total(); total != 0 {
		t.Errorf("Total() = %d, want 0", total)
	}
}

func TestMetricsSnapshot(t *testing.T) {
	m := NewMetrics()
	m.Inc(LevelDebug)
	m.Inc(LevelDebug)
	m.Inc(LevelInfo)
	m.Inc(LevelNotice)
	m.Inc(LevelWarn)
	m.Inc(LevelWarn)
	m.Inc(LevelWarn)
	m.Inc(LevelError)

	snap := m.Snapshot()
	if len(snap) != 5 {
		t.Errorf("Snapshot() len = %d, want 5", len(snap))
	}
	if snap["debug"] != 2 {
		t.Errorf("Snapshot() debug = %d, want 2", snap["debug"])
	}
	if snap["info"] != 1 {
		t.Errorf("Snapshot() info = %d, want 1", snap["info"])
	}
	if snap["notice"] != 1 {
		t.Errorf("Snapshot() notice = %d, want 1", snap["notice"])
	}
	if snap["warn"] != 3 {
		t.Errorf("Snapshot() warn = %d, want 3", snap["warn"])
	}
	if snap["error"] != 1 {
		t.Errorf("Snapshot() error = %d, want 1", snap["error"])
	}
}

func TestMetricsSnapshotKeysPresent(t *testing.T) {
	m := NewMetrics()
	snap := m.Snapshot()

	for _, key := range []string{"debug", "info", "notice", "warn", "error"} {
		if _, ok := snap[key]; !ok {
			t.Errorf("Snapshot() missing key %q", key)
		}
	}
	if v := snap["debug"]; v != 0 {
		t.Errorf("Snapshot() debug = %d, want 0", v)
	}
}

func TestMetricsReset(t *testing.T) {
	m := NewMetrics()
	m.Inc(LevelDebug)
	m.Inc(LevelInfo)
	m.Inc(LevelWarn)
	m.Inc(LevelError)

	if total := m.Total(); total != 4 {
		t.Fatalf("pre-reset Total() = %d, want 4", total)
	}

	m.Reset()

	if total := m.Total(); total != 0 {
		t.Errorf("post-reset Total() = %d, want 0", total)
	}
	for _, level := range []Level{LevelDebug, LevelInfo, LevelNotice, LevelWarn, LevelError} {
		if c := m.Count(level); c != 0 {
			t.Errorf("Count(%s) = %d, want 0", level.String(), c)
		}
	}
}

func TestMetricsConcurrentInc(t *testing.T) {
	m := NewMetrics()
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Inc(LevelInfo)
		}()
	}
	wg.Wait()

	if c := m.Count(LevelInfo); c != int64(n) {
		t.Errorf("Count(LevelInfo) = %d, want %d", c, n)
	}
}

func TestMetricsConcurrentAllLevels(t *testing.T) {
	m := NewMetrics()
	var wg sync.WaitGroup
	levels := []Level{LevelDebug, LevelInfo, LevelNotice, LevelWarn, LevelError}
	n := 50

	for _, lvl := range levels {
		wg.Add(n)
		for j := 0; j < n; j++ {
			go func(lv Level) {
				defer wg.Done()
				m.Inc(lv)
			}(lvl)
		}
	}
	wg.Wait()

	for _, lvl := range levels {
		if c := m.Count(lvl); c != int64(n) {
			t.Errorf("Count(%s) = %d, want %d", lvl.String(), c, n)
		}
	}
	if total := m.Total(); total != int64(n*len(levels)) {
		t.Errorf("Total() = %d, want %d", total, n*len(levels))
	}
}

func TestMetricsConcurrentSnapshot(t *testing.T) {
	m := NewMetrics()
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.Inc(LevelInfo)
		}()
		go func() {
			defer wg.Done()
			_ = m.Snapshot()
		}()
	}
	wg.Wait()
}

func TestMetricsResetConcurrent(t *testing.T) {
	m := NewMetrics()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			m.Inc(LevelDebug)
		}()
		go func() {
			defer wg.Done()
			m.Inc(LevelError)
		}()
		go func() {
			defer wg.Done()
			m.Reset()
		}()
	}
	wg.Wait()
}

func TestCountingHandlerIncrementsCounters(t *testing.T) {
	m := NewMetrics()
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelDebug})
	ch := NewCountingHandler(inner, m)

	logger := slog.New(ch)
	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")

	if c := m.Count(LevelDebug); c != 1 {
		t.Errorf("Count(LevelDebug) = %d, want 1", c)
	}
	if c := m.Count(LevelInfo); c != 1 {
		t.Errorf("Count(LevelInfo) = %d, want 1", c)
	}
	if c := m.Count(LevelWarn); c != 1 {
		t.Errorf("Count(LevelWarn) = %d, want 1", c)
	}
	if c := m.Count(LevelError); c != 1 {
		t.Errorf("Count(LevelError) = %d, want 1", c)
	}
	if total := m.Total(); total != 4 {
		t.Errorf("Total() = %d, want 4", total)
	}
}

func TestCountingHandlerDelegatesToInner(t *testing.T) {
	m := NewMetrics()
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelDebug})
	ch := NewCountingHandler(inner, m)

	logger := slog.New(ch)
	logger.Info("test message", "key", "value")

	got := buf.String()
	if !contains(got, "test message") {
		t.Errorf("inner handler output missing message: %q", got)
	}
	if !contains(got, "key=value") {
		t.Errorf("inner handler output missing attribute: %q", got)
	}
}

func TestCountingHandlerLevelFiltering(t *testing.T) {
	m := NewMetrics()
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelWarn})
	ch := NewCountingHandler(inner, m)

	logger := slog.New(ch)
	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")

	if c := m.Count(LevelDebug); c != 0 {
		t.Errorf("Count(LevelDebug) = %d, want 0 (filtered by level)", c)
	}
	if c := m.Count(LevelInfo); c != 0 {
		t.Errorf("Count(LevelInfo) = %d, want 0 (filtered by level)", c)
	}
	if c := m.Count(LevelWarn); c != 1 {
		t.Errorf("Count(LevelWarn) = %d, want 1", c)
	}
	if c := m.Count(LevelError); c != 1 {
		t.Errorf("Count(LevelError) = %d, want 1", c)
	}
}

func TestCountingHandlerWithAttrs(t *testing.T) {
	m := NewMetrics()
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelDebug})
	ch := NewCountingHandler(inner, m)

	logger := slog.New(ch)
	child := logger.With("component", "test")
	child.Info("with attrs msg")

	if c := m.Count(LevelInfo); c != 1 {
		t.Errorf("Count(LevelInfo) = %d, want 1", c)
	}
}

func TestCountingHandlerConcurrent(t *testing.T) {
	m := NewMetrics()
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelDebug})
	ch := NewCountingHandler(inner, m)

	logger := slog.New(ch)
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("concurrent")
		}()
	}
	wg.Wait()

	if c := m.Count(LevelInfo); c != int64(n) {
		t.Errorf("Count(LevelInfo) = %d, want %d", c, n)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSlice(s, substr)
}

func searchSlice(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
