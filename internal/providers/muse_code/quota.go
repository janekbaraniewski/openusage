// Experimental Muse Code subscription-quota fetch. Primary path: the same
// POST <api.meta.ai>/v1/responses SSE stream the TUI's /usage view renders —
// a minimal streamed probe returns a response.subscription_usage event with
// weekly + window percentages, authenticated by the CLI's own keychain
// api_key (service "ai.meta.dev.credentials", account "meta").
// Fallback path: the private Comet GraphQL route (LLMDCUsageQuery). Meta
// publishes no quota API; this replays the dashboard's own byte-exact
// envelope using the user's own browser session. See
// docs/MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md for the full research note.
// Undocumented routes — expect maintainer pushback and do not treat these
// meters as stable.
//
// Auth shape (passive, no standing browser process):
//   - The session cookie is re-read from the user's everyday browser on
//     every poll through shared.LoadOrRefreshBrowserSession (llm_sess, with
//     ecto_1_sess fallback). Chrome works; Firefox/Safari read without an
//     OS keychain prompt. The TUI browser picker decides which.
//   - Page-load tokens (fb_dtsg and friends) cannot be harvested without a
//     page visit, so they ride in a user-managed JSON file named by the
//     account's "quota_tokens_file" path (volatile params from one dashboard
//     page load, e.g. fb_dtsg, lsd, __s, __spin_*, doc_id). chmod 600.
//   - team_id comes from the account's "team_id" path (per-account setting
//     in the dashboard request).
//
// When tokens die the enrichment degrades to an auth diagnostic telling the
// user to visit dev.meta.ai in Chrome; local spend meters are unaffected.
package muse_code

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	quotaEndpoint     = "https://dev.meta.ai/api/graphql/"
	quotaCookieDomain = "dev.meta.ai"
	quotaFriendlyName = "LLMDCUsageQuery"
	quotaCallerClass  = "RelayModern"
	quotaCRN          = "comet.llamaapi.LLMDCUsageRoute"
	quotaConsoleURL   = "https://dev.meta.ai"
)

// quotaCookieNames are the session cookies the dashboard accepts, in
// preference order. Either one alone suffices (bisect probe 2026-09-07).
var quotaCookieNames = []string{"llm_sess", "ecto_1_sess"}

// quotaEndpointOverride swaps the POST target in tests.
var quotaEndpointOverride = ""

// quotaHTTPClient builds the POST client. A var (not a plain func) so tests
// can substitute a stub RoundTripper and exercise the full request path
// without binding a socket.
var quotaHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// loadQuotaSession is a seam so tests can stub the browser read.
var loadQuotaSession = func(ctx context.Context, acct core.AccountConfig) (config.BrowserSession, bool, error) {
	if acct.BrowserCookie != nil && strings.TrimSpace(acct.BrowserCookie.CookieName) != "" {
		return shared.LoadOrRefreshBrowserSession(ctx, acct, nil)
	}
	for _, name := range quotaCookieNames {
		probe := acct
		probe.BrowserCookie = &core.BrowserCookieRef{Domain: quotaCookieDomain, CookieName: name}
		sess, ok, err := shared.LoadOrRefreshBrowserSession(ctx, probe, nil)
		if err != nil {
			return sess, false, err
		}
		if ok && strings.TrimSpace(sess.Value) != "" {
			return sess, true, nil
		}
	}
	return config.BrowserSession{}, false, nil
}

// responsesBaseURL is the Meta provider API root behind the TUI's /usage
// view. A minimal streamed probe to POST <base>/responses yields the usage
// event with no browser session or page-load tokens.
const responsesBaseURL = "https://api.meta.ai/v1"

// responsesEndpointOverride swaps the probe target in tests.
var responsesEndpointOverride = ""

// responsesProbeModel is the model the TUI itself uses; the usage event
// arrives regardless of the tiny probe input.
const responsesProbeModel = "muse-spark-1.3-contributor"

// loadMuseAPIKey returns the CLI's API bearer: META_API_KEY when set, else
// the api_key field of the keychain blob (service
// "ai.meta.dev.credentials", account "meta", darwin only). A var so tests
// can stub it. Values are secrets; callers must never log them.
// museAPIKeyCache memoizes the keychain read within the process: macOS pops
// a keychain approval on first access, so every poll must not re-prompt.
var museAPIKeyCache struct {
	sync.Mutex
	key string
	ok  bool
	set bool
}

