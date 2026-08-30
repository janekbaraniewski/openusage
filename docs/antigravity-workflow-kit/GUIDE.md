# Antigravity Multi-Agent Workflow Kit

A practical playbook for running Antigravity custom agents with **minimal file touch**, **verification gates**, and **quality discipline** — without Antigravity Manager or quota-proxy tooling.

Copy the template into any repo: see [SETUP.md](./SETUP.md).

---

## Problem this solves

Default Antigravity harness behavior tends toward:

- Generalist agents with bloated context (every skill loaded, vague rules ignored)
- Sloppy code that "looks done" but fails review or tests
- **Drive-by refactors** — touching files unrelated to the task
- Single agent self-auditing its own work

This kit addresses those with **role separation**, **checkable rules**, and **Matt Pocock–style review discipline**.

---

## Architecture (three layers)

| Layer | Purpose | Antigravity tools |
|-------|---------|-------------------|
| **Scope** | Define *what* and *how we know it's done* | `/grill-me`, `/plan`, Teamwork Phase 1 |
| **Execute** | Build in isolated, file-bounded contexts | Custom agents, `/goal`, subagents |
| **Verify** | Catch sloppy code before you accept it | Verifier agent (read-only), CI exit codes |

**Do not skip layer 1 or 3.**

---

## Custom agents (the core)

Antigravity Custom Agents are markdown files under `.agents/agents/` with YAML frontmatter. Same file can be **main agent** (talk to directly) or **subagent** (delegated).

This template ships four roles:

| Agent | Role | Edits files? |
|-------|------|--------------|
| `coordinator` | Plans, lists target files, delegates | No |
| `explorer` | Read-only research before edits | No |
| `implementer` | Makes the minimal change set | Yes |
| `verifier` | Read-only audit + scope check | No |

**Execution symmetry** (Antigravity 2.0):

```yaml
mainAgent: true
subagent: true
```

Run directly: `agy --agent implementer` or select from the agent dropdown.

