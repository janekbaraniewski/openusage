package claude_code

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
	"github.com/samber/lo"
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "claude_code",
			Info: core.ProviderInfo{
				Name: "Claude Code CLI",
				Capabilities: []string{
					"local_stats", "daily_activity", "model_tokens",
					"account_info", "jsonl_conversations", "5h_billing_blocks",
					"cost_estimation", "burn_rate", "session_tracking",
				},
				DocURL: "https://code.claude.com/",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeLocal,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install Claude Code and authenticate in the CLI.",
					"Ensure Claude Code local stats/config files are readable.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

type statsCache struct {
	Version                     int                   `json:"version"`
	LastComputedDate            string                `json:"lastComputedDate"`
	DailyActivity               []dailyActivity       `json:"dailyActivity"`
	DailyModelTokens            []dailyTokens         `json:"dailyModelTokens"`
	ModelUsage                  map[string]modelUsage `json:"modelUsage"`
	TotalSessions               int                   `json:"totalSessions"`
	TotalMessages               int                   `json:"totalMessages"`
	TotalSpeculationTimeSavedMs int64                 `json:"totalSpeculationTimeSavedMs"`
	LongestSession              *longestSession       `json:"longestSession"`
	FirstSessionDate            string                `json:"firstSessionDate"`
	HourCounts                  map[string]int        `json:"hourCounts"`
}

type dailyActivity struct {
	Date          string `json:"date"`
	MessageCount  int    `json:"messageCount"`
	SessionCount  int    `json:"sessionCount"`
	ToolCallCount int    `json:"toolCallCount"`
}

type dailyTokens struct {
	Date          string         `json:"date"`
	TokensByModel map[string]int `json:"tokensByModel"`
}

type modelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	WebSearchRequests        int     `json:"webSearchRequests"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int     `json:"contextWindow"`
	MaxOutputTokens          int     `json:"maxOutputTokens"`
}

type longestSession struct {
	SessionID    string `json:"sessionId"`
	Duration     int64  `json:"duration"`
	MessageCount int    `json:"messageCount"`
	Timestamp    string `json:"timestamp"`
}

type accountConfig struct {
	HasAvailableSubscription bool                       `json:"hasAvailableSubscription"`
	OAuthAccount             *oauthAcct                 `json:"oauthAccount"`
	S1MAccessCache           map[string]s1mAccess       `json:"s1mAccessCache"`
	S1MNonSubscriberAccess   map[string]s1mAccess       `json:"s1mNonSubscriberAccessCache"`
	ClaudeCodeFirstTokenDate string                     `json:"claudeCodeFirstTokenDate"`
	SubscriptionNoticeCount  int                        `json:"subscriptionNoticeCount"`
	PenguinModeOrgEnabled    bool                       `json:"penguinModeOrgEnabled"`
	ClientDataCache          *clientDataCache           `json:"clientDataCache"`
	SkillUsage               map[string]skillUsageEntry `json:"skillUsage"`
	NumStartups              int                        `json:"numStartups"`
	InstallMethod            string                     `json:"installMethod"`
}

type oauthAcct struct {
	AccountUUID           string `json:"accountUuid"`
	EmailAddress          string `json:"emailAddress"`
	OrganizationUUID      string `json:"organizationUuid"`
	HasExtraUsageEnabled  bool   `json:"hasExtraUsageEnabled"`
	BillingType           string `json:"billingType"`
	DisplayName           string `json:"displayName"`
	AccountCreatedAt      string `json:"accountCreatedAt"`
	SubscriptionCreatedAt string `json:"subscriptionCreatedAt"`
}

type s1mAccess struct {
	HasAccess             bool  `json:"hasAccess"`
	HasAccessNotAsDefault bool  `json:"hasAccessNotAsDefault"`
	Timestamp             int64 `json:"timestamp"`
}

type clientDataCache struct {
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

type skillUsageEntry struct {
	UsageCount int   `json:"usageCount"`
	LastUsedAt int64 `json:"lastUsedAt"`
}

type settingsConfig struct {
	Model      string `json:"model"`
	StatusLine *struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"statusLine"`
	AlwaysThinkingEnabled bool `json:"alwaysThinkingEnabled"`
}

type jsonlEntry struct {
	Type      string    `json:"type"`
	SessionID string    `json:"sessionId"`
	Timestamp string    `json:"timestamp"`
	RequestID string    `json:"requestId,omitempty"`
	UUID      string    `json:"uuid,omitempty"`
	Message   *jsonlMsg `json:"message,omitempty"`
	Subtype   string    `json:"subtype,omitempty"`
	Version   string    `json:"version,omitempty"`
	CWD       string    `json:"cwd,omitempty"`
}

type jsonlMsg struct {
	ID         string         `json:"id,omitempty"`
	Model      string         `json:"model"`
	Role       string         `json:"role"`
	StopReason *string        `json:"stop_reason"`
	Usage      *jsonlUsage    `json:"usage,omitempty"`
	Content    []jsonlContent `json:"content,omitempty"`
}

