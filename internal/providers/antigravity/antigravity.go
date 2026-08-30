// Package antigravity fetches Antigravity account quota from Google's
// Cloud Code internal API using each box's local OAuth token.
package antigravity

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

const (
	providerID          = "antigravity"
	defaultAccountID    = "antigravity"
	defaultUsageWindow  = "session"
	quotaNearLimitRatio = 0.15
)

// Provider exposes Antigravity quota via retrieveUserQuotaSummary.
type Provider struct {
	providerbase.Base
}

// New returns the Antigravity provider.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: providerID,
			Info: core.ProviderInfo{
				Name:         "Antigravity CLI",
				Capabilities: []string{"local_config", "oauth", "quota"},
				DocURL:       "https://antigravity.google/docs/cli/reference",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: defaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				DocsURL: "https://antigravity.google/docs/cli/reference",
				Quickstart: []string{
					"Install the Antigravity CLI (`agy`) and sign in.",
					"For multi-account boxes, use `agy-box <name>` so ~/.agy-containers/<name> exists.",
					"OpenUsage reads antigravity-oauth-token and polls retrieveUserQuotaSummary.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

// DetailWidget keeps the provider's detail view focused on the generic usage
// sections. Antigravity's API path exposes quota buckets only.
func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DefaultDetailWidget()
}

// Fetch loads a usable OAuth token for the account, calls
// retrieveUserQuotaSummary, and projects quota metrics into a snapshot.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	if err := ctx.Err(); err != nil {
		return snap, err
	}

	if dir := configDir(acct); dir != "" {
		snap.Raw["config_dir"] = dir
	}
	if box := boxName(acct); box != "" {
		snap.SetAttribute("box", box)
	}

	accessToken, tokenPath, tokenRefreshed, err := ensureAccessToken(ctx, acct, p.Client())
	if tokenPath != "" {
		snap.Raw["oauth_token_file"] = tokenPath
	}
	if err != nil {
		snap.Status = core.StatusAuth
		snap.Message = "Antigravity OAuth token unavailable"
		snap.SetDiagnostic("auth_error", err.Error())
		snap.SetDiagnostic("setup", "Sign in with agy / agy-box so antigravity-oauth-token exists")
		return snap, nil
	}
	if tokenRefreshed {
		snap.Raw["oauth_status"] = "refreshed"
	} else {
		snap.Raw["oauth_status"] = "valid"
	}

	baseURL := strings.TrimSpace(acct.Hint("quota_endpoint", defaultQuotaEndpoint))
	summary, err := retrieveUserQuotaSummary(ctx, accessToken, baseURL, p.Client())
	if err != nil {
		// One retry after forcing a box ping when the access token looks rejected.
		if isAuthHTTPError(err) {
			if pingErr := pingBoxForToken(ctx, acct); pingErr == nil {
				accessToken, tokenPath, _, retryErr := ensureAccessToken(ctx, acct, p.Client())
				if retryErr == nil {
					summary, err = retrieveUserQuotaSummary(ctx, accessToken, baseURL, p.Client())
					if err == nil {
						snap.Raw["oauth_status"] = "refreshed_after_401"
						if tokenPath != "" {
							snap.Raw["oauth_token_file"] = tokenPath
						}
					}
				}
			}
		}
	}
	if err != nil {
		if isAuthHTTPError(err) {
			snap.Status = core.StatusAuth
			snap.Message = "Antigravity quota API rejected credentials"
		} else {
			snap.Status = core.StatusError
			snap.Message = "Antigravity quota API request failed"
		}
		snap.SetDiagnostic("quota_api_error", err.Error())
		return snap, nil
	}

	payload := statusLinePayload{
		Quota:      quotaMapFromSummary(summary),
		ReceivedAt: time.Now().UTC(),
		Product:    "antigravity",
	}
	if len(payload.Quota) == 0 {
		snap.Status = core.StatusError
		snap.Message = "Antigravity quota API returned no buckets"
		return snap, nil
	}

	projectSnapshot(&snap, payload)
	snap.Raw["quota_api"] = fmt.Sprintf("ok (%d buckets)", len(payload.Quota))
	snap.Raw["quota_source"] = "retrieveUserQuotaSummary"
	return snap, nil
}