var loadMuseAPIKey = func(ctx context.Context) (string, bool) {
	if k := strings.TrimSpace(os.Getenv("META_API_KEY")); k != "" {
		return k, true
	}
	if k, ok := loadMuseAPIKeyFromFile(); ok {
		return k, true
	}
	museAPIKeyCache.Lock()
	cached, cachedOK, set := museAPIKeyCache.key, museAPIKeyCache.ok, museAPIKeyCache.set
	museAPIKeyCache.Unlock()
	if set {
		return cached, cachedOK
	}
	key, ok := readMuseAPIKeyFromKeychain(ctx)
	museAPIKeyCache.Lock()
	museAPIKeyCache.key, museAPIKeyCache.ok, museAPIKeyCache.set = key, ok, true
	museAPIKeyCache.Unlock()
	if ok && key != "" {
		// Best-effort persist to the app-owned file so the next
		// launch (or the Swift app) doesn't need a keychain prompt.
		// Mirrors Swift's UserAPIKeyStore auto-save.
		_ = saveMuseAPIKeyToFile(key)
	}
	return key, ok
}

func loadMuseAPIKeyFromFile() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", false
	}
	path := filepath.Join(home, ".config", "openusage", "muse.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "", false
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err == nil {
		for _, field := range []string{"apiKey", "api_key", "key"} {
			if v, ok := obj[field].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v), true
			}
		}
		return "", false
	}
	if strings.Contains(trimmed, "{") {
		return "", false
	}
	return trimmed, true
}

func saveMuseAPIKeyToFile(key string) error {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return fmt.Errorf("no home dir")
	}
	path := filepath.Join(home, ".config", "openusage", "muse.json")
	if _, err := os.Stat(path); err == nil {
		// Don't overwrite an existing file — user may have set it
		// explicitly or Swift already persisted it.
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]string{"apiKey": strings.TrimSpace(key)})
	tmp := path + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readMuseAPIKeyFromKeychain(ctx context.Context) (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	probe, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, "/usr/bin/security", "find-generic-password", "-s", "ai.meta.dev.credentials", "-a", "meta", "-w").Output()
	if err != nil {
		return "", false
	}
	var blob struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &blob); err != nil {
		return "", false
	}
	key := strings.TrimSpace(blob.APIKey)
	return key, key != ""
}

// subscriptionUsage mirrors the response.subscription_usage SSE event.
type subscriptionUsage struct {
	Tier   string `json:"tier"`
	Weekly struct {
		ResetsAt    int64   `json:"resets_at"`
		UsedPercent float64 `json:"used_percent"`
	} `json:"weekly"`
	Window struct {
		ResetsAt           int64   `json:"resets_at"`
		UsedPercent        float64 `json:"used_percent"`
		WindowDurationMins int     `json:"window_duration_mins"`
	} `json:"window"`
}

