---
name: design-feature
scope: project
description: Design new features for OpenUsage with structured design docs and implementation tasks. Triggers for any change touching 3+ subsystems, or when explicitly invoked.
keywords: design, feature, architecture, plan, rfc
---

# OpenUsage Feature Designer

**Invocation**: When a user asks to design, plan, or architect a feature — OR when a proposed change touches 3+ subsystems (core, providers, TUI, config, detect, daemon, telemetry).

---

## Phase 0 — Quiz (MANDATORY)

Before any design work, gather answers to ALL of these. Research the codebase yourself if the user doesn't know.

1. **What problem does this solve?** One sentence. What's broken or missing today?
2. **Who benefits?** End users, contributors, or both?
3. **What subsystems are affected?** List from: core types, providers, TUI, config, detect, daemon, telemetry, CLI commands.
4. **What's explicitly out of scope?** Name at least one thing this feature does NOT do.
5. **Are there existing design docs that overlap?** Check `docs/*.md` for related designs. If overlap exists, ask the user whether to extend or create new.
6. **What's the simplest version that delivers value?** Identify the MVP slice.
7. **Does this change any public interfaces?** (`UsageProvider`, `UsageSnapshot`, `AccountConfig`, config JSON schema)
8. **Backward compatibility concerns?** Will existing configs, stored data, or provider behavior break?

---

## Phase 1 — Explore (MANDATORY)

Read these before designing. Skip only if already in context:

1. **Core types**: `internal/core/types.go`, `internal/core/provider_spec.go`, `internal/core/widget.go`
2. **Affected subsystems**: Read the primary files for each subsystem from Q3.
3. **Existing design docs**: Read any overlapping docs from `docs/`.
4. **Related providers**: If the feature changes provider behavior, read at least one provider of each affected pattern (header probing, rich API, local files, CLI).
5. **Config schema**: `internal/config/config.go` + `configs/example_settings.json`

After reading, summarize findings that affect the design. Don't just list files — state what you learned.

---

## Phase 2 — Design

Write the design doc to `docs/<FEATURE_NAME>_DESIGN.md`. Use the template in `references/design-template.md`.

### Design principles for this project

- **Simplest thing that works.** No abstractions for hypothetical futures.
- **Additive over breaking.** New fields, new types, new files. Don't restructure what works.
- **Provider patterns are sacred.** Don't force providers into a new pattern. If a provider needs special handling, let it be special.
- **Maps and slices over deep type hierarchies.** The codebase uses flat data (`map[string]Metric`, `map[string]string`) — follow that.
- **Config drives behavior.** Features should be configurable in `settings.json`. Sensible defaults, no mandatory config.
- **TUI is the consumer, not the source of truth.** Business logic in `core/` or subsystem packages, rendering in `tui/`.

### What NOT to do

- Don't introduce interfaces for one implementation.
- Don't add a package for fewer than 3 files.
- Don't design middleware/plugin systems — direct function calls are fine.
- Don't propose database migrations unless the feature requires persistence.
- Don't over-specify error handling — match existing patterns (`fmt.Errorf("provider: action: %w", err)`).

---

## Phase 3 — Implementation Tasks

After the design doc is written, break it into implementation tasks. Each task should be:

- **Self-contained**: Can be implemented and tested independently.
- **Ordered**: Tasks list their dependencies explicitly.
- **Concrete**: Names the files to create/modify and the tests to write.
- **Parallelizable when possible**: Tasks with no mutual dependencies should be identifiable as a parallel group.

Format each task as:

```
### Task N: <title>
Files: <list of files to create or modify>
Depends on: <task numbers or "none">
Description: <what to do, 2-4 sentences>
Tests: <what tests to write>
```

After all tasks, add a **dependency summary** showing which tasks can run in parallel:

```
### Dependency Graph
- Task 1, 2: sequential (foundational types and config)
- Tasks 3, 4, 5: parallel group (all depend on 1-2, independent of each other)
- Task 6: depends on 3, 4
- Task 7: depends on all (integration verification)
```

This helps the implementer (`/implement-feature`) launch parallel agents for independent tasks, significantly reducing implementation time.

### Task design tips

- **Minimize cross-task file overlap.** If two tasks both modify `server.go`, consider whether they can be merged or ordered to avoid merge conflicts during parallel execution.
- **Test helpers are shared state.** If a task changes a function signature that test helpers use, include the test helper update in that same task — don't leave it for integration verification.
- **TUI tasks typically depend on everything else.** The TUI wires together all subsystem changes, so TUI tasks should come last.

Append tasks to the design doc under a `## Implementation Tasks` section.

---

## Checklist

Before finishing:

- [ ] All 8 quiz questions answered
- [ ] Codebase exploration completed for affected subsystems
- [ ] Overlap with existing design docs addressed (extended or new, per user choice)
- [ ] Design doc written to `docs/<NAME>_DESIGN.md`
- [ ] Problem statement is one clear sentence
- [ ] Goals and non-goals are explicit
- [ ] Impact analysis covers all affected subsystems
- [ ] Component design is detailed but not over-abstracted
- [ ] No unnecessary interfaces, packages, or abstractions
- [ ] Backward compatibility addressed
- [ ] Implementation tasks are concrete and ordered
- [ ] Each task names specific files and tests
