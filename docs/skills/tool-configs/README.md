# Tool Config Templates

This directory contains the **single source of truth** for all AI tool configuration files in the repository.

## How it works

1. `template.md` defines the canonical content shared across all tool configs
2. `scripts/sync-tool-configs.sh` generates tool-specific configs from the template
3. `make sync-tools` runs the generator

## Generated files

| Tool | Generated file |
|------|---------------|
| Continue.dev | `.continuerules` |
| Windsurf | `.windsurfrules` |
| GitHub Copilot | `.github/copilot-instructions.md` |
| Aider | `.aider/conventions.md` |
| OpenCode | `.opencode/skills/*/SKILL.md` |

Claude Code commands (`.claude/commands/`) are **not** generated — they use a different format (slash command stubs that reference `docs/skills/`).

## When to update

1. Edit `template.md` (the source of truth)
2. Run `make sync-tools`
3. Commit all generated files together

Never edit the generated files directly — they'll be overwritten on next sync.
