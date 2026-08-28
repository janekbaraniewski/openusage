---
title: Local web dashboard
description: Open a browser view of your OpenUsage snapshots with `openusage serve`.
---

`openusage serve` starts a **local HTTP server** and a browser dashboard for the same usage snapshots the terminal UI shows. Use it when you want a larger layout, charts, or to glance at spend from a browser on the same machine.

This is **not** a hosted product and **not** the [multi-machine hub](./multi-machine.md). Data stays on the workstation; the default bind is loopback.

## Quick start

```bash
openusage serve
```

That binds `http://127.0.0.1:8080`, prefers the [telemetry daemon](../daemon/overview.md) for snapshots, and opens your default browser when stdout is a terminal. Press Ctrl+C to stop.

Preview the layout without API keys or a running daemon:

```bash
openusage serve --demo
```

## What you get

The page is a Keeper-style usage view with OpenUsage colors:

- **Overview** — spend, tokens, provider count, and healthy-account KPIs, a daily spend chart from `DailySeries`, and a model-mix breakdown from `ModelUsage`
- **Provider cards** — status pills, the snapshot message, and a remaining-quota gauge when the snapshot has limits
- **Detail drawer** — metrics table and per-model cost when you click a card
- **Theme toggle** — dark Catppuccin Mocha (default) or a warm light theme

The dashboard auto-refreshes on the same interval as `ui.refresh_interval_seconds` (default 30s).

## Collection source

`--source` matches [`openusage export`](../reference/cli.md#openusage-export):

| Value | Behavior |
|---|---|
| `auto` (default) | Daemon read-model when the socket is up; otherwise a one-shot direct provider poll |
| `daemon` | Daemon only; fails if it is not running |
| `direct` | Poll providers in-process, ignoring the daemon |

`--demo` skips collection and serves synthetic snapshots.

API keys and snapshot `Raw` maps are never sent to the browser.

## Flags

```bash
openusage serve [--listen ADDR] [--source auto|direct|daemon] [--demo] [--open|--no-open] [--allow-public]
```

| Flag | Default | Purpose |
|---|---|---|
| `--listen` | `127.0.0.1:8080` or `serve.listen_addr` | TCP bind address |
| `--source` | `auto` | Snapshot collection path |
| `--demo` | off | Synthetic data |
| `--open` | on when stdout is a TTY | Open the default browser |
| `--no-open` | off | Never open a browser |
| `--allow-public` | off | Allow a non-loopback bind without a token |

## Endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /` | no | Dashboard HTML |
| `GET /app.css`, `GET /app.js` | no | Embedded assets |
| `GET /healthz` | never | Liveness (`status`, `source`) |
| `GET /api/v1/snapshots` | Bearer when a token is set | Snapshot envelope |
| `GET /api/v1/meta` | Bearer when a token is set | Version, theme, window, catalog |

## Auth and bind posture

Loopback (`127.0.0.1`, `localhost`, `::1`) is always allowed without a token.

Binding `:8080` or another non-loopback address without a token is refused unless you pass `--allow-public`. To require a Bearer token on the JSON API:

```bash
export OPENUSAGE_SERVE_TOKEN=s3cret
openusage serve --listen :8080
```

The token is **never** written to `settings.json`. `/healthz` stays unauthenticated. When a token is required, the page prompts for it and keeps it in `sessionStorage` for that tab.

## Config

Optional, in `settings.json`:

```json
{
  "serve": {
    "listen_addr": "127.0.0.1:8080"
  }
}
```

`--listen` overrides `serve.listen_addr`.

## Headless / SSH

On a server, bind loopback and browse through an SSH tunnel:

```bash
# on the server
openusage serve --no-open --listen 127.0.0.1:8080

# on your laptop
ssh -L 8080:127.0.0.1:8080 user@server
```

Then open `http://127.0.0.1:8080` locally. See [Headless servers](./headless-servers.md).

## See also

- [CLI reference](../reference/cli.md#openusage-serve)
- [Ways to use OpenUsage](../getting-started/ways-to-use.md)
- [Headless servers](./headless-servers.md)
