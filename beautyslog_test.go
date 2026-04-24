package beautyslog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

//go:noinline
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
	handler := Must(io.Discard, &slog.HandlerOptions{
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
	handler := Must(io.Discard, &slog.HandlerOptions{
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

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ParsedTemplate
		wantErr bool
	}{
		{
			name:  "empty string",
			input: "",
			want:  ParsedTemplate{},
		},
		{
			name:  "simple fields",
			input: "{time} {level} {message} {attrs}",
			want: ParsedTemplate{
				{IsField: true, Field: FieldTime},
				{IsField: false, Literal: " "},
				{IsField: true, Field: FieldLevel},
				{IsField: false, Literal: " "},
				{IsField: true, Field: FieldMessage},
				{IsField: false, Literal: " "},
				{IsField: true, Field: FieldAttrs},
			},
		},
		{
			name:  "brackets and literals",
			input: "[{level}] {time} {message} {attrs}",
			want: ParsedTemplate{
				{IsField: false, Literal: "["},
				{IsField: true, Field: FieldLevel},
				{IsField: false, Literal: "] "},
				{IsField: true, Field: FieldTime},
				{IsField: false, Literal: " "},
				{IsField: true, Field: FieldMessage},
				{IsField: false, Literal: " "},
				{IsField: true, Field: FieldAttrs},
			},
		},
		{
			name:  "source included",
			input: "{level} {source} {message}",
			want: ParsedTemplate{
				{IsField: true, Field: FieldLevel},
				{IsField: false, Literal: " "},
				{IsField: true, Field: FieldSource},
				{IsField: false, Literal: " "},
				{IsField: true, Field: FieldMessage},
			},
		},
		{
			name:    "unknown field",
			input:   "{unknown}",
			wantErr: true,
		},
		{
			name:    "unmatched brace",
			input:   "{time {level}",
			wantErr: true,
		},
		{
			name:    "unmatched brace at end",
			input:   "{time",
			wantErr: true,
		},
		{
			name:  "only literals",
			input: "foo bar baz",
			want: ParsedTemplate{
				{IsField: false, Literal: "foo bar baz"},
			},
		},
		{
			name:  "adjacent fields",
			input: "{level}{message}",
			want: ParsedTemplate{
				{IsField: true, Field: FieldLevel},
				{IsField: true, Field: FieldMessage},
			},
		},
		{
			name:  "trailing literal",
			input: "{time} | END",
			want: ParsedTemplate{
				{IsField: true, Field: FieldTime},
				{IsField: false, Literal: " | END"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTemplate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTemplate(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("seg[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestHandlerCustomLayout(t *testing.T) {
	var buf bytes.Buffer
	h, err := NewWithConfig(&buf, &slog.HandlerOptions{
		AddSource: true,
	}, &Config{
		Layout: Layout{FieldLevel, FieldTime, FieldMessage, FieldSource, FieldAttrs},
	})
	if err != nil {
		t.Fatal(err)
	}

	r := createTestRecord()
	if err := h.Handle(context.TODO(), r); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// level should come before time (no ANSI assumption)
	levelIdx := strings.Index(out, "INFO")
	timeIdx := strings.Index(out, ":") // time contains colon
	if levelIdx == -1 || timeIdx == -1 || levelIdx > timeIdx {
		t.Fatalf("expected level before time, got: %q", out)
	}
}

func TestHandlerCustomColors(t *testing.T) {
	var buf bytes.Buffer
	h, err := NewWithConfig(&buf, nil, &Config{
		Colors: ColorScheme{
			Info:    ColorRed,
			Time:    ColorGreen,
			Message: ColorYellow,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	r := createTestRecord()
	if err := h.Handle(context.TODO(), r); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// default info color is blue; we overrode it to red
	if strings.Contains(out, string(ColorBlue)) {
		t.Fatalf("expected no default blue (info color overridden), got: %q", out)
	}
	if !strings.Contains(out, string(ColorRed)) {
		t.Fatalf("expected custom info color (red) in output, got: %q", out)
	}
	if !strings.Contains(out, string(ColorYellow)) {
		t.Fatalf("expected custom message color (yellow) in output, got: %q", out)
	}
}

func TestHandlerNoColor(t *testing.T) {
	var buf bytes.Buffer
	h, err := NewWithConfig(&buf, nil, &Config{
		NoColor: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := createTestRecord()
	if err := h.Handle(context.TODO(), r); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	// no ANSI escape codes
	if strings.Contains(out, "\033[") {
		t.Fatalf("expected no ANSI codes with NoColor, got: %q", out)
	}
}

func TestHandlerTemplate(t *testing.T) {
	var buf bytes.Buffer
	h, err := NewWithConfig(&buf, &slog.HandlerOptions{
		AddSource: true,
	}, &Config{
		Template: "[{level}] {message} {time} {attrs}",
	})
	if err != nil {
		t.Fatal(err)
	}

	r := createTestRecord()
	if err := h.Handle(context.TODO(), r); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	openIdx := strings.Index(out, "[")
	infoIdx := strings.Index(out, "INFO")
	closeIdx := strings.Index(out, "]")
	if openIdx == -1 || infoIdx == -1 || closeIdx == -1 || openIdx >= infoIdx || infoIdx >= closeIdx {
		t.Fatalf("expected [INFO] ordering, got: %q", out)
	}
	if !strings.Contains(out, "Processing user request") {
		t.Fatalf("expected message in output, got: %q", out)
	}
	// ensure no double padding inside brackets
	if strings.Contains(out, "[INFO ]") {
		t.Fatalf("unexpected padding inside brackets, got: %q", out)
	}
}

func TestHandlerCustomLevelFallback(t *testing.T) {
	var buf bytes.Buffer
	h := Must(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	r := slog.Record{
		Time:    time.Now(),
		Level:   slog.Level(42),
		Message: "custom level msg",
	}
	if err := h.Handle(context.TODO(), r); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "custom level msg") {
		t.Fatalf("expected message, got: %q", out)
	}
}

func TestHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := Must(&buf, nil)
	clone := h.WithAttrs([]slog.Attr{slog.String("extra", "attr")}).(*PrettyTextHandler)

	if len(h.preAttrs) != 0 {
		t.Fatalf("original handler mutated")
	}
	if len(clone.preAttrs) != 1 || clone.preAttrs[0].Key != "extra" {
		t.Fatalf("unexpected preAttrs: %+v", clone.preAttrs)
	}

	if err := clone.Handle(context.TODO(), slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "hello",
	}); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "extra") || !strings.Contains(out, "attr") {
		t.Fatalf("expected WithAttrs in output, got: %q", out)
	}
}

func TestHandlerWithGroup(t *testing.T) {
	var buf bytes.Buffer
	h := Must(&buf, nil)
	clone := h.WithGroup("service").(*PrettyTextHandler)

	if h.group != "" {
		t.Fatalf("original handler mutated")
	}
	if clone.group != "service" {
		t.Fatalf("unexpected group: %q", clone.group)
	}

	record := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "hello",
	}
	record.Add(slog.String("key", "val"))
	if err := clone.Handle(context.TODO(), record); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "service.key") {
		t.Fatalf("expected group prefix, got: %q", out)
	}
}

func TestHandlerNestedGroup(t *testing.T) {
	var buf bytes.Buffer
	h := Must(&buf, nil)
	clone := h.WithGroup("a").WithGroup("b").(*PrettyTextHandler)

	if clone.group != "a.b" {
		t.Fatalf("expected nested group 'a.b', got %q", clone.group)
	}
}

func TestHandlerReplaceAttrDrop(t *testing.T) {
	var buf bytes.Buffer
	h := Must(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == "secret" {
				return slog.Attr{}
			}
			return a
		},
	})

	record := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "hello",
	}
	record.Add(slog.String("secret", "hidden"), slog.String("visible", "yes"))

	if err := h.Handle(context.TODO(), record); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "secret") {
		t.Fatalf("expected secret to be dropped, got: %q", out)
	}
	if !strings.Contains(out, "visible") || !strings.Contains(out, "yes") {
		t.Fatalf("expected visible attr, got: %q", out)
	}
}

func TestHandlerGroupReplaceAttr(t *testing.T) {
	var buf bytes.Buffer
	h := Must(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == "secret" {
				return slog.Attr{}
			}
			return a
		},
	})

	record := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "hello",
	}
	record.Add(slog.Group("g", slog.String("secret", "hidden"), slog.String("visible", "yes")))

	if err := h.Handle(context.TODO(), record); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "secret") {
		t.Fatalf("expected secret to be dropped inside group, got: %q", out)
	}
	if !strings.Contains(out, "visible") || !strings.Contains(out, "yes") {
		t.Fatalf("expected visible attr in group, got: %q", out)
	}
}