// parseQuotaExhaustedResetsAt extracts the reset instant from a 429
// "Subscription quota exhausted" error. The Responses probe returns
// `{"error":{"code":"rate_limit_exceeded","message":"Subscription quota
// exhausted…","resets_at":1789344000}}` instead of an SSE stream when the
// account is at its limit. We surface the blocked state with the reset
// rather than fabricating 100% for both windows (P1-1).
func parseQuotaExhaustedResetsAt(body string) int64 {
	var payload struct {
		Error struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			ResetsAt int64  `json:"resets_at"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return 0
	}
	if payload.Error.Code != "rate_limit_exceeded" {
		return 0
	}
	if !strings.Contains(strings.ToLower(payload.Error.Message), "quota") {
		return 0
	}
	return payload.Error.ResetsAt
}

// parseQuotaExhausted is deprecated; use parseQuotaExhaustedResetsAt.
func parseQuotaExhausted(body string) *subscriptionUsage {
	if resetsAt := parseQuotaExhaustedResetsAt(body); resetsAt != 0 {
		sub := &subscriptionUsage{}
		sub.Weekly.UsedPercent = 100
		sub.Weekly.ResetsAt = resetsAt
		sub.Window.UsedPercent = 100
		sub.Window.ResetsAt = resetsAt
		sub.Window.WindowDurationMins = 300
		return sub
	}
	return nil
}

// postSubscriptionUsage sends the minimal streamed probe and returns the
// first response.subscription_usage event payload.
func postSubscriptionUsage(ctx context.Context, apiKey string) (*subscriptionUsage, int, error) {
	base := responsesBaseURL
	if responsesEndpointOverride != "" {
		base = responsesEndpointOverride
	}
	payload, err := json.Marshal(map[string]any{
		"model":  responsesProbeModel,
		"store":  false,
		"stream": true,
		"input":  "hi",
	})
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/responses", strings.NewReader(string(payload)))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := quotaHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		trimmed := strings.TrimSpace(string(body))
		// 429 with quota-exhausted is the blocked-subscription signal, not
		// a transient probe rate limit. Don't fabricate 100% for both windows
		// (P1-1); surface the blocked state with the reset so the provider
		// can render it without claiming measured percentages.
		if resp.StatusCode == http.StatusTooManyRequests {
			if resetsAt := parseQuotaExhaustedResetsAt(trimmed); resetsAt != 0 {
				return nil, resp.StatusCode, fmt.Errorf("quota exhausted, resets at %d", resetsAt)
			}
		}
		return nil, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, shared.Truncate(trimmed, 160))
	}
	// Stream the SSE frames and stop at the usage event instead of reading
	// to EOF: provider polls run under a tight per-fetch timeout.
	var data string
	var event string
	scan := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			event = ""
			continue
		}
		if rest, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(rest)
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			if rest == "[DONE]" || strings.TrimSpace(rest) == "[DONE]" {
				break
			}
			if event == "response.subscription_usage" {
				data = strings.TrimSpace(rest)
				break
			}
			continue
		}
	}
	if err := scan.Err(); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading usage stream: %w", err)
	}
	if data == "" {
		return nil, resp.StatusCode, fmt.Errorf("response stream contained no subscription_usage event")
	}
	var ev struct {
		Subscription subscriptionUsage `json:"subscription"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("cannot parse subscription_usage event: %w", err)
	}
	return &ev.Subscription, resp.StatusCode, nil
}

// quotaPlanName turns a raw tier into a display name: "Muse Code Everyday
// Usage" becomes "Everyday Usage". Opaque IDs (the Responses event carries
// an account-scoped tier ID, not a plan name) pass through unchanged rather
// than guessed at.
func quotaPlanName(tier string) string {
	trimmed := strings.TrimSpace(tier)
	if name := strings.TrimSpace(strings.TrimPrefix(trimmed, "Muse Code ")); name != "" {
		return name
	}
	return trimmed
}

// PlanNamePathKey is the provider_paths key for a user-attested plan label
// ("Everyday Usage", "High Usage", "Power Usage"). The quota probe only
// returns an opaque account-scoped tier ID, so the display name cannot be
// derived — the subscriber sets it once and it wins over the tier-derived
// value everywhere the tile reads plan_name.
const PlanNamePathKey = "plan_name"

// applyPlanNameOverride stamps the user-attested plan label onto the
// snapshot, winning over the tier-derived plan_name from enrichment.
func applyPlanNameOverride(acct core.AccountConfig, snap *core.UsageSnapshot) {
	name := strings.TrimSpace(acct.Path(PlanNamePathKey, ""))
	if name == "" {
		return
	}
	snap.EnsureMaps()
	snap.Raw["plan_name"] = name
}

