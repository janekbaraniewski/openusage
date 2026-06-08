---
title: Codebuff
description: Track local Codebuff (Manicode) chats, per-model tokens, and credit usage in OpenUsage.
sidebar_label: Codebuff
keywords: [codebuff usage tracker, codebuff quota tracking, codebuff cost tracking, codebuff token usage, track codebuff spend locally]
---

# Codebuff

Local-file provider for the [Codebuff](https://codebuff.com/) AI coding agent (named `manicode` in its on-disk layout for historical reasons). Reads per-chat JSON files under `~/.config/manicode/`, `~/.config/manicode-dev/`, and `~/.config/manicode-staging/`, and aggregates per-model token totals plus Codebuff credit spend. No network calls and no authentication.

## At a glance

- **Provider ID** — `codebuff`
- **Detection** — any of `~/.config/manicode/`, `~/.config/manicode-dev/`, `~/.config/manicode-staging/` exists, or a `codebuff` binary on `PATH`
- **Auth** — local file
- **Type** — coding agent
- **Tracks**:
  - Total chats, chats today, chats in the last 7 days
  - Total messages
  - Total input, output, cache-read, and cache-write tokens
  - Codebuff credits used (a Codebuff-internal unit, not USD)
  - Per-model token totals with inferred upstream provider hint
  - Daily series for chats, tokens, and credits

## Setup

### Auto-detection

OpenUsage registers the provider when any of the three default channel roots exist or `codebuff` is on `PATH`. Auto-detection records the first matching root under `data_dir`. At fetch time every existing root is scanned and an additional path from the `CODEBUFF_DATA_DIR` environment variable is also walked when set.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "codebuff",
      "provider": "codebuff",
      "extra": {
        "data_dir": "~/.config/manicode"
      }
    }
  ]
}
```

`data_dir` adds an additional channel root to the scan. The default roots and `CODEBUFF_DATA_DIR` are still honored; values are de-duped after path normalization, so listing the same directory twice is safe.

## Data sources & how each metric is computed

For each resolved channel root the provider walks `<root>/projects/<project>/chats/<chatId>/chat-messages.json`. Each file is a JSON array of message objects; only those with `role: "assistant"` and a recognizable token-usage block are counted.

### Usage extraction (three-tier fallback)

The provider looks for token counts in this order on each assistant message and stops at the first non-empty match:

1. `metadata.usage`
2. `metadata.codebuff.usage`
3. `metadata.runState.sessionState.mainAgentState.messageHistory[].providerOptions.usage` — the first history entry with any non-zero token wins

Field mapping inside the chosen usage block:

| Upstream                          | openusage metric        |
| --------------------------------- | ----------------------- |
| `input_tokens`                    | `total_input_tokens`    |
| `output_tokens`                   | `total_output_tokens`   |
| `cache_read_input_tokens`         | `total_cache_read`      |
| `cache_creation_input_tokens`     | `total_cache_write`     |
| `credits`                         | `total_credits`         |
| `model`                           | `RawModelID`            |

When `model` is missing the per-model row falls back to the literal string `codebuff-unknown`. The upstream provider hint is inferred from the model name prefix: `claude-` → `anthropic`, `gpt-` or `o1-` → `openai`, `gemini-` → `google`, otherwise `unknown`.

### Credits

`total_credits` is emitted in Codebuff's internal credit unit, **not** in USD. The dashboard renders it as a separate row labelled `Credits`. There is no automatic conversion to dollars; the upstream JSON does not expose a rate.

### Chat counts

A "chat" is keyed by the tuple `<channel>/<project>/<chatId>`. Channel is the basename of the channel root (`manicode`, `manicode-dev`, `manicode-staging`, or the basename of a custom root). Project is the first path segment under `<root>/projects/`. `chatId` is the directory name immediately above the `chat-messages.json` file.

- `total_chats` — distinct chat tuples observed
- `chats_today` — chats with at least one assistant message timestamped on the current UTC day
- `chats_7d` — chats with at least one assistant message in the last 7 days

### Timestamp resolution

The provider tries, in order: `metadata.timestamp`, `message.timestamp`, then a derivation from the `chatId`. A Codebuff `chatId` looks like `2025-12-14T10-00-00.000Z` — an ISO-8601 timestamp with the time-portion's `:` separators replaced by `-`. The provider rebuilds the colons in the time half only, leaving the date half alone, then parses RFC 3339.

### Per-message dedup

Each assistant message is hashed by `(input, output, cache_read, cache_write, ordinal)` if it lacks a stable `id`. The dedup key is then prefixed with `<channel>/<project>/<chatId>` so the same chat on two channels does not collide. This is what keeps repeated polls from inflating totals when an in-progress chat is rewritten between snapshots.

### Daily series

`DailySeries["sessions"]` (chats by day), `DailySeries["tokens"]`, and `DailySeries["credits"]` are populated.

### What's NOT tracked

- **USD cost.** Codebuff bills in credits. The provider does not run a pricing lookup, so `total_cost_usd` is never emitted.

## Caveats

- The on-disk directory name is `manicode` rather than `codebuff` for historical reasons. Both refer to the same product.
- Three channels (`manicode`, `manicode-dev`, `manicode-staging`) are walked separately so dev / staging activity stays counted, but tagged by channel inside the chat tuple.
- `CODEBUFF_DATA_DIR` is honored at fetch time; setting it does not require restarting the daemon for the next poll cycle.
- The `chatId` time-parsing has to keep date-half `-` separators intact while restoring `:` in the time half. If you spot timestamps falling on the wrong day, file an issue with a sample `chatId`.

## Troubleshooting

- **Tile is empty** — run a Codebuff chat so `chat-messages.json` lands under one of the channel roots. Confirm with `openusage detect`.
- **Credits show 0 despite running paid chats** — the installed CLI version did not write `credits` into the usage block. Check `~/.config/manicode/projects/<project>/chats/<chatId>/chat-messages.json` and grep for `"credits":`.
- **Custom data dir not being scanned** — confirm `echo $CODEBUFF_DATA_DIR` returns the expected path in the same shell that launched the OpenUsage daemon. The default roots are also scanned in addition to the override.

## Related

- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
- [OpenCode](./opencode.md) — sibling local-file coding-agent provider
