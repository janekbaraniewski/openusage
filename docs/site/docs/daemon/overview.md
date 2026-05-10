---
title: Daemon overview
description: Background telemetry daemon that aggregates AI tool usage from collectors, hooks, and a disk spool into a single SQLite store.
---

# Daemon overview

OpenUsage can run a small background service that continuously collects usage data from AI providers and tool integrations, persists it to SQLite, and serves a unified read model to the TUI. This is **Daemon mode** — an alternative to the default **Direct mode** in which the dashboard polls providers itself for the lifetime of one session.

See [Direct vs Daemon](../concepts/direct-vs-daemon.md) for a side-by-side comparison.

## What you get

- **Long-lived history.** Events persist across TUI restarts and machine reboots, so analytics and time-window views (`7d`, `30d`, `all`) reflect real activity.
- **Hook-based ingestion.** Tools like Claude Code, Codex, and OpenCode push every turn, message, and tool call directly to the daemon — no polling lag, no missed events.
- **Single source of truth.** All AI usage lives in one SQLite database with deduplication, retention, and provider linking.
- **Lower API load.** Provider rate-limit headers are polled once per interval by the daemon instead of once per dashboard run.

## When to use it

Use Daemon mode when:

- You want continuous tracking, not just a snapshot of "now."
- You use tools that emit hooks (Claude Code, Codex, OpenCode) and want every turn captured.
- You run the TUI on a headless server, in tmux, or open and close it frequently.
- You care about analytics over multi-day windows.

Stick with Direct mode when:

- You only run the TUI ad-hoc to peek at current limits.
- You don't want a background process or a service installed.
- You're on a system where launchd / systemd-user is not available.

## Dataflow

```
+----------------------+       +---------------------+
|  Collectors          |       |  Tool hooks         |
|  (poll providers)    |       |  (claude-code,      |
|                      |       |   codex, opencode)  |
+----------+-----------+       +----------+----------+
           |                              |
           |  HTTP over Unix socket       |  POST /v1/hook/{source}
           v                              v
       +------------------------------------+
       |          openusage daemon          |
       |  +------------------------------+  |
       |  |  Pipeline                    |  |
       |  |  - dedup (tool_call_id →     |  |
       |  |    message_id → turn_id →    |  |
       |  |    fingerprint hash)         |  |
       |  |  - provider linking          |  |
       |  |  - retention                 |  |
       |  +--------------+---------------+  |
       |                 v                  |
       |        +-----------------+         |
       |        |  SQLite store   |         |
       |        |  telemetry.db   |         |
       |        +--------+--------+         |
       |                 v                  |
       |        +-----------------+         |
       |        |   ReadModel     |         |
       |        +--------+--------+         |
       +------------------|-----------------+
                          |  POST /v1/read-model
                          v
                  +---------------+
                  |  TUI client   |
                  +---------------+
```

Three input sources feed the pipeline:

- **Collectors** — the same provider plugins used in Direct mode, but driven by the daemon's polling loop. They ingest rate-limit headers, billing snapshots, and dashboard-scraped balances.
- **Hooks** — tool integrations POST events to the daemon over its Unix socket as they happen. See [Integrations](./integrations.md).
- **Spool** — when the daemon is unreachable (hook fired but socket missing), events are written to a disk queue and drained on next startup. See [Storage](./storage.md).

## Endpoints

The daemon listens on a Unix domain socket (no TCP):

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness probe. Returns 200 OK when the pipeline is healthy. |
| `POST` | `/v1/hook/{source}?account_id=…` | Hook ingestion. `{source}` matches a provider link. |
| `POST` | `/v1/read-model` | TUI client fetches a `UsageSnapshot` map for the current time window. |

Default socket: `~/.local/state/openusage/telemetry.sock`. Override with `--socket-path` or the `OPENUSAGE_TELEMETRY_SOCKET` environment variable.

Timeouts are tight: 2-second dial, 12-second request — the protocol is meant to be local and fast.

## What the daemon is not

- **Not a network service.** It is bound to a Unix socket on your machine. There is no TCP listener, no auth, no remote ingest.
- **Not multi-user.** One daemon per user account. Run separate daemons for separate users.
- **Not a replacement for provider dashboards.** It mirrors and aggregates; it does not bill.

## Next steps

- [Install the daemon](./install.md) on macOS or Linux
- [Configure tool integrations](./integrations.md) for Claude Code, Codex, and OpenCode
- [Inspect the SQLite store](./storage.md) and tune retention
- [Troubleshoot](./troubleshooting.md) socket, log, and corruption issues
