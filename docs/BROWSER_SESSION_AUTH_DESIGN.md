# Browser-Session Auth: the Universal Solution for Dashboard-Gated Providers

Date: 2026-04-30
Status: Proposed
Author: Jan Baraniewski

Originally driven by issues #79 (Perplexity) and #80 (OpenCode / OpenAI-via-OpenCode-OAuth fallout). Live probing (2026-04-30) confirmed this isn't just an OpenCode/Perplexity quirk — **every modern AI-platform console hides usage / billing / account data behind session-cookie auth, and rejects OAuth tokens explicitly**. OpenAI's billing endpoint literally says so:

> `403: must be made with a session key (browser-only). You made it with: oauth.`

This same pattern was confirmed against `platform.openai.com`, `chatgpt.com`, `console.anthropic.com`, `aistudio.google.com`, `console.opencode.ai`, and `console.perplexity.ai`. All six return 403 / 302-to-login for unauthenticated requests on their dashboard API surface. All six work with a valid session cookie. **Cookie auth is the universal mechanism for full-data parity** across providers — not a workaround for two outliers.

The user has rejected manual cookie-paste UX as "hacky / not secure". This doc designs the alternative.

## Why OAuth doesn't substitute (verified)

OAuth tokens are *delegated* credentials — designed for third-party apps and intentionally scoped to a narrow surface (usually `chat.completions`). Probes against fresh, non-expired tokens issued by OpenCode for OpenAI / Anthropic / Google all confirmed:

- **OpenAI** OAuth (audience-claimed for `/v1`): 403 on `/v1/models` ("Missing scopes: api.model.read"), 401 on `/v1/usage`, 403 on `/v1/dashboard/billing/credit_grants` ("must be made with a session key").
- **Anthropic** OAuth: 401 on `/v1/messages` ("OAuth authentication is currently not supported"). Even with Claude-Code-style `anthropic-beta: oauth-2025-04-20` header.
- **Google** access token: 401 on `generativelanguage.googleapis.com` ("Expected OAuth 2 access token … or other valid authentication credential"). The token is opaque, OpenCode-internal.

Session cookies, by contrast, carry **the full identity of the logged-in user** with all the permissions they have in the dashboard. Cookie-authed requests can hit every endpoint the user reaches by clicking through the UI.

## 1. Problem Statement

Every major AI-platform console exposes rich data (balance, monthly usage, tier, subscription, per-model spend, organization metadata, payment method, rate-limit caps) **only behind session-cookie auth**. API keys are deliberately scoped to chat-completion / inference routes; OAuth tokens are delegated and similarly scoped. The data we need to populate full-feature provider tiles is simply not reachable from any non-browser credential.

The session cookie itself is set server-side after the user's OAuth dance with Google/GitHub/SSO, and is encrypted with a server-only key. Openusage cannot mint it. Only the user's browser can.

We need a way to get the cookie from the user's existing logged-in browser into openusage **without** asking the user to copy/paste it.

## 2. Goals

1. Zero copy-paste UX. The user clicks one thing and is done.
2. Works on macOS / Linux / Windows.
3. Works for Chrome / Safari / Firefox (the dominant ~95% of browsers).
4. Cookie storage in openusage is encrypted-at-rest (Keychain / libsecret / DPAPI, NOT plain JSON).
5. Auth refresh story is honest: when the cookie expires, the tile transitions to AUTH with a clear "log into provider.com to refresh" hint and re-extracts on next poll.
6. **Universal — one infrastructure, every dashboard-gated provider benefits.** Cover at minimum: OpenAI (platform + ChatGPT), Anthropic (console), Google AI Studio, OpenCode (Zen), Perplexity. Cursor already has equivalent local-extraction; same pattern.
7. Clear, explicit user consent — first time openusage reads a browser cookie, the user is prompted and informed, not surprised.
8. **Per-provider declaration is minimal.** A provider opts in by declaring `(domain, cookie_name)` in its `ProviderSpec` and writing an API client. The cookie plumbing stays generic.

## 3. Non-Goals

