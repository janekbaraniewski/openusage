---
title: Cache hit ratio
description: How OpenUsage computes the prompt cache hit ratio, which providers support it, and why some can't.
keywords: [prompt cache hit ratio, claude code cache hits, cache read tokens, openusage cache metric]
---

Many AI coding tools reuse a **prompt cache**: large, stable parts of a prompt (system instructions, file context, prior turns) are written to a cache once and read back cheaply on later requests. The **cache hit ratio** tells you how much of your prompt volume is being served from that cache instead of being re-sent as fresh input.

OpenUsage surfaces this as a `cache_hit_ratio` gauge on supported provider tiles and in the detail panel.

## What it measures

Prompt tokens split into three buckets that cache-capable providers report:

- **input** — non-cached prompt tokens
- **cache read** — prompt tokens served from cache (a hit)
- **cache write / creation** — prompt tokens written to the cache on a miss

The ratio is token-weighted:

```
cache_hit_ratio = cache_read / (input + cache_read + cache_write) × 100
```

That is: *of all prompt tokens this window, what fraction were served from cache.* This matches Anthropic's own cache-hit-rate definition and the OpenAI `prompt_tokens_details.cached_tokens` convention.

The metric is a percentage gauge (0–100%) scoped to whichever time window the tile is showing.

:::note It's a coverage metric, not a savings metric
The ratio reflects **token coverage**, not dollars saved. Cache reads are billed at a steep discount (Anthropic discounts them ~90%), so a 70% hit ratio saves far more than 70% of your input cost. The gauge answers "how much of my prompt was cached," not "how much money I saved."
:::

## When it appears

`cache_hit_ratio` is emitted only when there is prompt-cache activity in the window. Providers with no caching (or a quiet window) emit nothing, so you won't see a misleading `0%`.

| Provider | Supported | Notes |
|---|---|---|
| Claude Code | Yes | Rolling 7-day window from local conversation logs. The headline use case. |
| Codex CLI | Yes | Per-session, from `cached_input_tokens`. |
| OpenRouter | Yes | "Today" window, paired with native prompt tokens. |
| Copilot, Cursor, Gemini CLI, OpenCode, and other coding tools | Yes (daemon mode) | Computed centrally from stored telemetry token counts. Run `openusage telemetry` to collect them. |
| Gemini CLI | Partial | Reports cache reads but not cache writes, so the denominator omits writes and the ratio reads slightly high. |
| Anthropic, OpenAI, Groq, Mistral, DeepSeek, xAI, Gemini API, Alibaba Cloud, Z.ai, Moonshot, Perplexity | No | These probe rate-limit headers only and never see a usage body with cached-token counts. Cache hit ratio is not available. |

## How it's computed

There are two paths, both using the same formula:

- **Daemon / telemetry mode** — computed once in the telemetry read model from the per-model `input` / `cache_read` / `cache_write` token sums already stored in SQLite. Every telemetry-backed provider gets a window-scoped ratio from this single place.
- **Direct mode** (no daemon) — the provider's own fetch computes it from the token totals it already reads locally. Claude Code, Codex, and OpenRouter do this so the gauge works without running the daemon.

## Related

- [Claude Code](../providers/claude-code.md) — per-model cache read / cache create token breakdown
- [Codex CLI](../providers/codex.md)
- [OpenRouter](../providers/openrouter.md)
- [Daemon & telemetry](../daemon/integrations.md) — collecting per-turn token events for the providers that need it
