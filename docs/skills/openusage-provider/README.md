# OpenUsage Provider Skill - Installation Guide

## Quick Install

To use this skill with OpenCode, copy the skill files to your OpenCode skills directory:

```bash
# Create the skill directory
mkdir -p ~/.config/opencode/skills/openusage-provider

# Copy the skill files
cp docs/skills/openusage-provider/SKILL.md ~/.config/opencode/skills/openusage-provider/
cp docs/skills/openusage-provider/skill.json ~/.config/opencode/skills/openusage-provider/

# Verify installation
ls -la ~/.config/opencode/skills/openusage-provider/
```

## Usage

Once installed, the skill will automatically trigger when you mention adding a new provider:

- "Add a new provider for Z.ai"
- "Create provider for Cerebras"
- "Implement Together AI provider"
- "Add new AI provider"

The skill will guide you through a 6-phase process:

1. **Phase 0** - Quiz the user for provider details
2. **Phase 1** - Research the provider's API
3. **Phase 2** - Create the provider package
4. **Phase 3** - Configure dashboard widget
5. **Phase 4** - Register and auto-detect
6. **Phase 5** - Write tests
7. **Phase 6** - Verify implementation

## Files Created

When adding a new provider, the skill will create:

```
internal/providers/<provider_id>/
├── <provider_id>.go       # Main provider implementation
├── <provider_id>_test.go  # Unit tests
└── widget.go              # Dashboard widget (if needed)
```

And update:

- `internal/providers/registry.go` - Add to AllProviders()
- `internal/detect/detect.go` - Add env key mapping or detection
- `configs/example_settings.json` - Add example account config

## Reference

See the main skill file for complete documentation:
- `docs/skills/openusage-provider/SKILL.md`

Or after install:
- `~/.config/opencode/skills/openusage-provider/SKILL.md`
