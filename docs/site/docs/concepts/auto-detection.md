---
title: Auto-detection
description: How OpenUsage discovers AI tools and API keys on first run, and how detected accounts merge with manual configuration.
---

The first time you run `openusage`, no config file is required. The detector inspects your environment and creates accounts for everything it finds. You can override or extend its results by editing `~/.config/openusage/settings.json`.

## What gets scanned

Detection runs three passes:

### 1. Environment variables (API platforms)

For each supported provider, the detector checks whether a known env var is set in the current shell:

| Env var | Provider |
|---|---|
| `OPENAI_API_KEY` | openai |
| `ANTHROPIC_API_KEY` | anthropic |
| `OPENROUTER_API_KEY` | openrouter |
| `GROQ_API_KEY` | groq |
| `MISTRAL_API_KEY` | mistral |
| `DEEPSEEK_API_KEY` | deepseek |
| `XAI_API_KEY` | xai |
| `GEMINI_API_KEY` | gemini_api |
| `GOOGLE_API_KEY` | gemini_api (account id `gemini-google`) |
| `ALIBABA_CLOUD_API_KEY` | alibaba_cloud |
| `MOONSHOT_API_KEY` | moonshot |
| `ZAI_API_KEY` / `ZHIPUAI_API_KEY` | zai |
| `OPENCODE_API_KEY` / `ZEN_API_KEY` | opencode |

If the env var is present, an account is created with `api_key_env` set to that variable name. The actual key value is read at fetch time, never persisted.

### 2. Local binaries and config dirs (coding agents)

For coding agents, the detector looks for the CLI binary on `$PATH` plus a config directory that signals the tool has been used:

| Tool | Signals |
|---|---|
| Claude Code | `claude` binary + `~/.claude/` |
| Codex CLI | `~/.codex/` directory |
| Cursor IDE | App Support directory (`~/Library/Application Support/Cursor`, `~/.config/Cursor`, or `%APPDATA%\Cursor`) |
| GitHub Copilot | `gh` CLI with Copilot extension, or standalone `copilot` binary + `~/.copilot/` |
| Gemini CLI | `gemini` binary + `~/.gemini/` |

### 3. Local services

| Service | Signal |
|---|---|
| Ollama | local server reachable on `127.0.0.1:11434`, or `OLLAMA_API_KEY` set |

## First-run output

A typical first run looks like this:

```
$ openusage
[detect] OPENAI_API_KEY found      → account openai-default
[detect] ANTHROPIC_API_KEY found   → account anthropic-default
[detect] claude binary at /usr/local/bin/claude
[detect] ~/.claude found           → account claude_code-default
[detect] cursor support dir found  → account cursor-default
[detect] ollama reachable on :11434 → account ollama-default

5 providers active. Press ? for help.
```

(Detection messages only appear when `OPENUSAGE_DEBUG=1` is set; the dashboard otherwise launches silently.)

## Merging with manual configuration

`settings.json` accepts an `accounts` array. When you launch the dashboard, the resolver:

1. Loads manually configured accounts.
2. Runs auto-detection.
3. **Manual wins.** For each `(provider, id)` pair, the manual entry takes precedence. Detected accounts that don't conflict are appended.

That means you can:

- **Disable a detected provider** by setting `auto_detect: false` (turns off pass 1–3 entirely).
- **Override a detected account** by declaring an account with the same `id` and overriding fields like `base_url` or `probe_model`.
- **Add a second account for a provider** by giving it a different `id` and pointing `api_key_env` at a different env var.

```json
{
  "auto_detect": true,
  "accounts": [
    {
      "id": "openai-work",
      "provider": "openai",
      "api_key_env": "OPENAI_WORK_KEY",
      "probe_model": "gpt-4.1-mini"
    }
  ]
}
```

In the example above, auto-detection still creates `openai-default` from `OPENAI_API_KEY` if set, and `openai-work` runs alongside it from the manually declared env var.

## When detection misses something

If a provider you expected does not show up, walk through:

1. Is the env var actually exported in the shell that launched OpenUsage? `echo $OPENAI_API_KEY` should print it.
2. Is the binary on the same `$PATH` OpenUsage sees? `which claude` from the same shell.
3. Did the tool's config dir get created? Run the tool once before relaunching.
4. Run `OPENUSAGE_DEBUG=1 openusage` and look at stderr for skipped detections.

See [provider not detected](../troubleshooting/provider-not-detected.md) for a per-provider checklist.

## What detection does not do

- It never reads or stores the value of an API key.
- It never makes network calls during detection itself; that only happens when a provider's `Fetch()` runs.
- It does not modify any tool's config (only the integration installer does that).
