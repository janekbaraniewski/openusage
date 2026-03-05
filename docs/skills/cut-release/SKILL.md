# Skill: Cut Release

Create a new release for OpenUsage — tag, push, and publish GitHub release with hand-crafted notes.

## When to use

When the user asks to create a new release, cut a release, or bump the version.

## Prerequisites

- All changes merged to `main`
- On `main` branch (or will checkout)
- `gh` CLI authenticated

## Phases

### Phase 1 — Determine Version

1. Fetch tags: `git fetch --tags`
2. Find latest tag: `git tag --sort=-v:refname | head -1`
3. Suggest next version based on changes:
   - **Patch** (0.x.Y): bug fixes, performance improvements, small features
   - **Minor** (0.X.0): significant new features, breaking changes to internal APIs
   - Major bumps are not expected pre-1.0
4. Confirm version with user.

### Phase 2 — Review Changes

1. Fetch and update main: `git fetch origin main && git checkout main && git pull origin main`
   - If local changes conflict, stash first
2. List all commits since last tag: `git log <last-tag>..origin/main --oneline`
3. List merged PRs since last tag: `gh pr list --state merged --json number,title,mergedAt` filtered by date
4. Review the diff: `git diff <last-tag>..origin/main --stat`
5. Categorize changes into sections (see Release Notes Format below)

### Phase 3 — Create Tag and Release

1. Ensure on main at HEAD: `git checkout main && git pull origin main`
2. Create tag: `git tag v<version>`
3. Push tag: `git push origin v<version>`
   - This triggers the Release workflow (GoReleaser + macOS builds + Homebrew tap update)
4. Create GitHub release with hand-crafted notes: `gh release create v<version> --title "v<version>" --notes "..."`

### Phase 4 — Verify

1. Check workflow started: `gh run list --workflow=release.yaml --limit 1`
2. Report release URL to user

## Release Notes Format

Use this exact format. No version header, no tagline — start directly with `## Changelog`.

```markdown
## Changelog

### Performance
* Description of perf improvement (#PR)

### Features
* **Bold feature name** — description of what it does (#PR)

### Bug Fixes
* Description of fix (#PR)

### Maintenance
* Description of chore/refactor (#PR)

**Full Changelog**: https://github.com/janekbaraniewski/openusage/compare/v<prev>...v<version>
```

### Format rules

1. **No emojis** — not in headers, not in bullet points, nowhere
2. **No version header or tagline** — don't start with `## OpenUsage <version>` or project description. Jump straight to `## Changelog`
3. **PR references** — every bullet ends with `(#<number>)` linking to the PR that introduced it
4. **Section headers** — use: `Performance`, `Features`, `Bug Fixes`, `Maintenance`. Omit empty sections.
5. **Bold for feature names** — use `**bold**` for the feature name, followed by ` — ` (em dash) and description
6. **Full Changelog link** — always include at the bottom comparing previous tag to current
7. **No commit hashes** — don't include commit SHAs in the notes (unlike goreleaser auto-generated ones)
8. **No author attribution** — don't include `(@username)` in bullet points
9. **Concise descriptions** — each bullet should be 1 line, explain *what* changed not *how*
10. **Group related changes** — if multiple PRs contribute to one feature area, combine into a single bullet referencing all PRs

### What NOT to include

- Internal refactors that don't affect users (unless significant)
- `gofmt` / lint-only commits
- CI/workflow changes (unless they affect the release artifacts)
- WIP or stash commits
- Merge commits
- Design docs or documentation-only changes (unless user-facing docs)

### Goreleaser note

The release workflow uses GoReleaser which auto-generates its own changelog. When we create the release with `gh release create` *before* GoReleaser runs, our hand-crafted notes take precedence. GoReleaser will not overwrite an existing release body. This is the intended flow — we want curated notes, not auto-generated commit dumps.

Note: `.goreleaser.yaml` has a `release.header` template with a version header and tagline. That only applies when GoReleaser creates the release from scratch (i.e., if we don't pre-create it). Our flow always pre-creates the release, so that header is never used.

## Rules

1. NEVER create a tag without user confirmation of the version number.
2. NEVER tag anything other than the HEAD of `main`.
3. NEVER delete or move existing tags.
4. Always review the full diff before writing release notes — don't guess what changed.
5. If the release workflow fails, report it — don't try to manually upload artifacts.
6. If there are no meaningful changes since the last tag, tell the user — don't create an empty release.
