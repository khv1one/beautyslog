// Package beautyslog provides a fast and colorful slog.Handler
// implementation optimized for human‑friendly terminal output.
//
// It formats log entries with color, aligned levels, grouped
// attributes, efficient buffer pooling, and zero-reflection hot paths.
//
// Example usage:
//
//	logger := slog.New(beautyslog.Must(os.Stdout, &slog.HandlerOptions{}))
//	logger.Info("hello", "user", "alice")
package beautyslog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
	opts     slog.HandlerOptions
	out      io.Writer
	mu       sync.Mutex
	group    string
	groups   []string
	preAttrs []slog.Attr
	bufPool  *sync.Pool

	// resolved at construction; immutable thereafter
	useTemplate bool
	template    ParsedTemplate
	layout      Layout
	colors      resolvedColorScheme
	noColor     bool
}

// Must creates a new PrettyTextHandler writing output to 'out'.
// It panics if configuration is invalid. Use New for error handling.
func Must(out io.Writer, opts *slog.HandlerOptions) *PrettyTextHandler {
	h, err := NewWithConfig(out, opts, nil)
	if err != nil {
		panic(err)
	}
	return h
}

// New creates a new PrettyTextHandler writing output to 'out'.
// Returns an error if the handler cannot be constructed (e.g. invalid template).
func New(out io.Writer, opts *slog.HandlerOptions) (*PrettyTextHandler, error) {
	return NewWithConfig(out, opts, nil)
}

