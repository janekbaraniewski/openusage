# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What is this project?

OpenUsage is a terminal dashboard (TUI) for monitoring AI coding tool usage and spend. It auto-detects AI tools and API keys on the workstation and displays live data using [Bubble Tea](https://github.com/charmbracelet/bubbletea). Written in Go, requires CGO enabled (for `mattn/go-sqlite3` used by the Cursor provider).

## Commands

```bash
make build          # build binary to ./bin/openusage (includes version ldflags)
make test           # run all tests with -race and coverage
make test-verbose   # verbose test output
make lint           # golangci-lint (skips gracefully if not installed)
make fmt            # go fmt ./...
make vet            # go vet ./...
make run            # go run cmd/openusage/main.go
make demo           # build and run demo with dummy data (for screenshots)

# Run a single test
go test ./internal/providers/openai/ -run TestFetch -v

# Run provider tests only
go test ./internal/providers/...
```

## Code style

- Standard `gofmt` with `goimports`. Tabs for indentation.
- Import groups (separated by blank lines): stdlib, third-party, internal.
- Bubble Tea aliased as `tea`.
- Errors wrapped with provider prefix: `fmt.Errorf("openai: creating request: %w", err)`.
- Pointer fields for optional numerics: `Limit *float64`.
- JSON tags use `snake_case` with `omitempty` for optional fields.

## Architecture

### Data flow

There are two runtime modes:

**Direct mode** (default):
```
main.go → config.Load() → runDashboard()
  → detect.AutoDetect() → registers providers from providers.AllProviders()
  → polls providers concurrently on a ticker
  → snapshots sent to TUI via tea.Program.Send(SnapshotsMsg)
```

**Daemon mode** (`openusage telemetry`):
```
daemon.Server polls providers → ingests into SQLite (telemetry.Store)
  → TUI connects via daemon.ViewRuntime over unix socket
  → daemon.ReadModel hydrates snapshots from stored events
  → telemetry events deduplicated, mapped to providers via ProviderLinks
```

### Core interface

Every provider implements `core.UsageProvider` (`internal/core/provider.go`):

```go
type UsageProvider interface {
    ID() string
    Describe() ProviderInfo
    Spec() ProviderSpec
    DashboardWidget() DashboardWidget
    DetailWidget() DetailWidget
    Fetch(ctx context.Context, acct AccountConfig) (UsageSnapshot, error)
}
```

- `ProviderSpec` (`provider_spec.go`) bundles auth/setup metadata + widget definitions.
- `DashboardWidget` / `DetailWidget` define how provider metrics render in the TUI.
- Providers are registered in `internal/providers/registry.go` via `AllProviders()`.

### Provider patterns (16 providers)

- **HTTP header probing** (`openai`, `anthropic`, `groq`, `mistral`, `deepseek`, `xai`, `gemini_api`, `alibaba_cloud`): Lightweight API request, parse rate-limit headers using shared helpers from `internal/parsers/`.
- **Rich API / local hybrid** (`openrouter`, `cursor`): Multiple API endpoints; `cursor` also reads local SQLite DBs as fallback.
- **Local file readers** (`claude_code`, `codex`, `gemini_cli`, `ollama`): Read local stats/session files. `claude_code` is the most complex with billing block computation and burn rate tracking.
- **CLI subprocess** (`copilot`): Shells out to `gh` CLI commands.
- **Plugin/integration** (`opencode`): Reads local session data from the OpenCode tool.

### TUI structure (`internal/tui/`)

Built with Bubble Tea's Model-Update-View pattern. Two screens cycled with Tab:
1. **Dashboard** — tile grid (`tiles.go`) with master-detail: left list + right detail panel (`detail.go`)
2. **Analytics** — spend analysis with sub-tabs (`analytics.go`)

Theme system with 6 themes in `styles.go`, cycled with `t`. Visual components: smooth gauge bars (`gauge.go`), bar charts (`charts.go`), animated help overlay (`help.go`), fixed-size widget panels (`widget.go`), settings modal (`settings_modal.go`).

Provider widgets (`provider_widget.go`) are driven by `DashboardWidget`/`DetailWidget` definitions from each provider's `Spec()`.

### Daemon & telemetry (`internal/daemon/`, `internal/telemetry/`)

Background data collection system with server/client architecture:
- `daemon.Server` — polls providers on interval, ingests snapshots into SQLite
- `daemon.ViewRuntime` — client-side runtime that connects to daemon over unix socket
- `telemetry.Store` — SQLite-backed event storage with deduplication
- `telemetry.Pipeline` — processes events from multiple sources (collector, hooks, spooling)
- `telemetry.ReadModel` — builds `UsageSnapshot` views from stored events
- `telemetry.ProviderLinks` — maps telemetry source systems to display provider IDs

### Auto-detection (`internal/detect/`)

Scans for installed tools (Cursor, Claude Code, Codex, Copilot, Gemini CLI, Aider, Ollama) and environment variables for API keys. Auto-detected accounts merge with manually configured ones; configured accounts take precedence.

## Key design notes

- CGO is required due to `github.com/mattn/go-sqlite3` (Cursor provider + telemetry store). This affects cross-compilation.
- `AccountConfig.Token` has `json:"-"` — never persisted to config. Providers that need runtime tokens must extract them in `Fetch()`.
- `AccountConfig.Binary` and `AccountConfig.BaseURL` are repurposed for non-API providers (e.g., Binary stores file paths for `claude_code`).
- Config file: `~/.config/openusage/settings.json`. Reference config: `configs/example_settings.json`.
- Debug logging: set `OPENUSAGE_DEBUG=1`.
- API keys are referenced via `api_key_env` in config (env var name), never stored directly.
- CLI uses cobra (`cmd/openusage/main.go`): default command runs dashboard, `telemetry` subcommand runs daemon.

## Testing patterns

- Standard `testing` package, no mocking frameworks.
- Provider tests use `httptest.NewServer` with controlled headers/responses.
- Table-driven tests for type logic (see `core/types_test.go`).
- Config tests use `t.TempDir()` for temp files.
- Telemetry tests use in-memory SQLite stores.

## Adding a new provider

Follow the full specification in `docs/skills/add-new-provider.md`. It defines a mandatory 7-phase process: Quiz -> Research -> Implement -> Widget -> Register -> Test -> Verify. Do not skip the quiz phase.
