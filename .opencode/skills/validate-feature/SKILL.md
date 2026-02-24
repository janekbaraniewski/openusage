# Skill: Validate Feature

> **Invocation**: When a user asks to validate, verify, or check a feature implementation before PR.

Read and follow the full skill specification in `docs/skills/validate-feature/SKILL.md`.

The user's request is to validate a feature implementation.

Follow all phases in order:
1. Phase 0: Load context — design doc, changed files
2. Phase 1: Build verification (build, vet, fmt, lint)
3. Phase 2: Test verification (tests with -race, coverage)
4. Phase 3: Design compliance matrix
5. Phase 4: Code quality scan
6. Phase 5: Integration smoke test
7. Phase 6: Validation report with verdict
