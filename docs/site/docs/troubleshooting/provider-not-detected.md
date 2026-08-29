---
title: Provider not detected
description: Per-detection-style checklists for finding why a provider isn't showing up in the dashboard.
---

Auto-detection runs in three styles. Use the checklist for the style that matches the missing provider.

The fastest way to see what was found and what's missing is the dedicated subcommand:

```bash
openusage detect          # show tools, accounts (with masked tokens) and source provenance
openusage detect --all    # also list every registered provider
```

The `SOURCE` column tells you exactly where each credential came from (`env`, `shell_rc:/path`, `aider_yaml:/path`, `opencode_auth_json`, `keychain:…`). The trailing "No credentials found for:" list is the authoritative inventory of what's still missing.

## Style A: env var providers

Affected: `openai`, `anthropic`, `openrouter`, `groq`, `mistral`, `deepseek`, `xai`, `gemini_api`, `alibaba_cloud`, `moonshot`, `zai`, `opencode`.

OpenUsage looks for these keys in this order: process environment → shell rc files (`~/.zshrc`, `~/.bashrc`, fish, modular `~/.zshrc.d/*` etc.) → tool config files (Aider's `.aider.conf.yml`/`.env`, OpenCode's `auth.json`, Codex's `auth.json` `OPENAI_API_KEY` field).

### Checklist

1. **Run `openusage detect`** — if your provider appears with a `SOURCE` column entry, detection is working and the issue is elsewhere (open a [GitHub issue](https://github.com/janekbaraniewski/openusage/issues)).

2. **Is the env var set in the shell that launches OpenUsage, *or* in one of the supported file sources?**
   ```bash
   echo "${OPENAI_API_KEY+set}"
   grep -E "^(export +)?OPENAI_API_KEY=" ~/.zshrc ~/.zshenv ~/.zshrc.d/*.zsh 2>/dev/null
   ```
   If neither prints anything, OpenUsage will not find the key.

3. **Is it `export`ed, not just assigned?** Plain `VAR=value` lines are detected too, but they need to be at the start of a line and not embedded in shell logic.
   ```bash
   # Both of these are picked up from a rc file:
   export OPENAI_API_KEY=sk-...
   OPENAI_API_KEY=sk-...
   ```

4. **Are there shell substitutions in the value?** Lines like `export OPENAI_API_KEY=$(pass openai)` or `export FOO="$BAR"` are intentionally skipped — OpenUsage never invokes a shell. Either pre-resolve the value or set it via the process environment.

5. **Is the variable name spelled exactly right?** Case matters. `Openai_Api_Key` will not be picked up.

6. **For providers with multiple accepted names** (Z.AI accepts `ZAI_API_KEY` or `ZHIPUAI_API_KEY`; OpenCode accepts `OPENCODE_API_KEY` or `ZEN_API_KEY`), at least one must be set.

7. **Is `auto_detect` enabled?** In `settings.json`:
   ```json
   { "auto_detect": true }
   ```
   If false, no auto-detection happens.

8. **GUI launches still work** for shell-rc-stored keys: OpenUsage parses `~/.zshrc` and friends directly, so launching from Spotlight/Dock no longer requires re-exporting in launchd. macOS keychain entries (Claude Code) are also picked up regardless of how you launched.

### Windows / PowerShell

1. **Set the key as an environment variable, not a PowerShell variable.** This is the most common Windows trap. `$OPENROUTER_API_KEY = "sk-..."` creates a *PowerShell variable*, which `openusage` cannot see. You need an *environment variable*:
   ```powershell
   $env:OPENROUTER_API_KEY = "sk-..."   # current session only
   setx OPENROUTER_API_KEY "sk-..."     # persists to new sessions (reopen the terminal)
   ```
   Quick check: `Get-ChildItem Env:OPENROUTER_API_KEY` lists it when it is a real environment variable. `Get-Variable OPENROUTER_API_KEY` succeeding means you only set a PowerShell variable, and so does `OPENUSAGE_DEBUG` if you set it the same way (in which case debug logging never actually turns on).

2. **OpenCode's `auth.json` lives at the XDG-style path on Windows.** Because OpenCode resolves its data directory via `xdg-basedir` (which has no Windows branch), it writes to `%USERPROFILE%\.local\share\opencode\auth.json`, not under `%APPDATA%`. Auto-detection probes that location first. If your key is elsewhere, point `XDG_DATA_HOME` at its parent or set `OPENCODE_API_KEY` directly. See the [paths reference](../reference/paths.md#tool-integration-paths).

## Style B: local binary + config dir

Affected: `claude_code`, `codex`, `cursor`, `copilot`, `gemini_cli`, `antigravity`.

### Checklist

1. **Is the binary on `$PATH`?**
   ```bash
   which claude
   which codex
   which gemini
   which agy
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
   | Antigravity CLI | `~/.gemini/antigravity-cli/` |

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

On Windows PowerShell:

```powershell
$env:OPENUSAGE_DEBUG = "1"
openusage detect          # prints the same [detect] reasons inline
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
