---
title: GitHub Copilot
description: Track GitHub Copilot quotas, org seats, and rate limits in OpenUsage via the gh CLI.
sidebar_label: Copilot
keywords: [github copilot usage tracker, github copilot quota tracking, github copilot cost tracking, github copilot token usage, track github copilot spend locally]
---

# GitHub Copilot

Wraps the `gh` CLI (or the standalone `copilot` binary) to surface Copilot entitlements, quotas, and org metrics. No GitHub PAT is needed: OpenUsage shells out to commands you've already authorized.

## At a glance

- **Provider ID** — `copilot`
- **Detection** — `gh` CLI with the Copilot extension installed, **or** a standalone `copilot` binary plus `~/.copilot/`
- **Auth** — `gh auth login` (re-uses existing GitHub credentials), or local Copilot CLI state
- **Type** — coding agent
- **Tracks**:
  - User, plan, SKU
  - Chat, code, and premium quotas (entitlement, overage, remaining)
  - Org seats and feature toggles
  - Org metrics: active and engaged users by editor and model
  - Rate limits
  - Local session model and workspace info

## Setup

### Auto-detection

Two paths trigger detection:

1. **gh CLI**: `gh` on `PATH` with the Copilot extension installed (`<gh data dir>/extensions/gh-copilot`) or the Copilot CLI that `gh copilot` downloaded (`<gh data dir>/copilot`). The gh data dir is `$GH_DATA_DIR`, else `$XDG_DATA_HOME/gh`, else `%LOCALAPPDATA%\GitHub CLI` on Windows, else `~/.local/share/gh`. Detection only checks these paths. It never runs `gh copilot`.
2. **Standalone CLI**: a `copilot` binary on `PATH` plus a `~/.copilot/` directory

