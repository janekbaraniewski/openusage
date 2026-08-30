---
name: to-spec
description: "Turn the current conversation into a written spec with file plan and test seams. Use when the user wants a spec before coding. Writes a local markdown artifact — does not require an issue tracker."
---

# To spec

Vendored from [mattpocock/skills](https://github.com/mattpocock/skills) (MIT), adapted for Antigravity.

## Antigravity adaptation

- Do **not** require `/setup-matt-pocock-skills` or `docs/agents/issue-tracker.md`.
- Do **not** require publishing to GitHub/GitLab or applying `ready-for-agent` labels unless the user asks.
- Default: write the spec to `docs/specs/<short-name>.md` (create `docs/specs/` if needed) **or** paste it in chat if the user prefers.
- Include a **Planned files** section (required for this kit's minimal-scope workflow).

Do NOT interview the user; synthesize what you already know from the conversation. If critical gaps block a useful spec, list them under Further Notes and stop.

## Process

1. Explore the repo enough to ground the spec (patterns, existing seams). Prefer existing domain vocabulary; respect ADRs if present.
2. Sketch test seams — prefer existing public boundaries; fewer seams is better. Confirm seams with the user if ambiguous.
3. Write the spec using the template. Include Planned files with modify|create + reason.

## Spec template

```markdown
## Problem Statement
...

## Solution
...

## User Stories
1. As a <actor>, I want <feature>, so that <benefit>
(Extensive list covering the feature.)

## Implementation Decisions
- Modules / interfaces / architecture / schemas / API contracts
- Do NOT include brittle file paths in decisions *except* in Planned files below
- Prototype snippets only when they encode a decision better than prose

## Planned files
| Path | Action | Why |
|------|--------|-----|
| ... | modify/create | ... |

## Out of scope (files / behaviour)
- ...

## Testing Decisions
- What a good test is (external behaviour)
- Which modules / seams
- Prior art in the repo

## Done when
- Exact shell command(s) from AGENTS.md

## Further Notes
- ...
```
