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
      "base_url": "https://api.anthropic.com"
    }
  ]
}
```

Set `base_url` for proxies or compatible gateways.

## What you'll see

- Dashboard tile shows auth status and the most-constrained rate-limit gauge.
- Detail view splits RPM and TPM into limit, remaining, and reset time.

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

## Related

- [Claude Code](./claude-code.md) — local sessions, billing blocks, burn rate for the same models
