---
title: Telemetry pipeline
description: How the daemon stores events, deduplicates them, and turns them into snapshots — events, sources, dedup, and retention.
---

When OpenUsage is collecting data, it flows through a small event-sourced pipeline in the daemon before it ever reaches the TUI. Understanding this pipeline helps explain why hooks give richer data than polling alone, why the same conversation isn't double-counted, and where retention bounds live.

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

`provider.Fetch()` calls driven by the daemon on its own interval. Output: `provider_snapshots` rows + derived `usage_events`.

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

## Why a configured account is still required when telemetry is doing the work

A common point of confusion: you've installed the OpenCode plugin (or Claude Code hook), spend events are streaming into the store, you can see them in the SQLite database — but unless an account is configured for the provider those events are tagged with, no tile renders.

That's by design. A dashboard tile is owned by a configured account. An account exists when one of two things is true:

- A provider's auto-detection signal is present (typically the env var, e.g. `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`)
- An entry exists in `accounts` in your `settings.json`

Telemetry events are the **data** that lands on a tile. The account is the **container** that lets the tile exist in the first place. Without it, ingested events are stored, deduplicated, and remain queryable — but they don't surface in the UI because there's no place for them to appear.

### Why this split?

Three reasons:

1. **Each provider has data the plugin can't carry** — rate-limit headers, balance, plan, model catalog. Those come from native provider polling, which needs auth.
2. **A telemetry source ID is not the same as your account** — the OpenCode plugin tags events with whatever ID OpenCode uses for the upstream model (`anthropic`, `google`, `github-copilot`). Those IDs become tile owners only after you've configured the matching account in OpenUsage.
3. **No silent account creation** — auto-creating an account from a stream of foreign events would leak whatever provider the integration knows about into your dashboard without consent.

### What this looks like in practice

If you only have `OPENCODE_API_KEY` (or its alias `ZEN_API_KEY`) set and you're using OpenCode to call OpenAI, Anthropic, and Gemini:

- The OpenCode tile exists and shows the Zen model catalog and key validity (from native polling).
- The OpenCode plugin emits per-turn events tagged `openai`, `anthropic`, `google`.
- None of those have configured accounts → no tiles → events sit in the store.

To make the spend visible, set the env vars for the upstream providers (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`). Once configured, those tiles appear, and the plugin events route to them automatically.

If your tile names don't match the source IDs (`google` ↔ `gemini_api`, `github-copilot` ↔ `copilot`), see the next section.

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

- [Architecture](architecture.md) — how the daemon, providers, and TUI fit together.
- [Daemon overview](/daemon) — install, configure, troubleshoot.
- [Cost attribution](../guides/cost-attribution.md) — practical recipes for using the data.
