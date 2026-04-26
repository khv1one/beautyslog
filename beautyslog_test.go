package beautyslog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// ---------------------------------------------------------
// Existing benchmarks and smoke test
// ---------------------------------------------------------

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

// ---------------------------------------------------------
// New unit tests for Task 6 logic
// ---------------------------------------------------------

func TestBuildCache(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldLevel, FieldMessage},
		Theme: Theme{
			Time:    ColorGray,
			Source:  ColorGray,
			Key:     ColorGreen,
			Value:   ColorOrange,
			Message: ColorBlue,
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelDebug: {Name: "DBG", Color: ColorCyan},
			slog.LevelInfo:  {Name: "INFO", Color: ColorBlue},
			slog.LevelWarn:  {Name: "WARN", Color: ColorYellow},
		},
		TimeFormat: "15:04:05",
	})

	if len(h.fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(h.fields))
	}
	if h.timeFormat != "15:04:05" {
		t.Fatalf("expected timeFormat 15:04:05, got %s", h.timeFormat)
	}
	if len(h.levels) != 3 {
		t.Fatalf("expected 3 cached levels, got %d", len(h.levels))
	}

	cl := h.levels[slog.LevelDebug]
	if string(cl.name) != "DBG " {
		t.Fatalf("expected padded debug name 'DBG ', got '%s'", string(cl.name))
	}
	if string(cl.color) != string(ColorCyan) {
		t.Fatalf("expected debug color %q, got %q", string(ColorCyan), string(cl.color))
	}
}

func TestBuildCacheDisabledColors(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields:        []Field{FieldLevel},
		DisableColors: true,
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelError: {Name: "ERR", Color: ColorRed},
		},
	})

	if h.colorReset != nil {
		t.Fatal("expected colorReset to be nil when colors disabled")
	}
	cl := h.levels[slog.LevelError]
	if cl.color != nil {
		t.Fatalf("expected color to be nil when disabled, got %q", string(cl.color))
	}
}

func TestResolveFallbackColor(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		DisableColors: false,
		Fields:        []Field{FieldLevel},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	got := h.resolveFallbackColor(ColorRed)
	if string(got) != string(ColorRed) {
		t.Fatalf("expected fallback color %q, got %q", string(ColorRed), string(got))
	}

	hDisabled := NewWithConfig(&b, &Config{
		DisableColors: true,
		Fields:        []Field{FieldLevel},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	gotDisabled := hDisabled.resolveFallbackColor(ColorRed)
	if gotDisabled != nil {
		t.Fatalf("expected nil color when disabled, got %q", string(gotDisabled))
	}
}

func TestAppendColor(t *testing.T) {
	t.Parallel()

	buf := appendColor([]byte("x"), nil)
	if string(buf) != "x" {
		t.Fatalf("expected 'x', got '%s'", string(buf))
	}

	buf = appendColor([]byte("x"), []byte("\033[31m"))
	if string(buf) != "x\033[31m" {
		t.Fatalf("expected 'x\033[31m', got '%s'", string(buf))
	}
}

func TestHandleKnownLevel(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldLevel, FieldMessage},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo:  {Name: "INFO", Color: ColorBlue},
			slog.LevelError: {Name: "ERROR", Color: ColorRed},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "hello",
	}
	_ = h.Handle(context.Background(), r)

	out := b.String()
	if !strings.Contains(out, "INFO") {
		t.Fatalf("expected output to contain INFO, got %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected output to contain hello, got %q", out)
	}
}

func TestHandleUnknownLevel(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldLevel, FieldMessage},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.Level(42),
		Message: "custom",
	}
	_ = h.Handle(context.Background(), r)

	out := b.String()
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "ERROR+34") {
		t.Fatalf("expected output to contain 'ERROR+34', got stripped %q (raw %q)", stripped, out)
	}
	parts := strings.SplitN(stripped, " ", 3)
	if len(parts) < 1 || len(parts[0]) != len("ERROR+34") {
		t.Fatalf("expected unknown level len %d, got level part %q from %q", len("ERROR+34"), parts[0], stripped)
	}
}

func TestHandleFieldOrder(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldMessage, FieldLevel},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "I", Color: ColorBlue},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
	}
	_ = h.Handle(context.Background(), r)

	out := b.String()
	idxMsg := strings.Index(out, "msg")
	idxLvl := strings.Index(out, "I")
	if idxMsg == -1 || idxLvl == -1 {
		t.Fatalf("expected both msg and level in output, got %q", out)
	}
	if idxMsg > idxLvl {
		t.Fatalf("expected message before level in output %q", out)
	}
}

