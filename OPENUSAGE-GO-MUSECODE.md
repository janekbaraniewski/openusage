# Muse Code provider — Go TUI/daemon (`janekbaraniewski/openusage`) — maintainer notes

This doc is the handoff for `feat/muse-code-provider` on fork `tomck/openusage-go`.
It covers the local-only spend provider plus the live quota enrichment and the
dashboard clutter fix. The only UX surprise is a **single keychain prompt** — see §4.

## 1. What the provider does

* **Local spend, no API:** `internal/providers/muse_code/muse_code.go:224`
  `populateSnapshot` scans `<data>/muse/sessions/YYYY/MM/DD/<id>/session.jsonl`
  (`~/.local/share/muse/sessions` or `$XDG_DATA_HOME/muse/sessions` via
  `paths.go:resolveSessionsDirs` / `detect/muse_code.go:defaultMuseSessionsDir`)
  for `model_completed` run events (same `recorded_at` micros, `usage:{input_tokens,
  output_tokens, cached_tokens/cache_read_tokens, cache_write_tokens, reasoning_tokens}`,
  `model`; reasoning folds into output). Dedup by canonical path, skip
  `subagent/` transcripts (`isSubagentTranscript`), price via `pricing.Estimate`
  (`pricing_supplement.json` `muse-spark-1.3` `$1.25/M in, $4.25/M out` and
  `muse-spark-1.3-contributor` `$0.10/$0.20`). Sets `total_*`, `muse.session`/
  `muse.weekly` quota meters, `ModelUsage` (`AppendModelUsage`), `DailySeries`
  (`tokens`/`cost`/`sessions`), `total_sessions`/`sessions_today`/`sessions_7d`.

  `HasCredential` checks `META_API_KEY` env or `auth.json` (`MUSE_CONFIG_DIR`/
  `XDG_CONFIG_HOME`/`~/.config/muse/auth.json` or `MUSE_AUTH_PATH`). `HasChanged`
  always polls when credentialed (`muse_code.go:95` — quota drifts without local
  writes, same rationale as `codex`), otherwise scans `*.jsonl` mtimes recursively.

* **Live quota (best-effort, never fails Fetch):** `internal/providers/muse_code/quota.go:154`
  primary `POST https://api.meta.ai/v1/responses` `{model:"muse-spark-1.3-contributor",
  store:false, stream:true, input:"hi"}` `Authorization: Bearer <api_key>`,
  `Accept: text/event-stream`, parses first `event: response.subscription_usage`
  (`subscription:{tier, weekly:{used_percent,resets_at}, window:{used_percent,resets_at,
  window_duration_mins}}`) → `applySubscriptionUsage:272` (`muse.session`/`muse.weekly`
  `Used/Limit` 100, `Resets`, `plan_name` via `quotaPlanName`). Fallback
  `POST https://dev.meta.ai/api/graphql/` `LLMDCUsageQuery` (`quota.go:466`
  `postQuotaForm`) with `llm_sess`/`ecto_1_sess` browser cookie (passive
  `shared.LoadOrRefreshBrowserSession`, `quotaCookieDomain=dev.meta.ai`), `team_id`
  + volatile `fb_dtsg`/`lsd`/`doc_id` from `quota_tokens_file` JSON (see
  `docs/MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md`). Any auth-shaped failure records a
  `muse_quota_auth` diagnostic (`visit dev.meta.ai in Chrome`) and leaves local
  spend intact.

## 2. Prior behavior (committed `ab6f48a`/`425943a`)

* `muse_code.go` was logs-only: `Fetch` scanned sessions, `buildStatusMessage`,
  no `enrichQuota`. `widgets.go:8` used `providerbase.CodingToolDashboard` with
  `GaugePriority` `muse.session/muse.weekly` + `total_*`, `CompactRows`
  Sessions/Tokens/Cost, but `StandardSectionOrder = CodingToolSectionOrder()`
  (`Header, TopUsageProgress, ModelBurn, ClientBurn, ToolUsage, MCPUsage,
  LanguageBurn, CodeStats, OtherData`) and `ShowClientComposition`/`ShowLanguageComposition`/
  `ShowCodeStatsComposition`/`ShowActualToolUsage`/`ShowMCPUsage = true` — all
  empty for `muse_code` (it never sets `client_*`/`project_*`/`tool_*`/`mcp_*`/
  `lang_*`/`code_stats`), so `tui/tiles.go:538` `emptyTileSectionContent`
  rendered `No X data for this time range` for each. `quota.go` not yet present;
  `muse_code` had no quota.

## 3. What changed (uncommitted in this handoff)

* `quota.go` (new, `internal/providers/muse_code/quota.go:1-606`): `responsesBaseURL`,
  `responsesProbeModel`, `museAPIKeyCache` (`sync.Mutex` `key/ok/set`), `loadMuseAPIKey`
  (now checks `META_API_KEY` → `~/.config/openusage/muse.json` file → memo → keychain,
  and best-effort `saveMuseAPIKeyToFile`), `readMuseAPIKeyFromKeychain` (`security
  find-generic-password -a meta -s ai.meta.dev.credentials -w` `20s` timeout),
  `postSubscriptionUsage` (SSE, 64K trunc, `parseQuotaExhausted` for `429`),
  `trySubscriptionUsage`, `enrichQuota`, `quotaForm`/`postQuotaForm`, `applyQuotaMeters`,
  `quotaSummary`. Added `path/filepath` import.

* `muse_code.go:60` `HasChanged` now always polls when credentialed and scans
  `*.jsonl` mtimes; `Fetch:153` now calls `enrichQuota` + `applyPlanNameOverride`.

