---
description: Multi-agent feature workflow — explore, lock file plan, implement, verify. Use for features and multi-file fixes that must stay minimal-scope.
---

# Feature cycle

Structured multi-agent workflow with minimal scope and verification gates.

Invoke via `/feature-cycle` (or ask the agent to follow this workflow).

## Steps

### 1. Scope interview

Act as **coordinator**. Ask:

- What is the exact outcome?
- What command proves done? (default: commands in `AGENTS.md`)
- What must NOT change?

If requirements are vague, run `/grill-me` first, then return here.

### 2. Explore (read-only)

Call `invoke_subagent` with agent name **`explorer`**. Prompt must include the full task text.

Wait for the recommended file plan. For non-trivial plans, show it to the user before locking.

### 3. Lock file plan

Write:

```markdown
## Planned files
- path (modify|create — reason)

## Out of scope
- ...

## Done when
- <shell command(s)>
```

Do not proceed until this list is explicit.

### 4. Implement

Call `invoke_subagent` with agent name **`implementer`**. Prompt must include:

- Task description
- Locked file plan (full text)
- Done-when commands

Implementer must not edit files outside the plan and must not claim final PASS.

### 5. Verify

Call `invoke_subagent` with agent name **`verifier`**. Prompt must include:

- Same file plan
- Task / spec text
- Any implementer notes

Verifier runs scope check + CI + Standards/Spec review. All PASS required.

### 6. Loop or complete

- **FAIL** → send verifier table to `implementer`; return to step 4 (max 3 rounds)
- **PASS** → summarize for user: files changed, commands, exit codes

## Do not

- Skip explorer on unfamiliar code
- Skip verifier because tests passed once
- Expand the file plan without rewriting it
- Use tool names other than those in each agent's frontmatter