func TestHandleMessageInheritsLevelColor(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldMessage},
		Theme: Theme{
			// Message empty so it inherits level color
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "hello",
	}
	_ = h.Handle(context.Background(), r)

	out := b.String()
	if !strings.Contains(out, string(ColorBlue)) {
		t.Fatalf("expected message to inherit level color %q, got %q", string(ColorBlue), out)
	}
}

func TestHandleAttributes(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldAttributes},
		Theme: Theme{
			Key:   ColorGreen,
			Value: ColorOrange,
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
	}
	r.Add(slog.String("key1", "val1"))
	r.Add(slog.Int("num", 42))
	_ = h.Handle(context.Background(), r)

	out := b.String()
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "key1=val1") {
		t.Fatalf("expected key1=val1 in output, got stripped %q (raw %q)", stripped, out)
	}
	if !strings.Contains(stripped, "num=42") {
		t.Fatalf("expected num=42 in output, got stripped %q (raw %q)", stripped, out)
	}
}

func TestHandleReplaceAttr(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldAttributes},
		HandlerOptions: slog.HandlerOptions{
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == "secret" {
					return slog.String(a.Key, "***")
				}
				return a
			},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
	}
	r.Add(slog.String("secret", "password123"))
	_ = h.Handle(context.Background(), r)

	out := b.String()
	stripped := stripANSI(out)
	if strings.Contains(stripped, "password123") {
		t.Fatalf("expected secret to be replaced, got %q", stripped)
	}
	if !strings.Contains(stripped, "secret=***") {
		t.Fatalf("expected secret=***, got %q", stripped)
	}
}

func TestAppendValueKinds(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldAttributes},
	})

	tests := []struct {
		name     string
		val      slog.Value
		expected string
	}{
		{"string", slog.StringValue("hello"), "hello"},
		{"bool", slog.BoolValue(true), "true"},
		{"int64", slog.Int64Value(-42), "-42"},
		{"uint64", slog.Uint64Value(42), "42"},
		{"float64", slog.Float64Value(3.14), "3.14"},
		{"duration", slog.DurationValue(time.Hour), "3600"},
		{"any_error", slog.AnyValue(errors.New("boom")), "boom"},
		{"byte_slice", slog.AnyValue([]byte("raw")), "raw"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buf := h.appendValue([]byte{}, tt.val)
			if !strings.Contains(string(buf), tt.expected) {
				t.Fatalf("expected %q in buffer, got %q", tt.expected, string(buf))
			}
		})
	}
}

func TestAppendValueGroup(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldAttributes},
		Theme: Theme{
			Group: ColorPurple,
			Key:   ColorGreen,
			Value: ColorOrange,
		},
	})

	val := slog.GroupValue(
		slog.String("a", "1"),
		slog.Int("b", 2),
	)
	buf := h.appendValue([]byte{}, val)
	out := string(buf)
	stripped := stripANSI(out)

	if !strings.Contains(stripped, "a=1") {
		t.Fatalf("expected a=1 in group output, got stripped %q (raw %q)", stripped, out)
	}
	if !strings.Contains(stripped, "b=2") {
		t.Fatalf("expected b=2 in group output, got stripped %q (raw %q)", stripped, out)
	}
	if !strings.Contains(stripped, "(") || !strings.Contains(stripped, ")") {
		t.Fatalf("expected parentheses around group, got stripped %q (raw %q)", stripped, out)
	}
}

func TestUnknownLevelZeroCached(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldLevel},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelDebug: {Name: "D", Color: ColorCyan},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.Level(99),
		Message: "x",
	}
	_ = h.Handle(context.Background(), r)

	out := b.String()
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "ERROR+91") {
		t.Fatalf("expected padded unknown level 'ERROR+91', got stripped %q (raw %q)", stripped, out)
	}
}

func TestHandleWithGroup(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldAttributes},
	})

	h2 := h.WithGroup("api").(*PrettyTextHandler)

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
	}
	r.Add(slog.String("host", "localhost"))
	_ = h2.Handle(context.Background(), r)

	out := b.String()
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "api.host=localhost") {
		t.Fatalf("expected grouped attr key api.host=localhost, got stripped %q (raw %q)", stripped, out)
	}
}

