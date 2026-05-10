---
title: Anthropic
description: Track Anthropic API rate limits in OpenUsage.
sidebar_label: Anthropic
---

# Anthropic

Header-only rate-limit probe for the Anthropic API. OpenUsage sends a minimal request to `/v1/messages` and reads RPM/TPM headers from the response.

## At a glance

- **Provider ID** — `anthropic`
- **Detection** — `ANTHROPIC_API_KEY` environment variable
- **Auth** — API key
- **Type** — API platform (header-only rate limits)
- **Tracks**:
  - RPM and TPM rate limits (limit, remaining, reset)
  - Auth status

## Setup

### Auto-detection

Set `ANTHROPIC_API_KEY`. OpenUsage registers the provider on next start.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "anthropic",
      "provider": "anthropic",
      "api_key_env": "ANTHROPIC_API_KEY",
      "base_url": "https://api.anthropic.com/v1"
    }
  ]
}
```

Set `base_url` for proxies or compatible gateways.

## Data sources & how each metric is computed

OpenUsage sends one `POST https://api.anthropic.com/v1/messages` per poll cycle (default every 30 seconds in daemon mode). The body is minimal so Anthropic responds with HTTP 400, but the response **headers** carry rate-limit data and that is all this provider reads. The body is discarded.

Request headers:

- `x-api-key: $ANTHROPIC_API_KEY`
- `anthropic-version: 2023-06-01`
- `Content-Type: application/json`

### `rpm` — requests per minute

- Source: response headers
  - `anthropic-ratelimit-requests-limit`
  - `anthropic-ratelimit-requests-remaining`
  - `anthropic-ratelimit-requests-reset`
- Transform: copied verbatim into the metric's `Limit` and `Remaining`. The reset string is parsed as RFC3339 and stored as `Resets["rpm"]`.
- Window: 1 minute.

### `tpm` — tokens per minute

- Source: response headers
  - `anthropic-ratelimit-tokens-limit`
  - `anthropic-ratelimit-tokens-remaining`
  - `anthropic-ratelimit-tokens-reset`
- Transform: same as `rpm` but for tokens.

### Auth status

- Source: HTTP status code of the probe.
- Transform: `401`/`403` → `auth`; `429` → `limited`; otherwise `ok`. The 400 that the empty-body probe triggers still carries valid rate-limit headers, so the tile reads `ok`.

### What's NOT tracked

- **Spend / cost.** The API does not expose dollar figures or usage totals to API tokens, and there is no billing endpoint a key can authenticate against. Install [Claude Code](./claude-code.md) for token-level cost estimates from local session logs.
- **Per-model breakdown.** The probe is a single request; the headers reflect your active tier, not a model-by-model split.

### How fresh is the data?

- Polled every 30 s by default (`data.poll_interval`). Each poll is one request, no cache.

## API endpoints used

- `POST /v1/messages` — header-only probe with `anthropic-version: 2023-06-01`

## Caveats

:::note
Anthropic's API does not expose spend or token-usage data to API keys. For full visibility install [Claude Code](./claude-code.md), which reads local sessions and computes per-model costs.
:::

- Rate limits come from response headers and reflect the active tier.
- The probe is a single minimal request per poll — negligible cost.

## Troubleshooting

- **Auth failed** — verify `ANTHROPIC_API_KEY` and rotate if necessary.
- **Stale reset times** — Anthropic rolls reset windows; the next poll picks up the new value.

### Why is there no $ spend?

The Anthropic API does not return spend or token-usage data on response headers, and there is no per-key billing endpoint we can authenticate against. The Claude Code provider closes that gap by reading on-disk session logs and multiplying token counts by published pricing.

## Related

- [Claude Code](./claude-code.md) — local sessions, billing blocks, burn rate for the same models