Run `gh auth status` to confirm you're signed in.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "copilot",
      "provider": "copilot",
      "binary": "/usr/local/bin/gh",
      "extra": {
        "config_dir": "~/.copilot",
        "copilot_binary": "/usr/local/bin/copilot"
      }
    }
  ]
}
```

Set `binary` to the `gh` path; `copilot_binary` is only needed if the standalone CLI lives somewhere unusual.

### GitHub Enterprise Cloud/Server

If your organization uses a GitHub Enterprise Cloud (`*.ghe.com`) or Server hostname instead of `github.com`, set `base_url` on the account. OpenUsage derives the `gh --hostname` flag from it automatically:

```json
{
  "accounts": [
    {
      "id": "copilot",
      "provider": "copilot",
      "binary": "/usr/bin/gh",
      "base_url": "https://my-company.ghe.com"
    }
  ]
}
```

The host must be logged in with `gh` first. OpenUsage never runs `gh auth login` for you, so run it once per host before starting OpenUsage:

```bash
gh auth login --hostname my-company.ghe.com
gh auth status --hostname my-company.ghe.com   # verify
```

If the host is not logged in, the tile shows an auth error ("not authenticated with GitHub").

To track both `github.com` and a GHE host, add two accounts with different `id` values, one with `base_url` and one without. Each account/host pair keeps its own cached auth status and snapshot, so the two never show each other's data.

## Data sources & how each metric is computed

Copilot has two data paths:

1. **`gh` subprocess.** Several `gh api …` calls return user, plan/SKU, rate limits, and (for org admins) org-level billing and metrics.
2. **Local Copilot CLI files.** When the standalone `copilot` binary is installed, additional session metadata is read from `~/.copilot/`.

No direct HTTPS calls are made — everything goes through `gh`, which uses the credentials from `gh auth login`.

### User, plan, SKU

- Source: `gh api /user` and `gh api /copilot_internal/user`.
- Transform: `login`, `id`, `name`, `email` from `/user`; SKU and plan flags from `/copilot_internal/user`. Stored as snapshot attributes.

### Quotas (chat, code, premium): entitlement, overage, remaining

- Source: `gh api /copilot_internal/user` returns `quota_snapshots.{chat,code,premium_interactions}` with `entitlement`, `remaining`, `unlimited`, `overage_count` (int), `overage_permitted` (bool), etc.
- Transform: each quota becomes a metric: `Limit = entitlement`, `Used = entitlement - remaining`, `Remaining = remaining`. `overage_count` and `overage_permitted` are stored separately for the detail row.

### Rate limits (`core`, `search`, `graphql`)

- Source: `gh api /rate_limit` returns `resources.{core,search,graphql}` with `limit`, `remaining`, `reset` (Unix seconds).
- Transform: each is exposed as a metric (`rate_limit_core`, `rate_limit_search`, `rate_limit_graphql`). Reset times go to `Resets[…]`.

### Org seats and feature toggles

- Source: `gh api /orgs/<org>/copilot/billing`.
- Transform: total seats / pending invitations / cancelled seats and the `seat_breakdown` map become detail rows. Feature toggles (e.g. `public_code_suggestions`, `chat`) are stored as attributes.

### Org metrics (active / engaged users by editor and model)

- Source: `gh api /orgs/<org>/copilot/metrics` — returns daily rows of active / engaged users sliced by editor and model.
- Transform: rolled up into `active_users`, `engaged_users` and per-editor / per-model rows. Only available to Copilot Business / Enterprise admins.

### Local sessions (standalone CLI)

- Source: `~/.copilot/session-state/<id>/` directories, each containing `workspace.yaml` plus a JSONL log of session events (`session.start`, `session.model_change`, `session.info`, `session.shutdown`).
- Transform: total sessions, per-client tokens, and last-active workspace are derived. Only present when the standalone `copilot` binary has been used.

### Auth status

- Source: result of `gh auth status` (cached). Failure → snapshot status `auth`.

### What's NOT tracked

- **$ spend per turn.** Copilot is per-seat, so the dashboard exposes seat counts and quota usage rather than dollars per call.
- **Org metrics for non-admin accounts.** GitHub does not return them.

:::note Cost values hidden by default on Individual / Business / Enterprise
Copilot bills per seat on every plan, so per-turn dollar figures are not meaningful. OpenUsage hides cost columns by default on all Copilot plans (Individual, Business, Enterprise) and surfaces quotas, seats, and rate limits instead. Override with [`dashboard.hide_costs`](../reference/configuration.md#dashboardhide_costs) or the <kbd>c</kbd> keystroke.
:::

### How fresh is the data?

- Polled every 30 s by default. `gh` calls are throttled by GitHub's own rate limit; the values OpenUsage reads include `remaining` and `reset` so you can see headroom.
- To keep each poll to a few `gh` processes, results are cached per account and host:

  | Data | Cached for |
  |---|---|
  | Full snapshot | 2 min (1 min when the status is not OK) |
  | `gh auth status` | 5 min |
  | Copilot version | 1 h (10 min when no version command works) |
  | Org billing and metrics, including 403s for non-admins | 1 h |

- Every `gh` subprocess runs in its own process group. On timeout the whole group is killed, so a child that `gh` started (such as the `copilot` binary behind `gh copilot`) cannot keep the poll hanging.

## API endpoints used

All via `gh` subprocess; no direct HTTP calls:

- `gh auth status`
- `gh api /user`
- `gh api /copilot_internal/user`
- `gh api /rate_limit`
- `gh api /orgs/{org}/copilot/billing`
- `gh api /orgs/{org}/copilot/metrics`

## Files read

- `~/.copilot/logs/**`
- `~/.copilot/session-state/<id>/workspace.yaml`
- `~/.copilot/session-state/<id>/<events>.jsonl`
- `~/.copilot/config.json`

`~/.config/github-copilot/` is referenced only by auto-detection (to register the account); the provider does not read its contents.

## Caveats

- Org metrics only appear if your account has admin access to the org.
- The standalone Copilot CLI is newer and exposes a different subset of data; the `gh` path is preferred when both are available.
- Premium quotas reset monthly per GitHub's billing cycle.

## Troubleshooting

- **No data** — run `gh auth login` and check auth status with `gh auth status`. The optional `gh copilot` extension (`gh extension install github/gh-copilot`) is *not* required — OpenUsage only uses it to display a version string, and treats it as informational if missing.
- **Org metrics missing** — your account isn't a Copilot Business/Enterprise admin; this is expected.
- **Stale rate limits** — the GraphQL query is rate-limited; OpenUsage respects the polling interval to avoid hammering it.
- **GitHub Enterprise Cloud/Server** — set `base_url` on the account (see the Setup section above) so `gh` calls target the right hostname instead of `github.com`, and make sure you ran `gh auth login --hostname <host>` for that host first.
