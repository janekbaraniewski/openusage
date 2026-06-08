---
title: Droid
description: Track Factory Droid sessions and per-model token usage in OpenUsage.
sidebar_label: Droid
keywords: [droid usage tracker, droid quota tracking, droid cost tracking, droid token usage, track droid spend locally]
---

# Droid

Local-data provider for the Factory Droid CLI. Reads per-session settings files written under `~/.factory/sessions/`. No network calls are made and no authentication is required.

## At a glance

- **Provider ID** — `droid`
- **Detection** — `droid` binary on `PATH` or `~/.factory/sessions/` exists
- **Auth** — none (local files only)
- **Type** — coding agent
- **Tracks**:
  - Sessions (total, today, 7d)
  - Per-model input / output / cache-read / cache-write / reasoning tokens
  - Daily series for sessions and tokens

## Setup

### Auto-detection

Either signal is sufficient: the `droid` binary on `PATH`, or the existence of the sessions directory. The provider's `Fetch` handles a missing-or-empty sessions directory gracefully, so a freshly installed binary registers a tile that simply reports "No Droid sessions recorded" until the first run.

The sessions directory is `~/.factory/sessions`. The parent config directory is `~/.factory`.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "droid",
      "provider": "droid",
      "extra": {
        "sessions_dir": "~/.factory/sessions"
      }
    }
  ]
}
```

Only `sessions_dir` is honoured as a path-hint override. The override is used only when the directory exists; otherwise the provider falls back to the default location.

## Data sources & how each metric is computed

### Per-session settings file

Each session is a file pair under `~/.factory/sessions/`:

- `<uuid>.settings.json` — authoritative per-session metadata and token usage
- `<uuid>.jsonl` — full event log (consulted only as a model-name fallback)

The provider walks the sessions directory and parses every `*.settings.json` file. The session UUID is taken from the filename (`<uuid>.settings.json` → `<uuid>`).

### Token usage

From the settings file's `tokenUsage` object:

| Source field | OpenUsage metric |
|---|---|
| `tokenUsage.inputTokens` | `total_input_tokens` |
| `tokenUsage.outputTokens` | `total_output_tokens` |
| `tokenUsage.cacheCreationTokens` | `total_cache_write` |
| `tokenUsage.cacheReadTokens` | `total_cache_read` |
| `tokenUsage.thinkingTokens` | `total_reasoning_tokens` |

Negative values are clamped to zero. Sessions with `tokenUsage` missing, or with all-zero tokens across every category, are skipped silently. Malformed JSON in a single settings file is non-fatal: the walk continues.

`total_tokens` is the sum of input + output + reasoning. Cache-read and cache-write are surfaced separately on the tile.

### Model and provider attribution

Model and provider names are resolved in priority order:

1. `settings.model` (preferred). Names are normalised: `custom:` prefix stripped, `[...]` bracket annotations removed, lowercased, dots replaced with hyphens, runs of hyphens collapsed. Example: `custom:Claude-Opus-4.5-Thinking-[Anthropic]-0` becomes `claude-opus-4-5-thinking-0`.
2. If `settings.model` is empty, the provider scans up to the first 500 lines of the companion `<uuid>.jsonl` for a `Model: <name>` token.
3. If still empty, the provider falls back to a per-provider placeholder (`claude-unknown`, `gpt-unknown`, `gemini-unknown`, `grok-unknown`, `droid-unknown`).

`settings.providerLock` wins for the upstream-provider hint. When absent, the provider is inferred from the model name prefix (`claude*`/`opus`/`sonnet`/`haiku` → `anthropic`; `gpt*`/`o1`/`o3`/`o4` → `openai`; `gemini*` → `google`; `grok*` → `xai`; otherwise `droid`). The inferred value is stored as the `upstream_provider` dimension on the `ModelUsage` record.

### Timestamps

`settings.providerLockTimestamp` (RFC3339) is preferred. If absent or unparseable, the provider falls back to the settings file's mtime. UTC days are used for today / 7d buckets and daily series.

### Surfaced metrics

| Metric | Window |
|---|---|
| `total_sessions` | all-time |
| `sessions_today` / `sessions_7d` | today / 7d |
| `total_tokens` | all-time |
| `total_input_tokens` / `total_output_tokens` | all-time |
| `total_cache_read` / `total_cache_write` | all-time |
| `total_reasoning_tokens` | all-time |

`DailySeries`: `sessions`, `tokens`.

### What's NOT tracked

- **Cost.** The settings file does not carry a per-session dollar figure, so no cost metric is emitted. Per-model cost is best derived by combining the surfaced token counts with the upstream provider's pricing.
- **Per-message detail.** The provider does not parse the `<uuid>.jsonl` event log beyond a best-effort model-name probe.

### How fresh is the data?

- Polling: every 30 s by default.
- The provider's `HasChanged` hook stats the sessions directory and skips Fetch when the directory hasn't been touched since the last poll.

## Files read

- `~/.factory/sessions/*.settings.json` — per-session token usage and metadata
- `~/.factory/sessions/*.jsonl` — read only when `settings.model` is empty, to recover the model name

## Caveats

- A malformed `*.settings.json` is silently skipped. The tile will simply show fewer sessions than exist on disk; there is no per-file diagnostic.
- All-zero-token sessions are filtered out at parse time. Placeholder rows Droid creates on UI navigation don't pollute totals.
- Model normalisation is one-way and lossy: distinct upstream display names that normalise to the same string (e.g. `Claude-Opus-4.5` and `claude opus 4.5`) collapse into a single `ModelUsage` row.

## Troubleshooting

- **Tile shows "Droid sessions directory not found"** — `~/.factory/sessions` does not exist. Run a Droid session, or set the `sessions_dir` `extra` key if your install uses a non-default location.
- **Tile says "No Droid sessions recorded"** but you have run sessions — confirm the files under `~/.factory/sessions/` end with `.settings.json` and contain a `tokenUsage` object. The walk only picks up files with that suffix.
- **Model column shows `*-unknown`** — `settings.model` was empty and the JSONL probe didn't find a `Model:` token. This is harmless for token totals; only the per-model breakdown is affected.

## Related

- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
- [Amp](./amp.md) — sibling local-file coding-agent provider
- [Crush](./crush.md) — sibling per-project SQLite agent
