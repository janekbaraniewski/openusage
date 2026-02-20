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
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "claude_code" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name: "Claude Code CLI",
		Capabilities: []string{
			"local_stats", "daily_activity", "model_tokens",
			"account_info", "jsonl_conversations", "5h_billing_blocks",
			"cost_estimation", "burn_rate", "session_tracking",
		},
		DocURL: "https://code.claude.com/",
	}
}

type statsCache struct {
	Version          int                   `json:"version"`
	LastComputedDate string                `json:"lastComputedDate"`
	DailyActivity    []dailyActivity       `json:"dailyActivity"`
	DailyModelTokens []dailyTokens         `json:"dailyModelTokens"`
	ModelUsage       map[string]modelUsage `json:"modelUsage"`
	TotalSessions    int                   `json:"totalSessions"`
	TotalMessages    int                   `json:"totalMessages"`
	LongestSession   *longestSession       `json:"longestSession"`
	FirstSessionDate string                `json:"firstSessionDate"`
	HourCounts       map[string]int        `json:"hourCounts"`
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
	Message   *jsonlMsg `json:"message,omitempty"`
	Subtype   string    `json:"subtype,omitempty"`
	Version   string    `json:"version,omitempty"`
	CWD       string    `json:"cwd,omitempty"`
}

type jsonlMsg struct {
	Model      string      `json:"model"`
	Role       string      `json:"role"`
	StopReason *string     `json:"stop_reason"`
	Usage      *jsonlUsage `json:"usage,omitempty"`
}

