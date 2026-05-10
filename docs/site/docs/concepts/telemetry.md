---
title: Telemetry pipeline
description: How the daemon stores events, deduplicates them, and turns them into snapshots — events, sources, dedup, and retention.
---

When you run OpenUsage in [daemon mode](direct-vs-daemon.md), data flows through a small event-sourced pipeline before it ever reaches the TUI. Understanding this pipeline helps explain why hooks give richer data than polling alone, why the same conversation isn't double-counted, and where retention bounds live.

:::note
Telemetry stays local. The daemon listens on a Unix domain socket only; no TCP, no remote attach, nothing leaves your machine. The "telemetry" name refers to event-sourced collection, not external reporting.
:::

## Why a pipeline at all?

Polling alone has limits:

- Provider APIs only show aggregates, not individual turns.
- Some agents (Claude Code, Codex, OpenCode) record per-message detail in local files that change faster than a 30s poll can catch.
- Multiple sources of the same event need to be merged without double-counting.

The pipeline addresses all three by ingesting **events** from multiple sources, deduplicating them, and persisting the canonical set in SQLite.

## Pipeline shape

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Collectors   │  │ Hooks        │  │ Spool        │
│ (poll        │  │ (POST from   │  │ (disk queue, │
│  providers)  │  │  agents)     │  │  drained on  │
└──────┬───────┘  └──────┬───────┘  │  startup)    │
       │                 │          └──────┬───────┘
       └─────────┬───────┴─────────────────┘
                 ▼
          ┌────────────┐
          │  Pipeline  │  dedup, attach provider links
          └─────┬──────┘
                ▼
          ┌────────────┐
          │   Store    │  SQLite (WAL on, FK on)
          └─────┬──────┘
                ▼
          ┌────────────┐
          │ ReadModel  │  builds UsageSnapshot per provider
          └─────┬──────┘
                ▼
        UDS /v1/read-model → TUI
```

## The three sources

### Collectors

The same `provider.Fetch()` calls direct mode would make, but driven by the daemon on its own interval. Output: `provider_snapshots` rows + derived `usage_events`.

### Hooks

Tools you've integrated (Claude Code, Codex, OpenCode) post each turn or message to the daemon over the socket as it happens. Output: high-resolution `usage_events` and a copy in `raw_events` for forensics.

```
POST /v1/hook/{source}?account_id=…
```

Setup: `openusage integrations install <id>`. See [daemon/integrations](/daemon).

### Spool

If the daemon is briefly down or the socket isn't reachable, hook clients drop events into a disk spool (`~/.local/state/openusage/telemetry-spool/`). On daemon startup the spool is drained — no events lost.

## Event types

Every record in `usage_events` has a type:

| Event | Emitted by | Purpose |
|---|---|---|
| `turn_completed` | hooks | One agent turn finished (input + output tokens, cost, model). |
| `message_usage` | hooks, collectors | A single message's token accounting. |
| `tool_usage` | hooks | A tool call inside a turn (web search, fetch, etc). |
| `raw_envelope` | hooks | Vendor-specific JSON kept verbatim. |
| `limit_snapshot` | collectors | Rate-limit / quota state at poll time. |
| `reconcile_adjustment` | pipeline | Internal correction when collector and hook disagree. |

Raw payloads are stored separately in `raw_events` so the canonical event remains compact while a forensics trail still exists.

## Deduplication

The same conversation can produce multiple events from different sources. The pipeline picks one canonical record using a priority chain:

1. `tool_call_id` — vendor-stable ID for a single tool invocation.
2. `message_id` — vendor-stable ID for a single message.
3. `turn_id` — local ID for a turn.
4. `fingerprint` — SHA256 over event components when none of the above are present.

The first key that resolves wins. If two events share the same key, the earlier-arriving record stays; later ones are discarded.

This is why combining hooks **and** polling is safe: poll-derived events that overlap with hook-derived events are deduped on `message_id` or `fingerprint`.

## Provider links

Telemetry sources don't always match a display provider 1:1. The pipeline applies a `ProviderLinks` map so that, for example, an event tagged `"anthropic"` from the Claude Code hook shows up under the `claude_code` tile.

Default links:

| Source | Display |
|---|---|
| `anthropic` | `claude_code` |
| `google` | `gemini_api` |
| `github-copilot` | `copilot` |

Override in `settings.json`:

```json
{
  "telemetry": {
    "provider_links": {
      "anthropic": "anthropic"
    }
  }
}
```

## Retention

| Setting | Default | Effect |
|---|---|---|
| `data.retention_days` | 30 | Deletes `usage_events` and raw payloads older than this on each prune. |
| Spool `MaxAge` / `MaxFiles` / `MaxBytes` | varies | Caps the on-disk spool to prevent runaway growth if the daemon is down. |

Pruning runs periodically in the daemon. If you reduce retention, older data is removed at the next prune.

## Why you should care

| Benefit | Source |
|---|---|
| Per-turn detail (model, tokens, cost) | hooks |
| Tool-call breakdowns inside a turn | hooks |
| Continuous accumulation while TUI is closed | collectors |
| No double-counting when polling overlaps a hook | dedup |
| Survives short daemon outages | spool |
| Bounded disk usage | retention |

If you live mostly in Claude Code, Codex, or OpenCode, installing the matching integration is the single biggest data-quality upgrade the daemon offers — it turns a coarse polling cycle into a per-message stream.

## Where to read next

- [Direct vs daemon](direct-vs-daemon.md) — how the daemon fits in.
- [Daemon overview](/daemon) — install, configure, troubleshoot.
- [Cost attribution](../guides/cost-attribution.md) — practical recipes for using the data.
