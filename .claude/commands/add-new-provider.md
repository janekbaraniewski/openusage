Add a new AI provider "$ARGUMENTS" to the OpenUsage TUI dashboard.

Read and follow the full skill specification in docs/skills/add-new-provider.md.

Follow all phases in order:

1. **Phase 0 — Quiz**: Ask me all 10 questions from the skill doc before writing any code. If I provided the provider name as "$ARGUMENTS", use that as the starting point but still confirm the details. Research the provider's API docs yourself if I don't know an answer.

2. **Phase 1 — Research**: Study the provider's API documentation. Summarize findings before coding.

3. **Phase 2 — Create provider package**: Implement the provider in `internal/providers/<id>/` following the patterns and conventions documented in the skill.

4. **Phase 3 — Dashboard widget**: Configure a beautiful dashboard tile with appropriate gauges, compact rows, and color role.

5. **Phase 4 — Register**: Add to `registry.go`, `detect.go`, and `example_settings.json`.

6. **Phase 5 — Tests**: Write at least 3 tests (success, auth-required, rate-limited) using `httptest.NewServer`.

7. **Phase 6 — Verify**: Run `go build`, `go test`, and `make vet`.

Complete the full checklist at the end of the skill doc before finishing.
