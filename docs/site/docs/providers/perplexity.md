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
  - Subscription plan (Pro, Max, Enterprise) and renewal date
  - Monthly query usage against the plan quota
  - Pro Search usage and remaining count
  - Reasoning / research mode quotas where exposed
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

## What you'll see

- Dashboard tile shows the subscription plan and the most-constrained quota gauge (typically Pro Search remaining for the current cycle).
- Detail view breaks down per-feature usage: standard queries, Pro Search, reasoning mode, and any quotas the dashboard exposes.
- Plan renewal date is shown alongside the cycle window.
- When the cookie is missing or expired, the tile transitions to the AUTH state with a hint to re-login at `perplexity.ai`.

## API endpoints used

All under `https://www.perplexity.ai` and `https://console.perplexity.ai` (cookie-authed):

- `GET /rest/pplx-api/v2/groups/<group>/usage` — per-group usage counters
- `GET /rest/pplx-api/v2/groups/<group>/subscription` — plan and billing window
- Endpoints are dashboard-internal and may change without notice — see Caveats.

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
- **No spend in dollars.** Perplexity's plan model is flat-rate with quotas, not metered. The tile shows quota usage, not currency.

## Troubleshooting

- **"No browser session found"** — make sure you're logged into `perplexity.ai` in one of the supported browsers (Chrome / Edge / Brave / Vivaldi / Firefox, plus Safari on macOS), then press <kbd>r</kbd> in the connect modal.
- **"Session expired — re-login at perplexity.ai"** — log into Perplexity again in your browser. Next poll re-extracts the fresh cookie.
- **"Extraction failed: browser may be open"** — Chrome holds an exclusive lock on its cookie DB while running. Close Chrome briefly, or wait for the lock to release. OpenUsage falls back to the last successfully-extracted cookie until then.
- **"App-Bound Encryption blocks reads"** (Windows) — switch the cookie source to Firefox or Edge.
- **Tile shows quotas that don't match the dashboard** — the dashboard endpoint may have changed shape. Run with `OPENUSAGE_DEBUG=1` and file an issue with the log.

## Related

- [Browser-session auth design](https://github.com/janekbaraniewski/openusage/blob/main/docs/BROWSER_SESSION_AUTH_DESIGN.md) — the universal cookie-auth mechanism shared with OpenAI, Anthropic, Google AI Studio, and OpenCode console scrapes
- [OpenCode](./opencode.md) — sibling provider that uses the same browser-session machinery for `console.opencode.ai`
