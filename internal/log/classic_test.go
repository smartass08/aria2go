package log

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClassicHandlerFormat(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)
	logger := slog.New(h)

	now := time.Date(2026, 5, 19, 21, 0, 0, 123000000, time.UTC)
	r := slog.NewRecord(now, slog.LevelWarn, "Failed to resolve hostname: example.com", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	want := "2026-05-19 21:00:00.123000 [WARN] Failed to resolve hostname: example.com\n"
	got := buf.String()
	if got != want {
		t.Errorf("format mismatch:\n got: %q\nwant: %q", got, want)
	}

	_ = logger
}

func TestClassicHandlerAllLevels(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	tests := []struct {
		level    slog.Level
		name     string
		timeText string
	}{
		{slog.LevelDebug, "DEBUG", "2026-05-19 21:00:00.000000"},
		{slog.LevelInfo, "INFO", "2026-05-19 21:00:00.000000"},
		{slog.Level(LevelNotice), "NOTICE", "2026-05-19 21:00:00.000000"},
		{slog.LevelWarn, "WARN", "2026-05-19 21:00:00.000000"},
		{slog.LevelError, "ERROR", "2026-05-19 21:00:00.000000"},
	}

	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		buf.Reset()
		r := slog.NewRecord(now, tt.level, "test message", 0)
		if err := h.Handle(context.Background(), r); err != nil {
			t.Fatalf("Handle(%s) error: %v", tt.name, err)
		}
		want := tt.timeText + " [" + tt.name + "] test message\n"
		got := buf.String()
		if got != want {
			t.Errorf("level %s:\n got: %q\nwant: %q", tt.name, got, want)
		}
	}
}

func TestClassicHandlerAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)

	now := time.Date(2026, 5, 19, 21, 0, 1, 456000000, time.UTC)
	r := slog.NewRecord(now, slog.LevelInfo, "Download complete", 0)
	r.AddAttrs(
		slog.String("file", "/downloads/foo.iso"),
		slog.Int("size", 1024),
	)

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "2026-05-19 21:00:01.456000 [INFO] Download complete") {
		t.Errorf("missing prefix: %q", got)
	}
	if !strings.Contains(got, "file=/downloads/foo.iso") {
		t.Errorf("missing file attr: %q", got)
	}
	if !strings.Contains(got, "size=1024") {
		t.Errorf("missing size attr: %q", got)
	}
}

func TestClassicHandlerMicroseconds(t *testing.T) {
	tests := []struct {
		ns   int
		want string
	}{
		{0, "2026-05-19 21:00:00.000000"},
		{1000000, "2026-05-19 21:00:00.001000"},
		{10000000, "2026-05-19 21:00:00.010000"},
		{100000000, "2026-05-19 21:00:00.100000"},
		{123000000, "2026-05-19 21:00:00.123000"},
		{999000000, "2026-05-19 21:00:00.999000"},
	}

	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)

	for _, tt := range tests {
		buf.Reset()
		now := time.Date(2026, 5, 19, 21, 0, 0, tt.ns, time.UTC)
		r := slog.NewRecord(now, slog.LevelInfo, "ms test", 0)
		if err := h.Handle(context.Background(), r); err != nil {
			t.Fatalf("Handle error: %v", err)
		}
		wantPrefix := tt.want + " [INFO] ms test\n"
		got := buf.String()
		if got != wantPrefix {
			t.Errorf("ns=%d:\n got: %q\nwant: %q", tt.ns, got, wantPrefix)
		}
	}
}

func TestClassicHandlerEnabled(t *testing.T) {
	h := NewClassicHandler(nil, &slog.HandlerOptions{Level: LevelWarn})

	if !h.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("LevelWarn should be enabled when min level is LevelWarn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("LevelError should be enabled when min level is LevelWarn")
	}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("LevelInfo should be disabled when min level is LevelWarn")
	}
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("LevelDebug should be disabled when min level is LevelWarn")
	}
}

