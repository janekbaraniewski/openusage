# Implementer handoff

After your changes and local CI:

1. Do **not** claim the task complete.
2. Do **not** spawn yourself as verifier.
3. Report: planned files vs touched files, commands run, exit codes.
4. Wait for **verifier** (coordinator invokes it, or user selects the verifier agent).

Only the verifier may emit a final PASS/FAIL verdict.
