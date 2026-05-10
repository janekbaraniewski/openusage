---
title: Gemini CLI
description: Track Gemini CLI OAuth sessions, token usage, MCP config, and user quota in OpenUsage.
sidebar_label: Gemini CLI
---

# Gemini CLI

Tracks the Google Gemini CLI from local files. OAuth credentials and session logs feed token counts and conversation activity; an optional Cloud Code endpoint adds user-quota data.

## At a glance

- **Provider ID** — `gemini_cli`
- **Detection** — `gemini` binary on `PATH` plus `~/.gemini/`
- **Auth** — OAuth in `~/.gemini/oauth_creds.json` (refresh tokens supported)
- **Type** — coding agent
- **Tracks**:
  - OAuth status and scope
  - Account email
  - Auth type and install ID
  - Conversation count
  - Session usage: input, output, cached, reasoning, tool tokens
  - MCP configuration
  - Version

## Setup

### Auto-detection

OpenUsage requires both the `gemini` binary on `PATH` and the `~/.gemini/` directory. The CLI creates the directory after the first run.

Optional environment variables consulted when present:

- `GOOGLE_CLOUD_PROJECT`
- `GOOGLE_CLOUD_PROJECT_ID`

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "gemini_cli",
      "provider": "gemini_cli",
      "binary": "/usr/local/bin/gemini",
      "extra": {
        "config_dir": "~/.gemini"
      }
    }
  ]
}
```

## What you'll see

- Dashboard tile shows OAuth status, account email, and today's conversation count.
- Detail view breaks token usage into input, output, cached, reasoning, and tool buckets.
- MCP server list and CLI version appear as secondary metrics.

## API endpoints used

- Optional: `POST cloudcode-pa.googleapis.com/v1internal/retrieveUserQuota` — populated when a Google Cloud project is configured

## Files read

- `~/.gemini/oauth_creds.json` — OAuth tokens
- `~/.gemini/google_accounts.json` — account list
- `~/.gemini/settings.json` — CLI settings
- `~/.gemini/installation_id` — install ID
- `~/.gemini/antigravity/conversations/**/*.pb` — conversation history (protobuf)
- `~/.gemini/tmp/session_*.json` — session transcripts
- `~/.gemini/mcp-server-enablement.json` — MCP config

## Caveats

- Without a Google Cloud project, user-quota data is unavailable; local session counts still work.
- Refresh tokens are honored automatically; you should never need to re-authenticate.
- Conversation files are protobuf-encoded; OpenUsage decodes the headers it needs but does not store full transcripts.

## Troubleshooting

- **OAuth status: expired** — run `gemini` once to refresh; if that fails, re-authenticate with `gemini auth login`.
- **No quota data** — set `GOOGLE_CLOUD_PROJECT` and re-run.
- **Token counts missing** — check that `~/.gemini/tmp/session_*.json` files are being written.

## Related

- [Gemini API](./gemini-api.md) — track raw API usage for the same models
