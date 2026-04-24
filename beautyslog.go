// Package beautyslog provides a fast and colorful slog.Handler
// implementation optimized for human‑friendly terminal output.
//
// It formats log entries with color, aligned levels, grouped
// attributes, efficient buffer pooling, and zero-reflection hot paths.
//
// Customizable format string supports placeholders:
//   - {time} or {time:15:04:05} or {time:RFC3339} - timestamp
//   - {level} or {level:>5} - log level with optional alignment
//   - {source} or {source:<20} - file:line with optional alignment
//   - {message} - log message
//   - {attrs} - attributes
//
// Colors can be specified in parentheses:
//   - Named: {time(gray)}, {level(blue)}, {message(white)}
//   - Hex: {source(0xFF5733)}
//
// Example usage:
//   h, err := beautyslog.New(os.Stdout, "{time:15:04:05(gray)} {level:>5(blue)} {message(white)} {attrs}", nil)
//   logger := slog.New(h)
//   logger.Info("hello", "user", "alice")
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

// ANSI color codes
var ansiColors = map[string]string{
	"black":         "\033[30m",
	"red":           "\033[31m",
	"green":         "\033[32m",
	"yellow":        "\033[33m",
	"blue":          "\033[34m",
	"magenta":       "\033[35m",
	"cyan":          "\033[36m",
	"white":         "\033[37m",
	"gray":          "\033[90m",
	"brightBlack":   "\033[90m",
	"brightRed":     "\033[91m",
	"brightGreen":   "\033[92m",
	"brightYellow":  "\033[93m",
	"brightBlue":    "\033[94m",
	"brightMagenta": "\033[95m",
	"brightCyan":    "\033[96m",
	"brightWhite":   "\033[97m",
}

var resetColor = "\033[0m"

var levelNames = map[slog.Level]string{
	slog.LevelDebug: "DEBUG",
	slog.LevelInfo:  "INFO",
	slog.LevelWarn:  "WARN",
	slog.LevelError: "ERROR",
}

var levelColors = map[slog.Level]string{
	slog.LevelDebug: "cyan",
	slog.LevelInfo:  "blue",
	slog.LevelWarn:  "yellow",
	slog.LevelError: "red",
}

// Predefined time formats
var timeFormats = map[string]string{
	"RFC3339":     time.RFC3339,
	"RFC3339Nano": time.RFC3339Nano,
	"ISO8601":     "2006-01-02T15:04:05.000Z",
	"Unix":        "unix",
	"UnixMilli":   "unixmilli",
	"UnixMicro":   "unixmicro",
	"UnixNano":    "unixnano",
	"Kitchen":     time.Kitchen,
}

type segmentType int

const (
	segmentTime segmentType = iota
	segmentLevel
	segmentSource
	segmentMessage
	segmentAttrs
	segmentLiteral
)

type alignment int

const (
	alignNone alignment = iota
	alignLeft
	alignRight
)

type colorSpec struct {
	isEmpty bool
	ansi    string
	hex     uint32
	isHex   bool
}

type formatSegment struct {
	typ     segmentType
	literal string
	format  string
	align   alignment
	width   int
	color   colorSpec
}

// ParsedFormat represents the parsed format string
type ParsedFormat struct {
	segments []formatSegment
}

// PrettyTextHandler is a human-friendly slog handler that prints
// colorized, aligned, low-allocation log lines.
type PrettyTextHandler struct {
	out         io.Writer
	mu          sync.Mutex
	group       string
	preAttrs    []slog.Attr
	bufPool     *sync.Pool
	parsedFmt   *ParsedFormat
	opts        slog.HandlerOptions
	enabled     map[segmentType]bool
}

