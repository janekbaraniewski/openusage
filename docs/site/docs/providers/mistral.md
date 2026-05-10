---
title: Mistral AI
description: Track Mistral monthly budget, credit balance, spend, and tokens in OpenUsage.
sidebar_label: Mistral AI
---

# Mistral AI

Full billing visibility for Mistral AI. Surfaces the monthly budget, credit balance, monthly spend, token totals, and rate limits — all in EUR.

## At a glance

- **Provider ID** — `mistral`
- **Detection** — `MISTRAL_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (full billing data)
- **Tracks**:
  - Plan
  - Monthly budget (EUR)
  - Credit balance (EUR)
  - Monthly spend
  - Monthly tokens (input and output)
  - RPM and TPM

## Setup

### Auto-detection

Set `MISTRAL_API_KEY`.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "mistral",
      "provider": "mistral",
      "api_key_env": "MISTRAL_API_KEY"
    }
  ]
}
```

## Data sources & how each metric is computed

Each poll (default every 30 seconds in daemon mode) makes three calls under `https://api.mistral.ai/v1`. All requests use `Authorization: Bearer $MISTRAL_API_KEY`.

| Call | Endpoint | What it provides |
|---|---|---|
| 1 | `GET /billing/subscription` | Plan name, monthly budget cap, credit balance |
| 2 | `GET /billing/usage?start_date=YYYY-MM-01&end_date=<today>` | Daily spend & tokens for the current month |
| 3 | `GET /models` | Rate-limit headers (RPM, TPM) |

### `monthly_budget` — plan cap

- Source: `monthly_budget` field of `/billing/subscription`.
- Transform: copied verbatim into `Limit`. Currency: EUR.

### `credit_balance` — remaining credits

- Source: `credit_balance` field of `/billing/subscription`.
- Transform: copied verbatim into `Remaining`. Currency: EUR.

### `monthly_spend` — month-to-date cost

- Source: `total_cost` from `/billing/usage` for `start_date = first of the current UTC month`, `end_date = today`.
- Transform: stored as `Used`. If `monthly_budget` is known, `Limit` is set to it and `Remaining = Limit - Used`. Currency: EUR.

### `monthly_input_tokens` / `monthly_output_tokens`

- Source: sum of `input_tokens` / `output_tokens` across every entry in `data[]` returned by `/billing/usage` for the current month.
- Transform: simple row-by-row sum. Stored as raw token counts.

### `rpm` / `tpm` — rate limits

- Source: response headers on `GET /v1/models`. Three header groups are read:
  - **Primary `rpm`** — `ratelimit-limit`, `ratelimit-remaining`, `ratelimit-reset` (no `x-` prefix).
  - **Primary `tpm`** — `x-ratelimit-limit-tokens`, `x-ratelimit-remaining-tokens`, `x-ratelimit-reset-tokens`.
  - **`rpm_alt`** — `x-ratelimit-limit-requests`, `x-ratelimit-remaining-requests`, `x-ratelimit-reset-requests`. Mistral occasionally returns this alongside the primary headers; OpenUsage exposes it as a separate metric so both are visible.
- Transform: parsed verbatim into the corresponding metrics.

### Auth status

- Source: HTTP status code on any of the three endpoints. `401`/`403` → `auth`; `429` → `limited`; otherwise `ok`.

### What's NOT tracked

- **Per-model breakdown.** `/billing/usage` returns daily aggregates; the provider sums them month-to-date and does not split by model.

### How fresh is the data?

- Polled every 30 s by default. The `/billing/usage` totals are themselves aggregates Mistral updates on its own cadence — typically a few minutes behind real time.

## API endpoints used

- `GET /v1/billing/subscription`
- `GET /v1/billing/usage?start_date=…&end_date=…`
- `GET /v1/models`

## Caveats

:::warning
Mistral bills in **EUR**. Mixing it with USD-billed providers in a single dashboard requires you to convert manually.
:::

- The billing period is the calendar month; numbers reset at midnight UTC on the 1st.
- Rate-limit headers come from `/v1/models`.

## Troubleshooting

- **No spend data** — verify the API key has billing scope; check Mistral's console.
- **Currency confusion** — Mistral always reports EUR; OpenUsage displays whatever the API returns.

### Why doesn't monthly spend match the Mistral console exactly?

The dashboard sums `data[].total_cost` from `/v1/billing/usage` for `[first-of-month, today]`. Mistral's console can include same-day usage that hasn't aggregated yet, or apply different rounding. Refresh after the next aggregation pass.
