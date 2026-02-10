# AgentUsage

A terminal dashboard for monitoring AI coding tool quotas, spend, and rate limits — all in one place.

AgentUsage auto-detects the AI tools and API keys on your workstation and displays live quota snapshots in a [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI with dashboard, list, and analytics views. Six built-in color themes keep it looking good in any terminal.

## Supported Providers

AgentUsage ships with 13 provider adapters covering API services, local IDEs, and CLI tools.

| Provider | Method | Metrics |
|---|---|---|
| **OpenAI** | API header probing | RPM, TPM, RPD rate limits |
| **Anthropic** | API header probing | RPM, TPM rate limits |
| **OpenRouter** | REST API | Credits, spend, per-model generation stats |
| **Groq** | API header probing | RPM, TPM, RPD, TPD rate limits |
| **Mistral** | REST API | Subscription info, usage, rate limits |
| **DeepSeek** | REST API | Balance, rate limits |
| **xAI (Grok)** | REST API | Balance, rate limits |
| **Gemini API** | REST API | Model token limits, quota remaining |
| **Gemini CLI** | Local files + OAuth API | CodeAssist quota buckets, conversation counts |
| **GitHub Copilot** | `gh` CLI + API | Seat count, org billing, usage metrics, session state |
| **Cursor IDE** | DashboardService API + local SQLite | Spend limit, plan usage, per-model stats, daily activity |
| **Claude Code** | Local files | Daily cost, burn rate, billing blocks, messages, sessions |
| **OpenAI Codex CLI** | Session files | Rate limits, token usage, session counts |

## Installation

### Homebrew (macOS / Linux)

```bash
brew install janekbaraniewski/tap/agentusage
```

### From source

Requires Go 1.25+ and a C compiler (CGO is needed for the Cursor provider's SQLite support).

```bash
go install github.com/janekbaraniewski/agentusage/cmd/agentusage@latest
```

### From release binaries

Pre-built binaries are available for macOS (amd64, arm64), Linux (amd64, arm64), and Windows (amd64). Download the archive for your platform from [Releases](https://github.com/janekbaraniewski/agentusage/releases), extract it, and place `agentusage` somewhere on your `PATH`.

### Build locally

```bash
git clone https://github.com/janekbaraniewski/agentusage.git
cd agentusage
make build          # binary appears in ./bin/agentusage
```

> **Note:** Building from source requires CGO enabled (`CGO_ENABLED=1`) and a working C compiler, because the Cursor provider reads local SQLite databases via `mattn/go-sqlite3`.

## Quick Start

Just run:

```bash
agentusage
```

With **auto-detection enabled** (the default), AgentUsage scans for:

- **Cursor IDE** — reads local AI tracking databases and calls the DashboardService API
- **Claude Code CLI** — reads `~/.claude/stats-cache.json`, account info, and conversation logs
- **OpenAI Codex CLI** — reads session rate limits and token usage from `~/.codex/sessions/`
- **GitHub Copilot** — via the `gh` CLI extension (checks that `gh copilot` is installed)
- **Gemini CLI** — reads `~/.gemini/` config files and refreshes OAuth tokens for quota data
- **Aider CLI** — detected on PATH (delegates to underlying API providers)
- **Environment variables** — `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, `GROQ_API_KEY`, `MISTRAL_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`

If any of the above are present, AgentUsage will immediately start displaying live data.

## Configuration

Config file location: `~/.config/agentusage/config.toml`

```toml
auto_detect = true

[ui]
refresh_interval_seconds = 30
warn_threshold = 0.20    # 20% remaining → yellow
crit_threshold = 0.05    # 5% remaining → red

[[accounts]]
id = "openai-personal"
provider = "openai"
api_key_env = "OPENAI_API_KEY"
probe_model = "gpt-4.1-mini"

[[accounts]]
id = "anthropic-work"
provider = "anthropic"
api_key_env = "ANTHROPIC_API_KEY"
```

See [`configs/example.toml`](configs/example.toml) for a full reference with all providers.

Auto-detected accounts are merged with manually configured ones — configured accounts take precedence.

## TUI Navigation

AgentUsage has three screens, cycled with `Tab` / `Shift+Tab`:

| Screen | Description |
|---|---|
| **Dashboard** | Tile grid overview of all providers |
| **List** | Master-detail list with scrollable, tabbed detail panel |
| **Analytics** | Spend analysis with five sub-tabs: Overview, Providers, Models, Budget, Efficiency |

### Keyboard Shortcuts

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Switch screen |
| `↑` `↓` `←` `→` / `h` `j` `k` `l` | Navigate |
| `Enter` | Open detail view |
| `Esc` / `Backspace` | Back to list |
| `/` | Filter providers |
| `[` `]` | Cycle detail/analytics tabs |
| `g` / `G` | Jump to top / bottom |
| `t` | Cycle color theme |
| `r` | Manual refresh |
| `?` | Help overlay |
| `q` | Quit |

**Analytics-specific:**

| Key | Action |
|---|---|
| `o` `p` `m` `b` `e` | Jump to Overview / Providers / Models / Budget / Efficiency |
| `s` | Cycle sort order |

### Themes

Six built-in themes, cycled with `t`:

Catppuccin Mocha · Dracula · Nord · Tokyo Night · Gruvbox · Synthwave '84

## Environment Variables

| Variable | Purpose |
|---|---|
| `AGENTUSAGE_DEBUG` | Set to `1` to enable debug logging to stderr |

## Development

```bash
make help           # list all targets
make deps           # download and verify dependencies
make fmt            # format code
make vet            # run go vet
make lint           # run golangci-lint (skips if not installed)
make test           # run tests with race detection and coverage
make test-verbose   # verbose test output
make run            # run the app locally
make build          # build binary to ./bin/
make clean          # remove build artifacts
```

## Architecture

```
main.go → config.Load() → detect.AutoDetect() → core.NewEngine()
  → registers providers from providers.AllProviders()
  → engine.Run() polls all accounts concurrently on a ticker
  → snapshots sent to TUI via tea.Program.Send()
  → tui.Model renders Dashboard / List / Analytics
```

Every provider implements the `QuotaProvider` interface:

```go
type QuotaProvider interface {
    ID() string
    Describe() ProviderInfo
    Fetch(ctx context.Context, acct AccountConfig) (QuotaSnapshot, error)
}
```

Providers fall into four patterns:

- **HTTP header probing** — lightweight API request, parse rate-limit headers (OpenAI, Anthropic, Groq, Mistral, DeepSeek, xAI, Gemini API)
- **Rich REST API** — multiple endpoint calls for credits, billing, and generation stats (OpenRouter, Cursor)
- **Local file readers** — parse stats files, session logs, and conversation data (Claude Code, Codex, Gemini CLI)
- **CLI subprocess** — shell out to `gh` CLI commands (Copilot)

## Project Structure

```
cmd/agentusage/         CLI entry point
internal/
  config/               TOML config loading & defaults
  core/                 Engine, provider interface, snapshot types
  detect/               Auto-detection of tools & API keys
  parsers/              Shared rate-limit header parsing helpers
  providers/            Provider adapters (one package per vendor)
    anthropic/          Anthropic API header probing
    claude_code/        Claude Code local stats & billing blocks
    codex/              OpenAI Codex CLI session file reader
    copilot/            GitHub Copilot via gh CLI
    cursor/             Cursor IDE API + local SQLite fallback
    deepseek/           DeepSeek balance + rate limits
    gemini_api/         Google Gemini API quota probing
    gemini_cli/         Gemini CLI local config + OAuth quota API
    groq/               Groq API header probing
    mistral/            Mistral subscription + usage API
    openai/             OpenAI API header probing
    openrouter/         OpenRouter credits + generation stats
    xai/                xAI balance + rate limits
  tui/                  Bubble Tea UI (views, themes, charts, gauges)
  version/              Build-time version metadata (ldflags)
configs/                Example configuration file
```

## License

MIT — see [LICENSE](LICENSE) for details.
