---
title: Perplexity
description: Track Perplexity Pro/Max usage in OpenUsage via browser-session auth.
sidebar_label: Perplexity
---

# Perplexity

Tracks Perplexity Pro and Max usage by reading the user's browser session against `console.perplexity.ai`. The Perplexity API key surface is intentionally narrow — usage, subscription, and plan data live behind the dashboard, which only accepts session-cookie auth. OpenUsage closes that gap with its **browser-session auth** mechanism.

:::warning Experimental
Perplexity uses browser-session auth, which reads cookies from your locally-installed browser. This is an opt-in feature and requires explicit consent in the TUI on first connect. See the [browser-session auth design](https://github.com/janekbaraniewski/openusage/blob/main/docs/BROWSER_SESSION_AUTH_DESIGN.md) for the full rationale and threat model.
:::

## At a glance

- **Provider ID** — `perplexity`
- **Detection** — opt-in via Settings; not auto-detected from environment variables
- **Auth** — browser session cookie (read from Chrome / Edge / Brave / Vivaldi / Firefox / Safari)
- **Type** — API platform (dashboard-scraped)
- **Tracks**:
  - API org and usage tier
  - Available balance, pending balance, lifetime spend (USD)
  - Auto-top-up amount and threshold
  - Account email, country, payment method (brand + last 4)
  - Past-30-day rollups: API requests, input/output/citation/reasoning tokens, search queries, Pro Search count
  - Auth status

## Setup

Perplexity does not expose usage data through API keys, and OAuth tokens are similarly scoped to inference endpoints. The only credential that can read the dashboard surface is the session cookie set when you log into `perplexity.ai` in your browser. OpenUsage's browser-session auth flow lets you connect without any copy-paste.

### One-time connect

1. Open the OpenUsage TUI and press <kbd>,</kbd> to enter Settings.
2. Switch to the **API Keys** tab (<kbd>5</kbd>).
3. Find the Perplexity row and press <kbd>Enter</kbd>. The row reads:

   ```
     ▸ perplexity     │ STATUS │ <not connected>
                        press Enter to connect via browser
   ```

4. A modal asks for explicit consent. You'll see two paths:
   - **`r` — read cookie now (already logged in).** OpenUsage looks for a `perplexity.ai` session cookie in each supported browser in turn and uses the first one it finds.
   - **`y` — open perplexity.ai in your default browser.** Useful if you're not yet logged in. Log in, return to the TUI, then press <kbd>r</kbd>.

5. On macOS the first read of Chrome's cookie store triggers a Keychain prompt ("openusage wants to access Chrome Safe Storage") — approve it. The cookie is then stored encrypted in the OpenUsage credentials store (Keychain on macOS, libsecret on Linux, DPAPI on Windows). It is never written to disk in plain text.

6. On every poll, OpenUsage re-extracts the cookie from the source browser. If the fresh value is newer (different value, longer expiry), it replaces the stored copy.

### Manual configuration

Browser-session accounts persist their **cookie reference** (which browser, which domain, which cookie name) in `settings.json`, but not the cookie value itself. Manual entries usually aren't needed — the connect flow writes everything for you — but the schema looks like this:

```json
{
  "accounts": [
    {
      "id": "perplexity",
      "provider": "perplexity",
      "auth": "browser_session",
      "browser_cookie_ref": {
        "domain": ".perplexity.ai",
        "cookie_name": "__Secure-next-auth.session-token",
        "source_browser": "chrome"
      }
    }
  ]
}
```

`source_browser` is auto-detected on connect. Leave it blank to let OpenUsage rediscover the cookie if you switch browsers.

## Data sources & how each metric is computed

Perplexity is a **browser-session-only** provider. There is no API-key fallback — the public API is purely chat-completion and exposes no `/usage` or `/credits` endpoint. All visible metrics come from the same dashboard-internal endpoints `console.perplexity.ai` calls when you open the Usage page in your browser.

Each poll (default every 30 seconds in daemon mode) makes up to three calls. All requests carry the session cookie and the trio of `x-app-*` headers the SPA sets:

| Call | Endpoint | What it provides |
|---|---|---|
| 1 | `GET /rest/pplx-api/v2/groups` | List of API orgs you have access to + tier metadata |
| 2 | `GET /rest/pplx-api/v2/groups/<orgID>` | Customer info: balance, pending balance, total spend, payment method, top-up rules |
| 3 | `GET /rest/pplx-api/v2/groups/<orgID>/usage-analytics?time_bucket=day&time_range=past_month` | Meter-event time-series: requests, input/output/citation/reasoning tokens, search queries |

Auth header for every call: `Cookie: __Secure-next-auth.session-token=<value>`. The cookie is read locally from the browser's encrypted store on each poll, so a fresh login is picked up automatically without restart.

### Org selection

- Source: `groups` list response. Each entry has `api_org_id`, `display_name`, `is_default_org`, `runtime_settings.usage_tier`, `user_role`.
- Transform: the default org wins unless `extra.perplexity_org_id` overrides it. The chosen org's `display_name` becomes `Attributes["org_display_name"]`; its `usage_tier` becomes both an `Attributes["usage_tier"]` and a `Metrics["usage_tier"]` (unit `tier`, used for the tile's tier badge).

### `available_balance` — current cycle balance

- Source: `customerInfo.balance` on the org-detail response.
- Transform: stored as `Remaining` in USD. The status message becomes `$X.XX balance · Tier <N>`.

### `pending_balance`, `total_spend`

- Source: `customerInfo.pending_balance`, `customerInfo.spend.total_spend` on the same response.
- Transform: copied verbatim. Pending balance is what's been charged but not yet posted; total spend is lifetime.

### `auto_top_up_amount` / `auto_top_up_threshold`

