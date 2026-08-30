# Feature cycle

Structured multi-agent workflow with minimal scope and verification gates.

## When to use

Features or fixes that touch multiple files and need discipline against scope creep.

## Steps

### 1. Scope interview

Act as **coordinator**. Ask:

- What is the exact outcome?
- What command proves done? (e.g. `make test`)
- What must NOT change?

If requirements are vague, suggest `/grill-me` first.

### 2. Explore (read-only)

Invoke subagent **explorer** with the task description.

Wait for recommended file plan. Present to user for approval if non-trivial.

### 3. Lock file plan

Write the approved plan:

```
PLANNED FILES:
- ...
OUT OF SCOPE:
- ...
DONE WHEN:
- make test exits 0
```

User may edit this list before implementation proceeds.

### 4. Implement

Invoke subagent **implementer** with:

- Task description
- Locked file plan (mandatory)
- Done-when commands

Implementer must not edit files outside the plan.

### 5. Verify

Invoke subagent **verifier** with:

- File plan
- Task description

Verifier runs scope check + CI + two-axis review. All PASS required.

### 6. Loop or complete

- **FAIL** → send verifier table to implementer; return to step 4 (max 3 rounds)
- **PASS** → summarize for user: files changed, tests run

## Optional

- Install Matt Pocock `tdd` skill (bundled) for implementer red-green loops
- Install addyosmani/agent-skills for `/spec` before step 1

## Do not

- Skip explorer on unfamiliar code
- Skip verifier because tests passed once
- Expand file plan without writing it down