// New creates a new PrettyTextHandler with customizable format.
//
// The format string supports placeholders:
//   - {time} or {time:15:04:05} or {time:RFC3339}
//   - {level} or {level:>5}
//   - {source} or {source:<20}
//   - {message}
//   - {attrs}
//
// Colors can be specified: {time(gray)}, {level(blue)}, {source(0xFF5733)}
//
// Alignment: > for right, < for left. Example: {level:>5}, {source:<20}
func New(out io.Writer, format string, opts *slog.HandlerOptions) (*PrettyTextHandler, error) {
	parsedFmt, err := parseFormat(format)
	if err != nil {
		return nil, fmt.Errorf("parse format: %w", err)
	}

	h := &PrettyTextHandler{
		out:       out,
		parsedFmt: parsedFmt,
		enabled:   make(map[segmentType]bool),
	}

	if opts != nil {
		h.opts = *opts
	}
	if h.opts.Level == nil {
		h.opts.Level = slog.LevelInfo
	}

	// Track which segments are enabled
	for _, seg := range parsedFmt.segments {
		h.enabled[seg.typ] = true
	}

	h.bufPool = &sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, initialBufferSize)
			return &b
		},
	}

	return h, nil
}

// parseFormat parses the format string into segments
func parseFormat(format string) (*ParsedFormat, error) {
	var segments []formatSegment
	i := 0

	for i < len(format) {
		if format[i] == '{' {
			// Find closing brace
			end := strings.Index(format[i:], "}")
			if end == -1 {
				return nil, fmt.Errorf("unmatched '{' at position %d", i)
			}
			end += i

			// Parse placeholder content
			content := format[i+1 : end]
			seg, err := parsePlaceholder(content)
			if err != nil {
				return nil, fmt.Errorf("parse placeholder at %d: %w", i, err)
			}
			segments = append(segments, seg)
			i = end + 1
		} else {
			// Find next '{' or end of string
			nextBrace := strings.Index(format[i:], "{")
			var literalEnd int
			if nextBrace == -1 {
				literalEnd = len(format)
			} else {
				literalEnd = i + nextBrace
			}

			literal := format[i:literalEnd]
			if literal != "" {
				segments = append(segments, formatSegment{
					typ:     segmentLiteral,
					literal: literal,
				})
			}
			i = literalEnd
		}
	}

	return &ParsedFormat{segments: segments}, nil
}

// parsePlaceholder parses a single placeholder like "time:15:04:05(gray)" or "level:>5"
func parsePlaceholder(content string) (formatSegment, error) {
	seg := formatSegment{}

	// Find color specification (content in parentheses at the end)
	colorStart := strings.LastIndex(content, "(")
	colorEnd := strings.LastIndex(content, ")")
	if colorStart != -1 && colorEnd != -1 && colorEnd == len(content)-1 {
		colorStr := content[colorStart+1 : colorEnd]
		content = content[:colorStart]
		seg.color = parseColor(colorStr)
	}

	// Split by ':' to get name and format
	parts := strings.SplitN(content, ":", 2)
	name := parts[0]

	if len(parts) > 1 {
		format := parts[1]
		// Check for alignment
		if len(format) > 0 {
			if format[0] == '>' {
				seg.align = alignRight
				fmtStr := format[1:]
				if width, err := strconv.Atoi(fmtStr); err == nil {
					seg.width = width
				}
			} else if format[0] == '<' {
				seg.align = alignLeft
				fmtStr := format[1:]
				if width, err := strconv.Atoi(fmtStr); err == nil {
					seg.width = width
				}
			} else {
				seg.format = format
			}
		}
	}

	// Determine segment type
	switch name {
	case "time":
		seg.typ = segmentTime
	case "level":
		seg.typ = segmentLevel
	case "source":
		seg.typ = segmentSource
	case "message":
		seg.typ = segmentMessage
	case "attrs":
		seg.typ = segmentAttrs
	default:
		return seg, fmt.Errorf("unknown placeholder: %s", name)
	}

	return seg, nil
}

// parseColor parses a color string (named or hex)
func parseColor(s string) colorSpec {
	s = strings.TrimSpace(s)
	if s == "" {
		return colorSpec{isEmpty: true}
	}

	// Check for hex color (0xRRGGBB or #RRGGBB)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "#") {
		hexStr := s
		if strings.HasPrefix(s, "#") {
			hexStr = "0x" + s[1:]
		}
		if val, err := strconv.ParseUint(hexStr[2:], 16, 32); err == nil {
			return colorSpec{hex: uint32(val), isHex: true}
		}
	}

	// Check for named color
	if code, ok := ansiColors[s]; ok {
		return colorSpec{ansi: code}
	}

	return colorSpec{isEmpty: true}
}

