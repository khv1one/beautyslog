# beautyslog

Single-package Go library: a colorful, fast `slog.Handler` for terminal output.

## Quick Commands

```bash
# Test
go test ./...

# Test verbose (matches CI)
go test ./... -v

# Run benchmarks
go test -bench=. -benchmem

# Build (library, so just verifies compilation)
go build ./...
```

## Project Structure

- `beautyslog.go` — `PrettyTextHandler` implementation (~340 lines)
- `beautyslog_test.go` — tests + benchmarks comparing to `slog.TextHandler`
- `go.mod` — module `github.com/khv1one/beautyslog`, Go 1.24

## CI

GitHub Actions runs on push/PR to `main`:
1. `go mod tidy`
2. `go test ./... -v`
3. `go build ./...`

## Key Implementation Notes

- **No external dependencies** — stdlib only
- **Buffer pooling** — uses `sync.Pool` with 512B initial / 4096B max buffer size
- **Thread-safe** — `sync.Mutex` around output writes
- **Color output** — hardcoded ANSI codes (see `color*` constants)
- **Supports**: `AddSource`, `ReplaceAttr`, `WithGroup`, `WithAttrs`
