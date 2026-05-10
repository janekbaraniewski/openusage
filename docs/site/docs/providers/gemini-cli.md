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

## Data sources & how each metric is computed

Gemini CLI has two data paths:

1. **Local files** under `~/.gemini/` — the authoritative source for OAuth status, account email, conversation count, MCP config, and session token usage.
2. **Optional Cloud Code RPCs** — `loadCodeAssist` and `retrieveUserQuota` against `https://cloudcode-pa.googleapis.com/v1internal/`. Provides Google's view of tier/quota for your account. Requires the OAuth access token from `oauth_creds.json` (refreshed automatically when expired) plus a Google Cloud project ID either from `extra.config_dir`'s settings or the `GOOGLE_CLOUD_PROJECT` / `GOOGLE_CLOUD_PROJECT_ID` env var.

### OAuth status

- Source: `~/.gemini/oauth_creds.json`. Fields: `access_token`, `refresh_token`, `expiry_date` (Unix millis), `scope`.
- Transform: status is computed from `expiry_date - now`:
  - missing / unreadable → `auth` (no creds)
  - expired with `refresh_token` → background refresh against `https://oauth2.googleapis.com/token`; status remains `ok` if refresh succeeds.
  - otherwise `ok`. The scope string is stored verbatim.

### Account email

- Source: `~/.gemini/google_accounts.json` `active` field.
- Transform: stored as `Attributes["account_email"]`.

### Conversation count

- Source: count of `*.pb` files under `~/.gemini/antigravity/conversations/`. The provider decodes only the protobuf headers; it does not store transcript bodies.
- Transform: stored as `Metrics["total_conversations"]` (`Used = file count`).

### Session token usage (input / output / cached / reasoning / tool)

- Source: `~/.gemini/tmp/session_*.json` files. Each session's last-known token totals are read from the JSON.
- Transform: aggregated across sessions:
  - `session_input_tokens`, `session_output_tokens`, `session_cached_tokens`, `session_reasoning_tokens`, `session_tool_tokens`.
  - Per-model and per-client breakdowns where the session metadata identifies them.

### MCP configuration

- Source: `~/.gemini/settings.json` `mcpServers` map plus `~/.gemini/mcp-server-enablement.json`.
- Transform: count of enabled MCP servers stored as a metric; the list is rendered as detail rows.

### Install ID, version

- Source: `~/.gemini/installation_id` and the `gemini` binary version output.
- Transform: stored as snapshot attributes (`install_id`, `cli_version`).

### Quota (when enabled)

- Source: `POST https://cloudcode-pa.googleapis.com/v1internal/loadCodeAssist` returns the current tier; `POST .../retrieveUserQuota` returns per-tier quotas. Each bucket carries `remainingAmount` and `remainingFraction`; `used` and `limit` are derived (`limit = 100`, `used = 100 - remainingFraction * 100`).
- Transform: each quota becomes a metric (`quota_<name>`) with `Limit = 100`, `Remaining = remainingFraction * 100`, `Used = 100 - Remaining`, `Unit = %`. The active tier is stored as `Attributes["tier"]`. When the response indicates `< 15%` remaining on any quota, status promotes to `near_limit`.

### Auth status (composite)

- Source: combines OAuth status + Cloud Code call status. A missing project ID produces an `auth` warning only on the Cloud Code call; local data continues to render.

### What's NOT tracked

- **$ spend.** Google's free-tier Gemini CLI is not metered to the user, and the Cloud Code RPCs return quota counts, not dollars.
- **Full conversation content.** Protobuf bodies are not parsed beyond the header.

### How fresh is the data?

- Polled every 30 s by default. OAuth refresh runs at most once per poll. Conversation files and session JSONs are re-read each poll; counts update as the CLI writes them.

## API endpoints used

- `POST https://cloudcode-pa.googleapis.com/v1internal/loadCodeAssist` — tier discovery
- `POST https://cloudcode-pa.googleapis.com/v1internal/retrieveUserQuota` — per-tier quota counters
- `POST https://oauth2.googleapis.com/token` — refresh-token exchange (only when access token is expired)

## Files read

- `~/.gemini/oauth_creds.json` — OAuth tokens
- `~/.gemini/google_accounts.json` — account list
- `~/.gemini/settings.json` — CLI settings + MCP servers
- `~/.gemini/installation_id` — install ID
- `~/.gemini/antigravity/conversations/**/*.pb` — conversation history (protobuf, headers only)
- `~/.gemini/tmp/session_*.json` — session transcripts
- `~/.gemini/mcp-server-enablement.json` — MCP enable flags

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
