package beautyslog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"regexp"
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
	handler := New(io.Discard, &Config{HandlerOptions: slog.HandlerOptions{
		AddSource: true,
		Level:     slog.LevelInfo,
	}})
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
	handler := New(io.Discard, &Config{HandlerOptions: slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelInfo,
	}})
	record := createTestRecord()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := handler.Handle(context.TODO(), record); err != nil {
			b.Fatal(err)
		}
	}
}

func TestManualOutput(t *testing.T) {
	prettyHandler := New(os.Stdout, &Config{HandlerOptions: slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == "password" || a.Key == "token" {
				return slog.String(a.Key, "*****")
			}
			return a
		},
	}})

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

func TestNewNilConfig(t *testing.T) {
	h := New(io.Discard, nil)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.cfg.Level != nil && h.cfg.Level.Level() != slog.LevelInfo {
		t.Fatalf("expected default LevelInfo, got %v", h.cfg.Level)
	}
}

func TestNewCustomConfig(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelDebug},
		Fields:         []Field{FieldTime, FieldMessage},
	}
	h := New(io.Discard, cfg)
	if h.cfg.Level.Level() != slog.LevelDebug {
		t.Fatalf("expected LevelDebug, got %v", h.cfg.Level)
	}
	if len(h.cfg.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(h.cfg.Fields))
	}
}

func TestNewDefaultFieldsWhenNil(t *testing.T) {
	cfg := &Config{HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo}}
	h := New(io.Discard, cfg)
	if len(h.cfg.Fields) != len(DefaultFields) {
		t.Fatalf("expected %d default fields, got %d", len(DefaultFields), len(h.cfg.Fields))
	}
}

func TestNewDefaultLevelWhenNil(t *testing.T) {
	cfg := &Config{}
	h := New(io.Discard, cfg)
	if h.cfg.Level.Level() != slog.LevelInfo {
		t.Fatalf("expected LevelInfo, got %v", h.cfg.Level)
	}
}

func TestNewDoesNotMutateDefaultConfig(t *testing.T) {
	h := New(io.Discard, nil)
	h.cfg.Fields[0] = FieldMessage
	// DefaultFields should be unchanged
	if DefaultFields[0] != FieldTime {
		t.Fatalf("DefaultFields was mutated")
	}
	_ = h
}

func TestMergeThemeFillsEmptyColors(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		Theme:          Theme{}, // all zero values
	}
	h := New(io.Discard, cfg)
	if string(h.cfg.Theme.TimeColor) != string(DefaultTheme.TimeColor) {
		t.Errorf("TimeColor not merged from default")
	}
	if string(h.cfg.Theme.SourceColor) != string(DefaultTheme.SourceColor) {
		t.Errorf("SourceColor not merged from default")
	}
	if string(h.cfg.Theme.AttributeKeyColor) != string(DefaultTheme.AttributeKeyColor) {
		t.Errorf("AttributeKeyColor not merged from default")
	}
	if string(h.cfg.Theme.AttributeValueColor) != string(DefaultTheme.AttributeValueColor) {
		t.Errorf("AttributeValueColor not merged from default")
	}
	if string(h.cfg.Theme.GroupColor) != string(DefaultTheme.GroupColor) {
		t.Errorf("GroupColor not merged from default")
	}
	if string(h.cfg.Theme.Reset) != string(DefaultTheme.Reset) {
		t.Errorf("Reset not merged from default")
	}
	if len(h.cfg.Theme.LevelColors) != len(DefaultTheme.LevelColors) {
		t.Errorf("LevelColors not merged from default")
	}
}

func TestMergeThemePreservesSetColors(t *testing.T) {
	customTime := []byte("\033[33m")
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		Theme: Theme{
			TimeColor: customTime,
		},
	}
	h := New(io.Discard, cfg)
	if string(h.cfg.Theme.TimeColor) != string(customTime) {
		t.Errorf("TimeColor should be preserved as custom, got %q", h.cfg.Theme.TimeColor)
	}
	if string(h.cfg.Theme.SourceColor) != string(DefaultTheme.SourceColor) {
		t.Errorf("SourceColor should be filled from default")
	}
}

