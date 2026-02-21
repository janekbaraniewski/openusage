<p align="center">
  <img src="./assets/logo.gif" alt="logo">
</p>


<p align="center">The coding agent usage dashboard you’ve been looking for.</p>


OpenUsage auto-detects local tools/API keys and shows live quota and cost snapshots in your terminal.

![OpenUsage dashboard screenshot](./assets/dashboard.png)

Or side by side with your agent

![OpenUsage side by side](./assets/sidebyside.png)

## Install

```bash
brew install janekbaraniewski/tap/openusage
```

Pre-built binaries for macOS, Linux, and Windows are available on the [GitHub Releases](https://github.com/janekbaraniewski/openusage/releases) page.

Or from source (Go 1.25+):

```bash
go install github.com/janekbaraniewski/openusage/cmd/openusage@latest
```

Building from source requires CGO (`CGO_ENABLED=1`) because the Cursor provider uses SQLite via `mattn/go-sqlite3`.

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

| Provider | Detection | Tested |
|---|---|:---:|
| Claude Code CLI | `claude` binary + `~/.claude` | ✅ |
| Cursor IDE | `cursor` binary + local SQLite DBs | ✅ |
| GitHub Copilot | `gh` CLI + Copilot extension | ✅ |
| Gemini CLI | `gemini` binary + `~/.gemini` | ✅ |
| OpenRouter | `OPENROUTER_API_KEY` | ✅ |
| OpenAI Codex CLI | `codex` binary + `~/.codex` | ✅ |
| OpenAI | `OPENAI_API_KEY` | |
| Anthropic | `ANTHROPIC_API_KEY` | |
| Groq | `GROQ_API_KEY` | |
| Mistral AI | `MISTRAL_API_KEY` | |
| DeepSeek | `DEEPSEEK_API_KEY` | |
| xAI (Grok) | `XAI_API_KEY` | |
| OpenCode Zen | `ZEN_API_KEY` / `OPENCODE_API_KEY` | |
| Google Gemini API | `GEMINI_API_KEY` / `GOOGLE_API_KEY` | |

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
