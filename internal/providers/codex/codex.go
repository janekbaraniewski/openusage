package codex

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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/samber/lo"
)

const (
	defaultCodexConfigDir   = ".codex"
	defaultChatGPTBaseURL   = "https://chatgpt.com/backend-api"
	defaultUsageWindowLabel = "all-time"

	maxScannerBufferSize = 8 * 1024 * 1024
	maxHTTPErrorBodySize = 256

	maxBreakdownMetrics = 8
	maxBreakdownRaw     = 6
)

var errLiveUsageAuth = errors.New("live usage auth failed")

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "codex",
			Info: core.ProviderInfo{
				Name:         "OpenAI Codex CLI",
				Capabilities: []string{"local_sessions", "live_usage_endpoint", "rate_limits", "token_usage", "credits", "by_model", "by_client"},
				DocURL:       "https://github.com/openai/codex",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeToken,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Codex CLI and authenticate via `codex auth`.",
					"Ensure local Codex history/config paths are readable.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

type sessionEvent struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type eventPayload struct {
	Type       string      `json:"type"`
	Info       *tokenInfo  `json:"info,omitempty"`
	RateLimits *rateLimits `json:"rate_limits,omitempty"`
}

type tokenInfo struct {
	TotalTokenUsage    tokenUsage `json:"total_token_usage"`
	LastTokenUsage     tokenUsage `json:"last_token_usage"`
	ModelContextWindow int        `json:"model_context_window"`
}

type tokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

type rateLimits struct {
	Primary   *rateLimitBucket `json:"primary,omitempty"`
	Secondary *rateLimitBucket `json:"secondary,omitempty"`
	Credits   *creditInfo      `json:"credits,omitempty"`
	PlanType  *string          `json:"plan_type,omitempty"`
}

type rateLimitBucket struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"` // Unix timestamp
}

type creditInfo struct {
	HasCredits bool     `json:"has_credits"`
	Unlimited  bool     `json:"unlimited"`
	Balance    *float64 `json:"balance"`
}

type versionInfo struct {
	LatestVersion string `json:"latest_version"`
	LastCheckedAt string `json:"last_checked_at"`
}

type authFile struct {
	AccountID string     `json:"account_id,omitempty"`
	Tokens    authTokens `json:"tokens"`
}

type authTokens struct {
	AccessToken string `json:"access_token"`
	AccountID   string `json:"account_id,omitempty"`
}

type usagePayload struct {
	UserID               string                 `json:"user_id,omitempty"`
	AccountID            string                 `json:"account_id,omitempty"`
	Email                string                 `json:"email,omitempty"`
	PlanType             string                 `json:"plan_type,omitempty"`
	RateLimit            *usageLimitDetails     `json:"rate_limit,omitempty"`
	CodeReviewRateLimit  *usageLimitDetails     `json:"code_review_rate_limit,omitempty"`
	AdditionalRateLimits []usageAdditionalLimit `json:"additional_rate_limits,omitempty"`
	Credits              *usageCredits          `json:"credits,omitempty"`
	RateLimitStatus      *usageRateLimitStatus  `json:"rate_limit_status,omitempty"`
}

type usageRateLimitStatus struct {
	PlanType             string                 `json:"plan_type,omitempty"`
	RateLimit            *usageLimitDetails     `json:"rate_limit,omitempty"`
	CodeReviewRateLimit  *usageLimitDetails     `json:"code_review_rate_limit,omitempty"`
	AdditionalRateLimits []usageAdditionalLimit `json:"additional_rate_limits,omitempty"`
	Credits              *usageCredits          `json:"credits,omitempty"`
}

type usageLimitDetails struct {
	Allowed         bool             `json:"allowed"`
	LimitReached    bool             `json:"limit_reached"`
	PrimaryWindow   *usageWindowInfo `json:"primary_window,omitempty"`
	SecondaryWindow *usageWindowInfo `json:"secondary_window,omitempty"`
	Primary         *usageWindowInfo `json:"primary,omitempty"`
	Secondary       *usageWindowInfo `json:"secondary,omitempty"`
}

type usageWindowInfo struct {
	UsedPercent        *float64 `json:"used_percent,omitempty"`
	RemainingPercent   *float64 `json:"remaining_percent,omitempty"`
	LimitWindowSeconds int      `json:"limit_window_seconds,omitempty"`
	WindowMinutes      int      `json:"window_minutes,omitempty"`
	ResetAt            int64    `json:"reset_at,omitempty"`
	ResetsAt           int64    `json:"resets_at,omitempty"`
	ResetAfterSeconds  int      `json:"reset_after_seconds,omitempty"`
}

type usageAdditionalLimit struct {
	LimitName      string             `json:"limit_name,omitempty"`
	MeteredFeature string             `json:"metered_feature,omitempty"`
	RateLimit      *usageLimitDetails `json:"rate_limit,omitempty"`
}

type usageCredits struct {
	HasCredits bool `json:"has_credits"`
	Unlimited  bool `json:"unlimited"`
	Balance    any  `json:"balance"`
}

type sessionMetaPayload struct {
	Source     string `json:"source,omitempty"`
	Originator string `json:"originator,omitempty"`
	Model      string `json:"model,omitempty"`
}

type turnContextPayload struct {
	Model string `json:"model,omitempty"`
}

type usageEntry struct {
	Name string
	Data tokenUsage
}

type usageApplySummary struct {
	limitMetricsApplied int
}

type responseItemPayload struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Name      string          `json:"name,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Status    string          `json:"status,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Input     string          `json:"input,omitempty"`
	Output    string          `json:"output,omitempty"`
	Action    *responseAction `json:"action,omitempty"`
}

type responseAction struct {
	Type string `json:"type,omitempty"`
}

type commandArgs struct {
	Cmd string `json:"cmd"`
}

