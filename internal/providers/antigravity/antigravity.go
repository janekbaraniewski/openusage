// Package antigravity adapts the Antigravity CLI status-line feed to the
// OpenUsage provider and telemetry contracts.
package antigravity

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

const (
	providerID           = "antigravity"
	defaultAccountID     = "antigravity"
	defaultUsageWindow   = "session"
	quotaNearLimitRatio  = 0.15
	statusFilePathEnvVar = "OPENUSAGE_ANTIGRAVITY_STATUS_FILE"
)

// Provider exposes Antigravity's documented status-line data as a local
// provider. It does not authenticate independently or make network calls.
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
				Capabilities: []string{"local_config", "statusline", "token_usage", "quota", "by_model", "by_workspace"},
				DocURL:       "https://antigravity.google/docs/cli/statusline/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: defaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				DocsURL: "https://antigravity.google/docs/cli/statusline/",
				Quickstart: []string{
					"Install and run the Antigravity CLI so ~/.gemini/antigravity-cli exists.",
					"Run `openusage integrations install antigravity` to connect the status line.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

// DetailWidget keeps the provider's detail view focused on the generic usage
// and token sections. Antigravity does not expose code-stat or tool-call data
// through the status-line contract.
func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DefaultDetailWidget()
}

// DefaultStatusFilePath returns the path written by the installed status-line
// command. An environment override is useful for alternate installations and
// keeps tests isolated without changing the persisted account schema.
func DefaultStatusFilePath() string {
	if path := strings.TrimSpace(os.Getenv(statusFilePathEnvVar)); path != "" {
		return path
	}
	stateDir, err := telemetry.DefaultStateDir()
	if err != nil || strings.TrimSpace(stateDir) == "" {
		return ""
	}
	return filepath.Join(stateDir, "antigravity-status.json")
}

func statusFilePath(acct core.AccountConfig) string {
	return strings.TrimSpace(acct.Path("status_file", DefaultStatusFilePath()))
}

// Fetch projects the latest captured status-line document into a snapshot.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	path := statusFilePath(acct)
	if path != "" {
		snap.Raw["status_file"] = path
	}

	if err := ctx.Err(); err != nil {
		return snap, err
	}
	if path == "" {
		snap.Status = core.StatusAuth
		snap.Message = "Antigravity status-line path is unavailable"
		return snap, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			snap.Status = core.StatusAuth
			snap.Message = "No Antigravity status-line data yet"
			snap.SetDiagnostic("setup", "Run `openusage integrations install antigravity`, then start agy")
			return snap, nil
		}
		return snap, fmt.Errorf("antigravity: read status file: %w", err)
	}

	payload, err := parseStatusLinePayload(data)
	if err != nil {
		snap.Status = core.StatusError
		snap.Message = "Antigravity status-line data is malformed"
		snap.SetDiagnostic("parse_error", err.Error())
		return snap, nil
	}

	projectSnapshot(&snap, payload)
	return snap, nil
}

// HasChanged lets the dashboard avoid reparsing an unchanged status file.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	path := statusFilePath(acct)
	if path == "" {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.ModTime().After(since), nil
}

