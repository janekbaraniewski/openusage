// Muse Code live quota: the same POST api.meta.ai/v1/responses SSE probe
// the TUI's `/usage` view renders — a minimal `store:false, stream:true,
// input:"hi"` probe returns `response.subscription_usage` with weekly + window
// percentages, authenticated by the CLI's own keychain `api_key`
// (service `ai.meta.dev.credentials`, account `meta`). No browser session
// or page-load tokens. See `openusage/OPENUSAGE-GO-MUSECODE.md` for the
// current design; the archived `MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md` is the
// historical GraphQL investigation and is not shipped.
package muse_code

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// quotaHTTPClient builds the POST client. A var (not a plain func) so tests
// can substitute a stub RoundTripper and exercise the full request path
// without binding a socket.
var quotaHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
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
	if err == nil {
		trimmed := strings.TrimSpace(string(data))
		if trimmed != "" {
			var obj map[string]any
			if err := json.Unmarshal(data, &obj); err == nil {
				for _, field := range []string{"apiKey", "api_key", "key"} {
					if v, ok := obj[field].(string); ok && strings.TrimSpace(v) != "" {
						return strings.TrimSpace(v), true
					}
				}
			} else if !strings.Contains(trimmed, "{") {
				return trimmed, true
			}
		}
	}
	// Fallback: the Muse CLI's own auth file on Linux stores the api_key
	// inline as providers.meta.api_key (alongside the dca: access_token).
	// On darwin it is in the keychain, but on Linux the file is the source
	// of truth and avoids needing a separate ~/.config/openusage/muse.json
	// copy. This makes `muse login` on Linux immediately quota-capable.
	authPath := filepath.Join(home, ".config", "muse", "auth.json")
	if data, err := os.ReadFile(authPath); err == nil {
		var doc struct {
			Providers map[string]struct {
				APIKey string `json:"api_key"`
			} `json:"providers"`
		}
		if err := json.Unmarshal(data, &doc); err == nil {
			if prov, ok := doc.Providers["meta"]; ok && strings.TrimSpace(prov.APIKey) != "" {
				return strings.TrimSpace(prov.APIKey), true
			}
		}
	}
	return "", false
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

// quotaExhaustedError is the blocked-subscription signal from a 429
// "Subscription quota exhausted" probe response. Carries the reset instant
// as a typed field so callers don't string-parse it back out of an error.
type quotaExhaustedError struct {
	ResetsAt int64
}

func (e *quotaExhaustedError) Error() string {
	return fmt.Sprintf("quota exhausted, resets at %d", e.ResetsAt)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/responses", bytes.NewReader(payload))
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
				return nil, resp.StatusCode, &quotaExhaustedError{ResetsAt: resetsAt}
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
		var exhausted *quotaExhaustedError
		if status == http.StatusTooManyRequests && errors.As(err, &exhausted) {
			resetsAt := exhausted.ResetsAt
			if resetsAt > 0 {
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

// enrichQuota adds live quota (Responses SSE) to the snapshot.
// It is non-fatal: on any failure it records a diagnostic and leaves the
// local spend meters untouched. No browser session is required.
func enrichQuota(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) {
	// Single clean probe: POST api.meta.ai/v1/responses with the keychain
	// or file API key. No GraphQL fallback — that path required opening
	// dev.meta.ai periodically and is intentionally removed. The history
	// remains in git (MUSE_CODE_GRAPHQL_QUOTA_RESEARCH.md) for reference.
	if trySubscriptionUsage(ctx, snap) {
		return
	}
	// No API key: quota is unavailable, but local spend is still valid.
	// Keep the diagnostic minimal and honest — don't imply a browser
	// visit would fix it when the clean path is just `muse login` or
	// `META_API_KEY` / `~/.config/openusage/muse.json`.
	snap.SetDiagnostic("muse_quota", "quota unavailable — run `muse login` or set META_API_KEY / ~/.config/openusage/muse.json to enable quota")
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