type patchStats struct {
	Added      int
	Removed    int
	Files      map[string]struct{}
	Deleted    map[string]struct{}
	PatchCalls int
}

type countEntry struct {
	name  string
	count int
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DetailWidget{
		Sections: []core.DetailSection{
			{Name: "Usage", Order: 1, Style: core.DetailSectionStyleUsage},
			{Name: "Models", Order: 2, Style: core.DetailSectionStyleModels},
			{Name: "Languages", Order: 3, Style: core.DetailSectionStyleLanguages},
			{Name: "Spending", Order: 4, Style: core.DetailSectionStyleSpending},
			{Name: "Trends", Order: 5, Style: core.DetailSectionStyleTrends},
			{Name: "Tokens", Order: 6, Style: core.DetailSectionStyleTokens},
			{Name: "Activity", Order: 7, Style: core.DetailSectionStyleActivity},
		},
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   time.Now(),
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	configDir := ""
	if acct.ExtraData != nil {
		configDir = acct.ExtraData["config_dir"]
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			configDir = filepath.Join(home, defaultCodexConfigDir)
		}
	}

	if configDir == "" {
		snap.Status = core.StatusError
		snap.Message = "Cannot determine Codex config directory"
		return snap, nil
	}

	var hasLocalData bool

	sessionsDir := filepath.Join(configDir, "sessions")
	if acct.ExtraData != nil && acct.ExtraData["sessions_dir"] != "" {
		sessionsDir = acct.ExtraData["sessions_dir"]
	}

	if err := p.readLatestSession(sessionsDir, &snap); err != nil {
		snap.Raw["session_error"] = err.Error()
	} else {
		hasLocalData = true
	}

	p.readDailySessionCounts(sessionsDir, &snap)
	if err := p.readSessionUsageBreakdowns(sessionsDir, &snap); err != nil {
		snap.Raw["split_error"] = err.Error()
	}

	hasLiveData, liveErr := p.fetchLiveUsage(ctx, acct, configDir, &snap)
	if liveErr != nil {
		snap.Raw["quota_api_error"] = liveErr.Error()
	}

	versionFile := filepath.Join(configDir, "version.json")
	if data, err := os.ReadFile(versionFile); err == nil {
		var ver versionInfo
		if json.Unmarshal(data, &ver) == nil && ver.LatestVersion != "" {
			snap.Raw["cli_version"] = ver.LatestVersion
		}
	}

	if acct.ExtraData != nil {
		if email := acct.ExtraData["email"]; email != "" {
			snap.Raw["account_email"] = email
		}
		if planType := acct.ExtraData["plan_type"]; planType != "" {
			snap.Raw["plan_type"] = planType
		}
		if accountID := acct.ExtraData["account_id"]; accountID != "" {
			snap.Raw["account_id"] = accountID
		}
	}

	hasData := hasLocalData || hasLiveData
	if !hasData {
		if errors.Is(liveErr, errLiveUsageAuth) {
			snap.Status = core.StatusAuth
			snap.Message = "Codex auth required — run `codex login`"
		} else {
			snap.Status = core.StatusUnknown
			snap.Message = "No Codex usage data found"
		}
		return snap, nil
	}

	p.applyCursorCompatibilityMetrics(&snap)
	p.applyRateLimitStatus(&snap)

	switch {
	case hasLiveData && hasLocalData:
		snap.Message = "Codex live usage + local session data"
	case hasLiveData:
		snap.Message = "Codex live usage data"
	default:
		snap.Message = "Codex CLI session data"
	}

	return snap, nil
}