func projectSnapshot(snap *core.UsageSnapshot, payload statusLinePayload) {
	if snap == nil {
		return
	}

	snap.Timestamp = payloadReceivedAt(payload)
	snap.Status = statusFromQuota(payload)

	modelID := strings.TrimSpace(payload.Model.ID)
	modelName := strings.TrimSpace(payload.Model.DisplayName)
	if modelName == "" {
		modelName = modelID
	}
	if modelID != "" {
		snap.SetAttribute("model_id", modelID)
	}
	if modelName != "" {
		snap.SetAttribute("model", modelName)
	}
	if workspace := statusWorkspace(payload); workspace != "" {
		snap.SetAttribute("workspace", workspace)
	}
	if payload.SessionID != "" {
		snap.SetAttribute("session_id", payload.SessionID)
	}
	if payload.ConversationID != "" {
		snap.SetAttribute("conversation_id", payload.ConversationID)
	}
	if payload.Version != "" {
		snap.SetAttribute("cli_version", payload.Version)
	}
	if payload.Product != "" {
		snap.SetAttribute("product", payload.Product)
	}
	if payload.PlanTier != "" {
		snap.SetAttribute("plan_tier", payload.PlanTier)
	}
	if payload.Email != "" {
		snap.SetAttribute("account_email", payload.Email)
	}
	if payload.AgentState != "" {
		snap.Raw["agent_state"] = payload.AgentState
	}

	projectContextMetrics(snap, payload.ContextWindow)
	projectQuotaMetrics(snap, payload)

	if total := cumulativeTotalTokens(payload.ContextWindow); total > 0 {
		snap.Metrics["total_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(total)),
			Unit:   "tokens",
			Window: defaultUsageWindow,
		}
		if modelName != "" {
			record := core.ModelUsageRecord{
				RawModelID:   modelName,
				RawSource:    "statusline",
				Window:       defaultUsageWindow,
				InputTokens:  core.Float64Ptr(float64(payload.ContextWindow.TotalInputTokens)),
				OutputTokens: core.Float64Ptr(float64(payload.ContextWindow.TotalOutputTokens)),
				TotalTokens:  core.Float64Ptr(float64(total)),
			}
			record.SetDimension("workspace", statusWorkspace(payload))
			snap.AppendModelUsage(record)
		}
	}

	projectCurrentUsageMetrics(snap, payload.ContextWindow.CurrentUsage)
	if snap.Message == "" {
		if modelName != "" {
			snap.Message = fmt.Sprintf("Antigravity CLI (%s)", modelName)
		} else {
			snap.Message = "Antigravity CLI status line"
		}
	}
}

func projectContextMetrics(snap *core.UsageSnapshot, contextWindow statusLineContextWindow) {
	used, remaining, hasPercent := contextPercentages(contextWindow)
	if hasPercent {
		snap.Metrics["context_window"] = core.Metric{
			Limit:     core.Float64Ptr(100),
			Used:      core.Float64Ptr(used),
			Remaining: core.Float64Ptr(remaining),
			Unit:      "%",
			Window:    defaultUsageWindow,
		}
	}

	if contextWindow.TotalInputTokens > 0 {
		snap.Metrics["total_input_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(contextWindow.TotalInputTokens)),
			Unit:   "tokens",
			Window: defaultUsageWindow,
		}
	}
	if contextWindow.TotalOutputTokens > 0 {
		snap.Metrics["total_output_tokens"] = core.Metric{
			Used:   core.Float64Ptr(float64(contextWindow.TotalOutputTokens)),
			Unit:   "tokens",
			Window: defaultUsageWindow,
		}
	}
	if !hasPercent && contextWindow.ContextWindowSize != nil && *contextWindow.ContextWindowSize > 0 {
		usedTokens := cumulativeTotalTokens(contextWindow)
		if usedTokens > 0 {
			snap.Metrics["context_window"] = core.Metric{
				Limit:  core.Float64Ptr(float64(*contextWindow.ContextWindowSize)),
				Used:   core.Float64Ptr(float64(usedTokens)),
				Unit:   "tokens",
				Window: defaultUsageWindow,
			}
		}
	}
}

func projectCurrentUsageMetrics(snap *core.UsageSnapshot, usage statusLineCurrentUsage) {
	setTokenMetric := func(key string, value int64) {
		if value <= 0 {
			return
		}
		snap.Metrics[key] = core.Metric{Used: core.Float64Ptr(float64(value)), Unit: "tokens", Window: "current"}
	}
	setTokenMetric("current_input_tokens", usage.InputTokens)
	setTokenMetric("current_output_tokens", usage.OutputTokens)
	setTokenMetric("current_cache_read_tokens", usage.CacheReadTokens)
	setTokenMetric("current_cache_write_tokens", usage.CacheWriteTokensValue())
	if total := usage.TotalTokens(); total > 0 {
		setTokenMetric("current_tokens", total)
	}
}

