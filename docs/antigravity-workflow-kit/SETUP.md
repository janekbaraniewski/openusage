# Setup — Antigravity Workflow Kit

## Quick install (any project)

From your project root:

```bash
# Clone or pull openusage, then copy the template
REPO=/path/to/openusage   # or wherever you pulled this PR
cp -R "$REPO/docs/antigravity-workflow-kit/template/." .

# Or copy only .agents + AGENTS.md
cp "$REPO/docs/antigravity-workflow-kit/template/AGENTS.md" .
cp -R "$REPO/docs/antigravity-workflow-kit/template/.agents" .
```

Edit `AGENTS.md` — set your real test/lint commands (defaults assume Go/`make test`).

## Antigravity discovery

Antigravity loads automatically from:

| Path | Scope |
|------|-------|
| `.agents/agents/` | Custom agents |
| `.agents/skills/` | Skills |
| `.agents/rules/` | Rules |
| `.agents/workflows/` | `/workflow-name` slash commands |
| `AGENTS.md` (repo root) | Cross-runtime instructions |

Global agents: `~/.gemini/config/agents/<name>/agent.md`

Verify agents appear: run `agy`, type `/agents`.

## Bundled skills

Matt Pocock skills (`tdd`, `code-review`, `implement`, `to-spec`, `writing-for-agents`) are included in the template. See `.agents/skills/matt-pocock-LICENSE.md`.

## Optional plugins

```bash
# Lifecycle: /spec, /planning, /build, /test, /review
agy plugin install https://github.com/addyosmani/agent-skills.git

# Workflow aliases: asw-plan, asw, asw-review
npx antigravity-swarm install --hud
```

## Customize for your stack

1. **`AGENTS.md`** — Replace verification commands with yours (`npm test`, `cargo test`, etc.)
2. **`.agents/skills/run-ci/SKILL.md`** — Same command updates
3. **`.agents/rules/`** — Add glob rules, e.g. `*.tsx` → frontend conventions
4. **`implementer` agent** — Adjust `commandExecutionPolicy` if approvals are too noisy

## Recommended first run

1. Select **coordinator** as main agent
2. Run `/feature-cycle Add <small feature>`
3. Confirm verifier rejects any file outside the plan
4. Run `/learn` after correcting the agent once — persists the fix as a rule

## OpenUsage-specific note

This repo ships the kit under `docs/antigravity-workflow-kit/` as documentation and a copy-paste template. It is not loaded by the OpenUsage Go binary. Copy `template/` into projects where you use Antigravity.
