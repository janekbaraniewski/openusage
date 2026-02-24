# Skill: Validate Feature

Validate that a feature implementation is complete, correct, and ready for review.

## When to use

After `/implement-feature` completes, before `/finalize-feature`. Also useful standalone to check feature health after manual changes.

## Prerequisite

- A design doc in `docs/*_DESIGN.md`
- Implementation completed (code exists in working tree)

## Phases

### Phase 0 — Load Context

1. Find the design doc for the feature (search `docs/*_DESIGN.md` if path not given).
2. Read the full design doc. Extract:
   - Implementation tasks (Section 7)
   - Files listed per task
   - Test requirements per task
3. Run `git diff main --name-only` to get list of all changed files.
4. If an implementation summary exists in conversation context, use it. Otherwise, infer from changed files.

### Phase 1 — Build Verification

Run these checks. ALL must pass before proceeding:

1. `make build` — binary compiles cleanly
2. `make vet` — no vet warnings in changed packages
3. `make fmt` — no formatting issues (check `gofmt -l` output)
4. `make lint` — no lint errors (skip gracefully if golangci-lint not installed)

Report:
```
## Build Verification
- [x] make build: PASS
- [x] make vet: PASS
- [x] make fmt: PASS
- [ ] make lint: SKIP (not installed)
```

If any check fails, report the error and stop. Do not proceed to Phase 2 with build failures.

### Phase 2 — Test Verification

1. Identify all Go packages with changed files.
2. Run `go test ./<pkg>/... -count=1 -race` for each changed package.
3. Check for new test files — every task that specifies "Tests:" in the design doc MUST have corresponding test functions.
4. Run `go test ./<pkg>/... -count=1 -cover` and note coverage for changed packages.

Report:
```
## Test Verification
| Package | Tests | Coverage | Status |
|---------|-------|----------|--------|
| internal/core | 5 pass | 82% | PASS |
| internal/config | 8 pass | 71% | PASS |

Missing tests: none
```

Flag any design tasks that specify tests but have none implemented.

### Phase 3 — Design Compliance

Cross-reference the design doc tasks against actual changes:

1. For each implementation task in the design doc:
   - Check that ALL files listed under "Files:" were actually modified (or created if new)
   - Check that the described functionality exists in the code
   - Check that tests specified under "Tests:" exist and pass
2. Build a compliance matrix:

```
## Design Compliance
| Task | Files | Code | Tests | Status |
|------|-------|------|-------|--------|
| Task 1: Add TimeWindow type | ✓ | ✓ | ✓ | COMPLETE |
| Task 2: Wire into config | ✓ | ✓ | ✓ | COMPLETE |
| Task 3: TUI integration | ✓ | ✓ | ✗ | MISSING TESTS |
```

3. Flag any:
   - Tasks with no code changes (skipped?)
   - Files changed that aren't in any task (scope creep?)
   - Design doc sections marked as "intentional change" or "deferred"

### Phase 4 — Code Quality Scan

Scan changed files for common issues:

1. **Debug artifacts**: Search for `fmt.Println`, `log.Println`, `FIXME`, `HACK`, `XXX`, `TODO` (flag but don't auto-remove TODOs — they may be intentional)
2. **Unused code**: Run `go vet` with unused checks. Look for commented-out code blocks.
3. **Error handling**: Grep changed files for unchecked errors (bare `err` assignments without `if err != nil`).
4. **Import hygiene**: Check import grouping follows convention (stdlib, third-party, internal separated by blank lines).
5. **Secrets/sensitive data**: Scan for hardcoded tokens, API keys, passwords. Check no `.env` files or credentials are staged.

Report:
```
## Code Quality
- Debug artifacts: none found
- Unused code: none found
- Error handling: all errors checked
- Import hygiene: consistent
- Secrets scan: clean
```

### Phase 5 — Integration Smoke Test

1. Run `make build` one final time to confirm clean binary.
2. If `make demo` exists and the feature affects TUI rendering, run it and note if it starts without panics (exit after 2 seconds with timeout).
3. Check that all changed packages' tests pass together: `go test <all changed packages> -count=1 -race`
4. If the feature added new config fields, verify `configs/example_settings.json` includes them.

### Phase 6 — Validation Report

Produce a final summary:

```
## Validation Report

Feature: <name>
Design doc: <path>
Date: <date>

### Results
| Check | Status |
|-------|--------|
| Build | PASS |
| Vet/Lint | PASS |
| Tests (N packages) | PASS |
| Coverage | avg X% |
| Design compliance (N/N tasks) | PASS |
| Code quality | PASS |
| Integration smoke test | PASS |

### Issues Found
- <issue description, severity, file:line>

### Verdict
READY FOR REVIEW / NEEDS ITERATION
```

If verdict is "NEEDS ITERATION", recommend running `/iterate-feature` with the issues list.

## Rules

1. Never auto-fix issues found during validation — report them. Fixing is for `/iterate-feature`.
2. Never skip Phase 1 — build must pass before anything else.
3. Never run `go test ./...` — only test changed packages unless explicitly asked.
4. Always cross-reference against the design doc — implementation without design compliance is incomplete.
5. If no design doc exists, skip Phase 3 but run all other phases.
6. Report findings factually — no opinions on code style unless it violates CLAUDE.md conventions.

## Checklist

Before marking validation complete:
- [ ] Build compiles cleanly
- [ ] All changed packages pass tests with -race
- [ ] Every design task has corresponding code changes
- [ ] Every design task with "Tests:" has test functions
- [ ] No debug artifacts in changed files
- [ ] No secrets or credentials in changed files
- [ ] Example config updated if new config fields added
- [ ] Validation report produced with clear verdict