type jsonlUsage struct {
	InputTokens              int              `json:"input_tokens"`
	CacheCreationInputTokens int              `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int              `json:"cache_read_input_tokens"`
	OutputTokens             int              `json:"output_tokens"`
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

const billingBlockDuration = 5 * time.Hour

func floorToHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	snap := core.QuotaSnapshot{
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

	if statsPath == "" {
		statsPath = filepath.Join(claudeDir, "stats-cache.json")
	}
	if accountPath == "" {
		accountPath = filepath.Join(home, ".claude.json")
	}

	var hasData bool

	if err := p.readStats(statsPath, &snap); err != nil {
		snap.Raw["stats_error"] = err.Error()
	} else {
		hasData = true
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

	if !hasData {
		snap.Status = core.StatusError
		snap.Message = "No Claude Code stats data accessible"
		return snap, nil
	}

	snap.Message = "Claude Code CLI · costs are API-equivalent estimates, not subscription charges"
	return snap, nil
}

func (p *Provider) readUsageAPI(orgUUID string, snap *core.QuotaSnapshot) error {
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

func applyUsageResponse(usage *usageResponse, snap *core.QuotaSnapshot, now time.Time) {
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

func (p *Provider) readStats(path string, snap *core.QuotaSnapshot) error {
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

	today := time.Now().Format("2006-01-02")
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

		snap.Metrics[fmt.Sprintf("output_tokens_%s", name)] = core.Metric{
			Used:   &outTokens,
			Unit:   "tokens",
			Window: "all-time",
		}
		snap.Metrics[fmt.Sprintf("input_tokens_%s", name)] = core.Metric{
			Used:   &inTokens,
			Unit:   "tokens",
			Window: "all-time",
		}

		snap.Raw[fmt.Sprintf("model_%s_cache_read", name)] = fmt.Sprintf("%d tokens", usage.CacheReadInputTokens)
		snap.Raw[fmt.Sprintf("model_%s_cache_create", name)] = fmt.Sprintf("%d tokens", usage.CacheCreationInputTokens)

		if usage.CostUSD > 0 {
			totalCostUSD += usage.CostUSD
			modelCost := usage.CostUSD
			snap.Metrics[fmt.Sprintf("model_%s_cost_usd", name)] = core.Metric{
				Used:   &modelCost,
				Unit:   "USD",
				Window: "all-time",
			}
		}
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

	return nil
}

func (p *Provider) readAccount(path string, snap *core.QuotaSnapshot) error {
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
			snap.Raw[fmt.Sprintf("s1m_access_%s", orgID[:8])] = "true"
		}
	}

	snap.Raw["num_startups"] = fmt.Sprintf("%d", acct.NumStartups)
	if acct.InstallMethod != "" {
		snap.Raw["install_method"] = acct.InstallMethod
	}

	return nil
}

func (p *Provider) readSettings(path string, snap *core.QuotaSnapshot) error {
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

func (p *Provider) readConversationJSONL(projectsDir, altProjectsDir string, snap *core.QuotaSnapshot) error {
	jsonlFiles := collectJSONLFiles(projectsDir)
	if altProjectsDir != "" {
		jsonlFiles = append(jsonlFiles, collectJSONLFiles(altProjectsDir)...)
	}

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
		timestamp time.Time
		model     string
		usage     *jsonlUsage
	}

	var allUsages []parsedUsage

	for _, fpath := range jsonlFiles {
		entries := parseJSONLFile(fpath)
		for _, entry := range entries {
			if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
				continue
			}
			ts, err := time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				ts, err = time.Parse("2006-01-02T15:04:05.000Z", entry.Timestamp)
				if err != nil {
					continue
				}
			}
			allUsages = append(allUsages, parsedUsage{
				timestamp: ts,
				model:     entry.Message.Model,
				usage:     entry.Message.Usage,
			})
		}
	}

	sort.Slice(allUsages, func(i, j int) bool {
		return allUsages[i].timestamp.Before(allUsages[j].timestamp)
	})

	for _, u := range allUsages {
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
		cost := estimateCost(u.model, u.usage)
		allTimeCostUSD += cost
		allTimeEntries++

		if u.timestamp.After(todayStart) || u.timestamp.Equal(todayStart) {
			todayCostUSD += cost
			todayInputTokens += u.usage.InputTokens
			todayOutputTokens += u.usage.OutputTokens
			todayCacheRead += u.usage.CacheReadInputTokens
			todayCacheCreate += u.usage.CacheCreationInputTokens
			todayMessages++
			todayModels[u.model] = true
		}

		if u.timestamp.After(weekStart) {
			weeklyCostUSD += cost
			weeklyInputTokens += u.usage.InputTokens
			weeklyOutputTokens += u.usage.OutputTokens
			weeklyMessages++
		}

		if inCurrentBlock && (u.timestamp.After(currentBlockStart) || u.timestamp.Equal(currentBlockStart)) && u.timestamp.Before(currentBlockEnd) {
			blockCostUSD += cost
			blockInputTokens += u.usage.InputTokens
			blockOutputTokens += u.usage.OutputTokens
			blockCacheRead += u.usage.CacheReadInputTokens
			blockCacheCreate += u.usage.CacheCreationInputTokens
			blockMessages++
			blockModels[u.model] = true
		}
	}

	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
	if len(snap.DailySeries["messages"]) == 0 && len(allUsages) > 0 {
		dailyTokens := make(map[string]int)
		dailyMsgs := make(map[string]int)
		dailyCost := make(map[string]float64)
		dailyModelTok := make(map[string]map[string]int)

		for _, u := range allUsages {
			day := u.timestamp.Format("2006-01-02")
			totalTok := u.usage.InputTokens + u.usage.OutputTokens
			dailyTokens[day] += totalTok
			dailyMsgs[day]++
			dailyCost[day] += estimateCost(u.model, u.usage)
			if dailyModelTok[day] == nil {
				dailyModelTok[day] = make(map[string]int)
			}
			dailyModelTok[day][u.model] += totalTok
		}

		dates := make([]string, 0, len(dailyTokens))
		for d := range dailyTokens {
			dates = append(dates, d)
		}
		sort.Strings(dates)

		for _, d := range dates {
			snap.DailySeries["messages"] = append(snap.DailySeries["messages"],
				core.TimePoint{Date: d, Value: float64(dailyMsgs[d])})
			snap.DailySeries["tokens_total"] = append(snap.DailySeries["tokens_total"],
				core.TimePoint{Date: d, Value: float64(dailyTokens[d])})
			snap.DailySeries["cost"] = append(snap.DailySeries["cost"],
				core.TimePoint{Date: d, Value: dailyCost[d]})
		}

		allModels := make(map[string]int64)
		for _, dm := range dailyModelTok {
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
				tokens := dailyModelTok[d][model]
				snap.DailySeries[key] = append(snap.DailySeries[key],
					core.TimePoint{Date: d, Value: float64(tokens)})
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

	if todayMessages > 0 {
		snap.Raw["jsonl_today_date"] = today
		snap.Raw["jsonl_today_messages"] = fmt.Sprintf("%d", todayMessages)
		snap.Raw["jsonl_today_input_tokens"] = fmt.Sprintf("%d", todayInputTokens)
		snap.Raw["jsonl_today_output_tokens"] = fmt.Sprintf("%d", todayOutputTokens)
		snap.Raw["jsonl_today_cache_read_tokens"] = fmt.Sprintf("%d", todayCacheRead)
		snap.Raw["jsonl_today_cache_create_tokens"] = fmt.Sprintf("%d", todayCacheCreate)

		models := make([]string, 0, len(todayModels))
		for m := range todayModels {
			models = append(models, m)
		}
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

		blockModelList := make([]string, 0, len(blockModels))
		for m := range blockModels {
			blockModelList = append(blockModelList, m)
		}
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

	snap.Raw["jsonl_total_entries"] = fmt.Sprintf("%d", allTimeEntries)
	snap.Raw["jsonl_total_blocks"] = fmt.Sprintf("%d", len(blockStartCandidates))

	return nil
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
	result := make([]byte, 0, len(model))
	for _, c := range model {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			result = append(result, byte(c))
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}

func floatPtr(v float64) *float64 {
	return &v
}