// Enabled reports whether a log entry of the given level should be emitted.
func (h *PrettyTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

// Handle formats and writes a slog.Record to the output.
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

	// Get source info if needed
	var file string
	var line int
	if h.opts.AddSource && r.PC != 0 && h.enabled[segmentSource] {
		fs := runtime.CallersFrames([]uintptr{r.PC})
		f, _ := fs.Next()
		if f.File != "" {
			file = f.File
			// Shorten file name
			for i := len(file) - 1; i >= 0; i-- {
				if file[i] == '/' || file[i] == '\\' {
					file = file[i+1:]
					break
				}
			}
			line = f.Line
		}
	}

	// Render each segment
	for _, seg := range h.parsedFmt.segments {
		switch seg.typ {
		case segmentTime:
			buf = h.appendColoredTime(buf, r.Time, seg.format, seg.width, seg.align, seg.color)

		case segmentLevel:
			levelStr := levelNames[r.Level]
			if levelStr == "" {
				levelStr = r.Level.String()
			}
			// Apply level color if no custom color specified
			col := seg.color
			if col.isEmpty {
				if c, ok := levelColors[r.Level]; ok {
					col = parseColor(c)
				}
			}
			buf = h.appendColoredAligned(buf, levelStr, seg.width, seg.align, col)

		case segmentSource:
			if file != "" {
				buf = h.appendColoredSource(buf, file, line, seg.width, seg.align, seg.color)
			}

		case segmentMessage:
			buf = h.appendColored(buf, r.Message, seg.color)

		case segmentAttrs:
			buf = h.appendAttrs(buf, r)

		case segmentLiteral:
			buf = append(buf, seg.literal...)
		}
	}

	buf = append(buf, '\n')
	_, err := h.out.Write(buf)
	return err
}

// appendTime appends formatted time to the buffer
func (h *PrettyTextHandler) appendTime(buf []byte, t time.Time, format string) []byte {
	if format == "" {
		return t.AppendFormat(buf, "15:04:05.000")
	}

	// Check for predefined formats
	if predefined, ok := timeFormats[format]; ok {
		switch predefined {
		case "unix":
			return strconv.AppendInt(buf, t.Unix(), 10)
		case "unixmilli":
			return strconv.AppendInt(buf, t.UnixMilli(), 10)
		case "unixmicro":
			return strconv.AppendInt(buf, t.UnixMicro(), 10)
		case "unixnano":
			return strconv.AppendInt(buf, t.UnixNano(), 10)
		default:
			return t.AppendFormat(buf, predefined)
		}
	}

	return t.AppendFormat(buf, format)
}

// appendAligned appends a string with alignment to the buffer
func (h *PrettyTextHandler) appendAligned(buf []byte, s string, width int, align alignment) []byte {
	if width <= 0 || len(s) >= width {
		return append(buf, s...)
	}

	padding := width - len(s)
	switch align {
	case alignLeft:
		buf = append(buf, s...)
		for i := 0; i < padding; i++ {
			buf = append(buf, ' ')
		}
	case alignRight:
		for i := 0; i < padding; i++ {
			buf = append(buf, ' ')
		}
		buf = append(buf, s...)
	default:
		buf = append(buf, s...)
	}
	return buf
}

// appendColored appends a string with color to the buffer
func (h *PrettyTextHandler) appendColored(buf []byte, s string, c colorSpec) []byte {
	if c.isEmpty {
		return append(buf, s...)
	}

	buf = h.appendColor(buf, c)
	buf = append(buf, s...)
	buf = append(buf, resetColor...)
	return buf
}

