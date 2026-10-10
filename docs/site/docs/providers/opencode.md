---
title: OpenCode
description: Track OpenCode auth, available zen models, and spend via the telemetry plugin in OpenUsage.
sidebar_label: OpenCode
keywords: [opencode usage tracker, opencode quota tracking, opencode cost tracking, opencode token usage, track opencode spend locally]
---

# OpenCode

Tracks the OpenCode tool's auth status and available models. Spend and per-session activity come from the OpenCode telemetry plugin, not the public API.

## At a glance

- **Provider ID** — `opencode`
- **Detection** — `OPENCODE_API_KEY` or `ZEN_API_KEY` environment variable (`OPENCODE_API_KEY` is the primary env var; `ZEN_API_KEY` is an alias). Also adopts API keys written to OpenCode's `auth.json` (both `opencode`/Zen and `opencode-go`/Go catalog entries land on the same `opencode` tile).
- **Auth** — API key
- **Type** — coding agent
- **Tracks**:
  - Auth status
  - Available zen models with `owned_by` metadata
  - Spend and activity (only via the telemetry plugin)

## Setup

### Auto-detection

Set `OPENCODE_API_KEY` (preferred) or `ZEN_API_KEY` (alias). Both work; the first non-empty value wins.

OpenUsage also adopts API keys written to OpenCode's `auth.json` automatically. That file lives at `~/.local/share/opencode/auth.json` on Linux and macOS, at `$XDG_DATA_HOME/opencode/auth.json` when `XDG_DATA_HOME` is set, and at `%USERPROFILE%\.local\share\opencode\auth.json` on Windows (OpenCode uses the XDG-style location on Windows too). See the [paths reference](../reference/paths.md#tool-integration-paths).

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "opencode",
      "provider": "opencode",
      "api_key_env": "OPENCODE_API_KEY",
      "base_url": "https://opencode.ai"
    }
  ]
}
```

## Data sources & how each metric is computed

The OpenCode provider has two data paths:

1. **Polling.** The provider hits `GET https://opencode.ai/zen/v1/models` to list available Zen models and confirm the API key works. **The Zen API does not expose spend, balance, or per-session activity to API keys**, so polling alone never produces a usage figure on the OpenCode tile.
2. **Telemetry plugin.** When the OpenCode telemetry plugin is installed, OpenCode posts per-turn events (model, token counts, tools) to the OpenUsage daemon over its socket. **Those events are tagged with the upstream provider** (the model the turn actually called: `anthropic`, `openai`, `google`, etc.), not with `opencode`.
3. **Optional console enrichment.** When you import a browser-session cookie via Settings → 5 KEYS, the provider additionally calls OpenCode's authenticated console RPCs (`server.queryBilling`) to populate balance / monthly limit / subscription. This is opt-in.

### Available zen models

- Source: `data[].id` from `GET /zen/v1/models`. Each entry also carries an `owned_by` field surfaced in the detail view.
- Transform: count is stored as `Attributes["available_models_count"]`; the joined list is stored as `Attributes["available_models"]`.

### Auth status

- Source: HTTP status code of the models call. `401`/`403` → `auth`; `429` → `limited`; otherwise `ok`. The OpenUsage tile message shows `Auth OK · N Zen models` (or, when enrichment succeeded, `$X.XX balance · N Zen models`).

### `console_balance` / `monthly_usage` / `monthly_limit` / `reload_amount` / `reload_trigger`

- Source: optional console RPC `server.queryBilling`, only when a browser-session cookie is configured.
- Transform: OpenCode's UI represents balances in cents × 1e6 (billing UI divides by `1e8`). The provider divides by `1e8` to convert to USD before storing. Workspace ID is auto-discovered or provided via `extra.opencode_workspace_id`.

### Subscription metadata

- Source: same console RPC as above. Fields: `subscription_plan`, `has_subscription`, `payment_method_last4`, `payment_method_type`.
- Transform: stored as snapshot attributes.

### Where spend actually shows up

