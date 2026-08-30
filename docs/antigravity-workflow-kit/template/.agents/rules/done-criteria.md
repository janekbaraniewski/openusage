# Done criteria

Before claiming a task complete:

1. Run `make test` and `make vet` (or project-specific commands in AGENTS.md) — both exit 0.
2. Run `git diff --name-only` and confirm every path was on the approved file plan.
3. Spawn **verifier** subagent (or switch to verifier agent) — all checklist rows PASS.
4. Report: task summary, files changed, test output (pass/fail only, no secrets).

Never mark complete on self-assessment alone.
