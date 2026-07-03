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
- **Detection** — `AZURE_OPENAI_API_KEY` or `AZURE_API_KEY` environment variable
- **Auth** — API key (`api-key` header)
- **Type** — API platform (header-only rate limits)
- **Tracks**:
  - RPM and TPM rate limits when the resource returns them
  - Auth / connectivity status

## Setup

Azure OpenAI needs two things: an API key **and** your resource location.

### Auto-detection

Set an API key and your resource name. OpenUsage registers the provider on next
start.

```bash
export AZURE_API_KEY="<your-key>"          # or AZURE_OPENAI_API_KEY
export AZURE_RESOURCE_NAME="my-resource"   # builds https://my-resource.openai.azure.com
```

These are the same environment variables [OpenCode](./opencode.md) uses, so a
single configuration drives both tools — see [Using with OpenCode](#using-with-opencode).

The API key is accepted from either `AZURE_API_KEY` or `AZURE_OPENAI_API_KEY`
(when both are set, `AZURE_OPENAI_API_KEY` wins). The endpoint is resolved in
this order:

1. the account's `base_url`,
2. `AZURE_OPENAI_ENDPOINT`,
3. `AZURE_RESOURCE_NAME` → `https://<name>.openai.azure.com`.

Use `base_url` or `AZURE_OPENAI_ENDPOINT` (full URL) for **non-standard
endpoints** — sovereign clouds (e.g. `*.openai.azure.us`) or custom domains —
where the `*.openai.azure.com` template does not apply. If none of the three is
set, the tile reports that an endpoint must be configured.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "azure-openai",
      "provider": "azure_openai",
      "api_key_env": "AZURE_API_KEY",
      "base_url": "https://my-resource.openai.azure.com"
    }
  ]
}
```

## Data sources & how each metric is computed

OpenUsage sends one `GET {endpoint}/openai/deployments?api-version=2024-10-21` per poll cycle (default every 30 seconds in daemon mode). Listing deployments is a read-only, non-billable operation that validates auth and connectivity and exposes any rate-limit headers the resource attaches.

Request headers:

- `api-key: $AZURE_API_KEY` (or `$AZURE_OPENAI_API_KEY`)

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

## Using with OpenCode

When you drive Azure OpenAI **through [OpenCode](./opencode.md)** rather than
calling the API directly, the spend and token usage come from OpenCode's
telemetry, which tags those events with the provider id `azure`. OpenUsage ships
a built-in provider link — `azure` → `azure_openai` — so that usage is
**automatically attributed to the Azure OpenAI tile with no extra
configuration**. (This is the same mechanism that routes OpenCode's Gemini usage
to the Gemini tile via `google` → `gemini_api`.)

The two data paths complement each other on the same tile:

- **Direct probe** (this provider) → RPM/TPM rate limits.
- **OpenCode telemetry** (via the `azure` link) → per-model token usage and cost.

Because OpenUsage and OpenCode share the `AZURE_API_KEY` / `AZURE_RESOURCE_NAME`
environment variables, one set of exports configures both. If you had previously
added a manual `azure` → `azure_openai` entry under `telemetry.provider_links`,
you can remove it — the default now covers it.

## Troubleshooting

- **Endpoint not configured** — set `AZURE_RESOURCE_NAME` (e.g. `my-resource`), or `AZURE_OPENAI_ENDPOINT` / the account's `base_url` to the full `https://<resource>.openai.azure.com`.
- **Auth failed** — verify `AZURE_API_KEY` (or `AZURE_OPENAI_API_KEY`) matches a key from the resource's *Keys and Endpoint* blade; rotate if leaked.
- **No RPM/TPM data** — the resource may not attach rate-limit headers to the deployments listing; the tile still reports connectivity.
- **Azure usage shows on a different tile / as unmapped** — make sure the Azure OpenAI tile exists (set `AZURE_API_KEY` so the provider is detected); OpenCode's `azure`-tagged telemetry only attaches once an `azure_openai` account is configured.

## Related

- [OpenAI](./openai.md) — the same models via `api.openai.com`
- [Codex CLI](./codex.md) — OpenAI's coding agent with local session and credit data
