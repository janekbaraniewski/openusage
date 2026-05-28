---
title: Crush
description: Track Crush coding-agent sessions, per-project usage, and model token counts in OpenUsage.
sidebar_label: Crush
---

# Crush

Local-data provider for the Charmbracelet Crush CLI agent. Crush stores its usage data **per project** in a SQLite file at `<project>/.crush/crush.db`. OpenUsage walks a configurable set of search roots, finds every project DB, and aggregates session-level totals across them.

## At a glance

- **Provider ID** — `crush`
- **Detection** — `crush` binary on `PATH` or at least one `.crush/crush.db` under a default search root
- **Auth** — none (local SQLite reads only)
- **Type** — coding agent
- **Tracks**:
  - Sessions (total, today, 7d)
  - Per-project DB count
  - Per-model input / output / total tokens
  - Cost (when the upstream version recorded it)
  - Daily series for sessions, tokens, and cost

## Setup

### Auto-detection

Two signals trigger registration: the `crush` binary on `PATH`, or the existence of `<root>/.crush/crush.db` under any of the default search roots:

- `~/code`, `~/src`, `~/workspace`, `~/dev`
- `~/Projects`, `~/projects`, `~/Workspace`

`$HOME` and `~/Documents` are intentionally excluded. Walking either triggers macOS TCC permission prompts (Photo Library when `~/Pictures/Photos Library.photoslibrary` is reached, iCloud Drive when `~/Desktop` / `~/Documents` are iCloud-synced). If your project trees live in one of those locations, point `$OPENUSAGE_CRUSH_ROOTS` (or the per-account `search_roots`) at the specific subdirectory instead.

The walker descends up to 4 levels under each root and skips well-known noise directories (`.git`, `node_modules`, `.venv`, `vendor`, `target`, `build`, `dist`, `.idea`, `.vscode`, `__pycache__`, `.cache`, `.direnv`, `.terraform`), plus macOS-protected directories as a defense-in-depth when users override `search_roots` (`Library`, `Pictures`, `Movies`, `Music`, `Desktop`, `Public`, `Applications`, `.Trash`, and any `*.photoslibrary` bundle). Discovered DB paths are stored on the account so subsequent polls skip the walk.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "crush",
      "provider": "crush",
      "extra": {
        "search_roots": "/Users/me/code:/Users/me/work",
        "db_paths": "/Users/me/code/foo/.crush/crush.db:/Users/me/code/bar/.crush/crush.db",
        "db_path": "/Users/me/code/foo/.crush/crush.db"
      }
    }
  ]
}
```

Path-hint key precedence:

1. `db_paths` — an explicit colon-separated list of DBs (skips the walk entirely)
2. `db_path` — a single DB override
3. `search_roots` — a colon-separated list of roots to walk
4. Default search roots above

`$OPENUSAGE_CRUSH_ROOTS` overrides the default search roots without editing `settings.json`. All path-list values use the OS path-list separator (`:` on Unix, `;` on Windows).

## Data sources & how each metric is computed

### DB discovery

`resolveDBPaths` returns the effective list of `crush.db` files for the account. Pre-resolved lists from the detector are filtered against the filesystem on every poll so stale entries don't blow up the tile.

### Session reader

The provider opens each DB read-only and runs a single query against the `sessions` table, filtering to root sessions (`parent_session_id IS NULL`) so child sub-agent sessions don't double-count. Crush already rolls token and cost totals up into the parent row.

Per-session columns read: `id`, `message_count`, `prompt_tokens`, `completion_tokens`, `cost`, `created_at`, `updated_at`. Empty placeholder sessions (no messages AND no cost) are skipped.

### Model attribution

Each session is annotated with the model and (when available) the upstream provider from the latest assistant message:

```sql
SELECT model, provider FROM messages
WHERE session_id = ? AND role = 'assistant'
ORDER BY created_at DESC LIMIT 1
```

The `messages.provider` column was added by Crush migration `20250627000000_add_provider_to_messages.sql`. The provider probes `PRAGMA table_info(messages)` before selecting, so older DBs without the column degrade to model-only attribution. The upstream provider is stored as the `upstream_provider` dimension on each `ModelUsage` row.

When a session uses multiple models, the **most recent assistant message wins** for attribution; per-message breakdown is out of scope.

### Aggregation

Day attribution uses `created_at` (millisecond-since-epoch column) and falls back to `updated_at` when missing. UTC days are used for the today / 7d buckets and daily series.

### Surfaced metrics

| Metric | Window | Notes |
|---|---|---|
| `total_sessions` | all-time | distinct root sessions across all DBs |
| `sessions_today` / `sessions_7d` | today / 7d | UTC-day attribution |
| `total_input_tokens` / `total_output_tokens` | all-time | sum across sessions |
| `total_tokens` | all-time | input + output |
| `total_cost_usd` | all-time | emitted only when at least one session recorded a non-zero cost |
| `total_projects` | all-time | number of DBs aggregated |

`DailySeries`: `sessions`, `tokens`, and (when present) `cost_usd`.

### What's NOT tracked

- **Per-message token detail.** Crush stores tokens at the session level; the provider does not iterate the message table to build per-turn detail.
- **Sub-agent sessions.** Child sessions are intentionally filtered out to avoid double-counting; their tokens are already in the root session row.

### How fresh is the data?

- Polling: every 30 s by default.
- The provider's `HasChanged` hook stats every resolved DB path and skips Fetch when none changed since the last poll.

## Files read

- `<root>/.crush/crush.db` — one SQLite database per project root

## Caveats

- The default walk is best-effort: directories with permission denied or that disappear mid-walk are silently skipped (`fs.SkipDir`). One unreadable subtree never blanks the rest of the tile.
- One bad DB does not blank the tile. A per-DB read error is recorded under the `query_errors` diagnostic and the remaining DBs continue. Only when **every** DB fails does the tile go to `error` status.
- The Crush global config dir (`$XDG_DATA_HOME/crush` or `~/.local/share/crush`) holds OAuth tokens and recent-model preferences only; usage data lives per-project.
- Cost values come from Crush's own per-session aggregate. If you run a model Crush doesn't have a price for, the cost column will be absent for that session.

## Troubleshooting

- **Tile shows "No Crush project databases found"** — Crush has not been run inside any project tree under the default search roots, or `$OPENUSAGE_CRUSH_ROOTS` is set to a path that contains nothing. Confirm `.crush/crush.db` exists somewhere reachable, or set `db_paths` explicitly.
- **Some projects missing** — the walker stops at depth 4 from each root, and skips a list of noise directories. Either widen the search roots, or pin the DB explicitly via `db_paths`.
- **`query_errors` diagnostic present** — a DB read failed. The diagnostic lists the offending paths and SQLite errors verbatim. Typical causes are an old schema (no `messages` table at all) or a `.crush` directory left behind without the actual DB.

## Related

- [Claude Code](./claude-code.md) — sibling local-file coding-agent provider
- [Amp](./amp.md) — sibling local-file coding-agent provider
- [Goose](./goose.md) — sibling SQLite-backed coding-agent provider
