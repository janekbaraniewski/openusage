# ⚡ AgentUsage

A terminal dashboard for monitoring AI coding tool quotas, spend, and rate limits — all in one place.

AgentUsage auto-detects the AI tools and API keys on your workstation and displays live quota snapshots in a [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI with tile, list, and analytics views.

## Supported Providers

| Provider | Method | Metrics |
|---|---|---|
| **OpenAI** | API headers | RPM, TPM, RPD rate limits |
| **Anthropic** | API headers | RPM, TPM rate limits |
| **OpenRouter** | API | Plan spend, credits, limits |
| **Groq** | API headers | RPM, TPM rate limits |
| **Mistral** | API headers | Rate limits |
| **DeepSeek** | API headers | Rate limits |
| **xAI (Grok)** | API headers | Rate limits |
| **Gemini API** | API | Quota remaining |
| **Gemini CLI** | Local files | OAuth sessions, account info |
| **GitHub Copilot** | `gh` CLI | Copilot status |
| **Cursor IDE** | Local DBs | Spend limit, plan usage, model stats |
| **Claude Code** | Local files | Daily cost, burn rate, messages, sessions |
| **OpenAI Codex CLI** | Session files | Rate limits, token usage |

## Installation

### From source

```bash
go install github.com/janekbaraniewski/agentusage/cmd/agentusage@latest
```

### From release binaries

Download the archive for your platform from [Releases](https://github.com/janekbaraniewski/agentusage/releases), extract it, and place `agentusage` somewhere on your `PATH`.

### Build locally

```bash
git clone https://github.com/janekbaraniewski/agentusage.git
cd agentusage
make build          # binary appears in ./bin/agentusage
```

## Quick Start

Just run:

```bash
agentusage
```

With **auto-detection enabled** (the default), AgentUsage scans for:

- **Cursor IDE** — reads local AI tracking databases
- **Claude Code CLI** — reads `~/.claude/stats-cache.json` & account info
- **OpenAI Codex CLI** — reads session rate limits & tokens
- **GitHub Copilot** — via the `gh` CLI extension
- **Gemini CLI** — reads `~/.gemini/` config files
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
| **List** | Master-detail list with scrollable detail panel |
| **Analytics** | Spend analysis with per-provider/model breakdowns |

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
| `?` | Help overlay |
| `q` | Quit |

**Analytics-specific:**

| Key | Action |
|---|---|
| `o` `p` `m` `b` `e` | Jump to Overview / Providers / Models / Budget / Efficiency |
| `s` | Cycle sort order |

## Environment Variables

| Variable | Purpose |
|---|---|
| `AGENTUSAGE_DEBUG` | Set to `1` to enable debug logging to stderr |

## Development

```bash
make deps           # download dependencies
make fmt            # format code
make lint           # run golangci-lint
make test           # run tests with coverage
make test-verbose   # verbose test output
make run            # run the app locally
make build          # build binary to ./bin/
make clean          # remove build artifacts
```

## Project Structure

```
cmd/agentusage/         CLI entry point
internal/
  config/               TOML config loading
  core/                 Engine, provider interface, types
  detect/               Auto-detection of tools & API keys
  parsers/              Shared parsing helpers
  providers/            Provider adapters (one per vendor)
    anthropic/
    claude_code/
    codex/
    copilot/
    cursor/
    deepseek/
    gemini_api/
    gemini_cli/
    groq/
    mistral/
    openai/
    openrouter/
    xai/
  tui/                  Bubble Tea UI (views, styles, charts)
  version/              Build-time version metadata
configs/                Example configuration
```

## License

See [LICENSE](LICENSE) for details.
