# ISSUE-277: Copilot provider against GitHub Enterprise (custom hostname)

## Problem (as reported)

The `copilot` provider shells out to `gh`, which always targets the default
host (`github.com`) unless told otherwise. Users on a GitHub Enterprise Cloud
instance manage their Copilot subscription there, so
`gh api /copilot_internal/user` returns `401` against `github.com`.
The reporter's local patch hard-coded `--hostname my-org.ghe.com`; the open
question is how to model "gh supports many hosts" in openusage.

## Upstream context you must know (I first missed this)

I initially implemented this as a `github_hostname` provider-paths key with
per-command `--hostname` flags — and left the single shared `apiCache`
cross-talking between accounts. That contradicts the design the maintainer
merged in PR #340 (`docs/proposals/provider-options-and-gh-hosts.md`,
draft status, docs-only — no code). The implementation on this branch now
follows that proposal instead:

- `AccountConfig.Options` (`map[string]string`, persisted) + `Option` /
  `SetOption` in `openusage-go/internal/core/provider.go` — the general
  "extra provider config" mechanism. `Option` deliberately does NOT fall
  through to `RuntimeHints` (transient detection state ≠ config).
- `ProviderOption` + `ProviderSpec.Options` in
  `openusage-go/internal/core/provider_spec.go` — self-describing knobs so
  the settings UI can render them. Copilot declares exactly one:
  `gh_host` ("GitHub host", optional, placeholder `github.com`).
- `ghCLI{binary, host}` in
  `openusage-go/internal/providers/copilot/api_data.go` — the single choke
  point. `run` sets `GH_HOST` on the child env (explicitly, not inherited —
  safe for GUI-launched daemons); `api` keeps the endpoint as the last argv
  word. `Fetch` builds one `gh` from `acct.Option("gh_host", "")` and passes
  it where it used to pass the `ghBinary` string.
- Per-account cache: `Provider.apiCache` is now
  `map[string]*copilotAPICache` keyed by `acct.ID` (`cacheFor` in
  `openusage-go/internal/providers/copilot/copilot.go`). This ships with the
  change because one Provider serves all copilot accounts — without it, a
  `github.com` account's `authOK`/snapshot would be served to the GHE
  account and vice versa.
- Multiple instances = multiple accounts, each with its own `gh_host`
  (the `(multiple?)` in the issue title). Detection auto-discovery from
  `gh auth status` is proposal phase 2 and intentionally NOT done here.

An enterprise account is just:

```json
{
  "id": "copilot-acme",
  "provider": "copilot",
  "auth": "cli",
  "options": { "gh_host": "acme.ghe.com" }
}
```

## Why this fix works

Every network call the provider makes goes through `ghCLI.run`:
`auth status` (`copilot.go`), `/user`, `/copilot_internal/user`,
`/rate_limit`, `/orgs/<org>/copilot/{billing,metrics}` (`api_data.go`).
Version detection (`gh copilot --version` / `copilot --version`) is local and
deliberately stays host-free. So one env var in one place covers every
host-sensitive call, including paths the `--hostname` flag doesn't accept.

`GH_HOST` mechanism validated against real `gh` (no GHE instance here, so
validated on github.com): `GH_HOST=github.com gh api /user` returns the same
login as bare `gh api /user`, `gh auth status` resolves tokens under it, and
`GH_HOST=nonexistent.invalid gh api /user` tries that host — i.e. the var is
honored for both token resolution and endpoint routing. GHE-end-to-end still
needs the reporter's instance: `gh auth login --hostname <host>`, set
`options.gh_host`, refresh, confirm the tile populates instead of 401ing.

## Status: implemented on this branch

core `Options`/`Option`/`SetOption` + `ProviderOption`/`Spec.Options` with
unit tests; copilot `ghCLI` + `GH_HOST` choke point; per-account cache;
`gh_host` declared in `Spec()`; `copilot_ghhost_test.go` (env propagation,
default-unset, per-account isolation, spec declaration); GHE section in
`docs/site/docs/providers/copilot.md`; enterprise entry in
`configs/example_settings.json`. Full `go test ./...` green (57 packages).

## How to test (what you run to confirm)

1. `GOCACHE=/tmp/openusage-gocache go test ./internal/providers/copilot/ ./internal/core/`
   — fake-`gh` tests assert `GH_HOST` on every auth/api call when
   `options.gh_host` is set, absence by default, per-account cache
   isolation (two hosts → two auth calls, cached re-fetch spawns none),
   and the `Spec()` declaration.
2. `GOCACHE=/tmp/openusage-gocache go test ./...` — full suite.
3. GHE maintainer check (needs a real enterprise host): as above.