func TestCopyLevelColors(t *testing.T) {
	cp := copyLevelColors(DefaultTheme.LevelColors)
	if len(cp) != len(DefaultTheme.LevelColors) {
		t.Fatalf("expected %d entries, got %d", len(DefaultTheme.LevelColors), len(cp))
	}
	// Mutating the copy should not affect the original
	cp[slog.LevelDebug] = []byte("X")
	if string(DefaultTheme.LevelColors[slog.LevelDebug]) == "X" {
		t.Fatal("copyLevelColors did not deep-copy the map")
	}
}

func TestCopyLevelColorsNil(t *testing.T) {
	cp := copyLevelColors(nil)
	if cp != nil {
		t.Fatalf("expected nil, got %v", cp)
	}
}

func TestCopyFields(t *testing.T) {
	cp := copyFields(DefaultFields)
	if len(cp) != len(DefaultFields) {
		t.Fatalf("expected %d fields, got %d", len(DefaultFields), len(cp))
	}
	cp[0] = FieldMessage
	if DefaultFields[0] != FieldTime {
		t.Fatal("copyFields did not deep-copy the slice")
	}
}

func TestFieldConstants(t *testing.T) {
	fields := []Field{FieldTime, FieldSource, FieldLevel, FieldMessage, FieldAttributes}
	for i, f := range fields {
		if int(f) != i {
			t.Errorf("expected Field constant %d to have value %d, got %d", i, i, f)
		}
	}
}

func TestEnabledRespectsLevel(t *testing.T) {
	h := New(io.Discard, &Config{HandlerOptions: slog.HandlerOptions{Level: slog.LevelWarn}})
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Info to be disabled when level is Warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("expected Error to be enabled when level is Warn")
	}
}

func TestWithAttrsPreservesConfig(t *testing.T) {
	h := New(io.Discard, &Config{HandlerOptions: slog.HandlerOptions{Level: slog.LevelDebug}})
	h2 := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	h2t := h2.(*PrettyTextHandler)
	if h2t.cfg.Level.Level() != slog.LevelDebug {
		t.Error("WithAttrs did not preserve config")
	}
}

func TestWithGroupPreservesConfig(t *testing.T) {
	h := New(io.Discard, &Config{HandlerOptions: slog.HandlerOptions{Level: slog.LevelDebug}})
	h2 := h.WithGroup("mygroup")
	h2t := h2.(*PrettyTextHandler)
	if h2t.cfg.Level.Level() != slog.LevelDebug {
		t.Error("WithGroup did not preserve config")
	}
}

// captureOutput writes handler output to a buffer and returns the string.
func captureOutput(h *PrettyTextHandler, r slog.Record) string {
	var buf bytes.Buffer
	h.out = &buf
	_ = h.Handle(context.Background(), r)
	return buf.String()
}

func TestCustomFieldsOrder(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		Fields:         []Field{FieldLevel, FieldMessage, FieldTime, FieldAttributes},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
	}
	r.Add(slog.String("key", "val"))

	out := captureOutput(h, r)

	// Find positions: level should appear before message, message before time, time before attributes.
	// Use simple substrings since ANSI codes interleave.
	levelIdx := strings.Index(out, "INFO")
	msgIdx := strings.Index(out, "hello")
	timeIdx := strings.Index(out, "12:00:00")
	attrIdx := strings.Index(out, "key")

	if levelIdx == -1 {
		t.Fatal("level not found in output")
	}
	if msgIdx == -1 {
		t.Fatal("message not found in output")
	}
	if timeIdx == -1 {
		t.Fatal("time not found in output")
	}
	if attrIdx == -1 {
		t.Fatalf("attribute key not found in output: %q", out)
	}

	if levelIdx >= msgIdx || msgIdx >= timeIdx || timeIdx >= attrIdx {
		t.Fatalf("fields not in custom order: level=%d, msg=%d, time=%d, attr=%d\noutput: %q", levelIdx, msgIdx, timeIdx, attrIdx, out)
	}
}

