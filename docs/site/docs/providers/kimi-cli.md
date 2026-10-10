---
title: Kimi CLI
description: Track local Kimi CLI and Kimi Code CLI sessions and token usage in OpenUsage.
sidebar_label: Kimi CLI
keywords: [kimi cli usage tracker, kimi code cli token usage, track kimi cli spend locally]
---

# Kimi CLI

The `kimi_cli` provider reads local session logs from [Kimi CLI](https://github.com/MoonshotAI/kimi-cli) and [Kimi Code CLI](https://github.com/MoonshotAI/kimi-code). It aggregates sessions and per-model input, output, cache-read, and cache-write tokens. This local-data integration makes no network calls. It is separate from the [Moonshot API provider](./moonshot.md).

## Detection and paths

OpenUsage detects the `kimi` binary, a config file, or a sessions directory in either location:

| Client | Sessions | Config |
| --- | --- | --- |
| Kimi CLI | `~/.kimi/sessions/<group>/<uuid>/wire.jsonl` | `~/.kimi/config.json` |
| Kimi Code CLI | `~/.kimi-code/sessions/<group>/<uuid>/agents/<agent>/wire.jsonl` | `~/.kimi-code/config.json` |

When both session directories exist, `~/.kimi/sessions` takes priority. The default config is selected from the same client directory as the selected sessions. If only a config exists, it can still trigger detection.

## Manual configuration

Use `provider_paths` for provider-specific overrides. JSON paths must be absolute; a literal `~` is not expanded.

~~~json
{
  "accounts": [
    {
      "id": "kimi_cli",
      "provider": "kimi_cli",
      "provider_paths": {
        "sessions_dir": "/home/you/.kimi-code/sessions",
        "config_path": "/home/you/.kimi-code/config.json"
      }
    }
  ]
}
~~~

`sessions_dir` selects a sessions directory. `config_path` selects the model fallback config. Either override can be omitted; without `config_path`, OpenUsage looks beside the selected sessions directory.

## Metrics

The provider walks the selected sessions directory for `wire.jsonl` files. Kimi CLI records contain `message.type = StatusUpdate` and snake_case `token_usage` fields. Kimi Code CLI records contain `type = usage.record`, camelCase `usage` fields, and an epoch-millisecond `time`. Only records with nonzero token usage count.

| Metric | Source fields |
| --- | --- |
| `total_input_tokens` | `input_other` or `inputOther` |
| `total_output_tokens` | `output` |
| `total_cache_read` | `input_cache_read` or `inputCacheRead` |
| `total_cache_write` | `input_cache_creation` or `inputCacheCreation` |

The model comes from the record, then the selected `config.json`, then `kimi-for-coding`. Session IDs include both group and UUID, even when Kimi Code stores agent logs deeper. Each record's timestamp is converted to UTC for daily series and for `sessions_today` and `sessions_7d`.

The logs do not contain USD prices. Configure the Moonshot API provider separately for its API balance and quota.

## Troubleshooting

- **No sessions:** run a CLI session and check `openusage detect`. If both clients are installed, set `sessions_dir` explicitly to select Kimi Code.
- **Unexpected model name:** check the record's model and the matching client's `config.json`; `kimi-for-coding` is the fallback.
- **Missing tokens:** malformed JSON lines are skipped. A single line larger than 1 MiB stops scanning that file.

## Related

- [Moonshot API](./moonshot.md)
- [Codex CLI](./codex.md)
