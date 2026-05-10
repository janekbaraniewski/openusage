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

Set either `ZAI_API_KEY` or `ZHIPUAI_API_KEY`. Both work; the first non-empty value wins.

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

## What you'll see

- Dashboard tile shows the 5-hour window percentage and subscription status.
- Detail view lists per-model rows with request count, token breakdown, cost, and tool calls.
- Tool usage section breaks out web search, web fetch, and other tool invocations.
- Credit grants are listed individually; grants expiring within 30 days are highlighted.

## API endpoints used

- `/api/coding/paas/v4/models`
- `/api/monitor/usage/quota/limit`
- `/api/monitor/usage/model-usage`
- `/api/monitor/usage/tool-usage`
- `/api/paas/v4/user/credit_grants`

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
