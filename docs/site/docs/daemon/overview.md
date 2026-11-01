---
title: Daemon overview
description: Background telemetry daemon that aggregates AI tool usage from collectors, hooks, and a disk spool into a single SQLite store.
---

# Daemon overview

The daemon is OpenUsage's runtime. It is a small background service that continuously collects usage data from AI providers and tool integrations, persists it to SQLite, and serves a unified read model to the TUI. The TUI is a thin client that reads from the daemon over a Unix domain socket.

Install once with:

```bash
openusage telemetry daemon install
```

## What you get

- **Long-lived history.** Events persist across TUI restarts and machine reboots, so analytics and time-window views (`7d`, `30d`, `all`) reflect real activity.
- **Hook-based ingestion.** Tools like Claude Code, Codex, and OpenCode push every turn, message, and tool call directly to the daemon — no polling lag, no missed events.
- **Single source of truth.** All AI usage lives in one SQLite database with deduplication, retention, and provider linking.
- **Always-on collection.** Provider rate-limit headers are polled on the configured interval whether or not the TUI is open.

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

- **Collectors** — provider plugins driven by the daemon's polling loop. They ingest rate-limit headers, billing snapshots, and dashboard-scraped balances.
- **Hooks** — tool integrations POST events to the daemon over its Unix socket as they happen. See [Integrations](./integrations.md).
- **Spool** — when the daemon is unreachable (hook fired but socket missing), events are written to a disk queue and drained on next startup. See [Storage](./storage.md).

## Adaptive polling and backoff

The daemon slows down when there is nothing to do and when providers push back:

- **Collect loop.** Local collectors run every `--collect-interval` (30s by default). After three cycles in a row that ingest no new events, the interval doubles on each further idle cycle, up to 5 minutes. Re-emitted history that the store deduplicates does not count as activity. The first cycle that ingests something resets the interval.
- **Unchanged snapshots.** A provider whose snapshot has not changed is polled less often: up to 4x the poll interval for HTTP providers and up to 16x for providers with cheap local change detection.
- **Rate limits and errors.** When a poll is rate limited (HTTP 429) or ends in `error` (including HTTP 5xx), the account backs off exponentially: 2x, 4x, 8x the poll interval and so on, up to 30 minutes. A successful poll resets it. A `limited` status that only means a quota is used up (for example a Codex 5h window at 100%) does not back off, so the reset shows up on the next regular poll.
- **Retry-After.** When a provider returns a `Retry-After` header (seconds or HTTP date) on a 429 or 5xx, the account is not polled again before that time, up to 2 hours.

## Endpoints

The daemon listens on a Unix domain socket (no TCP):

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness probe. Returns 200 OK when the pipeline is healthy. |
| `POST` | `/v1/hook/{source}?account_id=…` | Hook ingestion. `{source}` matches a provider link. |
| `POST` | `/v1/read-model` | The TUI, `openusage tmux`, the Claude Code statusline and `openusage report` fetch a `UsageSnapshot` map for the current time window. |

Default socket: `~/.local/state/openusage/telemetry.sock`. Override with `--socket-path` or the `OPENUSAGE_TELEMETRY_SOCKET` environment variable.

### Account resolution

The daemon resolves your accounts (configured accounts plus auto-detected tools and API keys) once and keeps the result in memory. Clients send a read-model request without an account list and the daemon fills it in from that cache, so a render never runs auto-detection itself.

- Editing `settings.json` (for example toggling a provider on the dashboard or changing the time window) is picked up on the next request: the daemon re-reads the file when its modification time changes.
- Newly installed tools and new API keys are picked up by a background re-detection every 5 minutes, or right after any `settings.json` change.

### Read-model latency

A read-model request is answered from the daemon's cache when one exists. On a cache miss the daemon starts the computation in the background and waits only as long as the client can: clients send their remaining time budget in the `X-Openusage-Budget-Ms` header (capped at 5 seconds). If the result is not ready in time, the daemon answers with empty snapshots and `"pending": true`, and the next request is served from cache. `openusage tmux` and the statusline treat a pending answer like a timeout and keep showing their last good output instead of falling back to a slow direct collection.

Timeouts are tight and per-caller:

- **TUI client** (`/v1/read-model`): 2-second dial, 12-second HTTP timeout; each dashboard read is capped at 5 seconds.
- **tmux and statusline** (`/v1/read-model`): one request inside the 800ms render budget, with no separate `/healthz` probe. A missing socket falls back to direct mode immediately.
- **Hook command** (`POST /v1/hook/<source>`): 15-second overall context. On failure the event is written to the spool and re-ingested when the daemon comes back.

The protocol is meant to be local and fast.

## What the daemon is not

- **Not a network service.** It is bound to a Unix socket on your machine. There is no TCP listener, no auth, no remote ingest.
- **Not multi-user.** One daemon per user account. Run separate daemons for separate users.
- **Not a replacement for provider dashboards.** It mirrors and aggregates; it does not bill.

## Next steps

- [Install the daemon](./install.md) on macOS or Linux
- [Configure tool integrations](./integrations.md) for Claude Code, Codex, and OpenCode
- [Inspect the SQLite store](./storage.md) and tune retention
- [Troubleshoot](./troubleshooting.md) socket, log, and corruption issues