func (p *Provider) fetchLiveUsage(ctx context.Context, acct core.AccountConfig, configDir string, snap *core.UsageSnapshot) (bool, error) {
	authPath := filepath.Join(configDir, "auth.json")
	if acct.ExtraData != nil && acct.ExtraData["auth_file"] != "" {
		authPath = acct.ExtraData["auth_file"]
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

	accountID := firstNonEmpty(auth.Tokens.AccountID, auth.AccountID)
	if accountID == "" && acct.ExtraData != nil {
		accountID = acct.ExtraData["account_id"]
	}
	if accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", accountID)
	}

	if cliVersion := snap.Raw["cli_version"]; cliVersion != "" {
		req.Header.Set("User-Agent", "codex-cli/"+cliVersion)
	} else {
		req.Header.Set("User-Agent", "codex-cli")
	}

	resp, err := http.DefaultClient.Do(req)
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
		limitID := sanitizeMetricName(firstNonEmpty(extra.MeteredFeature, extra.LimitName))
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
	case acct.ExtraData != nil && strings.TrimSpace(acct.ExtraData["chatgpt_base_url"]) != "":
		return normalizeChatGPTBaseURL(acct.ExtraData["chatgpt_base_url"])
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

	scanner := bufio.NewScanner(bytes.NewReader(data))
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncateForError(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func (p *Provider) readLatestSession(sessionsDir string, snap *core.UsageSnapshot) error {
	latestFile, err := findLatestSessionFile(sessionsDir)
	if err != nil {
		return fmt.Errorf("finding latest session: %w", err)
	}

	snap.Raw["latest_session_file"] = filepath.Base(latestFile)

	lastPayload, err := findLastTokenCount(latestFile)
	if err != nil {
		return fmt.Errorf("reading session: %w", err)
	}

	if lastPayload == nil {
		return fmt.Errorf("no token_count events in latest session")
	}

	if lastPayload.Info != nil {
		info := lastPayload.Info
		total := info.TotalTokenUsage

		inputTokens := float64(total.InputTokens)
		snap.Metrics["session_input_tokens"] = core.Metric{
			Used:   &inputTokens,
			Unit:   "tokens",
			Window: "session",
		}

		outputTokens := float64(total.OutputTokens)
		snap.Metrics["session_output_tokens"] = core.Metric{
			Used:   &outputTokens,
			Unit:   "tokens",
			Window: "session",
		}

		cachedTokens := float64(total.CachedInputTokens)
		snap.Metrics["session_cached_tokens"] = core.Metric{
			Used:   &cachedTokens,
			Unit:   "tokens",
			Window: "session",
		}

		if total.ReasoningOutputTokens > 0 {
			reasoning := float64(total.ReasoningOutputTokens)
			snap.Metrics["session_reasoning_tokens"] = core.Metric{
				Used:   &reasoning,
				Unit:   "tokens",
				Window: "session",
			}
		}

		totalTokens := float64(total.TotalTokens)
		snap.Metrics["session_total_tokens"] = core.Metric{
			Used:   &totalTokens,
			Unit:   "tokens",
			Window: "session",
		}

		if info.ModelContextWindow > 0 {
			ctxWindow := float64(info.ModelContextWindow)
			ctxUsed := float64(total.InputTokens)
			snap.Metrics["context_window"] = core.Metric{
				Limit: &ctxWindow,
				Used:  &ctxUsed,
				Unit:  "tokens",
			}
		}
	}

	if lastPayload.RateLimits != nil {
		rl := lastPayload.RateLimits
		rateLimitSet := false

		if rl.Primary != nil {
			limit := float64(100)
			used := rl.Primary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Primary.WindowMinutes)
			snap.Metrics["rate_limit_primary"] = core.Metric{
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				Unit:      "%",
				Window:    windowStr,
			}

			if rl.Primary.ResetsAt > 0 {
				resetTime := time.Unix(rl.Primary.ResetsAt, 0)
				snap.Resets["rate_limit_primary"] = resetTime
			}
			rateLimitSet = true
		}

		if rl.Secondary != nil {
			limit := float64(100)
			used := rl.Secondary.UsedPercent
			remaining := 100 - used
			windowStr := formatWindow(rl.Secondary.WindowMinutes)
			snap.Metrics["rate_limit_secondary"] = core.Metric{
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				Unit:      "%",
				Window:    windowStr,
			}

			if rl.Secondary.ResetsAt > 0 {
				resetTime := time.Unix(rl.Secondary.ResetsAt, 0)
				snap.Resets["rate_limit_secondary"] = resetTime
			}
			rateLimitSet = true
		}

		if rl.Credits != nil {
			if rl.Credits.Unlimited {
				snap.Raw["credits"] = "unlimited"
			} else if rl.Credits.HasCredits {
				snap.Raw["credits"] = "available"
				if rl.Credits.Balance != nil {
					snap.Raw["credit_balance"] = fmt.Sprintf("$%.2f", *rl.Credits.Balance)
				}
			} else {
				snap.Raw["credits"] = "none"
			}
		}

		if rl.PlanType != nil {
			snap.Raw["plan_type"] = *rl.PlanType
		}
		if rateLimitSet && snap.Raw["rate_limit_source"] == "" {
			snap.Raw["rate_limit_source"] = "session"
		}
	}

	return nil
}

func (p *Provider) readSessionUsageBreakdowns(sessionsDir string, snap *core.UsageSnapshot) error {
	modelTotals := make(map[string]tokenUsage)
	clientTotals := make(map[string]tokenUsage)
	modelDaily := make(map[string]map[string]float64)
	clientDaily := make(map[string]map[string]float64)
	interfaceDaily := make(map[string]map[string]float64)
	dailyTokenTotals := make(map[string]float64)
	dailyRequestTotals := make(map[string]float64)
	clientSessions := make(map[string]int)
	clientRequests := make(map[string]int)
	toolCalls := make(map[string]int)
	langRequests := make(map[string]int)
	callTool := make(map[string]string)
	callOutcome := make(map[string]int)
	stats := patchStats{
		Files:   make(map[string]struct{}),
		Deleted: make(map[string]struct{}),
	}
	today := time.Now().UTC().Format("2006-01-02")
	totalRequests := 0
	requestsToday := 0
	promptCount := 0
	commits := 0
	completedWithoutCallID := 0

	walkErr := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		defaultDay := dayFromSessionPath(path, sessionsDir)
		sessionClient := "Other"
		currentModel := "unknown"
		var previous tokenUsage
		var hasPrevious bool
		var countedSession bool

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		buf := make([]byte, 0, 512*1024)
		scanner.Buffer(buf, maxScannerBufferSize)

		for scanner.Scan() {
			line := scanner.Bytes()
			if !bytes.Contains(line, []byte(`"type":"event_msg"`)) &&
				!bytes.Contains(line, []byte(`"type":"turn_context"`)) &&
				!bytes.Contains(line, []byte(`"type":"session_meta"`)) &&
				!bytes.Contains(line, []byte(`"type":"response_item"`)) {
				continue
			}

			var event sessionEvent
			if err := json.Unmarshal(line, &event); err != nil {
				continue
			}

			switch event.Type {
			case "session_meta":
				var meta sessionMetaPayload
				if json.Unmarshal(event.Payload, &meta) == nil {
					sessionClient = classifyClient(meta.Source, meta.Originator)
					if meta.Model != "" {
						currentModel = meta.Model
					}
				}
			case "turn_context":
				var tc turnContextPayload
				if json.Unmarshal(event.Payload, &tc) == nil && strings.TrimSpace(tc.Model) != "" {
					currentModel = tc.Model
				}
			case "event_msg":
				var payload eventPayload
				if json.Unmarshal(event.Payload, &payload) != nil {
					continue
				}
				if payload.Type == "user_message" {
					promptCount++
					continue
				}
				if payload.Type != "token_count" || payload.Info == nil {
					continue
				}

				total := payload.Info.TotalTokenUsage
				delta := total
				if hasPrevious {
					delta = usageDelta(total, previous)
					if !validUsageDelta(delta) {
						delta = total
					}
				}
				previous = total
				hasPrevious = true

				if delta.TotalTokens <= 0 {
					continue
				}

				modelName := normalizeModelName(currentModel)
				clientName := normalizeClientName(sessionClient)
				day := dayFromTimestamp(event.Timestamp)
				if day == "" {
					day = defaultDay
				}

				addUsage(modelTotals, modelName, delta)
				addUsage(clientTotals, clientName, delta)
				addDailyUsage(modelDaily, modelName, day, float64(delta.TotalTokens))
				addDailyUsage(clientDaily, clientName, day, float64(delta.TotalTokens))
				addDailyUsage(interfaceDaily, clientInterfaceBucket(clientName), day, 1)
				dailyTokenTotals[day] += float64(delta.TotalTokens)
				dailyRequestTotals[day]++
				clientRequests[clientName]++
				totalRequests++
				if day == today {
					requestsToday++
				}

				if !countedSession {
					clientSessions[clientName]++
					countedSession = true
				}
			case "response_item":
				var item responseItemPayload
				if json.Unmarshal(event.Payload, &item) != nil {
					continue
				}
				switch item.Type {
				case "function_call":
					tool := normalizeToolName(item.Name)
					recordToolCall(toolCalls, callTool, item.CallID, tool)
					if strings.EqualFold(tool, "exec_command") {
						var args commandArgs
						if json.Unmarshal(item.Arguments, &args) == nil {
							recordCommandLanguage(args.Cmd, langRequests)
							if commandContainsGitCommit(args.Cmd) {
								commits++
							}
						}
					}
				case "custom_tool_call":
					tool := normalizeToolName(item.Name)
					recordToolCall(toolCalls, callTool, item.CallID, tool)
					if strings.EqualFold(tool, "apply_patch") {
						stats.PatchCalls++
						accumulatePatchStats(item.Input, &stats, langRequests)
					}
				case "web_search_call":
					recordToolCall(toolCalls, callTool, "", "web_search")
					completedWithoutCallID++
				case "function_call_output", "custom_tool_call_output":
					setToolCallOutcome(item.CallID, item.Output, callOutcome)
				}
			}
		}

		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("walking session files: %w", walkErr)
	}

	emitBreakdownMetrics("model", modelTotals, modelDaily, snap)
	emitBreakdownMetrics("client", clientTotals, clientDaily, snap)
	emitClientSessionMetrics(clientSessions, snap)
	emitClientRequestMetrics(clientRequests, snap)
	emitToolMetrics(toolCalls, callTool, callOutcome, completedWithoutCallID, snap)
	emitLanguageMetrics(langRequests, snap)
	emitProductivityMetrics(stats, promptCount, commits, totalRequests, requestsToday, clientSessions, snap)
	emitDailyUsageSeries(dailyTokenTotals, dailyRequestTotals, interfaceDaily, snap)

	return nil
}

