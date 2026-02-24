Develop the feature "$ARGUMENTS" end-to-end — from design to pull request.

Read and follow the full skill specification in docs/skills/develop-feature/SKILL.md.

This skill orchestrates the full development lifecycle:

1. **Phase 0 — Intake**: Check for existing design doc. Ask: full lifecycle or specific phase?

2. **Phase 1 — Design** (`/design-feature`): Design the feature, produce design doc with tasks.

3. **Phase 2 — Review** (`/review-design`): Validate design against codebase, fix discrepancies.

4. **Phase 3 — Implement** (`/implement-feature`): Execute tasks with tests, parallel where possible.

5. **Phase 4 — Validate** (`/validate-feature`): Build, test, design compliance, code quality checks.

6. **Phase 5 — Iterate** (`/iterate-feature`): Fix issues from validation (loops until clean or user decides).

7. **Phase 6 — Finalize** (`/finalize-feature`): Create branch, commit, open PR.

8. **Phase 7 — Summary**: Report full lifecycle results.

Each phase pauses for user confirmation before proceeding to the next.
