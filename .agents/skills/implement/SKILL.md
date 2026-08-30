---
name: implement
description: "Implement work from a spec, tickets, or locked file plan. Use when coding a planned change. Prefer tdd at agreed seams; hand off to verifier — do not self-approve or auto-commit."
---

# Implement

Vendored from [mattpocock/skills](https://github.com/mattpocock/skills) (MIT), adapted for Antigravity.

## Antigravity adaptation

- Do **not** require `/setup-matt-pocock-skills`.
- Prefer the locked **file plan** + task/spec from the coordinator.
- Use the `tdd` skill where possible, at pre-agreed seams.
- Run project checks from `AGENTS.md` / `run-ci` regularly; full suite once at the end of your slice.
- When code is ready, hand off to **`verifier`** (or ask the user to run verifier). Do **not** treat `/code-review` as a substitute for verifier scope+CI gates.
- **Do not commit** unless the user explicitly asked you to commit.

## Steps

1. Confirm file plan exists (paths + modify|create).
2. Implement only planned paths; match local style.
3. Prefer red → green via `tdd` for new behaviour.
4. Run verification commands; fix failures you caused.
5. Report: paths touched, commands run, exit codes — ready for verifier.
