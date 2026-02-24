# Skill: Implement Feature

> **Invocation**: When a user asks to implement, code, or build a feature that already has a design doc.

Read and follow the full skill specification in `docs/skills/implement-feature/SKILL.md`.

The user's request is to implement a feature from its design doc.

Follow all phases in order:
1. Phase 0: Load the design doc, extract tasks
2. Phase 1: Analyze the codebase for affected files
3. Phase 1.5: Pre-implementation quiz for ambiguities
4. Phase 2: Present execution plan, wait for approval
5. Phase 3: Execute tasks with tests, parallel where possible
6. Phase 4: Integration check (build, test, lint)
7. Phase 5: Summary of all changes

Do NOT skip loading the design doc. Do NOT code without an approved plan.