type jsonlContent struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type jsonlUsage struct {
	InputTokens              int              `json:"input_tokens"`
	CacheCreationInputTokens int              `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int              `json:"cache_read_input_tokens"`
	OutputTokens             int              `json:"output_tokens"`
	ReasoningTokens          int              `json:"reasoning_tokens"`
	ServiceTier              string           `json:"service_tier"`
	InferenceGeo             string           `json:"inference_geo"`
	CacheCreation            *cacheBreakdown  `json:"cache_creation,omitempty"`
	ServerToolUse            *serverToolUsage `json:"server_tool_use,omitempty"`
}

type cacheBreakdown struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
}

type serverToolUsage struct {
	WebSearchRequests int `json:"web_search_requests"`
	WebFetchRequests  int `json:"web_fetch_requests"`
}

type pricing struct {
	InputPerMillion       float64
	OutputPerMillion      float64
	CacheReadPerMillion   float64
	CacheCreatePerMillion float64
}

var modelPricing = map[string]pricing{
	"opus": {
		InputPerMillion:       15.0,
		OutputPerMillion:      75.0,
		CacheReadPerMillion:   1.50,
		CacheCreatePerMillion: 18.75,
	},
	"sonnet": {
		InputPerMillion:       3.0,
		OutputPerMillion:      15.0,
		CacheReadPerMillion:   0.30,
		CacheCreatePerMillion: 3.75,
	},
	"haiku": {
		InputPerMillion:       0.80,
		OutputPerMillion:      4.0,
		CacheReadPerMillion:   0.08,
		CacheCreatePerMillion: 1.0,
	},
}

func findPricing(model string) pricing {
	lower := strings.ToLower(model)
	for _, family := range []string{"opus", "haiku", "sonnet"} {
		if strings.Contains(lower, family) {
			return modelPricing[family]
		}
	}
	return modelPricing["sonnet"]
}

func estimateCost(model string, u *jsonlUsage) float64 {
	if u == nil {
		return 0
	}
	p := findPricing(model)
	cost := float64(u.InputTokens) * p.InputPerMillion / 1_000_000
	cost += float64(u.OutputTokens) * p.OutputPerMillion / 1_000_000
	cost += float64(u.CacheReadInputTokens) * p.CacheReadPerMillion / 1_000_000
	cost += float64(u.CacheCreationInputTokens) * p.CacheCreatePerMillion / 1_000_000
	return cost
}

type modelUsageTotals struct {
	input       float64
	output      float64
	cached      float64
	cacheCreate float64
	cache5m     float64
	cache1h     float64
	reasoning   float64
	cost        float64
	webSearch   float64
	webFetch    float64
	sessions    float64
}

const (
	billingBlockDuration      = 5 * time.Hour
	maxModelUsageSummaryItems = 6
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

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.UsageSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   time.Now(),
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Raw:         make(map[string]string),
		Resets:      make(map[string]time.Time),
		DailySeries: make(map[string][]core.TimePoint),
	}

	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")
	if override, ok := acct.ExtraData["claude_dir"]; ok && override != "" {
		claudeDir = override
		home = filepath.Dir(claudeDir) // derive "home" from the override
	}

	statsPath := acct.Binary    // repurposed field — path to stats-cache.json
	accountPath := acct.BaseURL // repurposed field — path to .claude.json

	if accountPath == "" {
		accountPath = filepath.Join(home, ".claude.json")
	}

	var hasData bool

	statsCandidates := buildStatsCandidates(statsPath, claudeDir, home)
	snap.Raw["stats_candidates"] = strings.Join(statsCandidates, ", ")
	var statsErr error
	for _, candidate := range statsCandidates {
		if err := p.readStats(candidate, &snap); err != nil {
			statsErr = err
			continue
		}
		hasData = true
		snap.Raw["stats_path"] = candidate
		statsErr = nil
		break
	}
	if statsErr != nil {
		snap.Raw["stats_error"] = statsErr.Error()
	}

	if err := p.readAccount(accountPath, &snap); err != nil {
		snap.Raw["account_error"] = err.Error()
	} else {
		hasData = true
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := p.readSettings(settingsPath, &snap); err != nil {
		snap.Raw["settings_error"] = err.Error()
	}

	projectsDir := filepath.Join(claudeDir, "projects")
	newProjectsDir := filepath.Join(home, ".config", "claude", "projects")

	if err := p.readConversationJSONL(projectsDir, newProjectsDir, &snap); err != nil {
		snap.Raw["jsonl_error"] = err.Error()
	} else {
		hasData = true
	}

	if orgUUID, ok := snap.Raw["organization_uuid"]; ok && orgUUID != "" {
		if err := p.readUsageAPI(orgUUID, &snap); err != nil {
			snap.Raw["usage_api_error"] = err.Error()
		} else {
			hasData = true
		}
	}

	normalizeModelUsage(&snap)

	if !hasData {
		snap.Status = core.StatusError
		snap.Message = "No Claude Code stats data accessible"
		return snap, nil
	}

	snap.Message = "Claude Code CLI · costs are API-equivalent estimates, not subscription charges"
	return snap, nil
}

func (p *Provider) readUsageAPI(orgUUID string, snap *core.UsageSnapshot) error {
	cookies, err := getClaudeSessionCookies()
	if err != nil {
		return fmt.Errorf("cookie extraction: %w", err)
	}

	usage, err := fetchUsageAPI(orgUUID, cookies)
	if err != nil {
		return fmt.Errorf("API fetch: %w", err)
	}

	applyUsageResponse(usage, snap, time.Now())

	snap.Raw["usage_api_ok"] = "true"
	return nil
}

func applyUsageResponse(usage *usageResponse, snap *core.UsageSnapshot, now time.Time) {
	applyUsageBucket := func(metricKey, window, resetKey string, bucket *usageBucket) {
		if bucket == nil {
			return
		}

		util := bucket.Utilization
		limit := float64(100)
		if t, ok := parseReset(bucket.ResetsAt); ok {
			// Prevent stale "100%" (or other pre-reset values) from persisting
			// after reset boundary has already passed.
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
			snap.Metrics["messages_today"] = core.Metric{
				Used:   &msgs,
				Unit:   "messages",
				Window: "1d",
			}
			tools := float64(da.ToolCallCount)
			snap.Metrics["tool_calls_today"] = core.Metric{
				Used:   &tools,
				Unit:   "calls",
				Window: "1d",
			}
			sessions := float64(da.SessionCount)
			snap.Metrics["sessions_today"] = core.Metric{
				Used:   &sessions,
				Unit:   "sessions",
				Window: "1d",
			}
		}

		if day, err := time.Parse("2006-01-02", da.Date); err == nil && (day.After(weekStart) || day.Equal(weekStart)) {
			weeklyMessages += da.MessageCount
			weeklyToolCalls += da.ToolCallCount
			weeklySessions += da.SessionCount
		}
	}

	if weeklyMessages > 0 {
		wm := float64(weeklyMessages)
		snap.Metrics["7d_messages"] = core.Metric{
			Used:   &wm,
			Unit:   "messages",
			Window: "rolling 7 days",
		}
	}
	if weeklyToolCalls > 0 {
		wt := float64(weeklyToolCalls)
		snap.Metrics["7d_tool_calls"] = core.Metric{
			Used:   &wt,
			Unit:   "calls",
			Window: "rolling 7 days",
		}
	}
	if weeklySessions > 0 {
		ws := float64(weeklySessions)
		snap.Metrics["7d_sessions"] = core.Metric{
			Used:   &ws,
			Unit:   "sessions",
			Window: "rolling 7 days",
		}
	}

	for _, dt := range stats.DailyModelTokens {
		totalDayTokens := float64(0)
		for model, tokens := range dt.TokensByModel {
			name := sanitizeModelName(model)
			key := fmt.Sprintf("tokens_%s", name)
			snap.DailySeries[key] = append(snap.DailySeries[key], core.TimePoint{
				Date: dt.Date, Value: float64(tokens),
			})
			totalDayTokens += float64(tokens)
		}
		snap.DailySeries["tokens_total"] = append(snap.DailySeries["tokens_total"], core.TimePoint{
			Date: dt.Date, Value: totalDayTokens,
		})

		if dt.Date == today {
			for model, tokens := range dt.TokensByModel {
				t := float64(tokens)
				key := fmt.Sprintf("tokens_today_%s", sanitizeModelName(model))
				snap.Metrics[key] = core.Metric{
					Used:   &t,
					Unit:   "tokens",
					Window: "1d",
				}
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
		core.AppendModelUsageRecord(snap, rec)
	}

	if totalCostUSD > 0 {
		cost := totalCostUSD
		snap.Metrics["total_cost_usd"] = core.Metric{
			Used:   &cost,
			Unit:   "USD",
			Window: "all-time",
		}
	}

	snap.Raw["stats_last_computed"] = stats.LastComputedDate
	if stats.FirstSessionDate != "" {
		snap.Raw["first_session"] = stats.FirstSessionDate
	}
	if stats.LongestSession != nil {
		if stats.LongestSession.Duration > 0 {
			minutes := float64(stats.LongestSession.Duration) / float64(time.Minute/time.Millisecond)
			snap.Metrics["longest_session_minutes"] = core.Metric{
				Used:   &minutes,
				Unit:   "minutes",
				Window: "all-time",
			}
		}
		if stats.LongestSession.MessageCount > 0 {
			msgs := float64(stats.LongestSession.MessageCount)
			snap.Metrics["longest_session_messages"] = core.Metric{
				Used:   &msgs,
				Unit:   "messages",
				Window: "all-time",
			}
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

func (p *Provider) readConversationJSONL(projectsDir, altProjectsDir string, snap *core.UsageSnapshot) error {
	jsonlFiles := collectJSONLFiles(projectsDir)
	if altProjectsDir != "" {
		jsonlFiles = append(jsonlFiles, collectJSONLFiles(altProjectsDir)...)
	}
	jsonlFiles = dedupeStringSlice(jsonlFiles)
	sort.Strings(jsonlFiles)

	if len(jsonlFiles) == 0 {
		return fmt.Errorf("no JSONL conversation files found")
	}

	snap.Raw["jsonl_files_found"] = fmt.Sprintf("%d", len(jsonlFiles))

	now := time.Now()
	today := now.Format("2006-01-02")
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := now.Add(-7 * 24 * time.Hour)

	var (
		todayCostUSD      float64
		todayInputTokens  int
		todayOutputTokens int
		todayCacheRead    int
		todayCacheCreate  int
		todayMessages     int
		todayModels       = make(map[string]bool)

		weeklyCostUSD      float64
		weeklyInputTokens  int
		weeklyOutputTokens int
		weeklyMessages     int

		currentBlockStart time.Time
		currentBlockEnd   time.Time
		blockCostUSD      float64
		blockInputTokens  int
		blockOutputTokens int
		blockCacheRead    int
		blockCacheCreate  int
		blockMessages     int
		blockModels       = make(map[string]bool)
		inCurrentBlock    bool

		allTimeCostUSD float64
		allTimeEntries int
	)

	blockStartCandidates := []time.Time{}

	type parsedUsage struct {
		timestamp  time.Time
		model      string
		usage      *jsonlUsage
		requestID  string
		messageID  string
		sessionID  string
		cwd        string
		sourcePath string
		content    []jsonlContent
	}

	var allUsages []parsedUsage
	modelTotals := make(map[string]*modelUsageTotals)
	clientTotals := make(map[string]*modelUsageTotals)
	projectTotals := make(map[string]*modelUsageTotals)
	agentTotals := make(map[string]*modelUsageTotals)
	serviceTierTotals := make(map[string]float64)
	inferenceGeoTotals := make(map[string]float64)
	toolUsageCounts := make(map[string]int)
	clientSessions := make(map[string]map[string]bool)
	projectSessions := make(map[string]map[string]bool)
	agentSessions := make(map[string]map[string]bool)
	seenUsageKeys := make(map[string]bool)
	seenToolKeys := make(map[string]bool)
	dailyClientTokens := make(map[string]map[string]float64)
	dailyTokenTotals := make(map[string]int)
	dailyMessages := make(map[string]int)
	dailyCost := make(map[string]float64)
	dailyModelTokens := make(map[string]map[string]int)
	todaySessions := make(map[string]bool)
	weeklySessions := make(map[string]bool)
	var (
		todayCacheCreate5m   int
		todayCacheCreate1h   int
		todayReasoning       int
		todayToolCalls       int
		todayWebSearch       int
		todayWebFetch        int
		weeklyCacheRead      int
		weeklyCacheCreate    int
		weeklyCacheCreate5m  int
		weeklyCacheCreate1h  int
		weeklyReasoning      int
		weeklyToolCalls      int
		weeklyWebSearch      int
		weeklyWebFetch       int
		allTimeInputTokens   int
		allTimeOutputTokens  int
		allTimeCacheRead     int
		allTimeCacheCreate   int
		allTimeCacheCreate5m int
		allTimeCacheCreate1h int
		allTimeReasoning     int
		allTimeToolCalls     int
		allTimeWebSearch     int
		allTimeWebFetch      int
	)

	ensureTotals := func(m map[string]*modelUsageTotals, key string) *modelUsageTotals {
		if _, ok := m[key]; !ok {
			m[key] = &modelUsageTotals{}
		}
		return m[key]
	}
	ensureSessionSet := func(m map[string]map[string]bool, key string) map[string]bool {
		if _, ok := m[key]; !ok {
			m[key] = make(map[string]bool)
		}
		return m[key]
	}
	normalizeAgent := func(path string) string {
		if strings.Contains(path, string(filepath.Separator)+"subagents"+string(filepath.Separator)) {
			return "subagents"
		}
		return "main"
	}
	normalizeProject := func(cwd, sourcePath string) string {
		if cwd != "" {
			base := filepath.Base(cwd)
			if base != "" && base != "." && base != string(filepath.Separator) {
				return sanitizeModelName(base)
			}
			return sanitizeModelName(cwd)
		}
		dir := filepath.Base(filepath.Dir(sourcePath))
		if dir == "" || dir == "." {
			return "unknown"
		}
		return sanitizeModelName(dir)
	}
	usageDedupKey := func(u parsedUsage) string {
		if u.requestID != "" {
			return "req:" + u.requestID
		}
		if u.messageID != "" {
			return "msg:" + u.messageID
		}
		if u.usage == nil {
			return ""
		}
		return fmt.Sprintf("%s|%s|%d|%d|%d|%d|%d",
			u.sessionID,
			u.timestamp.UTC().Format(time.RFC3339Nano),
			u.usage.InputTokens,
			u.usage.OutputTokens,
			u.usage.CacheReadInputTokens,
			u.usage.CacheCreationInputTokens,
			u.usage.ReasoningTokens,
		)
	}
	toolDedupKey := func(u parsedUsage, idx int, item jsonlContent) string {
		base := u.requestID
		if base == "" {
			base = u.messageID
		}
		if base == "" {
			base = u.sessionID + "|" + u.timestamp.UTC().Format(time.RFC3339Nano)
		}
		if item.ID != "" {
			return base + "|tool|" + item.ID
		}
		name := strings.ToLower(strings.TrimSpace(item.Name))
		if name == "" {
			name = "unknown"
		}
		return fmt.Sprintf("%s|tool|%s|%d", base, name, idx)
	}

	for _, fpath := range jsonlFiles {
		entries := parseJSONLFile(fpath)
		for _, entry := range entries {
			if entry.Type != "assistant" || entry.Message == nil {
				continue
			}
			ts, ok := parseJSONLTimestamp(entry.Timestamp)
			if !ok {
				continue
			}
			model := entry.Message.Model
			if model == "" {
				model = "unknown"
			}
			allUsages = append(allUsages, parsedUsage{
				timestamp:  ts,
				model:      model,
				usage:      entry.Message.Usage,
				requestID:  entry.RequestID,
				messageID:  entry.Message.ID,
				sessionID:  entry.SessionID,
				cwd:        entry.CWD,
				sourcePath: fpath,
				content:    entry.Message.Content,
			})
		}
	}

	sort.Slice(allUsages, func(i, j int) bool {
		return allUsages[i].timestamp.Before(allUsages[j].timestamp)
	})

	seenForBlock := make(map[string]bool)
	for _, u := range allUsages {
		if u.usage == nil {
			continue
		}
		key := usageDedupKey(u)
		if key != "" {
			if seenForBlock[key] {
				continue
			}
			seenForBlock[key] = true
		}
		if currentBlockEnd.IsZero() || u.timestamp.After(currentBlockEnd) {
			currentBlockStart = floorToHour(u.timestamp)
			currentBlockEnd = currentBlockStart.Add(billingBlockDuration)
			blockStartCandidates = append(blockStartCandidates, currentBlockStart)
		}
	}

	inCurrentBlock = false
	if !currentBlockEnd.IsZero() && now.Before(currentBlockEnd) && (now.Equal(currentBlockStart) || now.After(currentBlockStart)) {
		inCurrentBlock = true
	}

	for _, u := range allUsages {
		for idx, item := range u.content {
			if item.Type != "tool_use" {
				continue
			}
			toolKey := toolDedupKey(u, idx, item)
			if seenToolKeys[toolKey] {
				continue
			}
			seenToolKeys[toolKey] = true
			toolName := strings.ToLower(strings.TrimSpace(item.Name))
			if toolName == "" {
				toolName = "unknown"
			}
			toolUsageCounts[toolName]++
			allTimeToolCalls++

			if u.timestamp.After(todayStart) || u.timestamp.Equal(todayStart) {
				todayToolCalls++
			}
			if u.timestamp.After(weekStart) || u.timestamp.Equal(weekStart) {
				weeklyToolCalls++
			}
		}

		if u.usage == nil {
			continue
		}
		usageKey := usageDedupKey(u)
		if usageKey != "" && seenUsageKeys[usageKey] {
			continue
		}
		if usageKey != "" {
			seenUsageKeys[usageKey] = true
		}

		modelID := sanitizeModelName(u.model)
		modelTotalsEntry := ensureTotals(modelTotals, modelID)
		projectID := normalizeProject(u.cwd, u.sourcePath)
		clientID := projectID
		clientTotalsEntry := ensureTotals(clientTotals, clientID)
		projectTotalsEntry := ensureTotals(projectTotals, projectID)
		agentID := normalizeAgent(u.sourcePath)
		agentTotalsEntry := ensureTotals(agentTotals, agentID)

		if u.sessionID != "" {
			ensureSessionSet(clientSessions, clientID)[u.sessionID] = true
			ensureSessionSet(projectSessions, projectID)[u.sessionID] = true
			ensureSessionSet(agentSessions, agentID)[u.sessionID] = true
			if u.timestamp.After(todayStart) || u.timestamp.Equal(todayStart) {
				todaySessions[u.sessionID] = true
			}
			if u.timestamp.After(weekStart) || u.timestamp.Equal(weekStart) {
				weeklySessions[u.sessionID] = true
			}
		}

		cost := estimateCost(u.model, u.usage)
		allTimeCostUSD += cost
		allTimeEntries++
		modelTotalsEntry.input += float64(u.usage.InputTokens)
		modelTotalsEntry.output += float64(u.usage.OutputTokens)
		modelTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
		modelTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		modelTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
		modelTotalsEntry.cost += cost
		if u.usage.CacheCreation != nil {
			modelTotalsEntry.cache5m += float64(u.usage.CacheCreation.Ephemeral5mInputTokens)
			modelTotalsEntry.cache1h += float64(u.usage.CacheCreation.Ephemeral1hInputTokens)
			allTimeCacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
			allTimeCacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
		}
		if u.usage.ServerToolUse != nil {
			modelTotalsEntry.webSearch += float64(u.usage.ServerToolUse.WebSearchRequests)
			modelTotalsEntry.webFetch += float64(u.usage.ServerToolUse.WebFetchRequests)
		}

		tokenVolume := float64(u.usage.InputTokens + u.usage.OutputTokens + u.usage.CacheReadInputTokens + u.usage.CacheCreationInputTokens + u.usage.ReasoningTokens)
		clientTotalsEntry.input += float64(u.usage.InputTokens)
		clientTotalsEntry.output += float64(u.usage.OutputTokens)
		clientTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
		clientTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		clientTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
		clientTotalsEntry.cost += cost
		clientTotalsEntry.sessions = float64(len(clientSessions[clientID]))

		projectTotalsEntry.input += float64(u.usage.InputTokens)
		projectTotalsEntry.output += float64(u.usage.OutputTokens)
		projectTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
		projectTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		projectTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
		projectTotalsEntry.cost += cost
		projectTotalsEntry.sessions = float64(len(projectSessions[projectID]))

		agentTotalsEntry.input += float64(u.usage.InputTokens)
		agentTotalsEntry.output += float64(u.usage.OutputTokens)
		agentTotalsEntry.cached += float64(u.usage.CacheReadInputTokens)
		agentTotalsEntry.cacheCreate += float64(u.usage.CacheCreationInputTokens)
		agentTotalsEntry.reasoning += float64(u.usage.ReasoningTokens)
		agentTotalsEntry.cost += cost
		agentTotalsEntry.sessions = float64(len(agentSessions[agentID]))

		allTimeInputTokens += u.usage.InputTokens
		allTimeOutputTokens += u.usage.OutputTokens
		allTimeCacheRead += u.usage.CacheReadInputTokens
		allTimeCacheCreate += u.usage.CacheCreationInputTokens
		allTimeReasoning += u.usage.ReasoningTokens
		if u.usage.ServerToolUse != nil {
			allTimeWebSearch += u.usage.ServerToolUse.WebSearchRequests
			allTimeWebFetch += u.usage.ServerToolUse.WebFetchRequests
		}

		day := u.timestamp.Format("2006-01-02")
		dailyTokenTotals[day] += u.usage.InputTokens + u.usage.OutputTokens
		dailyMessages[day]++
		dailyCost[day] += cost
		if dailyModelTokens[day] == nil {
			dailyModelTokens[day] = make(map[string]int)
		}
		dailyModelTokens[day][u.model] += u.usage.InputTokens + u.usage.OutputTokens
		if dailyClientTokens[day] == nil {
			dailyClientTokens[day] = make(map[string]float64)
		}
		dailyClientTokens[day][clientID] += tokenVolume

		if tier := strings.ToLower(strings.TrimSpace(u.usage.ServiceTier)); tier != "" {
			serviceTierTotals[tier] += tokenVolume
		}
		if geo := strings.ToLower(strings.TrimSpace(u.usage.InferenceGeo)); geo != "" {
			inferenceGeoTotals[geo] += tokenVolume
		}

		if u.timestamp.After(todayStart) || u.timestamp.Equal(todayStart) {
			todayCostUSD += cost
			todayInputTokens += u.usage.InputTokens
			todayOutputTokens += u.usage.OutputTokens
			todayCacheRead += u.usage.CacheReadInputTokens
			todayCacheCreate += u.usage.CacheCreationInputTokens
			todayReasoning += u.usage.ReasoningTokens
			if u.usage.CacheCreation != nil {
				todayCacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
				todayCacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
			}
			if u.usage.ServerToolUse != nil {
				todayWebSearch += u.usage.ServerToolUse.WebSearchRequests
				todayWebFetch += u.usage.ServerToolUse.WebFetchRequests
			}
			todayMessages++
			todayModels[modelID] = true
		}

		if u.timestamp.After(weekStart) || u.timestamp.Equal(weekStart) {
			weeklyCostUSD += cost
			weeklyInputTokens += u.usage.InputTokens
			weeklyOutputTokens += u.usage.OutputTokens
			weeklyCacheRead += u.usage.CacheReadInputTokens
			weeklyCacheCreate += u.usage.CacheCreationInputTokens
			weeklyReasoning += u.usage.ReasoningTokens
			if u.usage.CacheCreation != nil {
				weeklyCacheCreate5m += u.usage.CacheCreation.Ephemeral5mInputTokens
				weeklyCacheCreate1h += u.usage.CacheCreation.Ephemeral1hInputTokens
			}
			if u.usage.ServerToolUse != nil {
				weeklyWebSearch += u.usage.ServerToolUse.WebSearchRequests
				weeklyWebFetch += u.usage.ServerToolUse.WebFetchRequests
			}
			weeklyMessages++
		}

		if inCurrentBlock && (u.timestamp.After(currentBlockStart) || u.timestamp.Equal(currentBlockStart)) && u.timestamp.Before(currentBlockEnd) {
			blockCostUSD += cost
			blockInputTokens += u.usage.InputTokens
			blockOutputTokens += u.usage.OutputTokens
			blockCacheRead += u.usage.CacheReadInputTokens
			blockCacheCreate += u.usage.CacheCreationInputTokens
			blockMessages++
			blockModels[modelID] = true
		}
	}

	for model, totals := range modelTotals {
		modelPrefix := "model_" + model
		setMetricMax(snap, modelPrefix+"_input_tokens", totals.input, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_output_tokens", totals.output, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cached_tokens", totals.cached, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_tokens", totals.cacheCreate, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_5m_tokens", totals.cache5m, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cache_creation_1h_tokens", totals.cache1h, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_reasoning_tokens", totals.reasoning, "tokens", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_web_search_requests", totals.webSearch, "requests", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_web_fetch_requests", totals.webFetch, "requests", "all-time estimate")
		setMetricMax(snap, modelPrefix+"_cost_usd", totals.cost, "USD", "all-time estimate")
	}

	for client, totals := range clientTotals {
		key := "client_" + client
		setMetricMax(snap, key+"_input_tokens", totals.input, "tokens", "all-time")
		setMetricMax(snap, key+"_output_tokens", totals.output, "tokens", "all-time")
		setMetricMax(snap, key+"_cached_tokens", totals.cached, "tokens", "all-time")
		setMetricMax(snap, key+"_reasoning_tokens", totals.reasoning, "tokens", "all-time")
		setMetricMax(snap, key+"_total_tokens", totals.input+totals.output+totals.cached+totals.cacheCreate+totals.reasoning, "tokens", "all-time")
		setMetricMax(snap, key+"_sessions", totals.sessions, "sessions", "all-time")
	}

	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	dates := lo.Keys(dailyTokenTotals)
	sort.Strings(dates)

	if len(snap.DailySeries["messages"]) == 0 && len(dates) > 0 {
		for _, d := range dates {
			snap.DailySeries["messages"] = append(snap.DailySeries["messages"], core.TimePoint{Date: d, Value: float64(dailyMessages[d])})
			snap.DailySeries["tokens_total"] = append(snap.DailySeries["tokens_total"], core.TimePoint{Date: d, Value: float64(dailyTokenTotals[d])})
			snap.DailySeries["cost"] = append(snap.DailySeries["cost"], core.TimePoint{Date: d, Value: dailyCost[d]})
		}

		allModels := make(map[string]int64)
		for _, dm := range dailyModelTokens {
			for model, tokens := range dm {
				allModels[model] += int64(tokens)
			}
		}
		type mVol struct {
			name  string
			total int64
		}
		var mv []mVol
		for m, t := range allModels {
			mv = append(mv, mVol{m, t})
		}
		sort.Slice(mv, func(i, j int) bool { return mv[i].total > mv[j].total })
		limit := 5
		if len(mv) < limit {
			limit = len(mv)
		}
		for i := 0; i < limit; i++ {
			model := mv[i].name
			key := fmt.Sprintf("tokens_%s", sanitizeModelName(model))
			for _, d := range dates {
				tokens := dailyModelTokens[d][model]
				snap.DailySeries[key] = append(snap.DailySeries[key],
					core.TimePoint{Date: d, Value: float64(tokens)})
			}
		}
	}

	if len(dates) > 0 {
		clientNames := make(map[string]bool)
		for _, byClient := range dailyClientTokens {
			for client := range byClient {
				clientNames[client] = true
			}
		}
		for client := range clientNames {
			key := "tokens_client_" + client
			for _, d := range dates {
				snap.DailySeries[key] = append(snap.DailySeries[key], core.TimePoint{
					Date:  d,
					Value: dailyClientTokens[d][client],
				})
			}
		}
	}

	if todayCostUSD > 0 {
		snap.Metrics["today_api_cost"] = core.Metric{
			Used:   floatPtr(todayCostUSD),
			Unit:   "USD",
			Window: "since midnight",
		}
	}
	if todayInputTokens > 0 {
		in := float64(todayInputTokens)
		snap.Metrics["today_input_tokens"] = core.Metric{
			Used:   &in,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayOutputTokens > 0 {
		out := float64(todayOutputTokens)
		snap.Metrics["today_output_tokens"] = core.Metric{
			Used:   &out,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayCacheRead > 0 {
		cacheRead := float64(todayCacheRead)
		snap.Metrics["today_cache_read_tokens"] = core.Metric{
			Used:   &cacheRead,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayCacheCreate > 0 {
		cacheCreate := float64(todayCacheCreate)
		snap.Metrics["today_cache_create_tokens"] = core.Metric{
			Used:   &cacheCreate,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayMessages > 0 {
		msgs := float64(todayMessages)
		setMetricMax(snap, "messages_today", msgs, "messages", "since midnight")
	}
	if len(todaySessions) > 0 {
		setMetricMax(snap, "sessions_today", float64(len(todaySessions)), "sessions", "since midnight")
	}
	if todayToolCalls > 0 {
		setMetricMax(snap, "tool_calls_today", float64(todayToolCalls), "calls", "since midnight")
	}
	if todayReasoning > 0 {
		v := float64(todayReasoning)
		snap.Metrics["today_reasoning_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayCacheCreate5m > 0 {
		v := float64(todayCacheCreate5m)
		snap.Metrics["today_cache_create_5m_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayCacheCreate1h > 0 {
		v := float64(todayCacheCreate1h)
		snap.Metrics["today_cache_create_1h_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "since midnight",
		}
	}
	if todayWebSearch > 0 {
		v := float64(todayWebSearch)
		snap.Metrics["today_web_search_requests"] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: "since midnight",
		}
	}
	if todayWebFetch > 0 {
		v := float64(todayWebFetch)
		snap.Metrics["today_web_fetch_requests"] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: "since midnight",
		}
	}

	if weeklyCostUSD > 0 {
		snap.Metrics["7d_api_cost"] = core.Metric{
			Used:   floatPtr(weeklyCostUSD),
			Unit:   "USD",
			Window: "rolling 7 days",
		}
	}
	if weeklyMessages > 0 {
		wm := float64(weeklyMessages)
		snap.Metrics["7d_messages"] = core.Metric{
			Used:   &wm,
			Unit:   "messages",
			Window: "rolling 7 days",
		}
		wIn := float64(weeklyInputTokens)
		snap.Metrics["7d_input_tokens"] = core.Metric{
			Used:   &wIn,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
		wOut := float64(weeklyOutputTokens)
		snap.Metrics["7d_output_tokens"] = core.Metric{
			Used:   &wOut,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyCacheRead > 0 {
		v := float64(weeklyCacheRead)
		snap.Metrics["7d_cache_read_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyCacheCreate > 0 {
		v := float64(weeklyCacheCreate)
		snap.Metrics["7d_cache_create_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyCacheCreate5m > 0 {
		v := float64(weeklyCacheCreate5m)
		snap.Metrics["7d_cache_create_5m_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyCacheCreate1h > 0 {
		v := float64(weeklyCacheCreate1h)
		snap.Metrics["7d_cache_create_1h_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyReasoning > 0 {
		v := float64(weeklyReasoning)
		snap.Metrics["7d_reasoning_tokens"] = core.Metric{
			Used:   &v,
			Unit:   "tokens",
			Window: "rolling 7 days",
		}
	}
	if weeklyToolCalls > 0 {
		setMetricMax(snap, "7d_tool_calls", float64(weeklyToolCalls), "calls", "rolling 7 days")
	}
	if weeklyWebSearch > 0 {
		v := float64(weeklyWebSearch)
		snap.Metrics["7d_web_search_requests"] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: "rolling 7 days",
		}
	}
	if weeklyWebFetch > 0 {
		v := float64(weeklyWebFetch)
		snap.Metrics["7d_web_fetch_requests"] = core.Metric{
			Used:   &v,
			Unit:   "requests",
			Window: "rolling 7 days",
		}
	}
	if len(weeklySessions) > 0 {
		setMetricMax(snap, "7d_sessions", float64(len(weeklySessions)), "sessions", "rolling 7 days")
	}

	if todayMessages > 0 {
		snap.Raw["jsonl_today_date"] = today
		snap.Raw["jsonl_today_messages"] = fmt.Sprintf("%d", todayMessages)
		snap.Raw["jsonl_today_input_tokens"] = fmt.Sprintf("%d", todayInputTokens)
		snap.Raw["jsonl_today_output_tokens"] = fmt.Sprintf("%d", todayOutputTokens)
		snap.Raw["jsonl_today_cache_read_tokens"] = fmt.Sprintf("%d", todayCacheRead)
		snap.Raw["jsonl_today_cache_create_tokens"] = fmt.Sprintf("%d", todayCacheCreate)
		snap.Raw["jsonl_today_reasoning_tokens"] = fmt.Sprintf("%d", todayReasoning)
		snap.Raw["jsonl_today_web_search_requests"] = fmt.Sprintf("%d", todayWebSearch)
		snap.Raw["jsonl_today_web_fetch_requests"] = fmt.Sprintf("%d", todayWebFetch)

		models := lo.Keys(todayModels)
		sort.Strings(models)
		snap.Raw["jsonl_today_models"] = strings.Join(models, ", ")
	}

	if inCurrentBlock {
		snap.Metrics["5h_block_cost"] = core.Metric{
			Used:   floatPtr(blockCostUSD),
			Unit:   "USD",
			Window: fmt.Sprintf("%s – %s", currentBlockStart.Format("15:04"), currentBlockEnd.Format("15:04")),
		}

		blockIn := float64(blockInputTokens)
		snap.Metrics["5h_block_input"] = core.Metric{
			Used:   &blockIn,
			Unit:   "tokens",
			Window: "current 5h block",
		}

		blockOut := float64(blockOutputTokens)
		snap.Metrics["5h_block_output"] = core.Metric{
			Used:   &blockOut,
			Unit:   "tokens",
			Window: "current 5h block",
		}

		blockMsgs := float64(blockMessages)
		snap.Metrics["5h_block_msgs"] = core.Metric{
			Used:   &blockMsgs,
			Unit:   "messages",
			Window: "current 5h block",
		}
		if blockCacheRead > 0 {
			setMetricMax(snap, "5h_block_cache_read_tokens", float64(blockCacheRead), "tokens", "current 5h block")
		}
		if blockCacheCreate > 0 {
			setMetricMax(snap, "5h_block_cache_create_tokens", float64(blockCacheCreate), "tokens", "current 5h block")
		}

		remaining := currentBlockEnd.Sub(now)
		if remaining > 0 {
			snap.Resets["billing_block"] = currentBlockEnd
			snap.Raw["block_time_remaining"] = fmt.Sprintf("%s", remaining.Round(time.Minute))

			elapsed := now.Sub(currentBlockStart)
			progress := math.Min(elapsed.Seconds()/billingBlockDuration.Seconds()*100, 100)
			snap.Raw["block_progress_pct"] = fmt.Sprintf("%.0f", progress)
		}

		snap.Raw["block_start"] = currentBlockStart.Format(time.RFC3339)
		snap.Raw["block_end"] = currentBlockEnd.Format(time.RFC3339)

		blockModelList := lo.Keys(blockModels)
		sort.Strings(blockModelList)
		snap.Raw["block_models"] = strings.Join(blockModelList, ", ")

		elapsed := now.Sub(currentBlockStart)
		if elapsed > time.Minute && blockCostUSD > 0 {
			burnRate := blockCostUSD / elapsed.Hours()
			snap.Metrics["burn_rate"] = core.Metric{
				Used:   floatPtr(burnRate),
				Unit:   "USD/h",
				Window: "current 5h block",
			}
			snap.Raw["burn_rate"] = fmt.Sprintf("$%.2f/hour", burnRate)
		}
	}

	if allTimeCostUSD > 0 {
		snap.Metrics["all_time_api_cost"] = core.Metric{
			Used:   floatPtr(allTimeCostUSD),
			Unit:   "USD",
			Window: "all-time estimate",
		}
	}
	if allTimeInputTokens > 0 {
		setMetricMax(snap, "all_time_input_tokens", float64(allTimeInputTokens), "tokens", "all-time estimate")
	}
	if allTimeOutputTokens > 0 {
		setMetricMax(snap, "all_time_output_tokens", float64(allTimeOutputTokens), "tokens", "all-time estimate")
	}
	if allTimeCacheRead > 0 {
		setMetricMax(snap, "all_time_cache_read_tokens", float64(allTimeCacheRead), "tokens", "all-time estimate")
	}
	if allTimeCacheCreate > 0 {
		setMetricMax(snap, "all_time_cache_create_tokens", float64(allTimeCacheCreate), "tokens", "all-time estimate")
	}
	if allTimeCacheCreate5m > 0 {
		setMetricMax(snap, "all_time_cache_create_5m_tokens", float64(allTimeCacheCreate5m), "tokens", "all-time estimate")
	}
	if allTimeCacheCreate1h > 0 {
		setMetricMax(snap, "all_time_cache_create_1h_tokens", float64(allTimeCacheCreate1h), "tokens", "all-time estimate")
	}
	if allTimeReasoning > 0 {
		setMetricMax(snap, "all_time_reasoning_tokens", float64(allTimeReasoning), "tokens", "all-time estimate")
	}
	if allTimeToolCalls > 0 {
		setMetricMax(snap, "all_time_tool_calls", float64(allTimeToolCalls), "calls", "all-time estimate")
	}
	for toolName, count := range toolUsageCounts {
		if count <= 0 {
			continue
		}
		setMetricMax(snap, "tool_"+sanitizeModelName(toolName), float64(count), "calls", "all-time estimate")
	}
	if allTimeWebSearch > 0 {
		setMetricMax(snap, "all_time_web_search_requests", float64(allTimeWebSearch), "requests", "all-time estimate")
	}
	if allTimeWebFetch > 0 {
		setMetricMax(snap, "all_time_web_fetch_requests", float64(allTimeWebFetch), "requests", "all-time estimate")
	}

	snap.Raw["tool_usage"] = summarizeCountMap(toolUsageCounts, 6)
	snap.Raw["project_usage"] = summarizeTotalsMap(projectTotals, true, 6)
	snap.Raw["agent_usage"] = summarizeTotalsMap(agentTotals, false, 4)
	snap.Raw["service_tier_usage"] = summarizeFloatMap(serviceTierTotals, "tok", 4)
	snap.Raw["inference_geo_usage"] = summarizeFloatMap(inferenceGeoTotals, "tok", 4)
	if allTimeCacheRead > 0 || allTimeCacheCreate > 0 {
		snap.Raw["cache_usage"] = fmt.Sprintf("read %s · create %s (1h %s, 5m %s)",
			shortTokenCount(float64(allTimeCacheRead)),
			shortTokenCount(float64(allTimeCacheCreate)),
			shortTokenCount(float64(allTimeCacheCreate1h)),
			shortTokenCount(float64(allTimeCacheCreate5m)),
		)
	}
	snap.Raw["project_count"] = fmt.Sprintf("%d", len(projectTotals))
	snap.Raw["tool_count"] = fmt.Sprintf("%d", len(toolUsageCounts))

	snap.Raw["jsonl_total_entries"] = fmt.Sprintf("%d", allTimeEntries)
	snap.Raw["jsonl_total_blocks"] = fmt.Sprintf("%d", len(blockStartCandidates))
	snap.Raw["jsonl_unique_requests"] = fmt.Sprintf("%d", len(seenUsageKeys))
	buildModelUsageSummaryRaw(snap)

	return nil
}

func parseJSONLTimestamp(raw string) (time.Time, bool) {
	t, err := shared.ParseTimestampString(raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func dedupeStringSlice(items []string) []string {
	return lo.Uniq(lo.Compact(items))
}

func summarizeCountMap(values map[string]int, limit int) string {
	type entry struct {
		name  string
		value int
	}
	entries := make([]entry, 0, len(values))
	for name, value := range values {
		if value <= 0 {
			continue
		}
		entries = append(entries, entry{name: name, value: value})
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].value == entries[j].value {
			return entries[i].name < entries[j].name
		}
		return entries[i].value > entries[j].value
	})
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		name := strings.ReplaceAll(entries[i].name, "_", "-")
		parts = append(parts, fmt.Sprintf("%s %d", name, entries[i].value))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func summarizeFloatMap(values map[string]float64, unit string, limit int) string {
	type entry struct {
		name  string
		value float64
	}
	entries := make([]entry, 0, len(values))
	for name, value := range values {
		if value <= 0 {
			continue
		}
		entries = append(entries, entry{name: name, value: value})
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].value == entries[j].value {
			return entries[i].name < entries[j].name
		}
		return entries[i].value > entries[j].value
	})
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		name := strings.ReplaceAll(entries[i].name, "_", "-")
		value := shortTokenCount(entries[i].value)
		if unit != "" {
			value += " " + unit
		}
		parts = append(parts, fmt.Sprintf("%s %s", name, value))
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func summarizeTotalsMap(values map[string]*modelUsageTotals, preferCost bool, limit int) string {
	type entry struct {
		name   string
		tokens float64
		cost   float64
	}
	entries := make([]entry, 0, len(values))
	totalCost := 0.0
	for name, totals := range values {
		if totals == nil {
			continue
		}
		tokens := totals.input + totals.output + totals.cached + totals.cacheCreate + totals.reasoning
		cost := totals.cost
		if tokens <= 0 && cost <= 0 {
			continue
		}
		totalCost += cost
		entries = append(entries, entry{name: name, tokens: tokens, cost: cost})
	}
	if len(entries) == 0 {
		return ""
	}
	useCost := preferCost && totalCost > 0
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].tokens
		right := entries[j].tokens
		if useCost {
			left = entries[i].cost
			right = entries[j].cost
		}
		if left == right {
			return entries[i].name < entries[j].name
		}
		return left > right
	})
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		name := strings.ReplaceAll(entries[i].name, "_", "-")
		if useCost {
			parts = append(parts, fmt.Sprintf("%s %s %s tok", name, formatUSDSummary(entries[i].cost), shortTokenCount(entries[i].tokens)))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s tok", name, shortTokenCount(entries[i].tokens)))
		}
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}
	return strings.Join(parts, ", ")
}

func collectJSONLFiles(dir string) []string {
	var files []string
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return files
	}

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})

	return files
}

func parseJSONLFile(path string) []jsonlEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []jsonlEntry
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max line size

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry jsonlEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, entry)
	}

	return entries
}

func sanitizeModelName(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "unknown"
	}

	result := make([]byte, 0, len(model))
	for _, c := range model {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			result = append(result, byte(c))
		} else {
			result = append(result, '_')
		}
	}

	out := strings.Trim(string(result), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func floatPtr(v float64) *float64 {
	return &v
}

func setMetricMax(snap *core.UsageSnapshot, key string, value float64, unit, window string) {
	if value <= 0 {
		return
	}
	if existing, ok := snap.Metrics[key]; ok && existing.Used != nil && *existing.Used >= value {
		return
	}
	v := value
	snap.Metrics[key] = core.Metric{
		Used:   &v,
		Unit:   unit,
		Window: window,
	}
}

func normalizeModelUsage(snap *core.UsageSnapshot) {
	modelTotals := make(map[string]*modelUsageTotals)
	legacyMetricKeys := make([]string, 0, 16)

	ensureModel := func(name string) *modelUsageTotals {
		if _, ok := modelTotals[name]; !ok {
			modelTotals[name] = &modelUsageTotals{}
		}
		return modelTotals[name]
	}

	for key, metric := range snap.Metrics {
		if metric.Used == nil {
			continue
		}

		switch {
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_input_tokens"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_input_tokens")
			ensureModel(model).input += *metric.Used
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_output_tokens"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_output_tokens")
			ensureModel(model).output += *metric.Used
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cost_usd"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd")
			ensureModel(model).cost += *metric.Used
		case strings.HasPrefix(key, "input_tokens_"):
			model := sanitizeModelName(strings.TrimPrefix(key, "input_tokens_"))
			ensureModel(model).input += *metric.Used
			legacyMetricKeys = append(legacyMetricKeys, key)
		case strings.HasPrefix(key, "output_tokens_"):
			model := sanitizeModelName(strings.TrimPrefix(key, "output_tokens_"))
			ensureModel(model).output += *metric.Used
			legacyMetricKeys = append(legacyMetricKeys, key)
		}
	}

	for key, value := range snap.Raw {
		switch {
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cache_read"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cache_read")
			if parsed, ok := parseMetricNumber(value); ok {
				setMetricMax(snap, "model_"+model+"_cached_tokens", parsed, "tokens", "all-time")
			}
		case strings.HasPrefix(key, "model_") && strings.HasSuffix(key, "_cache_create"):
			model := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cache_create")
			if parsed, ok := parseMetricNumber(value); ok {
				setMetricMax(snap, "model_"+model+"_cache_creation_tokens", parsed, "tokens", "all-time")
			}
		}
	}

	for _, key := range legacyMetricKeys {
		delete(snap.Metrics, key)
	}

	for model, totals := range modelTotals {
		modelPrefix := "model_" + sanitizeModelName(model)
		setMetricMax(snap, modelPrefix+"_input_tokens", totals.input, "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_output_tokens", totals.output, "tokens", "all-time")
		setMetricMax(snap, modelPrefix+"_cost_usd", totals.cost, "USD", "all-time")
	}

	buildModelUsageSummaryRaw(snap)
}

func parseMetricNumber(raw string) (float64, bool) {
	clean := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if clean == "" {
		return 0, false
	}
	fields := strings.Fields(clean)
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func buildModelUsageSummaryRaw(snap *core.UsageSnapshot) {
	type entry struct {
		name   string
		input  float64
		output float64
		cost   float64
	}

	byModel := make(map[string]*entry)
	for key, metric := range snap.Metrics {
		if metric.Used == nil || !strings.HasPrefix(key, "model_") {
			continue
		}

		switch {
		case strings.HasSuffix(key, "_input_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_input_tokens")
			if _, ok := byModel[name]; !ok {
				byModel[name] = &entry{name: name}
			}
			byModel[name].input += *metric.Used
		case strings.HasSuffix(key, "_output_tokens"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_output_tokens")
			if _, ok := byModel[name]; !ok {
				byModel[name] = &entry{name: name}
			}
			byModel[name].output += *metric.Used
		case strings.HasSuffix(key, "_cost_usd"):
			name := strings.TrimSuffix(strings.TrimPrefix(key, "model_"), "_cost_usd")
			if _, ok := byModel[name]; !ok {
				byModel[name] = &entry{name: name}
			}
			byModel[name].cost += *metric.Used
		}
	}

	entries := make([]entry, 0, len(byModel))
	totalTokens := float64(0)
	totalCost := float64(0)
	for _, model := range byModel {
		if model.input <= 0 && model.output <= 0 && model.cost <= 0 {
			continue
		}
		entries = append(entries, *model)
		totalTokens += model.input + model.output
		totalCost += model.cost
	}
	if len(entries) == 0 {
		delete(snap.Raw, "model_usage")
		delete(snap.Raw, "model_usage_window")
		delete(snap.Raw, "model_count")
		return
	}

	useCost := totalCost > 0
	total := totalTokens
	if useCost {
		total = totalCost
	}
	if total <= 0 {
		delete(snap.Raw, "model_usage")
		delete(snap.Raw, "model_usage_window")
		delete(snap.Raw, "model_count")
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		left := entries[i].input + entries[i].output
		right := entries[j].input + entries[j].output
		if useCost {
			left = entries[i].cost
			right = entries[j].cost
		}
		if left == right {
			return entries[i].name < entries[j].name
		}
		return left > right
	})

	limit := maxModelUsageSummaryItems
	if limit > len(entries) {
		limit = len(entries)
	}
	parts := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		value := entries[i].input + entries[i].output
		if useCost {
			value = entries[i].cost
		}
		if value <= 0 {
			continue
		}
		pct := value / total * 100
		tokens := entries[i].input + entries[i].output
		modelName := strings.ReplaceAll(entries[i].name, "_", "-")

		if useCost {
			parts = append(parts, fmt.Sprintf("%s %s %s tok (%.0f%%)", modelName, formatUSDSummary(entries[i].cost), shortTokenCount(tokens), pct))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s tok (%.0f%%)", modelName, shortTokenCount(tokens), pct))
		}
	}
	if len(entries) > limit {
		parts = append(parts, fmt.Sprintf("+%d more", len(entries)-limit))
	}

	snap.Raw["model_usage"] = strings.Join(parts, ", ")
	snap.Raw["model_usage_window"] = "all-time"
	snap.Raw["model_count"] = fmt.Sprintf("%d", len(entries))
}

func shortTokenCount(v float64) string {
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", v/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", v/1_000)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

func formatUSDSummary(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}
