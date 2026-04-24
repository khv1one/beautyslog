// Package beautyhandler provides a fast and colorful slog.Handler
// implementation optimized for human‑friendly terminal output.
//
// It formats log entries with color, aligned levels, grouped
// attributes, efficient buffer pooling, and zero-reflection hot paths.
//
// Example usage:
// logger := slog.New(beautyslog.New(os.Stdout, &beautyslog.HandlerOptions{}))
// logger.Info("hello", "user", "alice")
package beautyslog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	initialBufferSize = 512
	maxBufferSize     = 4096
)

// Field identifies a component of a log entry.
type Field int

const (
	TimeField Field = iota
	SourceField
	LevelField
	MessageField
	AttrsField
)

// defaultFields is the standard order of log fields.
var defaultFields = []Field{TimeField, SourceField, LevelField, MessageField, AttrsField}

// ColorScheme holds per-component ANSI color overrides.
// Nil fields fall back to the package-level default colors.
type ColorScheme struct {
	Time   []byte
	Source []byte
	Debug  []byte
	Info   []byte
	Warn   []byte
	Error  []byte
	Key    []byte
	Value  []byte
	Group  []byte
	Reset  []byte
}

// HandlerOptions configures PrettyTextHandler.
type HandlerOptions struct {
	AddSource   bool
	Level       slog.Leveler
	ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
	Fields      []Field
	Colors      *ColorScheme
}

var (
	colorReset  = []byte("\033[0m")
	colorDebug  = []byte("\033[36m")
	colorInfo   = []byte("\033[34m")
	colorWarn   = []byte("\033[33m")
	colorError  = []byte("\033[31m")
	colorKey    = []byte("\033[32m")
	colorValue  = []byte("\033[38;5;216m")
	colorTime   = []byte("\033[90m")
	colorWhite  = []byte("\033[37m")
	colorPurple = []byte("\033[35m")
)

var levelColors = map[slog.Level][]byte{
	slog.LevelDebug: colorDebug,
	slog.LevelInfo:  colorInfo,
	slog.LevelWarn:  colorWarn,
	slog.LevelError: colorError,
}

var levelNames = map[slog.Level]string{
	slog.LevelDebug: "DEBUG",
	slog.LevelInfo:  "INFO",
	slog.LevelWarn:  "WARN",
	slog.LevelError: "ERROR",
}

// PrettyTextHandler is a human-friendly slog handler that prints
// colorized, aligned, low-allocation log lines.
//
// PrettyTextHandler supports slog groups, ReplaceAttr, AddSource, and
// attribute propagation. It is safe for concurrent use.
type PrettyTextHandler struct {
	opts     HandlerOptions
	out      io.Writer
	mu       sync.Mutex
	group    string
	preAttrs []slog.Attr
	bufPool  *sync.Pool
}

// New creates a new PrettyTextHandler writing output to 'out'.
//
// The handler respects HandlerOptions:
//   - Level: minimum log level
//   - AddSource: include file:line
//   - ReplaceAttr: transforms attributes
//   - Fields: order of log components; defaults to [Time, Source, Level, Message, Attrs]
//   - Colors: per-component ANSI colors; uses built-in defaults when nil
func New(out io.Writer, opts *HandlerOptions) *PrettyTextHandler {
	h := &PrettyTextHandler{out: out}
	if opts != nil {
		h.opts = *opts
	}
	if h.opts.Level == nil {
		h.opts.Level = slog.LevelInfo
	}
	if len(h.opts.Fields) == 0 {
		h.opts.Fields = defaultFields
	}
	if h.opts.Colors == nil {
		h.opts.Colors = &ColorScheme{}
	}

	h.bufPool = &sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, initialBufferSize)
			return &b
		},
	}

	return h
}

// Enabled reports whether a log entry of the given level should be emitted.
func (h *PrettyTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *PrettyTextHandler) resetColor() []byte {
	if h.opts.Colors.Reset != nil {
		return h.opts.Colors.Reset
	}
	return colorReset
}

func (h *PrettyTextHandler) timeColor() []byte {
	if h.opts.Colors.Time != nil {
		return h.opts.Colors.Time
	}
	return colorTime
}

func (h *PrettyTextHandler) sourceColor() []byte {
	if h.opts.Colors.Source != nil {
		return h.opts.Colors.Source
	}
	return colorTime
}

func (h *PrettyTextHandler) keyColor() []byte {
	if h.opts.Colors.Key != nil {
		return h.opts.Colors.Key
	}
	return colorKey
}

func (h *PrettyTextHandler) valueColor() []byte {
	if h.opts.Colors.Value != nil {
		return h.opts.Colors.Value
	}
	return colorValue
}

func (h *PrettyTextHandler) groupColor() []byte {
	if h.opts.Colors.Group != nil {
		return h.opts.Colors.Group
	}
	return colorPurple
}