// ---------------------------------------------------------
// Task 8: Additional customization coverage
// ---------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	expectedFields := []Field{FieldTime, FieldSource, FieldLevel, FieldMessage, FieldAttributes}
	if len(cfg.Fields) != len(expectedFields) {
		t.Fatalf("expected %d fields, got %d", len(expectedFields), len(cfg.Fields))
	}
	for i, f := range expectedFields {
		if cfg.Fields[i] != f {
			t.Fatalf("expected field %d to be %v, got %v", i, f, cfg.Fields[i])
		}
	}

	if cfg.TimeFormat != "15:04:05.999" {
		t.Fatalf("expected TimeFormat 15:04:05.999, got %s", cfg.TimeFormat)
	}

	if cfg.Theme.Time != ColorGray {
		t.Fatalf("expected Theme.Time %q, got %q", ColorGray, cfg.Theme.Time)
	}
	if cfg.Theme.Source != ColorGray {
		t.Fatalf("expected Theme.Source %q, got %q", ColorGray, cfg.Theme.Source)
	}
	if cfg.Theme.Key != ColorGreen {
		t.Fatalf("expected Theme.Key %q, got %q", ColorGreen, cfg.Theme.Key)
	}
	if cfg.Theme.Value != ColorOrange {
		t.Fatalf("expected Theme.Value %q, got %q", ColorOrange, cfg.Theme.Value)
	}
	if cfg.Theme.Group != ColorPurple {
		t.Fatalf("expected Theme.Group %q, got %q", ColorPurple, cfg.Theme.Group)
	}
	if cfg.Theme.Message != "" {
		t.Fatalf("expected Theme.Message to be empty, got %q", cfg.Theme.Message)
	}

	expectedLevels := map[slog.Level]LevelConfig{
		slog.LevelDebug: {Name: "DEBUG", Color: ColorCyan},
		slog.LevelInfo:  {Name: "INFO", Color: ColorBlue},
		slog.LevelWarn:  {Name: "WARN", Color: ColorYellow},
		slog.LevelError: {Name: "ERROR", Color: ColorRed},
	}
	if len(cfg.LevelConfigs) != len(expectedLevels) {
		t.Fatalf("expected %d level configs, got %d", len(expectedLevels), len(cfg.LevelConfigs))
	}
	for lvl, expected := range expectedLevels {
		got, ok := cfg.LevelConfigs[lvl]
		if !ok {
			t.Fatalf("missing level config for %v", lvl)
		}
		if got.Name != expected.Name || got.Color != expected.Color {
			t.Fatalf("expected level %v config %+v, got %+v", lvl, expected, got)
		}
	}
}

func TestNewWithConfig_DisableColors(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields:        []Field{FieldLevel, FieldMessage},
		DisableColors: true,
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "no colors",
	}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	if strings.Contains(out, "\033[") {
		t.Fatalf("expected no ANSI escape sequences, got %q", out)
	}
}

func TestHandle_CustomTheme(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldAttributes},
		Theme: Theme{
			Key:   ColorBlue,
			Value: ColorRed,
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
	}
	r.Add(slog.String("mykey", "myval"))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	if !strings.Contains(out, string(ColorBlue)) {
		t.Fatalf("expected blue ANSI before key, got %q", out)
	}
	if !strings.Contains(out, string(ColorRed)) {
		t.Fatalf("expected red ANSI before value, got %q", out)
	}
}

func TestHandle_CustomLevelConfig(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldLevel},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INF", Color: ColorGreen},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
	}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	if !strings.Contains(out, "INF") {
		t.Fatalf("expected output to contain 'INF', got %q", out)
	}
}

func TestNewWithConfig_NilConfig(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, nil)

	if len(h.fields) != 5 {
		t.Fatalf("expected default 5 fields, got %d", len(h.fields))
	}
	if h.timeFormat != "15:04:05.999" {
		t.Fatalf("expected default time format, got %s", h.timeFormat)
	}

	// Verify it works
	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "default config works",
	}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	if !strings.Contains(stripANSI(out), "INFO") {
		t.Fatalf("expected INFO in output, got %q", out)
	}
}

func TestHandle_EmptyFields(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "nothing",
	}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	// With empty fields, only newline should be emitted
	if out != "\n" {
		t.Fatalf("expected only newline for empty fields, got %q", out)
	}
}

func TestHandle_CustomTimeFormat(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields:     []Field{FieldTime},
		TimeFormat: time.RFC3339,
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	now := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	r := slog.Record{
		Time:    now,
		Level:   slog.LevelInfo,
		Message: "msg",
	}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	stripped := stripANSI(out)
	expected := now.Format(time.RFC3339)
	if !strings.Contains(stripped, expected) {
		t.Fatalf("expected time format %q in output, got %q", expected, stripped)
	}
}

