# Antigravity Workflow Kit

Copy-paste Antigravity **custom agents**, **rules**, and **skills** for multi-agent workflows with **minimal file scope** and **verification gates**.

| Doc | Purpose |
|-----|---------|
| [GUIDE.md](./GUIDE.md) | Full recommendations (GitHub projects, slash commands, Matt Pocock, multi-account) |
| [SETUP.md](./SETUP.md) | Install into your project |
| [template/](./template/) | Ready-to-copy `.agents/` + `AGENTS.md` |

## Quick copy

```bash
./docs/antigravity-workflow-kit/install-template.sh /path/to/your/project
```

Or manually:

```bash
cp -R docs/antigravity-workflow-kit/template/. /path/to/your/project/
```

## Highlights

- **coordinator / explorer / implementer / verifier** custom agents
- **minimal-scope** skill — plan files first; verifier rejects diff creep
- **Matt Pocock skills** vendored: `tdd`, `code-review`, `implement`, `to-spec`, `writing-for-agents`
- **`/feature-cycle`** workflow chaining the roles

Not affiliated with Google Antigravity. Community template for the Antigravity harness.
