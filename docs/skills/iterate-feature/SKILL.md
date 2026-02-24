# Skill: Iterate Feature

Fix issues, address feedback, and iterate on a feature implementation until it's ready for review.

## When to use

- After `/validate-feature` reports issues (verdict: NEEDS ITERATION)
- After PR review feedback
- After manual testing reveals problems
- When user reports bugs or requests changes to an in-progress feature

## Prerequisite

- A design doc in `docs/*_DESIGN.md`
- Implementation exists (code in working tree)
- At least one of: validation report, PR review comments, user feedback

## Phases

### Phase 0 — Load Context

1. Find and read the design doc for the feature.
2. Gather all feedback sources:
   - Validation report (from `/validate-feature` — if available in conversation context)
   - PR review comments (if user provides PR URL, fetch with `gh pr view` and `gh api`)
   - User-provided feedback (from conversation)
3. Read all files that were changed as part of the feature (use `git diff main --name-only`).
4. Summarize the current state: what's implemented, what's broken, what needs work.

### Phase 1 — Triage

Categorize every issue into one of these buckets:

| Category | Priority | Description |
|----------|----------|-------------|
| **Bug** | P0 | Code doesn't work as designed — wrong behavior, crashes, test failures |
| **Design gap** | P1 | Design doc missed something — new requirement discovered during implementation |
| **Quality** | P2 | Code works but has quality issues — missing tests, poor error messages, debug artifacts |
| **Polish** | P3 | Nice-to-have improvements — better naming, clearer comments, minor UX tweaks |

Present the triaged list:
```
## Triage

### P0 — Bugs
1. config_test.go: two test cases reference removed hourly windows

### P1 — Design Gaps
(none)

### P2 — Quality
1. Missing test for LargestWindowFitting helper

### P3 — Polish
(none)
```

Ask user: "Proceed with all issues, or pick specific ones?"

### Phase 2 — Plan Iterations

For each issue (in priority order), plan the fix:

```
## Iteration Plan

### Fix 1: config_test.go hourly window references (P0)
- Files: internal/config/config_test.go
- Approach: Update test expectations since "1h" and "6h" are no longer valid windows
- Risk: low
- Depends on: nothing

### Fix 2: Add LargestWindowFitting test (P2)
- Files: internal/core/time_window_test.go
- Approach: Add table-driven test with edge cases (0, 1, 7, 14, 30, 90 days)
- Risk: low
- Depends on: nothing
```

Identify parallelizable fixes (same rules as `/implement-feature` — no mutual file dependencies).

Ask: "Proceed with this plan?"

### Phase 3 — Execute Iterations

For each fix, follow this loop:

1. **Read** — Re-read affected files to understand current state
2. **Fix** — Make the minimal change that addresses the issue
3. **Test** — Run `go test ./<package>/... -count=1` for affected packages
4. **Diagnose** — Run `go_diagnostics` on modified files
5. **Verify** — Confirm the specific issue is resolved

After each fix, report:
```
### Fix 1: config_test.go hourly window references ✓
- Changed: internal/config/config_test.go (2 test cases updated)
- Tests: 8/8 passing
- Status: RESOLVED
```

If a fix introduces new issues:
- Stop and report the regression
- Assess whether the fix approach is wrong
- Ask user if the approach should change

If a fix requires design changes:
- Flag it explicitly
- Propose the design doc update
- Wait for user approval before editing the design doc

### Phase 4 — Re-validate

After all fixes are applied:

1. `make build` — must pass
2. `go test <all changed packages> -count=1 -race` — must pass
3. `make vet` — must pass
4. If Phase 3 introduced new files or significant changes, re-run the design compliance check from `/validate-feature` Phase 3

Report:
```
## Re-validation
- Build: PASS
- Tests: PASS (N packages, M tests)
- Vet: PASS
- Design compliance: N/N tasks complete
```

If re-validation fails, loop back to Phase 3 for the new issues.

### Phase 5 — Iteration Summary

```
## Iteration Summary

Feature: <name>
Design doc: <path>
Iterations: N fixes applied

### Changes
| Fix | Category | Files Changed | Status |
|-----|----------|---------------|--------|
| Fix 1: description | P0 Bug | file.go | RESOLVED |
| Fix 2: description | P2 Quality | file_test.go | RESOLVED |

### Design Doc Updates
- <any changes made to design doc, or "none">

### Re-validation
- Build: PASS
- Tests: PASS
- Vet: PASS

### Verdict
READY FOR REVIEW / NEEDS MORE ITERATION
```

## Rules

1. Fix in priority order — P0 bugs before P2 quality issues.
2. Minimal changes only — fix the issue, don't refactor surrounding code.
3. Never skip re-validation — every iteration round must end with a clean build and passing tests.
4. Design doc is living documentation — if iteration reveals design gaps, update the doc.
5. Always ask before changing scope — if a fix implies new features or architectural changes, get user approval.
6. Track what changed and why — the iteration summary is the audit trail.
7. If the same issue keeps recurring after 2 fix attempts, stop and escalate to the user.
8. PR review feedback takes priority — if iterating from review comments, address reviewer concerns first.

## Checklist

Before marking iteration complete:
- [ ] All P0 bugs resolved
- [ ] All P1 design gaps addressed (or explicitly deferred with user approval)
- [ ] All P2 quality issues fixed
- [ ] P3 polish applied (if user approved)
- [ ] Build compiles cleanly
- [ ] All tests pass with -race
- [ ] Design doc updated if scope changed
- [ ] Iteration summary produced
- [ ] Re-validation passes
