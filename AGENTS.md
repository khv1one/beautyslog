# AGENTS.md — beautyslog

Single-package Go library: a pretty, performant `slog.Handler` for terminals.

## Project Structure

- One package, root directory. No sub-packages, no monorepo.
- **Zero external dependencies** — `go.mod` has no `require` block.

## Key Files

| File | Purpose |
|---|---|
| `beautyslog.go` | Handler implementation (~540 lines). |
| `beautyslog_test.go` | Unit tests, benchmarks, fuzzing, concurrency/race tests (~1200 lines). |
| `go.mod` | Go 1.24. No deps to manage. |
| `.github/workflows/ci.yml` | CI: `go mod tidy` → `go test ./... -v` → `go build ./...` |

## Developer Commands

```bash
# Run tests (all)
go test ./... -v

# Run a specific test
go test ./... -run TestDefaultConfig -v

# Run with race detector
go test ./... -race -v

# Run benchmarks
go test ./... -bench=. -benchmem

# Run fuzzing (target: FuzzAppendValue)
go test ./... -fuzz=FuzzAppendValue -fuzztime=30s
```

## Critical Constraints

- **Performance-critical**: Uses `sync.Pool` for buffer reuse and zero-allocation hot paths.
  - Always run benchmarks (`go test -bench=.`) after any change to `Handle`, `appendValue`, or buffer logic.
- **Concurrency-safe**: `sync.Mutex` protects writes; `WithAttrs`/`WithGroup` return clones.
  - Always run with `-race` after changes to shared state or cloning logic.
- **NO_COLOR support**: The `NO_COLOR` env var disables colors automatically.
  - The test `TestHandle_NO_COLOR` mutates `os.Setenv` — if you add new env-based tests, isolate them or serialize them to avoid flakiness.
