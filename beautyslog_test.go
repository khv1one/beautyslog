package beautyslog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"runtime"
	"testing"
	"time"
)

func getPC() uintptr {
	var pcs [1]uintptr
	runtime.Callers(2, pcs[:])
	return pcs[0]
}

func createTestRecord() slog.Record {
	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "Processing user request",
		PC:      getPC(),
	}
	r.Add(slog.Any("user_id", 12345))
	r.Add(slog.Any("error", errors.New("dss")))
	r.Add(slog.String("request_id", "req-abc-123-def"))
	r.Add(slog.Bool("b", false))
	r.Add(slog.Group("group", slog.String("d", "dddd")))
	r.Add(slog.Time("t", time.Now()))
	r.Add(slog.Int64("latency_ms", 150))
	r.Add(slog.Duration("dur", time.Hour))
	return r
}

func BenchmarkSlogTextHandlerWithSource(b *testing.B) {
	handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	})
	record := createTestRecord()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := handler.Handle(context.TODO(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrettyTextHandlerWithSource(b *testing.B) {
	handler := New(io.Discard, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	})
	record := createTestRecord()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := handler.Handle(context.TODO(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSlogTextHandlerWithoutSource(b *testing.B) {
	handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelInfo,
	})
	record := createTestRecord()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := handler.Handle(context.TODO(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrettyTextHandlerWithoutSource(b *testing.B) {
	handler := New(io.Discard, &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelInfo,
	})
	record := createTestRecord()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := handler.Handle(context.TODO(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPrint(_ *testing.T) {
	prettyHandler := New(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "password" || a.Key == "token" {
				return slog.String(a.Key, "*****")
			}
			return a
		},
	})

	logger := slog.New(prettyHandler)

	logger.Debug("connect", slog.String("cache_addr", "localhost:6379"))
	time.Sleep(123 * time.Millisecond)
	logger.Info("start request")
	time.Sleep(770 * time.Millisecond)
	logger.Warn("quota limit", slog.Int("quota_used", 98), slog.Int("quota_limit", 100))
	time.Sleep(123 * time.Millisecond)
	logger.Error("input failed", slog.Any("error", errors.New("err")))
	time.Sleep(123 * time.Millisecond)
	logger.Warn("warn group", slog.Group("group", slog.String("a", "b"), slog.Int("quota_used", 98)))
	time.Sleep(10 * time.Millisecond)
	logger.Info("duration", slog.Duration("ddd", time.Hour), slog.Duration("ms", time.Microsecond))
	logger.Info("without slog types", "123k", 123, "dur", time.Hour, "b", true, "er", errors.New("dss"))
}