func TestCustomTheme(t *testing.T) {
	customTime := []byte("\033[95m")   // magenta
	customSource := []byte("\033[96m") // cyan
	customLevel := map[slog.Level][]byte{
		slog.LevelInfo: []byte("\033[92m"), // green
	}
	customKey := []byte("\033[93m")   // yellow
	customValue := []byte("\033[94m") // blue
	customGroup := []byte("\033[97m") // white
	customReset := []byte("\033[0m")

	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo, AddSource: true},
		Theme: Theme{
			TimeColor:           customTime,
			SourceColor:         customSource,
			LevelColors:         customLevel,
			AttributeKeyColor:   customKey,
			AttributeValueColor: customValue,
			GroupColor:          customGroup,
			Reset:               customReset,
		},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
		PC:      getPC(),
	}
	r.Add(slog.String("attr_key", "attr_val"))

	out := captureOutput(h, r)

	if !strings.Contains(out, string(customTime)) {
		t.Error("custom TimeColor not found in output")
	}
	if !strings.Contains(out, string(customSource)) {
		t.Error("custom SourceColor not found in output")
	}
	if !strings.Contains(out, string(customLevel[slog.LevelInfo])) {
		t.Error("custom LevelColor not found in output")
	}
	if !strings.Contains(out, string(customKey)) {
		t.Errorf("custom AttributeKeyColor not found in output: %q", out)
	}
	if !strings.Contains(out, string(customValue)) {
		t.Errorf("custom AttributeValueColor not found in output: %q", out)
	}
}

func TestSourceFallback(t *testing.T) {
	// Fields without FieldSource, but AddSource=true should auto-insert it.
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo, AddSource: true},
		Fields:         []Field{FieldTime, FieldLevel, FieldMessage},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
		PC:      getPC(),
	}

	out := captureOutput(h, r)

	// Source (file:line) should appear in output despite not being in Fields.
	if !strings.Contains(out, "beautyslog_test.go:") {
		t.Fatalf("expected source to appear in output, got: %q", out)
	}
}

func TestAddSourceDisabled(t *testing.T) {
	// FieldSource in Fields, but AddSource=false: source should NOT appear.
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo, AddSource: false},
		Fields:         []Field{FieldTime, FieldSource, FieldLevel, FieldMessage},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
		PC:      getPC(),
	}

	out := captureOutput(h, r)

	// Source should NOT appear since AddSource is false.
	if strings.Contains(out, "_test.go:") {
		t.Fatalf("source should not appear when AddSource=false, got: %q", out)
	}
}

func TestMessageColorFallback(t *testing.T) {
	// MessageColors is nil (default), so message color should fall back to LevelColors.
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		Theme: Theme{
			LevelColors: map[slog.Level][]byte{
				slog.LevelInfo: []byte("\033[92m"), // green
			},
			MessageColors: nil, // nil means fallback to LevelColors
		},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
	}

	out := captureOutput(h, r)

	expectedColor := string(cfg.Theme.LevelColors[slog.LevelInfo])
	if !strings.Contains(out, expectedColor) {
		t.Errorf("expected message to use LevelColors fallback (green), got: %q", out)
	}

	// Ensure the message text appears.
	if !strings.Contains(out, "hello") {
		t.Errorf("expected 'hello' in output, got: %q", out)
	}
}

func TestNewDeepCopyPreventsDataRace(t *testing.T) {
	// Provide non-nil maps/slices and verify that mutating the caller's
	// copy after New() does not affect the handler.
	callerLevelColors := map[slog.Level][]byte{
		slog.LevelInfo:  []byte("\033[34m"),
		slog.LevelDebug: []byte("\033[36m"),
	}
	callerFields := []Field{FieldTime, FieldLevel, FieldMessage}

	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		Theme:          Theme{LevelColors: callerLevelColors},
		Fields:         callerFields,
	}
	h := New(io.Discard, cfg)

	// Mutate the caller's map and slice after New().
	callerLevelColors[slog.LevelInfo] = []byte("MUTATED")
	callerFields[0] = FieldAttributes

	// The handler should have its own copy, unaffected by the mutation.
	if string(h.cfg.Theme.LevelColors[slog.LevelInfo]) == "MUTATED" {
		t.Fatal("handler shares LevelColors map with caller — data race risk")
	}
	if h.cfg.Fields[0] == FieldAttributes {
		t.Fatal("handler shares Fields slice with caller — data race risk")
	}
}

func TestWithGroupPrecomputesGroups(t *testing.T) {
	h := New(io.Discard, &Config{HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo}})
	h2 := h.WithGroup("foo").WithGroup("bar")
	h2t := h2.(*PrettyTextHandler)
	if h2t.group != "foo.bar" {
		t.Fatalf("expected group 'foo.bar', got %q", h2t.group)
	}
	if len(h2t.groups) != 2 || h2t.groups[0] != "foo" || h2t.groups[1] != "bar" {
		t.Fatalf("expected groups [foo, bar], got %v", h2t.groups)
	}
}