func recordToolCall(toolCalls map[string]int, callTool map[string]string, callID, tool string) {
	tool = normalizeToolName(tool)
	toolCalls[tool]++
	if strings.TrimSpace(callID) != "" {
		callTool[callID] = tool
	}
}

func normalizeToolName(tool string) string {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "unknown"
	}
	return tool
}

func setToolCallOutcome(callID, output string, outcomes map[string]int) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	outcomes[callID] = inferToolCallOutcome(output)
}

func inferToolCallOutcome(output string) int {
	lower := strings.ToLower(strings.TrimSpace(output))
	if lower == "" {
		return 1
	}
	if strings.Contains(lower, `"exit_code":0`) || strings.Contains(lower, "process exited with code 0") {
		return 1
	}
	if strings.Contains(lower, "cancelled") || strings.Contains(lower, "canceled") || strings.Contains(lower, "aborted") {
		return 3
	}
	if idx := strings.Index(lower, "process exited with code "); idx >= 0 {
		rest := lower[idx+len("process exited with code "):]
		n := 0
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		if n == 0 {
			return 1
		}
		return 2
	}
	if strings.Contains(lower, `"exit_code":`) && !strings.Contains(lower, `"exit_code":0`) {
		return 2
	}
	if strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
		return 2
	}
	return 1
}

func recordCommandLanguage(cmd string, langs map[string]int) {
	language := detectCommandLanguage(cmd)
	if language != "" {
		langs[language]++
	}
}

func detectCommandLanguage(cmd string) string {
	trimmed := strings.TrimSpace(strings.ToLower(cmd))
	if trimmed == "" {
		return ""
	}
	switch {
	case strings.Contains(trimmed, " go ") || strings.HasPrefix(trimmed, "go ") || strings.Contains(trimmed, "gofmt ") || strings.Contains(trimmed, "golangci-lint"):
		return "go"
	case strings.Contains(trimmed, " terraform ") || strings.HasPrefix(trimmed, "terraform "):
		return "terraform"
	case strings.Contains(trimmed, " python ") || strings.HasPrefix(trimmed, "python ") || strings.HasPrefix(trimmed, "python3 "):
		return "python"
	case strings.Contains(trimmed, " npm ") || strings.HasPrefix(trimmed, "npm ") || strings.Contains(trimmed, " yarn ") || strings.HasPrefix(trimmed, "pnpm ") || strings.Contains(trimmed, " node "):
		return "ts"
	case strings.Contains(trimmed, " cargo ") || strings.HasPrefix(trimmed, "cargo ") || strings.Contains(trimmed, " rustc "):
		return "rust"
	case strings.Contains(trimmed, " java ") || strings.HasPrefix(trimmed, "java ") || strings.Contains(trimmed, " gradle ") || strings.Contains(trimmed, " mvn "):
		return "java"
	case strings.Contains(trimmed, ".log"):
		return "log"
	case strings.Contains(trimmed, ".txt"):
		return "txt"
	default:
		return "shell"
	}
}

