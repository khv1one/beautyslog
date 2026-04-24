# Agent Instructions for beautyslog

Single-package Go library (`github.com/khv1one/beautyslog`) providing a custom `slog.Handler` for pretty terminal output. No external dependencies.

## Toolchain quirks

- No code generation, no build tags, no special tooling.

## Verification commands

```bash
go test ./... -v
go test -bench=. -benchmem
go build ./...
```

- CI runs `go mod tidy` → `go test ./... -v` → `go build ./...` on push/PR to `main`.

## Testing gotchas

- `TestPrint` is a visual smoke test that writes directly to `os.Stdout`. It will not fail under normal conditions, but it produces noise in verbose test output.
- Benchmarks compare against standard library `slog.TextHandler`.
- `.gitignore` ignores `coverage*` and `mem.out`; these are ad-hoc profiling artifacts.

## Architecture notes

- **Immutable handlers:** `WithAttrs` and `WithGroup` return new `*PrettyTextHandler` copies; never mutate the receiver.
- **Thread safety:** `Handle` acquires a `sync.Mutex` for the duration of the write.
- **Buffer pooling:** `sync.Pool` reuses `[]byte` buffers. Buffers larger than `maxBufferSize` (4096 bytes) are discarded instead of returned to the pool.
- **Constructors:**
  - `New(out, opts)` panics on error (only called internally with hard-coded config).
  - `NewWithConfig(out, opts, cfg)` returns `(\*PrettyTextHandler, error)` and is the only public entry point that validates user input (e.g., template parsing).
- **Template fields:** supported tokens are `{time}`, `{level}`, `{message}`, `{source}`, `{attrs}`. Level padding (to 5 chars) is applied only in default/non-template layout mode.
- **Color resolution:** `resolvedColorScheme` is built once at construction; zero-value fields in `ColorScheme` fall back to built-in defaults.
