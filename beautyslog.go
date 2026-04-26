// Package beautyslog provides a fast and colorful slog.Handler
// implementation optimized for human‑friendly terminal output.
//
// It formats log entries with color, aligned levels, grouped
// attributes, efficient buffer pooling, and zero-reflection hot paths.
//
// For quick setup, use New for backward-compatible defaults:
//
//	logger := slog.New(beautyslog.New(os.Stdout, &slog.HandlerOptions{}))
//	logger.Info("hello", "user", "alice")
//
// For full customization (field order, colors, level names, time format),
// build a Config and pass it to NewWithConfig:
//
//	cfg := beautyslog.DefaultConfig()
//	cfg.Theme.Key = beautyslog.ColorCyan
//	cfg.LevelConfigs[slog.LevelInfo] = beautyslog.LevelConfig{Name: "OK", Color: beautyslog.ColorGreen}
//	logger := slog.New(beautyslog.NewWithConfig(os.Stdout, cfg))
//
// Colors are automatically disabled when the NO_COLOR environment variable
// is set or Config.DisableColors is true.
package beautyslog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Field represents a specific component of the log line.
type Field int

const (
	FieldTime Field = iota
	FieldSource
	FieldLevel
	FieldMessage
	FieldAttributes
)

// Color represents an ANSI escape sequence string.
type Color string

const (
	ColorReset  Color = "\033[0m"
	ColorRed    Color = "\033[31m"
	ColorGreen  Color = "\033[32m"
	ColorYellow Color = "\033[33m"
	ColorBlue   Color = "\033[34m"
	ColorPurple Color = "\033[35m"
	ColorCyan   Color = "\033[36m"
	ColorWhite  Color = "\033[37m"
	ColorGray   Color = "\033[90m"
	ColorOrange Color = "\033[38;5;216m"
)

// Theme defines colors for fixed log elements.
type Theme struct {
	Time    Color
	Source  Color
	Message Color // If empty, the message inherits the color of the log level.
	Key     Color
	Value   Color
	Group   Color // Brackets or prefixes for groups
}

// LevelConfig allows customization of a specific log level.
type LevelConfig struct {
	Name  string
	Color Color
}

// Config defines the complete customization surface for the handler.
type Config struct {
	slog.HandlerOptions

	Fields        []Field
	Theme         Theme
	LevelConfigs  map[slog.Level]LevelConfig
	TimeFormat    string
	DisableColors bool
}

// cachedLevel stores pre-computed level name and color bytes.
type cachedLevel struct {
	name  []byte
	color []byte
}

const (
	initialBufferSize = 512
	maxBufferSize     = 4096
)

// PrettyTextHandler is a human-friendly slog handler that prints
// colorized, aligned, low-allocation log lines.
//
// PrettyTextHandler supports slog groups, ReplaceAttr, AddSource, and
// attribute propagation. It is safe for concurrent use.
type PrettyTextHandler struct {
	opts     slog.HandlerOptions
	out      io.Writer
	mu       *sync.Mutex
	group    string
	preAttrs []slog.Attr
	bufPool  *sync.Pool
	fields      []Field
	timeFormat  string
	colorTime   []byte
	colorSource []byte
	colorMsg    []byte
	colorKey    []byte
	colorValue  []byte
	colorGroup  []byte
	colorReset  []byte
	levels         map[slog.Level]cachedLevel
	maxLevelNameLen int
}

// DefaultConfig returns the legacy hardcoded behavior as a Config object.
func DefaultConfig() *Config {
	return &Config{
		Fields: []Field{
			FieldTime,
			FieldSource,
			FieldLevel,
			FieldMessage,
			FieldAttributes,
		},
		Theme: Theme{
			Time:    ColorGray,
			Source:  ColorGray,
			Key:     ColorGreen,
			Value:   ColorOrange,
			Group:   ColorPurple,
			// Message is intentionally empty so it inherits level color
		},
		LevelConfigs: map[slog.Level]LevelConfig{
			slog.LevelDebug: {Name: "DEBUG", Color: ColorCyan},
			slog.LevelInfo:  {Name: "INFO", Color: ColorBlue},
			slog.LevelWarn:  {Name: "WARN", Color: ColorYellow},
			slog.LevelError: {Name: "ERROR", Color: ColorRed},
		},
		TimeFormat: "15:04:05.999",
	}
}