func commandContainsGitCommit(cmd string) bool {
	normalized := " " + strings.ToLower(cmd) + " "
	return strings.Contains(normalized, " git commit ")
}

func accumulatePatchStats(input string, stats *patchStats, langs map[string]int) {
	if stats == nil {
		return
	}
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				stats.Deleted[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "*** Move to: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Move to: "))
			if path != "" {
				stats.Files[path] = struct{}{}
				if language := languageFromPath(path); language != "" {
					langs[language]++
				}
			}
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "***"):
			continue
		case strings.HasPrefix(line, "+"):
			stats.Added++
		case strings.HasPrefix(line, "-"):
			stats.Removed++
		}
	}
}

func languageFromPath(path string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".go"):
		return "go"
	case strings.HasSuffix(lower, ".tf"):
		return "terraform"
	case strings.HasSuffix(lower, ".ts"), strings.HasSuffix(lower, ".tsx"), strings.HasSuffix(lower, ".js"), strings.HasSuffix(lower, ".jsx"):
		return "ts"
	case strings.HasSuffix(lower, ".py"):
		return "python"
	case strings.HasSuffix(lower, ".rs"):
		return "rust"
	case strings.HasSuffix(lower, ".java"):
		return "java"
	case strings.HasSuffix(lower, ".yaml"), strings.HasSuffix(lower, ".yml"):
		return "yaml"
	case strings.HasSuffix(lower, ".json"):
		return "json"
	case strings.HasSuffix(lower, ".md"):
		return "md"
	case strings.HasSuffix(lower, ".tpl"):
		return "tpl"
	case strings.HasSuffix(lower, ".txt"):
		return "txt"
	case strings.HasSuffix(lower, ".log"):
		return "log"
	case strings.HasSuffix(lower, ".sh"), strings.HasSuffix(lower, ".zsh"), strings.HasSuffix(lower, ".bash"):
		return "shell"
	default:
		return ""
	}
}

func emitClientRequestMetrics(clientRequests map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	interfaceTotals := make(map[string]float64)
	for name, count := range clientRequests {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
			interfaceTotals[clientInterfaceBucket(name)] += float64(count)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})
	for i, item := range all {
		if i >= maxBreakdownMetrics {
			break
		}
		value := float64(item.count)
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_requests"] = core.Metric{
			Used:   &value,
			Unit:   "requests",
			Window: defaultUsageWindowLabel,
		}
	}
	for bucket, value := range interfaceTotals {
		v := value
		snap.Metrics["interface_"+sanitizeMetricName(bucket)] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: defaultUsageWindowLabel,
		}
	}
}

func clientInterfaceBucket(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "desktop"):
		return "desktop_app"
	case strings.Contains(lower, "cli"), strings.Contains(lower, "exec"), strings.Contains(lower, "terminal"):
		return "cli_agents"
	case strings.Contains(lower, "ide"), strings.Contains(lower, "vscode"), strings.Contains(lower, "editor"):
		return "ide"
	case strings.Contains(lower, "cloud"), strings.Contains(lower, "web"):
		return "cloud_agents"
	case strings.Contains(lower, "human"), strings.Contains(lower, "other"):
		return "human"
	default:
		return sanitizeMetricName(name)
	}
}

