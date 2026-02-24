---
name: implement-feature
scope: project
description: Implement a feature from an existing design doc. Reads the design, analyzes the codebase, validates assumptions via interactive quiz, plans execution with parallelization, implements tasks, and validates. Use after running /design-feature.
keywords: implement, build, code, execute, feature, tasks
---

# OpenUsage Feature Implementer

**Invocation**: When a user wants to implement a feature that has a design doc in `docs/*_DESIGN.md`. Always requires a design doc — if none exists, tell the user to run `/design-feature` first.

**Input**: Design doc path or feature name (resolved to `docs/<NAME>_DESIGN.md`).

---

## Phase 0 — Load Design

1. Read the design doc. If the path wasn't given, search `docs/*_DESIGN.md` for a match.
2. Extract and confirm with the user:
   - Problem statement
   - Affected subsystems (from Impact Analysis table)
   - Implementation tasks (Section 7)
   - Total task count
3. Ask: "Implement all tasks, or a subset?" Proceed only after confirmation.

---

## Phase 1 — Codebase Analysis

For each affected subsystem, read the primary files from `docs/skills/design-feature/references/subsystem-map.md`.

For each implementation task, read every file listed under `Files:`. Note:
- Current state of types, functions, and interfaces the task will modify.
- Existing test patterns in each package (use the same style).
- Import conventions (stdlib / third-party / internal groups).

Summarize blockers or conflicts found (e.g., a type was renamed since the design was written). If any exist, flag them before proceeding.

---

## Phase 1.5 — Pre-Implementation Quiz (MANDATORY)

After reading the codebase but **before** presenting the execution plan, surface ambiguities. Design docs cannot anticipate every integration detail. Present an interactive quiz covering:

1. **Ambiguous design choices**: Where the design says "add X" but there are multiple valid locations or approaches in the code.
2. **Missing details**: Decisions the design doc defers or doesn't address (e.g., UI placement, key bindings, exact data flow).
3. **Conflicting patterns**: Where the codebase has evolved since the design was written and there's more than one way to reconcile.
4. **Scope boundaries**: Confirm what's in vs. out — e.g., "Should this apply to both screens or just one?"

**Format**: Present numbered questions with options (A/B/C) where possible. For open-ended questions, propose a default and ask for confirmation.

**After the quiz**:
- Update the design doc with the resolved answers (add notes inline or update the relevant sections). The design doc is living documentation — keep it accurate.
- Proceed to Phase 2 only after all ambiguities are resolved.

---

## Phase 2 — Execution Plan

Present a numbered execution plan derived from the design doc's tasks. For each task state:

```
Task N: <title>
  Depends on: <task numbers or "none">
  Files: <from design doc>
  Approach: <1-2 sentences: what you'll do, in order>
  Risk: <low/medium — flag anything non-trivial>
```

### Parallelization analysis

After listing all tasks, identify **parallel groups** — sets of tasks with no mutual dependencies that can execute concurrently using agents:

```
Parallel group 1: Tasks 3, 4, 5 (all depend on Tasks 1-2 but not each other)
Sequential: Task 6 (depends on Tasks 3, 4) → Task 7 (depends on all)
```

Note: Parallel execution uses separate agents that cannot see each other's changes. Each agent must be given complete context for its task. Integration verification (Phase 3d) is mandatory after every parallel group.

Ask: "Proceed with this plan?" Adjust if the user requests changes.

---

## Phase 3 — Implement

Execute tasks **in dependency order**. Tasks within the same parallel group MAY be executed concurrently using agents when the user requests it or when there are 3+ independent tasks.

### 3a. Code

- Follow existing patterns exactly. Match naming, error wrapping, comment style.
- Respect the project's code style rules from CLAUDE.md (gofmt, import groups, error prefix, JSON tags).
- Add only what the design specifies. No extras, no refactors, no bonus features.
- If the design doc shows type definitions, use them verbatim unless they conflict with current code.

### 3b. Test

- Write tests for every task that specifies them.
- Match the package's existing test patterns (table-driven, httptest servers, t.TempDir, etc.).
- Run the tests for the changed packages: `go test ./<package>/... -count=1`
- Do NOT run `go test ./...` unless explicitly asked.

### 3c. Validate (per-task)

After each task:
1. Run `go_diagnostics` on all modified files.
2. Fix any errors before moving to the next task.
3. Run the package tests.
4. Briefly report: task title, files changed, tests passing.

If a test fails, fix it before moving on. If a fix requires changing the design, flag it and ask the user.

### 3d. Integration verification (after parallel groups)

After a parallel group completes, **before** starting the next group or task:

1. Run `go build ./...` to verify all parallel changes compile together.
2. Run `go test` for **all packages touched by any agent in the group**.
3. Check for signature mismatches: when one agent changes a function/method signature, other callers (including test helpers) may need updating.
4. Fix any issues. Common problems:
   - **Test helpers not updated**: An agent changed a function signature but a test file in the same package still uses the old signature.
   - **Import conflicts**: Two agents added the same import differently.
   - **Duplicate code**: Two agents solved overlapping concerns differently.

### 3e. Handling scope changes

If the user requests changes to scope during implementation (e.g., adding cases, expanding a type, changing behavior):

1. Assess impact on the current task and remaining tasks.
2. Implement the change in the current task.
3. Update the design doc to reflect the new scope.
4. Re-evaluate whether remaining tasks need adjustment.
5. Do NOT silently absorb scope changes — acknowledge them and note the deviation.

---

## Phase 4 — Integration Check

After all tasks are complete:

1. **Build**: `make build` — must succeed.
2. **Full test suite for changed packages**: `go test <each changed package> -count=1 -race`
3. **Lint** (if available): `make lint`
4. **Vet**: `make vet`

Report results. Fix any issues.

---

## Phase 5 — Summary

Present a completion summary:

```
## Implementation Summary

Design doc: <path>
Tasks completed: N/N

### Changes
| File | Change |
|------|--------|
| <file> | <what changed — one line each> |

### Tests added
- <test file>: <what's covered>

### Design doc updates
- <any changes made to the design doc during implementation>

### Notes
- <anything the user should know: design deviations, scope changes, follow-up items, etc.>
```

---

## Rules

- **Never skip Phase 0.** The user must confirm which tasks to implement.
- **Never skip Phase 1.5.** Surface ambiguities before coding. Even a "no questions" quiz is better than silent assumptions.
- **Never deviate from the design doc** without flagging it and getting approval.
- **Never run `go test ./...`** unless explicitly asked — test only changed packages.
- **Always run integration verification after parallel groups.** Parallel agents can't see each other's changes — their work must be verified together.
- **Keep the design doc updated.** When quiz answers, scope changes, or implementation discoveries change the design, update the doc. It's the source of truth for future reference.
- **If the design doc is stale** (references files/types that don't exist), stop and tell the user. Don't guess.
- **No cleanup commits.** Don't refactor surrounding code, add docstrings, or "improve" things not in the design.
