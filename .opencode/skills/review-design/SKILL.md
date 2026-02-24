# Skill: Review Design

> **Invocation**: When a user asks to review, validate, or check a design doc against the codebase.

Read and follow the full skill specification in `docs/skills/review-design/SKILL.md`.

The user's request is to review a design doc.

Follow all phases in order:
1. Phase 0: Load the design doc, extract references
2. Phase 1: Audit codebase against design using the review checklist
3. Phase 2: Quiz loop — present discrepancies, get user decisions, repeat until clean
4. Phase 3: Final verification of the updated design doc

Do NOT auto-fix discrepancies. Always ask the user first.
