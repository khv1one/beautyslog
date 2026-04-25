// Package beautyhandler provides a fast and colorful slog.Handler
// implementation optimized for human‑friendly terminal output.
//
// It formats log entries with color, aligned levels, grouped
// attributes, and efficient buffer pooling.
//
// Example usage:
// logger := slog.New(beautyhandler.New(os.Stdout, &slog.HandlerOptions{}))
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

var (
	ColorReset  = []byte("\033[0m")
	ColorDebug  = []byte("\033[36m")
	ColorInfo   = []byte("\033[34m")
	ColorWarn   = []byte("\033[33m")
	ColorError  = []byte("\033[31m")
	ColorKey    = []byte("\033[32m")
	ColorValue  = []byte("\033[38;5;216m")
	ColorTime   = []byte("\033[90m")
	ColorWhite  = []byte("\033[37m")
	ColorPurple = []byte("\033[35m")
)

var levelColors = map[slog.Level][]byte{
	slog.LevelDebug: ColorDebug,
	slog.LevelInfo:  ColorInfo,
	slog.LevelWarn:  ColorWarn,
	slog.LevelError: ColorError,
}

var levelNames = map[slog.Level]string{
	slog.LevelDebug: "DEBUG",
	slog.LevelInfo:  "INFO",
	slog.LevelWarn:  "WARN",
	slog.LevelError: "ERROR",
}

// Theme defines ANSI color sequences for each visual element of a log line.
type Theme struct {
	TimeColor           []byte
	SourceColor         []byte
	LevelColors         map[slog.Level][]byte
	MessageColors       map[slog.Level][]byte
	AttributeKeyColor   []byte
	AttributeValueColor []byte
	GroupColor          []byte
	Reset               []byte
}

// Field identifies a logical section of a log line.
type Field int

const (
	FieldTime Field = iota
	FieldSource
	FieldLevel
	FieldMessage
	FieldAttributes
)

// SourceFormat defines the rendering style for FieldSource.
type SourceFormat int

const (
	SourceShort SourceFormat = iota // basename:line (default)
	SourceLong                      // fullpath:line
)

// DefaultFields lists the fields rendered when cfg.Fields is nil.
var DefaultFields = []Field{FieldTime, FieldSource, FieldLevel, FieldMessage, FieldAttributes}

// DefaultTheme provides the built-in color scheme.
var DefaultTheme = Theme{
	TimeColor:           ColorTime,
	SourceColor:         ColorTime,
	LevelColors:         levelColors,
	MessageColors:       nil, // nil means fallback to LevelColors
	AttributeKeyColor:   ColorKey,
	AttributeValueColor: ColorValue,
	GroupColor:          ColorPurple,
	Reset:               ColorReset,
}

// Config groups all handler options including the slog.HandlerOptions,
// the Theme, and the ordered list of Fields to render.
type Config struct {
	slog.HandlerOptions
	Theme  Theme
	Fields []Field
	// TimeFormat specifies the time layout (time.Format). Empty defaults to "15:04:05.999".
	TimeFormat string
	// SourceFormat controls short vs long file path in source field.
	SourceFormat SourceFormat
	// FieldWidths defines fixed column widths for field alignment.
	FieldWidths map[Field]int
}

// DefaultConfig is used when New receives a nil Config.
var DefaultConfig = Config{
	HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
	Theme:          DefaultTheme,
	Fields:         DefaultFields,
}

// PrettyTextHandler is a human-friendly slog handler that prints
// colorized, aligned, low-allocation log lines.
//
// PrettyTextHandler supports slog groups, ReplaceAttr, AddSource, and
// attribute propagation. It is safe for concurrent use.
type PrettyTextHandler struct {
	cfg          Config
	out          io.Writer
	mu           sync.Mutex
	group        string
	groups       []string // pre-computed from group for Handle
	preAttrs     []slog.Attr
	bufPool      *sync.Pool
	timeFormat   string       // resolved once in New
	sourceFormat SourceFormat // cached from cfg
	fieldWidths  [6]int       // index = Field enum, O(1)
}