func applySubscriptionUsage(snap *core.UsageSnapshot, sub *subscriptionUsage) {
	snap.EnsureMaps()
	hundred := 100.0
	window, weekly := sub.Window.UsedPercent, sub.Weekly.UsedPercent
	snap.Metrics["muse.session"] = core.Metric{Used: &window, Limit: &hundred, Unit: "quota", Window: "session"}
	if sub.Window.ResetsAt > 0 {
		snap.Resets["muse.session"] = time.Unix(sub.Window.ResetsAt, 0).UTC()
	}
	snap.Metrics["muse.weekly"] = core.Metric{Used: &weekly, Limit: &hundred, Unit: "quota", Window: "weekly"}
	if sub.Weekly.ResetsAt > 0 {
		snap.Resets["muse.weekly"] = time.Unix(sub.Weekly.ResetsAt, 0).UTC()
	}
	if sub.Tier != "" {
		snap.SetAttribute("muse_quota_tier", sub.Tier)
		if snap.Raw == nil {
			snap.Raw = make(map[string]string)
		}
		snap.Raw["plan_name"] = quotaPlanName(sub.Tier)
	}
	if summary := quotaSummary(snap); summary != "quota n/a" {
		if snap.Message != "" {
			snap.Message += " · "
		}
		snap.Message += summary
	}
}

// trySubscriptionUsage polls the Responses SSE probe when an API key is
// available. It reports true when the quota outcome is decided either way,
// so enrichQuota only falls through to the legacy dashboard-cookie path when
// no key exists.
func trySubscriptionUsage(ctx context.Context, snap *core.UsageSnapshot) bool {
	key, ok := loadMuseAPIKey(ctx)
	if !ok {
		return false
	}
	sub, status, err := postSubscriptionUsage(ctx, key)
	if err != nil {
		// Quota exhausted is a known blocked state, not a transient probe
		// failure. Don't fabricate 100% for both windows (P1-1); surface the
		// blocked state with the reset so the TUI can render it without
		// claiming measured percentages.
		if status == http.StatusTooManyRequests && strings.Contains(strings.ToLower(err.Error()), "quota exhausted") {
			var resetsAt int64
			if _, scanErr := fmt.Sscanf(err.Error(), "quota exhausted, resets at %d", &resetsAt); scanErr == nil && resetsAt > 0 {
				// Surface the weekly window as 100% with the reset from the
				// error (typically ~6 days out, e.g. Sep 14). We don't fabricate
				// both windows at 100%; the weekly is the honest one for the
				// observed reset, and the diagnostic makes the blocked state
				// explicit (P1-1).
				snap.EnsureMaps()
				hundred := 100.0
				snap.Metrics["muse.weekly"] = core.Metric{Used: &hundred, Limit: &hundred, Unit: "quota", Window: "weekly"}
				snap.Resets["muse.weekly"] = time.Unix(resetsAt, 0).UTC()
				snap.SetDiagnostic("muse_quota_blocked", fmt.Sprintf("quota exhausted, resets at %s", time.Unix(resetsAt, 0).UTC().Format(time.RFC3339)))
				snap.SetAttribute("muse_quota_blocked_resets_at", time.Unix(resetsAt, 0).UTC().Format(time.RFC3339))
				if summary := quotaSummary(snap); summary != "quota n/a" {
					if snap.Message != "" {
						snap.Message += " · "
					}
					snap.Message += summary
				}
				return true
			}
			snap.SetDiagnostic("muse_quota_blocked", "quota exhausted")
			return true
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			snap.SetDiagnostic("muse_quota_auth", "quota API key rejected — re-authenticate via `muse login`, then re-poll")
		} else {
			snap.SetDiagnostic("muse_quota_error", fmt.Sprintf("quota probe failed: %v", shared.Truncate(err.Error(), 160)))
		}
		return true
	}
	applySubscriptionUsage(snap, sub)
	return true
}

type quotaUsage struct {
	Tier                string `json:"tier"`
	AsOf                int64  `json:"as_of"`
	WindowUsed          string `json:"window_weighted_used"`
	WindowLimit         string `json:"window_weighted_limit"`
	WindowResetsAt      int64  `json:"window_resets_at"`
	WeeklyUsed          string `json:"weekly_weighted_used"`
	WeeklyLimit         string `json:"weekly_weighted_limit"`
	WeeklyResetsAt      int64  `json:"weekly_resets_at"`
	AvailableModelCount int    `json:"-"`
}