func TestWithGroupEmptyReturnsSame(t *testing.T) {
	h := New(io.Discard, &Config{HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo}})
	h2 := h.WithGroup("")
	if h2 != h {
		t.Fatal("expected same handler for empty group name")
	}
}

func TestWithAttrsEmptyReturnsSame(t *testing.T) {
	h := New(io.Discard, &Config{HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo}})
	h2 := h.WithAttrs(nil)
	if h2 != h {
		t.Fatal("expected same handler for empty attrs")
	}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func TestExportedColorConstants(t *testing.T) {
	if len(ColorReset) == 0 {
		t.Error("ColorReset is empty")
	}
	if len(ColorDebug) == 0 {
		t.Error("ColorDebug is empty")
	}
	if len(ColorInfo) == 0 {
		t.Error("ColorInfo is empty")
	}
	if len(ColorWarn) == 0 {
		t.Error("ColorWarn is empty")
	}
	if len(ColorError) == 0 {
		t.Error("ColorError is empty")
	}
}

func TestSourceFormatShort(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo, AddSource: true},
		SourceFormat:   SourceShort,
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
		PC:      getPC(),
	}

	out := captureOutput(h, r)

	// SourceShort should have basename only, not full path
	if strings.Contains(out, "/beautyslog/") {
		t.Fatalf("SourceShort should not contain full path, got: %q", out)
	}
	if !strings.Contains(out, "beautyslog_test.go:") {
		t.Fatalf("SourceShort should contain basename:line, got: %q", out)
	}
}

func TestTimeFormatCustom(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		TimeFormat:     "15:04",
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
	}

	out := captureOutput(h, r)
	plain := stripANSI(out)

	if !strings.Contains(plain, "12:00") {
		t.Fatalf("expected '12:00' in output, got: %q", plain)
	}
	if strings.Contains(plain, "12:00:00") {
		t.Fatalf("expected no seconds in output with '15:04' format, got: %q", plain)
	}
}

func TestFieldWidthLevel(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		FieldWidths:    map[Field]int{FieldLevel: 8},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
	}

	out := captureOutput(h, r)
	plain := stripANSI(out)

	// "INFO" is 4 chars. With width 8, we should have 4 spaces after it.
	// Check that "INFO" is followed by at least 4 spaces before the message.
	idx := strings.Index(plain, "INFO")
	if idx == -1 {
		t.Fatal("INFO not found")
	}
	afterINFO := plain[idx+4:]
	spaceCount := 0
	for i := 0; i < len(afterINFO) && afterINFO[i] == ' '; i++ {
		spaceCount++
	}
	// spaceCount should be >= 4 (8 width - 4 chars = 4 padding)
	if spaceCount < 4 {
		t.Fatalf("expected at least 4 spaces after INFO, got %d spaces. output: %q", spaceCount, plain)
	}
}

func TestFieldWidthTime(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		TimeFormat:     "15:04:05.999",
		FieldWidths:    map[Field]int{FieldTime: 15},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
	}

	out := captureOutput(h, r)
	plain := stripANSI(out)

	// Check that the time string appears and has padding after it
	if !strings.Contains(plain, "12:00:00") {
		t.Fatalf("expected time in output, got: %q", plain)
	}
}

func TestFieldWidthsNilHasDefaultLevelPadding(t *testing.T) {
	// No FieldWidths set, should default to 5 spaces for level (backward compat).
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelInfo,
		Message: "hello",
	}

	out := captureOutput(h, r)
	plain := stripANSI(out)

	idx := strings.Index(plain, "INFO")
	if idx == -1 {
		t.Fatal("INFO not found")
	}
	afterINFO := plain[idx+4:]
	spaceCount := 0
	for i := 0; i < len(afterINFO) && afterINFO[i] == ' '; i++ {
		spaceCount++
	}
	if spaceCount < 1 {
		t.Fatalf("expected at least 1 padding space after INFO (default width=5), got %d. output: %q", spaceCount, plain)
	}
}

func TestBuilderDefaults(t *testing.T) {
	h := NewBuilder(io.Discard).Build()
	if h == nil {
		t.Fatal("Build returned nil")
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Info to be enabled with default builder")
	}
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Debug to be disabled with default builder")
	}
}

