---
title: Tracking multiple accounts
description: How to monitor several accounts of the same provider — for example a personal and a work OpenAI key — side by side.
---

Most providers in OpenUsage support more than one account. The pattern is the same everywhere: give each account a unique `id` in `settings.json` and point `api_key_env` at a different environment variable.

## When you need it

Common scenarios:

- Personal vs work API keys for the same vendor.
- Two Cursor profiles (personal account, team account).
- A primary and a fallback OpenRouter key with different rate limits.
- Splitting team credit pools across distinct keys for attribution.

## Step 1: pick a unique env var per account

OpenUsage reads keys from your shell environment at fetch time, never from `settings.json`. So each account needs its own variable name. Conventionally:

```bash
# in ~/.zshrc / ~/.bashrc / direnv / 1Password CLI
export OPENAI_API_KEY="sk-...personal..."
export OPENAI_WORK_KEY="sk-...work..."
```

Both can coexist in the same shell.

## Step 2: declare the accounts

Edit `~/.config/openusage/settings.json` (`%APPDATA%\openusage\settings.json` on Windows):

```json
{
  "auto_detect": true,
  "accounts": [
    {
      "id": "openai-personal",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "probe_model": "gpt-4.1-mini"
    },
    {
      "id": "openai-work",
      "provider": "openai",
      "api_key_env": "OPENAI_WORK_KEY",
      "probe_model": "gpt-4.1-mini",
      "base_url": "https://corp-gateway.example.com/v1"
    }
  ]
}
```

Notes:

- The `id` is yours to invent; just keep it stable. It's used as the row key.
- `auto_detect` can stay on. Manual entries take precedence over detected ones, but other providers still get auto-detected.
- `base_url` is optional — useful when one of the accounts goes through a corporate gateway, an Azure endpoint, or a regional API.

## Step 3: relaunch the dashboard

```
$ openusage
```

Both accounts render as separate tiles. The status badge, gauges, time-window filter, and detail panel all apply per account.

## Per-provider gotchas

### OpenAI

`probe_model` defaults to `gpt-4.1-mini`. If your work key doesn't have access to that model, override per account.

### Anthropic

Supports `base_url` overrides for proxies or Bedrock front-ends.

### Cursor

The Cursor provider reads local SQLite databases, not env vars. To track multiple Cursor profiles you need to point each account at a different `tracking_db` and `state_db` path:

```json
{
  "accounts": [
    {
      "id": "cursor-personal",
      "provider": "cursor",
      "account_config": {
        "tracking_db": "/Users/me/Library/Application Support/Cursor/...tracking.db",
        "state_db":    "/Users/me/Library/Application Support/Cursor/...state.db"
      }
    },
    {
      "id": "cursor-team",
      "provider": "cursor",
      "account_config": {
        "tracking_db": "/Users/me/Library/Application Support/Cursor-Team/...tracking.db",
        "state_db":    "/Users/me/Library/Application Support/Cursor-Team/...state.db"
      }
    }
  ]
}
```

This is rarer, since the Cursor app itself only runs one profile at a time, but it's how you'd compare snapshots from different macOS user accounts.

### Claude Code

Same idea — `account_config.claude_dir` lets you point at a non-default Claude config directory.

### OpenRouter

If you have a management key that can list other keys, the provider can auto-discover them. For separate billing scopes use distinct keys with their own `id`.

## Switching the active "current" account

The TUI shows all configured accounts simultaneously; there is no concept of a single "current" account. You navigate with arrow keys / `j`/`k` and view a detail panel per row.

If you want the Analytics screen to focus on one account, use `/` to filter to its provider/id.

## Disabling without deleting

You can keep an account in `settings.json` and toggle it off in the dashboard:

1. Press `,` to open Settings.
2. Tab to the **Providers** sub-tab.
3. Highlight the row, press Space to disable.

The setting persists until you re-enable it.

## See also

- [Accounts](../concepts/accounts.md) — the AccountConfig model.
- [Auto-detection](../concepts/auto-detection.md) — how detected accounts merge with manual ones.
- [Cost attribution](cost-attribution.md) — splitting spend across accounts.
