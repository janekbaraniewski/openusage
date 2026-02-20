# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is this project?

AgentUsage is a terminal dashboard (TUI) for monitoring AI coding tool usage and spend. It auto-detects AI tools and API keys on the workstation and displays live data using [Bubble Tea](https://github.com/charmbracelet/bubbletea). Written in Go, requires CGO enabled (for `mattn/go-sqlite3` used by the Cursor provider).

## Commands

```bash
make build          # build binary to ./bin/agentusage (includes version ldflags)
make test           # run all tests with -race and coverage
make test-verbose   # verbose test output
make lint           # golangci-lint (skips gracefully if not installed)
make fmt            # go fmt ./...
make vet            # go vet ./...
make run            # go run cmd/agentusage/main.go

# Run a single test
go test ./internal/providers/openai/ -run TestFetch -v

# Run provider tests only
go test ./internal/providers/...
```

## Architecture

### Data flow

```
main.go → config.Load() → detect.AutoDetect() → core.NewEngine()
  → registers all providers from providers.AllProviders()
  → engine.Run() polls providers concurrently on a ticker
  → snapshots sent to TUI via tea.Program.Send(SnapshotsMsg)
  → tui.Model renders three screens: Dashboard / List / Analytics
```

### Core interface

Every provider implements `core.QuotaProvider`:

```go
type QuotaProvider interface {
    ID() string
    Describe() ProviderInfo
    Fetch(ctx context.Context, acct AccountConfig) (QuotaSnapshot, error)
}
```

Providers are registered in `internal/providers/registry.go` via `AllProviders()`.

### Provider patterns (13 providers)

There are three distinct implementation patterns:

- **HTTP header probing** (`openai`, `anthropic`, `groq`, `mistral`, `deepseek`, `xai`, `gemini_api`): Make a lightweight API request, parse rate-limit headers using shared helpers from `internal/parsers/`. ~50-100 lines each.
- **Rich API / local hybrid** (`openrouter`, `cursor`): Call multiple API endpoints; `cursor` also reads local SQLite DBs as fallback.
- **Local file readers** (`claude_code`, `codex`, `gemini_cli`): Read local stats/session files. `claude_code` is the most complex (~900 lines) with billing block computation and burn rate tracking.
- **CLI subprocess** (`copilot`): Shells out to `gh` CLI commands.

### TUI structure (`internal/tui/`)

Built with Bubble Tea's Model-Update-View pattern. Three screens cycled with Tab:
1. **Dashboard** — tile grid (`tiles.go`)
2. **List** — master-detail with tabbed detail panel (`model.go`, `detail.go`)
3. **Analytics** — spend analysis with 5 sub-tabs (`analytics.go`)

Theme system with 6 themes in `styles.go`, cycled with `t`. Visual components: smooth gauge bars (`gauge.go`), bar charts (`charts.go`), animated help overlay (`help.go`), fixed-size widget panels (`widget.go`).

### Auto-detection (`internal/detect/`)

Scans for installed tools (Cursor, Claude Code, Codex, Copilot, Gemini CLI, Aider) and 9 environment variables for API keys. Auto-detected accounts merge with manually configured ones; configured accounts take precedence.

## Testing patterns

- Standard `testing` package, no mocking frameworks
- Provider tests use `httptest.NewServer` with controlled headers/responses
- Table-driven tests for type logic (see `core/types_test.go`)
- Config tests use `t.TempDir()` for temp files
- No TUI tests exist

## Key design notes

- CGO is required due to `github.com/mattn/go-sqlite3` (Cursor provider reads local SQLite DBs). This affects cross-compilation.
- `AccountConfig.Binary` and `AccountConfig.BaseURL` are repurposed for non-API providers (e.g., Binary stores file paths for `claude_code`).
- Config file: `~/.config/agentusage/settings.json`. Reference config: `configs/example_settings.json`.
- Debug logging: set `AGENTUSAGE_DEBUG=1`.
- API keys are referenced via `api_key_env` in config (env var name), never stored directly.
