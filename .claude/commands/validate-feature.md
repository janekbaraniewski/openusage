Validate the feature "$ARGUMENTS" implementation.

Read and follow the full skill specification in docs/skills/validate-feature/SKILL.md.

Follow all phases:

1. **Phase 0 — Load**: Find design doc, extract tasks, get changed files.
2. **Phase 1 — Build**: `make build`, `make vet`, `make fmt`, `make lint`.
3. **Phase 2 — Tests**: Run tests for changed packages.
4. **Phase 3 — Compliance**: Cross-reference design tasks vs actual changes.
5. **Phase 4 — Quality**: Scan for debug artifacts, unused code, secrets.
6. **Phase 5 — Smoke Test**: Final build and combined tests.
7. **Phase 6 — Report**: Verdict: READY FOR REVIEW or NEEDS ITERATION.
