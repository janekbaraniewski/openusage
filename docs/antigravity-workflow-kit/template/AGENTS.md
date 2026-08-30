# OpenUsage — Agent Instructions

Go terminal dashboard for AI coding tool usage. CGO required (`CGO_ENABLED=1`).

## Verification (must pass before claiming done)

```bash
make test    # go test -race ./...
make vet     # go vet ./...
```

For provider-only changes: `go test ./internal/providers/<name>/... -count=1`

## Scope

- **Minimal diff only.** Edit files required for the task. See `.agents/rules/always-on-minimal-scope.md`.
- **No drive-by refactors**, formatting sweeps, or unrelated renames.
- Spawn **verifier** subagent before reporting complete.

## Architecture pointers

- Providers: `internal/providers/<name>/` implement `core.UsageProvider`
- Register in `internal/providers/registry.go`
- Config: `~/.config/openusage/settings.json`
- Skills for features: `docs/skills/`

## Workflows

| Intent | Use |
|--------|-----|
| Small fix | `implementer` agent + `/goal` with test command |
| Feature | `/feature-cycle` workflow |
| Design first | `/grill-me` or `/plan` |
| Large refactor | `/teamwork-preview` |

## Bundled Matt Pocock skills (vendored, MIT)

Included in `.agents/skills/`: `tdd`, `code-review`, `implement`, `to-spec`, `writing-for-agents`. See `matt-pocock-LICENSE.md`.

Optional: [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) plugin for `/spec`, `/review` lifecycle.

Full playbook: `docs/antigravity-workflow-kit/GUIDE.md`
