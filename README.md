<p align="center">
  <img src="./assets/logo.gif" alt="OpenUsage logo">
</p>

<p align="center"><strong>OpenUsage.sh: terminal-first local quota and usage tracking for Claude Code, Codex CLI, Cursor, Copilot, and OpenRouter.</strong></p>

<p align="center">
  <a href="#install">Install</a> &middot;
  <a href="#supported-providers">Providers</a> &middot;
  <a href="#configuration">Config</a> &middot;
  <a href="#keybindings">Keybindings</a> &middot;
  <a href="#development">Development</a>
</p>

---

OpenUsage is the terminal-first local dashboard published at [openusage.sh](https://openusage.sh/). Publicly, the clearest brand reference is **OpenUsage.sh**. It auto-detects AI coding tools and API keys on your workstation and shows live quota, usage, spend, resets, rate limits, and model data in your terminal. It is built for mixed-tool workflows across Claude Code, Codex CLI, Cursor, Copilot, Gemini CLI, OpenRouter, OpenAI, Anthropic, and more. Zero config required — just run `openusage`.

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

```bash
curl -fsSL https://github.com/janekbaraniewski/openusage/releases/latest/download/install.sh | bash
```

### From source (Go 1.25+)

```bash
go install github.com/janekbaraniewski/openusage/cmd/openusage@latest
```

Requires CGO (`CGO_ENABLED=1`). Pre-built binaries are also available on the [Releases](https://github.com/janekbaraniewski/openusage/releases) page.

## Run

```bash
openusage
```

Auto-detection picks up local tools and common API key env vars. No config needed.

## Command-line reports & statusline

Besides the live dashboard, OpenUsage has headless subcommands that reuse the same parsing and pricing — handy for scripts, CI, and quick checks:

```bash
openusage daily                # usage & cost by day (also: weekly, monthly)
openusage session              # grouped by session
openusage blocks               # by 5-hour billing block, with burn rate + projection
openusage daily --json         # machine-readable output for scripts/CI
openusage statusline install   # one-line status bar for Claude Code
```

What each report can show, by provider:

| Report | Providers |
|---|---|
| `daily` / `weekly` / `monthly` | every provider that reports cost or tokens |
| `session` / `blocks` | Claude Code, Codex, Gemini CLI, Copilot, Cursor, OpenCode, Ollama, Amp, Codebuff, OpenClaw, Roo Code, Kilo Code, Crush, Goose, Hermes, Zed, Droid, Kiro |
| `statusline` | Claude Code |

Remote API platforms (OpenAI, Anthropic, OpenRouter, …) appear in the periodic reports only — they expose no per-turn data. See the [headless reports & statusline guide](docs/site/docs/guides/cli-reports.md) for the full matrix and flags.

### Add to tmux

<table>
<tr>
<td width="55%" valign="middle">

Show your Claude Code, Codex, Cursor, Copilot, and OpenRouter usage — cost, quota, burn rate, and the active tool — right in your **tmux status bar**. It tracks whichever tool you're actively using and renders its real, brand-colored logo.

</td>
<td width="45%" valign="middle">

![OpenUsage in the tmux status bar](./assets/tmux-ccode.png)

</td>
</tr>
</table>

Setup is a single command. It opens a one-screen, live-preview configurator where you pick the position, which tool(s) to show, the segment components, and emoji vs. real provider icons — and watch the result update as you go:

```bash
openusage tmux install                         # interactive: live-preview configurator
tmux source-file ~/.config/tmux/tmux.conf      # reload
```

<p align="center">
  <img src="./assets/install-tmux.gif" alt="Installing the OpenUsage tmux status segment with the live-preview configurator" width="720">
</p>

Want real provider logos instead of emoji? The configurator offers to install a bundled icon font and configure your terminal — see [provider icons](docs/site/docs/guides/tmux-integration.md#provider-icons-custom-font).

```bash
openusage tmux install --write                 # non-interactive (scripting): just write the snippet
openusage tmux --preset claude-focused         # preview other presets (12 built-in)
openusage tmux font setup                       # configure icons for kitty/Ghostty/WezTerm
openusage tmux doctor                          # diagnose if something is off
```

See the [tmux integration guide](docs/site/docs/guides/tmux-integration.md) for the format grammar, theming, the icon font, and watch-mode alerts.

### Claude Code statusline

Show live session cost, today's spend, the active 5-hour block, burn rate, and context-window usage right in the **Claude Code status bar**:

![OpenUsage statusline in Claude Code](./assets/claudecodestatus.png)

```bash
openusage statusline install     # interactive: live-preview configurator
```

Like the tmux installer, this opens a one-screen configurator where you toggle which segments to show (model, session/today/block cost, burn rate, **5h usage window %**, context %) and watch a live preview, then writes it into `~/.claude/settings.json`. Restart Claude Code to see it.

<p align="center">
  <img src="./assets/statusline-install.gif" alt="Installing the Claude Code statusline with the live-preview configurator" width="720">
</p>

See the [statusline guide](docs/site/docs/guides/claude-code-statusline.md) for customization and manual setup.

## Track coding agent usage across multiple platforms

Native dashboards show one provider at a time. OpenUsage gives you one local-first view across coding agents, API platforms, and local runtimes so you can answer:

- Which tool or provider is burning budget?
- Which model caused the spike?
- Which quota or reset is getting close?
- Which sessions, projects, or MCP tools drove the change?

It is built for end-user tool tracking, not for instrumenting a separate AI app with tracing SDKs or a billing backend.

If you want the full positioning argument, read the guide: [best way to track coding agent usage and quotas across providers](https://openusage.sh/best-way-track-coding-agent-usage-quotas-across-providers/).

If the question is whether this is the right fit versus a simpler local limits tracker, use:

- [OpenUsage.sh vs OpenUsage.ai](https://openusage.sh/docs/openusage-sh-vs-openusage-ai/)
- [Capability matrix](https://openusage.sh/docs/capability-matrix/)
- [Docs hub](https://openusage.sh/docs/)

## Features

- **Cross-provider tracking** — compare coding agents, API platforms, and local runtimes in one local dashboard
- **34 providers** — coding agents and CLIs (Claude Code, Codex, Cursor, Copilot, Gemini CLI, OpenCode, Amp, Goose, Roo Code, Kilo Code, Kiro, Zed, and more), API platforms (OpenAI, Anthropic, OpenRouter, Groq, Mistral, DeepSeek, Moonshot, Perplexity, xAI, Z.AI, and more), and local runtimes (Ollama)
- **Zero config** — auto-detects your AI tools and API keys, just run it
- **Live dashboard** — see spend, quotas, rate limits, tokens, burn rate, and per-model usage at a glance
- **tmux integration** — show the active tool's usage in your tmux status bar, with provider icons, presets, and active-tool detection
- **Claude Code statusline** — one-line session cost, today's cost, burn rate, and context usage in Claude Code
- **Headless reports** — `daily`, `weekly`, `monthly`, `session`, and `blocks` reports in table or JSON
- **Background tracking** — a daemon collects data continuously, even when the dashboard is closed, into a local SQLite database you own
- **Deep cost insights** — model, tool, project, MCP, and session breakdowns; combine providers like OpenCode + OpenRouter
- **Tool integrations** — optional hooks for Claude Code, Codex CLI, and OpenCode provide richer, real-time usage data
- **Export & metrics** — export snapshots to JSON or CSV, look up model pricing, or serve Prometheus metrics from the built-in hub
- **Customizable** — 17 built-in themes, adjustable time windows, configurable thresholds, provider reordering, plus external theme files

## Supported providers

34 provider integrations covering coding agents, CLIs, IDE tools, API platforms, and local runtimes. See [docs/providers.md](docs/providers.md) for all providers with detailed descriptions and screenshots.

### Claude Code

**Detection:** `claude` binary + `~/.claude` directory

Tracks daily activity, per-model token usage, 5-hour billing block computation, burn rate, and cost estimation.

![Claude Code provider](./assets/claudecode.png)

### OpenRouter

**Detection:** `OPENROUTER_API_KEY` environment variable

Tracks credits, activity, generation stats, and per-model breakdown across multiple API endpoints.

![OpenRouter provider](./assets/openrouter.png)

### All providers

#### Coding agents & IDEs

| Provider | Detection | What it tracks |
|---|---|---|
| **Claude Code** | `claude` binary + `~/.claude` | Daily activity, per-model tokens, billing blocks, burn rate |
| **Cursor** | `cursor` binary + local SQLite DBs | Plan spend & limits, per-model aggregation, Composer sessions |
| **GitHub Copilot** | `gh` CLI + Copilot extension | Chat & completions quota, org billing, session tracking |
| **Codex CLI** | `codex` binary + `~/.codex` | Session tokens, per-model breakdown, credits, rate limits |
| **Gemini CLI** | `gemini` binary + `~/.gemini` | OAuth status, conversation count, per-model tokens |
| **OpenCode** | `OPENCODE_API_KEY` / `ZEN_API_KEY` | Credits, activity, generation stats |
| **Ollama** | `OLLAMA_HOST` / binary | Local models, per-model usage |

#### API platforms

| Provider | Detection | What it tracks |
|---|---|---|
| **OpenAI** | `OPENAI_API_KEY` | Rate limits via header probing |
| **Anthropic** | `ANTHROPIC_API_KEY` | Rate limits via header probing |
| **OpenRouter** | `OPENROUTER_API_KEY` | Credits, activity, per-model breakdown |
| **Groq** | `GROQ_API_KEY` | Rate limits, daily usage windows |
| **Mistral AI** | `MISTRAL_API_KEY` | Subscription, usage endpoints |
| **DeepSeek** | `DEEPSEEK_API_KEY` | Rate limits, account balance |
| **Moonshot (Kimi)** | `MOONSHOT_API_KEY` | Balance breakdown (cash + voucher), org limits, tier; supports api.moonshot.ai (default) and api.moonshot.cn |
| **Perplexity** | Browser session at console.perplexity.ai | Tier, balance, lifetime spend, auto-reload, 30d usage analytics |
| **OpenCode (Zen + Console)** | `OPENCODE_API_KEY` / `ZEN_API_KEY` + browser session at opencode.ai | Zen models (API key) + balance, monthly limit/usage, subscription, payment method (cookie) |
| **xAI (Grok)** | `XAI_API_KEY` | Rate limits, API key info |
| **Z.AI Coding Plan** | `ZAI_API_KEY` / `ZHIPUAI_API_KEY` | Coding plan quotas, model/tool usage, daily trends |
| **Google Gemini API** | `GEMINI_API_KEY` / `GOOGLE_API_KEY` | Rate limits, model limits |
| **Alibaba Cloud** | `ALIBABA_CLOUD_API_KEY` | Quotas, credits, per-model tracking |

## Configuration

No config file needed — auto-detection handles everything. Override or extend via:

- macOS/Linux: `~/.config/openusage/settings.json`
- Windows: `%APPDATA%\openusage\settings.json`

```json
{
  "auto_detect": true,
  "ui": { "refresh_interval_seconds": 30 },
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

Full reference: [`configs/example_settings.json`](configs/example_settings.json)

### External themes

You can define custom themes as JSON files loaded at startup from:

- `~/.config/openusage/themes/*.json` (macOS/Linux)
- `%APPDATA%\\openusage\\themes\\*.json` (Windows)
- Any extra directory in `OPENUSAGE_THEME_DIR` (path-list separated)

Theme files use the same color token fields as built-ins. Browse the bundled examples for reference shapes — every shipped theme lives at [`internal/tui/bundled_themes/`](internal/tui/bundled_themes/).

## Daemon

Background data collection, even when the dashboard isn't open:

```bash
openusage telemetry daemon                # Run in foreground
openusage telemetry daemon install        # Install as system service (launchd / systemd)
openusage telemetry daemon status         # Check status
openusage telemetry daemon uninstall      # Uninstall
```

Installed services snapshot the provider env vars currently set in your shell.
If you change API key env vars later, rerun `openusage telemetry daemon install`
to refresh the service environment.

Manage tool integrations:

```bash
openusage integrations list [--all]       # List integration statuses
openusage integrations install <id>       # Install hook/plugin
openusage integrations uninstall <id>     # Remove
```

## Keybindings

| Key | Action |
|---|---|
| `Tab` | Switch views |
| `j` / `k`, `Up` / `Down` | Move cursor |
| `h` / `l`, `Left` / `Right` | Navigate panels |
| `Enter` / `Esc` | Open detail / back |
| `PgUp` / `PgDn` | Scroll tile |
| `[ ]` | Switch detail tabs |
| `r` | Refresh all |
| `/` | Filter providers |
| `t` | Cycle theme |
| `w` | Cycle time window |
| `c` | Cycle cost visibility for focused tile (auto → hide → show → auto, persists per-account) |
| `,` | Open settings |
| `Shift+J` / `Shift+K` | Reorder providers |
| `?` | Help |
| `q` | Quit |

## Development

```bash
make build    # Build binary to ./bin/openusage
make test     # Run tests with -race and coverage
make lint     # golangci-lint
make run      # go run cmd/openusage/main.go
make demo     # Preview with simulated data (no API keys needed)
```

Debug mode: `OPENUSAGE_DEBUG=1 openusage`

## License

[MIT](LICENSE)
