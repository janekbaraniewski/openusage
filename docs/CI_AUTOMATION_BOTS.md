# CI automation — bots & tools landscape

A research note backing the `ci/automation-bots` PR. We list every category of bot/tool we considered, what's already in place, what we're adding, and what we're explicitly skipping (and why).

> This file is a snapshot of decisions taken in May 2026. It's safe to delete or rewrite once the choices it justifies feel obvious.

## Decision summary

| Category | Choice | Status |
|---|---|---|
| Dependency updates | **Dependabot** (already in place) — extended to npm, with grouping | enabled |
| Dependabot auto-merge | **Custom workflow** using `dependabot/fetch-metadata` + `gh pr merge --auto --squash` | added |
| Go vulnerability scanning | **`govulncheck-action`** | added |
| Supply-chain score | **OpenSSF Scorecard** | added |
| Broken link checker | **`lycheeverse/lychee-action`** (nightly + on docs PR) | added |
| Release automation | **`release-please`** for changelog/version PRs | added |
| Stale issue/PR cleanup | **`actions/stale`** | added |
| Container/IaC scanning | Trivy | skipped — no container/IaC in repo yet |
| License compliance | FOSSA / `licensee` | skipped — single MIT, single license file |
| Coverage upload | Codecov / Coveralls | skipped — no public dashboard target |
| Renovate | alternative to Dependabot | skipped — Dependabot covers our case |
| Lighthouse CI | docs perf | skipped — premature; revisit when traffic grows |
| Vale / markdownlint | prose linting | skipped — high signal-to-noise cost; revisit |
| CLA assistant | external contribs | skipped — not currently accepting external PRs at scale |

## What's already on

| Tool | Workflow | Purpose |
|---|---|---|
| GitHub Dependabot | `.github/dependabot.yml` | Dependency PRs (gomod + actions) |
| Dependency Review | `.github/workflows/dependency-review.yaml` | Block PRs that introduce vulnerable deps |
| CodeQL | `.github/workflows/codeql.yaml` | Static analysis security findings |
| Secret scanning | GitHub native, free for public repos | Leaked-secret detection |
| `golangci-lint` | `.github/workflows/ci.yaml` | Static analysis |
| `go vet` | `.github/workflows/ci.yaml` | Static analysis |
| `go test -race` | `.github/workflows/ci.yaml` | Race detector |
| Goreleaser | `.github/workflows/release.yaml` | Cross-platform release builds |
| Cloudflare Pages PR previews | `.github/workflows/docs-preview.yaml` | Per-PR docs preview URL |

## What we're adding

### 1. Extend Dependabot to npm + group updates

The repo has two npm trees we currently don't watch: `website/` (Vite marketing site) and `docs/site/` (Docusaurus). Bots have already shipped two security advisories that affected `docs/site/` — Dependabot needs to watch it.

We also add **grouping** so that `@docusaurus/*` patch+minor bumps come as a single PR instead of fifteen.

### 2. Dependabot auto-merge workflow

Approach: every Dependabot PR is auto-approved and gets squash auto-merge enabled. Branch protection on `main` enforces that every required check must pass before the squash actually fires. If CI fails, the PR sits open for human attention — no force-merging.

CI is the safety net. We trust that:

- `go test -race`, `golangci-lint`, and `vet` catch behavioral regressions
- `govulncheck` catches reachable CVEs introduced by the bump
- The Dependency Review action blocks PRs that introduce vulnerable transitive deps
- The Docusaurus build catches anything that breaks docs tooling

If those gates pass, the bump is safe to ship. The cost of human review on every patch update is higher than the residual risk this leaves.

**This requires branch protection on `main` with required status checks.** Without it, `gh pr merge --auto` merges as soon as nothing is blocking — which is "immediately" if nothing's required.

**Required-check workflows must NOT have `paths:` filters.** If a required workflow's path filter doesn't match a PR's diff, the check never fires, so the required-check gate is never satisfied, and auto-merge stalls forever waiting for a check that won't run. We've removed path filters from `lychee.yaml` and `govulncheck.yaml` for this reason. Non-required workflows (e.g. `docs-preview.yaml`) keep their path filters.

### Required checks

The current required-check set on `main`:

- `Build (ubuntu-latest)`, `Build (macos-latest)`
- `Test (ubuntu-latest)`, `Test (macos-latest)`
- `Lint`, `Vet`, `gofmt`, `Check go.mod tidiness`
- `Review` (Dependency Review), `CodeQL`, `Scan for known Go vulnerabilities`
- `Lychee`

We use the **native GitHub auto-merge** (via `gh pr merge --auto`) instead of a third-party action. Cleaner, no extra permissions to grant.

### 3. `govulncheck-action`

Go's official vulnerability scanner. Different from Dependency Review (which scans manifests) — `govulncheck` does call-graph analysis, so it only flags vulnerabilities that are actually reachable from code. Lower noise.

Runs on every PR plus nightly to catch newly-published advisories.

### 4. OpenSSF Scorecard

Publishes a public security/maintainability score (0-10) for the repo. Useful to:

- Catch missing best practices we don't even know about
- Provide a signal to downstream consumers evaluating the project
- Track score over time

Runs nightly on the default branch. Adds a badge URL to the README (separate PR).

