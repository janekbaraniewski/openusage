---
name: code-review
description: "Two-axis review (Standards + Spec) of a diff or working tree. Use after implementation, when reviewing a branch/PR, or when the verifier audits a change. Antigravity-adapted from Matt Pocock skills."
---

# Code review (Standards + Spec)

Vendored from [mattpocock/skills](https://github.com/mattpocock/skills) (MIT), adapted for Antigravity.

## Antigravity adaptation (read first)

Ignore any upstream instruction to run `/setup-matt-pocock-skills` or to read `docs/agents/issue-tracker.md` — those files are **not** part of this kit.

| Need | Use this instead |
|------|------------------|
| Diff to review | Prefer **working tree**: `git diff` and `git diff --cached`. If the user names a fixed point (`main`, SHA, tag), use `git diff <fixed-point>...HEAD`. |
| Spec / requirements | Task text, file plan, `/plan` artifact, or a path under `docs/` / `specs/`. If none, Spec axis = "no spec — review against user message only". |
| Issue tracker | Optional. Skip if unavailable. |
| Parallel sub-agents | Optional. You may run both axes yourself in one pass (typical for the `verifier` agent). |

Also enforce **file-plan scope**: any path in the diff not on the approved file plan is a Spec failure (scope creep).

## Axes

- **Standards**: does the code conform to this repo's documented standards (`AGENTS.md`, `.agents/rules/`, `CODING_STANDARDS.md`, `CONTRIBUTING.md` if present)?
- **Spec**: does the code implement what was asked — nothing missing, nothing extra?

Report the axes **separately**. Do not merge rankings.

## Process

### 1. Pin the diff

- Default: unstaged + staged working tree.
- Or user-supplied fixed point (confirm with `git rev-parse`).
- Empty diff → stop and say there is nothing to review.

### 2. Spec source

1. Explicit task / file plan in the prompt
2. User-supplied path
3. Matching file under `docs/`, `specs/`, or `.scratch/`
4. Else: user message only; note "no formal spec"

### 3. Standards sources + smell baseline

Use repo docs when present. Always apply the **smell baseline** below as judgement calls (not hard fails). Repo docs override the baseline. Skip anything already enforced by tooling (linters).

- **Mysterious Name** → rename; if no honest name, design is murky
- **Duplicated Code** → extract shared shape
- **Feature Envy** → move behaviour onto the data it uses
- **Data Clumps** → bundle into one type
- **Primitive Obsession** → small domain type
- **Repeated Switches** → polymorphism or shared map
- **Shotgun Surgery** → gather what changes together
- **Divergent Change** → split by reason to change
- **Speculative Generality** → delete unused abstraction
- **Message Chains** → hide behind one method
- **Middle Man** → call the real target
- **Refused Bequest** → prefer composition

### 4. Review

For each axis, under 400 words, with `file:line` evidence where possible.

**Standards brief:** documented violations + baseline smells; distinguish hard vs judgement.

**Spec brief:** (a) missing/partial requirements (b) unrequested behaviour / unplanned files (c) wrong implementation — quote the requirement.

### 5. Aggregate

```markdown
## Standards
...

## Spec
...

## Summary
Standards: N findings (worst: …). Spec: N findings (worst: …).
```

## Why two axes

- Right behaviour, wrong style → Spec pass, Standards fail
- Clean code, wrong behaviour → Standards pass, Spec fail
