package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func (p *Provider) fetchLiveUsage(ctx context.Context, acct core.AccountConfig, configDir string, snap *core.UsageSnapshot) (bool, error) {
	authPath := filepath.Join(configDir, "auth.json")
	if override := acct.Hint("auth_file", ""); override != "" {
		authPath = override
	}

	data, err := os.ReadFile(authPath)
	if err != nil {
		return false, nil
	}

	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return false, nil
	}

	if strings.TrimSpace(auth.Tokens.AccessToken) == "" {
		return false, nil
	}

	baseURL := resolveChatGPTBaseURL(acct, configDir)
	usageURL := usageURLForBase(baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return false, fmt.Errorf("codex: creating live usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
	req.Header.Set("Accept", "application/json")

	accountID := core.FirstNonEmpty(auth.Tokens.AccountID, auth.AccountID)
	if accountID == "" {
		accountID = acct.Hint("account_id", "")
	}
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}

	if cliVersion := snap.Raw["cli_version"]; cliVersion != "" {
		req.Header.Set("User-Agent", "codex-cli/"+cliVersion)
	} else {
		req.Header.Set("User-Agent", "codex-cli")
	}

	resp, err := p.Client().Do(req)
	if err != nil {
		return false, fmt.Errorf("codex: live usage request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false, fmt.Errorf("%w: HTTP %d", errLiveUsageAuth, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("codex: live usage HTTP %d: %s", resp.StatusCode, truncateForError(string(body), maxHTTPErrorBodySize))
	}

	var payload usagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("codex: parsing live usage response: %w", err)
	}

	summary := applyUsagePayload(&payload, snap)
	if summary.limitMetricsApplied > 0 {
		snap.Raw["rate_limit_source"] = "live"
	} else {
		clearRateLimitMetrics(snap)
		snap.Raw["rate_limit_source"] = "live_unavailable"
		snap.Raw["rate_limit_warning"] = "live usage payload did not include limit windows"
	}
	snap.Raw["quota_api"] = "live"
	return true, nil
}

func applyUsagePayload(payload *usagePayload, snap *core.UsageSnapshot) usageApplySummary {
	var summary usageApplySummary
	if payload == nil {
		return summary
	}

	if payload.Email != "" {
		snap.Raw["account_email"] = payload.Email
	}
	if payload.AccountID != "" {
		snap.Raw["account_id"] = payload.AccountID
	}
	if payload.PlanType != "" {
		snap.Raw["plan_type"] = payload.PlanType
	}

	summary.limitMetricsApplied += applyUsageLimitDetails(payload.RateLimit, "rate_limit_primary", "rate_limit_secondary", snap)
	summary.limitMetricsApplied += applyUsageLimitDetails(payload.CodeReviewRateLimit, "rate_limit_code_review_primary", "rate_limit_code_review_secondary", snap)
	summary.limitMetricsApplied += applyUsageAdditionalLimits(payload.AdditionalRateLimits, snap)

	if payload.RateLimitStatus != nil {
		status := payload.RateLimitStatus
		if payload.PlanType == "" && status.PlanType != "" {
			snap.Raw["plan_type"] = status.PlanType
		}
		summary.limitMetricsApplied += applyUsageLimitDetails(status.RateLimit, "rate_limit_primary", "rate_limit_secondary", snap)
		summary.limitMetricsApplied += applyUsageLimitDetails(status.CodeReviewRateLimit, "rate_limit_code_review_primary", "rate_limit_code_review_secondary", snap)
		summary.limitMetricsApplied += applyUsageAdditionalLimits(status.AdditionalRateLimits, snap)
		if payload.Credits == nil {
			applyUsageCredits(status.Credits, snap)
		}
	}

	applyUsageCredits(payload.Credits, snap)
	return summary
}

func applyUsageAdditionalLimits(additional []usageAdditionalLimit, snap *core.UsageSnapshot) int {
	applied := 0
	for _, extra := range additional {
		limitID := sanitizeMetricName(core.FirstNonEmpty(extra.MeteredFeature, extra.LimitName))
		if limitID == "" || limitID == "codex" {
			continue
		}

		primaryKey := "rate_limit_" + limitID + "_primary"
		secondaryKey := "rate_limit_" + limitID + "_secondary"
		applied += applyUsageLimitDetails(extra.RateLimit, primaryKey, secondaryKey, snap)
		if extra.LimitName != "" {
			snap.Raw["rate_limit_"+limitID+"_name"] = extra.LimitName
		}
	}
	return applied
}

func applyUsageCredits(credits *usageCredits, snap *core.UsageSnapshot) {
	if credits == nil {
		return
	}

	switch {
	case credits.Unlimited:
		snap.Raw["credits"] = "unlimited"
	case credits.HasCredits:
		snap.Raw["credits"] = "available"
		if formatted := formatCreditsBalance(credits.Balance); formatted != "" {
			snap.Raw["credit_balance"] = formatted
		}
	default:
		snap.Raw["credits"] = "none"
	}
}

