---
title: Command Code
description: Track Command Code credit balance, 5-hour and weekly usage windows, and spend in OpenUsage.
sidebar_label: Command Code
keywords: [command code usage tracker, command code quota tracking, command code credits, command code api key, track command code spend]
---

# Command Code

Provider for [Command Code](https://commandcode.ai), the coding agent and its Provider API. It reports the account-level billing picture behind the CLI's `/usage` command: monthly credit balance, the rolling 5-hour and weekly spend windows, plan identity, and billing-period spend, requests and tokens.

## At a glance

- **Provider ID** — `command_code`
- **Detection** — `~/.commandcode/auth.json` exists, `~/.commandcode/` exists, or a `command-code` / `cmd` / `cmdc` binary is on `PATH`
- **Auth** — API key (`COMMAND_CODE_API_KEY`), or the key the CLI stores at `~/.commandcode/auth.json`
- **Type** — coding agent / API platform
- **Tracks**:
  - Credit balance (monthly included credits remaining, plus purchased/free extra credits)
  - Rolling 5-hour and weekly usage windows — percent used, cap, and reset time
  - Plan name/id, subscription status, current billing period
  - Billing-cycle and today's spend, requests, and input/output/total tokens

## Setup

### Auto-detection

OpenUsage registers the provider when the Command Code CLI has been used on this machine: `~/.commandcode/auth.json`, `~/.commandcode/`, or an installed `command-code` / `cmd` / `cmdc` binary. When `~/.commandcode/auth.json` exists, the CLI's API key is adopted automatically, so no configuration is required.

### Environment variable

```bash
export COMMAND_CODE_API_KEY="cmd_..."
```

`COMMAND_CODE_API_KEY` wins over the stored CLI key and is the recommended path for CI or headless use.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "command_code",
      "provider": "command_code",
      "api_key_env": "COMMAND_CODE_API_KEY"
    }
  ]
}
```

Set `base_url` to override the API host (default `https://api.commandcode.ai`). The provider accepts an `auth_file` path override when the CLI stores its key somewhere other than `~/.commandcode/auth.json`.

## Data sources & how each metric is computed

Authenticated with `Authorization: Bearer <key>` against the same billing surface the CLI reads:

| Endpoint                              | Used for                                            |
| ------------------------------------- | --------------------------------------------------- |
| `/alpha/whoami?limits=1`              | Account email/username, organization (and org id)   |
| `/alpha/billing/credits`              | Credit balance and the rolling usage windows        |
| `/alpha/billing/subscriptions`        | Plan id, subscription status, billing period        |
| `/alpha/usage/summary`                | Billing-period spend/requests/tokens                |
| `/alpha/usage/summary?since=<ISO8601>` | Today's spend and requests                          |

When the account belongs to an organization, `orgId` is added to the billing and summary requests.

### Field mapping

| Upstream field                                   | openusage metric         |
| ------------------------------------------------ | ------------------------ |
| `credits.monthlyCredits` + `purchasedCredits` + `freeCredits` | `credit_balance` (`Remaining`) |
| plan included monthly credits + extras           | `credit_balance` (`Limit`) |
| `credits.purchasedCredits` + `freeCredits`       | `extra_credits`          |
| `windowLimits.fiveHour.used / cap`               | `usage_five_hour` (% used) |
| `windowLimits.weekly.used / cap`                 | `usage_seven_day` (% used) |
| `windowLimits.*.resetAt` (epoch ms)              | reset entry for the matching window |
| `summary.totalCost` (billing period)             | `monthly_spend`          |
| `summary.totalCost` (`since` = local midnight)   | `today_spend`            |
| `summary.totalTokens` / `totalTokensIn` / `totalTokensOut` | `total_tokens` / `input_tokens` / `output_tokens` |
| `summary.totalCount`                             | `requests`, `requests_today` |

`usage_five_hour` and `usage_seven_day` are percentage gauges (`Used` / `Limit = 100`), matching the meters shown by `cmd`'s `/usage`.

### Plans and credit pools

The included monthly credit pool is derived from the subscription's `planId` (Go 10, GOAT 70, Pro 30/80, Provider 15, Max 150, Ultra 300, Teams Pro 40) and used only as the denominator for `credit_balance`. Window caps come from the API — nothing is assumed.

## Caveats

- **Alpha endpoints.** The billing routes are Command Code's internal CLI API (`/alpha/...`), not the documented Provider API (`/provider/v1/...`). They can change without notice; the provider records failures in the snapshot diagnostics rather than failing the whole tile when a secondary route breaks.
- **No per-model breakdown.** `/alpha/usage/summary` is account-aggregate only, so the Analytics model table stays empty for this provider.
- **Extra credits skip the windows.** Pay-as-you-go credits are never capped, so an account funded only by extra credits reports no window usage.

## Troubleshooting

- **Tile says "AUTH_REQUIRED"** — run `cmd login`, or export `COMMAND_CODE_API_KEY`. Confirm the CLI sees the key with `cmd status`.
- **"rate limited (HTTP 429)"** — the billing API throttled the poll; data refreshes on the next cycle.
- **Windows missing** — the plan exposes no rolling windows (for example a pure pay-as-you-go Provider account); only the credit balance is shown.
- **Spend shows only the billing period** — today's summary is fetched separately; if that call fails the diagnostic `today_summary_error` is recorded and the billing-period figures remain.

## Related

- [Codex CLI](./codex.md) — another local coding-agent provider with an API-key fallback
- [Z.AI](./zai.md) — sibling provider with rolling 5-hour and monthly windows