func emitToolMetrics(toolCalls map[string]int, callTool map[string]string, callOutcome map[string]int, completedWithoutCallID int, snap *core.UsageSnapshot) {
	var all []countEntry
	totalCalls := 0
	for name, count := range toolCalls {
		if count <= 0 {
			continue
		}
		all = append(all, countEntry{name: name, count: count})
		totalCalls += count
		v := float64(count)
		snap.Metrics["tool_"+sanitizeMetricName(name)] = core.Metric{
			Used:   &v,
			Unit:   "calls",
			Window: defaultUsageWindowLabel,
		}
	}
	if totalCalls <= 0 {
		return
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	completed := completedWithoutCallID
	errored := 0
	cancelled := 0
	for callID := range callTool {
		switch callOutcome[callID] {
		case 2:
			errored++
		case 3:
			cancelled++
		default:
			completed++
		}
	}
	if completed+errored+cancelled < totalCalls {
		completed += totalCalls - (completed + errored + cancelled)
	}

	totalV := float64(totalCalls)
	snap.Metrics["tool_calls_total"] = core.Metric{Used: &totalV, Unit: "calls", Window: defaultUsageWindowLabel}
	if completed > 0 {
		v := float64(completed)
		snap.Metrics["tool_completed"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if errored > 0 {
		v := float64(errored)
		snap.Metrics["tool_errored"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if cancelled > 0 {
		v := float64(cancelled)
		snap.Metrics["tool_cancelled"] = core.Metric{Used: &v, Unit: "calls", Window: defaultUsageWindowLabel}
	}
	if totalCalls > 0 {
		success := float64(completed) / float64(totalCalls) * 100
		snap.Metrics["tool_success_rate"] = core.Metric{
			Used:   &success,
			Unit:   "%",
			Window: defaultUsageWindowLabel,
		}
	}
	snap.Raw["tool_usage"] = formatCountSummary(all, maxBreakdownRaw)
}

func emitLanguageMetrics(langRequests map[string]int, snap *core.UsageSnapshot) {
	var all []countEntry
	for language, count := range langRequests {
		if count <= 0 {
			continue
		}
		all = append(all, countEntry{name: language, count: count})
		v := float64(count)
		snap.Metrics["lang_"+sanitizeMetricName(language)] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: defaultUsageWindowLabel,
		}
	}
	if len(all) == 0 {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})
	snap.Raw["language_usage"] = formatCountSummary(all, maxBreakdownRaw)
}

func emitProductivityMetrics(stats patchStats, promptCount, commits, totalRequests, requestsToday int, clientSessions map[string]int, snap *core.UsageSnapshot) {
	if totalRequests > 0 {
		v := float64(totalRequests)
		snap.Metrics["total_ai_requests"] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
		snap.Metrics["composer_requests"] = core.Metric{Used: &v, Unit: "requests", Window: defaultUsageWindowLabel}
	}
	if requestsToday > 0 {
		v := float64(requestsToday)
		snap.Metrics["requests_today"] = core.Metric{Used: &v, Unit: "requests", Window: "today"}
		snap.Metrics["today_composer_requests"] = core.Metric{Used: &v, Unit: "requests", Window: "today"}
	}

	totalSessions := 0
	for _, count := range clientSessions {
		totalSessions += count
	}
	if totalSessions > 0 {
		v := float64(totalSessions)
		snap.Metrics["composer_sessions"] = core.Metric{Used: &v, Unit: "sessions", Window: defaultUsageWindowLabel}
	}

	if metric, ok := snap.Metrics["context_window"]; ok && metric.Used != nil && metric.Limit != nil && *metric.Limit > 0 {
		pct := *metric.Used / *metric.Limit * 100
		if pct > 100 {
			pct = 100
		}
		if pct < 0 {
			pct = 0
		}
		snap.Metrics["composer_context_pct"] = core.Metric{
			Used:   &pct,
			Unit:   "%",
			Window: metric.Window,
		}
	}

	if stats.Added > 0 {
		v := float64(stats.Added)
		snap.Metrics["composer_lines_added"] = core.Metric{Used: &v, Unit: "lines", Window: defaultUsageWindowLabel}
	}
	if stats.Removed > 0 {
		v := float64(stats.Removed)
		snap.Metrics["composer_lines_removed"] = core.Metric{Used: &v, Unit: "lines", Window: defaultUsageWindowLabel}
	}
	if filesChanged := len(stats.Files); filesChanged > 0 {
		v := float64(filesChanged)
		snap.Metrics["composer_files_changed"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
		snap.Metrics["ai_tracked_files"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
	}
	if deleted := len(stats.Deleted); deleted > 0 {
		v := float64(deleted)
		snap.Metrics["ai_deleted_files"] = core.Metric{Used: &v, Unit: "files", Window: defaultUsageWindowLabel}
	}
	if commits > 0 {
		v := float64(commits)
		snap.Metrics["scored_commits"] = core.Metric{Used: &v, Unit: "commits", Window: defaultUsageWindowLabel}
	}
	if promptCount > 0 {
		v := float64(promptCount)
		snap.Metrics["total_prompts"] = core.Metric{Used: &v, Unit: "prompts", Window: defaultUsageWindowLabel}
	}
	if stats.PatchCalls > 0 {
		base := totalRequests
		if base < stats.PatchCalls {
			base = stats.PatchCalls
		}
		if base > 0 {
			pct := float64(stats.PatchCalls) / float64(base) * 100
			snap.Metrics["ai_code_percentage"] = core.Metric{Used: &pct, Unit: "%", Window: defaultUsageWindowLabel}
		}
	}
}

func emitDailyUsageSeries(dailyTokenTotals, dailyRequestTotals map[string]float64, interfaceDaily map[string]map[string]float64, snap *core.UsageSnapshot) {
	if len(dailyTokenTotals) > 0 {
		points := mapToSortedTimePoints(dailyTokenTotals)
		snap.DailySeries["analytics_tokens"] = points
		snap.DailySeries["tokens_total"] = points
	}
	if len(dailyRequestTotals) > 0 {
		points := mapToSortedTimePoints(dailyRequestTotals)
		snap.DailySeries["analytics_requests"] = points
		snap.DailySeries["requests"] = points
	}
	for name, byDay := range interfaceDaily {
		if len(byDay) == 0 {
			continue
		}
		key := sanitizeMetricName(name)
		snap.DailySeries["usage_client_"+key] = mapToSortedTimePoints(byDay)
		snap.DailySeries["usage_source_"+key] = mapToSortedTimePoints(byDay)
	}
}

func formatCountSummary(entries []countEntry, max int) string {
	if len(entries) == 0 || max <= 0 {
		return ""
	}
	total := 0
	for _, entry := range entries {
		total += entry.count
	}
	if total <= 0 {
		return ""
	}
	limit := max
	if limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		pct := float64(entries[i].count) / float64(total) * 100
		parts = append(parts, fmt.Sprintf("%s %s (%.0f%%)", entries[i].name, formatTokenCount(entries[i].count), pct))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func emitBreakdownMetrics(prefix string, totals map[string]tokenUsage, daily map[string]map[string]float64, snap *core.UsageSnapshot) {
	entries := sortUsageEntries(totals)
	if len(entries) == 0 {
		return
	}

	for i, entry := range entries {
		if i >= maxBreakdownMetrics {
			break
		}
		keyPrefix := prefix + "_" + sanitizeMetricName(entry.Name)
		setUsageMetric(snap, keyPrefix+"_total_tokens", float64(entry.Data.TotalTokens))
		setUsageMetric(snap, keyPrefix+"_input_tokens", float64(entry.Data.InputTokens))
		setUsageMetric(snap, keyPrefix+"_output_tokens", float64(entry.Data.OutputTokens))

		if entry.Data.CachedInputTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_cached_tokens", float64(entry.Data.CachedInputTokens))
		}
		if entry.Data.ReasoningOutputTokens > 0 {
			setUsageMetric(snap, keyPrefix+"_reasoning_tokens", float64(entry.Data.ReasoningOutputTokens))
		}

		if byDay, ok := daily[entry.Name]; ok {
			series := mapToSortedTimePoints(byDay)
			snap.DailySeries["tokens_"+prefix+"_"+sanitizeMetricName(entry.Name)] = series
			snap.DailySeries["usage_"+prefix+"_"+sanitizeMetricName(entry.Name)] = series
		}

		if prefix == "model" {
			rec := core.ModelUsageRecord{
				RawModelID:   entry.Name,
				RawSource:    "jsonl",
				Window:       defaultUsageWindowLabel,
				InputTokens:  core.Float64Ptr(float64(entry.Data.InputTokens)),
				OutputTokens: core.Float64Ptr(float64(entry.Data.OutputTokens)),
				TotalTokens:  core.Float64Ptr(float64(entry.Data.TotalTokens)),
			}
			if entry.Data.CachedInputTokens > 0 {
				rec.CachedTokens = core.Float64Ptr(float64(entry.Data.CachedInputTokens))
			}
			if entry.Data.ReasoningOutputTokens > 0 {
				rec.ReasoningTokens = core.Float64Ptr(float64(entry.Data.ReasoningOutputTokens))
			}
			core.AppendModelUsageRecord(snap, rec)
		}
	}

	rawKey := prefix + "_usage"
	snap.Raw[rawKey] = formatUsageSummary(entries, maxBreakdownRaw)
}

func emitClientSessionMetrics(clientSessions map[string]int, snap *core.UsageSnapshot) {
	type entry struct {
		name  string
		count int
	}
	var all []entry
	for name, count := range clientSessions {
		if count > 0 {
			all = append(all, entry{name: name, count: count})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].count == all[j].count {
			return all[i].name < all[j].name
		}
		return all[i].count > all[j].count
	})

	for i, item := range all {
		if i >= maxBreakdownMetrics {
			break
		}
		value := float64(item.count)
		snap.Metrics["client_"+sanitizeMetricName(item.name)+"_sessions"] = core.Metric{
			Used:   &value,
			Unit:   "sessions",
			Window: defaultUsageWindowLabel,
		}
	}
}

func setUsageMetric(snap *core.UsageSnapshot, key string, value float64) {
	if value <= 0 {
		return
	}
	snap.Metrics[key] = core.Metric{
		Used:   &value,
		Unit:   "tokens",
		Window: defaultUsageWindowLabel,
	}
}

func addUsage(target map[string]tokenUsage, name string, delta tokenUsage) {
	current := target[name]
	current.InputTokens += delta.InputTokens
	current.CachedInputTokens += delta.CachedInputTokens
	current.OutputTokens += delta.OutputTokens
	current.ReasoningOutputTokens += delta.ReasoningOutputTokens
	current.TotalTokens += delta.TotalTokens
	target[name] = current
}

func addDailyUsage(target map[string]map[string]float64, name, day string, value float64) {
	if day == "" || value <= 0 {
		return
	}
	if target[name] == nil {
		target[name] = make(map[string]float64)
	}
	target[name][day] += value
}

func sortUsageEntries(values map[string]tokenUsage) []usageEntry {
	out := make([]usageEntry, 0, len(values))
	for name, data := range values {
		out = append(out, usageEntry{Name: name, Data: data})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Data.TotalTokens == out[j].Data.TotalTokens {
			return out[i].Name < out[j].Name
		}
		return out[i].Data.TotalTokens > out[j].Data.TotalTokens
	})
	return out
}

func formatUsageSummary(entries []usageEntry, max int) string {
	total := 0
	for _, entry := range entries {
		total += entry.Data.TotalTokens
	}
	if total <= 0 {
		return ""
	}

	limit := max
	if limit > len(entries) {
		limit = len(entries)
	}

	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		entry := entries[i]
		pct := float64(entry.Data.TotalTokens) / float64(total) * 100
		parts = append(parts, fmt.Sprintf("%s %s (%.0f%%)", entry.Name, formatTokenCount(entry.Data.TotalTokens), pct))
	}

	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func formatTokenCount(value int) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(value)/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.1fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func usageDelta(current, previous tokenUsage) tokenUsage {
	return tokenUsage{
		InputTokens:           current.InputTokens - previous.InputTokens,
		CachedInputTokens:     current.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:          current.OutputTokens - previous.OutputTokens,
		ReasoningOutputTokens: current.ReasoningOutputTokens - previous.ReasoningOutputTokens,
		TotalTokens:           current.TotalTokens - previous.TotalTokens,
	}
}

func validUsageDelta(delta tokenUsage) bool {
	return delta.InputTokens >= 0 &&
		delta.CachedInputTokens >= 0 &&
		delta.OutputTokens >= 0 &&
		delta.ReasoningOutputTokens >= 0 &&
		delta.TotalTokens >= 0
}

func normalizeModelName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

func classifyClient(source, originator string) string {
	src := strings.ToLower(strings.TrimSpace(source))
	org := strings.ToLower(strings.TrimSpace(originator))

	switch {
	case strings.Contains(org, "desktop"):
		return "Desktop App"
	case strings.Contains(org, "exec") || src == "exec":
		return "Exec"
	case strings.Contains(org, "cli") || src == "cli":
		return "CLI"
	case src == "vscode" || src == "ide":
		return "IDE"
	case src == "":
		return "Other"
	default:
		return strings.ToUpper(src)
	}
}

func normalizeClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Other"
	}
	return name
}

func sanitizeMetricName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func dayFromTimestamp(timestamp string) string {
	if timestamp == "" {
		return ""
	}

	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, timestamp); err == nil {
			return parsed.Format("2006-01-02")
		}
	}

	if len(timestamp) >= 10 {
		candidate := timestamp[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func dayFromSessionPath(path, sessionsDir string) string {
	rel, err := filepath.Rel(sessionsDir, path)
	if err != nil {
		return ""
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return ""
	}

	candidate := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
	if _, err := time.Parse("2006-01-02", candidate); err != nil {
		return ""
	}
	return candidate
}

func mapToSortedTimePoints(byDate map[string]float64) []core.TimePoint {
	if len(byDate) == 0 {
		return nil
	}

	keys := lo.Keys(byDate)
	sort.Strings(keys)

	points := make([]core.TimePoint, 0, len(keys))
	for _, date := range keys {
		points = append(points, core.TimePoint{Date: date, Value: byDate[date]})
	}
	return points
}

func (p *Provider) applyRateLimitStatus(snap *core.UsageSnapshot) {
	if snap.Status == core.StatusAuth || snap.Status == core.StatusError || snap.Status == core.StatusUnknown || snap.Status == core.StatusUnsupported {
		return
	}

	status := core.StatusOK
	for key, metric := range snap.Metrics {
		if !strings.HasPrefix(key, "rate_limit_") || metric.Unit != "%" || metric.Used == nil {
			continue
		}
		used := *metric.Used
		if used >= 100 {
			status = core.StatusLimited
			break
		}
		if used >= 90 {
			status = core.StatusNearLimit
		}
	}
	snap.Status = status
}

func (p *Provider) applyCursorCompatibilityMetrics(snap *core.UsageSnapshot) {
	aliasMetricIfMissing(snap, "rate_limit_primary", "plan_auto_percent_used")
	aliasMetricIfMissing(snap, "rate_limit_secondary", "plan_api_percent_used")

	if _, ok := snap.Metrics["plan_percent_used"]; !ok {
		used := 0.0
		sourceWindow := ""
		if metric, ok := snap.Metrics["rate_limit_primary"]; ok && metric.Used != nil {
			used = *metric.Used
			sourceWindow = metric.Window
		}
		if metric, ok := snap.Metrics["rate_limit_secondary"]; ok && metric.Used != nil && *metric.Used > used {
			used = *metric.Used
			sourceWindow = metric.Window
		}
		if used > 0 {
			limit := float64(100)
			remaining := 100 - used
			snap.Metrics["plan_percent_used"] = core.Metric{
				Limit:     &limit,
				Used:      &used,
				Remaining: &remaining,
				Unit:      "%",
				Window:    sourceWindow,
			}
		}
	}

	if _, ok := snap.Metrics["composer_context_pct"]; !ok {
		if metric, ok := snap.Metrics["context_window"]; ok && metric.Used != nil && metric.Limit != nil && *metric.Limit > 0 {
			pct := *metric.Used / *metric.Limit * 100
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			snap.Metrics["composer_context_pct"] = core.Metric{
				Used:   &pct,
				Unit:   "%",
				Window: metric.Window,
			}
		}
	}

	if _, ok := snap.Metrics["credit_balance"]; !ok {
		if balance, ok := parseCurrencyValue(snap.Raw["credit_balance"]); ok {
			snap.Metrics["credit_balance"] = core.Metric{
				Used:   &balance,
				Unit:   "USD",
				Window: "current",
			}
		}
	}

	aliasMetricIfMissing(snap, "total_ai_requests", "composer_requests")
	aliasMetricIfMissing(snap, "requests_today", "today_composer_requests")
}

func aliasMetricIfMissing(snap *core.UsageSnapshot, source, target string) {
	if snap == nil || source == "" || target == "" {
		return
	}
	if _, exists := snap.Metrics[target]; exists {
		return
	}
	metric, ok := snap.Metrics[source]
	if !ok {
		return
	}
	snap.Metrics[target] = metric
}

func parseCurrencyValue(raw string) (float64, bool) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return 0, false
	}
	normalized = strings.TrimPrefix(normalized, "$")
	normalized = strings.ReplaceAll(normalized, ",", "")
	value, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func findLatestSessionFile(sessionsDir string) (string, error) {
	var files []string

	err := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walking sessions dir: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no session files found in %s", sessionsDir)
	}

	sort.Slice(files, func(i, j int) bool {
		si, _ := os.Stat(files[i])
		sj, _ := os.Stat(files[j])
		if si == nil || sj == nil {
			return false
		}
		return si.ModTime().After(sj.ModTime())
	})

	return files[0], nil
}

