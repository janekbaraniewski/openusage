---
title: OpenCode
description: Track OpenCode auth, available zen models, and spend via the telemetry plugin in OpenUsage.
sidebar_label: OpenCode
---

# OpenCode

Tracks the OpenCode tool's auth status and available models. Spend and per-session activity come from the OpenCode telemetry plugin, not the public API.

## At a glance

- **Provider ID** — `opencode`
- **Detection** — `ZEN_API_KEY` or `OPENCODE_API_KEY` environment variable
- **Auth** — API key
- **Type** — coding agent
- **Tracks**:
  - Auth status
  - Available zen models with `owned_by` metadata
  - Spend and activity (only via the telemetry plugin)

## Setup

### Auto-detection

Set either `ZEN_API_KEY` or `OPENCODE_API_KEY`. Both work; the first non-empty value wins.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "opencode",
      "provider": "opencode",
      "api_key_env": "ZEN_API_KEY",
      "base_url": "https://opencode.ai"
    }
  ]
}
```

## What you'll see

- Dashboard tile shows auth status and the count of available zen models.
- Detail view lists model IDs with their `owned_by` field.
- Spend and per-session metrics appear **only** when the OpenCode telemetry plugin streams events into the OpenUsage daemon.

## API endpoints used

- `GET /zen/v1/models`

## Caveats

:::tip
To see spend, install the OpenCode telemetry plugin and run OpenUsage in daemon mode. See [Daemon integrations](../daemon/integrations.md).
:::

- Without telemetry the tile shows model availability only; this is expected.
- `base_url` defaults to `https://opencode.ai`.

## Troubleshooting

- **No models listed** — verify the API key is valid and not rate-limited.
- **Empty spend tile** — install and configure the OpenCode telemetry plugin; see daemon docs.