func formatCreditsBalance(balance any) string {
	switch v := balance.(type) {
	case nil:
		return ""
	case string:
		if strings.TrimSpace(v) == "" {
			return ""
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return fmt.Sprintf("$%.2f", f)
		}
		return v
	case float64:
		return fmt.Sprintf("$%.2f", v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return fmt.Sprintf("$%.2f", f)
		}
	}
	return ""
}

func applyUsageLimitDetails(details *usageLimitDetails, primaryKey, secondaryKey string, snap *core.UsageSnapshot) int {
	if details == nil {
		return 0
	}
	applied := 0
	primary := details.PrimaryWindow
	if primary == nil {
		primary = details.Primary
	}
	secondary := details.SecondaryWindow
	if secondary == nil {
		secondary = details.Secondary
	}
	if applyUsageWindowMetric(primary, primaryKey, snap) {
		applied++
	}
	if applyUsageWindowMetric(secondary, secondaryKey, snap) {
		applied++
	}
	return applied
}

func applyUsageWindowMetric(window *usageWindowInfo, key string, snap *core.UsageSnapshot) bool {
	if window == nil || key == "" {
		return false
	}

	used, ok := resolveWindowUsedPercent(window)
	if !ok {
		return false
	}

	limit := float64(100)
	remaining := 100 - used
	windowLabel := formatWindow(resolveWindowMinutes(window))

	snap.Metrics[key] = core.Metric{
		Limit:     &limit,
		Used:      &used,
		Remaining: &remaining,
		Unit:      "%",
		Window:    windowLabel,
	}

	if resetAt := resolveWindowResetAt(window); resetAt > 0 {
		snap.Resets[key] = time.Unix(resetAt, 0)
	}
	return true
}

func resolveWindowUsedPercent(window *usageWindowInfo) (float64, bool) {
	if window == nil {
		return 0, false
	}
	if window.UsedPercent != nil {
		return clampPercent(*window.UsedPercent), true
	}
	if window.RemainingPercent != nil {
		return clampPercent(100 - *window.RemainingPercent), true
	}
	return 0, false
}

func resolveWindowMinutes(window *usageWindowInfo) int {
	if window == nil {
		return 0
	}
	if window.LimitWindowSeconds > 0 {
		return secondsToMinutes(window.LimitWindowSeconds)
	}
	if window.WindowMinutes > 0 {
		return window.WindowMinutes
	}
	return 0
}

func resolveWindowResetAt(window *usageWindowInfo) int64 {
	if window == nil {
		return 0
	}
	switch {
	case window.ResetAt > 0:
		return window.ResetAt
	case window.ResetsAt > 0:
		return window.ResetsAt
	case window.ResetAfterSeconds > 0:
		return time.Now().UTC().Add(time.Duration(window.ResetAfterSeconds) * time.Second).Unix()
	default:
		return 0
	}
}

func clearRateLimitMetrics(snap *core.UsageSnapshot) {
	for key := range snap.Metrics {
		if strings.HasPrefix(key, "rate_limit_") {
			delete(snap.Metrics, key)
		}
	}
	for key := range snap.Resets {
		if strings.HasPrefix(key, "rate_limit_") {
			delete(snap.Resets, key)
		}
	}
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func secondsToMinutes(seconds int) int {
	if seconds <= 0 {
		return 0
	}
	return (seconds + 59) / 60
}

func resolveChatGPTBaseURL(acct core.AccountConfig, configDir string) string {
	switch {
	case strings.TrimSpace(acct.BaseURL) != "":
		return normalizeChatGPTBaseURL(acct.BaseURL)
	case strings.TrimSpace(acct.Hint("chatgpt_base_url", "")) != "":
		return normalizeChatGPTBaseURL(acct.Hint("chatgpt_base_url", ""))
	default:
		if fromConfig := readChatGPTBaseURLFromConfig(configDir); fromConfig != "" {
			return normalizeChatGPTBaseURL(fromConfig)
		}
	}
	return normalizeChatGPTBaseURL(defaultChatGPTBaseURL)
}

func readChatGPTBaseURLFromConfig(configDir string) string {
	if strings.TrimSpace(configDir) == "" {
		return ""
	}

	configPath := filepath.Join(configDir, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		if !strings.HasPrefix(line, "chatgpt_base_url") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, "\"'")
		if val != "" {
			return val
		}
	}

	return ""
}

func normalizeChatGPTBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return defaultChatGPTBaseURL
	}
	if (strings.HasPrefix(baseURL, "https://chatgpt.com") || strings.HasPrefix(baseURL, "https://chat.openai.com")) &&
		!strings.Contains(baseURL, "/backend-api") {
		baseURL += "/backend-api"
	}
	return baseURL
}

func usageURLForBase(baseURL string) string {
	if strings.Contains(baseURL, "/backend-api") {
		return baseURL + "/wham/usage"
	}
	return baseURL + "/api/codex/usage"
}

func truncateForError(value string, max int) string {
	return shared.Truncate(strings.TrimSpace(value), max)
}
