---
name: verify-before-done
description: Mandatory post-implementation verification. Spawns or runs read-only audit against planned files, CI commands, and rules. Use before marking any coding task complete.
---

# Verify before done

Builder ≠ verifier. The implementer must not sign off its own work.

## Process

1. Collect **planned file list** from coordinator/explorer/plan artifact.
2. Run scope commands:

```bash
git diff --name-only
git diff --cached --name-only
```

3. Run CI from AGENTS.md (default: `make test`, `make vet`).
4. Read each changed file — do not trust implementer summary.
5. Fill the checklist table (all PASS required).

## Checklist table (required output)

| # | Check | Status | Evidence |
|---|-------|--------|----------|
| 1 | All diff paths in file plan | PASS/FAIL | list extra or missing paths |
| 2 | Tests pass | PASS/FAIL | exit code |
| 3 | Vet/lint pass | PASS/FAIL | exit code |
| 4 | No unrelated logic changes | PASS/FAIL | file:line |
| 5 | Matches task spec | PASS/FAIL | quote task vs diff |

## On FAIL

Return findings to implementer. Do not mark complete. Coordinator loops until all PASS or user intervenes.

## Tools

Verifier agent should use read-only tools only (`view_file`, `grep_search`, `run_command` for git/test).