1. **No bundled headless browser.** Adding a Chromium dependency would balloon the binary by ~100MB and bring fragility (UI changes, headless-detection bot challenges). The user already has a browser; we use theirs.
2. **No browser extension.** Friction (install in N browsers) and maintenance overhead (review cycles per browser store).
3. **No openusage-hosted OAuth proxy.** Operational cost, trust implications. We don't want to sit in the middle of users' auth flows.
4. **No replacing existing API-key auth.** Where a provider's API key already gives all the data we need (Moonshot, OpenAI, etc.), we don't add cookie auth. This is purely additive for providers where the API key is data-poor.
5. **No automatic browser-cookie extraction without user opt-in.** Reading another app's data is sensitive — gated on explicit consent in the TUI, ~~not~~ never on by default.
6. **No CSRF-token tracking for mutating endpoints.** We only read (`GET`-style RPCs). If a provider requires CSRF for reads (rare), we revisit.

## 4. Impact Analysis

### Affected Subsystems

| Subsystem | Impact | Summary |
|-----------|--------|---------|
| core types | minor | New `ProviderAuthTypeBrowserSession` constant; new `BrowserCookieRef` field on `AccountConfig` for the persisted reference. |
| providers | moderate | OpenCode + Perplexity gain a cookie-fed code path alongside their existing API-key probe. Other providers untouched. |
| TUI | moderate | New row type in 5 KEYS for cookie-auth providers, "Connect via browser" action, refresh flow on expiry. |
| config | minor | Cookie blob stored in the existing credentials store with a new `kind: "browser_session"` discriminator. Same encryption-at-rest as API keys. |
| detect | minor | Optional: detect "user is logged into provider X in browser Y" passively for the UI hint, not for auto-extraction. |
| daemon | none | The daemon poll path stays unchanged — it consumes whatever credential the provider hands it. |
| telemetry | none | |
| CLI | none | |
| Dependencies | adds one | `github.com/browserutils/kooky` (cross-platform browser cookie reader) — battle-tested, used by yt-dlp et al., handles Chrome encryption / Safari binarycookies / Firefox SQLite. ~250KB compiled. Apache-2.0. |

### Existing Design Doc Overlap

- `docs/TELEMETRY_INTEGRATIONS.md` — unrelated; this is provider auth, not telemetry.
- No active design docs overlap.

## 5. Detailed Design

### 5.1 Cookie acquisition: how and from where

We use **`kooky`** (or roll our own thin equivalent if the dep is rejected). It abstracts over:

- **Chrome / Edge / Brave / Vivaldi** — SQLite cookie DB at platform-specific paths. Values are AES-128-CBC encrypted on Linux/macOS (key from libsecret / Keychain) or DPAPI on Windows. Chrome v20+ App-Bound Encryption is *not* yet defeated by kooky on Windows; we accept that limitation and document it (users on Windows + Chrome v20+ can fall back to Firefox or Edge for the cookie source).
- **Firefox** — plain SQLite, no decryption needed.
- **Safari** — `Cookies.binarycookies` plist format, Apple's binary spec.

Per provider account config, we record:
- `BrowserCookieRef.Domain` — e.g. `.opencode.ai`
- `BrowserCookieRef.CookieName` — e.g. `auth`
- `BrowserCookieRef.SourceBrowser` — auto-detected on connect, persisted

On every poll, openusage re-reads the cookie fresh from the source browser. If extraction fails (browser DB locked because browser is open, key unavailable, etc.) we fall back to the **last successfully-extracted cookie** stored in our credentials store — which has a known expiry, beyond which we transition the tile to AUTH.

### 5.2 The "Connect via browser" flow

In **Settings → 5 KEYS** for cookie-auth-capable providers, the row shows:

```
  ▸ opencode-zen     │ STATUS │ <not connected>
                       press Enter to connect via browser
```

On Enter:

1. TUI displays a modal:
   ```
   ┌── Connect OpenCode (cookie auth) ──────────────────────────┐
   │ openusage will read your opencode.ai session cookie from   │
   │ your browser to fetch billing and usage data.              │
   │                                                            │
   │ This requires you to be logged into opencode.ai in one of: │
   │   • Google Chrome / Edge / Brave / Vivaldi                 │
   │   • Firefox                                                │
   │   • Safari (macOS only)                                    │
   │                                                            │
   │ The cookie is stored encrypted at rest in your             │
   │ openusage credentials store. It's read fresh from the      │
   │ browser on every poll.                                     │
   │                                                            │
   │   y  open opencode.ai in your default browser              │
   │   r  read cookie now (already logged in)                   │
   │   esc cancel                                               │
   └────────────────────────────────────────────────────────────┘
   ```

