---
title: Multi-machine aggregation
description: Push usage snapshots from several workstations into a single hub and view the aggregated dashboard remotely.
---

OpenUsage runs per-machine by default — the dashboard sees only what the local daemon collected. When you want a single pane that shows spend across **multiple workstations** (e.g. work laptop, home desktop, dedicated build host), the **Hub + Exporter** pair gives you that without changing how local data is collected.

## Architecture

```
machine A  (openusage telemetry)
  └─ Exporter ──POST /v1/push──▶ Hub server ◀── openusage hub-view <url>
machine B  (openusage telemetry)
  └─ Exporter ──POST /v1/push──▶ (in-memory Store)
```

Every worker still runs the normal `openusage telemetry` daemon — collection logic, providers, and the SQLite store are unchanged. The new piece is an **exporter** inside the daemon that, when `export.target` is set, periodically POSTs the latest `UsageSnapshot` batch to a remote **hub**. The hub holds the latest snapshot per machine in memory and exposes:

| Endpoint | Method | Auth required |
|---|---|---|
| `/v1/push` | POST | Bearer (if auth_token set) |
| `/v1/snapshots` | GET | Bearer (if auth_token set) |
| `/healthz` | GET | never (liveness probe) |

`openusage hub` provides a built-in TUI for the aggregated view. `openusage hub-view <url>` is a read-only client suitable for a laptop that doesn't need its own daemon.

## Step 1: choose where the hub runs

Pick one host that all workers can reach. Common picks:

- A home-lab box or always-on workstation on the same LAN
- A small VPS, exposed via Tailscale / WireGuard / Cloudflare Tunnel
- A docker host running the hub container

## Step 2: pick an auth posture

| Scenario | auth token | bind |
|---|---|---|
| Trusted home LAN, just for personal visibility | none | `127.0.0.1:9190` (loopback only) |
| Trusted LAN, accessed from another LAN machine | none, `--allow-public` | `:9190` |
| Reachable over Tailscale / WireGuard / VPN | `OPENUSAGE_HUB_TOKEN` set | `:9190` |
| Public internet | `OPENUSAGE_HUB_TOKEN` set | `:9190` + TLS terminator in front |

**Recommendation: always set a token if anything other than this machine can reach the port.** Once set, every push or snapshot fetch must include `Authorization: Bearer <token>`.