func TestHandlerAppendValueKinds(t *testing.T) {
	var buf bytes.Buffer
	h := Must(&buf, nil)

	record := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "kinds",
	}
	record.Add(
		slog.Uint64("u", 42),
		slog.Float64("f", 3.14),
		slog.Time("t", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
		slog.Any("b", []byte("hello")),
	)

	if err := h.Handle(context.TODO(), record); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "u") || !strings.Contains(out, "42") {
		t.Fatalf("expected uint64, got: %q", out)
	}
	if !strings.Contains(out, "f") || !strings.Contains(out, "3.14") {
		t.Fatalf("expected float64, got: %q", out)
	}
	if !strings.Contains(out, "2024-01-01") {
		t.Fatalf("expected time, got: %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected raw bytes, got: %q", out)
	}
}

func TestAppendDurationEdgeCases(t *testing.T) {
	cases := []struct {
		name     string
		d        time.Duration
		expected string
	}{
		{name: "zero", d: 0, expected: "0s"},
		{name: "negative", d: -time.Second, expected: "-1s"},
		{name: "subsecond", d: 123 * time.Millisecond, expected: "123ms"},
		{name: "negative subsecond", d: -500 * time.Millisecond, expected: "-500ms"},
		{name: "with nanos", d: 1500*time.Millisecond + 678*time.Microsecond + 500*time.Nanosecond, expected: "1.500678500s"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := appendDuration(nil, tc.d)
			if string(buf) != tc.expected {
				t.Fatalf("appendDuration(%v) = %q, want %q", tc.d, string(buf), tc.expected)
			}
		})
	}
}

