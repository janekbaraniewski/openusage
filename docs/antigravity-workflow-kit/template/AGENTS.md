# Agent instructions

<!-- Customize the Verification section for your stack. Defaults assume Go / OpenUsage. -->

Short project rules for coding agents. Prefer skills under `.agents/skills/` for procedures.

## Verification (must pass before claiming done)

```bash
make test
make vet
```

Provider-only (OpenUsage): `go test ./internal/providers/<name>/... -count=1`

Replace the commands above when copying this template to a non-Go repo.

## Scope

- Minimal diff only — see `.agents/rules/always-on-minimal-scope.md`
- No drive-by refactors or unrelated renames
- **Verifier** agent must PASS before done (implementer never self-approves)

## Workflows

| Intent | Use |
|--------|-----|
| Small fix | `implementer` + clear file plan + done-when command |
| Feature / multi-file | `/feature-cycle` |
| Spec first | `to-spec` skill or `/grill-me` then `/plan` |
| Large refactor | `/teamwork-preview` |

## Bundled skills

Matt Pocock (MIT, adapted): `tdd`, `code-review`, `implement`, `to-spec`, `writing-for-agents` — see `.agents/skills/matt-pocock-LICENSE.md`.

Kit-native: `minimal-scope`, `verify-before-done`, `run-ci`.

## OpenUsage map (delete if unused)

- Providers: `internal/providers/<name>/` → `core.UsageProvider`
- Register: `internal/providers/registry.go`
- Feature skills: `docs/skills/`
- Full playbook: `docs/antigravity-workflow-kit/GUIDE.md`
