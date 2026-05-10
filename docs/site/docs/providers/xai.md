---
title: xAI (Grok)
description: Track xAI Grok credits, rate limits, and allowed models in OpenUsage.
sidebar_label: xAI
---

# xAI (Grok)

Full account visibility for xAI. Surfaces key metadata, credit breakdown, rate limits, and the list of models the key is allowed to call.

## At a glance

- **Provider ID** — `xai`
- **Detection** — `XAI_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (full billing data)
- **Tracks**:
  - Key info: name, team
  - Credits: remaining, spent, granted (USD)
  - RPM and TPM
  - Allowed models

## Setup

### Auto-detection

Set `XAI_API_KEY`.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "xai",
      "provider": "xai",
      "api_key_env": "XAI_API_KEY",
      "base_url": "https://api.x.ai/v1"
    }
  ]
}
```

## Data sources & how each metric is computed

Each poll (default every 30 seconds in daemon mode) makes two calls under `https://api.x.ai/v1`. All requests use `Authorization: Bearer $XAI_API_KEY`.

| Call | Endpoint | What it provides |
|---|---|---|
| 1 | `GET /api-key` | Key metadata, credit breakdown, allowed models |
| 2 | `GET /models` | Rate-limit headers |

### Key metadata

- Source: top-level fields of the `/api-key` JSON: `name`, `team_id`, `id`.
- Transform: stored under `Raw["api_key_name"]`, `Raw["team_id"]`. Used for the tile heading.

### `credits` — combined credit metric

- Source: `/api-key` fields `remaining_balance`, `spent_balance`, `total_granted`.
- Transform: copied as `Remaining`, `Used`, `Limit` of a single metric. Currency is fixed at USD.
- The status message becomes `$X.XX remaining` (formatted from `Remaining`).

### Allowed models

- Source: `allowed_models` array on `/api-key`.
- Transform: stored as `Raw["allowed_models"]`. The detail view lists them; calls to other models fail at xAI's edge.

### `rpm` / `tpm` — rate limits

- Source: response headers on `GET /v1/models`
  - `x-ratelimit-limit-requests`, `x-ratelimit-remaining-requests`, `x-ratelimit-reset-requests`
  - `x-ratelimit-limit-tokens`, `x-ratelimit-remaining-tokens`, `x-ratelimit-reset-tokens`
- Transform: parsed verbatim.

### Auth status

- Source: HTTP status code. `401`/`403` → `auth`; `429` → `limited`; otherwise `ok`.

### What's NOT tracked

- **Promo vs paid split.** `total_granted` lumps promotional and paid credits together. The API does not break them apart.
- **Per-model spend.** The credit endpoint returns aggregate dollars only.

### How fresh is the data?

- Polled every 30 s by default. The credit endpoint reflects xAI's near-real-time accounting.

## API endpoints used

- `GET /v1/api-key`
- `GET /v1/models`

## Caveats

- Granted credits include both promotional and paid; the API does not split them further.
- Allowed models reflect the key's scope, not the team's full catalog.
- Currency is USD.

## Troubleshooting

- **Empty allowed models** — the key has no model permissions; create a new key with model access in the xAI console.
- **Spend higher than expected** — xAI charges for both successful and certain failed requests; check the console for itemized billing.
