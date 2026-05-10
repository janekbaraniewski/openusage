---
title: Accounts
description: The AccountConfig model, how api_key_env points to a variable name not a value, and how to track multiple accounts of the same provider.
---

An **account** in OpenUsage is a configured instance of a provider. One provider can have many accounts (a personal OpenAI key and a work OpenAI key, two Cursor profiles, etc). Accounts are the granularity at which snapshots, gauges, and detail panels render.

## The AccountConfig model

Internally each account is represented by an `AccountConfig`. The persisted JSON form lives under `accounts` in `~/.config/openusage/settings.json`:

```json
{
  "id": "openai-work",
  "provider": "openai",
  "api_key_env": "OPENAI_WORK_KEY",
  "base_url": "https://api.openai.com/v1",
  "probe_model": "gpt-4.1-mini"
}
```

Common fields:

| Field | Purpose |
|---|---|
| `id` | Stable, unique identifier inside this config. Used as the row key and in URLs. |
| `provider` | Provider ID (e.g. `openai`, `claude_code`). |
| `api_key_env` | Name of the env var that holds the secret. **Not the secret itself.** |
| `base_url` | Optional API base override (proxy, EU endpoint, custom gateway). |
| `probe_model` | For header-probe providers, which model to ping. |
| `binary` | For local-tool providers, path to the CLI binary. Reused for some non-API metadata. |
| `account_config` | Optional sub-map for provider-specific knobs. |

:::note
`AccountConfig.Binary` and `AccountConfig.BaseURL` are reused by some local providers as generic string slots. For `claude_code` for example, `binary` may carry a directory path. Check the per-provider page for what each field means.
:::

## Why `api_key_env` is just a name

A common point of confusion: `api_key_env` does not contain the API key. It contains the **name of the environment variable** that holds the API key. OpenUsage reads the value from your shell environment at fetch time and never writes it back to disk.

This means:

- The settings file is safe to commit to a private dotfiles repo (no secrets inside).
- Rotating a key is just rotating the env var.
- Two accounts of the same provider can use different env vars and run side-by-side.

The runtime field that does carry the resolved secret (`AccountConfig.Token`) has `json:"-"` so it cannot be persisted.

## Multiple accounts per provider

Give each account a unique `id` and pick a different env var:

```json
{
  "accounts": [
    {
      "id": "openai-personal",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY"
    },
    {
      "id": "openai-work",
      "provider": "openai",
      "api_key_env": "OPENAI_WORK_KEY",
      "base_url": "https://corp-gateway.example.com/v1"
    }
  ]
}
```

Both render as separate tiles. Snapshots, alerts, and time-window filters apply per account.

For a complete walk-through see [guides/multi-account](../guides/multi-account.md).

## Detected vs configured

Auto-detection produces `AccountConfig` records too. The merge rules are:

- Manual entries always win over detected ones with the same `(provider, id)`.
- Detected entries that do not conflict are appended.
- Setting `auto_detect: false` at the top of `settings.json` disables detection entirely; only the manual list is used.

## Account-level overrides

A few things can be tuned per account rather than globally:

| Override | Where |
|---|---|
| API base URL | `base_url` |
| Probe model | `probe_model` |
| Local config dir | provider-specific (often `account_config.config_dir`) |
| Binary path | `binary` |
| Display name | `display_name` (in some providers) |

Settings the TUI manages globally (poll interval, theme, time window, gauge thresholds) live elsewhere in `settings.json` and apply to all accounts.

## Removing or disabling an account

- Delete the entry from `accounts` and restart `openusage`. If detection still reproduces it, also unset the env var or set `auto_detect: false`.
- Disable an account temporarily from the dashboard: open Settings (`,`), Providers tab, Space toggles enabled state.
