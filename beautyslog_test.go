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
	handler, err := New(io.Discard, "{time:15:04:05(gray)} {level:>5(blue)} {source} {message(white)} {attrs}", &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	})
	if err != nil {
		b.Fatal(err)
	}
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
	handler, err := New(io.Discard, "{time:15:04:05(gray)} {level:>5(blue)} {message(white)} {attrs}", &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelInfo,
	})
	if err != nil {
		b.Fatal(err)
	}
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
	prettyHandler, err := New(os.Stdout, "{attrs} {time:15:04:05(gray)} {level:>5(blue)} {source} {message(red)}", &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "password" || a.Key == "token" {
				return slog.String(a.Key, "*****")
			}
			return a
		},
	})
	if err != nil {
		panic(err)
	}

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

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		wantErr bool
	}{
		{
			name:    "simple format",
			format:  "{time} {level} {message} {attrs}",
			wantErr: false,
		},
		{
			name:    "format with time format",
			format:  "{time:15:04:05} {level} {message} {attrs}",
			wantErr: false,
		},
		{
			name:    "format with colors",
			format:  "{time:15:04:05(gray)} {level:>5(blue)} {source:<20(0x666666)} {message(white)} {attrs}",
			wantErr: false,
		},
		{
			name:    "format with alignment",
			format:  "{level:>5} {source:<20} {message}",
			wantErr: false,
		},
		{
			name:    "format with predefined time",
			format:  "{time:RFC3339} {level} {message}",
			wantErr: false,
		},
		{
			name:    "format without attrs",
			format:  "{time} {level} {message}",
			wantErr: false,
		},
		{
			name:    "format with literal text",
			format:  "[{time}] {level} - {message}",
			wantErr: false,
		},
		{
			name:    "unmatched brace",
			format:  "{time {level}",
			wantErr: true,
		},
		{
			name:    "unknown placeholder",
			format:  "{unknown} {level}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(io.Discard, tt.format, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseColor(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantAnsi string
		wantHex  uint32
		isHex    bool
		isEmpty  bool
	}{
		{
			name:     "named color gray",
			input:    "gray",
			wantAnsi: "\033[90m",
		},
		{
			name:     "named color blue",
			input:    "blue",
			wantAnsi: "\033[34m",
		},
		{
			name:    "hex color",
			input:   "0xFF5733",
			wantHex: 0xFF5733,
			isHex:   true,
		},
		{
			name:    "hex color with hash",
			input:   "#FF5733",
			wantHex: 0xFF5733,
			isHex:   true,
		},
		{
			name:    "empty color",
			input:   "",
			isEmpty: true,
		},
		{
			name:    "unknown color",
			input:   "unknowncolor",
			isEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseColor(tt.input)
			if got.isEmpty != tt.isEmpty {
				t.Errorf("parseColor() isEmpty = %v, want %v", got.isEmpty, tt.isEmpty)
			}
			if !tt.isEmpty && !tt.isHex && got.ansi != tt.wantAnsi {
				t.Errorf("parseColor() ansi = %q, want %q", got.ansi, tt.wantAnsi)
			}
			if tt.isHex && got.hex != tt.wantHex {
				t.Errorf("parseColor() hex = %x, want %x", got.hex, tt.wantHex)
			}
		})
	}
}

func TestAppendTime(t *testing.T) {
	h := &PrettyTextHandler{}
	testTime := time.Date(2024, 1, 15, 10, 30, 45, 123000000, time.UTC)

	tests := []struct {
		format   string
		expected string
	}{
		{"", "10:30:45.123"},
		{"15:04:05", "10:30:45"},
		{"2006-01-02", "2024-01-15"},
		{"RFC3339", "2024-01-15T10:30:45Z"},
		{"Unix", "1705314645"},
		{"UnixMilli", "1705314645123"},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			buf := make([]byte, 0, 64)
			got := string(h.appendTime(buf, testTime, tt.format))
			if got != tt.expected {
				t.Errorf("appendTime() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAppendAligned(t *testing.T) {
	h := &PrettyTextHandler{}

	tests := []struct {
		input    string
		width    int
		align    alignment
		expected string
	}{
		{"INFO", 6, alignRight, "  INFO"},
		{"INFO", 6, alignLeft, "INFO  "},
		{"INFO", 6, alignNone, "INFO"},
		{"INFO", 2, alignRight, "INFO"},
		{"INFO", 0, alignRight, "INFO"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			buf := make([]byte, 0, 64)
			got := string(h.appendAligned(buf, tt.input, tt.width, tt.align))
			if got != tt.expected {
				t.Errorf("appendAligned() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestWithAttrs(t *testing.T) {
	h1, err := New(io.Discard, "{time} {level} {message} {attrs}", nil)
	if err != nil {
		t.Fatal(err)
	}

	h2 := h1.WithAttrs([]slog.Attr{
		slog.String("app", "test"),
		slog.String("env", "dev"),
	})

	if h2 == h1 {
		t.Error("WithAttrs should return a new handler")
	}
}

func TestWithGroup(t *testing.T) {
	h1, err := New(io.Discard, "{time} {level} {message} {attrs}", nil)
	if err != nil {
		t.Fatal(err)
	}

	h2 := h1.WithGroup("request")

	if h2 == h1 {
		t.Error("WithGroup should return a new handler")
	}
}
