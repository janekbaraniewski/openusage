# Dev Workflow Audit Checklist

## 1. Tool Config Sync

For each generated file, verify it matches what `make sync-tools` would produce:

| File | Source |
|------|--------|
| `.continuerules` | `template.md` with title "Continue.dev Rules" |
| `.windsurfrules` | `template.md` with title "Windsurf Rules" |
| `.github/copilot-instructions.md` | `template.md` with title "GitHub Copilot Instructions" |
| `.aider/conventions.md` | `template.md` with title "Aider Conventions" |

## 2. Skill Registration

For each skill in `docs/skills/*/SKILL.md`:

- [ ] Row exists in `docs/skills/tool-configs/skills-table.md`
- [ ] Entry exists in `.claude/commands/<skill-name>.md`
- [ ] Entry exists in `.opencode/skills/<skill-name>/SKILL.md`
- [ ] Entry exists in CLAUDE.md's skills table (if applicable)

## 3. Skill Quality

For each SKILL.md:

- [ ] Has clear "When to use" section
- [ ] Has numbered phases
- [ ] All referenced files exist on disk
- [ ] All referenced skills exist in `docs/skills/`
- [ ] References directory paths use correct format
- [ ] No TODO or FIXME markers left in

## 4. Template Completeness

The template (`docs/skills/tool-configs/template.md`) should contain:

- [ ] Project overview (what is OpenUsage, key tech: Go, Bubble Tea, CGO)
- [ ] Key commands (make build, test, vet, single provider test)
- [ ] Code style (gofmt, imports, error wrapping, pointer fields, JSON tags, testing)
- [ ] Architecture (core interface, registry, detect, config path)
- [ ] Skills table (via `{{SKILLS_TABLE}}` placeholder)
- [ ] Mandatory phase rule

## 5. Generator Completeness

The generator (`scripts/sync-tool-configs.sh`) should:

- [ ] Generate all 4 tool config files
- [ ] Generate all OpenCode skill stubs
- [ ] Generate all Claude command stubs
- [ ] Be idempotent (running twice produces same output)
- [ ] Handle new skills added to `docs/skills/` automatically
- [ ] Have descriptions for each skill in both SKILL_DESCRIPTIONS and CLAUDE_DESCRIPTIONS arrays

## 6. Cross-References

| From | To | Check |
|------|----|-------|
| CLAUDE.md skills table | `skills-table.md` | Content matches |
| `.claude/commands/*.md` | `docs/skills/*/SKILL.md` | Every command has a skill |
| `docs/skills/*/SKILL.md` | Referenced source files | Files exist |
| `docs/skills/develop-feature/SKILL.md` | All other skills | Skill names match |
