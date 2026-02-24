Finalize the feature "$ARGUMENTS" — create branch, commit, and open PR.

Read and follow the full skill specification in docs/skills/finalize-feature/SKILL.md.

Follow all phases in order:

1. **Phase 0 — Pre-flight Checks**: Verify build, vet, tests pass. Scan for secrets and debug code. Block if issues found.

2. **Phase 1 — Branch**: Create feature branch with proper naming convention. Confirm with user.

3. **Phase 2 — Commit**: Stage specific files, draft conventional commit message, present for approval, commit.

4. **Phase 3 — Push & PR**: Push to remote, draft PR with summary/changes/test plan from design doc, create via `gh pr create`.

5. **Phase 4 — Final Checklist**: Report branch, commit, PR URL, and next steps.
