package beautyslog

import (
	"io"
	"log/slog"
	"sync"
)

// Config controls the output format, field order, and colors of PrettyTextHandler.
type Config struct {
	// Layout defines the order of fields. Ignored if Template is non-empty.
	// Zero/nil means DefaultLayout.
	Layout Layout

	// Template overrides Layout. Allows literals and custom separators.
	// Example: "[{level}] {time} {message} {attrs}"
	Template string

	// Colors overrides defaults. Zero-value Color fields fall back to built-ins.
	Colors ColorScheme

	// NoColor disables all ANSI codes.
	NoColor bool
}

// NewWithConfig creates a PrettyTextHandler with full customization.
// Returns an error if cfg.Template is non-empty and cannot be parsed.
func NewWithConfig(out io.Writer, slogOpts *slog.HandlerOptions, cfg *Config) (*PrettyTextHandler, error) {
	h := &PrettyTextHandler{out: out}
	if slogOpts != nil {
		h.opts = *slogOpts
	}
	if h.opts.Level == nil {
		h.opts.Level = slog.LevelInfo
	}

	// Resolve config.
	if cfg == nil {
		h.layout = DefaultLayout
		h.colors = defaultColorScheme()
	} else {
		if cfg.Template != "" {
			parsed, err := ParseTemplate(cfg.Template)
			if err != nil {
				return nil, err
			}
			h.useTemplate = true
			h.template = parsed
		} else if len(cfg.Layout) > 0 {
			h.layout = cfg.Layout
		} else {
			h.layout = DefaultLayout
		}
		h.noColor = cfg.NoColor
		h.colors = resolveColorScheme(cfg.Colors, cfg.NoColor)
	}

	h.bufPool = &sync.Pool{
		New: func() any {
			b := make([]byte, 0, initialBufferSize)
			return &b
		},
	}

	return h, nil
}
