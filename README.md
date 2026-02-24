<p align="center">
  <img src="./assets/logo.gif" alt="OpenUsage logo">
</p>

<p align="center"><strong>The coding agent usage dashboard you've been looking for.</strong></p>

<p align="center">
  <a href="#install">Install</a> &middot;
  <a href="#features">Features</a> &middot;
  <a href="#supported-providers">Providers</a> &middot;
  <a href="#configuration">Config</a> &middot;
  <a href="#telemetry--daemon">Daemon</a> &middot;
  <a href="#keybindings">Keybindings</a> &middot;
  <a href="#development">Development</a>
</p>

---

OpenUsage auto-detects AI coding tools and API keys on your workstation and shows live quota, usage, and cost data in your terminal. Zero config required — just run `openusage`.

![OpenUsage dashboard](./assets/dashboard.png)

Run it side-by-side with your coding agent:

<p align="center">
  <img src="./assets/sidebyside.png" alt="OpenUsage side by side">
  <br>
  <em>OpenUsage running alongside OpenCode monitoring live OpenRouter usage.</em>
</p>

## Install

### macOS (Homebrew, recommended)

```bash
brew install janekbaraniewski/tap/openusage
```

### All platforms (quick install script)

Downloads the latest release artifact for your platform and installs `openusage`.

```bash
curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash
```

On Windows, run the command in Git Bash, MSYS2, or Cygwin.

### From source (Go 1.25+)

