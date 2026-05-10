---
title: Z.AI
description: Track Z.AI 5-hour window, monthly usage, credit grants, and tool usage in OpenUsage.
sidebar_label: Z.AI
---

# Z.AI

Deep visibility for Z.AI coding subscriptions. Tracks the 5-hour rolling token window, monthly usage, per-model and per-tool breakdowns, and credit grants with expiry warnings.

## At a glance

- **Provider ID** — `zai`
- **Detection** — `ZAI_API_KEY` or `ZHIPUAI_API_KEY` (China fallback)
- **Auth** — API key
- **Type** — API platform (full billing data)
- **Tracks**:
  - Coding models
  - 5-hour token usage percentage
  - Monthly usage
  - Per-model: requests, input/output/reasoning/cached tokens, cost (USD), tools
  - Tool usage: web search, web fetch, other
  - Credits: available, used (USD)
  - Credit grants list
  - Grants expiring in ≤30 days
  - Subscription status

## Setup

### Auto-detection

`ZAI_API_KEY` and `ZHIPUAI_API_KEY` are not interchangeable — they create **separate accounts**. Setting `ZAI_API_KEY` produces an account with id `zai` configured for the global region (`api.z.ai`); setting `ZHIPUAI_API_KEY` produces an account with id `zhipuai-auto` for the China region (`open.bigmodel.cn`). Both can be active simultaneously and will appear as separate tiles.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "zai",
      "provider": "zai",
      "api_key_env": "ZAI_API_KEY",
      "base_url": "https://api.z.ai"
    }
  ]
}
```

## Regional endpoints

Z.AI has two regions:

| Region | `base_url` | Notes |
|--------|------------|-------|
| Global | `https://api.z.ai` | Default |
| China | `https://open.bigmodel.cn` | Used with `ZHIPUAI_API_KEY` |

## Data sources & how each metric is computed

Z.AI splits its surface across two base URLs: a **coding** base for the model catalog and a **monitor** base for usage/credit data. Both are derived from the configured `base_url`.

| Region | Coding base | Monitor base |
|---|---|---|
| Global | `https://api.z.ai/api/coding/paas/v4` | `https://api.z.ai` |
| China | `https://open.bigmodel.cn/api/coding/paas/v4` | `https://open.bigmodel.cn` |

Each poll (default every 30 seconds in daemon mode) hits up to five endpoints. All requests use `Authorization: Bearer $ZAI_API_KEY` (or `$ZHIPUAI_API_KEY`).

| Call | Endpoint | What it provides |
|---|---|---|
| 1 | `GET <coding>/models` | Coding model catalog |
| 2 | `GET <monitor>/api/monitor/usage/quota/limit` | 5h window usage % + active subscription |
| 3 | `GET <monitor>/api/monitor/usage/model-usage` | Per-model request, token, cost samples |
| 4 | `GET <monitor>/api/monitor/usage/tool-usage` | Web search, web fetch, other tool invocations |
| 5 | `GET <monitor>/api/paas/v4/user/credit_grants` | Credit grants list with expiries |

### Coding model catalog

- Source: `data[].id` from `<coding>/models`.
- Transform: stored under `Raw["coding_models"]`. The detail view renders one row per model.

### `5h_window` — 5-hour rolling token usage

- Source: the `quota/limit` JSON. The body is wrapped in a monitor envelope; the inner data carries the rolling 5-hour percentage and remaining tokens.
- Transform: percentage stored as `Used`/`Remaining` against `Limit = 100`. The window is rolling — not aligned to wall-clock — so heavy bursts push the gauge up quickly.

### Subscription status

- Source: a flag in the `quota/limit` response.
- Transform: stored as `Attributes["subscription_status"]`. When no coding package is active, the value is `inactive_or_free` and the tile flags it.

### Per-model rows

- Source: rows under `data` of `model-usage`. Each row carries a model name, request count, input/output/reasoning/cached tokens, cost in USD, and tool calls.
- Transform: aggregated into `usageRollup` totals per model and emitted as detail rows. Reasoning and cached tokens are kept separate from input/output. Cost is in USD even on China endpoints.

### Tool usage (`web_search`, `web_fetch`, other)

- Source: `tool-usage` response.
- Transform: counted by name into `Metrics["tool_web_search"]`, `Metrics["tool_web_fetch"]`, and an aggregate `tool_other` for everything else.

### `credits_available` / `credits_used` and grants

- Source: `credit_grants` response. Each grant has an amount, used amount, and an `expire_at`.
- Transform: aggregate `available` and `used` are exposed as a single credit metric in USD. Each individual grant becomes a detail row; grants whose `expire_at` is within 30 days are flagged with a warning indicator.

### Auth status

- Source: HTTP status code on any of the calls. `401`/`403` → `auth`; `429` → `limited`; otherwise `ok`. Plus monitor envelopes carry their own success flag — when false, the `quota/limit` call sets `noPackage` which becomes the `inactive_or_free` subscription state.

### What's NOT tracked

- **Daily spend chart.** The monitor endpoints return totals and recent samples; no daily-spend series is produced.
- **Tool call cost.** `tool-usage` reports counts, not per-call cost.

### How fresh is the data?

- Polled every 30 s by default. The monitor surfaces are themselves rolling aggregates with their own update cadence.

## API endpoints used

- `GET <coding>/models`
- `GET <monitor>/api/monitor/usage/quota/limit`
- `GET <monitor>/api/monitor/usage/model-usage`
- `GET <monitor>/api/monitor/usage/tool-usage`
- `GET <monitor>/api/paas/v4/user/credit_grants`

## Caveats

:::note
The 5-hour window is rolling, not aligned to the wall clock. Heavy bursts of activity will push the gauge up quickly.
:::

- Subscription status reads `inactive_or_free` if no coding package is active.
- Per-model cost is reported in USD even on China endpoints; reconcile against your invoice.
- Reasoning and cached tokens are tracked separately from input/output.

## Troubleshooting

- **Subscription `inactive_or_free`** — purchase a coding package in the Z.AI console.
- **No tool usage** — the account has not made web-search or web-fetch calls yet.
- **Wrong region** — switch between `api.z.ai` and `open.bigmodel.cn` and the matching env var.

### "no package" or rejected key

You are pointing at the wrong region. A `ZAI_API_KEY` issued for `api.z.ai` won't authenticate against `open.bigmodel.cn`, and `ZHIPUAI_API_KEY` is the China-region equivalent. Update `base_url` to match the console that issued the key.
