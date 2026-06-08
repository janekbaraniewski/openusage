---
title: Kiro CLI
description: Track Kiro CLI (renamed Amazon Q Developer CLI) conversations and token estimates in OpenUsage.
sidebar_label: Kiro
keywords: [kiro cli usage tracker, kiro cli quota tracking, kiro cli cost tracking, kiro cli token usage, track kiro cli spend locally]
---

# Kiro CLI

Local-data provider for Kiro CLI, the renamed Amazon Q Developer CLI. Reads file-based session transcripts under `~/.kiro/sessions/cli/` and the platform-specific `data.sqlite3` store, then aggregates conversations, models, and best-effort token totals. No network calls, no auth.

:::warning Experimental
Schema confidence is LOW. Kiro CLI does not persist token counts directly: at best they are recovered from explicit `input_tokens` / `output_tokens` fields inside per-turn metadata, otherwise estimated from `context_usage_percentage` × `context_window_tokens`. Numbers can under- or over-report on schema variants we have not yet observed. File an issue with your `data.sqlite3` schema if values look wrong.
:::

## At a glance

- **Provider ID** — `kiro_cli`
- **Detection** — `kiro` (or `q`) binary on PATH, or the file-session directory `~/.kiro/sessions/cli/`, or `data.sqlite3` at the platform-specific location
- **Auth** — none (local files only)
- **Type** — coding agent (experimental)
- **Tracks**:
  - Total conversations
  - Conversations with recoverable token data
  - Input / output / total tokens (estimated)
  - Per-model breakdown with request counts and workspace dimension
  - Daily series for conversations and tokens

## Setup

### Auto-detection

OpenUsage registers the provider when any of the following are present:

- The `kiro` binary on PATH (or `q`, kept as a fallback for older Amazon Q Developer CLI installs).
- The file-session directory `~/.kiro/sessions/cli/`. Override with `KIRO_SESSIONS_DIR`.
- The SQLite store `data.sqlite3` at the platform-specific data directory. Override with `KIRO_DATA_DIR` (the file under that root is always `data.sqlite3`).

Default `data.sqlite3` locations:

- macOS — `~/Library/Application Support/kiro-cli/data.sqlite3`
- Linux — `$XDG_DATA_HOME/kiro-cli/data.sqlite3` (fallback `~/.local/share/kiro-cli/data.sqlite3`)
- Windows — not yet published by Amazon; set `KIRO_DATA_DIR` explicitly. Linux conventions are used as a fallback.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "kiro-cli",
      "provider": "kiro_cli",
      "extra": {
        "sessions_dir": "/absolute/path/to/.kiro/sessions/cli",
        "db_path": "/absolute/path/to/data.sqlite3"
      }
    }
  ]
}
```

Path hints honoured by the provider:

- `sessions_dir` — file-session directory (one JSON header + one JSONL transcript per session).
- `db_path` — absolute path to `data.sqlite3`.

Either source on its own is enough; when both are present they are merged.

## Data sources & how each metric is computed

The provider runs two readers and merges the results by conversation ID (or by session-file key when no ID is exposed). Duplicates across sources are coalesced: the entry with the newer `UpdatedAt` wins, and token data from either side fills in when the other lacks it.

### File sessions — `~/.kiro/sessions/cli/`

Every `<session>.json` is a small header with a companion `<session>.jsonl` transcript:

- The header carries `session_id`, `cwd`, `updated_at`, and `session_state` with model info, context window, and `user_turn_metadatas`.
- The JSONL is one line per event. Lines with `kind == "AssistantMessage"` are folded into per-message events, deduplicated by `message_id` (the last occurrence wins so streamed updates with richer metadata are preserved).

Token resolution per assistant message, in order: explicit `input_tokens` / `output_tokens` on the matching turn → `context_usage_percentage` × `context_window_tokens` for input → `response_size` (or content character count, divided by 4) for output. The conversation summary uses the JSONL message count when available, otherwise the count of `user_turn_metadatas` entries.

### SQLite store — `data.sqlite3`

Opened in read-only / immutable mode. The provider auto-detects which conversations table is present (`conversations_v2` for current Kiro CLI, `conversations` for older Amazon Q Developer CLI). Both are key/value JSON blobs.

For each row the provider walks `session_state.rts_model_state.model_info` for the model and context window, sums explicit `input_tokens` / `output_tokens` from `conversation_metadata.user_turn_metadatas` when present, and falls back to the context-percentage estimate when not. Rows that do not parse as JSON are still surfaced as session-only records so they contribute to the conversation count.

### Metrics

- `total_conversations` — distinct merged conversations across both sources.
- `conversations_with_tokens` — subset that produced any recoverable token figure.
- `total_input_tokens` / `total_output_tokens` / `total_tokens` — best-effort sums.
- `total_messages` — set only when at least one conversation exposed a message count.
- Daily series: `conversations` and `tokens` bucketed by UTC date of `UpdatedAt`.

The per-model `ModelUsageRecord` carries `requests` (conversation count for that model), token totals, and `workspace` / `message_count` dimensions when the underlying conversation exposed them.

### How fresh is the data?

- Polling: every 30 s by default. The provider stat()s both the sessions directory and `data.sqlite3` and short-circuits when nothing has changed since the last poll.

## Caveats

- Token counts are best-effort. The status message appends `(est.)` to make this visible.
- Schema changes in Kiro CLI between versions can break extraction. The provider records a `schema_confidence=experimental` diagnostic on every snapshot to make this expectation explicit.
- The provider never writes to the database; it opens SQLite in read-only / immutable mode so Kiro CLI itself is never blocked.
- When both sources error, the tile reports `StatusError` and the joined error messages. When only one errors, the other continues to populate the snapshot.

## Troubleshooting

- **Tile shows "Kiro CLI sessions not found"** — confirm `~/.kiro/sessions/cli/` exists, or set `KIRO_SESSIONS_DIR` / `KIRO_DATA_DIR`. The provider needs at least one of the two sources to be present.
- **`schema_confidence=experimental` and zero tokens** — your schema is missing the fields the parser recognises. File an issue with the output of `sqlite3 data.sqlite3 '.schema'` so the extraction can be tightened.
- **`query_error` or `sessions_error` diagnostics** — one of the readers failed. Both are recorded so the other source can keep populating data; check the diagnostic text for the underlying cause.