// New creates a new PrettyTextHandler writing output to 'out'.
//
// If cfg is nil, DefaultConfig is used. Empty theme colors are filled
// from DefaultTheme. If cfg.Fields is nil, DefaultFields is used.
func New(out io.Writer, cfg *Config) *PrettyTextHandler {
	h := &PrettyTextHandler{out: out}

	if cfg == nil {
		h.cfg = DefaultConfig
		h.cfg.Theme.LevelColors = copyLevelColors(DefaultTheme.LevelColors)
		if DefaultTheme.MessageColors != nil {
			h.cfg.Theme.MessageColors = copyLevelColors(DefaultTheme.MessageColors)
		}
		h.cfg.Fields = copyFields(DefaultFields)
	} else {
		h.cfg = *cfg
		// Deep copy mutable maps/slices to avoid data race with caller.
		h.cfg.Theme.LevelColors = copyLevelColors(h.cfg.Theme.LevelColors)
		if h.cfg.Theme.MessageColors != nil {
			h.cfg.Theme.MessageColors = copyLevelColors(h.cfg.Theme.MessageColors)
		}
		if h.cfg.Fields != nil {
			h.cfg.Fields = copyFields(h.cfg.Fields)
		}
		mergeTheme(&h.cfg.Theme)
		if h.cfg.Fields == nil {
			h.cfg.Fields = copyFields(DefaultFields)
		}
	}

	if h.cfg.Level == nil {
		h.cfg.Level = slog.LevelInfo
	}

	// Resolve time format
	if h.cfg.TimeFormat == "" {
		h.timeFormat = "15:04:05.999"
	} else {
		h.timeFormat = h.cfg.TimeFormat
	}

	// Cache source format
	h.sourceFormat = h.cfg.SourceFormat

	// Build field widths array from cfg.FieldWidths
	// FieldLevel defaults to 5 for backward compatibility
	h.fieldWidths[FieldLevel] = 5
	if h.cfg.FieldWidths != nil {
		for f, w := range h.cfg.FieldWidths {
			if int(f) < len(h.fieldWidths) {
				h.fieldWidths[f] = w
			}
		}
	}

	h.bufPool = &sync.Pool{
		New: func() any {
			b := make([]byte, 0, initialBufferSize)
			return &b
		},
	}

	return h
}

// mergeTheme fills zero-value colors in t with values from DefaultTheme.
func mergeTheme(t *Theme) {
	if len(t.TimeColor) == 0 {
		t.TimeColor = DefaultTheme.TimeColor
	}
	if len(t.SourceColor) == 0 {
		t.SourceColor = DefaultTheme.SourceColor
	}
	if t.LevelColors == nil {
		t.LevelColors = copyLevelColors(DefaultTheme.LevelColors)
	}
	// MessageColors nil is intentional: it means "fallback to LevelColors".
	if len(t.AttributeKeyColor) == 0 {
		t.AttributeKeyColor = DefaultTheme.AttributeKeyColor
	}
	if len(t.AttributeValueColor) == 0 {
		t.AttributeValueColor = DefaultTheme.AttributeValueColor
	}
	if len(t.GroupColor) == 0 {
		t.GroupColor = DefaultTheme.GroupColor
	}
	if len(t.Reset) == 0 {
		t.Reset = DefaultTheme.Reset
	}
}

