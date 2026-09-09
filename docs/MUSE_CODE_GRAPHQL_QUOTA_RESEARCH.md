# Muse Code subscription quota: GraphQL research

Date: 2026-09-07. Status: replay proven live, longevity unknown.
No secrets in this file: field and endpoint names only, no tokens or IDs.

## Bottom line

Meta publishes no quota/spend API for Muse Code: every plausible
`api.meta.ai/v1/*` usage/billing path returns 404 even with a valid key
(probed 2026-09-07, post Muse Spark 1.3). The dashboard figures come from a
private Comet GraphQL route that replays programmatically with a byte-exact
envelope. Percentages are derived, not served: no raw percent field exists.

## The call

- `POST https://dev.meta.ai/api/graphql/`, form-urlencoded
- `fb_api_req_friendly_name=LLMDCUsageQuery`,
  `__crn=comet.llamaapi.LLMDCUsageRoute`, `fb_api_caller_class=RelayModern`
- `variables`: `api_key_id` (null), `model_id` (null), `team_id` (required,
  per-account setting), `start_date`/`end_date` (range), `timezone`, plus
  Relay persist flags (`..._ShouldIncludeSubscriptionQuotarelayprovider: true`,
  `..._ShouldIncludeCostMetrics...: true`, `..._ShouldIncludeImageMetrics...: true`)
- Auth: session cookies for `dev.meta.ai` (candidates `llm_sess`,
  `ecto_1_sess`; the rest are device/analytics) plus per-load tokens
  (`fb_dtsg`, `lsd` / `X-FB-LSD`).
- A sibling `LLMDCBillingBannerContainerQuery` (payment method, grace
  period) carries no quota numbers; ignore it.

## Response shape (`data.team.subscription_quota_usage`)

- `tier` (e.g. "Muse Code Everyday Usage"), `as_of` (epoch seconds)
- Session window: `window_weighted_used` / `window_weighted_limit`,
  `window_resets_at`
- Weekly window: `weekly_weighted_used` / `weekly_weighted_limit`,
  `weekly_resets_at`
- Percent = used / limit per window (verified against dashboard bars).
- The same payload lists `available_models` (muse-spark-1.3/-1.2/-1.1 and
  `-contributor` variants, muse-voice-transcribe-1.0, muse-image-1.0).

## Replay findings (bisect probe, live account)

- Byte-exact replay returns HTTP 200 with freshly computed numbers
  (`as_of` advances, `*_used` climbs with real usage): nothing cached.
- Envelope is trimmable (earlier group-bisect 408s were a coarse-group
  artifact): removing `lsd`, `__dyn`, `__csr`, or `__hblp` individually
  still returns quota. `fb_dtsg` is essential. Either sess cookie alone
  suffices; removing both fails. Single-param elimination for the rest
  is still open.
- Open: shelf life of page-load tokens (`fb_dtsg`, `__s`, `__spin_*`).
  Experiment running: replay the same capture daily until it breaks;
  that number decides whether quota meters are a feature or a
  party trick. `doc_id` was stable across captures days apart,
  including across a fresh login.

## Alternative: CDP sidecar (proposed, feasibility established)

Instead of recreating the envelope, drive a real browser: dedicated
persistent Chrome profile (one interactive login), attach over CDP,
reload the usage page, read the `LLMDCUsageQuery` response with
`Network.getResponseBody`, extract only
`data.team.subscription_quota_usage`. Verified working against a live
session. Tradeoffs: no envelope maintenance and immune to `doc_id`
rotation, but inherits a Chrome process (memory, lifecycle, headless
reliability vs Meta session policy) that neither codebase currently
wants as a dependency. Best viewed as a local experimental collector,
not a shippable provider dependency.

## Client (implemented, experimental — 2026-09-08)

`internal/providers/muse_code/quota.go` enriches the local-spend snapshot,
non-fatally, following the opencode console-enrichment pattern:

- Cookie refresh via the existing `shared.LoadOrRefreshBrowserSession`
  infra (`llm_sess`, `ecto_1_sess` fallback, `dev.meta.ai`). Passive read
  from the user's everyday browser every poll — no standing Chrome process.
  Firefox/Safari read without an OS keychain prompt; Chrome users pick it in
  the TUI browser picker (persisted per account).
- `team_id` from the account's `team_id` path; volatile page-load params
  (`fb_dtsg`, `lsd`, `doc_id`, …) from the account's `quota_tokens_file`
  (user-managed JSON, chmod 600 — values never enter settings).
- Exact-envelope POST, meters `muse.session` / `muse.weekly` (Used/Limit
  with Resets) above the local spend, tier/as-of/model-count attributes.
- Degradation: any auth-shaped failure (no cookie, HTTP 401/403, GraphQL
  errors, empty payload) records a `muse_quota_auth` diagnostic reading
  "quota … — visit https://dev.meta.ai in Chrome, then re-poll". Local
  spend meters always stand.

No periodic Chrome runner: merely launching Chrome refreshes nothing — a
page visit is what renews cookies and page tokens, and the cookie half is
already free via the passive re-read. If token shelf life (still measured
by the daily replay) ever justifies automation, the honest shape is a
harvest-on-degradation page visit, not a schedule. Expect maintainer
pushback: undocumented route.
