# Per-Task Execution Checklist

Use this checklist for every implementation task. Do not skip steps.

## Before coding

- [ ] Read all files listed in the task's `Files:` field
- [ ] Identify existing patterns (naming, error handling, test style)
- [ ] Confirm dependencies (prior tasks) are complete and tests pass
- [ ] Check if any types/functions referenced in the design doc have changed since writing

## While coding

- [ ] Match existing code style exactly (gofmt, import groups, error wrapping)
- [ ] Add only what the design specifies — nothing more
- [ ] Use type definitions from the design doc verbatim (unless they conflict with current code)
- [ ] Wire new code into existing call sites as specified

## After coding

- [ ] Run `go_diagnostics` on all modified files — zero errors
- [ ] Write tests as specified in the task
- [ ] Run package tests: `go test ./<package>/... -count=1` — all pass
- [ ] Report: task title, files changed, test status

## If something goes wrong

- **Test failure**: Fix before moving on
- **Design conflict**: Flag to user, do not improvise
- **Missing dependency**: Check if a prior task was skipped
- **Stale reference**: Stop and report — the design doc may need updating
- **Scope change from user**: Implement it, update the design doc, re-evaluate remaining tasks

---

## Parallel Execution Checklist

Use this checklist when launching tasks as parallel agents.

### Before launching agents

- [ ] Verify all prerequisite tasks are complete and tests pass
- [ ] Confirm tasks in the parallel group have no mutual dependencies
- [ ] Prepare detailed prompts for each agent with full context (agents cannot see each other's work)
- [ ] Each agent prompt must include: task description, file list, design doc excerpt, existing code patterns, expected test patterns

### Agent prompt template

Each parallel agent should receive:
1. The specific task description from the design doc
2. The relevant file contents (or instructions to read them)
3. Coding conventions from CLAUDE.md
4. The package's existing test patterns
5. Clear instruction: "Write code, write tests, run diagnostics, run package tests. Report what you changed."

### After all agents complete

- [ ] Run `go build ./...` — all agent changes must compile together
- [ ] Run `go test` for ALL packages touched by ANY agent in the group
- [ ] Check for signature mismatches (function signatures changed by one agent but callers in other files not updated)
- [ ] Check for test helper mismatches (test utilities using old function signatures)
- [ ] Fix any integration issues before proceeding to the next group
- [ ] Report which issues were found and how they were resolved

### Common parallel execution pitfalls

| Problem | Cause | Fix |
|---------|-------|-----|
| Compile error in test file | Agent A changed function signature, test helper uses old signature | Update the test helper to match new signature |
| Duplicate imports | Two agents added the same dependency differently | Reconcile import styles |
| Conflicting field names | Two agents added fields to the same struct | Merge and reconcile |
| Missing wire-up | Agent expected another agent's output but got nothing | Add the missing connection in integration verification |
