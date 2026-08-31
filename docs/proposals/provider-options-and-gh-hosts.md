# Proposal: provider `Options` + multi-host GitHub Copilot support

Status: **draft** (local, not filed)
Tracks: [#277 — support for (multiple?) dedicated github instances](https://github.com/janekbaraniewski/openusage/issues/277)
Author: (draft)

## 1. Problem

The `copilot` provider shells out to the `gh` CLI, which always targets the
default host (`github.com`) unless told otherwise. Users on a GitHub Enterprise
Cloud instance manage their Copilot subscription on that instance, so
`gh api /copilot_internal/user` returns `401` against `github.com`.

The reporter's working local patch hard-codes `--hostname my-org.ghe.com` into
the `gh` invocations. That proves the mechanism but isn't configurable, and the
issue asks the right follow-up: **`gh` supports many hosts — how do we model that
in openusage?**

Two needs are tangled here:

1. **A general one** — a first-class way to pass provider-specific, persisted,
   non-secret *configuration knobs* to a provider (a hostname, an org filter, a
   feature toggle). openusage doesn't cleanly have this yet.
2. **A specific one** — the `copilot` provider should target one or more GitHub
   hosts.

This proposal solves (1) properly and uses it to solve (2).

## 2. What we already have (and why none of it fits)

`core.AccountConfig` (internal/core/provider.go) carries several config-ish
fields today:

| Field | Purpose | Persisted | Fit for a gh hostname? |
|-------|---------|-----------|------------------------|
| `BaseURL` | HTTP API base URL for configurable endpoints | yes | No — a gh *hostname* is neither an HTTP URL nor a data path, so it doesn't fit `BaseURL`'s live meaning. (`BaseURL` and `Binary` are *not* deprecated — both have current primary uses. Only their historical *data-path* overload is being migrated into `ProviderPaths`; see §8.) |
| `ProviderPaths` (`Path`/`SetPath`) | named filesystem paths / data locators (`tracking_db`, `state_db`) | yes | No — semantically "paths", and `Path()` deliberately falls through to `RuntimeHints`, mixing persisted config with transient detection state. |
| `RuntimeHints` (`Hint`/`SetHint`) | detection metadata + local hints | **no** (`json:"-"`) | No — never persisted, so a user-entered hostname would evaporate on the next run. |
| `Binary` | CLI binary path | yes | No — that's the `gh` binary, orthogonal to the host. |

The gap is precise: there is **no persisted, provider-scoped bag for scalar
configuration knobs** that aren't paths, URLs, or secrets. Every existing option
is either the wrong shape, transient, or a shared field the codebase is trying to
stop overloading.

## 3. Proposal — part A: a general `Options` mechanism

Add one persisted, provider-scoped string map to `AccountConfig`, mirroring the
existing `Path`/`Hint` accessor pattern so it reads like the rest of the type.

```go
// Options holds provider-specific, persisted, non-secret configuration knobs
// that are not paths, URLs, or credentials — e.g. a gh hostname, an org filter,
// a feature toggle. Keys are provider-defined and declared in ProviderSpec.
// Never store secrets here (they belong in the credentials store / api_key_env).
Options map[string]string `json:"options,omitempty"`
```

```go
// Option returns the named provider-specific option, or fallback when unset.
// Unlike Path, it does NOT fall through to RuntimeHints — Options is
// persisted configuration only, kept distinct from transient detection state.
func (c AccountConfig) Option(key, fallback string) string {
	if c.Options != nil {
		if v, ok := c.Options[key]; ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return fallback
}

// SetOption stores a named provider-specific option.
func (c *AccountConfig) SetOption(key, value string) {
	if c == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	if c.Options == nil {
		c.Options = make(map[string]string)
	}
	c.Options[strings.TrimSpace(key)] = strings.TrimSpace(value)
}
```

Deliberately **not** reusing `Path()`'s fallthrough: `Options` is persisted
config, `RuntimeHints` is transient detection. Keeping them separate is what
makes this "config" rather than "another hint bag".

### Part A': make options self-describing in `ProviderSpec`

A free-form `map[string]string` is flexible but opaque — the settings UI can't
render it, and typos in keys fail silently. Declare the options a provider
understands, so the map is schema-driven and the UI/validation come for free:

```go
// In core/provider_spec.go
type ProviderOption struct {
	Key         string // e.g. "gh_host"
	Label       string // "GitHub host", shown in settings
	Placeholder string // "my-org.ghe.com"
	Help        string // one line
	Required    bool
}

type ProviderSpec struct {
	// ...existing fields...
	Options []ProviderOption // declared provider-specific config knobs (optional)
}
```

This is optional per provider (empty for the 15 that need nothing), costs
nothing to those, and gives the settings modal a concrete list to render.
Providers that don't declare an option simply ignore any stray keys.

## 4. Proposal — part B: `copilot` targets a host via `gh_host`

### 4.1 Config surface

The `copilot` provider declares one option:

```go
Options: []core.ProviderOption{{
	Key:         "gh_host",
	Label:       "GitHub host",
	Placeholder: "github.com",
	Help:        "Target a GitHub Enterprise host instead of github.com.",
}},
```

An enterprise account is then just:

```json
{
  "id": "copilot-acme",
  "provider": "copilot",
  "auth": "cli",
  "options": { "gh_host": "acme.ghe.com" }
}
```

No schema migration: `options` is additive and `omitempty`.

### 4.2 Threading the host: `GH_HOST` env, one choke point

The reporter's patch sprinkles `--hostname` across individual commands. There's a
cleaner mechanism: `gh` honors the **`GH_HOST`** environment variable as the
default host for every subcommand. Set it once in the single `exec.Command`
choke point (`runGH`, api_data.go:353) and *all* API/auth calls target the right
host uniformly — no per-command flag surgery, and it naturally covers calls the
`--hostname` flag doesn't accept.

Rather than add a `host string` positional to `runGH`/`runGHAPI` and every
`fetch*` helper that threads `ghBinary` today, introduce a small value type that
carries the invocation context:

```go
type ghCLI struct {
	binary string
	host   string // "" = gh default (github.com)
}

func (g ghCLI) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.binary, args...)
	cmd.Env = os.Environ()
	if g.host != "" {
		cmd.Env = append(cmd.Env, "GH_HOST="+g.host)
	}
	// ...existing output handling...
}

func (g ghCLI) api(ctx context.Context, endpoint string) (string, error) {
	return g.run(ctx, "api", /* existing headers */, endpoint)
}
```

`Fetch` builds one `gh := ghCLI{binary: ghBinary, host: acct.Option("gh_host", "")}`
and passes `gh` where it currently passes `ghBinary string`. The host logic lives
in exactly one place.

> Note: the reporter validated `--hostname` end-to-end; `GH_HOST` is the
> documented env equivalent and is preferred here for the single-choke-point
> property. Worth a 2-minute confirmation against a real GHE instance before
> merge — if any code path misbehaves, fall back to appending `--hostname` inside
> `api()`/`authStatus()` (still one place).

### 4.3 Multiple instances = multiple accounts

openusage is already multi-account. "Multiple GitHub instances" is naturally *N*
copilot accounts, each with its own `gh_host` (github.com needs none). No new
"list of hosts" concept — the account list already is that list.

### 4.4 Required fix: key the cache per host

`copilot.Provider` holds a **single, unkeyed `apiCache`** (copilot.go:36-56),
but one Provider instance serves *all* copilot accounts via `Fetch(acct)`. Today
that's invisible (one account); with multiple hosts it becomes a correctness bug:
`authOK`, `version`, and `lastSnap` from `github.com` would be served to the
`acme.ghe.com` account and vice-versa.

This must ship with part B. Options, cheapest first:

- **Key the cache by account.** Replace `apiCache *copilotAPICache` with
  `apiCache map[string]*copilotAPICache` keyed by `acct.ID` (or `gh_host`),
  guarded by the existing `cacheMu`. Minimal, local, obviously correct.
- Alternatively, per-account provider instances — larger change, not warranted.

The binary-resolution cache (`gh`/`copilot` path) is host-independent and could
stay shared, but keying the whole struct by account is simpler than splitting it.

### 4.5 Detection (phase 2, optional)

`gh auth status` lists *every* authenticated host. The detector could parse it
and auto-create one copilot account per host (github.com stays the default,
enterprise hosts get `options.gh_host` prefilled). Nice-to-have; the manual
`options` entry is the MVP and stands alone.

## 5. Why this is the right shape

- **Solves the general problem, not just the symptom.** `Options` is the
  reusable "extra provider config" mechanism the codebase was missing; #277 is
  its first consumer, not a special case.
- **On the codebase's trajectory.** Named, provider-scoped config over
  overloaded shared scalars (`BaseURL`/`Binary`) — the same direction
  `ProviderPaths` already established.
- **Self-documenting.** `ProviderSpec.Options` lets the settings UI and
  validation be generated, not hand-maintained per provider.
- **Multi-instance falls out of the existing account model** — no bespoke host
  list, no schema migration.
- **One integration seam.** `GH_HOST` in a single `ghCLI.run` choke point; the
  per-account cache key is the only other required change.

## 6. Scope / task breakdown

1. `core`: add `Options` field + `Option`/`SetOption` (+ tests). *(small)*
2. `core`: add `ProviderSpec.Options` / `ProviderOption`. *(small)*
3. `copilot`: `ghCLI{binary,host}` value type; thread `GH_HOST`; read
   `acct.Option("gh_host","")`; declare the option in `Spec()`. *(medium)*
4. `copilot`: key `apiCache` by account/host. *(small, correctness-critical)*
5. TUI settings modal: render declared `ProviderSpec.Options` as editable
   fields. *(medium; can trail the core+copilot change)*
6. Detection: auto-discover gh hosts from `gh auth status`. *(phase 2, optional)*
7. Docs: `docs/site/docs/` provider page — configuring Copilot for GitHub
   Enterprise. *(required by repo policy)*
8. `configs/example_settings.json`: an enterprise copilot entry.

## 7. Open questions

- **Option name:** `gh_host` vs `hostname`. `gh_host` mirrors the `GH_HOST` env
  var and is provider-unambiguous; leaning `gh_host`.
- **`GH_HOST` vs `--hostname`:** confirm `GH_HOST` covers `gh api` token
  resolution on a GHE host (§4.2 note).
- **Should `Options` values ever be validated/typed?** Starting stringly-typed;
  `ProviderOption.Required` is the only validation for now.

## 8. Related: finish the pre-existing config-field migrations

Adding `Options` sits next to two migrations already in the codebase. `Options`
does not depend on them, but they're "started work" worth closing so the config
surface stays coherent. **`Binary` and `BaseURL` are NOT deprecated** — both have
current primary meanings (`Binary` = CLI binary path, set by ~20 detectors and
read by copilot/codex/gemini_cli; `BaseURL` = HTTP API base, read by
azure_openai/codex/ollama/zai/`shared`). Only the items below are legacy.

### 8.1 `Paths` → `ProviderPaths` (a map rename) — nearly done

`config.normalizeAccounts` (config.go:377-392) already copies `Paths` →
`ProviderPaths` and nils `Paths` on every load; new code writes via `SetPath`.
Remaining:

- Drop the `c.Paths` fallback in `Path()` (provider.go:56) and `PathMap()`
  (provider.go:112) — dead for any normalized account.
- Keep the `Paths` field only as a JSON unmarshal target for old files (or drop
  it once pre-migration configs are no longer supported).

Provider-agnostic; lives entirely in core.

### 8.2 `Binary`/`BaseURL` *data-path overload* → `ProviderPaths` — stuck, no finish line

Two local-hybrid providers historically stored a **data path** in these scalars,
and per-`Fetch` in-memory shims translate it (never rewriting persisted config):

- `cursor/legacy_paths.go`: `Binary`→`tracking_db`, `BaseURL`→`state_db`
- `claude_code/legacy_paths.go`: `Binary`→`stats_cache`, `BaseURL`→`account_config`

Because the shim runs at `Fetch` on a local copy, the old values live in
`settings.json` forever — the migration can never complete. It's stuck per-`Fetch`
(not load-time like §8.1) because the `Binary`→key mapping is provider-specific,
while core config normalization is provider-agnostic.

**Finish plan:**

1. Add a provider hook (e.g. `NormalizeLegacyConfig(*core.AccountConfig)`, or fold
   into `Spec()`), and have config-load call it once per account — moving the
   data-path value into the correct `ProviderPaths` key and **clearing**
   `Binary`/`BaseURL`. Safe for cursor/claude_code, which never used those scalars
   for anything but data paths.
2. The rewrite persists on the next config save (same effective behavior as §8.1).
3. After one release carrying the load-time rewrite, delete the two
   `legacy_paths.go` per-`Fetch` shims.
4. Leave `Binary`/`BaseURL` on the struct — still live for every other provider.

This can ship independently of, and before or after, the `Options` change.