// New creates a new PrettyTextHandler writing output to 'out'.
//
// The handler respects slog.HandlerOptions:
// - Level: minimum log level
// - AddSource: include file:line
// - ReplaceAttr: transforms attributes
func New(out io.Writer, opts *slog.HandlerOptions) *PrettyTextHandler {
	cfg := DefaultConfig()
	if opts != nil {
		cfg.HandlerOptions = *opts
	}
	return NewWithConfig(out, cfg)
}

// NewWithConfig creates a customized handler using the provided Config.
func NewWithConfig(out io.Writer, cfg *Config) *PrettyTextHandler {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if cfg.Level == nil {
		cfg.Level = slog.LevelInfo
	}

	h := &PrettyTextHandler{
		out: out,
		mu:  &sync.Mutex{},
		opts: cfg.HandlerOptions,
		bufPool: &sync.Pool{
			New: func() interface{} {
				b := make([]byte, 0, initialBufferSize)
				return &b
			},
		},
	}

	h.buildCache(cfg)
	return h
}

// buildCache pre-computes all color bytes and level names from config.
// Called once at construction time to keep the Handle() hot path allocation-free.
func (h *PrettyTextHandler) buildCache(cfg *Config) {
	h.fields = cfg.Fields
	h.timeFormat = cfg.TimeFormat

	colorsDisabled := cfg.DisableColors || os.Getenv("NO_COLOR") != ""

	resolveColor := func(c Color) []byte {
		if colorsDisabled || c == "" {
			return nil
		}
		return []byte(c)
	}

	h.colorTime = resolveColor(cfg.Theme.Time)
	h.colorSource = resolveColor(cfg.Theme.Source)
	h.colorMsg = resolveColor(cfg.Theme.Message)
	h.colorKey = resolveColor(cfg.Theme.Key)
	h.colorValue = resolveColor(cfg.Theme.Value)
	h.colorGroup = resolveColor(cfg.Theme.Group)
	h.colorReset = resolveColor(ColorReset)

	maxLen := 0
	for _, lc := range cfg.LevelConfigs {
		if len(lc.Name) > maxLen {
			maxLen = len(lc.Name)
		}
	}

	h.levels = make(map[slog.Level]cachedLevel, len(cfg.LevelConfigs))
	for lvl, lc := range cfg.LevelConfigs {
		padded := lc.Name
		for len(padded) < maxLen {
			padded += " "
		}
		h.levels[lvl] = cachedLevel{
			name:  []byte(padded),
			color: resolveColor(lc.Color),
		}
	}

	h.maxLevelNameLen = maxLen
}

