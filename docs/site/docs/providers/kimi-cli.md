---
title: Kimi CLI
description: Track local Kimi CLI sessions, per-model tokens, and cache usage in OpenUsage.
sidebar_label: Kimi CLI
keywords: [kimi cli usage tracker, kimi cli quota tracking, kimi cli cost tracking, kimi cli token usage, track kimi cli spend locally]
---

# Kimi CLI

Local-file provider for the [Kimi CLI](https://github.com/MoonshotAI/kimi-cli). Reads per-session `wire.jsonl` files under `~/.kimi/sessions/` and aggregates per-model token totals. No network calls and no authentication.

This is a different provider from the [Moonshot](./moonshot.md) API tile. Moonshot reports remote quota and balance via API key; Kimi CLI reports local session activity. Both can be configured at the same time and they will appear as separate tiles.

## At a glance

- **Provider ID** — `kimi_cli`
- **Detection** — `~/.kimi/sessions/` exists, `~/.kimi/config.json` exists, or a `kimi` binary on `PATH`
- **Auth** — local file
- **Type** — coding agent
- **Tracks**:
  - Total sessions, sessions today, sessions in the last 7 days
  - Total input, output, cache-read, and cache-write tokens
  - Per-model token totals with upstream provider hint (`moonshot`)
  - Daily series for sessions and tokens

## Setup

### Auto-detection

OpenUsage registers the provider when `~/.kimi/sessions/` exists, `~/.kimi/config.json` exists, or `kimi` is on `PATH`. Run a Kimi CLI session at least once to create `~/.kimi/sessions/<group>/<session>/wire.jsonl`.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "kimi_cli",
      "provider": "kimi_cli",
      "extra": {
        "sessions_dir": "~/.kimi/sessions",
        "config_path": "~/.kimi/config.json"
      }
    }
  ]
}
```

- `sessions_dir` — replaces the default search with an explicit sessions directory
- `config_path` — points at the `config.json` that provides the default model name

Both overrides are independent. Either can be omitted to keep its default.

## Data sources & how each metric is computed

The provider walks `sessions_dir` recursively and decodes every file named `wire.jsonl`. Each is JSON-lines; only records whose `message.type` is `StatusUpdate` and whose `message.payload.token_usage` is non-empty are counted.

### Field mapping

Upstream `token_usage` fields → openusage metrics:

| Upstream                  | openusage metric        |
| ------------------------- | ----------------------- |
| `input_other`             | `total_input_tokens`    |
| `output`                  | `total_output_tokens`   |
| `input_cache_read`        | `total_cache_read`      |
| `input_cache_creation`    | `total_cache_write`     |

The upstream provider hint on each per-model row is hard-coded to `moonshot`.

### Model

`message.payload.model` is used when present. When missing, the model name is read from `~/.kimi/config.json` (the `model` field). If that file is missing, unreadable, or does not declare a model, the fallback is the literal string `kimi-for-coding`.

### Session ID

The session ID is the basename of the parent directory of `wire.jsonl` (the session UUID directory). The group directory above it is not included in the ID, so two sessions across different groups with the same UUID would collide; in practice Kimi CLI uses UUIDs so this is not a concern.

### Session counts

- `total_sessions` — distinct session IDs observed
- `sessions_today` — sessions with at least one StatusUpdate timestamped on the current UTC day
- `sessions_7d` — sessions with at least one StatusUpdate in the last 7 days

### Timestamps

Each `wire.jsonl` record carries a float-seconds-since-epoch timestamp (with sub-second precision). The provider converts this to a UTC `time.Time` and uses it for the per-day buckets. Non-positive, NaN, or infinite timestamps are dropped.

### Daily series

`DailySeries["sessions"]` and `DailySeries["tokens"]` are populated by day.

### What's NOT tracked

- **Cost in USD.** Kimi CLI `wire.jsonl` does not carry pricing and the provider does not run a pricing lookup. To see USD spend against the underlying Moonshot account, configure the [Moonshot](./moonshot.md) API provider alongside this one.

## Caveats

- The Kimi CLI and Moonshot providers are intentionally separate. Configure both for full visibility: Moonshot gives you remote balance / quota; Kimi CLI gives you local activity.
- Buffer size for scanning `wire.jsonl` is 1 MiB per line; very long tool-call payloads inside a single StatusUpdate frame may be skipped if they exceed that. Per-line decode failures are silently dropped.
- The fallback model `kimi-for-coding` exists so that per-model rows always have a label. Seeing it on the tile means the installed CLI version is not emitting per-StatusUpdate model names and `~/.kimi/config.json` does not declare one either.

## Troubleshooting

- **Tile is empty** — run a Kimi CLI session so a `wire.jsonl` lands under `~/.kimi/sessions/<group>/<session>/`. Confirm with `openusage detect`.
- **All tokens attributed to one model** — the CLI is not stamping per-record `model` and the fallback from `config.json` is being used. Set the model in `~/.kimi/config.json` or upgrade the CLI.
- **Cost is missing despite paid usage** — expected. Add the Moonshot API tile by setting `MOONSHOT_API_KEY`; see the [Moonshot](./moonshot.md) page.

## Related

- [Moonshot](./moonshot.md) — sibling API-key provider for the underlying Moonshot platform
- [Codex CLI](./codex.md) — sibling local-file coding-agent provider
