package beautyslog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
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

func TestHandleFieldOrder(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	record := slog.Record{
		Time:    fixedTime,
		Level:   slog.LevelInfo,
		Message: "hello",
		PC:      0,
	}
	record.Add(slog.String("k", "v"))

	tests := []struct {
		name     string
		fields   []Field
		wantSub1 string
		wantSub2 string
	}{
		{
			name:     "default order",
			fields:   []Field{TimeField, SourceField, LevelField, MessageField, AttrsField},
			wantSub1: "12:00:00",
			wantSub2: "INFO",
		},
		{
			name:     "level first",
			fields:   []Field{LevelField, MessageField, TimeField},
			wantSub1: "INFO",
			wantSub2: "12:00:00",
		},
		{
			name:     "only message and attrs",
			fields:   []Field{MessageField, AttrsField},
			wantSub1: "hello",
			wantSub2: "k", // value has color codes around it
		},
		{
			name:     "message then level",
			fields:   []Field{MessageField, LevelField},
			wantSub1: "hello",
			wantSub2: "INFO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := New(&buf, &HandlerOptions{
				Level:  slog.LevelDebug,
				Fields: tt.fields,
			})
			if err := h.Handle(context.Background(), record); err != nil {
				t.Fatalf("Handle error: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, tt.wantSub1) {
				t.Errorf("output %q missing %q", out, tt.wantSub1)
			}
			if !strings.Contains(out, tt.wantSub2) {
				t.Errorf("output %q missing %q", out, tt.wantSub2)
			}
		})
	}
}

func TestHandleCustomLevel(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	record := slog.Record{
		Time:    fixedTime,
		Level:   slog.Level(5),
		Message: "custom",
		PC:      0,
	}

	var buf bytes.Buffer
	h := New(&buf, &HandlerOptions{
		Level:  slog.LevelDebug,
		Fields: []Field{LevelField, MessageField},
	})
	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	out := buf.String()
	want := "WARN+1"
	if !strings.Contains(out, want) {
		t.Errorf("output %q missing custom level string %q", out, want)
	}
}

func TestHandleSourceField(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	record := slog.Record{
		Time:    fixedTime,
		Level:   slog.LevelInfo,
		Message: "msg",
		PC:      getPC(),
	}

	tests := []struct {
		name       string
		addSource  bool
		fields     []Field
		wantSource bool
	}{
		{
			name:       "source shown",
			addSource:  true,
			fields:     []Field{TimeField, SourceField, LevelField, MessageField},
			wantSource: true,
		},
		{
			name:       "source hidden by option",
			addSource:  false,
			fields:     []Field{TimeField, SourceField, LevelField, MessageField},
			wantSource: false,
		},
		{
			name:       "source omitted from fields",
			addSource:  true,
			fields:     []Field{TimeField, LevelField, MessageField},
			wantSource: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := New(&buf, &HandlerOptions{
				Level:     slog.LevelDebug,
				AddSource: tt.addSource,
				Fields:    tt.fields,
			})
			if err := h.Handle(context.Background(), record); err != nil {
				t.Fatalf("Handle error: %v", err)
			}
			out := buf.String()
			hasSource := strings.Contains(out, ".go:")
			if hasSource != tt.wantSource {
				t.Errorf("source presence = %v, want %v; output: %q", hasSource, tt.wantSource, out)
			}
		})
	}
}

func TestHandleSkippedSourceNoExtraSpaces(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	record := slog.Record{
		Time:    fixedTime,
		Level:   slog.LevelInfo,
		Message: "msg",
		PC:      0, // no source info
	}

	var buf bytes.Buffer
	h := New(&buf, &HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
		Fields:    []Field{TimeField, SourceField, LevelField, MessageField},
	})
	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	out := buf.String()

	// Strip ANSI escape codes and check the boundary where source was skipped.
	// Expected: "12:00:00 INFO  msg" (one space between time and level,
	// then two spaces between level and msg due to 5-char level padding).
	plain := stripANSI(out)
	want := "12:00:00 INFO  msg"
	if !strings.HasPrefix(plain, want) {
		t.Errorf("expected prefix %q, got: %q (raw: %q)", want, plain, out)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == ';') {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestHandleCustomColors(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	record := slog.Record{
		Time:    fixedTime,
		Level:   slog.LevelWarn,
		Message: "alert",
		PC:      0,
	}
	record.Add(slog.String("key", "val"))

	customYellow := []byte("\033[93m")
	customCyan := []byte("\033[96m")
	customReset := []byte("\033[0m")

	var buf bytes.Buffer
	h := New(&buf, &HandlerOptions{
		Level: slog.LevelDebug,
		Fields: []Field{
			TimeField,
			LevelField,
			MessageField,
			AttrsField,
		},
		Colors: &ColorScheme{
			Time:  customCyan,
			Warn:  customYellow,
			Key:   customCyan,
			Value: customYellow,
			Reset: customReset,
		},
	})
	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle error: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, string(customCyan)) {
		t.Errorf("output missing custom cyan color")
	}
	if !strings.Contains(out, string(customYellow)) {
		t.Errorf("output missing custom yellow color")
	}
	if !strings.Contains(out, string(customReset)) {
		t.Errorf("output missing reset color")
	}
}

func TestWithAttrsAndWithGroupCopyOptions(t *testing.T) {
	h := New(io.Discard, &HandlerOptions{
		Level:  slog.LevelDebug,
		Fields: []Field{MessageField, AttrsField},
		Colors: &ColorScheme{
			Key: []byte("\033[92m"),
		},
	})

	h2 := h.WithAttrs([]slog.Attr{slog.String("a", "1")})
	h3 := h.WithGroup("g")

	ph2, ok := h2.(*PrettyTextHandler)
	if !ok {
		t.Fatalf("WithAttrs did not return *PrettyTextHandler")
	}
	if len(ph2.opts.Fields) != 2 {
		t.Errorf("WithAttrs fields = %v, want 2", len(ph2.opts.Fields))
	}
	if ph2.opts.Colors == nil {
		t.Errorf("WithAttrs Colors = nil, want non-nil")
	}

	ph3, ok := h3.(*PrettyTextHandler)
	if !ok {
		t.Fatalf("WithGroup did not return *PrettyTextHandler")
	}
	if len(ph3.opts.Fields) != 2 {
		t.Errorf("WithGroup fields = %v, want 2", len(ph3.opts.Fields))
	}
	if ph3.opts.Colors == nil {
		t.Errorf("WithGroup Colors = nil, want non-nil")
	}
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
	handler := New(io.Discard, &HandlerOptions{
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
	handler := New(io.Discard, &HandlerOptions{
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
	prettyHandler := New(os.Stdout, &HandlerOptions{
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
