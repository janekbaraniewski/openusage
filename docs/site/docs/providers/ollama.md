---
title: Ollama
description: Track local Ollama models, VRAM, request log analytics, and cloud credits in OpenUsage.
sidebar_label: Ollama
---

# Ollama

Tracks local Ollama servers and, optionally, the Ollama Cloud account. The local side reads the on-machine HTTP API and the server log; cloud credits come from authenticated endpoints when a key is set.

## At a glance

- **Provider ID** — `ollama`
- **Detection** — local server reachable on `127.0.0.1:11434`, **or** `OLLAMA_API_KEY` set
- **Auth** — none for local; optional API key for cloud
- **Type** — local runtime
- **Tracks**:
  - Installed models and their details (family, parameter count, quantization)
  - Running processes: loaded models and VRAM usage
  - Server-log derived metrics: daily requests, chat vs generate split, latency, errors, and 5h/1d/7d windows
  - Cloud credits and limits (when authed)

## Setup

### Auto-detection

OpenUsage probes `http://127.0.0.1:11434/api/tags`. If the server responds, the provider registers without any config. Setting `OLLAMA_API_KEY` enables cloud features additionally.

### Manual configuration

```json
{
  "accounts": [
    {
      "id": "ollama",
      "provider": "ollama",
      "api_key_env": "OLLAMA_API_KEY",
      "base_url": "http://127.0.0.1:11434"
    }
  ]
}
```

Set `base_url` if Ollama runs on a different host or port.

## Data sources & how each metric is computed

Ollama has three independent data sources. The provider runs them in parallel and merges what each returns. None requires the others — a fresh local install with no log file still produces a useful tile.

| Source | Path / endpoint | When used |
|---|---|---|
| Local HTTP API | `GET http://127.0.0.1:11434/api/tags` and `/api/ps` | Always, when the server responds |
| Local SQLite + Gin log | `~/.ollama/logs/server*.log` (override via `logs_dir`); desktop DB path is OS-specific | Always, falls back gracefully when missing |
| Cloud HTTP API | `https://ollama.com` (authenticated) | Only when `OLLAMA_API_KEY` is set |

### Models and details (local API)

- Source: `GET /api/tags` returns `models[]`, each with `name`, `details.family`, `details.parameter_size`, `details.quantization_level`.
- Transform: count of models becomes `models_total`; each model becomes a detail row.

### Running processes & VRAM (local API)

- Source: `GET /api/ps` returns currently-loaded models with `size_vram` in bytes.
- Transform: a row per loaded model with the VRAM figure converted to GB. The sum populates the tile's "VRAM in use" line.

### Request analytics (server log)

The Ollama server emits a Gin-style HTTP access log line per request. The provider tails `~/.ollama/logs/server*.log` (matching the rotated siblings as well) on every platform. Override with the `logs_dir` hint, or via `config_dir` (the provider then looks under `<config_dir>/logs/`).

For each parsed line the provider extracts timestamp, HTTP status, latency, and path. Lines whose path is in the inference set are counted:

- `/api/chat`, `/v1/chat/completions`, `/v1/responses`, `/v1/messages` → chat
- `/api/generate`, `/v1/completions` → generate

Metrics are bucketed into 5h, 1d, 7d, and "today" windows:

- `requests_today`, `requests_5h`, `requests_1d`, `requests_7d`, `recent_requests` (24h)
- `chat_requests_*` / `generate_requests_*` per window
- `http_4xx_*` / `http_5xx_*` per window (`status >= 400` / `>= 500`)
- `avg_latency_ms_5h`, `avg_latency_ms_1d`, `avg_latency_ms_today` — total latency ÷ count per window
- `DailySeries["requests"]` — per-day request count for the trailing window, used by the daily chart

### Desktop database (optional)

- Source: a SQLite database that the Ollama Desktop app writes to. The provider opens it read-only and reads breakdowns + per-token settings when the file exists.
- Transform: stored alongside the API-derived metrics.

### Server config

- Source: a JSON config file under the Ollama config directory.
- Transform: the `disable_ollama_cloud` flag is stored as `Attributes["cloud_disabled"]`.

### Cloud credits & limits (optional)

- Source: authenticated calls to Ollama Cloud (`https://ollama.com`) when `OLLAMA_API_KEY` is set.
- Transform: balance and quota metrics are emitted when the response is 200. 401/403 sets `cloud_auth_failed`; 429 sets `cloud_rate_limited` (both as diagnostics, not fatal — the local data still renders).

### Status message

- Source: derived from whichever metrics populated. Format: `<X> msgs today, <X> req today, <X> req 5h, <X> req 1d, <Y> models`.

### What's NOT tracked

- **Per-model token counts.** Local Ollama does not log token usage in the access log; only HTTP-level request counts are available unless the desktop DB has them.
- **GPU utilization.** Only VRAM (from `/api/ps`) is exposed.

### How fresh is the data?

- Polled every 30 s by default. The local API is real-time; the log parser re-reads the file each poll (it stops at end-of-file). The desktop DB is also re-read each poll.

## API endpoints used

- `GET /api/tags` — installed models
- `GET /api/ps` — running processes
- Cloud endpoints when `OLLAMA_API_KEY` is set

## Files read

- Server log: `~/.ollama/logs/server*.log` (default on every platform; override with the `logs_dir` hint or via `config_dir`)
- Desktop database (optional, SQLite, read-only) — OS-specific path:
  - macOS — `~/Library/Application Support/Ollama/db.sqlite`
  - Linux — `~/.local/share/Ollama/db.sqlite` or `~/.config/Ollama/db.sqlite`
  - Windows — `%APPDATA%\Ollama\db.sqlite`
- Server config (JSON) at `~/.ollama/server.json` (override with `server_config` or `config_dir`)

## Caveats

- Without a server log file, request analytics are unavailable; live model and VRAM data still works.
- Cloud credit data requires `OLLAMA_API_KEY`; local-only setups never see it.
- Latency and error rates are derived from log parsing, so very high request volume may exceed the parser's window.

## Troubleshooting

- **Server unreachable** — start Ollama (`ollama serve`) and re-run.
- **No request analytics** — confirm `~/.ollama/logs/server*.log` exists; check permissions, or set the `logs_dir` hint if your install writes elsewhere.
- **Wrong port** — set `base_url` in your config.
