---
name: minimal-scope
description: Enforces minimal file touch for code changes. Use before and during implementation when editing files, fixing bugs, or adding features. Requires explicit file plan and blocks drive-by refactors.
---

# Minimal scope

## When to use

- Any task that modifies source files
- Before implementer starts editing
- When verifier checks scope creep

## Workflow

### 1. Establish the file plan

Write or obtain a list:

```
PLANNED FILES:
- path (modify|create) — one-line reason
OUT OF SCOPE:
- directories or files that must not change
```

If the plan is missing or vague, invoke **explorer** or ask the user.

### 2. Pre-edit gate

Before the first edit, confirm:

- [ ] Every intended path is listed
- [ ] No "while I'm here" items included
- [ ] Test command identified

### 3. During implementation

- Edit planned paths only
- If a new path is required: **stop**, add to plan with reason, then continue
- Do not run repo-wide formatters unless the task is explicitly formatting

### 4. Post-edit scope check

```bash
git diff --name-only
git diff --cached --name-only
```

Every output path must appear in PLANNED FILES. Otherwise: **scope failure** — revert extra files or update plan and re-verify.

## Anti-patterns (reject)

| Pattern | Why |
|---------|-----|
| Touching 10+ files for a 1-file bugfix | Likely drive-by refactor |
| Changing imports/style in unrelated packages | Formatting sweep |
| Adding helpers used once | Over-engineering |
| New config/env for a local fix | Speculative generality |

## Completion criterion

Scope check passes: diff paths ⊆ planned paths, and verifier agrees.
