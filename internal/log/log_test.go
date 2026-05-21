package log

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestLevelConstants(t *testing.T) {
	if int(LevelDebug) != -4 {
		t.Errorf("LevelDebug = %d, want -4", LevelDebug)
	}
	if int(LevelInfo) != 0 {
		t.Errorf("LevelInfo = %d, want 0", LevelInfo)
	}
	if int(LevelNotice) != 2 {
		t.Errorf("LevelNotice = %d, want 2", LevelNotice)
	}
	if int(LevelWarn) != 4 {
		t.Errorf("LevelWarn = %d, want 4", LevelWarn)
	}
	if int(LevelError) != 8 {
		t.Errorf("LevelError = %d, want 8", LevelError)
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "debug"},
		{LevelInfo, "info"},
		{LevelNotice, "notice"},
		{LevelWarn, "warn"},
		{LevelError, "error"},
		{Level(99), "info"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestLevelLeveler(t *testing.T) {
	tests := []struct {
		level Level
		want  int
	}{
		{LevelDebug, -4},
		{LevelInfo, 0},
		{LevelNotice, 0}, // maps to slog.LevelInfo
		{LevelWarn, 4},
		{LevelError, 8},
	}
	for _, tt := range tests {
		if got := int(tt.level.Level()); got != tt.want {
			t.Errorf("Level(%d).Level() = %d, want %d", tt.level, got, tt.want)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    Level
		wantErr bool
	}{
		{"debug", LevelDebug, false},
		{"DEBUG", LevelDebug, false},
		{"Debug", LevelDebug, false},
		{"  debug  ", LevelDebug, false},
		{"info", LevelInfo, false},
		{"INFO", LevelInfo, false},
		{"notice", LevelNotice, false},
		{"NOTICE", LevelNotice, false},
		{"warn", LevelWarn, false},
		{"WARN", LevelWarn, false},
		{"error", LevelError, false},
		{"ERROR", LevelError, false},
		{"", LevelInfo, false},
		{"   ", LevelInfo, false},
		{"invalid", LevelInfo, true},
		{"unknown", LevelInfo, true},
		{"info  ", LevelInfo, false},
	}
	for _, tt := range tests {
		got, err := ParseLevel(tt.input)
		if tt.wantErr && err == nil {
			t.Errorf("ParseLevel(%q) expected error, got nil", tt.input)
			continue
		}
		if !tt.wantErr && err != nil {
			t.Errorf("ParseLevel(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseLevel(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseLevelErrorSentinel(t *testing.T) {
	_, err := ParseLevel("bogus")
	if err != ErrInvalidLevel {
		t.Errorf("ParseLevel error = %v, want ErrInvalidLevel", err)
	}
}

func TestNewNilOutput(t *testing.T) {
	_, err := New(Config{Output: nil})
	if err != ErrNoOutput {
		t.Errorf("New(nil output) error = %v, want ErrNoOutput", err)
	}
}

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:   LevelDebug,
		Output:  &buf,
		Handler: HandlerJSON,
	}
	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if logger == nil {
		t.Fatal("New() returned nil logger")
	}
	logger.Info("hello", "key", "val")
	got := buf.String()
	if !strings.Contains(got, "hello") {
		t.Errorf("log output missing message: %q", got)
	}
	if !strings.Contains(got, `"key":"val"`) {
		t.Errorf("log output missing key=val: %q", got)
	}
}

func TestNewLoggerClassicHandler(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:   LevelInfo,
		Output:  &buf,
		Handler: HandlerClassic,
	}
	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	logger.Info("test message")
	got := buf.String()
	if !strings.Contains(got, "test message") {
		t.Errorf("classic handler output missing message: %q", got)
	}
}

func TestNewLoggerUnknownHandlerUsesClassic(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:   LevelInfo,
		Output:  &buf,
		Handler: HandlerType(99),
	}
	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	logger.Info("fallback message")
	got := buf.String()
	if !strings.Contains(got, "[INFO]") || !strings.Contains(got, "fallback message") {
		t.Errorf("unknown handler should use classic output, got: %q", got)
	}
	if strings.Contains(got, "level=INFO") {
		t.Errorf("unknown handler should not use slog text handler, got: %q", got)
	}
}

func TestLoggerSetLogger(t *testing.T) {
	// Clear global state
	SetLogger(nil)
	if l := Logger(); l != nil {
		t.Fatal("Logger() should be nil after SetLogger(nil)")
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	SetLogger(logger)

	if l := Logger(); l != logger {
		t.Fatal("Logger() should return the logger set by SetLogger")
	}

	Info("test-from-global")
	got := buf.String()
	if !strings.Contains(got, "test-from-global") {
		t.Errorf("convenience wrapper output missing message: %q", got)
	}
}

func TestConvenienceWrappersNilLogger(t *testing.T) {
	SetLogger(nil)

	// These should not panic.
	Debug("should not panic")
	Info("should not panic")
	Warn("should not panic")
	Error("should not panic")
	Log(context.Background(), slog.LevelInfo, "should not panic")

	if l := With("key", "val"); l != nil {
		t.Error("With() should return nil when global logger is nil")
	}
	if l := WithGroup("g"); l != nil {
		t.Error("WithGroup() should return nil when global logger is nil")
	}
}

func TestConvenienceWrappersWithLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelDebug}))
	SetLogger(logger)

	Debug("debug msg", "a", 1)
	Info("info msg", "b", 2)
	Warn("warn msg", "c", 3)
	Error("error msg", "d", 4)

	got := buf.String()
	for _, want := range []string{"debug msg", "info msg", "warn msg", "error msg"} {
		if !strings.Contains(got, want) {
			t.Errorf("convenience wrapper output missing %q", want)
		}
	}
}

func TestLogWithContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelDebug}))
	SetLogger(logger)

	ctx := context.Background()
	Log(ctx, slog.LevelInfo, "context-log", "ctxkey", "ctxval")
	got := buf.String()
	if !strings.Contains(got, "context-log") {
		t.Errorf("Log output missing message: %q", got)
	}
}

func TestWithAndWithGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: LevelDebug}))
	SetLogger(logger)

	l := With("component", "test")
	if l == nil {
		t.Fatal("With() returned nil")
	}
	l.Info("with message")
	got := buf.String()
	if !strings.Contains(got, "component=test") {
		t.Errorf("With output missing attribute: %q", got)
	}

	buf.Reset()
	lg := WithGroup("group1")
	if lg == nil {
		t.Fatal("WithGroup() returned nil")
	}
	lg.Info("group message", "key", "val")
	got = buf.String()
	if !strings.Contains(got, "group1") {
		t.Errorf("WithGroup output missing group name: %q", got)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:   LevelWarn, // only Warn and above
		Output:  &buf,
		Handler: HandlerJSON,
	}
	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	logger.Debug("should be filtered")
	logger.Info("should be filtered")
	logger.Warn("should appear")
	logger.Error("should appear")

	got := buf.String()
	if strings.Contains(got, "should be filtered") {
		t.Error("level filtering did not suppress Debug/Info messages")
	}
	if !strings.Contains(got, "should appear") {
		t.Error("level filtering incorrectly suppressed Warn/Error messages")
	}
}

func TestConcurrentLogging(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:   LevelDebug,
		Output:  &buf,
		Handler: HandlerJSON,
	}
	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	n := 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			logger.Info("concurrent", "id", id)
		}(i)
	}
	wg.Wait()

	// Verify no panic and some output
	got := buf.String()
	if got == "" {
		t.Error("concurrent logging produced no output")
	}
}

func TestConcurrentGlobalLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: LevelDebug}))
	SetLogger(logger)

	var wg sync.WaitGroup
	n := 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			Info("concurrent-global", "id", id)
		}(i)
	}
	wg.Wait()

	got := buf.String()
	if got == "" {
		t.Error("concurrent global logging produced no output")
	}
}

func TestDefaultHandlerType(t *testing.T) {
	var buf bytes.Buffer
	cfg := Config{
		Level:   LevelInfo,
		Output:  &buf,
		Handler: HandlerType(99), // unknown handler type
	}
	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	logger.Info("should still log")
	if buf.String() == "" {
		t.Error("default handler did not produce output")
	}
}

func FuzzParseLevel(f *testing.F) {
	seeds := []string{
		"debug", "info", "notice", "warn", "error",
		"DEBUG", "INFO", "NOTICE", "WARN", "ERROR",
		"", "unknown", "Debug", "  info  ", "WARNING",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		level, err := ParseLevel(s)
		if err != nil && err != ErrInvalidLevel {
			t.Errorf("ParseLevel(%q) returned unexpected error: %v", s, err)
		}
		_ = level
	})
}