### 5. Lychee broken-link checker

Docusaurus catches *internal* broken links at build time. It does NOT catch:

- Links to external GitHub URLs (could rot)
- Links to vendor docs (Anthropic, OpenAI, etc.)
- Cross-page links to legacy marketing-site URLs

Lychee fixes that. Runs:

- On PRs that touch `docs/site/docs/**` or `README.md`
- Nightly on `main` (creates a sticky issue if anything broke)

Configured to skip transient endpoints (rate-limit-prone APIs) via a `lychee.toml`.

### 6. `release-please` for automated releases

Replaces the manual tag-and-release flow. How it works:

- Watches the default branch
- Parses commit messages (Conventional Commits — we already use this)
- Maintains a draft "Release v0.X.Y" PR that always reflects what would happen if you released now
- When the PR is merged, it tags the commit and triggers the existing Goreleaser workflow (no goreleaser changes needed)
- Generates `CHANGELOG.md` automatically

Benefit: the v0.10.1 / v0.10.2 cuts we just did become a single click on a PR.

### 7. Dependabot rebase-on-main-update workflow

A separate workflow at `.github/workflows/dependabot-rebase-on-main.yaml` runs on every push to `main` and updates open Dependabot PR branches with an automation token.

Dependabot does automatically rebase PRs, but it is not an immediate per-push guarantee and strict branch protection requires PR branches to be current before auto-merge can complete. The workflow is the deterministic backstop: it calls GitHub's `updateBranch` API with `AUTOMATION_TOKEN`, which must be a PAT with `repo` + `workflow` scope or an equivalent GitHub App token. Using `GITHUB_TOKEN` here is not sufficient: GitHub suppresses follow-up workflow runs for most events created by `GITHUB_TOKEN`, and it also cannot update PRs that touch `.github/workflows/**` without workflow-level permission.

Conflicted or polluted Dependabot PRs are handled separately. If a PR is `dirty` or already has non-Dependabot commits, the workflow requests `@dependabot recreate` using `AUTOMATION_TOKEN`; this must be a real write-access user token because Dependabot ignores/denies commands from `github-actions[bot]`.

### 8. Stale issue/PR bot

`actions/stale` with conservative defaults:

- Issues: warn at 90 days idle, close at 120
- PRs: warn at 60, close at 90
- Anything labeled `pinned` or `security` is exempt
- Friendly comment, easy reopen

## What we're skipping (and why)

- **Trivy / container scanning** — we don't ship containers. Add when we do.
- **License compliance bots** (FOSSA, etc.) — single-license MIT project, low value.
- **Codecov / Coveralls** — coverage is already collected by `make test`; no dashboard target. Adds friction without proportional value at this stage.
- **Renovate** — superset of Dependabot's features but Dependabot covers our case and is GitHub-native. Don't run two dependency bots.
- **Lighthouse CI** for the docs site — premature. Revisit when docs traffic justifies investment in perf regressions.
- **Vale / markdownlint** — prose linting is high-signal but high-friction. Revisit when there's more contributor traffic to standardize.
- **CLA assistant** — not accepting external contributions in volume yet.

## Required repo settings

For auto-merge to work, the repo needs:

- **Auto-merge enabled** on the repo (Settings → General → Pull Requests → Allow auto-merge)
- **Branch protection** on `main` requiring CI checks to pass
- **GITHUB_TOKEN** with write permission for the auto-merge workflow

For `release-please`:

- The workflow needs `contents: write`, `pull-requests: write`, and `actions: write` on the `GITHUB_TOKEN`.
- Configure a `RELEASE_PLEASE_TOKEN` repository secret with `contents`, `pull_requests`, and `workflow` write access for native release PR updates that trigger downstream PR checks. If that secret is absent, the workflow uses `AUTOMATION_TOKEN`. It falls back to `GITHUB_TOKEN` only as a last resort, and that fallback still needs the explicit refresh/dispatch backstop below because events created by `GITHUB_TOKEN` do not chain into normal `pull_request` runs.
- Release PR commits are authored by `github-actions[bot]`, so the same "no chained workflow runs from bot-authored commits" rule applies there too.
- When the fallback `GITHUB_TOKEN` path is used, the workflow keeps any open release PR branch current with `main` through `refresh-pr-branches` and then calls the same reusable required-check dispatcher. This is required because `release-please` PR updates made with `GITHUB_TOKEN` do not automatically trigger downstream `pull_request` workflows.

For manually dispatched required-check workflows:

- `CI`, `Dependency Review`, and `CodeQL` need `workflow_dispatch` enabled.
- `Dependency Review` must set `base-ref`/`head-ref` explicitly on `workflow_dispatch`, because outside `pull_request` events GitHub does not infer the comparison pair for the action.

For Scorecard:

- Public repo only (which we are)
- A `SCORECARD_READ_TOKEN` PAT with read access to GitHub branch protection settings (or skip the branch-protection check)

## Rollout order

1. Land Dependabot extension + auto-merge first; wait one week to confirm no surprises
2. Then govulncheck + lychee + Scorecard (low-risk, nightly)
3. Then release-please (changes the muscle memory for cutting releases)
4. Stale bot last, on a quiet week
