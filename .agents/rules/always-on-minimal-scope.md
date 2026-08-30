# Minimal scope (always on)

Apply to every implementation task.

## File touch rules

1. **Plan before edit** — No file changes until a written file list exists (coordinator, explorer, or `/plan` / `to-spec` output).
2. **Listed files only** — Implementer may create/modify only paths on the plan.
3. **No collateral edits** — Do not reformat, rename, or refactor files outside the plan.
4. **No new dependencies** — Do not add packages, tools, or config unless listed in the plan.
5. **Verifier blocks scope creep** — Any `git diff` path not on the plan fails verification.

## When extra files are allowed

Only if the user or coordinator **updates the plan in writing** before the edit, with a one-line reason per file.

## Question-only tasks

If the user asks a question without requesting code changes, do not edit any files.

## Antigravity UI tip

In Customizations → Rules, set this rule to **Always On** (or keep it bound via agent `rules:` frontmatter). Bound via agents in this kit by default.
