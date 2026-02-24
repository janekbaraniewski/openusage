Review the design doc for "$ARGUMENTS" against the current codebase.

Read and follow the full skill specification in docs/skills/review-design/SKILL.md.

Follow all phases in order:

1. **Phase 0 — Load Design**: Find the design doc for "$ARGUMENTS" in `docs/`. Read it fully. Extract the problem statement, affected subsystems, type definitions, and implementation tasks. Confirm with me which doc to review.

2. **Phase 1 — Codebase Audit**: Read the subsystem map in docs/skills/design-feature/references/subsystem-map.md. Read every file referenced in the design doc's implementation tasks. Use the checklist in docs/skills/review-design/references/review-checklist.md to find discrepancies.

3. **Phase 2 — Quiz Loop**: Present each discrepancy as a question with severity and resolution options. Wait for my answer. Apply the resolution (edit the design doc or note as intentional). Re-scan after edits. **Repeat until no issues remain.**

4. **Phase 3 — Final Verification**: Re-read the design doc, verify all tasks reference valid files/types, confirm dependency order, and report the summary.

Do not auto-fix anything — always ask me first.
