---
title: Provider not detected
description: Per-detection-style checklists for finding why a provider isn't showing up in the dashboard.
---

Auto-detection runs in three styles. Use the checklist for the style that matches the missing provider.

## Style A: env var providers

Affected: `openai`, `anthropic`, `openrouter`, `groq`, `mistral`, `deepseek`, `xai`, `gemini_api`, `alibaba_cloud`, `moonshot`, `zai`, `opencode`.

### Checklist

1. **Is the env var set in the shell that launches OpenUsage?**
   ```bash
   echo "${OPENAI_API_KEY+set}"
   ```
   Empty output means the variable is not exported in this process.

2. **Is it `export`ed, not just assigned?**
   ```bash
   # Wrong (visible only in the current shell, not subprocesses):
   OPENAI_API_KEY=sk-...
   # Right:
   export OPENAI_API_KEY=sk-...
   ```

3. **Is the variable name spelled exactly right?** Case matters. `Openai_Api_Key` will not be picked up.

4. **For providers with multiple accepted names** (Z.AI accepts `ZAI_API_KEY` or `ZHIPUAI_API_KEY`; OpenCode accepts `OPENCODE_API_KEY` or `ZEN_API_KEY`), at least one must be set.

5. **Is `auto_detect` enabled?** In `settings.json`:
   ```json
   { "auto_detect": true }
   ```
   If false, no env-var detection happens.

6. **Did you launch from a GUI app launcher?** macOS Finder / Dock launches don't inherit your shell `~/.zshrc` exports. Run `openusage` from a terminal, or move exports into `~/.config/zsh/.zshenv` / launchd.

## Style B: local binary + config dir

Affected: `claude_code`, `codex`, `cursor`, `copilot`, `gemini_cli`.

### Checklist

1. **Is the binary on `$PATH`?**
   ```bash
   which claude
   which codex
   which gemini
   which gh && gh extension list | grep copilot
   ```
   No output → install the tool, or fix `$PATH` for the shell that runs OpenUsage.

2. **Has the tool been launched at least once?** Detection requires both the binary **and** a config directory created by the tool's own first run.
   | Tool | Expected dir |
   |---|---|
   | Claude Code | `~/.claude/` (or `~/.config/claude/` on Linux) |
   | Codex | `~/.codex/` |
   | Cursor | macOS `~/Library/Application Support/Cursor`, Linux `~/.config/Cursor`, Windows `%APPDATA%\Cursor` |
   | Copilot | `~/.copilot/` (standalone) or `~/.config/github-copilot/devices.json` |
   | Gemini CLI | `~/.gemini/` |

3. **For Cursor specifically**, the provider reads local SQLite files. If the app has never been opened on this machine, the DBs don't exist yet.

4. **For Copilot via gh**, you also need:
   ```bash
   gh auth status
   ```
   to show an authenticated user with Copilot scope.

5. **Permissions.** The provider must be able to read the config files. On a server with a different user, `chmod`/`chown` may have made files unreadable. Try:
   ```bash
   ls -l ~/.claude/stats-cache.json
   ```

6. **Override paths if needed.** Each provider exposes a knob:
   ```json
   {
     "accounts": [
       { "id": "claude_code-default", "provider": "claude_code", "account_config": { "claude_dir": "/custom/path/.claude" } }
     ]
   }
   ```

## Style C: local service

Affected: `ollama`.

### Checklist

1. **Is the local server reachable?**
   ```bash
   curl -sS http://127.0.0.1:11434/api/tags | head -1
   ```
   Non-200 or no response → start `ollama serve` (or the macOS app).

2. **Is it bound to a non-default port or host?** Set `base_url` on the account:
   ```json
   { "id": "ollama-remote", "provider": "ollama", "base_url": "http://10.0.0.5:11434" }
   ```

3. **Cloud Ollama**: set `OLLAMA_API_KEY` for the cloud endpoints.

4. **Logs.** Server-log derived metrics need readable log files:
   - Linux: `/tmp/ollama.log`
   - macOS: `~/Library/Logs/Ollama/`
   - Windows: `%LOCALAPPDATA%\Ollama\logs`

## Verifying detection

Run with debug logging:

```bash
OPENUSAGE_DEBUG=1 openusage 2> /tmp/openusage-detect.log
```

Quit and grep:

```bash
grep -i 'detect\|skip\|provider' /tmp/openusage-detect.log
```

Each missed provider prints a reason (env var missing, binary not found, dir absent, etc).

## Manual override

If detection is fundamentally broken on your setup, you can always declare an account manually. Auto-detect's default path is convenient but not the source of truth — `settings.json` is.

```json
{
  "auto_detect": false,
  "accounts": [
    { "id": "openai-manual", "provider": "openai", "api_key_env": "OPENAI_API_KEY" }
  ]
}
```

Setting `auto_detect: false` makes the manual list authoritative.

## See also

- [Auto-detection](../concepts/auto-detection.md)
- [Common issues](common-issues.md)
- [Debug mode](debug-mode.md)
