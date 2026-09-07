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
- All-or-nothing envelope: dropping even two ad-telemetry params turns the
  call into a 60s hang (HTTP 408). There is no minimal subset; the client
  must replay the full ~28-param set plus full headers.
- Open: shelf life of page-load tokens (`fb_dtsg`, `__s`, `__dyn`,
  `__spin_*`). Experiment running: replay the same capture daily until it
  breaks; that number decides whether quota meters are a feature or a
  party trick. `doc_id` was stable across captures days apart.

## Client sketch (when shelf life justifies it)

New fetch path in `internal/providers/muse_code`: cookie refresh via the
existing `shared.LoadOrRefreshBrowserSession` infra (`dev.meta.ai` cookie
ref), `team_id` from account config, exact-envelope POST, meters
`muse.session` / `muse.weekly` (`.percent` with Limit/ResetsAt) above the
local spend. Degrade to auth-required when page tokens die; the browser
revisit repairs it. Expect maintainer pushback: undocumented route.