* `widgets.go:8` `dashboardWidget` now overrides `StandardSectionOrder` to
  `Header, TopUsageProgress, OtherData` only, and sets `ShowClientComposition=false`,
  `ShowLanguageComposition=false`, `ShowCodeStatsComposition=false`,
  `ShowActualToolUsage=false`, `ShowMCPUsage=false` (ModelBurn/DailyUsage hidden —
  they expect `model_*` metrics while `muse_code` only populates `ModelUsage`/
  `DailySeries`). `detailWidget` changed from `CodingToolDetailWidget(false)` to
  minimal `DetailWidget{Usage, Models, Spending, Trends, Tokens, Activity}`
  (clients/projects/tools/MCP/language/codeStats hidden). Header tag now correctly
  shows `Usage` (⚡) when `muse.session` present; previously `total_cost_usd` alone
  fell through to `Credits` (💰) (`tui/model_display_info.go:396` `hasUsage`).

* `tui/provider_widget.go` / `settings_modal_preferences.go` / `quota_test.go`
  / `has_changed_test.go` / `plan_name_test.go` / `local_auth_hint_test.go` deltas.

* `docs/MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md` updated with implemented client sketch.

## 4. The single password prompt — what the maintainer should know

* `~/.config/muse/auth.json` is `{"providers":{"meta":{"storage":"keychain"}}}`,
  the secret is the keychain blob `{"api_key":"LLM|…","access_token":"dca:…"}`.
  `loadMuseAPIKey` (`quota.go:117`) is now:

  ```go
  META_API_KEY env → ~/.config/openusage/muse.json file (apiKey/api_key/key or plain text)
  → museAPIKeyCache (sync.Mutex, in-process, one entry)
  → security find-generic-password
  ```

  The `security` exec prompts for password/Touch ID when the login keychain is
  locked. The cache makes it at most one prompt per daemon lifetime (the Go
  `provider.HasChanged` always returns true when credentialed, so every poll would
  otherwise re-prompt).

  On first successful keychain read it best-effort persists to
  `~/.config/openusage/muse.json` (`0o600` via `os.WriteFile` + `os.Rename`,
  `saveMuseAPIKeyToFile:173`, don't overwrite existing file). Next launch
  (or the Swift app) hits the file before the keychain, so zero prompts. The file
  is the same one Swift's `UserAPIKeyStore` (`~/.config/openusage/muse.json`)
  reads, so the two apps share the cached key. `META_API_KEY` env still wins.

  First failure (locked/denied) is cached as `("",false)` and never re-prompts
  until the daemon restarts (same as `museAPIKeyCache` before).

## 5. Quota edge case — exhausted subscription

When `Everyday Usage` is exhausted the Responses probe returns

```
HTTP 429 {"error":{"code":"rate_limit_exceeded","message":"Subscription quota exhausted. Your usage window resets at 2026-09-14T00:00:00Z.","resets_at":1789344000}}
```

instead of `200` SSE. Previously `postSubscriptionUsage:196` did
`return nil,429,fmt.Errorf("HTTP 429…")` → `trySubscriptionUsage:308`
set `muse_quota_error` and returned empty, so `muse.session`/`muse.weekly`
were missing and `computeDisplayInfo` fell through to `total_cost_usd` → `Credits`.

Now `postSubscriptionUsage:196` checks `429` + `parseQuotaExhausted:166`
(`code==rate_limit_exceeded` && `message` contains `quota` case-insensitive &&
`resets_at>0`) and returns `subscriptionUsage{Weekly:100, Window:100, Resets:resets_at,
WindowDurationMins:300}`. `applySubscriptionUsage` then emits `muse.session`/
`muse.weekly` `Used:100 Limit:100 Resets:2026-09-14T00:00:00Z`, `quotaSummary`
appends `quota session 100% / quota weekly 100%` to `Message`, and the TUI
header becomes `Usage` with Session/Weekly 100% gauges and countdowns.
Generic `429` without `quota` still returns `requestFailed(429)` and degrades to
local spend.

## 6. Branch / PR status

Branch `feat/muse-code-provider` on fork `tomck/openusage-go` is at `425943a`
(`fork/feat/muse-code-provider` up-to-date). The quota client + file cache +
429 handling + widget slimming are local unstaged/untracked changes
(`quota.go`, `quota_test.go`, `has_changed_test.go`, `plan_name_test.go`,
`muse_code.go`, `widgets.go`, `provider_widget.go`, `settings_modal_preferences.go`,
`local_auth_hint_test.go`, `SPARKTHINKING.md`). No PR has been opened by an
agent yet. To open a PR:

```bash
git -C openusage-go add internal/providers/muse_code/quota.go internal/providers/muse_code/quota_test.go \
  internal/providers/muse_code/has_changed_test.go internal/providers/muse_code/plan_name_test.go \
  internal/providers/muse_code/muse_code.go internal/providers/muse_code/widgets.go \
  internal/tui/provider_widget.go internal/tui/settings_modal_preferences.go \
  internal/tui/local_auth_hint_test.go docs/MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md
git commit -m "Add Muse Code live quota (Responses SSE) with single-prompt file cache and exhausted-quota handling"
git push fork feat/muse-code-provider
gh pr create --repo janekbaraniewski/openusage --base main --head tomck:feat/muse-code-provider
```

Update `docs/MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md` and `README` provider table
before opening. The daemon must be restarted after `make build` (`pkill -f
"openusage telemetry daemon"` + `launchctl kickstart -k` or `install`) — a
stale daemon shows `export --source auto` 4h old while `export --source direct`
is fresh.