type quotaResponse struct {
	Data struct {
		Team struct {
			Usage           quotaUsage `json:"subscription_quota_usage"`
			AvailableModels []struct {
				ModelID string `json:"model_id"`
			} `json:"available_models"`
		} `json:"team"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// enrichQuota adds subscription-quota meters to an already-populated snapshot.
// It is non-fatal by design: every failure path records a diagnostic and
// leaves the local spend meters untouched.
func enrichQuota(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) {
	// Prefer the Responses SSE probe (keychain or META_API_KEY, no browser
	// session needed); fall through to the dashboard-cookie path only when
	// no API key exists.
	if trySubscriptionUsage(ctx, snap) {
		return
	}
	// Team and tokens come from the account's provider paths, with MUSE_* env
	// fallbacks mirroring MUSE_AUTH_PATH — the account is auto-detected, so
	// env vars are the surgery-free way to configure quota.
	tokensPath := core.FirstNonEmpty(acct.Path("quota_tokens_file", ""), os.Getenv("MUSE_QUOTA_TOKENS_FILE"))
	if strings.TrimSpace(tokensPath) == "" {
		snap.SetDiagnostic("muse_quota", "not configured — set quota_tokens_file (page-load params JSON) and team_id on the account to enable quota meters")
		return
	}
	volatile, err := readQuotaTokens(tokensPath)
	if err != nil {
		snap.SetDiagnostic("muse_quota_error", fmt.Sprintf("cannot read quota tokens file: %v", shared.Truncate(err.Error(), 160)))
		return
	}
	teamID := core.FirstNonEmpty(acct.Path("team_id", ""), os.Getenv("MUSE_QUOTA_TEAM_ID"))
	if strings.TrimSpace(teamID) == "" {
		snap.SetDiagnostic("muse_quota", "not configured — set team_id on the account (per-account setting from the dashboard request)")
		return
	}
	sess, ok, err := loadQuotaSession(ctx, acct)
	if err != nil || !ok || strings.TrimSpace(sess.Value) == "" {
		quotaAuthHint(snap, "no browser session")
		return
	}

	form, err := quotaForm(volatile, teamID)
	if err != nil {
		snap.SetDiagnostic("muse_quota_error", fmt.Sprintf("cannot build quota request: %v", shared.Truncate(err.Error(), 160)))
		return
	}
	body, status, err := postQuotaForm(ctx, form, sess)
	if err != nil {
		snap.SetDiagnostic("muse_quota_error", fmt.Sprintf("quota request failed: %v", shared.Truncate(err.Error(), 160)))
		return
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		quotaAuthHint(snap, fmt.Sprintf("HTTP %d", status))
		return
	}
	var qr quotaResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		snap.SetDiagnostic("muse_quota_error", fmt.Sprintf("cannot parse quota response (HTTP %d)", status))
		return
	}
	if len(qr.Errors) > 0 || qr.Data.Team.Usage.Tier == "" {
		// GraphQL errors or an empty payload mean the page tokens died even
		// though the cookie is fine — same remedy as an expired session.
		quotaAuthHint(snap, "empty quota payload")
		return
	}
	applyQuotaMeters(snap, &qr)
}

func quotaAuthHint(snap *core.UsageSnapshot, reason string) {
	snap.SetDiagnostic("muse_quota_auth", fmt.Sprintf("quota %s — visit %s in Chrome, then re-poll", reason, quotaConsoleURL))
}

// readQuotaTokens loads the volatile page-load params (fb_dtsg, lsd, __s,
// __spin_*, doc_id, …) from the user's JSON file. Values are secrets; only
// the file path lives in settings, never the params themselves.
func readQuotaTokens(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("quota tokens file must be a JSON string map: %w", err)
	}
	if strings.TrimSpace(m["fb_dtsg"]) == "" {
		return nil, fmt.Errorf("quota tokens file is missing fb_dtsg (re-save it from a fresh dashboard page load)")
	}
	if strings.TrimSpace(m["doc_id"]) == "" {
		return nil, fmt.Errorf("quota tokens file is missing doc_id (re-save it from a fresh dashboard page load)")
	}
	return m, nil
}

// quotaForm merges the fixed envelope skeleton with the stored volatile
// params and the per-account variables block.
func quotaForm(volatile map[string]string, teamID string) (url.Values, error) {
	now := time.Now().UTC()
	variables := map[string]any{
		"api_key_id": nil,
		"model_id":   nil,
		"team_id":    teamID,
		"start_date": now.AddDate(0, 0, -30).Format("2006-01-02"),
		"end_date":   now.Format("2006-01-02"),
		"timezone":   "UTC",
		"LLMDCUsageRelayPreloader_ShouldIncludeSubscriptionQuotarelayprovider": true,
		"LLMDCUsageRelayPreloader_ShouldIncludeCostMetricsrelayprovider":       true,
		"LLMDCUsageRelayPreloader_ShouldIncludeImageMetricsrelayprovider":      true,
	}
	varRaw, err := json.Marshal(variables)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	for k, v := range volatile {
		if strings.TrimSpace(v) != "" {
			form.Set(k, v)
		}
	}
	form.Set("fb_api_req_friendly_name", quotaFriendlyName)
	form.Set("fb_api_caller_class", quotaCallerClass)
	form.Set("__crn", quotaCRN)
	form.Set("variables", string(varRaw))
	return form, nil
}

func postQuotaForm(ctx context.Context, form url.Values, sess config.BrowserSession) ([]byte, int, error) {
	endpoint := quotaEndpoint
	if quotaEndpointOverride != "" {
		endpoint = quotaEndpointOverride
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", quotaConsoleURL)
	req.Header.Set("Referer", quotaConsoleURL+"/")
	req.Header.Set("X-FB-Friendly-Name", quotaFriendlyName)
	if lsd := form.Get("lsd"); lsd != "" {
		req.Header.Set("X-FB-LSD", lsd)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.AddCookie(&http.Cookie{Name: sess.CookieName, Value: sess.Value, Domain: quotaCookieDomain, Path: "/"})

	resp, err := quotaHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func quotaFloat(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func applyQuotaMeters(snap *core.UsageSnapshot, qr *quotaResponse) {
	u := &qr.Data.Team.Usage
	snap.EnsureMaps()
	if used, ok := quotaFloat(u.WindowUsed); ok {
		if limit, ok := quotaFloat(u.WindowLimit); ok && limit > 0 {
			usedCp, limitCp := used, limit
			snap.Metrics["muse.session"] = core.Metric{Used: &usedCp, Limit: &limitCp, Unit: "quota", Window: "session"}
			if u.WindowResetsAt > 0 {
				snap.Resets["muse.session"] = time.Unix(u.WindowResetsAt, 0).UTC()
			}
		}
	}
	if used, ok := quotaFloat(u.WeeklyUsed); ok {
		if limit, ok := quotaFloat(u.WeeklyLimit); ok && limit > 0 {
			usedCp, limitCp := used, limit
			snap.Metrics["muse.weekly"] = core.Metric{Used: &usedCp, Limit: &limitCp, Unit: "quota", Window: "weekly"}
			if u.WeeklyResetsAt > 0 {
				snap.Resets["muse.weekly"] = time.Unix(u.WeeklyResetsAt, 0).UTC()
			}
		}
	}
	if u.Tier != "" {
		snap.SetAttribute("muse_quota_tier", u.Tier)
		if snap.Raw == nil {
			snap.Raw = make(map[string]string)
		}
		snap.Raw["plan_name"] = quotaPlanName(u.Tier)
	}
	if u.AsOf > 0 {
		snap.SetAttribute("muse_quota_as_of", time.Unix(u.AsOf, 0).UTC().Format(time.RFC3339))
	}
	if n := len(qr.Data.Team.AvailableModels); n > 0 {
		snap.SetAttribute("muse_quota_models", fmt.Sprintf("%d", n))
	}
	if summary := quotaSummary(snap); summary != "quota n/a" {
		if snap.Message != "" {
			snap.Message += " · "
		}
		snap.Message += summary
	}
}

func quotaSummary(snap *core.UsageSnapshot) string {
	parts := []string{}
	for _, key := range []string{"muse.session", "muse.weekly"} {
		m, ok := snap.Metrics[key]
		if !ok || m.Used == nil || m.Limit == nil || *m.Limit <= 0 {
			continue
		}
		short := strings.TrimPrefix(key, "muse.")
		parts = append(parts, fmt.Sprintf("quota %s %.0f%%", short, *m.Used / *m.Limit * 100))
	}
	if len(parts) == 0 {
		return "quota n/a"
	}
	return strings.Join(parts, " / ")
}
