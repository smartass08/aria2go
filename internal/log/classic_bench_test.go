package log

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"
)

func BenchmarkClassicHandlerNoAttrs(b *testing.B) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)
	now := time.Date(2026, 5, 19, 21, 0, 0, 123000000, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := slog.NewRecord(now, slog.LevelWarn, "Failed to resolve hostname: example.com", 0)
		_ = h.Handle(context.Background(), r)
		buf.Reset()
	}
}

func BenchmarkClassicHandlerWithAttrs(b *testing.B) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)
	now := time.Date(2026, 5, 19, 21, 0, 1, 456000000, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := slog.NewRecord(now, slog.LevelInfo, "Download complete", 0)
		r.AddAttrs(
			slog.String("file", "/downloads/foo.iso"),
			slog.Int("size", 1024),
		)
		_ = h.Handle(context.Background(), r)
		buf.Reset()
	}
}

func BenchmarkClassicHandlerWithSource(b *testing.B) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, &slog.HandlerOptions{
		AddSource: true,
		Level:     LevelDebug,
	})
	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := slog.NewRecord(now, slog.LevelInfo, "source location test", 0)
		_ = h.Handle(context.Background(), r)
		buf.Reset()
	}
}

func BenchmarkClassicHandlerConcurrent(b *testing.B) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, nil)
	now := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		id := 0
		for pb.Next() {
			r := slog.NewRecord(now, slog.LevelInfo, "concurrent", 0)
			r.AddAttrs(slog.Int("id", id))
			_ = h.Handle(context.Background(), r)
			id++
		}
	})
}

func BenchmarkClassicHandlerThroughLogger(b *testing.B) {
	var buf bytes.Buffer
	h := NewClassicHandler(&buf, &slog.HandlerOptions{Level: LevelDebug})
	logger := slog.New(h)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("test message", "key", "val")
		buf.Reset()
	}
}