2. User picks `r` (already logged in) — openusage tries each supported browser in turn for `.opencode.ai/auth`. First hit wins, gets stored, tile flips to OK.

3. User picks `y` — openusage `exec.Command("open", "https://opencode.ai/login")` (and platform equivalents), modal shows "Waiting for you to finish logging in… [r] read now [esc] cancel". User logs in, returns to TUI, presses `r`. Same extraction as path (2).

4. **No copy-paste.** No "open DevTools and copy". The user only ever logs into the provider's site like normal.

### 5.3 Cookie storage

Two artifacts:

**Per-account reference** (in the account's config — non-sensitive):
```json
{
  "id": "opencode-zen",
  "provider": "opencode",
  "auth": "browser_session",
  "browser_cookie_ref": {
    "domain": ".opencode.ai",
    "cookie_name": "auth",
    "source_browser": "chrome"
  }
}
```
Persists in `settings.json` like any other account config.

**Cookie value** (in the credentials store — sensitive):
- Existing credentials store gains a `kind` field: `"api_key"` (default) or `"browser_session"`.
- For `browser_session`, the value is the cookie blob plus `expiry`, `last_extracted_at`, `source_browser`.
- Storage encryption-at-rest stays as it is today (the credentials store already uses keychain on macOS / libsecret / DPAPI per `internal/credentials`). New entries use the same path.

### 5.4 Provider integration pattern

Add `ProviderAuthTypeBrowserSession` to the `core.ProviderAuthSpec` enum. Provider declares which auth types it supports:

```go
Auth: core.ProviderAuthSpec{
    Type:                core.ProviderAuthTypeAPIKey,            // primary
    APIKeyEnv:           "OPENCODE_API_KEY",
    DefaultAccountID:    "opencode",
    SupplementalTypes:   []core.ProviderAuthType{core.ProviderAuthTypeBrowserSession},
    BrowserCookieDomain: ".opencode.ai",
    BrowserCookieName:   "auth",
},
```

The provider's `Fetch()` accepts both: if the account has a usable cookie blob, it makes the cookie-authed RPC calls; otherwise it falls back to API-key-only data. **The merge happens inside `Fetch()`** — no architectural changes needed in the daemon or read-model layers.

### 5.5 Cookie expiry & refresh

Cookies have explicit `Expires`. Openusage:

1. Tracks the expiry alongside the cookie blob.
2. **On every poll**, before making the RPC, re-extracts from the browser. If the fresh extract is newer (longer expiry, different value), it replaces the stored blob.
3. If the cookie has expired AND extraction returns nothing newer, the tile transitions to AUTH with message "session expired — re-login at opencode.ai". The user logs in to the provider in their browser; next poll extracts the fresh cookie and the tile flips back to OK.

This is graceful and doesn't require any TUI interaction during the common refresh flow.

### 5.6 Privacy / consent boundary

Reading another app's data is touchy. Mitigations:

1. **Off by default.** Cookie auth is opt-in per-account. The TUI flow above is the only place it gets enabled.
2. **Scoped by domain.** We only ever ask kooky for `(domain, cookie_name)` — never enumerate cookies, never read other domains.
3. **First-extraction OS prompt.** On macOS, the first read of Chrome's keychain entry triggers a system dialog ("openusage wants to access Chrome Safe Storage") — the user explicitly approves at the OS level. We don't suppress this; it's the right confirmation.
4. **Local-only.** The cookie blob never leaves the user's machine. No outbound network calls except to the provider itself.
5. **Documented in README.** The README provider table will note "cookie auth (read from browser)" so it's not hidden.

### 5.7 Failure modes & how the tile reflects them

| Situation | Tile state | Message |
|---|---|---|
| Cookie not configured | normal API-key state (no degradation) | API-key auth only — connect a browser session for billing data |
| Cookie present, extraction OK, RPC OK | OK | (provider-specific message) |
| Cookie present, extraction OK, RPC 401 | AUTH | session invalid — re-login at provider.com |
| Cookie present, extraction failed (browser DB locked) | LAST_KNOWN | extraction failed: browser may be open. Retrying. |
| Cookie expired AND no fresh one in browser | AUTH | session expired — re-login at provider.com |
| User on Windows Chrome v20+ (App-Bound Enc.) | UNSUPPORTED | App-Bound Encryption blocks reads. Use Firefox / Edge for this provider. |

### 5.N Backward Compatibility

- Pure additive: existing API-key providers stay as they are. New `ProviderAuthTypeBrowserSession` only opts in providers that declare it.
- Existing credentials store gains a new `kind` field. Older entries default to `"api_key"`. No migration needed.
- New `kooky` dependency is the only new import.

## 6. Alternatives Considered

### A: Bundled headless browser (Playwright / Chromedp)

Spawn a controlled Chromium that drives the OAuth flow start to finish, then exfiltrates the cookie via DevTools Protocol. Rejected:
- Bundle size: ~100MB Chrome + Playwright ~200MB.
- Brittleness: provider UI changes break automation.
- User experience: a chrome window opens for a few seconds, feels weird.
- Bot detection: Cloudflare Turnstile and similar may block headless flows.

### B: Browser extension companion

A tiny extension that listens for relevant logins and posts the cookie to a localhost socket. Rejected:
- Install friction (Chrome Web Store + Firefox Add-ons + Safari Extension separately).
- Review-cycle overhead for cross-store updates.
- Users dislike installing extensions for "trust" reasons.

### C: Hosted OAuth proxy

Openusage runs a backend that initiates OAuth on the user's behalf, captures the callback, and returns the session. Rejected:
- We don't operate services and don't want to.
- Trust posture (we sit in the middle of every auth flow). Bad look.
- Single point of failure for the dashboard.

### D: Reverse-proxy interception

Spawn a local HTTPS proxy with a CA cert the user trusts, intercept the cookie on the next provider login. Rejected:
- Asking users to install a CA cert is a serious security ask.
- Browser HSTS pinning blocks this for many providers.
- Opens a wider attack surface than necessary.

### E: Wait for upstream PATs / bearer-token support

File issues with OpenCode and Perplexity asking for PATs. Track. Don't gate on it.

This is **complementary**, not an alternative. We file the issues regardless. If they ship PATs, we replace cookie auth with PATs and the cookie code becomes dead.

### F: Manual cookie paste

User's stated NO. Documented for completeness only — would have been the simplest implementation but isn't acceptable UX.

## 7. Implementation Tasks

### Task 1: core types + auth spec extension
Files: `internal/core/provider.go`, `internal/core/provider_spec.go`, tests
Depends on: none
Description: Add `ProviderAuthTypeBrowserSession`, `BrowserCookieRef` struct, `SupplementalTypes`/`BrowserCookieDomain`/`BrowserCookieName` on `ProviderAuthSpec`. Backward-compatible defaults.
Tests: marshalling of new fields, default value semantics.

### Task 2: cookie extractor abstraction
Files: `internal/browsercookies/cookies.go`, `internal/browsercookies/cookies_test.go`
Depends on: Task 1
Description: Thin wrapper over `github.com/browserutils/kooky`. Exposes `ReadCookie(ctx, domain, name) (BrowserCookie, error)` + `ListSourceBrowsers() []string`. Sets a strict timeout (10s) so a slow keychain prompt doesn't block the daemon.
Tests: mock kooky-like backend, success / not-found / timeout / multi-browser preference order.

### Task 3: credentials store extension
Files: `internal/credentials/store.go`, `internal/credentials/store_test.go`
Depends on: Task 1
Description: `kind` field on stored entries. `kind=browser_session` entries persist `value`, `expiry`, `last_extracted_at`, `source_browser`. Migration: existing entries default `kind=api_key`. Encryption-at-rest unchanged.
Tests: round-trip with new fields; legacy load.

### Task 4: TUI 5 KEYS extensions
Files: `internal/tui/settings_modal_input.go`, `internal/tui/settings_modal_preferences.go`, `internal/tui/settings_modal_layout.go`, new `internal/tui/browser_session_picker.go`, tests
Depends on: Tasks 2, 3
Description: For accounts with `SupplementalTypes` containing `browser_session`: a new sub-state for connect modal (the "y / r / esc" picker), a `connectBrowserSessionCmd` that calls into the extractor, status updates on the row.
Tests: modal open / read action / cancel / extraction failure handling.

### Task 5: OpenCode provider integration
Files: `internal/providers/opencode/console_rpc.go` (new), `internal/providers/opencode/seroval.go` (new), `internal/providers/opencode/provider.go` (extend Fetch), tests + fixtures
Depends on: Tasks 1, 2, 3
Description: Mini Seroval parser (~150 LOC), thin RPC client that POSTs to `/_server` with the cookie + `x-server-id`, four pinned action IDs (billing.get, queryUsage, queryUsageMonth, queryKeys) with comments dating them. Map results into existing tile metric keys: `balance`, `monthly_usage`, `monthly_limit`, `payment_method_last4`, `subscription_plan`, etc.
Tests: Seroval parser round-trips for our concrete fixtures from the captured HAR; extractor injection so tests don't touch a real browser; tile metrics populated correctly with both cookies-only and api-key-only paths; cookie-expired transitions to AUTH.

### Task 6: docs + README
Files: `README.md`, `docs/providers.md`, `docs/BROWSER_SESSION_AUTH_DESIGN.md` (this), `configs/example_settings.json`
Depends on: Task 5
Description: Document opt-in cookie auth, supported browsers, the privacy posture, failure modes. Add a row in providers.md for OpenCode noting "cookie auth available for billing data".

### Task 7: Perplexity provider integration (separate PR)
Files: `internal/providers/perplexity/...`
Depends on: Tasks 1–4
Description: New provider package that uses the same browser-session machinery against Perplexity's `/rest/pplx-api/v2/groups/...` endpoints.

### Task 8: OpenAI provider browser-session enrichment (separate PR)
Files: `internal/providers/openai/console_client.go` (new), provider extension
Depends on: Tasks 1–4
Description: Closes the issue #80 OpenAI gap. Adds session-cookie-fed RPCs against `platform.openai.com` and `chatgpt.com` to surface usage / billing / per-model breakdown / Plus-or-Team subscription state. Existing API-key probe stays as a separate code path. Pinned cookie name(s): `__Secure-next-auth.session-token` and equivalents.

### Task 9: Anthropic provider browser-session enrichment (separate PR)
Files: `internal/providers/anthropic/console_client.go` (new), provider extension
Depends on: Tasks 1–4
Description: Adds `console.anthropic.com` session-cookie-fed RPCs for organization usage / billing / per-model spend.

### Task 10: Google AI Studio provider browser-session enrichment (separate PR)
Files: `internal/providers/gemini_api/console_client.go` (new) or new `internal/providers/google_ai_studio/`
Depends on: Tasks 1–4
Description: Adds `aistudio.google.com` session-cookie-fed RPCs for free-tier quota state and any billing data exposed there.

### Dependency Graph

```
Task 1 ──┐
         ├─→ Task 2 ─┐
         └─→ Task 3 ─┼─→ Task 4 ──┐
                     └─→ Task 5 ──┴─→ Task 6
                                   ↘
                                    Tasks 7, 8, 9, 10 (parallel, separate PRs)
```

Tasks 7–10 are separate downstream provider PRs, all riding on the shared infrastructure from Tasks 1–4. Each is small (one HAR + one provider package + one parser).

## 8. Open Questions

1. **Chrome App-Bound Encryption on Windows v20+.** Is a workable path. Worth confirming kooky's current state before committing; if dead, document as "use Firefox/Edge on Windows for cookie source".
2. **Should the cookie ref store the source browser or auto-rediscover each poll?** Storing it is faster; rediscovering is more resilient if the user switches browsers. Default to storing with a "rediscover if not found" fallback.
3. **What if the user has multiple Chrome profiles?** kooky reads the default profile. v1 limitation; document.
4. **Do we want to expire cookies proactively or lazily?** Lazy (on next poll) is simpler; proactive (background timer) refreshes faster after a re-login. Lazy for v1.
