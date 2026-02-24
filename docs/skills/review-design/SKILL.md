---
name: review-design
scope: project
description: Review a design doc against the actual codebase, find inconsistencies, and quiz the user on needed fixes. Loops until all issues are resolved. Use after /design-feature and before /implement-feature.
keywords: review, design, validate, quiz, codebase, verify
---

# OpenUsage Design Doc Reviewer

**Invocation**: When a user wants to validate a design doc against the current codebase before implementing it.

**Input**: Design doc path or feature name (resolved to `docs/<NAME>_DESIGN.md`).

---

## Phase 0 — Load Design

1. Find the design doc. If path not given, search `docs/*_DESIGN.md`.
2. Read the full design doc. Extract:
   - Problem statement
   - Affected subsystems
   - Type definitions and interface changes
   - Implementation tasks with file lists
3. Confirm with the user which design doc to review.

---

## Phase 1 — Codebase Audit

For each subsystem listed in the design doc's impact analysis:

1. Read the primary files from `docs/skills/design-feature/references/subsystem-map.md`.
2. Read every file referenced in the implementation tasks.
3. Build a **discrepancy list** by checking each item in `references/review-checklist.md`.

Focus on concrete mismatches. Don't flag style preferences or hypotheticals.

---

## Phase 2 — Quiz Loop (CORE)

Present findings one at a time as questions. For each discrepancy:

```
## Issue N: <short title>

**Design says**: <quote or paraphrase from design doc>
**Codebase says**: <what actually exists, with file:line reference>
**Severity**: breaking | incorrect | stale | minor

**Question**: <specific question asking the user how to resolve this>
Options:
  a) Update the design doc to match the codebase
  b) Keep the design (codebase will change during implementation)
  c) Something else — explain
```

**Rules for the quiz loop:**
- Present 1-3 issues per round (group related issues).
- Wait for the user's answer before proceeding.
- After the user answers, apply their decision:
  - If (a): edit the design doc immediately.
  - If (b): note it as "intentional change" — no edit needed.
  - If (c): discuss, then apply the agreed resolution.
- After resolving a batch, re-scan affected areas for new issues introduced by the edits.
- **Repeat until no discrepancies remain.**

---

## Phase 3 — Final Verification

When no more issues are found:

1. Re-read the design doc (it may have been edited during the quiz).
2. Verify all implementation tasks still reference valid files and types.
3. Confirm the task dependency order is still correct.
4. Report:

```
## Review Complete

Design doc: <path>
Issues found: N
Issues resolved: N
Design doc edits: N

### Resolutions
- Issue 1: <title> — <resolution summary>
- Issue 2: <title> — <resolution summary>

### Ready for implementation
The design doc is consistent with the codebase. Run /implement-feature <name> to proceed.
```

---

## Rules

- **Never skip Phase 1.** Always audit the codebase — don't trust the design doc blindly.
- **Never auto-fix without asking.** Every discrepancy goes through the quiz.
- **Always cite file:line.** Vague references like "in the types file" are not acceptable.
- **Stop if the design doc is fundamentally broken** (e.g., references a subsystem that doesn't exist). Tell the user to re-run `/design-feature`.
- **Don't review code style.** Focus on structural correctness: types, interfaces, function signatures, file locations, config schema.
- **Group related issues.** If three fields on the same struct are wrong, that's one issue, not three.
