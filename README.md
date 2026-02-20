# openusage

Terminal dashboard for AI tool usage and spend.

OpenUsage auto-detects local tools/API keys and shows live quota and cost snapshots in a Bubble Tea TUI.

![OpenUsage dashboard screenshot](./assets/dashboard.png)

Or side by side with your agent

![OpenUsage side by side](./assets/sidebyside.png)

## Install

```bash
brew install janekbaraniewski/tap/openusage
```

Or from source (Go 1.25+):

```bash
go install github.com/janekbaraniewski/openusage/cmd/openusage@latest
```

CGO is required (`CGO_ENABLED=1`) because the Cursor provider uses SQLite via `mattn/go-sqlite3`.

## Run

```bash
openusage
```

Auto-detection is enabled by default and picks up local tools plus common API key env vars.

## Config

You don't need to create config yourself, everything gets automatically detected. You can always overwrite/edit defaults.

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

Configured accounts are merged with auto-detected accounts, and configured ones win on conflicts.

## Supported Providers

OpenAI, Anthropic, OpenRouter, Groq, Mistral, DeepSeek, xAI, Gemini API, Gemini CLI, GitHub Copilot, Cursor, Claude Code, and OpenAI Codex CLI.

## Keybindings

- `Tab` / `Shift+Tab`: switch views
- `r`: refresh
- `/`: filter
- `t`: cycle theme
- `?`: help
- `q`: quit

## Development

```bash
make deps
make fmt
make vet
make lint
make test
make run
make build
```

## License

[MIT](LICENSE)