func projectQuotaMetrics(snap *core.UsageSnapshot, payload statusLinePayload) {
	keys := make([]string, 0, len(payload.Quota))
	for key := range payload.Quota {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	worst := 1.0
	worstName := ""
	found := false
	for _, name := range keys {
		quota := payload.Quota[name]
		if quota.RemainingFraction == nil {
			continue
		}
		remaining := clamp(*quota.RemainingFraction, 0, 1)
		if quota.Disabled {
			remaining = 0
		}
		remainingPercent := remaining * 100
		cleanName := sanitizeMetricName(name)

		window := "quota"
		if strings.Contains(cleanName, "5h") {
			window = "5h"
		} else if strings.Contains(cleanName, "weekly") || strings.Contains(cleanName, "7d") {
			window = "7d"
		}

		metric := core.Metric{
			Limit:     core.Float64Ptr(100),
			Used:      core.Float64Ptr(100 - remainingPercent),
			Remaining: core.Float64Ptr(remainingPercent),
			Unit:      "%",
			Window:    window,
		}

		key := "quota_" + cleanName
		snap.Metrics[key] = metric

		reset := quotaResetTime(quota, payloadReceivedAt(payload))
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

		if !found || remaining < worst {
			worst = remaining
			worstName = name
			found = true
		}
	}

	// Synthesize 5h window if only weekly was emitted by Antigravity CLI statusline.
	// When an account is fresh or hasn't made calls in the rolling 5h window, 5h remaining is 100%.
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

	// Overall usable quota metric:
	// If both model pools are tracked, overall available capacity reflects the active pool that
	// is still available, so having Claude/GPT exhausted does not falsely show 100% used for the account.
	var overallRemaining float64
	geminiRem, hasGemini := getPoolRemainingFraction(payload, "gemini")
	claudeRem, hasClaude := getPoolRemainingFraction(payload, "claude", "3p", "opus", "sonnet")

	if hasGemini && hasClaude {
		// If either pool has remaining capacity, use the max remaining so available pool is shown
		overallRemaining = math.Max(geminiRem, claudeRem)
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
		if quota.RemainingFraction == nil {
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
		if quota.Disabled {
			fraction = 0
		}
		if !found || fraction < worst {
			worst = fraction
			found = true
		}
	}
	return worst, found
}

func statusFromQuota(payload statusLinePayload) core.Status {
	geminiRem, hasGemini := getPoolRemainingFraction(payload, "gemini")
	claudeRem, hasClaude := getPoolRemainingFraction(payload, "claude", "3p", "opus", "sonnet")

	if hasGemini && hasClaude {
		// Only LIMITED if BOTH Gemini and Claude/Opus model pools are exhausted
		if geminiRem <= 0 && claudeRem <= 0 {
			return core.StatusLimited
		}
		// If both are near limit (< 15%)
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
		if quota.RemainingFraction == nil {
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

func contextPercentages(contextWindow statusLineContextWindow) (float64, float64, bool) {
	if contextWindow.UsedPercentage == nil && contextWindow.RemainingPercentage == nil {
		return 0, 0, false
	}
	used := 0.0
	remaining := 0.0
	if contextWindow.UsedPercentage != nil {
		used = clamp(*contextWindow.UsedPercentage, 0, 100)
	}
	if contextWindow.RemainingPercentage != nil {
		remaining = clamp(*contextWindow.RemainingPercentage, 0, 100)
	}
	if contextWindow.UsedPercentage == nil {
		used = 100 - remaining
	}
	if contextWindow.RemainingPercentage == nil {
		remaining = 100 - used
	}
	return used, remaining, true
}

func cumulativeTotalTokens(contextWindow statusLineContextWindow) int64 {
	return contextWindow.TotalInputTokens + contextWindow.TotalOutputTokens
}

func statusWorkspace(payload statusLinePayload) string {
	if current := strings.TrimSpace(payload.Workspace.CurrentDir); current != "" {
		return current
	}
	if project := strings.TrimSpace(payload.Workspace.ProjectDir); project != "" {
		return project
	}
	return strings.TrimSpace(payload.CWD)
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
