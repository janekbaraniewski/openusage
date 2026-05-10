---
title: Configuration reference
description: Every field in OpenUsage's settings.json schema with type, default, and example values.
---

# Configuration reference

OpenUsage stores its configuration in a single JSON file at:

- macOS / Linux — `~/.config/openusage/settings.json`
- Windows — `%APPDATA%\openusage\settings.json`

The TUI reads the file on startup and writes it back when you change settings interactively. You can also edit the file directly — changes take effect on the next refresh (<kbd>r</kbd>) or restart.

## Top-level keys

| Key | Type | Purpose |
|---|---|---|
| [`auto_detect`](#auto_detect) | bool | Toggle automatic detection of installed tools and API keys. |
| [`theme`](#theme) | string | Name of the active theme. |
| [`ui`](#ui) | object | Refresh interval and gauge thresholds. |
| [`data`](#data) | object | Time window default and retention. |
| [`telemetry`](#telemetry) | object | Daemon-related settings. |
| [`dashboard`](#dashboard) | object | Provider list, view, and widget sections. |
| [`experimental`](#experimental) | object | Opt-in screens. |
| [`model_normalization`](#model_normalization) | object | Group raw model ids by canonical lineage. |
| [`integrations`](#integrations) | object | Install state for tool hooks. |
| [`accounts`](#accounts) | array | Manually configured provider accounts. |
| [`auto_detected_accounts`](#auto_detected_accounts) | array | Read-only mirror of accounts found by the detector. |

## `auto_detect`

Whether to auto-detect installed AI tools (Cursor, Claude Code, Codex, Copilot, Gemini CLI, Aider, Ollama) and API keys from the environment.

```json
{ "auto_detect": true }
```

Default: `true`. When `false`, only `accounts` is used.

## `theme`

The active theme by name. Must match a built-in or external theme. See [Themes](../customization/themes.md).

```json
{ "theme": "Tokyo Night" }
```

Default: `"Gruvbox"`.

## `ui`

```json
{
  "ui": {
    "refresh_interval_seconds": 30,
    "warn_threshold": 0.20,
    "crit_threshold": 0.05
  }
}
```

| Field | Type | Default | Purpose |
|---|---|---|---|
| `refresh_interval_seconds` | int | `30` | How often the TUI re-fetches the read model from the daemon. |
| `warn_threshold` | float | `0.20` | Gauge turns yellow when remaining ratio drops below this. |
| `crit_threshold` | float | `0.05` | Gauge turns red below this. |

Thresholds are remaining-ratio fractions, so `0.20` means "yellow when less than 20% remains."

## `data`

```json
{
  "data": {
    "time_window": "30d",
    "retention_days": 30
  }
}
```

| Field | Type | Default | Purpose |
|---|---|---|---|
| `time_window` | string | `"30d"` | Default time window. One of `1d`, `3d`, `7d`, `30d`, `all`. |
| `retention_days` | int | `30` | Days of history to keep in the daemon's SQLite store. Older rows are pruned. Hard-capped at **90** — values above 90 are silently clamped at startup. |

## `telemetry`

```json
{
  "telemetry": {
    "provider_links": {
      "anthropic": "claude_code",
      "google": "gemini_api",
      "github-copilot": "copilot"
    }
  }
}
```

| Field | Type | Purpose |
|---|---|---|
| `provider_links` | `map<string,string>` | Map telemetry source strings to display provider IDs. Defaults shown above. |

Edit interactively via the Telemetry settings tab (<kbd>,</kbd> then <kbd>6</kbd>).

## `dashboard`

```json
{
  "dashboard": {
    "view": "grid",
    "hide_sections_with_no_data": false,
    "providers": [
      { "account_id": "openai-personal", "enabled": true },
      { "account_id": "anthropic-work",  "enabled": true }
    ],
    "widget_sections": [
      { "id": "top_usage_progress", "enabled": true },
      { "id": "model_burn",         "enabled": true }
    ]
  }
}
```

### `dashboard.view`

| Value | Layout |
|---|---|
| `grid` | Default — adaptive multi-column grid. |
| `stacked` | Single full-width column. |
| `tabs` | Focused pane plus a tab strip. |
| `split` | Tile list left / detail right. |
| `compare` | Two adjacent provider panes. |

A viewport too narrow for the chosen view is auto-fallen-back to `stacked`.

### `dashboard.providers`

Ordered list of accounts to render in the dashboard. Order in the array is the display order.

| Field | Type | Purpose |
|---|---|---|
| `account_id` | string | Must match an `id` from `accounts` or `auto_detected_accounts`. |
| `enabled` | bool | Show the tile or hide it. |

### `dashboard.hide_sections_with_no_data`

| Type | Default | Purpose |
|---|---|---|
| bool | `false` | When `true`, any widget section that produces no rows for the active provider is hidden instead of rendered as an empty card. |

### `dashboard.widget_sections`

Ordered list of widget sections shown on dashboard tiles. See [Widgets](../customization/widgets.md).

| Field | Type | Purpose |
|---|---|---|
| `id` | string | Section ID (provider-defined). |
| `enabled` | bool | Render or hide globally. |

### `dashboard.detail_sections`

Same shape as `widget_sections`, but applied to the detail (full-page) view rather than the tile view. Use this to control which widget sections appear when you press <kbd>Enter</kbd> on a tile.

| Field | Type | Purpose |
|---|---|---|
| `id` | string | Section ID (provider-defined). |
| `enabled` | bool | Render or hide on the detail view. |

## `experimental`

```json
{
  "experimental": {
    "analytics": true
  }
}
```

| Field | Type | Default | Purpose |
|---|---|---|---|
| `analytics` | bool | `false` | Enables the Analytics screen (<kbd>Tab</kbd> from dashboard). |

## `model_normalization`

Groups raw model strings (`gpt-4o-2024-08-06`, `gpt-4o`, `chatgpt-4o-latest`) under a single canonical lineage so charts and breakdowns aggregate cleanly.

```json
{
  "model_normalization": {
    "enabled": true,
    "group_by": "lineage",
    "min_confidence": 0.80,
    "overrides": [
      {
        "provider": "cursor",
        "raw_model_id": "claude-4.6-opus-high-thinking",
        "canonical_lineage_id": "anthropic/claude-opus-4.6"
      }
    ]
  }
}
```

| Field | Type | Default | Purpose |
|---|---|---|---|
| `enabled` | bool | `true` | Master switch. |
| `group_by` | string | `"lineage"` | Currently only `lineage` is supported. |
| `min_confidence` | float | `0.80` | Heuristic confidence threshold for automatic grouping. |
| `overrides` | array | `[]` | Manual mappings that bypass the heuristic. |

Each override:

| Field | Purpose |
|---|---|
| `provider` | Provider id the raw model belongs to. |
| `raw_model_id` | Raw string from the provider's API. |
| `canonical_lineage_id` | Canonical lineage to map it to (e.g. `anthropic/claude-opus-4.6`). |

## `integrations`

Install state for tool hook integrations. Managed by `openusage integrations` — usually you don't edit this by hand.

```json
{
  "integrations": {
    "claude_code": {
      "installed": true,
      "version": "1.0.0",
      "installed_at": "2025-01-15T10:30:00Z"
    },
    "cursor-rules": {
      "installed": false,
      "declined": true
    }
  }
}
```

| Field | Type | Purpose |
|---|---|---|
| `installed` | bool | True when the integration is currently active. |
| `version` | string | Version of the installed template. |
| `installed_at` | RFC3339 | Timestamp of last install. |
| `declined` | bool | If true, the install prompt is suppressed. |

## `accounts`

Manually configured provider accounts. Account `id` must be unique across `accounts` and `auto_detected_accounts`.

```json
{
  "accounts": [
    {
      "id": "openai-personal",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "probe_model": "gpt-4.1-mini"
    },
    {
      "id": "anthropic-work",
      "provider": "anthropic",
      "api_key_env": "ANTHROPIC_API_KEY"
    },
    {
      "id": "moonshot-cn",
      "provider": "moonshot",
      "api_key_env": "MOONSHOT_API_KEY",
      "base_url": "https://api.moonshot.cn"
    },
    {
      "id": "ollama-cloud",
      "provider": "ollama",
      "auth": "api_key",
      "base_url": "https://ollama.com",
      "api_key_env": "OLLAMA_API_KEY"
    },
    {
      "id": "copilot",
      "provider": "copilot",
      "binary": "gh"
    }
  ]
}
```

### Account fields

| Field | Type | Purpose |
|---|---|---|
| `id` | string | Stable unique identifier. Used in `dashboard.providers` and account-id tags. |
| `provider` | string | Provider plugin id (e.g. `openai`, `anthropic`, `cursor`, `claude_code`). |
| `api_key_env` | string | Name of the env var that holds the API key. The key is **never** persisted — only the var name is. |
| `auth` | string | Optional auth mode override (`api_key`, `oauth`, etc., where supported). |
| `base_url` | string | Override the provider's base URL. Common for self-hosted Ollama or alternate Moonshot endpoints. |
| `binary` | string | For non-API providers, the path or name of the local binary or file (e.g. `gh` for Copilot, the Gemini CLI binary, the Claude state file path). |
| `probe_model` | string | For header-probing providers, the model to send a minimal request against. |

:::warning API keys are never stored
The `api_key_env` field stores the **name** of the environment variable, not its value. The TUI reads the value from your shell at runtime. Don't put plaintext API keys in `settings.json`.
:::

## `auto_detected_accounts`

Read-only mirror of accounts the detector found at startup. Format is identical to `accounts`. When the same `id` appears in both, the manually configured entry wins.

## Full annotated example

```json
{
  "auto_detect": true,
  "theme": "Gruvbox",
  "ui": {
    "refresh_interval_seconds": 30,
    "warn_threshold": 0.20,
    "crit_threshold": 0.05
  },
  "data": {
    "time_window": "7d",
    "retention_days": 30
  },
  "telemetry": {
    "provider_links": {
      "anthropic": "claude_code",
      "google": "gemini_api",
      "github-copilot": "copilot"
    }
  },
  "experimental": {
    "analytics": false
  },
  "model_normalization": {
    "enabled": true,
    "group_by": "lineage",
    "min_confidence": 0.80,
    "overrides": []
  },
  "dashboard": {
    "view": "grid",
    "providers": [
      { "account_id": "openai-personal", "enabled": true },
      { "account_id": "anthropic-work",  "enabled": true },
      { "account_id": "openrouter",      "enabled": false }
    ],
    "widget_sections": [
      { "id": "top_usage_progress", "enabled": true },
      { "id": "model_burn",         "enabled": true },
      { "id": "client_burn",        "enabled": true },
      { "id": "other_data",         "enabled": true },
      { "id": "daily_usage",        "enabled": false }
    ]
  },
  "integrations": {
    "claude_code": {
      "installed": true,
      "version": "1.0.0",
      "installed_at": "2025-01-15T10:30:00Z"
    }
  },
  "accounts": [
    {
      "id": "openai-personal",
      "provider": "openai",
      "api_key_env": "OPENAI_API_KEY",
      "probe_model": "gpt-4.1-mini"
    },
    {
      "id": "anthropic-work",
      "provider": "anthropic",
      "api_key_env": "ANTHROPIC_API_KEY"
    }
  ],
  "auto_detected_accounts": []
}
```

## See also

- [Environment variables](./env-vars.md) — runtime overrides
- [Paths reference](./paths.md) — where the file lives on each OS
- [Themes](../customization/themes.md) — values for the `theme` field
- [Widgets](../customization/widgets.md) — values for `dashboard.widget_sections`
