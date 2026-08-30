---
name: run-ci
description: Runs project verification commands after code changes. Use after implementation and during verify-before-done checks.
---

# Run CI (OpenUsage / Go defaults)

Customize commands in AGENTS.md for other projects.

## Standard verification

```bash
make test
make vet
```

## Scoped provider change

```bash
go test ./internal/providers/<provider>/... -count=1 -race
go vet ./internal/providers/<provider>/...
```

## Single package

```bash
go test ./internal/<package>/... -count=1
```

## Completion criterion

All invoked commands exit 0. Capture exit codes in verifier evidence table.

## Do not

- Skip tests because "change is small"
- Claim pass without running commands in this session
