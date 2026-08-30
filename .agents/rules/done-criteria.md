# Done criteria

Applies to **coordinator** and **verifier** (final sign-off).

Before claiming a task complete:

1. Verifier has run scope check: `git diff --name-only` (+ cached) ⊆ planned files.
2. Project verification commands from `AGENTS.md` exited 0.
3. Verifier checklist all PASS (Standards + Spec).
4. Report: summary, files changed, test status (no secrets).

Never mark complete on implementer self-assessment alone.
