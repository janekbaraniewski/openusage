Validate the feature "$ARGUMENTS" implementation.

Read and follow the full skill specification in docs/skills/validate-feature/SKILL.md.

Follow all phases in order:

1. **Phase 0 — Load Context**: Find the design doc for "$ARGUMENTS" in `docs/`. Extract tasks, files, and test requirements. Get list of changed files.

2. **Phase 1 — Build Verification**: Run make build, vet, fmt, lint. All must pass.

3. **Phase 2 — Test Verification**: Run tests for all changed packages with -race. Check coverage. Flag missing tests.

4. **Phase 3 — Design Compliance**: Cross-reference design tasks against actual changes. Build compliance matrix.

5. **Phase 4 — Code Quality Scan**: Check for debug artifacts, unused code, error handling, import hygiene, secrets.

6. **Phase 5 — Integration Smoke Test**: Final build, demo run, combined test run.

7. **Phase 6 — Validation Report**: Produce summary with verdict: READY FOR REVIEW or NEEDS ITERATION.
