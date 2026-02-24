# Skill: Dev Workflow Improvements

Audit and improve the OpenUsage development workflow. Ensures the dev flow is complete, consistent, and propagated to all AI tools.

## When to use

- After adding/modifying a skill in `docs/skills/`
- After changing tool config content (code style, architecture, commands)
- When onboarding a new AI tool
- Periodically to check for drift or staleness
- When the development flow feels broken or incomplete

## Architecture

### Source of truth

```
docs/skills/tool-configs/template.md      ← shared content (style, architecture, commands)
docs/skills/tool-configs/skills-table.md   ← skills registry (single table, all skills)
docs/skills/<skill-name>/SKILL.md          ← individual skill specifications
```

### Generated files (never edit directly)

```
.continuerules                    ← Continue.dev
.windsurfrules                    ← Windsurf
.github/copilot-instructions.md  ← GitHub Copilot
.aider/conventions.md            ← Aider
.opencode/skills/*/SKILL.md      ← OpenCode (thin stubs → docs/skills/)
.claude/commands/*.md             ← Claude Code (slash command stubs)
```

### Generator

`scripts/sync-tool-configs.sh` — reads template + skills table, writes all generated files.
Run via `make sync-tools`.

## Phases

### Phase 0 — Audit

1. Run `make sync-tools` and check if any files changed (`git diff`).
   - If changes: report which files drifted and what changed.
   - If clean: report "All tool configs are in sync."

2. Validate skill completeness:
   - For each directory in `docs/skills/*/`:
     - Has a `SKILL.md`?
     - Listed in `docs/skills/tool-configs/skills-table.md`?
     - Has a matching `.claude/commands/<name>.md`?
     - Has a matching `.opencode/skills/<name>/SKILL.md`?
   - Report any gaps.

3. Validate skill references:
   - For each skill's SKILL.md, check that referenced files exist:
     - `references/*.md` files mentioned in the skill
     - Source files mentioned (e.g., `internal/core/types.go`)
     - Other skills referenced (e.g., `/design-feature`)
   - Report broken references.

4. Check CLAUDE.md skills table matches `skills-table.md`:
   - Compare the skills table in `CLAUDE.md` with `docs/skills/tool-configs/skills-table.md`.
   - Report any mismatches.

### Phase 1 — Fix

Based on audit findings, fix issues in priority order:

1. **Sync drift**: Run `make sync-tools` to regenerate.
2. **Missing registrations**: Add skill to `skills-table.md`, re-run sync.
3. **Missing stubs**: The sync script generates these automatically.
4. **Broken references**: Fix or remove stale file references in SKILL.md.
5. **CLAUDE.md mismatch**: Update the skills table in CLAUDE.md.

After each fix category, re-run the audit for that category to confirm.

### Phase 2 — Improve

If the user requested workflow improvements (not just sync):

1. **Quiz** (ask the user):
   - What part of the workflow feels broken or incomplete?
   - Any new skills needed?
   - Any existing skills that need updating?
   - Any new AI tools to onboard?

2. **Execute improvements** based on answers:
   - New skill: create `docs/skills/<name>/SKILL.md`, add to skills-table, run sync.
   - Update skill: edit the SKILL.md, check if tool configs need template changes, run sync.
   - New tool: add to `scripts/sync-tool-configs.sh`, add generation target, run sync.
   - Workflow gap: identify the gap, propose a fix, implement after user approval.

3. **Final sync**: Run `make sync-tools` to propagate all changes.

### Phase 3 — Verify

1. Run `make sync-tools` — should produce no changes (idempotent).
2. Run `make build` — project still compiles.
3. Run `make test` — tests still pass.
4. `git diff` — show all changes for user review.

## Rules

1. NEVER edit generated files directly — always edit the source of truth and run sync.
2. Every new skill MUST be added to `skills-table.md` — this is the single registry.
3. After ANY skill change, run `make sync-tools` before committing.
4. The template is authoritative — if a tool config disagrees, the template wins.
5. Claude commands get more detail than other tools (phase breakdowns) — this is intentional.
6. OpenCode skills are thin stubs pointing to `docs/skills/` — don't duplicate content.

## Adding a new AI tool

1. Add a `generate_config` call in `scripts/sync-tool-configs.sh`.
2. Add the output file to the "Generated files" list in this doc.
3. Add the output file to `docs/skills/tool-configs/README.md`.
4. Run `make sync-tools`.
5. Commit the script change + generated file together.

## Checklist

- [ ] `make sync-tools` produces no diff (all configs in sync)
- [ ] Every `docs/skills/*/SKILL.md` is in `skills-table.md`
- [ ] Every skill has a `.claude/commands/<name>.md` stub
- [ ] Every skill has a `.opencode/skills/<name>/SKILL.md` stub
- [ ] CLAUDE.md skills table matches `skills-table.md`
- [ ] No broken file references in any SKILL.md
- [ ] `make build` passes
- [ ] `make test` passes
