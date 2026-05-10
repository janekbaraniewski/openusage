---
title: Adding a provider
description: High-level walk-through of the seven-phase process for contributing a new AI provider.
---

OpenUsage's provider model is small and stable. Adding a new vendor takes three to six hours of focused work depending on how much rich data the vendor exposes. This page is the high-level overview; the in-repo skill at [`docs/skills/add-new-provider.md`](https://github.com/janekbaraniewski/openusage/blob/main/docs/skills/add-new-provider.md) has the step-by-step prompts and validation checks.

## Before you start

Have answers ready for:

1. **Auth model.** API key in env var? OAuth? Local credentials file?
2. **Detection signal.** Env var name(s)? Binary on `$PATH`? Config dir?
3. **What the vendor exposes.** Just rate-limit headers? Per-model usage JSON? Credit balance? Per-day breakdowns?
4. **Currency** if spend is reported (USD, EUR, CNY, etc).

If the answer to #3 is "nothing useful", a header-probe provider is fine — it'll show rate-limit gauges and an auth status badge, which is already valuable.

## The seven phases

The skill breaks the work into seven phases, each with its own validation:

### Phase 1: Provider discovery

Read vendor docs, identify endpoints, capture sample responses. Output: a fact sheet that mirrors the structure of the existing [provider catalog](/providers).

### Phase 2: Package skeleton

Create `internal/providers/<id>/`:

```
<id>/
├── provider.go        # Implements UsageProvider
├── spec.go            # Returns ProviderSpec (auth, setup hints, widgets)
├── widgets.go         # DashboardWidget + DetailWidget definitions
├── fetch.go           # Fetch(ctx, acct) implementation
├── parse.go           # response → UsageSnapshot mapping
└── provider_test.go
```

Register in `internal/providers/registry.go` under `AllProviders()`.

### Phase 3: Detection

Wire detection in `internal/detect/`:

- Env var presence (Style A).
- Binary + dir check (Style B).
- Local service reachability (Style C).

Add a default `AccountConfig` builder that returns the auto-detected account.

### Phase 4: Fetch and parse

Implement `Fetch(ctx, acct)`:

- Build the HTTP request (or read the file, or call the CLI).
- Wrap errors as `fmt.Errorf("<id>: <what>: %w", err)`.
- Parse the response into a `UsageSnapshot`.
- For shared rate-limit header formats, reuse helpers from `internal/parsers/`.

### Phase 5: Widget design

Define `DashboardWidget` (the tile) and `DetailWidget` (the right panel):

- Pick a primary metric for the gauge.
- Group secondary metrics into detail sections.
- For per-model tables, declare columns once; the renderer handles sorting and overflow.

The TUI is data-driven from these definitions — you should not need to touch `internal/tui/`.

### Phase 6: Tests

The conventions:

- Use `httptest.NewServer` to fake the vendor API.
- Table-driven tests for the parser.
- `t.TempDir()` for any local-file fixtures.
- One test per error path (auth, malformed JSON, missing field).

See [development](development.md) for examples.

### Phase 7: Docs

- Add a provider page under `docs/site/docs/providers/<id>.md`.
- Add the page to the sidebar in `docs/site/sidebars.ts`.
- Update `README.md` if the provider count changes.

## Quick reference

| Pattern | Example providers | When to use |
|---|---|---|
| Header-only probe | `openai`, `anthropic`, `groq` | Vendor exposes rate-limit headers but no usage API |
| Rich JSON API | `openrouter`, `xai`, `mistral`, `moonshot`, `zai` | Vendor returns credits, balances, per-model breakdowns |
| Local files only | `claude_code`, `codex`, `gemini_cli` | All data lives in the agent's config dir |
| Local files + API | `cursor`, `ollama` | SQLite or log files plus optional cloud endpoints |
| CLI subprocess | `copilot` | Easiest data path is shelling out to a vendor CLI |

Pick the closest existing provider and copy its shape.

## Common pitfalls

- **Forgetting `json:"-"` on token fields.** Anything you mark as a runtime-only secret needs the tag, or it'll get persisted and leak.
- **Returning errors without provider prefix.** `fmt.Errorf("openai: parsing models: %w", err)` is the convention; bare errors make logs ambiguous.
- **Hard-coding base URLs.** Always read from `acct.BaseURL` first, fall back to a constant default.
- **Computing currency conversion.** Don't. Render in the provider's native currency; let the user reconcile.

## See also

- The full skill: [`docs/skills/add-new-provider.md`](https://github.com/janekbaraniewski/openusage/blob/main/docs/skills/add-new-provider.md)
- [Development conventions](development.md)
- [Providers concept page](../concepts/providers.md)