:::warning Tokens are never written to settings.json
The token lives in your shell environment (`OPENUSAGE_HUB_TOKEN`), not in `~/.config/openusage/settings.json`. This matches the [`accounts[].api_key_env`](../reference/configuration.md#accounts) convention: configs reference secrets by env-var name; secrets themselves never sit on disk.
:::

## Step 3: start the hub

### As a normal process

```bash
# Trusted LAN, no auth
openusage hub --listen :9190 --allow-public

# With Bearer auth, headless (for systemd / launchd / containers)
export OPENUSAGE_HUB_TOKEN=$(openssl rand -hex 32)
openusage hub --headless
```

The unsafe-default guard refuses to bind a non-loopback interface when no auth is configured. You'll see one of three remediations in the startup error:

```
hub: refusing to listen on ":9190" without auth_token.
  Choose one:
    1. export OPENUSAGE_HUB_TOKEN=<secret> to enable Bearer auth, OR
    2. bind to loopback only:  --listen 127.0.0.1:9190, OR
    3. pass --allow-public if you have a network-level firewall in place
```

### In Docker

A `Dockerfile.hub` is included at the repo root. Build it locally:

```bash
docker build -f Dockerfile.hub -t openusage-hub:dev .
docker run --rm \
  -e OPENUSAGE_HUB_TOKEN=$(openssl rand -hex 32) \
  -p 9190:9190 \
  openusage-hub:dev
```

The container is for the **hub server only** — it does not run the TUI dashboard. Expect a published image and a versioned release tag to follow in a separate PR.

Key properties of the image:

- Non-root user (`USER 65534:65534` / `nobody`)
- `HEALTHCHECK` against `/healthz`
- `EXPOSE 9190`
- OCI labels (`source`, `version`, `revision`, `created`, `licenses`)

## Step 4: enable the exporter on each worker

On every machine you want feeding the hub, edit `~/.config/openusage/settings.json`:

```json
{
  "export": {
    "target": "http://hub.lan:9190",
    "interval_seconds": 60,
    "machine_name": "work-laptop"
  }
}
```

If the hub requires auth, `OPENUSAGE_HUB_TOKEN` needs to live in the daemon's environment — not your interactive shell. The easiest path is to export it once and (re)install the daemon service:

```bash
export OPENUSAGE_HUB_TOKEN=<secret>
openusage telemetry daemon install   # rewrites the platform service file
                                     # with a snapshot of relevant env vars
```

`daemon install` captures a whitelisted snapshot of your current environment (`OPENAI_API_KEY`, `OPENUSAGE_HUB_TOKEN`, etc.) and writes it into:

- Linux — `~/.local/state/openusage/daemon.env` (loaded via `EnvironmentFile=` in `~/.config/systemd/user/openusage-telemetry.service`)
- macOS — the `EnvironmentVariables` dict inside `~/Library/LaunchAgents/com.openusage.telemetryd.plist`

If you'd rather edit those files by hand, append `OPENUSAGE_HUB_TOKEN="<secret>"` to `~/.local/state/openusage/daemon.env` on Linux or add it to the `EnvironmentVariables` dict in the plist on macOS, then reload the service (`systemctl --user restart openusage-telemetry` / `launchctl kickstart -k gui/$UID/com.openusage.telemetryd`). Note that the next `daemon install` overwrites these files with a fresh snapshot of your current shell environment, so prefer the `export + install` path unless you're already managing the unit by other means.

The exporter pushes immediately on startup and then every `interval_seconds`. Best-effort: errors are logged and swallowed; the daemon never stops over an exporter failure.

## Step 5: view the aggregated dashboard

### From a machine with a daemon

```bash
openusage hub --listen 127.0.0.1:9190
```

Same TUI as the local dashboard. Each provider tile is keyed by `machine:providerID:accountID` so two machines running the same provider don't collide.

### From a laptop with no daemon

```bash
openusage hub-view http://hub.lan:9190
OPENUSAGE_HUB_TOKEN=s3cret openusage hub-view https://openusage.example.com
```

`hub-view` polls `GET /v1/snapshots` on the [`ui.refresh_interval_seconds`](../reference/configuration.md#ui) cadence (override with `--interval`). The status line shows `hub <url> · N machine snapshots` and flips to an error if the hub becomes unreachable.

## Operational notes

- **Stale eviction.** A machine entry is pruned after `hub.stale_timeout_seconds` (default 300s). Stop a worker and within 5 min its tiles disappear from the aggregated view.
- **Snapshot is the latest, not a stream.** The hub holds only the newest batch per machine. If you want historical aggregates, query each daemon's SQLite separately.
- **`/healthz` is unauthenticated by design.** Liveness probes work without secrets; the response only lists machine names — no snapshot data. On a trusted LAN this is fine, but on an internet-facing hub the machine list leaks topology. If you care, bind the hub loopback-only and reverse-proxy `/v1/push` and `/v1/snapshots` via your existing TLS terminator, or firewall `/healthz` at the network layer.
- **Bind address matters.** `127.0.0.1:9190` is loopback-only and safe even without auth; `:9190` or `0.0.0.0:9190` is all-interfaces and requires either auth or `--allow-public`.

## See also

- [`openusage hub` and `openusage hub-view`](../reference/cli.md#openusage-hub) — flag reference
- [`export` and `hub` config blocks](../reference/configuration.md#export) — settings.json fields
- [`OPENUSAGE_HUB_TOKEN`](../reference/env-vars.md) — the shared Bearer-token env var
- [Headless servers](./headless-servers.md) — running the daemon on hosts without a desktop