func TestClassicHandlerEnabledNotice(t *testing.T) {
	// When handler min level is LevelNotice (2), only NOTICE(2), WARN(4), ERROR(8)
	// should be enabled. INFO(0) and DEBUG(-4) should be disabled.
	// Matches aria2: A2_DEBUG(-4) < A2_INFO(0) < A2_NOTICE(2) < A2_WARN(4) < A2_ERROR(8).
	h := NewClassicHandler(nil, &slog.HandlerOptions{Level: LevelNotice})

	if !h.Enabled(context.Background(), slog.Level(LevelNotice)) {
		t.Error("LevelNotice should be enabled when min level is LevelNotice")
	}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("LevelInfo should be disabled when min level is LevelNotice")
	}
	if !h.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("LevelWarn should be enabled when min level is LevelNotice")
	}
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("LevelDebug should be disabled when min level is LevelNotice")
	}
}

func TestClassicHandlerEnabledAll(t *testing.T) {
	h := NewClassicHandler(nil, &slog.HandlerOptions{Level: LevelDebug})

	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("LevelDebug should be enabled")
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("LevelInfo should be enabled")
	}
	if !h.Enabled(context.Background(), slog.Level(LevelNotice)) {
		t.Error("LevelNotice should be enabled")
	}
	if !h.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("LevelWarn should be enabled")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("LevelError should be enabled")
	}
}

func TestClassicHandlerDefaultLevelIsNotice(t *testing.T) {
	h := NewClassicHandler(nil, nil)

	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("LevelInfo should be disabled by the default classic console level")
	}
	if !h.Enabled(context.Background(), slog.Level(LevelNotice)) {
		t.Error("LevelNotice should be enabled by the default classic console level")
	}
	if !h.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("LevelWarn should be enabled by the default classic console level")
	}
}

func TestClassicHandlerDefaultLoggerFiltersBelowNotice(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(NewClassicHandler(&buf, nil))

	logger.Info("info hidden")
	logger.Log(context.Background(), slog.Level(LevelNotice), "notice shown")

	got := buf.String()
	if strings.Contains(got, "info hidden") {
		t.Errorf("default classic logger should filter info, got: %q", got)
	}
	if !strings.Contains(got, "notice shown") {
		t.Errorf("default classic logger should emit notice, got: %q", got)
	}
}

func TestClassicHandlerSingleLine(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)

	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	r := slog.NewRecord(now, slog.LevelError, "Exception caught while processing command.", 0)
	r.AddAttrs(slog.String("CUID", "7"))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	got := buf.String()
	if strings.Count(got, "\n") != 1 {
		t.Errorf("expected exactly one line, got %d lines: %q", strings.Count(got, "\n"), got)
	}
}

func TestClassicHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := NewClassicHandler(&buf, nil)

	h := base.WithAttrs([]slog.Attr{
		slog.String("component", "engine"),
	}).(*ClassicHandler)

	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	r := slog.NewRecord(now, slog.LevelInfo, "test", 0)
	r.AddAttrs(slog.String("key", "val"))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "component=engine") {
		t.Errorf("missing pre-attached attr: %q", got)
	}
	if !strings.Contains(got, "key=val") {
		t.Errorf("missing record attr: %q", got)
	}
}

func TestClassicHandlerWithGroup(t *testing.T) {
	var buf bytes.Buffer
	base := NewClassicHandler(&buf, nil)

	h := base.WithGroup("request").(*ClassicHandler)

	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	r := slog.NewRecord(now, slog.LevelInfo, "test", 0)
	r.AddAttrs(slog.String("method", "POST"))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "request.method=POST") {
		t.Errorf("missing grouped attr, got: %q", got)
	}
}

func TestClassicHandlerWithGroupNested(t *testing.T) {
	var buf bytes.Buffer
	base := NewClassicHandler(&buf, nil)

	h := base.WithGroup("a").WithGroup("b").(*ClassicHandler)

	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	r := slog.NewRecord(now, slog.LevelInfo, "test", 0)
	r.AddAttrs(slog.String("key", "val"))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "a.b.key=val") {
		t.Errorf("missing nested attr, got: %q", got)
	}
}