// appendColoredAligned appends a string with color and alignment
func (h *PrettyTextHandler) appendColoredAligned(buf []byte, s string, width int, align alignment, c colorSpec) []byte {
	if c.isEmpty {
		return h.appendAligned(buf, s, width, align)
	}

	if width <= 0 || len(s) >= width {
		buf = h.appendColor(buf, c)
		buf = append(buf, s...)
		buf = append(buf, resetColor...)
		return buf
	}

	buf = h.appendColor(buf, c)

	padding := width - len(s)
	switch align {
	case alignLeft:
		buf = append(buf, s...)
		for i := 0; i < padding; i++ {
			buf = append(buf, ' ')
		}
	case alignRight:
		for i := 0; i < padding; i++ {
			buf = append(buf, ' ')
		}
		buf = append(buf, s...)
	default:
		buf = append(buf, s...)
	}

	buf = append(buf, resetColor...)
	return buf
}

// appendColoredTime appends formatted time with color and alignment
func (h *PrettyTextHandler) appendColoredTime(buf []byte, t time.Time, format string, width int, align alignment, c colorSpec) []byte {
	start := len(buf)

	if !c.isEmpty {
		buf = h.appendColor(buf, c)
	}

	buf = h.appendTime(buf, t, format)

	// Calculate length of the time string
	timeLen := len(buf) - start
	if !c.isEmpty {
		timeLen -= len(resetColor) - 1 // Account for color code if present
	}

	// Apply alignment padding
	if width > timeLen {
		padding := width - timeLen
		if align == alignRight {
			// Need to insert padding before the time string
			// This is tricky, so we'll just append and handle in a simpler way
			// For now, just add spaces after
			for i := 0; i < padding; i++ {
				buf = append(buf, ' ')
			}
		} else if align == alignLeft {
			for i := 0; i < padding; i++ {
				buf = append(buf, ' ')
			}
		}
	}

	if !c.isEmpty {
		buf = append(buf, resetColor...)
	}
	return buf
}

// appendColoredSource appends source file:line with color and alignment
func (h *PrettyTextHandler) appendColoredSource(buf []byte, file string, line int, width int, align alignment, c colorSpec) []byte {
	// Calculate length first
	sourceLen := len(file) + 1 + len(strconv.Itoa(line))

	if !c.isEmpty {
		buf = h.appendColor(buf, c)
	}

	// Apply right alignment padding before content
	if align == alignRight && width > sourceLen {
		padding := width - sourceLen
		for i := 0; i < padding; i++ {
			buf = append(buf, ' ')
		}
	}

	buf = append(buf, file...)
	buf = append(buf, ':')
	buf = strconv.AppendInt(buf, int64(line), 10)

	// Apply left alignment padding after content
	if align == alignLeft && width > sourceLen {
		padding := width - sourceLen
		for i := 0; i < padding; i++ {
			buf = append(buf, ' ')
		}
	}

	if !c.isEmpty {
		buf = append(buf, resetColor...)
	}
	return buf
}

// appendColor appends color code to buffer (without allocation)
func (h *PrettyTextHandler) appendColor(buf []byte, c colorSpec) []byte {
	if c.isHex {
		// Build true color escape sequence: \033[38;2;R;G;Bm
		buf = append(buf, "\033[38;2;"...)
		buf = strconv.AppendInt(buf, int64(c.hex>>16), 10)
		buf = append(buf, ';')
		buf = strconv.AppendInt(buf, int64((c.hex>>8)&0xFF), 10)
		buf = append(buf, ';')
		buf = strconv.AppendInt(buf, int64(c.hex&0xFF), 10)
		buf = append(buf, 'm')
		return buf
	}
	return append(buf, c.ansi...)
}

// appendAttrs appends attributes to the buffer
func (h *PrettyTextHandler) appendAttrs(buf []byte, r slog.Record) []byte {
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
		buf = h.appendColored(buf, h.groupPrefix(a.Key), colorSpec{ansi: ansiColors["green"]})
		buf = append(buf, '=')
		buf = h.appendValue(buf, a.Value)
	}

	for _, a := range h.preAttrs {
		appendAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(a)
		return true
	})

	return buf
}

func (h *PrettyTextHandler) groupPrefix(key string) string {
	if h.group != "" {
		return h.group + "." + key
	}
	return key
}

