# Providers

OpenUsage ships with 17 provider integrations covering coding agents, API platforms, and local tools. All providers are auto-detected when available — no manual config needed.

## Coding agents & IDEs

### Claude Code

**Detection:** `claude` binary + `~/.claude` directory

Tracks daily activity, per-model token usage, 5-hour billing block computation, burn rate, and cost estimation.

![Claude Code provider](../assets/claudecode.png)

### Cursor

**Detection:** `cursor` binary + local SQLite databases

Tracks plan spend and limits, per-model aggregation, Composer sessions, and AI code scoring. Uses a hybrid approach — API endpoints plus local SQLite DB reads.

![Cursor provider](../assets/cursor.png)

### GitHub Copilot

**Detection:** `gh` CLI with Copilot extension installed

Tracks chat and completions quota, org billing, org metrics, and session tracking.

![Copilot provider](../assets/copilot.png)

### Codex CLI

**Detection:** `codex` binary + `~/.codex` directory

Tracks session tokens, per-model and per-client breakdown, credits, and rate limits.

![Codex CLI provider](../assets/codex.png)

### Gemini CLI

**Detection:** `gemini` binary + `~/.gemini` directory

Tracks OAuth status, conversation count, per-model tokens, and quota API data.

![Gemini CLI provider](../assets/gemini.png)

### OpenCode

**Detection:** `OPENCODE_API_KEY` or `ZEN_API_KEY` environment variable

Tracks credits, activity, and generation stats via an OpenRouter-compatible API.

### Ollama

**Detection:** `OLLAMA_HOST` environment variable or `ollama` binary

Tracks local server models, per-model usage, and optional cloud billing.

## API platforms

### OpenRouter

**Detection:** `OPENROUTER_API_KEY` environment variable

Tracks credits, activity, generation stats, and per-model breakdown across multiple API endpoints.

![OpenRouter provider](../assets/openrouter.png)

### OpenAI

**Detection:** `OPENAI_API_KEY` environment variable

Tracks rate limits via lightweight header probing.

### Anthropic

**Detection:** `ANTHROPIC_API_KEY` environment variable

Tracks rate limits via lightweight header probing.

### Groq

**Detection:** `GROQ_API_KEY` environment variable

Tracks rate limits and daily usage windows.

### Mistral AI

**Detection:** `MISTRAL_API_KEY` environment variable

Tracks subscription info and usage endpoints.

### DeepSeek

**Detection:** `DEEPSEEK_API_KEY` environment variable

Tracks rate limits and account balance.

### Browser-session auth (universal mechanism)

For providers whose billing / usage / account data is gated by web-console
session cookies and never exposed via API key, openusage supports a
"connect via browser" flow that reads the session cookie directly out of
your default browser's cookie jar (Chrome / Firefox / Safari / Edge /
Brave on macOS / Linux / Windows).

**How to connect**: Settings → 5 KEYS → navigate to the provider row →
press Enter. Openusage reads the `(domain, cookie name)` pair declared by
the provider, persists it encrypted-at-rest in the credentials store, and
uses it on every poll. When the cookie expires, the tile transitions to
AUTH with a "re-login at console.X.com" hint; logging into the site again
in your browser refreshes openusage on the next poll automatically.

**Privacy**: opt-in per-account, scoped to a single (domain, cookie name)
pair, never sent off-machine. macOS will prompt for Keychain access the
first time openusage reads Chrome's cookie store; that's the OS-level
consent gate.

**Cookie auth currently shipping** (full implementation):
- Perplexity → `console.perplexity.ai` — tier, balance, spend, analytics
- OpenCode → `opencode.ai/_server` — balance, monthly limit, subscription

**Cookie auth in progress** (HAR captured, RPC client needed):
- Google AI Studio → `aistudio.google.com` — per-project quotas (needs
  SAPISIDHASH + MakerSuite tuple decoding; captured 2026-04-30)
- ChatGPT consumer → `chatgpt.com` — Plus/Team plan + message quotas
  (HAR captured but thin; needs re-capture from Settings → Subscription
  pages)

