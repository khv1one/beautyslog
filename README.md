[![Go Reference](https://pkg.go.dev/badge/github.com/khv1one/beautyslog.svg)](https://pkg.go.dev/github.com/khv1one/beautyslog)
[![Go Report Card](https://goreportcard.com/badge/github.com/khv1one/beautyslog)](https://goreportcard.com/report/github.com/khv1one/beautyslog)
[![Build](https://github.com/khv1one/beautyslog/actions/workflows/ci.yml/badge.svg)](https://github.com/khv1one/beautyslog/actions)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

<img src="./assets/logo.svg" alt="beautyslog logo" width="420" />

---

> **A fast & beautiful `slog.Handler` for modern Go terminals**

A fast and colorful `slog.Handler` implementation for human‑friendly terminal logs.

`beautyslog` formats log entries with colors, aligned levels, grouped attributes, and efficient buffer reuse.

It is **2x faster than the default `slog.TextHandler`** and performs **3x fewer allocations** (benchmarks below).

---

<img src="./assets/output.png" alt="beautyslog logo" width="640" />

## 🚀 Features

* Pretty, colorized terminal logs
* Fast and allocation‑efficient (`sync.Pool` for buffers)
* Fully compatible with `log/slog`
* Supports groups, attributes, `ReplaceAttr`, `AddSource`
* **Fully customizable** — field order, colors per element, level names/colors, time format
* Respects `NO_COLOR` environment variable
* Clean, aligned output
* Safe for concurrent use
* Works as a drop‑in replacement for any slog handler

---

## 📦 Installation

```bash
go get github.com/khv1one/beautyslog
```

---

## 🧩 Usage

### Basic Setup (backward compatible)

```go
logger := slog.New(beautyslog.New(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
    AddSource: true,
}))

logger.Info("server started", "port", 8080)
logger.Debug("processing request", "id", 123)
```

### Using groups and attributes

```go
log := logger.With(
    slog.String("service", "billing"),
)

log.WithGroup("db").Info("query executed",
    "sql", "SELECT * FROM payments",
    "duration", 12*time.Millisecond,
)
```

### Using ReplaceAttr

```go
handler := beautyslog.New(os.Stdout, &slog.HandlerOptions{
    ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
        if a.Key == slog.TimeKey {
            return slog.String(a.Key, time.Now().Format(time.DateTime))
        }
        return a
    },
})
```

---

## 🎨 Customization

Use `NewWithConfig` with a `Config` to control every aspect of the output.
Start from `DefaultConfig()` and tweak only what you need.

### Custom field order

Reorder or omit log line fields:

```go
cfg := beautyslog.DefaultConfig()
cfg.Fields = []beautyslog.Field{
    beautyslog.FieldLevel,
    beautyslog.FieldTime,
    beautyslog.FieldMessage,
    beautyslog.FieldAttributes,
    // FieldSource omitted — no source line printed
}
logger := slog.New(beautyslog.NewWithConfig(os.Stdout, cfg))
```

### Custom colors per element

Override colors for time, source, message, keys, values, or groups:

```go
cfg := beautyslog.DefaultConfig()
cfg.Theme = beautyslog.Theme{
    Time:    beautyslog.ColorPurple,
    Source:  beautyslog.ColorGray,
    Message: "",            // empty → inherits level color
    Key:     beautyslog.ColorCyan,
    Value:   beautyslog.ColorWhite,
    Group:   beautyslog.ColorYellow,
}
logger := slog.New(beautyslog.NewWithConfig(os.Stdout, cfg))
```

Available color constants: `ColorRed`, `ColorGreen`, `ColorYellow`, `ColorBlue`,
`ColorPurple`, `ColorCyan`, `ColorWhite`, `ColorGray`, `ColorOrange`.

### Custom level names and colors

Rename levels or change their colors:

```go
cfg := beautyslog.DefaultConfig()
cfg.LevelConfigs[slog.LevelInfo] = beautyslog.LevelConfig{
    Name:  "OK",
    Color: beautyslog.ColorGreen,
}
cfg.LevelConfigs[slog.LevelWarn] = beautyslog.LevelConfig{
    Name:  "SLOW",
    Color: beautyslog.ColorOrange,
}
logger := slog.New(beautyslog.NewWithConfig(os.Stdout, cfg))
```

### Disabling colors

Set `DisableColors` to strip all ANSI escape sequences:

```go
cfg := beautyslog.DefaultConfig()
cfg.DisableColors = true
logger := slog.New(beautyslog.NewWithConfig(os.Stdout, cfg))
```

Or set the `NO_COLOR` environment variable (no code changes needed):

```bash
NO_COLOR=1 ./myapp
```

`beautyslog` respects the [NO_COLOR](https://no-color.org/) convention automatically.

### Custom time format

```go
cfg := beautyslog.DefaultConfig()
cfg.TimeFormat = "2006-01-02 15:04:05"
logger := slog.New(beautyslog.NewWithConfig(os.Stdout, cfg))
```

### Full config example

```go
cfg := beautyslog.DefaultConfig()

// Reorder fields: level first, then time, message, attrs
cfg.Fields = []beautyslog.Field{
    beautyslog.FieldLevel,
    beautyslog.FieldTime,
    beautyslog.FieldMessage,
    beautyslog.FieldAttributes,
}

cfg.Theme.Key   = beautyslog.ColorCyan
cfg.Theme.Value = beautyslog.ColorWhite

cfg.LevelConfigs[slog.LevelInfo] = beautyslog.LevelConfig{
    Name:  "OK",
    Color: beautyslog.ColorGreen,
}

cfg.TimeFormat = "15:04:05"

logger := slog.New(beautyslog.NewWithConfig(os.Stdout, cfg))
logger.Info("ready", "addr", ":8080")
```

---

## 🧪 Benchmarks

Measured on Apple M1 Pro:

```
BenchmarkSlogTextHandlerWithSource-8         909481         1299 ns/op        384 B/op      6 allocs/op
BenchmarkPrettyTextHandlerWithSource-8       2020834        595.0 ns/op       248 B/op      2 allocs/op
BenchmarkSlogTextHandlerWithoutSource-8      1370437        874.2 ns/op         3 B/op      1 allocs/op
BenchmarkPrettyTextHandlerWithoutSource-8    2736182        435.6 ns/op         0 B/op      0 allocs/op
```

**beautyslog is twice as fast and 3× more memory‑efficient.**

---

## 🔧 Best Practices

* Use `WithGroup` for struct‑like hierarchical logs
* Use `WithAttrs` for shared fields
* Prefer `NewWithConfig` over `ReplaceAttr` for color/field customization
* Keep attribute names short for cleaner output
* Avoid logging giant byte arrays; they render raw
* Set `NO_COLOR` in production to auto‑disable colors for log files