func (h *PrettyTextHandler) levelColor(lvl slog.Level) []byte {
	switch lvl {
	case slog.LevelDebug:
		if h.opts.Colors.Debug != nil {
			return h.opts.Colors.Debug
		}
	case slog.LevelInfo:
		if h.opts.Colors.Info != nil {
			return h.opts.Colors.Info
		}
	case slog.LevelWarn:
		if h.opts.Colors.Warn != nil {
			return h.opts.Colors.Warn
		}
	case slog.LevelError:
		if h.opts.Colors.Error != nil {
			return h.opts.Colors.Error
		}
	}
	if c, ok := levelColors[lvl]; ok {
		return c
	}
	return colorWhite
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

	var groups []string
	if h.group != "" {
		groups = strings.Split(h.group, ".")
	}

	needSpace := false

	for _, field := range h.opts.Fields {
		switch field {
		case TimeField:
			if needSpace {
				buf = append(buf, ' ')
			}
			buf = append(buf, h.timeColor()...)
			buf = r.Time.AppendFormat(buf, "15:04:05.999")
			buf = append(buf, h.resetColor()...)
			needSpace = true

		case SourceField:
			if !h.opts.AddSource || r.PC == 0 {
				continue
			}
			fs := runtime.CallersFrames([]uintptr{r.PC})
			f, _ := fs.Next()
			if f.File == "" {
				continue
			}
			if needSpace {
				buf = append(buf, ' ')
			}
			file := f.File
			for i := len(file) - 1; i >= 0; i-- {
				if file[i] == '/' || file[i] == '\\' {
					file = file[i+1:]
					break
				}
			}
			buf = append(buf, h.sourceColor()...)
			buf = append(buf, file...)
			buf = append(buf, ':')
			buf = strconv.AppendInt(buf, int64(f.Line), 10)
			buf = append(buf, h.resetColor()...)
			needSpace = true

		case LevelField:
			if needSpace {
				buf = append(buf, ' ')
			}
			levelColor := h.levelColor(r.Level)
			buf = append(buf, levelColor...)
			levelStr, ok := levelNames[r.Level]
			if !ok {
				levelStr = r.Level.String()
			}
			buf = append(buf, levelStr...)
			buf = append(buf, h.resetColor()...)
			padding := 5 - len(levelStr)
			for i := 0; i < padding; i++ {
				buf = append(buf, ' ')
			}
			needSpace = true

		case MessageField:
			if needSpace {
				buf = append(buf, ' ')
			}
			buf = append(buf, h.levelColor(r.Level)...)
			buf = append(buf, r.Message...)
			buf = append(buf, h.resetColor()...)
			needSpace = true

		case AttrsField:
			for _, a := range h.preAttrs {
				buf, needSpace = h.appendAttr(buf, groups, a, needSpace)
			}
			r.Attrs(func(a slog.Attr) bool {
				buf, needSpace = h.appendAttr(buf, groups, a, needSpace)
				return true
			})
		}
	}

	buf = append(buf, '\n')
	_, err := h.out.Write(buf)
	return err
}

func (h *PrettyTextHandler) appendAttr(buf []byte, groups []string, a slog.Attr, needSpace bool) ([]byte, bool) {
	if h.opts.ReplaceAttr != nil {
		a = h.opts.ReplaceAttr(groups, a)
		if a.Equal(slog.Attr{}) {
			return buf, needSpace
		}
	}

	if needSpace {
		buf = append(buf, ' ')
	}
	buf = append(buf, h.keyColor()...)
	if h.group != "" {
		buf = append(buf, h.group...)
		buf = append(buf, '.')
		buf = append(buf, a.Key...)
	} else {
		buf = append(buf, a.Key...)
	}
	buf = append(buf, h.resetColor()...)
	buf = append(buf, '=')
	buf = append(buf, h.valueColor()...)
	buf = h.appendValue(buf, a.Value)
	buf = append(buf, h.resetColor()...)
	return buf, true
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
		buf = append(buf, h.resetColor()...)
		buf = append(buf, h.groupColor()...)
		buf = append(buf, '(')
		for i, attr := range attrs {
			if i > 0 {
				buf = append(buf, ' ')
			} else {
				buf = append(buf, h.resetColor()...)
			}
			buf = append(buf, h.keyColor()...)
			buf = append(buf, attr.Key...)
			buf = append(buf, h.resetColor()...)
			buf = append(buf, '=')
			buf = append(buf, h.valueColor()...)
			buf = h.appendValue(buf, attr.Value)
			buf = append(buf, h.resetColor()...)
		}
		buf = append(buf, h.groupColor()...)
		buf = append(buf, ')')
		buf = append(buf, h.resetColor()...)
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

	neg := d < 0
	if neg {
		d = -d
	}

	u := uint64(d)
	start := len(buf)

	if u < uint64(time.Second) {
		buf = strconv.AppendFloat(buf, float64(u)/1000000, 'f', -1, 64)
		buf = append(buf, 'm')
		buf = append(buf, 's')
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

	if neg {
		buf = append(buf[:start+1], buf[start:]...)
		buf[start] = '-'
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
		opts:     h.opts,
		out:      h.out,
		mu:       sync.Mutex{},
		group:    h.group,
		preAttrs: newPreAttrs,
		bufPool:  h.bufPool,
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
		opts:     h.opts,
		out:      h.out,
		mu:       sync.Mutex{},
		group:    newGroup,
		preAttrs: h.preAttrs,
		bufPool:  h.bufPool,
	}
}
