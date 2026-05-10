---
title: Team tracking
description: Practical patterns for using OpenUsage to monitor a team's combined AI tool usage — and what's out of scope.
---

OpenUsage is a **local, end-user tool**. It is not a backend, not a SaaS, and does not aggregate data across machines on its own. That said, several of the providers it talks to expose team-scoped data, and a handful of patterns let a team get useful visibility without building anything custom.

:::note Scope check
If you need a centralized dashboard with role-based access control across an org, OpenUsage is not that tool. Look at vendor team consoles or a dedicated observability product. OpenUsage shines when each engineer wants the same single-pane view of their own (and their team's) spend.
:::

## Patterns that work

### 1. Shared keys, individual dashboards

The simplest pattern: one set of team API keys, every engineer runs OpenUsage locally with the same env vars.

```bash
export OPENROUTER_API_KEY="sk-or-team-..."
export OPENAI_API_KEY="sk-team-..."
openusage
```

What this gives you:

- Every engineer sees the same totals because the provider attributes spend to the team.
- Burndown is shared — when one engineer's job spends, everyone sees it on the next poll.

What this doesn't give you:

- Per-engineer breakdown. Provider APIs typically aggregate at the key level, so all team members appear merged.

Pair this with **per-engineer** keys when you need attribution; many providers let a team key list its sub-keys.

### 2. Provider-side team APIs

Several providers expose first-class team views that OpenUsage surfaces directly:

#### OpenRouter

If your `OPENROUTER_API_KEY` is a management key, the provider pulls `/api/v1/keys` and shows per-key usage in the detail panel. This is the cleanest team-attribution path because every engineer's key shows up as its own row.

#### Cursor (team plan)

The Cursor provider hits `/team/members` and surfaces team membership in the detail view. Per-member spend depends on what Cursor returns for that endpoint.

#### Copilot

When `gh` is logged in to an org-admin account, the GraphQL queries return org-level metrics: active/engaged users by editor and model, seat allocation, feature toggles. Engineers without admin scope see only their own.

#### Z.AI / Moonshot / Mistral

These providers expose org or project-level spend and quotas. The data is whatever the underlying tier allows.

### 3. Daemon per machine, manual roll-up

If you want longer-term per-engineer history, install the [daemon](/daemon) on each developer machine. The SQLite store at `~/.local/state/openusage/telemetry.db` keeps events for `data.retention_days` (default 30).

You can periodically:

1. Copy each developer's `telemetry.db` to a shared location (rsync, syncthing, etc).
2. Open them on a single laptop one at a time to inspect.

There is no built-in merge across stores. This pattern is fine for "let's all check our spend at the end of the week"; it is not a real central database.

### 4. Compare-mode pairing

The dashboard's Compare view (cycle with `v` / `V`) puts two providers side by side. Useful when:

- Two engineers run OpenUsage and screen-share to compare.
- A single engineer compares two accounts (personal + work, or two team keys).

## Patterns that don't work well

- **Pushing local data to a central server.** OpenUsage has no built-in shipper. The daemon listens on a Unix domain socket, not a network socket.
- **Single dashboard for everyone.** No multi-user mode. One TUI per shell.
- **Real-time team notifications.** No webhook or alerting integration. The TUI shows status badges; that's it.

If any of these matter, treat OpenUsage as the per-engineer view and pair it with whatever your team uses for centralized billing visibility.

## Tips

- Standardize the same `~/.config/openusage/settings.json` across machines (commit it to a dotfiles repo) so every engineer sees the same providers in the same order.
- Use [time windows](../concepts/time-windows.md) (`w`) to align comparisons — pick `7d` for weekly checkpoints, `1d` for daily standup.
- For Claude Code teams, install the [integration hook](/daemon) so per-turn costs accumulate even when the dashboard is closed.

## See also

- [Multi-account](multi-account.md)
- [Cost attribution](cost-attribution.md)
- [Headless servers](headless-servers.md) — running daemons on a shared machine.
