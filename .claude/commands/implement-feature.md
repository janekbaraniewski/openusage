Implement the feature "$ARGUMENTS" from its design doc.

Read and follow the full skill specification in docs/skills/implement-feature/SKILL.md.

Follow all phases in order:

1. **Phase 0 — Load Design**: Find the design doc for "$ARGUMENTS" in `docs/`. Extract the problem statement, affected subsystems, and implementation tasks. Confirm with me which tasks to implement.

2. **Phase 1 — Codebase Analysis**: Read the subsystem map in docs/skills/design-feature/references/subsystem-map.md. Read every file listed in the implementation tasks. Note current state and any conflicts with the design.

3. **Phase 1.5 — Pre-Implementation Quiz**: Surface ambiguities between the design doc and the actual codebase. Present questions about unclear integration points, multiple valid approaches, or missing details. Update the design doc with my answers.

4. **Phase 2 — Execution Plan**: Present the ordered task plan with approaches, risks, and parallelization analysis (which tasks can run concurrently). Wait for my approval before coding.

5. **Phase 3 — Implement**: Execute tasks in dependency order. Use parallel agents for independent task groups when there are 3+ tasks that don't depend on each other. For each task: write code, write tests, run diagnostics, run package tests. After each parallel group, run integration verification. Fix issues before moving on. Flag any design deviations.

6. **Phase 4 — Integration Check**: After all tasks, run `make build`, test changed packages with `-race`, and lint/vet.

7. **Phase 5 — Summary**: Report all changes, tests added, design doc updates, and any notes.

Use the per-task checklist in docs/skills/implement-feature/references/execution-checklist.md for every task.