func TestBuilderChain(t *testing.T) {
	magenta := []byte("\033[35m")
	green := []byte("\033[32m")
	customFields := []Field{FieldLevel, FieldMessage}

	h := NewBuilder(io.Discard).
		WithLevel(slog.LevelDebug).
		WithAddSource(false).
		WithFields(customFields...).
		WithTimeFormat(time.Kitchen).
		WithSourceFormat(SourceShort).
		WithLevelColor(slog.LevelWarn, magenta).
		WithMessageColor(slog.LevelWarn, green).
		Build()

	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Debug to be enabled")
	}

	// Check custom colors made it into handler
	if string(h.cfg.Theme.LevelColors[slog.LevelWarn]) != string(magenta) {
		t.Error("WithLevelColor did not set warn color")
	}
	if string(h.cfg.Theme.MessageColors[slog.LevelWarn]) != string(green) {
		t.Error("WithMessageColor did not set warn message color")
	}

	// Output a Warn record with custom colors
	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   slog.LevelWarn,
		Message: "caution",
	}

	out := captureOutput(h, r)
	if !strings.Contains(out, string(magenta)) {
		t.Errorf("output should contain custom warn color, got: %q", out)
	}
}

func TestBuilderDoesNotMutateDefault(t *testing.T) {
	b := NewBuilder(io.Discard)
	b.WithLevelColor(slog.LevelDebug, []byte("\033[100m"))
	b.Build()

	// DefaultTheme should be untouched
	if d, ok := DefaultTheme.LevelColors[slog.LevelDebug]; ok && string(d) == "\033[100m" {
		t.Fatal("Builder mutated DefaultTheme.LevelColors")
	}
}

func TestWithAttrsCopiesCachedFields(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		TimeFormat:     "15:04",
		SourceFormat:   SourceShort,
		FieldWidths:     map[Field]int{FieldLevel: 8},
	}
	h := New(io.Discard, cfg)

	h2 := h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	h2t := h2.(*PrettyTextHandler)

	// Cached fields should be preserved
	if h2t.timeFormat != "15:04" {
		t.Errorf("timeFormat not copied: got %q", h2t.timeFormat)
	}
	if h2t.sourceFormat != SourceShort {
		t.Errorf("sourceFormat not copied: got %v", h2t.sourceFormat)
	}
	if h2t.fieldWidths[FieldLevel] != 8 {
		t.Errorf("fieldWidths[FieldLevel] not copied: got %d", h2t.fieldWidths[FieldLevel])
	}
}

func TestWithGroupCopiesCachedFields(t *testing.T) {
	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
		TimeFormat:     "15:04",
		SourceFormat:   SourceShort,
		FieldWidths:     map[Field]int{FieldLevel: 10},
	}
	h := New(io.Discard, cfg)

	h2 := h.WithGroup("g")
	h2t := h2.(*PrettyTextHandler)

	if h2t.timeFormat != "15:04" {
		t.Errorf("timeFormat not copied: got %q", h2t.timeFormat)
	}
	if h2t.sourceFormat != SourceShort {
		t.Errorf("sourceFormat not copied: got %v", h2t.sourceFormat)
	}
	if h2t.fieldWidths[FieldLevel] != 10 {
		t.Errorf("fieldWidths[FieldLevel] not copied: got %d", h2t.fieldWidths[FieldLevel])
	}
}

func TestCustomLevelColor(t *testing.T) {
	customLevel := slog.Level(12)
	customColor := []byte("\033[95m") // magenta

	cfg := &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelDebug},
		Theme: Theme{
			LevelColors: map[slog.Level][]byte{
				customLevel: customColor,
			},
		},
	}
	h := New(io.Discard, cfg)

	r := slog.Record{
		Time:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Level:   customLevel,
		Message: "custom trace",
	}

	out := captureOutput(h, r)
	if !strings.Contains(out, string(customColor)) {
		t.Errorf("expected custom level color in output, got: %q", out)
	}
}

// FuzzAppendPadding validates appendPadding for all non-negative width/len combos.
func FuzzAppendPadding(f *testing.F) {
	f.Add(0, 0)
	f.Add(5, 3)
	f.Add(10, 15)
	f.Add(64, 0)
	f.Add(128, 100)
	f.Fuzz(func(t *testing.T, width, currentLen int) {
		if width < 0 {
			width = -width
		}
		if currentLen < 0 {
			currentLen = -currentLen
		}
		buf := make([]byte, 0, width+64)
		result := appendPadding(buf, width, currentLen)
		// If currentLen >= width, no padding should be added
		if currentLen >= width && len(result) > 0 {
			t.Errorf("expected no padding when currentLen(%d) >= width(%d), got %d bytes", currentLen, width, len(result))
		}
		// Otherwise, result length should be (width - currentLen)
		if currentLen < width && len(result) != width-currentLen {
			t.Errorf("expected %d bytes padding, got %d", width-currentLen, len(result))
		}
	})
}

