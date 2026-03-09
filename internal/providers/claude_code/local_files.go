package claude_code

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func floorToHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

func buildStatsCandidates(explicitPath, claudeDir, home string) []string {
	if explicitPath != "" {
		return []string{explicitPath}
	}

	candidates := []string{
		filepath.Join(claudeDir, "stats-cache.json"),
		filepath.Join(claudeDir, ".claude-backup", "stats-cache.json"),
		filepath.Join(home, ".claude-backup", "stats-cache.json"),
	}

	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func applyUsageResponse(usage *usageResponse, snap *core.UsageSnapshot, now time.Time) {
	applyUsageBucket := func(metricKey, window, resetKey string, bucket *usageBucket) {
		if bucket == nil {
			return
		}

		util := bucket.Utilization
		limit := float64(100)
		if t, ok := parseReset(bucket.ResetsAt); ok {
			if !t.After(now) {
				util = 0
			}
			if resetKey != "" {
				snap.Resets[resetKey] = t
			}
		}

		snap.Metrics[metricKey] = core.Metric{
			Used:   &util,
			Limit:  &limit,
			Unit:   "%",
			Window: window,
		}
	}

	applyUsageBucket("usage_five_hour", "5h", "usage_five_hour", usage.FiveHour)
	applyUsageBucket("usage_seven_day", "7d", "usage_seven_day", usage.SevenDay)
	applyUsageBucket("usage_seven_day_sonnet", "7d-sonnet", "", usage.SevenDaySonnet)
	applyUsageBucket("usage_seven_day_opus", "7d-opus", "", usage.SevenDayOpus)
	applyUsageBucket("usage_seven_day_cowork", "7d-cowork", "", usage.SevenDayCowork)
}

func parseReset(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func (p *Provider) readStats(path string, snap *core.UsageSnapshot) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading stats cache: %w", err)
	}

	var stats statsCache
	if err := json.Unmarshal(data, &stats); err != nil {
		return fmt.Errorf("parsing stats cache: %w", err)
	}

	if stats.TotalMessages > 0 {
		total := float64(stats.TotalMessages)
		snap.Metrics["total_messages"] = core.Metric{
			Used:   &total,
			Unit:   "messages",
			Window: "all-time",
		}
	}

	if stats.TotalSessions > 0 {
		total := float64(stats.TotalSessions)
		snap.Metrics["total_sessions"] = core.Metric{
			Used:   &total,
			Unit:   "sessions",
			Window: "all-time",
		}
	}

	if stats.TotalSpeculationTimeSavedMs > 0 {
		hoursSaved := float64(stats.TotalSpeculationTimeSavedMs) / float64(time.Hour/time.Millisecond)
		snap.Metrics["speculation_time_saved_hours"] = core.Metric{
			Used:   &hoursSaved,
			Unit:   "hours",
			Window: "all-time",
		}
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	weekStart := now.Add(-7 * 24 * time.Hour)
	var weeklyMessages int
	var weeklyToolCalls int
	var weeklySessions int
	for _, da := range stats.DailyActivity {
		snap.DailySeries["messages"] = append(snap.DailySeries["messages"], core.TimePoint{
			Date: da.Date, Value: float64(da.MessageCount),
		})
		snap.DailySeries["sessions"] = append(snap.DailySeries["sessions"], core.TimePoint{
			Date: da.Date, Value: float64(da.SessionCount),
		})
		snap.DailySeries["tool_calls"] = append(snap.DailySeries["tool_calls"], core.TimePoint{
			Date: da.Date, Value: float64(da.ToolCallCount),
		})

		if da.Date == today {
			msgs := float64(da.MessageCount)
			snap.Metrics["messages_today"] = core.Metric{Used: &msgs, Unit: "messages", Window: "1d"}
			tools := float64(da.ToolCallCount)
			snap.Metrics["tool_calls_today"] = core.Metric{Used: &tools, Unit: "calls", Window: "1d"}
			sessions := float64(da.SessionCount)
			snap.Metrics["sessions_today"] = core.Metric{Used: &sessions, Unit: "sessions", Window: "1d"}
		}

		if day, err := time.Parse("2006-01-02", da.Date); err == nil && (day.After(weekStart) || day.Equal(weekStart)) {
			weeklyMessages += da.MessageCount
			weeklyToolCalls += da.ToolCallCount
			weeklySessions += da.SessionCount
		}
	}

	if weeklyMessages > 0 {
		wm := float64(weeklyMessages)
		snap.Metrics["7d_messages"] = core.Metric{Used: &wm, Unit: "messages", Window: "rolling 7 days"}
	}
	if weeklyToolCalls > 0 {
		wt := float64(weeklyToolCalls)
		snap.Metrics["7d_tool_calls"] = core.Metric{Used: &wt, Unit: "calls", Window: "rolling 7 days"}
	}
	if weeklySessions > 0 {
		ws := float64(weeklySessions)
		snap.Metrics["7d_sessions"] = core.Metric{Used: &ws, Unit: "sessions", Window: "rolling 7 days"}
	}

	for _, dt := range stats.DailyModelTokens {
		totalDayTokens := float64(0)
		for model, tokens := range dt.TokensByModel {
			name := sanitizeModelName(model)
			key := fmt.Sprintf("tokens_%s", name)
			snap.DailySeries[key] = append(snap.DailySeries[key], core.TimePoint{Date: dt.Date, Value: float64(tokens)})
			totalDayTokens += float64(tokens)
		}
		snap.DailySeries["tokens_total"] = append(snap.DailySeries["tokens_total"], core.TimePoint{Date: dt.Date, Value: totalDayTokens})

		if dt.Date == today {
			for model, tokens := range dt.TokensByModel {
				t := float64(tokens)
				key := fmt.Sprintf("tokens_today_%s", sanitizeModelName(model))
				snap.Metrics[key] = core.Metric{Used: &t, Unit: "tokens", Window: "1d"}
			}
		}
	}

	var totalCostUSD float64
	for model, usage := range stats.ModelUsage {
		outTokens := float64(usage.OutputTokens)
		inTokens := float64(usage.InputTokens)
		name := sanitizeModelName(model)
		modelPrefix := "model_" + name

		setMetricMax(snap, modelPrefix+"_input_tokens", inTokens, "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_output_tokens", outTokens, "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_cached_tokens", float64(usage.CacheReadInputTokens), "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_cache_creation_tokens", float64(usage.CacheCreationInputTokens), "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_web_search_requests", float64(usage.WebSearchRequests), "requests", "all-time")
		setMetricMax(snap, modelPrefix+"_context_window_tokens", float64(usage.ContextWindow), "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_max_output_tokens", float64(usage.MaxOutputTokens), "tokens", "all-time")

		snap.Raw[fmt.Sprintf("model_%s_cache_read", name)] = fmt.Sprintf("%d tokens", usage.CacheReadInputTokens)
		snap.Raw[fmt.Sprintf("model_%s_cache_create", name)] = fmt.Sprintf("%d tokens", usage.CacheCreationInputTokens)
		if usage.WebSearchRequests > 0 {
			snap.Raw[fmt.Sprintf("model_%s_web_search_requests", name)] = fmt.Sprintf("%d", usage.WebSearchRequests)
		}
		if usage.ContextWindow > 0 {
			snap.Raw[fmt.Sprintf("model_%s_context_window", name)] = fmt.Sprintf("%d", usage.ContextWindow)
		}
		if usage.MaxOutputTokens > 0 {
			snap.Raw[fmt.Sprintf("model_%s_max_output_tokens", name)] = fmt.Sprintf("%d", usage.MaxOutputTokens)
		}

		if usage.CostUSD > 0 {
			totalCostUSD += usage.CostUSD
			setMetricMax(snap, modelPrefix+"_cost_usd", usage.CostUSD, "USD", "all-time")
		}

		rec := core.ModelUsageRecord{
			RawModelID:   model,
			RawSource:    "stats_cache",
			Window:       "all-time",
			InputTokens:  core.Float64Ptr(inTokens),
			OutputTokens: core.Float64Ptr(outTokens),
			TotalTokens:  core.Float64Ptr(inTokens + outTokens),
		}
		if usage.CacheReadInputTokens > 0 || usage.CacheCreationInputTokens > 0 {
			rec.CachedTokens = core.Float64Ptr(float64(usage.CacheReadInputTokens + usage.CacheCreationInputTokens))
		}
		if usage.CostUSD > 0 {
			rec.CostUSD = core.Float64Ptr(usage.CostUSD)
		}
		snap.AppendModelUsage(rec)
	}

	if totalCostUSD > 0 {
		cost := totalCostUSD
		snap.Metrics["total_cost_usd"] = core.Metric{Used: &cost, Unit: "USD", Window: "all-time"}
	}

	snap.Raw["stats_last_computed"] = stats.LastComputedDate
	if stats.FirstSessionDate != "" {
		snap.Raw["first_session"] = stats.FirstSessionDate
	}
	if stats.LongestSession != nil {
		if stats.LongestSession.Duration > 0 {
			minutes := float64(stats.LongestSession.Duration) / float64(time.Minute/time.Millisecond)
			snap.Metrics["longest_session_minutes"] = core.Metric{Used: &minutes, Unit: "minutes", Window: "all-time"}
		}
		if stats.LongestSession.MessageCount > 0 {
			msgs := float64(stats.LongestSession.MessageCount)
			snap.Metrics["longest_session_messages"] = core.Metric{Used: &msgs, Unit: "messages", Window: "all-time"}
		}
		if stats.LongestSession.SessionID != "" {
			snap.Raw["longest_session_id"] = stats.LongestSession.SessionID
		}
		if stats.LongestSession.Timestamp != "" {
			snap.Raw["longest_session_timestamp"] = stats.LongestSession.Timestamp
		}
	}
	if len(stats.HourCounts) > 0 {
		peakHour := ""
		peakCount := 0
		for h, c := range stats.HourCounts {
			if c > peakCount {
				peakHour = h
				peakCount = c
			}
		}
		if peakHour != "" {
			snap.Raw["peak_hour"] = peakHour
			snap.Raw["peak_hour_messages"] = fmt.Sprintf("%d", peakCount)
		}
	}

	return nil
}

func (p *Provider) readAccount(path string, snap *core.UsageSnapshot) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading account config: %w", err)
	}

	var acct accountConfig
	if err := json.Unmarshal(data, &acct); err != nil {
		return fmt.Errorf("parsing account config: %w", err)
	}

	if acct.OAuthAccount != nil {
		if acct.OAuthAccount.EmailAddress != "" {
			snap.Raw["account_email"] = acct.OAuthAccount.EmailAddress
		}
		if acct.OAuthAccount.DisplayName != "" {
			snap.Raw["account_name"] = acct.OAuthAccount.DisplayName
		}
		if acct.OAuthAccount.BillingType != "" {
			snap.Raw["billing_type"] = acct.OAuthAccount.BillingType
		}
		if acct.OAuthAccount.HasExtraUsageEnabled {
			snap.Raw["extra_usage_enabled"] = "true"
		}
		if acct.OAuthAccount.AccountCreatedAt != "" {
			snap.Raw["account_created_at"] = acct.OAuthAccount.AccountCreatedAt
		}
		if acct.OAuthAccount.SubscriptionCreatedAt != "" {
			snap.Raw["subscription_created_at"] = acct.OAuthAccount.SubscriptionCreatedAt
		}
		if acct.OAuthAccount.OrganizationUUID != "" {
			snap.Raw["organization_uuid"] = acct.OAuthAccount.OrganizationUUID
		}
	}

	if acct.HasAvailableSubscription {
		snap.Raw["subscription"] = "active"
	} else {
		snap.Raw["subscription"] = "none"
	}

	if acct.ClaudeCodeFirstTokenDate != "" {
		snap.Raw["claude_code_first_token_date"] = acct.ClaudeCodeFirstTokenDate
	}

	if acct.PenguinModeOrgEnabled {
		snap.Raw["penguin_mode_enabled"] = "true"
	}

	for orgID, access := range acct.S1MAccessCache {
		if access.HasAccess {
			shortID := orgID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			snap.Raw[fmt.Sprintf("s1m_access_%s", shortID)] = "true"
		}
	}

	snap.Raw["num_startups"] = fmt.Sprintf("%d", acct.NumStartups)
	if acct.InstallMethod != "" {
		snap.Raw["install_method"] = acct.InstallMethod
	}
	if acct.ClientDataCache != nil && acct.ClientDataCache.Timestamp > 0 {
		snap.Raw["client_data_cache_ts"] = strconv.FormatInt(acct.ClientDataCache.Timestamp, 10)
	}
	if len(acct.SkillUsage) > 0 {
		counts := make(map[string]int, len(acct.SkillUsage))
		for skill, usage := range acct.SkillUsage {
			counts[sanitizeModelName(skill)] = usage.UsageCount
		}
		snap.Raw["skill_usage"] = summarizeCountMap(counts, 6)
	}

	return nil
}

func (p *Provider) readSettings(path string, snap *core.UsageSnapshot) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading settings: %w", err)
	}

	var settings settingsConfig
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parsing settings: %w", err)
	}

	if settings.Model != "" {
		snap.Raw["active_model"] = settings.Model
	}
	if settings.AlwaysThinkingEnabled {
		snap.Raw["always_thinking"] = "true"
	}

	return nil
}
