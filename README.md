<p align="center">
  <img src="./assets/logo.gif" alt="OpenUsage logo">
</p>

<p align="center"><strong>OpenUsage.sh: terminal-first local quota and usage tracking for AI coding agents, IDEs, and LLM APIs.</strong></p>

<p align="center">
  <a href="#install">Install</a> &middot;
  <a href="#supported-providers">Providers (37)</a> &middot;
  <a href="#command-line-reports">CLI Reports</a> &middot;
  <a href="#tmux-integration">tmux</a> &middot;
  <a href="#claude-code-statusline">Statusline</a> &middot;
  <a href="#daemon--background-tracking">Daemon</a> &middot;
  <a href="#web-dashboard--hub">Web & Hub</a> &middot;
  <a href="#configuration">Config</a> &middot;
  <a href="#keybindings">Keybindings</a> &middot;
  <a href="#development">Development</a>
</p>

---

OpenUsage is the terminal-first local dashboard published at [openusage.sh](https://openusage.sh/). Publicly, the clearest brand reference is **OpenUsage.sh**. It auto-detects AI coding tools and API keys on your workstation and shows live quota, usage, spend, resets, rate limits, and model data in your terminal. It is built for mixed-tool workflows across Claude Code, Codex CLI, Cursor, Copilot, Gemini CLI, OpenRouter, OpenAI, Anthropic, and 37 total providers. Zero config required — just run `openusage`.

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

Requires CGO (`CGO_ENABLED=1`) for SQLite storage. Pre-built binaries are also available on the [Releases](https://github.com/janekbaraniewski/openusage/releases) page.

## Run

```bash
openusage
```

Auto-detection picks up local tools and common API key environment variables immediately without configuration.

## Features

- **Cross-provider tracking** — compare coding agents, API platforms, and local runtimes in one local dashboard
- **37 providers** — 24 coding agents & IDEs (Claude Code, Cursor, Copilot, Codex, Gemini CLI, Antigravity, OpenCode, Ollama, Amp, Goose, Hermes, Mux, Droid, Crush, Roo Code, Kilo Code, Kiro, Zed, Codebuff, Kimi CLI, OpenClaw, Pi, Qwen CLI, Command Code) and 13 API platforms (OpenAI, Anthropic, Azure OpenAI, Alibaba Cloud, OpenRouter, Perplexity, Groq, Mistral, Moonshot, DeepSeek, xAI, Z.AI, Gemini API)
- **Zero config** — auto-detects installed coding tools, shell configs, and environment variables
- **Live dashboard** — spend, quotas, rate limits, tokens, burn rate, and per-model breakdowns at a glance
- **tmux integration** — show the active tool's usage in your tmux status bar with 12 presets, custom formatting, and icon font support
- **Claude Code statusline** — one-line session cost, today's cost, burn rate, context usage, and 5-hour limit in Claude Code
- **Command-line reports** — `daily`, `weekly`, `monthly`, `session`, and `blocks` usage reports in table or JSON format
- **Background tracking** — continuous telemetry daemon collects events into a local SQLite database
- **Web dashboard & Hub** — local browser dashboard (`openusage serve`) and multi-machine aggregation server (`openusage hub`)
- **Model pricing lookup** — instant offline/online token pricing calculator (`openusage pricing`)
- **Export & metrics** — export snapshots to JSON or CSV (`openusage export`)
- **Customizable** — 17 built-in themes, custom external JSON themes, adjustable time windows, and threshold color rules

## Supported providers

OpenUsage ships with 37 provider integrations covering coding agents, IDEs, API platforms, and local tools. All providers are auto-detected when available.

### Coding agents & IDEs (24 providers)

| Provider | Detection | What it tracks |
|---|---|---|
| **Claude Code** | `claude` binary + `~/.claude` directory | Daily activity, per-model tokens, 5-hour billing block computation, burn rate, cost estimation |
| **Cursor** | `cursor` binary + local SQLite DBs (`state.vscdb`) | Plan spend and limits, per-model aggregation, Composer sessions, AI code scoring |
| **GitHub Copilot** | `gh` CLI with Copilot extension or `copilot` binary + `~/.copilot` | Chat and completions quota, org billing, session tracking |
| **Codex CLI** | `codex` binary + `~/.codex` directory | Session tokens, per-model and per-client breakdown, credits, rate limits |
| **Gemini CLI** | `gemini` binary + `~/.gemini` directory | OAuth status, conversation count, per-model tokens, quota API |
| **Antigravity CLI** | `agy` binary + `~/.gemini/antigravity-cli` (or `~/.agy-containers/<box>/...`) | Live quota buckets, session tokens, model quotas, OAuth token refresh |
| **OpenCode** | `OPENCODE_API_KEY` / `ZEN_API_KEY` or `~/.local/share/opencode/auth.json` | Credits, activity, generation stats, Zen models + balance, monthly limit, subscription |
| **Ollama** | `OLLAMA_HOST` env var or `ollama` binary | Local server models, per-model usage, optional cloud billing |
| **Amp** | `amp` binary + `~/.amp` directory | Session tokens, per-turn usage and costs |
| **Goose** | `goose` binary + `~/.config/goose` (or `~/.local/share/goose`) | Session tokens, model breakdowns, local logs |
| **Hermes Agent** | `hermes` binary + `~/.hermes` directory | Conversation logs, session tokens, cost estimation |
| **Mux** | `mux` binary + `~/.mux` directory | Multiplexed sessions, turn usage, token counts |
| **Droid** | `droid` binary + `~/.factory` directory | Droid sessions, model tokens, activity |
| **Crush** | `crush` binary + `~/.crush` directory | Charm Crush agent sessions, per-model token usage |
| **Roo Code** | Roo Code VS Code extension storage | Session tokens, model costs, workspace activity |
| **Kilo Code** | `~/.kilocode` directory or storage | Kilo Code sessions, token usage, cost tracking |
| **Kiro** | `kiro` binary + `~/.kiro` directory | Kiro sessions, model usage, turn metrics |
| **Zed** | `zed` binary + `~/.config/zed` directory | Zed assistant/agent session tokens, model usage |
| **Codebuff** | `codebuff` binary + `~/.codebuff` directory | Codebuff generation tokens, sessions, cost estimation |
| **Kimi CLI** | `kimi` binary + `~/.kimi` directory | Kimi CLI session tokens, model breakdown |
| **OpenClaw** | `openclaw` binary + `~/.openclaw` directory | OpenClaw session tokens, workspace metrics |
| **Pi** | `pi` binary + `~/.pi` directory | Pi coding agent session tokens, turn metrics |
| **Qwen CLI** | `qwen` binary + `~/.qwen` directory | Qwen CLI session tokens, model usage |
| **Command Code** | `command-code` binary + `~/.command-code` directory | Command Code session logs, token usage, cost tracking |

### API platforms (13 providers)

| Provider | Detection | What it tracks |
|---|---|---|
| **OpenAI** | `OPENAI_API_KEY` environment variable | Rate limits via lightweight header probing |
| **Anthropic** | `ANTHROPIC_API_KEY` environment variable | Rate limits via lightweight header probing |
| **Azure OpenAI** | `AZURE_OPENAI_API_KEY` or `AZURE_API_KEY` + `AZURE_RESOURCE_NAME` (or `AZURE_OPENAI_ENDPOINT`) | Rate limits via lightweight header probing on the resource endpoint |
| **Alibaba Cloud** | `ALIBABA_CLOUD_API_KEY` or `DASHSCOPE_API_KEY` | Quotas, credits, daily usage, per-model tracking (DashScope) |
| **OpenRouter** | `OPENROUTER_API_KEY` environment variable | Credits, activity, generation stats, per-model breakdown across endpoints |
| **Perplexity** | Browser session at `console.perplexity.ai` | Tier (0–5), available balance, lifetime spend, auto-reload settings, payment method, 30-day analytics |
| **Groq** | `GROQ_API_KEY` environment variable | Rate limits, daily usage windows |
| **Mistral AI** | `MISTRAL_API_KEY` environment variable | Subscription info, usage endpoints |
| **Moonshot (Kimi)** | `MOONSHOT_API_KEY` environment variable | Balance breakdown (cash + voucher), org limits, tier; supports `api.moonshot.ai` (default) and `api.moonshot.cn` |
| **DeepSeek** | `DEEPSEEK_API_KEY` environment variable | Rate limits, account balance |
| **xAI (Grok)** | `XAI_API_KEY` environment variable | Rate limits, API key info |
| **Z.AI Coding Plan** | `ZAI_API_KEY` / `ZHIPUAI_API_KEY` or `~/.chelper/config.yaml` | Coding plan quotas, model/tool usage, daily trends, credit balance |
| **Google Gemini API** | `GEMINI_API_KEY` or `GOOGLE_API_KEY` | Rate limits, per-model limits |

### Browser-session auth (universal mechanism)

For providers whose billing, usage, and account data is gated behind web-console session cookies rather than standard API keys, OpenUsage supports a "connect via browser" flow that reads the session cookie directly out of your local browser's cookie jar (Chrome, Firefox, Safari, Edge, Brave on macOS, Linux, and Windows).

- **How to connect**: Launch `openusage`, open Settings (`,`), navigate to **5 KEYS**, highlight the provider row, and press **Enter** (for cookie-only providers like Perplexity) or press **c** (for mixed-auth providers like OpenCode). OpenUsage opens a browser picker, reads the `(domain, cookie name)` pair declared by the provider, and securely stores the cookie in `credentials.json` with `0600` permissions.
- **Auto-refresh**: When the cookie expires, the dashboard tile displays an `AUTH` status with a re-login hint. Logging into the site again in your browser refreshes OpenUsage automatically on the next poll.
- **Privacy & Security**: Opt-in per account, scoped strictly to the declared `(domain, cookie name)` pair, and never transmitted off-machine. macOS prompts for Keychain access the first time OpenUsage reads Chrome's encrypted cookie store.

**Shipping implementations**:
- **Perplexity** (`console.perplexity.ai`): Tier (0–5), available balance, lifetime spend, auto-reload settings, payment method, and 30-day analytics (requests, input/output/reasoning tokens, search queries).
- **OpenCode Console** (`opencode.ai/_server`): Balance, monthly limit / monthly usage, auto-reload settings, payment method, and subscription state.

### OpenCode credential adoption

If [OpenCode](https://opencode.ai) is installed and you have configured providers inside it, OpenUsage automatically reads `~/.local/share/opencode/auth.json` on startup and adopts any API keys it finds:

| OpenCode entry | OpenUsage account | Target provider |
|---|---|---|
| `moonshotai` (api) | `moonshot-ai` | `moonshot` |
| `openrouter` (api) | `openrouter` | `openrouter` |
| `zai` (api) | `zai` | `zai` |
| `opencode` (api) | `opencode` | `opencode` |
| `opencode-go` (api) | `opencode` | `opencode` |
| `ollama-cloud` (api) | `ollama-cloud` | `ollama` |

*Note*: OAuth-typed entries (`anthropic`, `openai`, `google`, `cursor`) are skipped because they contain chat-scoped tokens rather than API keys. Process environment variables take precedence over adopted keys.

## Command-line reports

Besides the interactive TUI, OpenUsage provides headless subcommands that leverage the same parsing and pricing engine for scripting, CI pipelines, and terminal summaries:

```bash
openusage daily                # Usage & cost aggregated by day
openusage weekly               # Usage & cost aggregated by week
openusage monthly              # Usage & cost aggregated by month
openusage session              # Grouped by Claude Code / agent session
openusage blocks               # By 5-hour billing block with burn rate & projection
openusage daily --json         # Structured JSON output for CI / automation
```

### Provider report coverage

| Report | Providers supported |
|---|---|
| `daily` / `weekly` / `monthly` | All 37 providers that report tokens, cost series, or snapshot metrics |
| `session` / `blocks` | Claude Code, Codex CLI, Gemini CLI, GitHub Copilot, Cursor, OpenCode, Ollama, Amp, Codebuff, OpenClaw, Roo Code, Kilo Code, Crush, Goose, Hermes, Zed, Droid, Kiro |
| `statusline` | Claude Code |

### Report flags

| Flag | Description |
|---|---|
| `--json` | Emit structured JSON instead of formatted ANSI table |
| `--since YYYY-MM-DD` | Filter events on or after date |
| `--until YYYY-MM-DD` | Filter events on or before date |
| `-b`, `--breakdown` | Display per-model sub-rows under each periodic aggregate |
| `--provider <id>` | Limit report to a single provider ID (e.g. `claude_code`, `cursor`) |
| `--project <label>` | Filter by workspace or project label |
| `--mode <mode>` | Claude Code cost mode: `calculate` (recompute from tokens), `display` (trust logged cost), or `auto` |
| `--offline` | Skip network pricing lookups; use embedded token rates |
| `--top-models <n>` | Cap the number of models shown in breakdown rows (0 = all) |
| `--source <source>` | Snapshot source for non-itemized providers: `auto` (default), `direct`, or `daemon` |
| `--week-start <day>` | Week boundary for `weekly`: `monday` (default) or `sunday` |

## tmux integration

Show your live AI tool usage — active provider logo, cost, quota, burn rate, and context window — directly in your **tmux status bar**.

<table>
<tr>
<td width="55%" valign="middle">

OpenUsage auto-detects whichever tool you are actively using and renders its brand-colored logo, active billing block, burn rate ($/hr), and today's spend.

</td>
<td width="45%" valign="middle">

![OpenUsage in the tmux status bar](./assets/tmux-ccode.png)

</td>
</tr>
</table>

### Setup

```bash
openusage tmux install                         # Interactive setup wizard
tmux source-file ~/.config/tmux/tmux.conf      # Reload tmux configuration
```

<p align="center">
  <img src="./assets/install-tmux.gif" alt="Installing the OpenUsage tmux status segment" width="720">
</p>

For scriptable or automated installations:

```bash
openusage tmux install --write                 # Write directly to tmux.conf (creates .bak backup)
openusage tmux install --write --preset burn  # Install with a specific preset
```

**Install flags**:
- `--write`: Apply configuration directly to `tmux.conf`.
- `--position <left|right|both>`: Position in tmux status bar (default `right`).
- `--preset <name>`: Embedded preset name (default `compact`).
- `--interval <sec>`: Update frequency in seconds (default `5`).
- `--right-length <n>` / `--left-length <n>`: tmux status length allowance.
- `--bind-popup <key>`: Bind key to open full OpenUsage dashboard in a floating popup (tmux 3.2+).
- `--bind-refresh <key>`: Bind key to force an instant status refresh.
- `--with-font` / `--no-font`: Install or skip the bundled provider-icon font non-interactively.

### Built-in presets (12 available)

Preview and choose among 12 built-in presets (`openusage tmux presets`):

| Preset | Glyphs | Sample output |
|---|---|---|
| `compact` | unicode | `🤖 5h 15% $6.79/today` |
| `claude-focused` | unicode | `🤖 Opus 4.7 $3.40 block (2h17m) 🔥 $1.20/hr 🧠 42%` |
| `burn` | unicode | `🔥 $1.20/hr → $9.40 EOB` |
| `ascii-safe` | ascii | `[CLAUDE_CODE] $4.21 block:47% burn:$1.20/hr ctx:42%` |
| `cost-only` | ascii | `$4.21` |
| `minimal` | ascii | `claude_code $4.21` |
| `emoji-rich` | unicode | `🤖 CLAUDE_CODE \| 💰 $4.21 \| 📅 42 req \| 🔥 $1.20/hr \| 🧠 42%` |
| `verbose` | unicode | `🤖 Opus 4.7 \| 💰 $4.21 today / $3.40 block \| 🔥 $1.20/hr \| 🧠 84k (42%)` |
| `themed` | unicode | `🤖 $4.21 $1.20/hr` |
| `multi-tool` | unicode | `claude_code \| cursor \| codex` |
| `nerdfont` | nerdfont | `claude_code  $4.21  $1.20` |
| `powerline` | nerdfont | `🤖  $4.21  $1.20/hr` |

### Custom format templates

Pass a custom template string with `--format`:

```bash
openusage tmux --format '{tool:icon} {tool:brand} #[fg=$subtext]{today_cost:money}#[default] {burn_rate:money}/hr'
```

#### Template grammar
- `{variable}`: Evaluates variable from context.
- `{variable:modifier[:arg...]}`: Evaluates variable and applies chained formatting modifiers.
- `{?condition:then_clause:else_clause}`: Conditional branch based on variable presence/truthiness.
- `#[fg=$color]`, `#[bg=$color]`, `#[default]`: Named theme colors expanded into tmux styling directives.
- `#[...]`: Direct tmux attribute passthrough.

#### Available variables (`openusage tmux variables`)

| Variable | Kind | Description |
|---|---|---|
| `{tool}` / `{provider}` | attribute | Active provider ID (e.g. `claude_code`, `cursor`) |
| `{account}` | attribute | Active account identifier |
| `{model}` | attribute | Current model identifier (e.g. `claude-3-7-sonnet`) |
| `{cost}` | segment | Formatted total cost segment |
| `{daily}` | segment | Formatted today's spend segment |
| `{block}` | segment | Formatted active 5h billing block cost and duration |
| `{burn}` | segment | Formatted burn rate segment |
| `{context}` | segment | Formatted context window token and usage % segment |
| `{tokens}` | segment | Formatted token counts segment |
| `{active_tools}` | segment | List of active tools detected in recent window |
| `{today_cost}` | semantic alias | Numeric today's cost |
| `{block_cost}` | semantic alias | Active billing block cost |
| `{burn_rate}` | semantic alias | Current burn rate in USD/hour |
| `{block_pct}` | semantic alias | Percentage of block duration consumed |
| `{block_remaining}` | semantic alias | Duration left in active billing block (e.g. `2h17m`) |
| `{block_projection}` | semantic alias | Projected end-of-block cost |
| `{context_pct}` | semantic alias | Context window utilization percentage |
| `{context_tokens}` | semantic alias | Context tokens used in current turn |
| `{plan_pct}` | semantic alias | Plan quota percentage used |
| `{requests_today}` | semantic alias | Request count today |
| `{today_input_tokens}` | semantic alias | Input tokens consumed today |
| `{today_output_tokens}` | semantic alias | Output tokens consumed today |
| `{tool_color}` | semantic alias | Provider brand hex color |

#### Modifiers
- `:short`: Compact representation (`$4.20`).
- `:long`: Descriptive representation (`$4.20 today`, `$1.20/hr`).
- `:money[n]`: Currency formatting with precision `n` (default 2, e.g. `:money:4` -> `$4.2000`).
- `:pct[n]`: Percentage with `n` decimal places (default 0).
- `:bar[width]`: Progress bar of specified cell width using active glyph tier.
- `:color`: Apply threshold color styling (`green` -> `yellow` -> `red`).
- `:brand`: Tint value with provider's brand color.
- `:icon`: Provider logo glyph for active tier.
- `:tokens`: Compact metric token counts (`120k`, `1.2M`).
- `:duration`: Format seconds or duration strings (`2h15m`).
- `:upper` / `:lower`: Text casing.
- `:trunc[n]`: Truncate string to length `n` with ellipsis.
- `:pad[n[,l|r]]`: Pad string to width `n` (left or right).
- `:default[val]`: Fallback string when variable is empty.

### Provider icon font

OpenUsage includes a custom font containing sharp, brand-colored vector glyphs for AI tools.

```bash
openusage tmux font setup      # Auto-configure Kitty, Ghostty, WezTerm (per-range fallback)
openusage tmux font install    # Install TTF into user font directory
openusage tmux font status     # Check font installation status
openusage tmux font patch      # Augment terminal font for iTerm2 / Terminal.app
openusage tmux font uninstall  # Remove font
```

### tmux diagnostics & watch alerts

```bash
openusage tmux doctor          # Diagnose tmux integration, font, and daemon health
openusage tmux preview         # Render live ANSI terminal preview of active status segment
openusage tmux watch           # Monitor usage thresholds and trigger tmux alerts
```

To remove the tmux integration:
```bash
openusage tmux uninstall
```

## Claude Code statusline

Display session cost, today's cost, active 5-hour billing block, burn rate, and context window utilization directly in the **Claude Code status bar**:

![OpenUsage statusline in Claude Code](./assets/claudecodestatus.png)

### Setup

```bash
openusage statusline install
```

<p align="center">
  <img src="./assets/statusline-install.gif" alt="Installing the Claude Code statusline" width="720">
</p>

On an interactive terminal, `openusage statusline install` opens a live-preview configurator where you can toggle individual segments. For scripted installation, pass options via flags:

```bash
openusage statusline install --segments model,session,today,burn,context
openusage statusline install --offline=true --color=true
```

### Statusline segments

| Segment | Default | Description |
|---|---|---|
| `model` | enabled | Active model identifier (e.g. `🤖 Claude 3.7 Sonnet`) |
| `session` | enabled | Cost of current conversation session |
| `today` | enabled | Total spend across all sessions today |
| `block` | enabled | Current 5-hour billing block spend and time remaining |
| `burn` | enabled | Active burn rate in $/hr |
| `window5h` | enabled | 5-hour rate-limit window consumption % |
| `context` | enabled | Context window memory size and percentage (e.g. `🧠 84k (42%)`) |

### Manual configuration

Add the statusline directly into `~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "openusage statusline",
    "padding": 0
  }
}
```

To uninstall:
```bash
openusage statusline uninstall
```

## Daemon & background tracking

OpenUsage runs an optional lightweight background daemon to collect provider snapshots and telemetry events continuously into a local SQLite database, ensuring zero data gaps even when the dashboard is closed.

```bash
openusage telemetry daemon run             # Run daemon in foreground
openusage telemetry daemon install         # Install system service (launchd on macOS, systemd on Linux)
openusage telemetry daemon status          # Check daemon process and socket status
openusage telemetry daemon uninstall       # Remove system service
```

### Event hooks & tool integrations

Inject turn and session events directly from tools:

```bash
openusage telemetry hook <source> [payload]
```

Manage tool integrations (Claude Code, Codex, OpenCode plugins):

```bash
openusage integrations list [--all]       # List available tool integration hooks
openusage integrations install <id>       # Install integration hook/plugin
openusage integrations upgrade [id]       # Upgrade integration to latest version
openusage integrations uninstall <id>     # Uninstall integration hook
```

## Web dashboard & Hub

### Local Web dashboard (`openusage serve`)

Serve an interactive browser-based dashboard on your local network:

```bash
openusage serve                            # Serves on http://127.0.0.1:8080 and opens browser
openusage serve --listen 127.0.0.1:9090     # Custom address
openusage serve --demo                     # Preview with synthetic snapshots
OPENUSAGE_SERVE_TOKEN=s3cret openusage serve --listen :8080 # Require Bearer token for external access
```

### Multi-machine aggregator Hub (`openusage hub`)

Aggregate usage across multiple developer machines or CI workers into a centralized dashboard:

```bash
openusage hub                              # Run Hub server with TUI on 127.0.0.1:9190
openusage hub --headless                   # Run headless aggregator in Docker / server
OPENUSAGE_HUB_TOKEN=s3cret openusage hub --headless # Run with Bearer token authentication
openusage hub-view http://hub-host:9190    # View remote hub snapshots in local TUI
```

### Snapshot export (`openusage export`)

Export snapshot data for analysis or archival:

```bash
openusage export --output ~/usage.json                # JSON snapshot envelope
openusage export --output /tmp/usage.csv --format csv # CSV format
openusage export --output - --source direct           # Direct provider poll to stdout
```

### Model pricing lookup (`openusage pricing`)

Query per-million token rates from LiteLLM and OpenRouter public indexes:

```bash
openusage pricing claude-3-7-sonnet
openusage pricing gpt-4o --context 250000
openusage pricing gemini-2.0-flash --json
```

### Auto-detection check (`openusage detect`)

Inspect all AI tools and API keys detected on the workstation:

```bash
openusage detect
```

## Configuration

No config file is required — auto-detection handles accounts automatically. To customize behaviors, create or edit:

- macOS / Linux: `~/.config/openusage/settings.json`
- Windows: `%APPDATA%\openusage\settings.json`

```json
{
  "auto_detect": true,
  "theme": "default",
  "ui": {
    "refresh_interval_seconds": 30,
    "warn_threshold": 80.0,
    "crit_threshold": 95.0
  },
  "data": {
    "time_window": "today"
  },
  "accounts": [
    {
      "id": "openai-work",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "probe_model": "gpt-4o-mini"
    }
  ],
  "tmux": {
    "preset": "compact",
    "color_mode": "truecolor"
  }
}
```

Full configuration schema reference: [`configs/example_settings.json`](configs/example_settings.json).

### Custom external themes

Create custom JSON themes at:
- `~/.config/openusage/themes/<name>.json` (macOS/Linux)
- `%APPDATA%\openusage\themes\<name>.json` (Windows)
- Any path in `OPENUSAGE_THEME_DIR`

Browse bundled themes for color references in [`internal/tui/bundled_themes/`](internal/tui/bundled_themes/).

## Keybindings

| Key | Action |
|---|---|
| `Tab` | Switch between Dashboard and Analytics views |
| `j` / `k`, `Up` / `Down` | Move cursor up / down |
| `h` / `l`, `Left` / `Right` | Navigate between panels |
| `Enter` / `Esc` | Open tile detail / return back |
| `PgUp` / `PgDn` | Scroll tile contents |
| `[ ]` | Switch tabs in detail view |
| `r` | Refresh all providers |
| `/` | Filter providers list |
| `t` | Cycle dashboard color themes |
| `w` | Cycle time window (today, 7d, 30d, all) |
| `c` | Cycle cost visibility (auto → hide → show → auto) |
| `,` | Open settings modal (manage keys, cookie auth, layout) |
| `Shift+J` / `Shift+K` | Reorder provider tiles |
| `?` | Toggle help overlay |
| `q` | Quit |

## Development

```bash
make build          # Build binary to ./bin/openusage
make test           # Run all unit and race tests
make lint           # Run golangci-lint
make vet            # Run go vet
make fmt            # Format Go code
make run            # Run dashboard directly
make demo           # Run demo binary with simulated data
```

Enable verbose debug logging: `OPENUSAGE_DEBUG=1 openusage`

## License

[MIT](LICENSE)
