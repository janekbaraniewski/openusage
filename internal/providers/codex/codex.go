package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
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
	return core.CodingToolDetailWidget(true)
}

// HasChanged reports whether the Codex sessions directory has been modified since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	configDir := acct.Hint("config_dir", "")
	if configDir == "" {
		if home, _ := os.UserHomeDir(); home != "" {
			configDir = filepath.Join(home, ".codex")
		}
	}
	if configDir == "" {
		return true, nil
	}
	sessionsDir := acct.Hint("sessions_dir", filepath.Join(configDir, "sessions"))
	for _, path := range []string{sessionsDir, filepath.Join(configDir, "version.json")} {
		if info, err := os.Stat(path); err == nil && info.ModTime().After(since) {
			return true, nil
		}
	}
	return false, nil
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

	configDir := acct.Hint("config_dir", "")
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
	if override := acct.Hint("sessions_dir", ""); override != "" {
		sessionsDir = override
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
