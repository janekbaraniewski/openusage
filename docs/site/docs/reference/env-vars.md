---
title: Environment variables
description: Every environment variable OpenUsage reads, including per-provider API key envs.
---

# Environment variables

OpenUsage reads two kinds of environment variables: **runtime overrides** (debug, paths, sockets) and **API key envs** referenced from `accounts[].api_key_env`. Both are listed below.

## Runtime overrides

| Variable | Purpose |
|---|---|
| `OPENUSAGE_DEBUG` | When set to any non-empty value, enables verbose logging to stderr (theme loader, daemon connection, integration installer, hook plumbing). |
| `OPENUSAGE_BIN` | Override the binary path embedded in hook scripts. Useful when the binary lives at a non-standard location. |
| `OPENUSAGE_TELEMETRY_SOCKET` | Override the daemon Unix socket path. Equivalent to `--socket-path`, but inherited by every process (daemon, TUI, hooks). |
| `OPENUSAGE_GITHUB_TOKEN` | Token used for the in-app update check against GitHub. Optional; used to avoid anonymous rate limits. |
| `OPENUSAGE_THEME_DIR` | Colon-separated list (semicolon on Windows) of extra directories scanned for theme JSON files. See [External themes](../customization/external-themes.md). |
| `OPENUSAGE_MOONSHOT_STATE_PATH` | Override the path Moonshot's state file is read from. |
| `XDG_CONFIG_HOME` | Override the config base directory (default `~/.config`). |
| `XDG_STATE_HOME` | Override the state base directory (default `~/.local/state`). |
| `CLAUDE_SETTINGS_FILE` | Override the path to `~/.claude/settings.json`. Used by the `claude_code` provider and integration. |
| `CODEX_CONFIG_DIR` | Override the path to `~/.codex/`. Used by the `codex` provider and integration. |

## API key environment variables

Each provider's account references its key via `api_key_env` — the name of the variable, not its value. Below are the conventional names used in [`configs/example_settings.json`](https://github.com/loft-sh/openusage/blob/main/configs/example_settings.json). You may override these; just keep `api_key_env` in sync.

| Provider | Default env var |
|---|---|
| OpenAI | `OPENAI_API_KEY` |
| Anthropic | `ANTHROPIC_API_KEY` |
| OpenRouter | `OPENROUTER_API_KEY` |
| Groq | `GROQ_API_KEY` |
| Mistral | `MISTRAL_API_KEY` |
| DeepSeek | `DEEPSEEK_API_KEY` |
| Moonshot | `MOONSHOT_API_KEY` |
| xAI | `XAI_API_KEY` |
| Z.AI | `ZAI_API_KEY` |
| Gemini API | `GEMINI_API_KEY` (also detects `GOOGLE_API_KEY` as an alias) |
| Alibaba Cloud | `ALIBABA_CLOUD_API_KEY` |
| Ollama (cloud) | `OLLAMA_API_KEY` |

:::tip Adding a key without restarting
The TUI reads env vars on startup. After exporting a new key, press <kbd>q</kbd> to quit and re-launch — or use the API Keys settings tab (<kbd>,</kbd> then <kbd>5</kbd>) to enter the value at runtime, which writes it to your shell session for future processes only.
:::

## CLI tool / local file providers

Some providers don't use API keys; they read local files or shell out to a tool binary. Their `accounts` entries use `binary` rather than `api_key_env`.

| Provider | What it reads | Override |
|---|---|---|
| `claude_code` | `~/.claude.json, ~/.claude/stats-cache.json, ~/.claude/projects/**/*.jsonl, ~/.claude/settings.json` | `CLAUDE_SETTINGS_FILE`, plus `binary` field |
| `codex` | `~/.codex/sessions/*.jsonl` | `CODEX_CONFIG_DIR`, plus `binary` field |
| `cursor` | Local SQLite databases under `~/Library/Application Support/Cursor/` (or platform equivalent) | `binary` field |
| `gemini_cli` | Gemini CLI's session files | `binary` field (default `gemini`) |
| `copilot` | `gh copilot` subcommands | `binary` field (default `gh`) |
| `ollama` (local) | `http://127.0.0.1:11434` | `base_url` field |
| `opencode` | OpenCode session data | `binary` field |

## Setting variables

### Persistent

```bash
# zsh / bash
echo 'export OPENAI_API_KEY=sk-...' >> ~/.zshrc

# fish
set -Ux OPENAI_API_KEY sk-...
```

### Per-process

```bash
OPENUSAGE_DEBUG=1 OPENUSAGE_TELEMETRY_SOCKET=/tmp/ou.sock openusage telemetry daemon run
```

### In a service unit

For the daemon, set env vars via the launchd plist's `EnvironmentVariables` dictionary (macOS) or the systemd unit's `Environment=` lines (Linux). Reinstall via `openusage telemetry daemon install` after changing the unit if you want fresh defaults.

## See also

- [CLI reference](./cli.md) — flags equivalent to most env vars
- [Paths reference](./paths.md) — what each path-related variable controls
- [Configuration reference](./configuration.md) — `accounts[].api_key_env` schema
