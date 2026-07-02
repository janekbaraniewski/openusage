---
title: Azure OpenAI
description: Track Azure OpenAI Service rate limits and quotas in OpenUsage.
sidebar_label: Azure OpenAI
keywords: [azure openai usage tracker, azure openai quota tracking, azure openai rate limits, track azure openai spend locally]
---

# Azure OpenAI

Lightweight rate-limit probe for [Azure OpenAI Service](https://learn.microsoft.com/azure/ai-services/openai/). OpenUsage issues a single header-only request to your Azure resource and parses any RPM and TPM limits it returns — no billing data, no token counts.

Azure OpenAI serves the same models as OpenAI but through a different endpoint layout and auth scheme, so it has its own provider tile rather than reusing the [OpenAI](./openai.md) one.

## At a glance

- **Provider ID** — `azure_openai`
- **Detection** — `AZURE_OPENAI_API_KEY` environment variable
- **Auth** — API key (`api-key` header)
- **Type** — API platform (header-only rate limits)
- **Tracks**:
  - RPM and TPM rate limits when the resource returns them
  - Auth / connectivity status

## Setup

Azure OpenAI needs two things: an API key **and** your resource endpoint.

### Auto-detection

Set both `AZURE_OPENAI_API_KEY` and `AZURE_OPENAI_ENDPOINT`. OpenUsage registers the provider on next start; the endpoint is read from `AZURE_OPENAI_ENDPOINT` when the account has no `base_url`.

```bash
export AZURE_OPENAI_API_KEY="<your-key>"
export AZURE_OPENAI_ENDPOINT="https://my-resource.openai.azure.com"
```

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "azure-openai",
      "provider": "azure_openai",
      "api_key_env": "AZURE_OPENAI_API_KEY",
      "base_url": "https://my-resource.openai.azure.com"
    }
  ]
}
```

`base_url` is your Azure OpenAI resource endpoint. If it is omitted, OpenUsage falls back to the `AZURE_OPENAI_ENDPOINT` environment variable. If neither is set, the tile reports that an endpoint must be configured.

## Data sources & how each metric is computed

OpenUsage sends one `GET {endpoint}/openai/deployments?api-version=2024-10-21` per poll cycle (default every 30 seconds in daemon mode). Listing deployments is a read-only, non-billable operation that validates auth and connectivity and exposes any rate-limit headers the resource attaches.

Request headers:

- `api-key: $AZURE_OPENAI_API_KEY`

### `rpm` — requests per minute

- Source: response headers
  - `x-ratelimit-limit-requests`
  - `x-ratelimit-remaining-requests`
  - `x-ratelimit-reset-requests`
- Transform: copied verbatim into `Limit` / `Remaining`. Reset is decoded into `Resets["rpm"]`.
- Window: 1 minute.

### `tpm` — tokens per minute

- Source: response headers
  - `x-ratelimit-limit-tokens`
  - `x-ratelimit-remaining-tokens`
  - `x-ratelimit-reset-tokens`
- Transform: same shape as `rpm` but for tokens.

### Auth status

- Source: HTTP status code.
- Transform: `401`/`403` → `auth`; `429` → `limited` (with `retry_after` from `Retry-After` if present); otherwise `ok`.

### What's NOT tracked

- **Spend / cost.** Azure OpenAI does not expose dollar figures on data-plane responses. Cost lives in Azure Cost Management, which requires Azure AD auth and is out of scope for this header probe.
- **Per-deployment token usage.** The probe lists deployments; it does not aggregate usage per deployment.

### How fresh is the data?

- Polled every 30 s by default. One request per poll, no cache.

## API endpoints used

- `GET /openai/deployments?api-version=2024-10-21` — header-only probe (lists deployments).

## Caveats

:::note
Azure attaches `x-ratelimit-*` headers primarily to inference responses. The deployments-listing probe validates auth and connectivity for every resource, but some resources may not return rate-limit headers on it — in that case the tile shows auth/connectivity status without RPM/TPM gauges.
:::

- Rate limits, when present, reflect the resource's quota for the probed operation.
- The probe is a single request per poll cycle — negligible cost, no tokens spent.

## Troubleshooting

- **Endpoint not configured** — set `AZURE_OPENAI_ENDPOINT` or the account's `base_url` to `https://<resource>.openai.azure.com`.
- **Auth failed** — verify `AZURE_OPENAI_API_KEY` matches a key from the resource's *Keys and Endpoint* blade; rotate if leaked.
- **No RPM/TPM data** — the resource may not attach rate-limit headers to the deployments listing; the tile still reports connectivity.

## Related

- [OpenAI](./openai.md) — the same models via `api.openai.com`
- [Codex CLI](./codex.md) — OpenAI's coding agent with local session and credit data