**Cookie auth planned** (no HAR yet — capture and submit a HAR to enable):
- OpenAI Platform → `platform.openai.com` — usage, billing, models
- Anthropic Console → `console.anthropic.com` — org usage, billing
- Mistral Console → `console.mistral.ai` — billing, per-model spend
- Groq Console → `console.groq.com` — usage, billing
- xAI Console → `console.x.ai` — credit balance, usage breakdown
- DeepSeek Platform → `platform.deepseek.com` — extended usage history
- Z.AI Console → `open.bigmodel.cn` — usage detail
- Alibaba Cloud Console → `console.aliyun.com` — DashScope billing

To add one of these: capture a HAR file from your logged-in browser on
the site (covering the Usage / Billing / Account pages), drop at
`~/Downloads/<host>.har`, and we wire up the RPC client + parser.

### OpenCode credential adoption (cross-provider)

If [OpenCode](https://opencode.ai) is installed and you've authed any
of its providers, openusage will read `~/.local/share/opencode/auth.json`
on startup and adopt the API keys it finds. Currently maps:

| OpenCode entry | openusage account |
|---|---|
| `moonshotai` (api) | `moonshot-ai` (provider `moonshot`) |
| `openrouter` (api) | `openrouter` |
| `zai` (api) | `zai` |
| `opencode` (api) | `opencode` |
| `ollama-cloud` (api) | `ollama-cloud` (provider `ollama`) |

OAuth-typed entries (`anthropic`, `openai`, `google`, `cursor`) are skipped:
they're chat-scoped tokens, not the API-key shape openusage's poll-time probes
expect. Env-var detection runs first; if both are present the env var wins.

### Perplexity

**Detection:** browser-session cookie from `console.perplexity.ai` (Settings → 5 KEYS → perplexity → Enter).

Browser-session-auth-only — Perplexity's API key is chat-only. Tile surfaces tier (0–5), available balance, lifetime spend, auto-reload settings, payment method, and 30-day analytics (api_requests, input/output/reasoning tokens, search queries) from the console RPCs at `/rest/pplx-api/v2/groups/<org_id>/...`.

### OpenCode (Zen + Console)

**Detection:** `OPENCODE_API_KEY` / `ZEN_API_KEY` env var for chat-surface auth, optionally a browser-session cookie from `console.opencode.ai` for billing data.

Two-tier auth. The API key probes `/zen/v1/models` for chat-side validation and surfaces the available Zen model count. When connected via browser session (Settings → 5 KEYS → opencode → Enter), the tile gains balance, monthly limit / monthly usage, auto-reload settings, payment method, and subscription state from the SolidStart server-fn endpoints at `opencode.ai/_server`.

Workspace ID required for console enrichment — set `extra_data.opencode_workspace_id` on the account config. Auto-discovery is a follow-up.

### Moonshot (Kimi)

**Detection:** `MOONSHOT_API_KEY` environment variable

Tracks balance breakdown (`available_balance` = `cash_balance` + `voucher_balance`), org-level rate caps (`max_request_per_minute`, `max_token_per_minute`, `max_concurrency`, `max_token_quota`), tier (`user_group_id`), and account metadata (org id, project id, masked access key).

By default targets `api.moonshot.ai` (international, USD). For Moonshot.cn (China, CNY) add a second account in `settings.json` with `"base_url": "https://api.moonshot.cn"`.

### xAI (Grok)

**Detection:** `XAI_API_KEY` environment variable

Tracks rate limits and API key info.

### Z.AI Coding Plan

**Detection:** `ZAI_API_KEY` / `ZHIPUAI_API_KEY` environment variable, or `~/.chelper/config.yaml`

Tracks coding-plan quota limits, model/tool usage, daily trend series, and optional credit balance metadata.

### Google Gemini API

**Detection:** `GEMINI_API_KEY` or `GOOGLE_API_KEY` environment variable

Tracks rate limits and per-model limits.

### Alibaba Cloud

**Detection:** `ALIBABA_CLOUD_API_KEY` environment variable

Tracks quotas, credits, daily usage, and per-model tracking.