func TestHandle_MissingSourceField(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	// AddSource true, but FieldSource NOT in Fields
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldTime, FieldLevel, FieldMessage},
		HandlerOptions: slog.HandlerOptions{
			AddSource: true,
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
		PC:      getPC(),
	}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	stripped := stripANSI(out)
	// Should NOT contain source file info despite AddSource=true
	if strings.Contains(stripped, "beautyslog_test.go:") {
		t.Fatalf("expected no source info when FieldSource not in Fields, got %q", stripped)
	}
}

func TestHandle_NO_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldLevel, FieldMessage, FieldAttributes},
		Theme: Theme{
			Key:   ColorGreen,
			Value: ColorOrange,
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "no color",
	}
	r.Add(slog.String("key", "val"))
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	if strings.Contains(out, "\033[") {
		t.Fatalf("expected no ANSI escape sequences with NO_COLOR=1, got %q", out)
	}
}

func TestHandle_EmptyLevelConfigs(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields:       []Field{FieldLevel, FieldMessage},
		LevelConfigs: map[slog.Level]LevelConfig{},
	})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "empty levels",
	}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "INFO") {
		t.Fatalf("expected 'INFO' (from slog default) in output, got %q", stripped)
	}
	if !strings.Contains(stripped, "empty levels") {
		t.Fatalf("expected message in output, got %q", stripped)
	}
}

func TestBuildCache_EmptyLevelConfigs(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields:       []Field{FieldLevel},
		LevelConfigs: map[slog.Level]LevelConfig{},
	})

	if len(h.levels) != 0 {
		t.Fatalf("expected empty levels cache, got %d", len(h.levels))
	}

	// Verify unknown level doesn't panic. slog.LevelWarn == 4, so Level(7) == WARN+3.
	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.Level(7),
		Message: "x",
	}
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "WARN+3") {
		t.Fatalf("expected fallback level name WARN+3, got %q", stripped)
	}
}

func TestWithAttrs_PropagatesConfig(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields:     []Field{FieldLevel, FieldMessage, FieldAttributes},
		TimeFormat: "15:04",
		Theme: Theme{
			Key: ColorGreen,
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "I", Color: ColorBlue},
		},
	})

	h2 := h.WithAttrs([]slog.Attr{slog.String("version", "1.0")}).(*PrettyTextHandler)

	if !reflect.DeepEqual(h2.fields, h.fields) {
		t.Fatalf("WithAttrs did not propagate fields")
	}
	if h2.timeFormat != h.timeFormat {
		t.Fatalf("WithAttrs did not propagate timeFormat")
	}
	if !reflect.DeepEqual(h2.levels, h.levels) {
		t.Fatalf("WithAttrs did not propagate levels")
	}

	// Ensure preAttrs are appended
	if len(h2.preAttrs) != 1 {
		t.Fatalf("expected 1 preAttr, got %d", len(h2.preAttrs))
	}

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
	}
	if err := h2.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "version=1.0") {
		t.Fatalf("expected WithAttrs attribute in output, got %q", stripped)
	}
}

func TestWithGroup_PropagatesConfig(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields:     []Field{FieldAttributes},
		TimeFormat: "15:04",
		Theme: Theme{
			Group: ColorPurple,
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "I", Color: ColorBlue},
		},
	})

	h2 := h.WithGroup("svc").(*PrettyTextHandler)

	if !reflect.DeepEqual(h2.fields, h.fields) {
		t.Fatalf("WithGroup did not propagate fields")
	}
	if h2.timeFormat != h.timeFormat {
		t.Fatalf("WithGroup did not propagate timeFormat")
	}
	if h2.group != "svc" {
		t.Fatalf("expected group 'svc', got %q", h2.group)
	}

	// Test nested groups also propagate config
	h3 := h2.WithGroup("v1").(*PrettyTextHandler)
	if h3.group != "svc.v1" {
		t.Fatalf("expected nested group 'svc.v1', got %q", h3.group)
	}
	if !reflect.DeepEqual(h3.levels, h.levels) {
		t.Fatalf("WithGroup did not propagate levels through nesting")
	}
}

func TestHandle_EmptyRecord(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields:     []Field{FieldTime, FieldLevel, FieldMessage},
		TimeFormat: "15:04:05.999",
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	var r slog.Record
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	out := b.String()
	stripped := stripANSI(out)
	if !strings.Contains(stripped, "INFO") {
		t.Fatalf("expected INFO in output for empty record, got %q", stripped)
	}
	// Zero time formatted with "15:04:05.999" produces "00:00:00" (no fractional seconds for zero nanoseconds)
	if !strings.HasPrefix(stripped, "00:00:00") {
		t.Fatalf("expected zero time prefix, got %q", stripped)
	}
}