func findLastTokenCount(path string) (*eventPayload, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lastPayload *eventPayload

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, maxScannerBufferSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"type":"event_msg"`)) {
			continue
		}

		var event sessionEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Type != "event_msg" {
			continue
		}

		var payload eventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}

		if payload.Type == "token_count" {
			lastPayload = &payload
		}
	}

	return lastPayload, scanner.Err()
}

func (p *Provider) readDailySessionCounts(sessionsDir string, snap *core.UsageSnapshot) {
	dayCounts := make(map[string]int) // "2025-01-15" → count

	_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		rel, relErr := filepath.Rel(sessionsDir, path)
		if relErr != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 3 {
			dateStr := fmt.Sprintf("%s-%s-%s", parts[0], parts[1], parts[2])
			if _, parseErr := time.Parse("2006-01-02", dateStr); parseErr == nil {
				dayCounts[dateStr]++
			}
		}
		return nil
	})

	if len(dayCounts) == 0 {
		return
	}

	dates := lo.Keys(dayCounts)
	sort.Strings(dates)

	for _, d := range dates {
		snap.DailySeries["sessions"] = append(snap.DailySeries["sessions"], core.TimePoint{
			Date:  d,
			Value: float64(dayCounts[d]),
		})
	}
}

func formatWindow(minutes int) string {
	if minutes <= 0 {
		return ""
	}
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	remaining := minutes % 60
	if remaining == 0 {
		if hours >= 24 {
			days := hours / 24
			leftover := hours % 24
			if leftover == 0 {
				return fmt.Sprintf("%dd", days)
			}
			return fmt.Sprintf("%dd%dh", days, leftover)
		}
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, remaining)
}