// Enabled reports whether a log entry of the given level should be emitted.
func (h *PrettyTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func appendColor(buf []byte, color []byte) []byte {
	if color != nil {
		buf = append(buf, color...)
	}
	return buf
}

// resolveFallbackColor converts a Color to []byte respecting the disable-colors setting.
func (h *PrettyTextHandler) resolveFallbackColor(c Color) []byte {
	if h.colorReset == nil {
		// Colors are globally disabled
		return nil
	}
	return []byte(c)
}

// Handle formats and writes a slog.Record to the output.
// It reuses an internal buffer pool for efficiency.
func (h *PrettyTextHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	bufPtr := h.bufPool.Get().(*[]byte)
	defer func() {
		if cap(*bufPtr) <= maxBufferSize {
			h.bufPool.Put(bufPtr)
		}
	}()
	buf := (*bufPtr)[:0]

	// Resolve level color and name
	cl, levelKnown := h.levels[r.Level]
	var levelColor []byte
	var levelName []byte
	if levelKnown {
		levelColor = cl.color
		levelName = cl.name
	} else {
		levelColor = h.resolveFallbackColor(ColorWhite)
		levelNameStr := r.Level.String()
		// Pad unknown level name to max cached name length
		maxLen := h.maxLevelNameLen
		for len(levelNameStr) < maxLen {
			levelNameStr += " "
		}
		levelName = []byte(levelNameStr)
	}

	// Message color: use h.colorMsg if set, else inherit level color
	msgColor := h.colorMsg
	if msgColor == nil {
		msgColor = levelColor
	}

	// Emit fields in configured order
	for _, field := range h.fields {
		switch field {
		case FieldTime:
			buf = appendColor(buf, h.colorTime)
			buf = r.Time.AppendFormat(buf, h.timeFormat)
			buf = appendColor(buf, h.colorReset)
			buf = append(buf, ' ')
		case FieldSource:
			if h.opts.AddSource && r.PC != 0 {
				fs := runtime.CallersFrames([]uintptr{r.PC})
				f, _ := fs.Next()
				if f.File != "" {
					file := f.File
					for i := len(file) - 1; i >= 0; i-- {
						if file[i] == '/' || file[i] == '\\' {
							file = file[i+1:]
							break
						}
					}
					buf = appendColor(buf, h.colorSource)
					buf = append(buf, file...)
					buf = append(buf, ':')
					buf = strconv.AppendInt(buf, int64(f.Line), 10)
					buf = appendColor(buf, h.colorReset)
					buf = append(buf, ' ')
				}
			}
		case FieldLevel:
			buf = appendColor(buf, levelColor)
			buf = append(buf, levelName...)
			buf = appendColor(buf, h.colorReset)
			buf = append(buf, ' ')
		case FieldMessage:
			buf = appendColor(buf, msgColor)
			buf = append(buf, r.Message...)
			buf = appendColor(buf, h.colorReset)
		case FieldAttributes:
			var groups []string
			if h.group != "" {
				groups = strings.Split(h.group, ".")
			}

			appendAttr := func(a slog.Attr) {
				if h.opts.ReplaceAttr != nil {
					a = h.opts.ReplaceAttr(groups, a)
					if a.Equal(slog.Attr{}) {
						return
					}
				}
				buf = append(buf, ' ')
				buf = appendColor(buf, h.colorKey)
				if h.group != "" {
					buf = append(buf, h.group...)
					buf = append(buf, '.')
					buf = append(buf, a.Key...)
				} else {
					buf = append(buf, a.Key...)
				}
				buf = appendColor(buf, h.colorReset)
				buf = append(buf, '=')
				buf = appendColor(buf, h.colorValue)
				buf = h.appendValue(buf, a.Value)
				buf = appendColor(buf, h.colorReset)
			}

			for _, a := range h.preAttrs {
				appendAttr(a)
			}
			r.Attrs(func(a slog.Attr) bool {
				appendAttr(a)
				return true
			})
		}
	}

	buf = append(buf, '\n')
	_, err := h.out.Write(buf)
	return err
}

func (h *PrettyTextHandler) appendValue(buf []byte, v slog.Value) []byte {
	switch v.Kind() {
	case slog.KindString:
		return append(buf, v.String()...)
	case slog.KindBool:
		return strconv.AppendBool(buf, v.Bool())
	case slog.KindInt64:
		return strconv.AppendInt(buf, v.Int64(), 10)
	case slog.KindUint64:
		return strconv.AppendUint(buf, v.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.AppendFloat(buf, v.Float64(), 'f', -1, 64)
	case slog.KindDuration:
		return appendDuration(buf, v.Duration())
	case slog.KindTime:
		return v.Time().AppendFormat(buf, time.RFC3339Nano)
	case slog.KindGroup:
		attrs := v.Group()
		buf = appendColor(buf, h.colorReset)
		buf = appendColor(buf, h.colorGroup)
		buf = append(buf, '(')
		for i, attr := range attrs {
			if i > 0 {
				buf = append(buf, ' ')
			}
			buf = appendColor(buf, h.colorReset)
			buf = appendColor(buf, h.colorKey)
			buf = append(buf, attr.Key...)
			buf = appendColor(buf, h.colorReset)
			buf = append(buf, '=')
			buf = appendColor(buf, h.colorValue)
			buf = h.appendValue(buf, attr.Value)
			buf = appendColor(buf, h.colorReset)
		}
		buf = appendColor(buf, h.colorGroup)
		buf = append(buf, ')')
		buf = appendColor(buf, h.colorReset)
		return buf
	case slog.KindAny:
		if bs, ok := byteSlice(v.Any()); ok {
			return append(buf, bs...)
		}
		return fmt.Append(buf, v.Any())
	default:
		return append(buf, v.String()...)
	}
}

func byteSlice(a any) ([]byte, bool) {
	if bs, ok := a.([]byte); ok {
		return bs, true
	}

	t := reflect.TypeOf(a)
	if t != nil && t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return reflect.ValueOf(a).Bytes(), true
	}
	return nil, false
}

func appendDuration(buf []byte, d time.Duration) []byte {
	if d == 0 {
		return append(buf, "0s"...)
	}
	if d < 0 {
		buf = append(buf, '-')
		d = -d
	}

	u := uint64(d)

	if u < uint64(time.Second) {
		buf = strconv.AppendFloat(buf, float64(u)/1000000, 'f', -1, 64)
		buf = append(buf, 'm', 's')
	} else {
		secs := u / uint64(time.Second)
		nsecs := u % uint64(time.Second)

		buf = strconv.AppendUint(buf, secs, 10)

		if nsecs > 0 {
			buf = append(buf, '.')
			var nsBuf [9]byte
			ns := strconv.AppendUint(nsBuf[:0], nsecs, 10)
			for i := 0; i < 9-len(ns); i++ {
				buf = append(buf, '0')
			}
			buf = append(buf, ns...)
		}

		buf = append(buf, 's')
	}

	return buf
}

// WithAttrs returns a new handler with additional pre‑attached attributes.
// The attributes will be written for every log entry.
func (h *PrettyTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	newPreAttrs := make([]slog.Attr, len(h.preAttrs), len(h.preAttrs)+len(attrs))
	copy(newPreAttrs, h.preAttrs)
	newPreAttrs = append(newPreAttrs, attrs...)

	return &PrettyTextHandler{
		opts:            h.opts,
		out:             h.out,
		mu:              h.mu,
		group:           h.group,
		preAttrs:        newPreAttrs,
		bufPool:         h.bufPool,
		fields:          h.fields,
		timeFormat:      h.timeFormat,
		colorTime:       h.colorTime,
		colorSource:     h.colorSource,
		colorMsg:        h.colorMsg,
		colorKey:        h.colorKey,
		colorValue:      h.colorValue,
		colorGroup:      h.colorGroup,
		colorReset:      h.colorReset,
		levels:          h.levels,
		maxLevelNameLen: h.maxLevelNameLen,
	}
}

// WithGroup returns a new handler with the given attribute group.
// Nested groups are supported using dot notation.
func (h *PrettyTextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newGroup := h.group
	if newGroup != "" {
		newGroup += "." + name
	} else {
		newGroup = name
	}
	return &PrettyTextHandler{
		opts:            h.opts,
		out:             h.out,
		mu:              h.mu,
		group:           newGroup,
		preAttrs:        h.preAttrs,
		bufPool:         h.bufPool,
		fields:          h.fields,
		timeFormat:      h.timeFormat,
		colorTime:       h.colorTime,
		colorSource:     h.colorSource,
		colorMsg:        h.colorMsg,
		colorKey:        h.colorKey,
		colorValue:      h.colorValue,
		colorGroup:      h.colorGroup,
		colorReset:      h.colorReset,
		levels:          h.levels,
		maxLevelNameLen: h.maxLevelNameLen,
	}
}