func TestConcurrency_WithAttrsAndHandle(t *testing.T) {
	// Use io.Discard because bytes.Buffer is not safe for concurrent writes.
	h := NewWithConfig(io.Discard, &Config{
		Fields: []Field{FieldLevel, FieldMessage},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h2 := h.WithAttrs([]slog.Attr{slog.Int("id", i)})
			r := slog.Record{
				Time:    time.Now(),
				Level:   slog.LevelInfo,
				Message: "concurrent",
			}
			_ = h2.Handle(context.Background(), r)
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------
// Fuzz testing
// ---------------------------------------------------------

func FuzzAppendValue(f *testing.F) {
	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldAttributes},
		Theme: Theme{
			Group: ColorPurple,
			Key:   ColorGreen,
			Value: ColorOrange,
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	// Seed corpus with various kinds
	f.Add("hello", int64(0), uint64(0), float64(0), true, []byte("hello"))
	f.Add("", int64(0), uint64(0), float64(0), false, []byte{})
	f.Add("special\t\n\r", int64(-42), uint64(42), float64(3.14), true, []byte("raw bytes"))

	f.Fuzz(func(t *testing.T, s string, i int64, u uint64, fl float64, b bool, bs []byte) {
		values := []slog.Value{
			slog.StringValue(s),
			slog.Int64Value(i),
			slog.Uint64Value(u),
			slog.Float64Value(fl),
			slog.BoolValue(b),
			slog.AnyValue(bs),
			slog.DurationValue(time.Duration(i)),
			slog.TimeValue(time.Unix(i, 0)),
			slog.GroupValue(slog.String("k", s)),
		}

		for _, v := range values {
			buf := h.appendValue([]byte{}, v)
			if buf == nil {
				t.Fatal("appendValue returned nil")
			}
			_ = string(buf) // ensure no panic converting to string
		}
	})
}

// ---------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------

func BenchmarkPrettyTextHandlerWithConfig(b *testing.B) {
	cfg := &Config{
		Fields: []Field{FieldTime, FieldSource, FieldLevel, FieldMessage, FieldAttributes},
		Theme: Theme{
			Time:    ColorGray,
			Source:  ColorGray,
			Key:     ColorGreen,
			Value:   ColorOrange,
			Group:   ColorPurple,
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelDebug: {Name: "DEBUG", Color: ColorCyan},
			slog.LevelInfo:  {Name: "INFO", Color: ColorBlue},
			slog.LevelWarn:  {Name: "WARN", Color: ColorYellow},
			slog.LevelError: {Name: "ERROR", Color: ColorRed},
		},
		TimeFormat: "15:04:05.999",
	}

	handler := NewWithConfig(io.Discard, cfg)
	record := createTestRecord()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := handler.Handle(context.TODO(), record); err != nil {
			b.Fatal(err)
		}
	}
}

// TestRace_ParentAndClones tests the critical fix for shared mu across clones.
// Parent and clone must synchronize on the same mutex when writing to the same io.Writer.
func TestRace_ParentAndClones(t *testing.T) {
	var b bytes.Buffer
	h := NewWithConfig(&b, &Config{
		Fields: []Field{FieldLevel, FieldMessage},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelInfo: {Name: "INFO", Color: ColorBlue},
		},
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h2 := h.WithAttrs([]slog.Attr{slog.Int("id", i)})
			r := slog.Record{
				Time:    time.Now(),
				Level:   slog.LevelInfo,
				Message: "concurrent",
			}
			_ = h2.Handle(context.Background(), r)
		}(i)
	}
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := slog.Record{
				Time:    time.Now(),
				Level:   slog.LevelInfo,
				Message: "parent",
			}
			_ = h.Handle(context.Background(), r)
		}()
	}
	wg.Wait()
}

// TestAppendDuration_Negative verifies negative durations are formatted correctly
// after the allocation fix.
func TestAppendDuration_Negative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		d        time.Duration
		expected string
	}{
		{"zero", 0, "0s"},
		{"positive_second", time.Second, "1s"},
		{"positive_millis", 500 * time.Millisecond, "500ms"},
		{"positive_micro", 750 * time.Microsecond, "0.75ms"},
		{"negative_second", -time.Second, "-1s"},
		{"negative_millis", -500 * time.Millisecond, "-500ms"},
		{"negative_micro", -750 * time.Microsecond, "-0.75ms"},
		{"with_nanos", 123456789 * time.Nanosecond, "123.456789ms"},
		{"negative_with_nanos", -123456789 * time.Nanosecond, "-123.456789ms"},
		{"large", 3661 * time.Second, "3661s"},
		{"negative_large", -3661 * time.Second, "-3661s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buf := appendDuration([]byte{}, tt.d)
			got := string(buf)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