func TestClassicHandlerConcurrent(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)

	var wg sync.WaitGroup
	n := 50
	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r := slog.NewRecord(now, slog.LevelInfo, "concurrent", 0)
			r.AddAttrs(slog.Int("id", id))
			_ = h.Handle(context.Background(), r)
		}(i)
	}
	wg.Wait()

	got := buf.String()
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != n {
		t.Errorf("expected %d lines, got %d", n, len(lines))
	}
}

func TestClassicHandlerThroughLogger(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, &slog.HandlerOptions{Level: LevelDebug})
	logger := slog.New(h)

	logger.Log(context.Background(), slog.Level(LevelNotice), "notice message")
	logger.Warn("warn message", "key", "val")
	logger.Error("error message")

	got := buf.String()
	if !strings.Contains(got, "[NOTICE]") {
		t.Errorf("missing NOTICE: %q", got)
	}
	if !strings.Contains(got, "notice message") {
		t.Errorf("missing notice message: %q", got)
	}
	if !strings.Contains(got, "[WARN]") {
		t.Errorf("missing WARN: %q", got)
	}
	if !strings.Contains(got, "warn message") {
		t.Errorf("missing warn message: %q", got)
	}
	if !strings.Contains(got, "key=val") {
		t.Errorf("missing key=val: %q", got)
	}
	if !strings.Contains(got, "[ERROR]") {
		t.Errorf("missing ERROR: %q", got)
	}
	if !strings.Contains(got, "error message") {
		t.Errorf("missing error message: %q", got)
	}
}

func TestClassicHandlerEmptyRecord(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)

	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	r := slog.NewRecord(now, slog.LevelInfo, "", 0)

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	got := buf.String()
	want := "2026-05-19 21:00:00.000000 [INFO] \n"
	if got != want {
		t.Errorf("empty message:\n got: %q\nwant: %q", got, want)
	}
}

func TestClassicHandlerNilOpts(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)

	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	r := slog.NewRecord(now, slog.LevelDebug, "debug msg", 0)

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "[DEBUG] debug msg") {
		t.Errorf("nil opts should still allow Debug: %q", got)
	}
}

func TestClassicHandlerFileLine(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, &slog.HandlerOptions{
		AddSource: true,
		Level:     LevelDebug,
	})

	logger := slog.New(h)
	logger.Info("source location test")

	got := buf.String()
	if !strings.Contains(got, "[INFO]") {
		t.Errorf("missing INFO level: %q", got)
	}
	if !strings.Contains(got, "source location test") {
		t.Errorf("missing message: %q", got)
	}
	// When AddSource is true, slog attaches source file:line via PC.
	// The output should contain [classic_test.go:lineNum] (or similar).
	if !strings.Contains(got, "[") || !strings.Contains(got, "]") {
		t.Errorf("missing [file:line] section: %q", got)
	}
	// Verify the section between level marker and message contains a colon
	// (the file:line separator). Format: [level] [file:line] message
	afterLevel := strings.Index(got, "] ")
	if afterLevel < 0 {
		t.Fatalf("unexpected format, no '] ' found: %q", got)
	}
	afterLevel += 2
	rest := got[afterLevel:]
	// next char should be '[' (start of [file:line])
	if len(rest) == 0 || rest[0] != '[' {
		t.Errorf("expected [file:line] after level, got: %q", rest)
		return
	}
	closeBracket := strings.IndexByte(rest, ']')
	if closeBracket < 0 {
		t.Errorf("expected closing ] for [file:line]: %q", rest)
		return
	}
	fileLine := rest[1:closeBracket]
	if !strings.Contains(fileLine, ":") {
		t.Errorf("expected 'file:line' format, got: %q", fileLine)
	}
	if !strings.Contains(fileLine, "classic_test.go") {
		t.Errorf("expected file name in [file:line], got: %q", fileLine)
	}
}

func TestClassicHandlerFileLineNoPC(t *testing.T) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)

	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	r := slog.NewRecord(now, slog.LevelInfo, "no pc test", 0)

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle error: %v", err)
	}

	got := buf.String()
	want := "2026-05-19 21:00:00.000000 [INFO] no pc test\n"
	if got != want {
		t.Errorf("no PC should skip [file:line]:\n got: %q\nwant: %q", got, want)
	}
}