Pre-built binaries for macOS, Linux, and Windows are also available on the [GitHub Releases](https://github.com/janekbaraniewski/openusage/releases) page.

```bash
go install github.com/janekbaraniewski/openusage/cmd/openusage@latest
```

Building from source requires CGO (`CGO_ENABLED=1`) because the Cursor provider uses SQLite via `mattn/go-sqlite3`.

## Run

```bash
openusage
```

Auto-detection is enabled by default and picks up local tools plus common API key env vars. No config needed.

## Features

### Auto-detection

OpenUsage scans your system for installed AI tools and environment variables — no manual setup required:

- **Local tools**: Cursor, Claude Code, Codex, Copilot (`gh` CLI), Gemini CLI, Ollama, Aider
- **API keys**: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `OPENROUTER_API_KEY`, `GROQ_API_KEY`, `MISTRAL_API_KEY`, `DEEPSEEK_API_KEY`, `XAI_API_KEY`, `GEMINI_API_KEY`, `ALIBABA_CLOUD_API_KEY`, `OLLAMA_API_KEY`, `OPENCODE_API_KEY`

Auto-detected accounts merge with manually configured ones; manual config takes precedence on conflicts.

### Dashboard view

The primary screen shows a master-detail layout: a tile grid of all your providers on the left, with a scrollable detail panel on the right for the selected provider.

Each provider tile shows:
- Status badge (OK / WARN / LIMIT / AUTH / ERR)
- Key metrics at a glance (spend, quota, tokens)
- Color-coded gauge bar (green → yellow → red as you approach limits)

The detail panel shows full breakdowns: per-model usage, billing info, daily trends, and raw metrics.

### Analytics view (experimental)

Switch to the analytics screen with `Tab` for spend analysis across all providers:
- Per-provider and per-model cost breakdowns
- Budget tracking with gauge bars
- Token activity charts
- Sortable by cost, name, or token count

Enable in config: `"experimental": {"analytics": true}`

### Themes

Cycle through 7 built-in themes with `t`:

| Theme | |
|---|---|
| Catppuccin Mocha | Purple accents |
| Dracula | Cyan/pink accents |
| Nord | Arctic blue/green |
| Tokyo Night | Purple night palette |
| Gruvbox (default) | Warm retro tones |
| Synthwave '84 | Neon 80s vibes |
| One Dark | VS Code-inspired |

### Time windows

Cycle with `w` to filter data by time range: **1d**, **3d**, **7d**, or **30d**.

### Settings modal

Press `,` or `Shift+S` to open the in-app settings modal with tabs for:

1. **Theme** — pick from 7 themes
2. **UI** — warn/crit thresholds, refresh interval
3. **Integrations** — install/uninstall hooks for Claude Code, Cursor, etc.
4. **Daemon** — install/uninstall the background telemetry service
5. **Accounts** — add/edit accounts with API key validation
6. **Provider order** — reorder providers with `Shift+J/K`

### Provider reordering

Reorder providers directly from the dashboard with `Shift+J` / `Shift+K`. Your order is persisted in the config file.

## Supported providers

OpenUsage ships with 16 provider integrations covering coding agents, API platforms, and local tools.

### Coding agents & IDEs

| Provider | Detection | What it tracks |
|---|---|---|
| **Claude Code** | `claude` binary + `~/.claude` | Daily activity, per-model tokens, 5-hour billing blocks, burn rate, cost estimation |
| **Cursor** | `cursor` binary + local SQLite DBs | Plan spend & limits, per-model aggregation, Composer sessions, AI code scoring |
| **GitHub Copilot** | `gh` CLI + Copilot extension | Chat & completions quota, org billing, org metrics, session tracking |
| **Codex CLI** | `codex` binary + `~/.codex` | Session tokens, per-model and per-client breakdown, credits, rate limits |
| **Gemini CLI** | `gemini` binary + `~/.gemini` | OAuth status, conversation count, per-model tokens, quota API |
| **OpenCode** | `OPENCODE_API_KEY` / `ZEN_API_KEY` | Credits, activity, generation stats (OpenRouter-compatible) |
| **Ollama** | `OLLAMA_HOST` / binary | Local server models, per-model usage, optional cloud billing |

#### Claude Code

![Claude Code provider](./assets/claudecode.png)

#### Cursor

![Cursor provider](./assets/cursor.png)

#### GitHub Copilot

![Copilot provider](./assets/copilot.png)

#### OpenRouter

![OpenRouter provider](./assets/openrouter.png)

### API platforms

| Provider | Detection | What it tracks |
|---|---|---|
| **OpenAI** | `OPENAI_API_KEY` | Rate limits via header probing |
| **Anthropic** | `ANTHROPIC_API_KEY` | Rate limits via header probing |
| **OpenRouter** | `OPENROUTER_API_KEY` | Credits, activity, generation stats, per-model breakdown |
| **Groq** | `GROQ_API_KEY` | Rate limits, daily usage windows |
| **Mistral AI** | `MISTRAL_API_KEY` | Subscription, usage endpoints |
| **DeepSeek** | `DEEPSEEK_API_KEY` | Rate limits, account balance |
| **xAI (Grok)** | `XAI_API_KEY` | Rate limits, API key info |
| **Google Gemini API** | `GEMINI_API_KEY` / `GOOGLE_API_KEY` | Rate limits, model limits |
| **Alibaba Cloud** | `ALIBABA_CLOUD_API_KEY` | Quotas, credits, daily usage, per-model tracking |

## Configuration

You don't need a config file — auto-detection handles everything. You can always override or extend the defaults.

Config path:

- macOS/Linux: `~/.config/openusage/settings.json`
- Windows: `%APPDATA%\openusage\settings.json`

Minimal example:

```json
{
  "auto_detect": true,
  "ui": {
    "refresh_interval_seconds": 30,
    "warn_threshold": 0.2,
    "crit_threshold": 0.05
  },
  "accounts": [
    {
      "id": "openai-personal",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "probe_model": "gpt-4.1-mini"
    }
  ]
}
```

Full config example: [`configs/example_settings.json`](configs/example_settings.json)

### Config options

| Key | Description | Default |
|---|---|---|
| `auto_detect` | Scan for installed tools and API keys | `true` |
| `ui.refresh_interval_seconds` | Poll interval in seconds | `30` |
| `ui.warn_threshold` | Yellow gauge at this % remaining | `0.20` |
| `ui.crit_threshold` | Red gauge at this % remaining | `0.05` |
| `theme` | Active theme name | `"Gruvbox"` |
| `data.time_window` | Default time window (`1d`, `3d`, `7d`, `30d`) | `"30d"` |
| `data.retention_days` | How long to keep telemetry data | `30` |
| `experimental.analytics` | Enable the analytics screen | `false` |
| `model_normalization.enabled` | Group variant model names into lineages | `true` |

## Telemetry & daemon

OpenUsage includes a background daemon for continuous data collection, even when the dashboard isn't open. The TUI connects to the daemon over a unix socket when available.

### Start the daemon

```bash
# Run in foreground
openusage telemetry daemon

# Install as system service (launchd / systemd)
openusage telemetry daemon install

# Check status
openusage telemetry daemon status

# Uninstall
openusage telemetry daemon uninstall
```

### Ingest events via hooks

Pipe JSON payloads to the daemon for supported providers:

```bash
cat event.json | openusage telemetry hook opencode
```

Supported hook sources: `opencode`, `codex`, `claude_code`.

### Integrations

Install hooks and plugins for deeper tool integration:

```bash
openusage integrations list [--all]       # List integration statuses
openusage integrations install <id>       # Install hook/plugin
openusage integrations uninstall <id>     # Remove
openusage integrations upgrade --all      # Upgrade all installed
```

Available integrations: Claude Code hooks, Cursor rules, Copilot, Codex CLI, Gemini CLI.

## Keybindings

### Navigation

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Switch between Dashboard and Analytics |
| `j` / `k` or `Up` / `Down` | Move cursor |
| `h` / `l` or `Left` / `Right` | Navigate panels |
| `Enter` | Open detail view |
| `Esc` | Back |
| `PgUp` / `PgDn` | Scroll selected tile |
| `Ctrl+U` / `Ctrl+D` | Fast scroll |
| `[ ]` | Switch detail tabs |

### Actions

| Key | Action |
|---|---|
| `r` | Refresh all providers |
| `/` | Filter providers |
| `t` | Cycle theme |
| `w` | Cycle time window (1d / 3d / 7d / 30d) |
| `,` or `Shift+S` | Open settings |
| `Ctrl+O` | Expand/collapse usage breakdowns |
| `Shift+J` / `Shift+K` | Reorder providers |
| `?` | Help overlay |
| `q` | Quit |

## Development

```bash
make build          # Build binary to ./bin/openusage
make test           # Run all tests with -race and coverage
make test-verbose   # Verbose test output
make lint           # golangci-lint (skips gracefully if not installed)
make fmt            # go fmt ./...
make vet            # go vet ./...
make run            # go run cmd/openusage/main.go
```

### Run demo

Preview the dashboard with simulated data (no API keys needed):

```bash
make demo
```

### Debug mode

```bash
OPENUSAGE_DEBUG=1 openusage
```

## License

[MIT](LICENSE)
