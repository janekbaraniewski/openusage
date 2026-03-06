Add a new AI provider "$ARGUMENTS" to the OpenUsage TUI dashboard.

Read and follow the full skill specification in docs/skills/add-new-provider.md.

Follow all phases:

1. **Phase 0 — Quiz**: Ask all 10 provider questions.
2. **Phase 1 — Research**: Study provider API docs.
3. **Phase 2 — Create Package**: Implement provider in `internal/providers/<id>/`.
4. **Phase 3 — Dashboard Widget**: Create tile with gauges and compact rows.
5. **Phase 4 — Register**: Add to registry.go, detect.go, example_settings.json.
6. **Phase 5 — Tests**: Minimum 3 tests using httptest.NewServer.
7. **Phase 6 — Verify**: `go build`, `go test`, `make vet`.
