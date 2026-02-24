# Skill: Finalize Feature

> **Invocation**: When a user asks to finalize, ship, commit, or create a PR for a feature.

Read and follow the full skill specification in `docs/skills/finalize-feature/SKILL.md`.

The user's request is to finalize a feature.

Follow all phases in order:
1. Phase 0: Pre-flight checks (build, vet, tests, scan for secrets)
2. Phase 1: Create feature branch
3. Phase 2: Stage and commit with conventional message
4. Phase 3: Push and create PR via gh
5. Phase 4: Final checklist with branch, commit, PR URL