func isAuthHTTPError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 401") || strings.Contains(msg, "HTTP 403")
}

func projectSnapshot(snap *core.UsageSnapshot, payload statusLinePayload) {
	if snap == nil {
		return
	}

	snap.Timestamp = payloadReceivedAt(payload)
	snap.Status = statusFromQuota(payload)

	if payload.Product != "" {
		snap.SetAttribute("product", payload.Product)
	}
	if payload.PlanTier != "" {
		snap.SetAttribute("plan_tier", payload.PlanTier)
	}
	if payload.Email != "" {
		snap.SetAttribute("account_email", payload.Email)
	}

	projectQuotaMetrics(snap, payload)

	if snap.Message == "" {
		snap.Message = "Antigravity quota"
	}
}

// EnrichSnapshots overlays live retrieveUserQuotaSummary data onto daemon
// snapshots. The TUI refresh path (`r` / auto-refresh) reads the telemetry
// read-model, which only updates on the daemon poll cadence; this fetches
// current quota in the dashboard process so refresh is immediate.
func (p *Provider) EnrichSnapshots(ctx context.Context, accounts []core.AccountConfig, snaps map[string]core.UsageSnapshot) {
	if p == nil || len(snaps) == 0 {
		return
	}
	byID := make(map[string]core.AccountConfig, len(accounts))
	for _, acct := range accounts {
		if id := strings.TrimSpace(acct.ID); id != "" {
			byID[id] = acct
		}
	}

	type result struct {
		id   string
		snap core.UsageSnapshot
		ok   bool
	}
	ids := make([]string, 0, len(snaps))
	for id, snap := range snaps {
		if snap.ProviderID != providerID && !strings.HasPrefix(id, providerID+"-") && id != providerID {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}

	results := make(chan result, len(ids))
	var wg sync.WaitGroup
	for _, id := range ids {
		acct, ok := byID[id]
		if !ok {
			acct = core.AccountConfig{ID: id, Provider: providerID}
		}
		wg.Add(1)
		go func(id string, acct core.AccountConfig) {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				results <- result{id: id}
				return
			}
			fresh, err := p.Fetch(ctx, acct)
			if err != nil || !snapshotUsable(fresh) {
				results <- result{id: id}
				return
			}
			results <- result{id: id, snap: fresh, ok: true}
		}(id, acct)
	}
	wg.Wait()
	close(results)
	for res := range results {
		if res.ok {
			snaps[res.id] = res.snap
		}
	}
}

func snapshotUsable(snap core.UsageSnapshot) bool {
	if snap.Status == core.StatusAuth || snap.Status == core.StatusError {
		return len(snap.Metrics) > 0
	}
	return len(snap.Metrics) > 0 || (snap.Status != "" && snap.Status != core.StatusUnknown)
}