// Enabled reports whether a log entry of the given level should be emitted.
func (h *PrettyTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

// Handle formats and writes a slog.Record to the output.
// It reuses an internal buffer pool for efficiency.
func (h *PrettyTextHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	bufPtr := h.bufPool.Get().(*[]byte)
	defer func() {
		if cap(*bufPtr) > maxBufferSize {
			return
		}
		h.bufPool.Put(bufPtr)
	}()
	buf := (*bufPtr)[:0]

	if h.useTemplate {
		for _, seg := range h.template {
			if seg.IsField {
				buf = h.appendField(buf, seg.Field, r)
			} else {
				buf = append(buf, seg.Literal...)
			}
		}
	} else {
		for i, f := range h.layout {
			buf = h.appendField(buf, f, r)
			if i < len(h.layout)-1 {
				buf = append(buf, ' ')
			}
		}
	}

	buf = append(buf, '\n')
	_, err := h.out.Write(buf)
	return err
}

func (h *PrettyTextHandler) appendField(buf []byte, f Field, r slog.Record) []byte {
	switch f {
	case FieldTime:
		return h.appendTime(buf, r.Time)
	case FieldSource:
		return h.appendSource(buf, r.PC)
	case FieldLevel:
		buf = h.appendLevel(buf, r.Level)
		if !h.useTemplate {
			levelStr := levelNames[r.Level]
			if levelStr == "" {
				levelStr = r.Level.String()
			}
			padding := 5 - len(levelStr)
			for range padding {
				buf = append(buf, ' ')
			}
		}
		return buf
	case FieldMessage:
		return h.appendMessage(buf, r.Message, r.Level)
	case FieldAttrs:
		return h.appendAttrs(buf, r)
	}
	return buf
}

func (h *PrettyTextHandler) appendTime(buf []byte, t time.Time) []byte {
	buf = append(buf, h.colors.time...)
	buf = t.AppendFormat(buf, "15:04:05.999")
	buf = append(buf, h.colors.reset...)
	return buf
}

func (h *PrettyTextHandler) appendSource(buf []byte, pc uintptr) []byte {
	if !h.opts.AddSource || pc == 0 {
		return buf
	}
	fs := runtime.CallersFrames([]uintptr{pc})
	f, _ := fs.Next()
	if f.File == "" {
		return buf
	}
	file := f.File
	for i := len(file) - 1; i >= 0; i-- {
		if file[i] == '/' || file[i] == '\\' {
			file = file[i+1:]
			break
		}
	}
	buf = append(buf, h.colors.source...)
	buf = append(buf, file...)
	buf = append(buf, ':')
	buf = strconv.AppendInt(buf, int64(f.Line), 10)
	buf = append(buf, h.colors.reset...)
	return buf
}

func (h *PrettyTextHandler) appendLevel(buf []byte, lvl slog.Level) []byte {
	lc := h.colors.levelColor(lvl)
	buf = append(buf, lc...)
	levelStr := levelNames[lvl]
	if levelStr == "" {
		levelStr = lvl.String()
	}
	buf = append(buf, levelStr...)
	buf = append(buf, h.colors.reset...)
	return buf
}

func (h *PrettyTextHandler) appendMessage(buf []byte, msg string, lvl slog.Level) []byte {
	if h.colors.message != "" {
		buf = append(buf, h.colors.message...)
	} else {
		buf = append(buf, h.colors.levelColor(lvl)...)
	}
	buf = append(buf, msg...)
	buf = append(buf, h.colors.reset...)
	return buf
}

func (h *PrettyTextHandler) appendAttrs(buf []byte, r slog.Record) []byte {
	first := true
	for _, a := range h.preAttrs {
		if first {
			first = false
		} else {
			buf = append(buf, ' ')
		}
		buf = h.appendSingleAttr(buf, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		if first {
			first = false
		} else {
			buf = append(buf, ' ')
		}
		buf = h.appendSingleAttr(buf, a)
		return true
	})

	return buf
}

func (h *PrettyTextHandler) appendSingleAttr(buf []byte, a slog.Attr) []byte {
	if h.opts.ReplaceAttr != nil {
		a = h.opts.ReplaceAttr(h.groups, a)
		if a.Equal(slog.Attr{}) {
			return buf
		}
	}

	buf = append(buf, h.colors.key...)
	if h.group != "" {
		buf = append(buf, h.group...)
		buf = append(buf, '.')
		buf = append(buf, a.Key...)
	} else {
		buf = append(buf, a.Key...)
	}
	buf = append(buf, h.colors.reset...)
	buf = append(buf, '=')
	buf = append(buf, h.colors.value...)
	buf = h.appendValue(buf, a.Value)
	buf = append(buf, h.colors.reset...)
	return buf
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
		buf = append(buf, h.colors.reset...)
		buf = append(buf, h.colors.group...)
		buf = append(buf, '(')
		first := true
		for _, attr := range attrs {
			if h.opts.ReplaceAttr != nil {
				attr = h.opts.ReplaceAttr(h.groups, attr)
				if attr.Equal(slog.Attr{}) {
					continue
				}
			}
			if !first {
				buf = append(buf, ' ')
			}
			first = false
			buf = append(buf, h.colors.reset...)
			buf = append(buf, h.colors.key...)
			buf = append(buf, attr.Key...)
			buf = append(buf, h.colors.reset...)
			buf = append(buf, '=')
			buf = append(buf, h.colors.value...)
			buf = h.appendValue(buf, attr.Value)
			buf = append(buf, h.colors.reset...)
		}
		buf = append(buf, h.colors.group...)
		buf = append(buf, ')')
		buf = append(buf, h.colors.reset...)
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
	bs, ok := a.([]byte)
	return bs, ok
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

	if u < uint64(time.Second) {
		if neg {
			buf = append(buf, '-')
		}
		buf = strconv.AppendFloat(buf, float64(u)/1000000, 'f', -1, 64)
		buf = append(buf, 'm', 's')
		return buf
	}

	if neg {
		buf = append(buf, '-')
	}

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
		opts:        h.opts,
		out:         h.out,
		mu:          sync.Mutex{},
		group:       h.group,
		groups:      h.groups,
		preAttrs:    newPreAttrs,
		bufPool:     h.bufPool,
		useTemplate: h.useTemplate,
		template:    h.template,
		layout:      h.layout,
		colors:      h.colors,
		noColor:     h.noColor,
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
		opts:        h.opts,
		out:         h.out,
		mu:          sync.Mutex{},
		group:       newGroup,
		groups:      strings.Split(newGroup, "."),
		preAttrs:    h.preAttrs,
		bufPool:     h.bufPool,
		useTemplate: h.useTemplate,
		template:    h.template,
		layout:      h.layout,
		colors:      h.colors,
		noColor:     h.noColor,
	}
}