// FuzzBuilderBuild validates that Builder.Build never panics with arbitrary inputs.
func FuzzBuilderBuild(f *testing.F) {
	f.Add(int64(0), "15:04:05")
	f.Add(int64(8), time.Kitchen)
	f.Fuzz(func(t *testing.T, levelInt int64, timeFormat string) {
		// Clamp level to valid range
		if levelInt < -8 || levelInt > 24 {
			return
		}
		if len(timeFormat) > 64 {
			return
		}
		lvl := slog.Level(levelInt)
		h := NewBuilder(io.Discard).
			WithLevel(lvl).
			WithTimeFormat(timeFormat).
			Build()
		if h == nil {
			t.Fatal("Build returned nil")
		}
		// Should not panic on Handle with various levels
		r := slog.Record{
			Time:    time.Now(),
			Level:   lvl,
			Message: "test",
		}
		_ = h.Handle(context.Background(), r)
	})
}

func BenchmarkHandleWithCustomFormats(b *testing.B) {
	h := New(io.Discard, &Config{
		HandlerOptions: slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true},
		TimeFormat:     time.RFC3339Nano,
		SourceFormat:   SourceLong,
		FieldWidths:    map[Field]int{FieldLevel: 8, FieldTime: 30, FieldSource: 40},
	})
	r := createTestRecord()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = h.Handle(context.Background(), r)
	}
}

func BenchmarkBuilderBuild(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		h := NewBuilder(io.Discard).
			WithLevel(slog.LevelDebug).
			WithTimeFormat(time.RFC3339).
			WithLevelColor(slog.LevelWarn, ColorError).
			Build()
		_ = h
	}
}

// Coverage gap fillers for Builder methods.

func TestBuilderWithReplaceAttr(t *testing.T) {
	called := false
	h := NewBuilder(io.Discard).
		WithReplaceAttr(func(groups []string, a slog.Attr) slog.Attr {
			called = true
			if a.Key == "secret" {
				return slog.String(a.Key, "***")
			}
			return a
		}).
		Build()

	r := slog.Record{Time: time.Now(), Level: slog.LevelInfo, Message: "msg"}
	r.Add(slog.String("secret", "value"))
	_ = h.Handle(context.Background(), r)
	if !called {
		t.Error("ReplaceAttr was not called")
	}
}

func TestBuilderWithFieldWidth(t *testing.T) {
	h := NewBuilder(io.Discard).
		WithFieldWidth(FieldLevel, 10).
		Build()

	if h.fieldWidths[FieldLevel] != 10 {
		t.Fatalf("expected fieldWidth[FieldLevel]=10, got %d", h.fieldWidths[FieldLevel])
	}
}

func TestBuilderWithTheme(t *testing.T) {
	custom := Theme{
		TimeColor:   []byte("\033[95m"),
		Reset:       []byte("\033[0m"),
		LevelColors: map[slog.Level][]byte{slog.LevelInfo: []byte("\033[92m")},
	}
	h := NewBuilder(io.Discard).
		WithTheme(custom).
		Build()

	if string(h.cfg.Theme.TimeColor) != string(custom.TimeColor) {
		t.Error("WithTheme did not set TimeColor")
	}
}

func TestBuilderWithLevelColorNilMap(t *testing.T) {
	b := &Builder{
		out: io.Discard,
		cfg: Config{
			Theme: Theme{LevelColors: nil},
		},
	}
	b.WithLevelColor(slog.LevelInfo, ColorError)
	if b.cfg.Theme.LevelColors == nil {
		t.Fatal("WithLevelColor did not initialize nil LevelColors map")
	}
}

func TestAppendPaddingLargeWidth(t *testing.T) {
	// Width > len(spaces) (64) to hit the chunking branch.
	buf := make([]byte, 0, 256)
	result := appendPadding(buf, 100, 10)
	if len(result) != 90 {
		t.Fatalf("expected 90 padding bytes, got %d", len(result))
	}
	for i := 0; i < len(result); i++ {
		if result[i] != ' ' {
			t.Fatalf("expected space at index %d, got %q", i, result[i])
		}
	}
}