func projectQuotaMetrics(snap *core.UsageSnapshot, payload statusLinePayload) {
	keys := make([]string, 0, len(payload.Quota))
	for key := range payload.Quota {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	now := time.Now().UTC()
	receivedAt := payloadReceivedAt(payload)

	modelID := strings.TrimSpace(payload.Model.ID)
	modelName := strings.TrimSpace(payload.Model.DisplayName)
	if modelName == "" {
		modelName = modelID
	}
	modelLower := strings.ToLower(modelName)
	activePool := ""
	if strings.Contains(modelLower, "gemini") {
		activePool = "gemini"
	} else if strings.Contains(modelLower, "claude") || strings.Contains(modelLower, "sonnet") || strings.Contains(modelLower, "opus") || strings.Contains(modelLower, "3p") || strings.Contains(modelLower, "gpt") {
		activePool = "claude"
	}

	isForActivePool := func(name string) bool {
		lower := strings.ToLower(name)
		if activePool == "gemini" {
			return strings.Contains(lower, "gemini")
		}
		if activePool == "claude" {
			return strings.Contains(lower, "claude") || strings.Contains(lower, "3p") || strings.Contains(lower, "opus") || strings.Contains(lower, "sonnet")
		}
		return true
	}

	worst := 1.0
	worstName := ""
	found := false
	for _, name := range keys {
		quota := payload.Quota[name]
		if quota.RemainingFraction == nil {
			continue
		}
		remaining := clamp(*quota.RemainingFraction, 0, 1)
		cleanName := sanitizeMetricName(name)

		window := "quota"
		var period time.Duration
		if strings.Contains(cleanName, "5h") {
			window = "5h"
			period = 5 * time.Hour
		} else if strings.Contains(cleanName, "weekly") || strings.Contains(cleanName, "7d") {
			window = "7d"
			period = 7 * 24 * time.Hour
		}

		reset := quotaResetTime(quota, receivedAt)

		// If the quota reset timestamp is in the past, the reset event has already occurred.
		// Advance the reset timestamp by the window period until it represents the upcoming reset,
		// and reset the remaining fraction to 1.0 (100% remaining).
		if !reset.IsZero() && period > 0 && reset.Before(now) {
			for reset.Before(now) {
				reset = reset.Add(period)
			}
			remaining = 1.0
		}

		remainingPercent := remaining * 100

		metric := core.Metric{
			Limit:     core.Float64Ptr(100),
			Used:      core.Float64Ptr(100 - remainingPercent),
			Remaining: core.Float64Ptr(remainingPercent),
			Unit:      "%",
			Window:    window,
		}

		key := "quota_" + cleanName
		snap.Metrics[key] = metric

		if !reset.IsZero() {
			snap.Resets[key] = reset
			snap.Resets[key+"_reset"] = reset
		}

		// Alias 3p keys to claude keys for backward and cross-widget compatibility
		if cleanName == "3p_5h" {
			snap.Metrics["quota_claude_5h"] = metric
			if !reset.IsZero() {
				snap.Resets["quota_claude_5h"] = reset
				snap.Resets["quota_claude_5h_reset"] = reset
			}
		} else if cleanName == "3p_weekly" {
			snap.Metrics["quota_claude_weekly"] = metric
			if !reset.IsZero() {
				snap.Resets["quota_claude_weekly"] = reset
				snap.Resets["quota_claude_weekly_reset"] = reset
			}
		}

		if !quota.Disabled {
			if !found || (isForActivePool(name) && !isForActivePool(worstName)) || (isForActivePool(name) == isForActivePool(worstName) && remaining < worst) {
				worst = remaining
				worstName = name
				found = true
			}
		}
	}

	// Synthesize 5h window if only weekly was emitted.
	if _, hasGemini5h := snap.Metrics["quota_gemini_5h"]; !hasGemini5h {
		if gWk, hasGeminiWk := snap.Metrics["quota_gemini_weekly"]; hasGeminiWk && gWk.Remaining != nil {
			rem := 100.0
			if *gWk.Remaining <= 0 {
				rem = 0.0
			}
			snap.Metrics["quota_gemini_5h"] = core.Metric{
				Limit:     core.Float64Ptr(100),
				Used:      core.Float64Ptr(100 - rem),
				Remaining: core.Float64Ptr(rem),
				Unit:      "%",
				Window:    "5h",
			}
		}
	}
	if _, hasClaude5h := snap.Metrics["quota_claude_5h"]; !hasClaude5h {
		if cWk, hasClaudeWk := snap.Metrics["quota_claude_weekly"]; hasClaudeWk && cWk.Remaining != nil {
			rem := 100.0
			if *cWk.Remaining <= 0 {
				rem = 0.0
			}
			snap.Metrics["quota_claude_5h"] = core.Metric{
				Limit:     core.Float64Ptr(100),
				Used:      core.Float64Ptr(100 - rem),
				Remaining: core.Float64Ptr(rem),
				Unit:      "%",
				Window:    "5h",
			}
			snap.Metrics["quota_3p_5h"] = snap.Metrics["quota_claude_5h"]
		}
	}

	if !found {
		return
	}

	var overallRemaining float64
	geminiRem, hasGemini := getPoolRemainingFraction(payload, "gemini")
	claudeRem, hasClaude := getPoolRemainingFraction(payload, "claude", "3p", "opus", "sonnet")

	if hasGemini && hasClaude {
		if activePool == "gemini" {
			overallRemaining = geminiRem
		} else if activePool == "claude" {
			overallRemaining = claudeRem
		} else {
			overallRemaining = math.Max(geminiRem, claudeRem)
		}
	} else if hasGemini {
		overallRemaining = geminiRem
	} else if hasClaude {
		overallRemaining = claudeRem
	} else if found {
		overallRemaining = worst
	} else {
		overallRemaining = 1.0
	}

	remainingPercent := overallRemaining * 100
	snap.Metrics["quota"] = core.Metric{
		Limit:     core.Float64Ptr(100),
		Used:      core.Float64Ptr(100 - remainingPercent),
		Remaining: core.Float64Ptr(remainingPercent),
		Unit:      "%",
		Window:    "quota",
	}
	if quota, ok := payload.Quota[worstName]; ok {
		if reset := quotaResetTime(quota, payloadReceivedAt(payload)); !reset.IsZero() {
			snap.Resets["quota"] = reset
			snap.Resets["quota_reset"] = reset
		}
	}
}

func getPoolRemainingFraction(payload statusLinePayload, poolKeywords ...string) (float64, bool) {
	worst := 1.0
	found := false
	for name, quota := range payload.Quota {
		if quota.RemainingFraction == nil || quota.Disabled {
			continue
		}
		cleanName := strings.ToLower(sanitizeMetricName(name))
		matches := false
		for _, kw := range poolKeywords {
			if strings.Contains(cleanName, kw) {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		fraction := clamp(*quota.RemainingFraction, 0, 1)
		if !found || fraction < worst {
			worst = fraction
			found = true
		}
	}
	return worst, found
}

func statusFromQuota(payload statusLinePayload) core.Status {
	model := strings.ToLower(payload.Model.DisplayName)
	if model == "" {
		model = strings.ToLower(payload.Model.ID)
	}
	if strings.Contains(model, "gemini") {
		if rem, has := getPoolRemainingFraction(payload, "gemini"); has {
			if rem <= 0 {
				return core.StatusLimited
			}
			if rem < quotaNearLimitRatio {
				return core.StatusNearLimit
			}
			return core.StatusOK
		}
	} else if strings.Contains(model, "claude") || strings.Contains(model, "sonnet") || strings.Contains(model, "opus") || strings.Contains(model, "3p") || strings.Contains(model, "gpt") {
		if rem, has := getPoolRemainingFraction(payload, "claude", "3p", "opus", "sonnet"); has {
			if rem <= 0 {
				return core.StatusLimited
			}
			if rem < quotaNearLimitRatio {
				return core.StatusNearLimit
			}
			return core.StatusOK
		}
	}

	geminiRem, hasGemini := getPoolRemainingFraction(payload, "gemini")
	claudeRem, hasClaude := getPoolRemainingFraction(payload, "claude", "3p", "opus", "sonnet")

	if hasGemini && hasClaude {
		if geminiRem <= 0 && claudeRem <= 0 {
			return core.StatusLimited
		}
		if geminiRem < quotaNearLimitRatio && claudeRem < quotaNearLimitRatio {
			return core.StatusNearLimit
		}
		return core.StatusOK
	}

	worst, ok := worstQuotaFraction(payload)
	if !ok {
		return core.StatusOK
	}
	if worst <= 0 {
		return core.StatusLimited
	}
	if worst < quotaNearLimitRatio {
		return core.StatusNearLimit
	}
	return core.StatusOK
}

func worstQuotaFraction(payload statusLinePayload) (float64, bool) {
	worst := 1.0
	found := false
	for _, quota := range payload.Quota {
		if quota.RemainingFraction == nil || quota.Disabled {
			continue
		}
		fraction := clamp(*quota.RemainingFraction, 0, 1)
		if !found || fraction < worst {
			worst = fraction
			found = true
		}
	}
	return worst, found
}

func payloadReceivedAt(payload statusLinePayload) time.Time {
	if !payload.ReceivedAt.IsZero() {
		return payload.ReceivedAt.UTC()
	}
	return time.Now().UTC()
}

func quotaResetTime(quota statusLineQuota, receivedAt time.Time) time.Time {
	if reset := strings.TrimSpace(quota.ResetTime); reset != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, reset); err == nil {
			return parsed.UTC()
		}
		if parsed, err := time.Parse(time.RFC3339, reset); err == nil {
			return parsed.UTC()
		}
	}
	if quota.ResetInSeconds != nil && *quota.ResetInSeconds > 0 {
		return receivedAt.Add(time.Duration(*quota.ResetInSeconds) * time.Second)
	}
	return time.Time{}
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func sanitizeMetricName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}
