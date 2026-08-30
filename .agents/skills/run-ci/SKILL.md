---
name: run-ci
description: Runs project verification commands after code changes. Use after implementation and during verifier checks. Reads commands from AGENTS.md.
---

# Run CI

## Steps

1. Read root `AGENTS.md` → **Verification** section for the exact commands.
2. If `AGENTS.md` is missing, ask the user for the test/lint commands. Do not invent a stack.
3. Run those commands with `run_command`.
4. Record each command and exit code.

## OpenUsage / Go defaults (only if AGENTS.md says so)

```bash
make test
make vet
```

Scoped provider:

```bash
go test ./internal/providers/<provider>/... -count=1 -race
go vet ./internal/providers/<provider>/...
```

## Completion criterion

All invoked commands exit 0. Verifier pastes exit codes into the evidence table.

## Do not

- Skip because "the change is small"
- Claim pass without running commands in this session
- Run destructive git commands (`push --force`, `reset --hard`) as part of CI