func TestPartialColorOverride(t *testing.T) {
	var buf bytes.Buffer
	h, err := NewWithConfig(&buf, nil, &Config{
		Colors: ColorScheme{
			Group: "\033[95m",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	record := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "hello",
	}
	record.Add(slog.Group("g", slog.String("k", "v")))

	if err := h.Handle(context.TODO(), record); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "g") || !strings.Contains(out, "k") || !strings.Contains(out, "v") {
		t.Fatalf("expected group attr, got: %q", out)
	}
}

func TestNilConfigAndSlogOpts(t *testing.T) {
	var buf bytes.Buffer

	h, err := NewWithConfig(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.opts.Level.Level() != slog.LevelDebug {
		t.Fatalf("expected debug level")
	}

	buf.Reset()
	h2, err := NewWithConfig(&buf, nil, &Config{Layout: nil})
	if err != nil {
		t.Fatal(err)
	}
	if h2.opts.Level.Level() != slog.LevelInfo {
		t.Fatalf("expected info level default")
	}

	if err := h2.Handle(context.TODO(), slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "nil opts",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "nil opts") {
		t.Fatalf("expected output, got: %q", buf.String())
	}
}

func TestTemplateParseError(t *testing.T) {
	_, err := NewWithConfig(io.Discard, nil, &Config{Template: "{bad"})
	if err == nil {
		t.Fatalf("expected error for invalid template")
	}
}

func TestWithAttrsEmpty(t *testing.T) {
	h := Must(io.Discard, nil)
	clone := h.WithAttrs([]slog.Attr{})
	if clone != h {
		t.Fatalf("expected same handler for empty attrs")
	}
}

func TestWithGroupEmpty(t *testing.T) {
	h := Must(io.Discard, nil)
	clone := h.WithGroup("")
	if clone != h {
		t.Fatalf("expected same handler for empty group")
	}
}

func TestHandlerTemplateAdjacentFields(t *testing.T) {
	var buf bytes.Buffer
	h, err := NewWithConfig(&buf, nil, &Config{
		Template: "{level}{message}",
	})
	if err != nil {
		t.Fatal(err)
	}

	record := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "test",
	}

	if err := h.Handle(context.TODO(), record); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	infoIdx := strings.Index(out, "INFO")
	testIdx := strings.Index(out, "test")
	if infoIdx == -1 || testIdx == -1 {
		t.Fatalf("expected both INFO and test, got: %q", out)
	}
	between := out[infoIdx+len("INFO") : testIdx]
	if strings.Contains(between, " ") {
		t.Fatalf("unexpected space between adjacent fields: %q", out)
	}
}

func TestHandlerOnlyLiteralTemplate(t *testing.T) {
	var buf bytes.Buffer
	h, err := NewWithConfig(&buf, nil, &Config{
		Template: "[STATIC]",
	})
	if err != nil {
		t.Fatal(err)
	}

	record := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "msg",
	}

	if err := h.Handle(context.TODO(), record); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.HasPrefix(out, "[STATIC]") {
		t.Fatalf("expected literal output, got: %q", out)
	}
}

func TestPrint(_ *testing.T) {
	prettyHandler := Must(os.Stdout, &slog.HandlerOptions{
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

	// custom template + colors demo
	fmt.Println("\n--- custom template & colors ---")
	custom, err := NewWithConfig(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	}, &Config{
		Template: "[{level}] {time} {source} {message} {attrs}",
		Colors: ColorScheme{
			Debug:   ColorPurple,
			Info:    ColorGreen,
			Warn:    ColorYellow,
			Error:   ColorRed,
			Time:    ColorCyan,
			Source:  ColorGray,
			Key:     ColorBlue,
			Value:   ColorWhite,
			Message: ColorPeach,
		},
	})
	if err != nil {
		panic(err)
	}
	logger2 := slog.New(custom)
	logger2.Info("customized output", slog.String("foo", "bar"))
	logger2.Warn("watch out", slog.Int("count", 42))
}

func TestHandlerConcurrency(t *testing.T) {
	h := Must(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
	h = h.WithAttrs([]slog.Attr{slog.String("app", "test")}).WithGroup("g").(*PrettyTextHandler)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
			r.Add(slog.Int("i", i))
			if err := h.Handle(context.Background(), r); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}
