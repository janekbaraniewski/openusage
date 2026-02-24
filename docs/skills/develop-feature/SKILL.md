# Skill: Develop Feature

End-to-end feature development — from idea to pull request in a single command.

## When to use

When you want to design, implement, validate, and ship a feature without manually invoking each skill. This skill orchestrates the full development lifecycle.

## What it does

Chains these skills in order, with user decision points between each:

```
/design-feature    → Design doc + implementation tasks
       ↓
/review-design     → Validate design against codebase
       ↓
/implement-feature → Execute implementation tasks
       ↓
/validate-feature  → Verify completeness and quality
       ↓
/iterate-feature   → Fix issues (if needed, may loop)
       ↓
/finalize-feature  → Branch, commit, PR
```

## Phases

### Phase 0 — Intake

1. Accept the feature name/description from the user.
2. Check if a design doc already exists for this feature (search `docs/*_DESIGN.md`).
   - If found: ask "Design doc exists at `<path>`. Skip design phase and start from review/implementation?"
   - If not found: proceed with full lifecycle.
3. Ask: "Full lifecycle (design → PR), or start from a specific phase?"
   - Options: full, design-only, implement (skip design), validate-only, iterate-only, finalize-only
   - Default: full lifecycle

### Phase 1 — Design

**Skill**: `/design-feature`

1. Execute the design-feature skill for the given feature name.
2. This produces `docs/<FEATURE_NAME>_DESIGN.md` with problem statement, design, and implementation tasks.
3. **Decision point**: Present the design doc summary to user.
   - Ask: "Design complete. Review it now, or proceed directly to implementation?"
   - If review: continue to Phase 2
   - If skip review: jump to Phase 3

### Phase 2 — Review

**Skill**: `/review-design`

1. Execute the review-design skill against the design doc.
2. This validates the design against the actual codebase and fixes discrepancies.
3. **Decision point**: After review completes:
   - Ask: "Design reviewed and updated. Ready to implement?"
   - If yes: continue to Phase 3
   - If no: user may want to manually edit the design doc first

### Phase 3 — Implement

**Skill**: `/implement-feature`

1. Execute the implement-feature skill for the feature.
2. This includes: loading design, codebase analysis, pre-implementation quiz, execution plan, task implementation with tests, integration checks.
3. **Decision point**: After implementation summary:
   - Ask: "Implementation complete. Run validation?"
   - If yes: continue to Phase 4
   - If no: user may want to manually test first

### Phase 4 — Validate

**Skill**: `/validate-feature`

1. Execute the validate-feature skill.
2. This checks: build, tests, design compliance, code quality, integration.
3. **Decision point**: Based on verdict:
   - If READY FOR REVIEW: ask "Validation passed. Finalize (branch + PR)?"
     - If yes: jump to Phase 6
     - If no: stop here
   - If NEEDS ITERATION: ask "Issues found. Run iteration to fix them?"
     - If yes: continue to Phase 5
     - If no: stop here, user will fix manually

### Phase 5 — Iterate

**Skill**: `/iterate-feature`

1. Execute the iterate-feature skill with the issues from validation.
2. This triages issues, plans fixes, executes them, and re-validates.
3. **Loop**: If re-validation still shows issues:
   - Ask: "Some issues remain. Run another iteration round?"
   - If yes: repeat Phase 5
   - If no: proceed to Phase 6 anyway (user accepts current state) or stop
4. Maximum 3 iteration rounds before requiring user decision on whether to continue.
5. After clean re-validation: ask "All issues resolved. Finalize?"

### Phase 6 — Finalize

**Skill**: `/finalize-feature`

1. Execute the finalize-feature skill.
2. This creates the branch, commits with proper message, and opens a PR.
3. Report the PR URL.

### Phase 7 — Summary

Produce a lifecycle summary:

```
## Development Complete

Feature: <name>
Design doc: <path>
PR: <url>

### Lifecycle
| Phase | Status | Duration |
|-------|--------|----------|
| Design | COMPLETE | — |
| Review | COMPLETE | — |
| Implementation | COMPLETE (N tasks) | — |
| Validation | PASS | — |
| Iteration | 1 round (3 fixes) | — |
| Finalization | PR #123 opened | — |

### Files Changed
<count> files across <count> packages

### Tests Added
<count> new test functions

### Design Doc
<path> (updated during implementation)
```

## Decision Points Summary

The skill pauses at these points for user input:

1. **After intake**: Full lifecycle or specific phase?
2. **After design**: Review or skip to implementation?
3. **After review**: Ready to implement?
4. **After implementation**: Run validation?
5. **After validation**: Finalize or iterate?
6. **After iteration**: Finalize or iterate again?
7. **After finalization**: Done!

Each pause is a natural stopping point. Users can exit at any phase and resume later using the individual skill commands.

## Rules

1. Always pause at decision points — never auto-proceed through the full lifecycle without user confirmation.
2. If any phase fails catastrophically (build broken, design fundamentally flawed), stop and report. Don't try to push through.
3. Respect phase skip requests — if user says "skip design, I already have a design doc", start from review or implementation.
4. Maximum 3 iteration rounds before escalating — if issues persist after 3 rounds, something is fundamentally wrong.
5. Each phase follows its own skill's rules completely — this skill only orchestrates, it doesn't override individual skill behavior.
6. If the user interrupts or changes direction mid-lifecycle, adapt gracefully. The lifecycle is a guide, not a cage.
7. Track which phases completed so the user can resume from where they left off if the session ends.

## Quick Reference

```
# Full lifecycle
/develop-feature "daily spend trends"

# Skip design (design doc already exists)
/develop-feature "daily spend trends"
→ "Design doc exists. Skip design phase and start from review/implementation?"
→ "implement"

# Just validate + iterate + finalize
/develop-feature "daily spend trends"
→ "Start from a specific phase?"
→ "validate"
```