- Source: `customerInfo.auto_top_up_amount`, `customerInfo.auto_top_up_threshold`.
- Transform: each becomes a `Limit` metric (USD). Only emitted when the corresponding value is &gt; 0.

### Account email, country, payment method

- Source: `customerInfo.contact_info.{email, country}`, `defaultPaymentMethodCard.{brand, last_digits}`.
- Transform: stored as `Attributes["account_email"]`, `account_country`, `payment_method_last4`, `payment_method_brand`.

### `requests_window`, `input_tokens_window`, `output_tokens_window`, `citation_tokens_window`, `reasoning_tokens_window`, `search_queries_window`, `pro_search_window`

- Source: usage-analytics meter-event summaries. Each meter has a `name` (e.g. `api_requests`, `input_tokens`, `output_tokens`, `citation_tokens`, `reasoning_tokens`, `num_search_queries` / `search_request_count`, `pro_search_request_count`) and an array of `meter_event_summaries` with per-day `value`.
- Transform: for each known meter the values are summed across the past-month window and stored under the matching `*_window` metric (window label `30d`, unit `requests` / `tokens` / `queries`). Meters whose total is zero are omitted.

### Auth status

- Source: HTTP status from any of the three calls. `401`/`403` becomes `auth` with the message `session expired — re-login at console.perplexity.ai`. With no session configured the snapshot is `auth` with `browser session not configured — Settings → 5 KEYS → perplexity → Enter`. Otherwise `ok`.

### What's NOT tracked

- **Native API spend.** The public chat-completion API doesn't expose any usage data; everything you see comes from the dashboard surface, which only authenticates against a logged-in session.
- **Multi-org balance aggregation.** Only the chosen org is read per poll. Configure separate accounts (different `extra.perplexity_org_id`) to track multiple orgs.

### How fresh is the data?

- Polled every 30 s by default. The cookie is re-read from the browser store each poll, so a freshly-renewed session is picked up on the next cycle without any restart.

## API endpoints used

All under `https://console.perplexity.ai` (cookie-authed):

- `GET /rest/pplx-api/v2/groups`
- `GET /rest/pplx-api/v2/groups/<orgID>`
- `GET /rest/pplx-api/v2/groups/<orgID>/usage-analytics?time_bucket=day&time_range=past_month`

The cookie itself is read locally from the user's browser cookie store; no network call to Perplexity is made to obtain it.

## Caveats

:::note
Perplexity does not currently offer personal access tokens (PATs) or any non-cookie credential that exposes dashboard data. We've filed an upstream issue requesting one; if PATs ship, OpenUsage will switch and the cookie path will become dead code.
:::

- **Dashboard endpoints are not stable.** Perplexity's dashboard API is internal to the website and can change at any time. OpenUsage pins each request shape and surfaces a clear error if a response stops parsing — but expect occasional breakage as the dashboard evolves.
- **Cookie expiry is real.** Perplexity sessions expire after a few weeks. When they do, the tile flips to AUTH with a "session expired — re-login at perplexity.ai" message. Logging back in via your browser is enough; the next poll picks up the new cookie automatically.
- **Browser must be installed and logged in.** OpenUsage cannot mint a cookie. You need a working browser session on the same machine.
- **Windows Chrome v20+ App-Bound Encryption** blocks the cookie read. On affected systems, use Firefox or Edge as the cookie source until upstream support lands.
- **Multiple Chrome profiles.** OpenUsage reads the default profile in v1. If your Perplexity session lives in a non-default profile, log into the default profile too — or use a different browser.
- **API spend is in USD; consumer Pro/Max plans are not.** This provider reads the API console (`console.perplexity.ai`), which exposes per-org balance and metered spend. Personal Pro/Max subscription plans are billed flat-rate by Perplexity and are not surfaced here.

## Troubleshooting

- **"No browser session found"** — make sure you're logged into `perplexity.ai` in one of the supported browsers (Chrome / Edge / Brave / Vivaldi / Firefox, plus Safari on macOS), then press <kbd>r</kbd> in the connect modal.
- **"Session expired — re-login at perplexity.ai"** — log into Perplexity again in your browser. Next poll re-extracts the fresh cookie.
- **"Extraction failed: browser may be open"** — Chrome holds an exclusive lock on its cookie DB while running. Close Chrome briefly, or wait for the lock to release. OpenUsage falls back to the last successfully-extracted cookie until then.
- **"App-Bound Encryption blocks reads"** (Windows) — switch the cookie source to Firefox or Edge.
- **Tile shows quotas that don't match the dashboard** — the dashboard endpoint may have changed shape. Run with `OPENUSAGE_DEBUG=1` and file an issue with the log.

### Why does the tile stop working after a few weeks?

Perplexity sets a relatively short session-cookie expiry. When your console session expires the tile transitions to `auth` with a "session expired" message. Logging back in at `console.perplexity.ai` from the same browser is enough — the next poll re-extracts the new cookie automatically. There's no need to re-run the connect flow.

### Why do I see no usage data on a fresh account?

The `usage-analytics` endpoint returns empty meter arrays until the org has activity. Balance and tier still populate from the org-detail call. Make a few API requests and the rollups appear on the next poll.

### Can I track my Pro / Max consumer subscription this way?

No — this provider talks to the API console only. Consumer Pro / Max plans bill flat-rate and have no per-account spend surface OpenUsage can read.

## Related

- [Browser-session auth design](https://github.com/janekbaraniewski/openusage/blob/main/docs/BROWSER_SESSION_AUTH_DESIGN.md) — the universal cookie-auth mechanism shared with OpenAI, Anthropic, Google AI Studio, and OpenCode console scrapes
- [OpenCode](./opencode.md) — sibling provider that uses the same browser-session machinery for `console.opencode.ai`
