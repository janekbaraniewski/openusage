# OpenUsage Subsystem Map

Quick reference for the exploration phase. Lists primary files per subsystem.

## Core Types
- `internal/core/types.go` — UsageSnapshot, Metric, Status, AccountConfig
- `internal/core/provider_spec.go` — ProviderSpec, ProviderAuthSpec, UsageProvider interface
- `internal/core/widget.go` — DashboardWidget, ColorRole, CompactRows
- `internal/core/detail_widget.go` — DetailWidget, section styles
- `internal/core/snapshot_normalize.go` — snapshot post-processing
- `internal/core/time_window.go` — TimeWindow type, parsing, cycling, SQL offsets

## Providers
- `internal/providers/registry.go` — AllProviders(), TelemetrySourceBySystem()
- `internal/providers/providerbase/base.go` — Base struct, DefaultDashboard()
- `internal/providers/shared/helpers.go` — RequireAPIKey, CreateStandardRequest, etc.
- `internal/parsers/helpers.go` — rate limit header parsing

### Provider patterns
- **Header probing**: `openai/`, `anthropic/`, `groq/`, `mistral/`, `deepseek/`, `xai/`, `gemini_api/`, `alibaba_cloud/`
- **Rich API**: `openrouter/`, `cursor/`
- **Local files**: `claude_code/`, `codex/`, `gemini_cli/`, `ollama/`
- **CLI subprocess**: `copilot/`
- **Plugin/integration**: `opencode/`

## TUI
- `internal/tui/model.go` — main Model, Update, View, key handlers
- `internal/tui/tiles.go` — dashboard tile rendering
- `internal/tui/analytics.go` — analytics tab
- `internal/tui/detail.go` — detail panel
- `internal/tui/styles.go` — themes, colors
- `internal/tui/gauge.go` — gauge bars
- `internal/tui/charts.go` — bar charts
- `internal/tui/help.go` — help overlay, keybinding reference
- `internal/tui/settings_modal.go` — settings modal with tabs (providers, theme, API keys, telemetry, integrations)
- `internal/tui/provider_widget.go` — provider widget rendering from DashboardWidget/DetailWidget specs

## Config
- `internal/config/config.go` — Config struct, Load(), defaults, DataConfig, normalization
- `internal/config/credentials.go` — credential storage
- `configs/example_settings.json` — reference config

## Detect
- `internal/detect/detect.go` — AutoDetect(), env key mapping, tool detection

## Daemon
- `internal/daemon/types.go` — Config, ReadModelRequest/Response, DaemonStatus
- `internal/daemon/server.go` — daemon server, poll loop, retention loop
- `internal/daemon/client.go` — daemon client
- `internal/daemon/runtime.go` — ViewRuntime (client-side), time window state, ReadWithFallback
- `internal/daemon/accounts.go` — BuildReadModelRequest, account normalization, cache keys

## Telemetry
- `internal/telemetry/collector.go` — snapshot collection
- `internal/telemetry/read_model.go` — ReadModelOptions, aggregated view
- `internal/telemetry/usage_view.go` — per-account canonical usage view, SQL queries
- `internal/telemetry/store.go` — SQLite event storage, pruning
- `internal/telemetry/pipeline_test.go` — pipeline tests

## CLI
- `cmd/openusage/main.go` — CLI entry point
- `cmd/openusage/dashboard.go` — dashboard command, ViewRuntime setup, TUI callbacks
- `cmd/openusage/telemetry.go` — telemetry commands
- `cmd/demo/main.go` — demo mode with dummy data