The OpenCode telemetry plugin streams events tagged with the upstream provider that served each turn. Examples of how that data lands on the dashboard:

- A Claude Sonnet turn through OpenCode → event tagged `anthropic` → spend appears on the Claude Code tile (or anywhere `anthropic` is mapped via `telemetry.provider_links`).
- A GPT-4o turn through OpenCode → event tagged `openai` → spend appears on the OpenAI tile.
- A Gemini turn through OpenCode → event tagged `google` → spend appears on the Gemini API tile (`google` is the default mapping for `gemini_api`).

If the upstream provider doesn't have an account configured in OpenUsage, the events sit in the telemetry store and surface as `telemetry_unmapped_providers` diagnostics — the OpenCode tile itself does **not** absorb them, because it's a different provider.

### Tile layout

- **Headline.** With OpenCode Go quota (browser session), the headline shows the 5h / weekly / monthly usage percentages and gauges. Without quota (Zen API key only), it shows request and token activity for the selected time window, for example `11 reqs · 166.7k tok in Today`, followed by tool call and session counts.
- **Sections.** OpenCode events tagged `opencode` / `opencode-go` populate Model Burn, Provider Burn, Clients, Project Breakdown, Tool Usage, MCP Usage, and Language, the same layout other coding-tool tiles use.
- **Cache hit ratio.** `cache_hit_ratio` is cache reads divided by (input + cache reads + cache writes). It matches the `N% cached` figure in the Token Breakdown. It is a ratio, not a quota, so it never appears as the tile's `% used` headline.

### What's NOT tracked

- **Spend on the OpenCode tile from polling.** The Zen API does not expose it. The tile shows model availability and (with cookie auth) console balance only.
- **Per-session detail without the plugin.** Token counts, tools, and per-message breakdowns require the telemetry plugin.

### How fresh is the data?

- Polling: every 30 s by default.
- Telemetry: real-time (events ingested as the plugin emits them, dedup'd in the daemon's SQLite store).
- Console enrichment: same cadence as polling.

## API endpoints used

- `GET /zen/v1/models` — auth probe + model list.
- Console RPCs (browser-session auth, opt-in): OpenCode's authenticated `server.*` endpoints, including `queryBilling`.

## Caveats

:::tip
To see spend on this tile, install the OpenCode telemetry plugin and run OpenUsage in daemon mode. See [Daemon integrations](../daemon/integrations.md).
:::

- Without telemetry the tile shows model availability only; this is expected.
- `base_url` defaults to `https://opencode.ai`.

## Troubleshooting

- **No models listed** — verify the API key is valid and not rate-limited.
- **Empty spend tile** — install and configure the OpenCode telemetry plugin; see daemon docs.
- **Tile shows "No ... data for this time range" with `Auth OK · N Zen models`**: the API key works but there are no OpenCode telemetry events in the selected window and no browser session for console quota. Widen the window (`w`), connect a browser session for Go quota, or check that the telemetry plugin is still emitting events. The OpenUsage loading banner only appears while the daemon has not resolved the account yet.

### Why does the OpenCode tile not show spend even with the plugin installed?

The plugin tags each event with the **upstream provider** that served the turn (`anthropic`, `openai`, `google`, …) rather than with `opencode`. The OpenCode tile only owns events whose source provider is `opencode`. The spend is being recorded — it's just routed to the upstream provider's tile, or to `telemetry_unmapped_providers` if you have not configured that provider in OpenUsage. Set the upstream's env var (e.g. `OPENAI_API_KEY`) so a tile exists, or remap with `telemetry.provider_links`.

### What do I see if I only set OPENCODE_API_KEY and nothing else?

The OpenCode tile renders auth status and the Zen model count. Telemetry events from the plugin are written to the store but have nowhere to display: there is no Anthropic or OpenAI tile to absorb them. They appear in the daemon's `telemetry_unmapped_providers` diagnostic. Setting the upstream provider env vars (or remapping) makes the data visible.