Docs: [Introducing Custom Agents](https://antigravity.google/blog/introducing-custom-agents) · [Subagents spec](https://antigravity.google/docs/subagents/)

---

## Minimal scope (only change files you need)

The **`minimal-scope`** skill and **`always-on-minimal-scope`** rule enforce:

1. **Plan first** — coordinator or implementer lists every file to touch *before* editing
2. **Explorer pass** — read call chain; confirm smallest surface area
3. **Implement** — edit only listed files; new files need explicit justification
4. **Verifier gate** — `git diff --name-only` must match the plan; any extra file = FAIL

Red flags the verifier rejects:

- Formatting-only churn in unrelated files
- Renames/refactors outside the stated task
- New dependencies or config changes not in the plan
- "While I was here" edits

Pair with `/plan` or `/grill-me` for non-trivial work so the file list is agreed before code.

---

## Slash commands — when to use what

| Task | Command |
|------|---------|
| Small fix, machine-verifiable done criteria | `/goal Fix X. Done when make test exits 0.` |
| Needs design alignment | `/grill-me` → `/plan` → review artifact → Proceed |
| Multi-file / multi-day | `/teamwork-preview` (paid; built-in Critic/Auditor) |
| Repeatable pipeline | Custom workflow `/feature-cycle` (in template) |
| Distill corrections into rules | `/learn` |

**`/goal` is only good when "done" = a shell command exiting 0**, plus verifier pass.

---

## Antigravity pitfalls this kit avoids

| Pitfall | Fix in template |
|---------|-----------------|
| Wrong `tools:` names hang subagents | Only blog-documented names; see `.agents/TOOLS.md` |
| Matt Pocock skills demand `/setup-matt-pocock-skills` | Adapted `code-review`, `implement`, `to-spec` |
| `implement` auto-commits / self-reviews | Hand off to `verifier`; no commit unless asked |
| `code-review` needs branch fixed-point | Defaults to working-tree `git diff` |
| Implementer claims done | `implementer-handoff` rule + verifier PASS required |
| `list_dir` / SDK-only tool ids | Removed from frontmatter |

## Matt Pocock skills (bundled)

The template **vendors** these skills from [mattpocock/skills](https://github.com/mattpocock/skills) (MIT) under `.agents/skills/`:

| Skill | Role |
|-------|------|
| `tdd` | Red-green-refactor; vertical slices; anti horizontal slicing |
| `code-review` | Two-axis review: **Standards** vs **Spec** (parallel subagents) |
| `implement` | Spec-driven implementation ending in code review |
| `to-spec` | Write spec before code |
| `writing-for-agents` | Keep AGENTS.md/skills minimal and followable |

Attribution: `template/.agents/skills/matt-pocock-LICENSE.md`

To refresh from upstream later:

```bash
# Re-copy from a pinned mattpocock/skills checkout into template/.agents/skills/
```

Optional upstream updates via skills.sh (may duplicate if you already have vendored copies):

```bash
npx skills add mattpocock/skills --skill tdd,code-review,implement
```

**AGENTS.md discipline** (Matt / progressive disclosure):

- Root `AGENTS.md` under ~150 lines: commands, boundaries, invariants only
- Move workflows to `.agents/skills/`
- Nested `AGENTS.md` in packages for monorepos
- Checkable rules ("`make test` must exit 0") not vibes ("write clean code")

---

## Recommended GitHub projects

### Quality & orchestration (install these)

| Stars | Repo | Use for |
|------:|------|---------|
| ~1.3k | [first-fluke/oh-my-agent](https://github.com/first-fluke/oh-my-agent) | Stop hooks until test/lint pass; independent judge; artifact gates |
| ~39k | [wshobson/agents](https://github.com/wshobson/agents) | 202 agents, 181 skills; `make generate HARNESS=antigravity` |
| ~91k | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | `/spec` → `/build` → `/test` → `/review`; reviewer subagents. `agy plugin install https://github.com/addyosmani/agent-skills.git` |
| ~166 | [wjgoarxiv/antigravity-swarm](https://github.com/wjgoarxiv/antigravity-swarm) | `asw-plan`, `asw`, `asw-review` aliases + subagent presets |

**Suggested stack:** this template + `addyosmani/agent-skills` plugin + cherry-pick Matt Pocock `tdd` + `code-review`.

### Skill catalogs (cherry-pick only — never `--all`)

| Stars | Repo | Note |
|------:|------|------|
| ~46k | [sickn33/agentic-awesome-skills](https://github.com/sickn33/agentic-awesome-skills) | `npx agentic-awesome-skills --antigravity --skills id1,id2 --dry-run` |
| ~5k | [tech-leads-club/agent-skills](https://github.com/tech-leads-club/agent-skills) | Validated, smaller catalog |
| ~7k | [trailofbits/skills](https://github.com/trailofbits/skills) | Security audits for verifier role |

### Multi-account (optional — you said you don't need Manager)

| Stars | Repo | Use for |
|------:|------|---------|
| ~31k | [lbjlaq/Antigravity-Manager](https://github.com/lbjlaq/Antigravity-Manager) | Dashboard + API proxy + auto-rotation (skip if using `agy-box` only) |
| Native | `agy-box add <name>` | Isolated account containers |

### Skip

- Full sickn33 catalog install (context exhaustion)
- LangGraph / CrewAI / AutoGen for Antigravity-native work (different harness)

---

## Multi-account pattern (without Manager)

```
Account/box A → implementer on backend (exclusive dir)
Account/box B → implementer on frontend (exclusive dir)
Account/box C → verifier after A/B finish
```

One worker per file. Never parallel-edit the same paths across accounts.

---

## Example sessions

### Small bugfix

```
/plan Fix nil pointer in internal/providers/cursor when token path missing.

Select implementer agent:
/goal Implement approved plan. Touch only files in plan.
Done when: go test ./internal/providers/cursor/... exits 0
Spawn verifier before reporting complete.
```

### Feature with quality gates

```
/grill-me Add rate limiting to public API endpoints.

Use /feature-cycle workflow (template):
  coordinator → explorer → implementer → verifier
  Loop on verifier FAIL until all PASS.
```

### Large refactor

```
/teamwork-preview Migrate provider X to new API.
Use demo integrity mode. Real test output required.
```

---

## Builder ≠ verifier (non-negotiable)

The agent that wrote the code must not sign off. After every implementation:

1. Spawn **verifier** subagent (read-only tools)
2. Pass `git diff --name-only` and the planned file list
3. Require PASS/FAIL table with evidence (line numbers)
4. Block completion on any FAIL or scope creep

Teamwork does this natively (Critic/Challenger/Auditor). For `/goal` and custom agents, the template's verifier enforces it.

---

## File layout (template)

```
your-repo/
├── AGENTS.md                 # Short root instructions (copy from template)
└── .agents/
    ├── agents/
    │   ├── coordinator/agent.md
    │   ├── explorer/agent.md
    │   ├── implementer/agent.md
    │   └── verifier/agent.md
    ├── rules/
    │   ├── always-on-minimal-scope.md
    │   └── done-criteria.md
    ├── skills/
    │   ├── minimal-scope/SKILL.md
    │   ├── verify-before-done/SKILL.md
    │   ├── run-ci/SKILL.md
    │   ├── tdd/                    # Matt Pocock (vendored)
    │   ├── code-review/
    │   ├── implement/
    │   ├── to-spec/
    │   └── writing-for-agents/
    └── workflows/
        └── feature-cycle.md
```

---

## References

- [Custom Agents blog](https://antigravity.google/blog/introducing-custom-agents)
- [Teamwork `/teamwork-preview`](https://antigravity.google/docs/teamwork/)
- [Slash commands](https://antigravity.google/docs/slash-commands/)
- [Skills](https://antigravity.google/docs/skills/)
- [Matt Pocock skills](https://github.com/mattpocock/skills)
- [oh-my-agent verification model](https://github.com/first-fluke/oh-my-agent)
