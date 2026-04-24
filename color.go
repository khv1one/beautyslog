package beautyslog

import "log/slog"

// Color is an ANSI escape code for terminal colors.
type Color string

// Named color constants.
const (
	ColorReset  Color = "\033[0m"
	ColorCyan   Color = "\033[36m"
	ColorBlue   Color = "\033[34m"
	ColorYellow Color = "\033[33m"
	ColorRed    Color = "\033[31m"
	ColorGreen  Color = "\033[32m"
	ColorPeach  Color = "\033[38;5;216m"
	ColorGray   Color = "\033[90m"
	ColorWhite  Color = "\033[37m"
	ColorPurple Color = "\033[35m"
)

// ColorScheme defines colors for different log elements.
type ColorScheme struct {
	Debug   Color
	Info    Color
	Warn    Color
	Error   Color
	Time    Color
	Source  Color
	Key     Color
	Value   Color
	Message Color
	Group   Color
	Reset   Color
}

// resolvedColorScheme holds concrete strings for hot-path appends.
type resolvedColorScheme struct {
	debug, info, warn, errorLvl string
	time, source, key, value    string
	message, group, reset       string
}

func defaultColorScheme() resolvedColorScheme {
	return resolvedColorScheme{
		debug:    string(ColorCyan),
		info:     string(ColorBlue),
		warn:     string(ColorYellow),
		errorLvl: string(ColorRed),
		time:     string(ColorGray),
		source:   string(ColorGray),
		key:      string(ColorGreen),
		value:    string(ColorPeach),
		message:  "",
		group:    string(ColorPurple),
		reset:    string(ColorReset),
	}
}

func resolveColorScheme(cfg ColorScheme, noColor bool) resolvedColorScheme {
	if noColor {
		return resolvedColorScheme{}
	}
	d := defaultColorScheme()
	if cfg.Debug != "" {
		d.debug = string(cfg.Debug)
	}
	if cfg.Info != "" {
		d.info = string(cfg.Info)
	}
	if cfg.Warn != "" {
		d.warn = string(cfg.Warn)
	}
	if cfg.Error != "" {
		d.errorLvl = string(cfg.Error)
	}
	if cfg.Time != "" {
		d.time = string(cfg.Time)
	}
	if cfg.Source != "" {
		d.source = string(cfg.Source)
	}
	if cfg.Key != "" {
		d.key = string(cfg.Key)
	}
	if cfg.Value != "" {
		d.value = string(cfg.Value)
	}
	if cfg.Message != "" {
		d.message = string(cfg.Message)
	}
	if cfg.Group != "" {
		d.group = string(cfg.Group)
	}
	if cfg.Reset != "" {
		d.reset = string(cfg.Reset)
	}
	return d
}

func (c resolvedColorScheme) levelColor(lvl slog.Level) string {
	switch lvl {
	case slog.LevelDebug:
		return c.debug
	case slog.LevelInfo:
		return c.info
	case slog.LevelWarn:
		return c.warn
	case slog.LevelError:
		return c.errorLvl
	default:
		return c.reset
	}
}
