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

## What you'll see

- Dashboard tile shows running models and total VRAM in use.
- Detail view lists every installed model with family, parameter count, and quantization.
- Request analytics (chat/generate split, average latency, error counts) come from parsing the server log.
- Cloud credits and limits appear when an API key is configured.

## API endpoints used

- `GET /api/tags` — installed models
- `GET /api/ps` — running processes
- Cloud endpoints when authed

## Files read

Server log path varies by OS:

- Linux — `/tmp/ollama.log`
- macOS — `~/Library/Logs/Ollama/`
- Windows — `%LOCALAPPDATA%\Ollama\logs`

## Caveats

- Without a server log file, request analytics are unavailable; live model and VRAM data still works.
- Cloud credit data requires `OLLAMA_API_KEY`; local-only setups never see it.
- Latency and error rates are derived from log parsing, so very high request volume may exceed the parser's window.

## Troubleshooting

- **Server unreachable** — start Ollama (`ollama serve`) and re-run.
- **No request analytics** — confirm the log file exists at the OS-specific path; check permissions.
- **Wrong port** — set `base_url` in your config.