func copyLevelColors(m map[slog.Level][]byte) map[slog.Level][]byte {
	if m == nil {
		return nil
	}
	cp := make(map[slog.Level][]byte, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func copyFields(f []Field) []Field {
	cp := make([]Field, len(f))
	copy(cp, f)
	return cp
}

// spaces provides pre-allocated spaces for padding (zero-alloc).
var spaces = []byte("                                                                ") // 64 spaces

// appendPadding pads buf to the given width based on visible content length.
func appendPadding(buf []byte, width, currentLen int) []byte {
	padLen := width - currentLen
	if padLen <= 0 {
		return buf
	}
	for padLen > 0 {
		chunk := padLen
		if chunk > len(spaces) {
			chunk = len(spaces)
		}
		buf = append(buf, spaces[:chunk]...)
		padLen -= chunk
	}
	return buf
}

// Enabled reports whether a log entry of the given level should be emitted.
func (h *PrettyTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.cfg.Level.Level()
}

// Handle formats and writes a slog.Record to the output.
// It iterates over the configured Fields, applying Theme colors,
// and reuses an internal buffer pool for efficiency.
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

	// Build effective fields from config using a stack-allocated array
	// to avoid heap allocation on every Handle call.
	var effectiveFields [6]Field // max 5 configured fields + 1 for source fallback
	n := copy(effectiveFields[:], h.cfg.Fields)
	fieldsSlice := effectiveFields[:n]

	// Auto-insert FieldSource if AddSource is on and it's not already listed.
	if h.cfg.AddSource && r.PC != 0 {
		hasSource := false
		for _, f := range fieldsSlice {
			if f == FieldSource {
				hasSource = true
				break
			}
		}
		if !hasSource && len(fieldsSlice) < cap(effectiveFields[:]) {
			insertIdx := 0
			for i, f := range fieldsSlice {
				if f == FieldTime {
					insertIdx = i + 1
					break
				}
			}
			fieldsSlice = append(fieldsSlice, Field(0))
			copy(fieldsSlice[insertIdx+1:], fieldsSlice[insertIdx:])
			fieldsSlice[insertIdx] = FieldSource
		}
	}

	needSpace := false

	for _, field := range fieldsSlice {
		switch field {
		case FieldTime:
			if needSpace {
				buf = append(buf, ' ')
			}
			buf = append(buf, h.cfg.Theme.TimeColor...)
			timeStart := len(buf)
			buf = r.Time.AppendFormat(buf, h.timeFormat)
			timeLen := len(buf) - timeStart
			buf = append(buf, h.cfg.Theme.Reset...)
			buf = appendPadding(buf, h.fieldWidths[FieldTime], timeLen)
			needSpace = true

		case FieldSource:
			if h.cfg.AddSource && r.PC != 0 {
				fn := runtime.FuncForPC(r.PC)
				if fn != nil {
					file, line := fn.FileLine(r.PC)
					if file != "" {
						if h.sourceFormat == SourceShort {
							// Compute basename of file.
							for j := len(file) - 1; j >= 0; j-- {
								if file[j] == '/' || file[j] == '\\' {
									file = file[j+1:]
									break
								}
							}
						}
						if needSpace {
							buf = append(buf, ' ')
						}
						buf = append(buf, h.cfg.Theme.SourceColor...)
						srcStart := len(buf)
						buf = append(buf, file...)
						buf = append(buf, ':')
						buf = strconv.AppendInt(buf, int64(line), 10)
						srcLen := len(buf) - srcStart
						buf = append(buf, h.cfg.Theme.Reset...)
						buf = appendPadding(buf, h.fieldWidths[FieldSource], srcLen)
						needSpace = true
					}
				}
			}

		case FieldLevel:
			levelColor, ok := h.cfg.Theme.LevelColors[r.Level]
			if !ok {
				levelColor = ColorWhite
			}
			if needSpace {
				buf = append(buf, ' ')
			}
			buf = append(buf, levelColor...)
			levelStr := levelNames[r.Level]
			levelStart := len(buf)
			buf = append(buf, levelStr...)
			levelLen := len(buf) - levelStart
			buf = append(buf, h.cfg.Theme.Reset...)
			buf = appendPadding(buf, h.fieldWidths[FieldLevel], levelLen)
			needSpace = true

		case FieldMessage:
			msgColor := h.cfg.Theme.MessageColors[r.Level]
			if len(msgColor) == 0 {
				msgColor = h.cfg.Theme.LevelColors[r.Level]
			}
			if len(msgColor) == 0 {
				msgColor = ColorWhite
			}
			if needSpace {
				buf = append(buf, ' ')
			}
			buf = append(buf, msgColor...)
			msgStart := len(buf)
			buf = append(buf, r.Message...)
			msgLen := len(buf) - msgStart
			buf = append(buf, h.cfg.Theme.Reset...)
			buf = appendPadding(buf, h.fieldWidths[FieldMessage], msgLen)
			needSpace = true

		case FieldAttributes:
			groups := h.groups

			appendAttr := func(a slog.Attr) {
				if h.cfg.ReplaceAttr != nil {
					a = h.cfg.ReplaceAttr(groups, a)
					if a.Equal(slog.Attr{}) {
						return
					}
				}

				if needSpace {
					buf = append(buf, ' ')
				}
				buf = append(buf, h.cfg.Theme.AttributeKeyColor...)
				if h.group != "" {
					buf = append(buf, h.group...)
					buf = append(buf, '.')
				}
				buf = append(buf, a.Key...)
				buf = append(buf, h.cfg.Theme.Reset...)
				buf = append(buf, '=')
				buf = append(buf, h.cfg.Theme.AttributeValueColor...)
				buf = h.appendValue(buf, a.Value)
				buf = append(buf, h.cfg.Theme.Reset...)
				needSpace = true
			}

			for _, a := range h.preAttrs {
				appendAttr(a)
			}
			r.Attrs(func(a slog.Attr) bool {
				appendAttr(a)
				return true
			})
		default:
			// Unknown field, skip.
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
		buf = append(buf, h.cfg.Theme.Reset...)
		buf = append(buf, h.cfg.Theme.GroupColor...)
		buf = append(buf, '(')
		for i, attr := range attrs {
			if i > 0 {
				buf = append(buf, ' ')
			}
			buf = append(buf, h.cfg.Theme.Reset...)
			buf = append(buf, h.cfg.Theme.AttributeKeyColor...)
			buf = append(buf, attr.Key...)
			buf = append(buf, h.cfg.Theme.Reset...)
			buf = append(buf, '=')
			buf = append(buf, h.cfg.Theme.AttributeValueColor...)
			buf = h.appendValue(buf, attr.Value)
			buf = append(buf, h.cfg.Theme.Reset...)
		}
		buf = append(buf, h.cfg.Theme.GroupColor...)
		buf = append(buf, ')')
		buf = append(buf, h.cfg.Theme.Reset...)
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
		buf = append([]byte{'-'}, buf...)
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
		cfg:          h.cfg,
		out:          h.out,
		mu:           sync.Mutex{},
		group:        h.group,
		groups:       h.groups,
		preAttrs:     newPreAttrs,
		bufPool:      h.bufPool,
		timeFormat:   h.timeFormat,
		sourceFormat: h.sourceFormat,
		fieldWidths:  h.fieldWidths,
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
		cfg:          h.cfg,
		out:          h.out,
		mu:           sync.Mutex{},
		group:        newGroup,
		groups:       strings.Split(newGroup, "."),
		preAttrs:     h.preAttrs,
		bufPool:      h.bufPool,
		timeFormat:   h.timeFormat,
		sourceFormat: h.sourceFormat,
		fieldWidths:  h.fieldWidths,
	}
}

// Builder provides a chainable API for constructing PrettyTextHandler
// with custom themes, fields, and formatting options.
type Builder struct {
	out io.Writer
	cfg Config
}

// NewBuilder creates a new Builder with DefaultConfig values.
// Mutable maps and slices are deep-copied to prevent data races.
func NewBuilder(out io.Writer) *Builder {
	b := &Builder{
		out: out,
		cfg: Config{
			HandlerOptions: slog.HandlerOptions{Level: slog.LevelInfo},
			Theme: Theme{
				TimeColor:           DefaultTheme.TimeColor,
				SourceColor:         DefaultTheme.SourceColor,
				LevelColors:         copyLevelColors(DefaultTheme.LevelColors),
				MessageColors:       nil, // nil means fallback to LevelColors
				AttributeKeyColor:   DefaultTheme.AttributeKeyColor,
				AttributeValueColor: DefaultTheme.AttributeValueColor,
				GroupColor:          DefaultTheme.GroupColor,
				Reset:               DefaultTheme.Reset,
			},
			Fields:       copyFields(DefaultFields),
			SourceFormat: SourceShort,
		},
	}
	return b
}

// WithLevel sets the minimum log level.
func (b *Builder) WithLevel(level slog.Leveler) *Builder {
	b.cfg.Level = level
	return b
}

// WithAddSource enables or disables source file:line output.
func (b *Builder) WithAddSource(addSource bool) *Builder {
	b.cfg.AddSource = addSource
	return b
}

// WithReplaceAttr sets the attribute replacement function.
func (b *Builder) WithReplaceAttr(fn func([]string, slog.Attr) slog.Attr) *Builder {
	b.cfg.ReplaceAttr = fn
	return b
}

// WithFields sets the ordered list of fields to render.
func (b *Builder) WithFields(fields ...Field) *Builder {
	b.cfg.Fields = copyFields(fields)
	return b
}

// WithTimeFormat sets the time format layout (time.Format).
func (b *Builder) WithTimeFormat(format string) *Builder {
	b.cfg.TimeFormat = format
	return b
}

// WithSourceFormat sets short or long file path rendering.
func (b *Builder) WithSourceFormat(format SourceFormat) *Builder {
	b.cfg.SourceFormat = format
	return b
}

// WithFieldWidth sets a fixed column width for a field.
func (b *Builder) WithFieldWidth(field Field, width int) *Builder {
	if b.cfg.FieldWidths == nil {
		b.cfg.FieldWidths = make(map[Field]int)
	}
	b.cfg.FieldWidths[field] = width
	return b
}

// WithTheme replaces the entire color theme.
func (b *Builder) WithTheme(t Theme) *Builder {
	b.cfg.Theme = t
	return b
}

// WithLevelColor sets the label color for a specific log level.
func (b *Builder) WithLevelColor(level slog.Level, color []byte) *Builder {
	if b.cfg.Theme.LevelColors == nil {
		b.cfg.Theme.LevelColors = make(map[slog.Level][]byte)
	}
	b.cfg.Theme.LevelColors[level] = color
	return b
}

// WithMessageColor sets the message color for a specific log level.
func (b *Builder) WithMessageColor(level slog.Level, color []byte) *Builder {
	if b.cfg.Theme.MessageColors == nil {
		b.cfg.Theme.MessageColors = make(map[slog.Level][]byte)
	}
	b.cfg.Theme.MessageColors[level] = color
	return b
}

// Build creates a PrettyTextHandler from the builder configuration.
func (b *Builder) Build() *PrettyTextHandler {
	return New(b.out, &b.cfg)
}