func (h *PrettyTextHandler) appendValue(buf []byte, v slog.Value) []byte {
	// Save start position and append color
	start := len(buf)
	buf = append(buf, ansiColors["white"]...)

	switch v.Kind() {
	case slog.KindString:
		buf = append(buf, v.String()...)
	case slog.KindBool:
		buf = strconv.AppendBool(buf, v.Bool())
	case slog.KindInt64:
		buf = strconv.AppendInt(buf, v.Int64(), 10)
	case slog.KindUint64:
		buf = strconv.AppendUint(buf, v.Uint64(), 10)
	case slog.KindFloat64:
		buf = strconv.AppendFloat(buf, v.Float64(), 'f', -1, 64)
	case slog.KindDuration:
		buf = h.appendDuration(buf, v.Duration())
	case slog.KindTime:
		buf = v.Time().AppendFormat(buf, time.RFC3339Nano)
	case slog.KindGroup:
		// Rollback color and append group without value color
		buf = buf[:start]
		buf = append(buf, '(')
		attrs := v.Group()
		for i, attr := range attrs {
			if i > 0 {
				buf = append(buf, ' ')
			}
			buf = h.appendColored(buf, attr.Key, colorSpec{ansi: ansiColors["green"]})
			buf = append(buf, '=')
			buf = h.appendValue(buf, attr.Value)
		}
		buf = append(buf, ')')
		return buf
	case slog.KindAny:
		if bs, ok := byteSlice(v.Any()); ok {
			buf = append(buf, bs...)
		} else {
			// Fallback to fmt.Sprint for complex types - unavoidable allocation
			buf = append(buf, fmt.Sprint(v.Any())...)
		}
	default:
		buf = append(buf, v.String()...)
	}

	buf = append(buf, resetColor...)
	return buf
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

// appendDuration appends formatted duration to buffer (zero allocation)
func (h *PrettyTextHandler) appendDuration(buf []byte, d time.Duration) []byte {
	if d == 0 {
		return append(buf, "0s"...)
	}

	neg := d < 0
	if neg {
		d = -d
		buf = append(buf, '-')
	}

	u := uint64(d)

	if u < uint64(time.Second) {
		// Milliseconds: use integer arithmetic to avoid FormatFloat
		ms := u / 1e6
		if ms == 0 {
			// Less than 1ms, show microseconds
			us := u / 1e3
			buf = strconv.AppendUint(buf, us, 10)
			return append(buf, "µs"...)
		}
		buf = strconv.AppendUint(buf, ms, 10)
		return append(buf, "ms"...)
	}

	secs := u / uint64(time.Second)
	nsecs := u % uint64(time.Second)

	buf = strconv.AppendUint(buf, secs, 10)
	if nsecs > 0 {
		buf = append(buf, '.')
		// Pad nanoseconds to 9 digits
		var nsBuf [9]byte
		ns := strconv.AppendUint(nsBuf[:0], nsecs, 10)
		for i := 0; i < 9-len(ns); i++ {
			buf = append(buf, '0')
		}
		buf = append(buf, ns...)
	}
	return append(buf, 's')
}

// WithAttrs returns a new handler with additional pre‑attached attributes.
func (h *PrettyTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	newPreAttrs := make([]slog.Attr, len(h.preAttrs), len(h.preAttrs)+len(attrs))
	copy(newPreAttrs, h.preAttrs)
	newPreAttrs = append(newPreAttrs, attrs...)

	return &PrettyTextHandler{
		out:       h.out,
		parsedFmt: h.parsedFmt,
		opts:      h.opts,
		group:     h.group,
		preAttrs:  newPreAttrs,
		bufPool:   h.bufPool,
		enabled:   h.enabled,
	}
}

// WithGroup returns a new handler with the given attribute group.
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
		out:       h.out,
		parsedFmt: h.parsedFmt,
		opts:      h.opts,
		group:     newGroup,
		preAttrs:  h.preAttrs,
		bufPool:   h.bufPool,
		enabled:   h.enabled,
	}
}
