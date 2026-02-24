Audit and improve the development workflow for OpenUsage.

Read and follow the full skill specification in docs/skills/dev-workflow-improvements/SKILL.md.

This skill ensures the development flow is complete, consistent, and propagated to all AI tools.

Follow all phases:

1. **Phase 0 — Audit**: Run `make sync-tools`, check for drift. Validate all skills are registered in skills-table.md, have Claude commands, OpenCode stubs. Check for broken references.

2. **Phase 1 — Fix**: Fix any issues found: sync drift, missing registrations, broken references, CLAUDE.md mismatches.

3. **Phase 2 — Improve**: If improvements requested, quiz the user about what needs changing. Add/update skills, onboard new tools, fix workflow gaps. Run sync after each change.

4. **Phase 3 — Verify**: Run sync (should be clean), build, test, show git diff for review.
