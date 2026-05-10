---
title: Development conventions
description: Code style, branch & PR rules, and testing patterns used across OpenUsage.
---

The codebase is small enough that a few simple conventions go a long way. Follow these and review will be quick.

## Code style

### Formatting

- `gofmt` + `goimports`. Run `make fmt` before committing.
- **Tabs for indentation.** No spaces.
- **Import groups** separated by blank lines, in this order:
  1. stdlib
  2. third-party
  3. internal (`github.com/janekbaraniewski/openusage/...`)

### Naming and aliases

- Bubble Tea is aliased as `tea`:
  ```go
  import tea "github.com/charmbracelet/bubbletea"
  ```
- Provider package names match the provider ID (`openai`, `claude_code`, `gemini_cli`).
- Test files end in `_test.go` and live next to the code under test.

### Errors

Wrap errors with the provider (or subsystem) prefix and the action being attempted:

```go
return fmt.Errorf("openai: creating request: %w", err)
return fmt.Errorf("daemon: opening socket %q: %w", path, err)
```

Bare returns (`return err`) are acceptable inside small leaf functions, but anywhere a user might see the message in a log, prefix it.

### Optional fields

Use pointer fields for optional numerics so absence is distinguishable from zero:

```go
type RateLimit struct {
    Limit     *float64 `json:"limit,omitempty"`
    Remaining *float64 `json:"remaining,omitempty"`
}
```

For optional strings, omit-empty + empty-string is fine.

### JSON tags

- `snake_case` keys.
- `omitempty` on optional fields.
- `json:"-"` on any runtime-only secret (`AccountConfig.Token` is the canonical example).

### Comments

- Public types, functions, and methods get a doc comment that starts with the name.
- Keep comments load-bearing — explain *why*, not *what*.

## Branch and PR conventions

- Branch off `main`. Use any sensible branch name; we don't enforce a prefix scheme.
- Conventional commit subjects are appreciated (`feat(provider/openai): ...`, `fix(daemon): ...`) but not required.
- Squash on merge by default; the maintainer picks per PR.
- PR description should call out:
  - which provider or subsystem is touched
  - any user-visible changes (config keys, keybindings, behavior)
  - whether docs and tests were updated

Include screenshots for TUI changes — `make demo` is the easiest way to capture them.

## Testing patterns

### Standard library only

No mocking frameworks. The standard `testing` package plus `httptest` is sufficient for everything OpenUsage tests.

### HTTP-backed providers

Use `httptest.NewServer` and pass its URL via `BaseURL`:

```go
func TestFetch(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("x-ratelimit-limit-requests", "5000")
        w.Header().Set("x-ratelimit-remaining-requests", "4999")
        fmt.Fprintln(w, `{"id":"gpt-4.1-mini"}`)
    }))
    defer srv.Close()

    p := New()
    snap, err := p.Fetch(ctx, AccountConfig{
        ID:       "openai-test",
        Provider: "openai",
        BaseURL:  srv.URL,
        Token:    "sk-test",
    })
    // assertions ...
}
```

### Table-driven tests

Type logic and parsers are typically table-driven:

```go
cases := []struct{
    name string
    in   string
    want float64
}{
    {"plain number", "5000", 5000},
    {"with reset", "5000;w=60", 5000},
    {"empty",       "",     0},
}
for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
        got := parseLimit(c.in)
        if got != c.want { /* ... */ }
    })
}
```

### File-backed providers

Use `t.TempDir()` for fixtures so cleanup is automatic:

```go
dir := t.TempDir()
must(os.WriteFile(filepath.Join(dir, "stats-cache.json"), fixture, 0644))
p := New()
snap, _ := p.Fetch(ctx, AccountConfig{ /* point at dir */ })
```

### Telemetry tests

Use in-memory SQLite (`:memory:`) for store tests so they don't pollute a temp dir.

### Race detection

`make test` runs with `-race` and a coverage profile. New code should not introduce data races.

## Things to avoid

- New runtime dependencies. The dependency tree is intentionally small; talk before adding one.
- Reaching into `internal/tui/` from a provider package — providers describe their UI declaratively via widgets.
- Persisting secrets. If you find yourself adding a string field to `AccountConfig`, ask whether it should be `json:"-"`.
- Cross-compilation tricks. CGO is required; document that fact rather than working around it.

## See also

- [Contributing overview](overview.md)
- [Add a provider](add-provider.md)
- The in-repo skills under [`docs/skills/`](https://github.com/janekbaraniewski/openusage/tree/main/docs/skills) for fully-spec'd flows (`/develop-feature`, `/design-feature`, etc).
