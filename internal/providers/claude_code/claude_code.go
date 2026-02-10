// Package claude_code implements a QuotaProvider that reads Claude Code CLI's
// local data files for comprehensive usage tracking.
//
// Data sources (like ccusage, ccstatusline, and other community tools):
//
//  1. ~/.claude/stats-cache.json: aggregate usage stats (daily activity, model tokens,
//     per-model input/output tokens, cache usage, total sessions/messages)
//
//  2. ~/.claude.json: account info (subscription status, billing, org, plan details,
//     s1m access cache, extra usage toggle)
//
//  3. ~/.claude/projects/<project>/<session>.jsonl: per-session conversation JSONL files
//     that Claude Code writes during each conversation. Each assistant message contains:
//     - usage.input_tokens, usage.output_tokens
//     - usage.cache_creation_input_tokens, usage.cache_read_input_tokens
//     - usage.cache_creation.ephemeral_5m_input_tokens, ephemeral_1h_input_tokens
//     - usage.service_tier (e.g. "standard")
//     - usage.inference_geo (e.g. "global")
//     - message.model (the actual model used, e.g. "claude-opus-4-6")
//     - timestamp
//     This is the same data source used by ccusage (npm), ccstatusline, and other
//     community tools. From this data we compute:
//     - 5-hour billing block usage (Claude's actual billing window)
//     - Daily cost estimates using token pricing
//     - Current session spend
//     - Per-model token breakdowns
//     - Burn rate (cost/hour within current block)
//
//  4. ~/.claude/settings.json: current model selection, status line config
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

	"github.com/janekbaraniewski/agentusage/internal/core"
)

// Provider reads Claude Code CLI's local usage data.
type Provider struct{}

// New returns a new Claude Code provider instance.
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

// ──────────────────────────────────────────────────────────────────────────────
// stats-cache.json structures
// ──────────────────────────────────────────────────────────────────────────────

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

// ──────────────────────────────────────────────────────────────────────────────
// .claude.json (account config) structures
// ──────────────────────────────────────────────────────────────────────────────

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

// ──────────────────────────────────────────────────────────────────────────────
// settings.json structures
// ──────────────────────────────────────────────────────────────────────────────

type settingsConfig struct {
	Model      string `json:"model"`
	StatusLine *struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"statusLine"`
	AlwaysThinkingEnabled bool `json:"alwaysThinkingEnabled"`
}

// ──────────────────────────────────────────────────────────────────────────────
// JSONL conversation entry structures (the main data source for ccusage etc.)
// ──────────────────────────────────────────────────────────────────────────────

// jsonlEntry represents a single line in a Claude Code conversation JSONL file.
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

// ──────────────────────────────────────────────────────────────────────────────
// Model pricing (same approach as ccusage — per-million-token USD pricing)
// These prices are approximate and will need periodic updates.
// ──────────────────────────────────────────────────────────────────────────────

type modelPricing struct {
	InputPerMillion       float64
	OutputPerMillion      float64
	CacheReadPerMillion   float64
	CacheCreatePerMillion float64
}

// Current pricing as of early 2026. ccusage uses similar pricing tables.
// Source: https://docs.anthropic.com/en/docs/about-claude/models
var modelPricingTable = map[string]modelPricing{
	// Claude Opus 4 family
	"claude-opus-4-6":          {InputPerMillion: 15.0, OutputPerMillion: 75.0, CacheReadPerMillion: 1.50, CacheCreatePerMillion: 18.75},
	"claude-opus-4-5-20251101": {InputPerMillion: 15.0, OutputPerMillion: 75.0, CacheReadPerMillion: 1.50, CacheCreatePerMillion: 18.75},
	// Claude Sonnet 4 family
	"claude-sonnet-4-5-20250929": {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75},
	"claude-sonnet-4-20250514":   {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75},
	"claude-sonnet-4-5":          {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75},
	"claude-sonnet-4":            {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75},
	// Claude Haiku 3.5
	"claude-haiku-3-5-20241022": {InputPerMillion: 0.80, OutputPerMillion: 4.0, CacheReadPerMillion: 0.08, CacheCreatePerMillion: 1.0},
	"claude-3-5-haiku-20241022": {InputPerMillion: 0.80, OutputPerMillion: 4.0, CacheReadPerMillion: 0.08, CacheCreatePerMillion: 1.0},
	// Claude 3 Opus (legacy)
	"claude-3-opus-20240229": {InputPerMillion: 15.0, OutputPerMillion: 75.0, CacheReadPerMillion: 1.50, CacheCreatePerMillion: 18.75},
	// Claude 3 Sonnet (legacy)
	"claude-3-sonnet-20240229": {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75},
	// Claude 3 Haiku (legacy)
	"claude-3-haiku-20240307": {InputPerMillion: 0.25, OutputPerMillion: 1.25, CacheReadPerMillion: 0.03, CacheCreatePerMillion: 0.30},
}

// Subscription pricing reference for Claude Code (Max plan).
// Claude Code is included with Max subscription ($100/mo individual, $200/mo team).
// The 5-hour rolling window billing applies to usage beyond the plan limits.
const subscriptionPricingSummary = "Max plan: $100/mo (individual) / $200/mo (team) · " +
	"opus-4: $15/$75 · sonnet-4: $3/$15 · haiku-3.5: $0.80/$4 (per 1M tokens)"

// estimateCost returns estimated USD cost for a given usage and model.
func estimateCost(model string, u *jsonlUsage) float64 {
	pricing := findPricing(model)
	cost := float64(u.InputTokens) / 1e6 * pricing.InputPerMillion
	cost += float64(u.OutputTokens) / 1e6 * pricing.OutputPerMillion
	cost += float64(u.CacheReadInputTokens) / 1e6 * pricing.CacheReadPerMillion
	cost += float64(u.CacheCreationInputTokens) / 1e6 * pricing.CacheCreatePerMillion
	return cost
}

// findPricing returns the pricing for a model, falling back to a best-effort prefix match.
func findPricing(model string) modelPricing {
	if p, ok := modelPricingTable[model]; ok {
		return p
	}
	// Prefix-based fallback (e.g. "claude-opus-4-6-xxx" matches "claude-opus-4-6")
	for key, p := range modelPricingTable {
		if strings.HasPrefix(model, key) {
			return p
		}
	}
	// Very rough fallback — assume sonnet pricing
	if strings.Contains(model, "opus") {
		return modelPricing{InputPerMillion: 15.0, OutputPerMillion: 75.0, CacheReadPerMillion: 1.50, CacheCreatePerMillion: 18.75}
	}
	if strings.Contains(model, "haiku") {
		return modelPricing{InputPerMillion: 0.80, OutputPerMillion: 4.0, CacheReadPerMillion: 0.08, CacheCreatePerMillion: 1.0}
	}
	return modelPricing{InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75}
}

// ──────────────────────────────────────────────────────────────────────────────
// 5-hour billing block computation (same algorithm as ccusage/ccstatusline)
// ──────────────────────────────────────────────────────────────────────────────

const billingBlockDuration = 5 * time.Hour

// floorToHour rounds a time down to the start of its hour.
func floorToHour(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

// ──────────────────────────────────────────────────────────────────────────────
// Provider Fetch implementation
// ──────────────────────────────────────────────────────────────────────────────

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Status:     core.StatusOK,
		Metrics:    make(map[string]core.Metric),
		Raw:        make(map[string]string),
		Resets:     make(map[string]time.Time),
	}

	// Determine home / claude directory. ExtraData["claude_dir"] allows
	// tests (and advanced config) to override the default ~/.claude location.
	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")
	if override, ok := acct.ExtraData["claude_dir"]; ok && override != "" {
		claudeDir = override
		home = filepath.Dir(claudeDir) // derive "home" from the override
	}

	statsPath := acct.Binary    // repurposed field — path to stats-cache.json
	accountPath := acct.BaseURL // repurposed field — path to .claude.json

	// Auto-fill paths if not provided
	if statsPath == "" {
		statsPath = filepath.Join(claudeDir, "stats-cache.json")
	}
	if accountPath == "" {
		accountPath = filepath.Join(home, ".claude.json")
	}

	var hasData bool

	// ─── Source 1: stats-cache.json ───
	if err := p.readStats(statsPath, &snap); err != nil {
		snap.Raw["stats_error"] = err.Error()
	} else {
		hasData = true
	}

	// ─── Source 2: .claude.json (account info) ───
	if err := p.readAccount(accountPath, &snap); err != nil {
		snap.Raw["account_error"] = err.Error()
	} else {
		hasData = true
	}

	// ─── Source 3: settings.json ───
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := p.readSettings(settingsPath, &snap); err != nil {
		snap.Raw["settings_error"] = err.Error()
	}

	// ─── Source 4: JSONL conversation files (the big one — like ccusage) ───
	projectsDir := filepath.Join(claudeDir, "projects")
	// Also check new location (Claude Code v1.0.30+)
	newProjectsDir := filepath.Join(home, ".config", "claude", "projects")

	if err := p.readConversationJSONL(projectsDir, newProjectsDir, &snap); err != nil {
		snap.Raw["jsonl_error"] = err.Error()
	} else {
		hasData = true
	}

	if !hasData {
		snap.Status = core.StatusError
		snap.Message = "No Claude Code stats data accessible"
		return snap, nil
	}

	snap.Raw["pricing_summary"] = subscriptionPricingSummary
	snap.Message = "Claude Code CLI usage (stats-cache + account + JSONL conversations)"
	return snap, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Source 1: stats-cache.json reader
// ──────────────────────────────────────────────────────────────────────────────

func (p *Provider) readStats(path string, snap *core.QuotaSnapshot) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading stats cache: %w", err)
	}

	var stats statsCache
	if err := json.Unmarshal(data, &stats); err != nil {
		return fmt.Errorf("parsing stats cache: %w", err)
	}

	// Total messages metric
	if stats.TotalMessages > 0 {
		total := float64(stats.TotalMessages)
		snap.Metrics["total_messages"] = core.Metric{
			Used:   &total,
			Unit:   "messages",
			Window: "all-time",
		}
	}

	// Total sessions
	if stats.TotalSessions > 0 {
		total := float64(stats.TotalSessions)
		snap.Metrics["total_sessions"] = core.Metric{
			Used:   &total,
			Unit:   "sessions",
			Window: "all-time",
		}
	}

	// Today's activity
	today := time.Now().Format("2006-01-02")
	for _, da := range stats.DailyActivity {
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
			break
		}
	}

	// Today's token usage by model (from stats-cache)
	for _, dt := range stats.DailyModelTokens {
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
			break
		}
	}

	// Per-model cumulative usage
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

// ──────────────────────────────────────────────────────────────────────────────
// Source 2: .claude.json (account/subscription info)
// ──────────────────────────────────────────────────────────────────────────────

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

	// S1M (Sonnet 1M context) access
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

// ──────────────────────────────────────────────────────────────────────────────
// Source 3: settings.json
// ──────────────────────────────────────────────────────────────────────────────

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

// ──────────────────────────────────────────────────────────────────────────────
// Source 4: JSONL conversation files (the ccusage approach)
// ──────────────────────────────────────────────────────────────────────────────

func (p *Provider) readConversationJSONL(projectsDir, altProjectsDir string, snap *core.QuotaSnapshot) error {
	// Collect all JSONL files from both possible locations
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

	// Aggregate data structures
	var (
		todayCostUSD      float64
		todayInputTokens  int
		todayOutputTokens int
		todayCacheRead    int
		todayCacheCreate  int
		todayMessages     int
		todayModels       = make(map[string]bool)

		// 5-hour billing block tracking
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

		// All-time from JSONL (for comparison with stats-cache)
		allTimeCostUSD float64
		allTimeEntries int
	)

	// Determine the current 5-hour billing block
	// Block starts at the most recent hour boundary where usage occurred
	// and extends for 5 hours. We find the active block by looking at
	// recent usage timestamps.
	blockStartCandidates := []time.Time{}

	type parsedUsage struct {
		timestamp time.Time
		model     string
		usage     *jsonlUsage
	}

	var allUsages []parsedUsage

	// Parse all JSONL files for assistant messages with usage data
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

	// Sort by timestamp
	sort.Slice(allUsages, func(i, j int) bool {
		return allUsages[i].timestamp.Before(allUsages[j].timestamp)
	})

	// Compute billing blocks using the same algorithm as ccstatusline:
	// Walk through timestamps, start a new block when we're past the end
	// of the previous one.
	for _, u := range allUsages {
		if currentBlockEnd.IsZero() || u.timestamp.After(currentBlockEnd) {
			currentBlockStart = floorToHour(u.timestamp)
			currentBlockEnd = currentBlockStart.Add(billingBlockDuration)
			blockStartCandidates = append(blockStartCandidates, currentBlockStart)
		}
	}

	// The active billing block is the one that contains "now",
	// or the most recent one if we're between blocks.
	inCurrentBlock = false
	if !currentBlockEnd.IsZero() && now.Before(currentBlockEnd) && (now.Equal(currentBlockStart) || now.After(currentBlockStart)) {
		inCurrentBlock = true
	}

	// Now aggregate usage data
	for _, u := range allUsages {
		cost := estimateCost(u.model, u.usage)
		allTimeCostUSD += cost
		allTimeEntries++

		// Today's usage
		if u.timestamp.After(todayStart) || u.timestamp.Equal(todayStart) {
			todayCostUSD += cost
			todayInputTokens += u.usage.InputTokens
			todayOutputTokens += u.usage.OutputTokens
			todayCacheRead += u.usage.CacheReadInputTokens
			todayCacheCreate += u.usage.CacheCreationInputTokens
			todayMessages++
			todayModels[u.model] = true
		}

		// Current billing block usage
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

	// ─── Emit JSONL-derived metrics ───

	// Today's estimated cost from JSONL
	if todayCostUSD > 0 {
		snap.Metrics["daily_cost_usd"] = core.Metric{
			Used:   floatPtr(todayCostUSD),
			Unit:   "USD",
			Window: "1d",
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

	// 5-hour billing block metrics
	if inCurrentBlock {
		snap.Metrics["block_cost_usd"] = core.Metric{
			Used:   floatPtr(blockCostUSD),
			Unit:   "USD",
			Window: "rolling-5h",
		}

		blockIn := float64(blockInputTokens)
		snap.Metrics["block_input_tokens"] = core.Metric{
			Used:   &blockIn,
			Unit:   "tokens",
			Window: "rolling-5h",
		}

		blockOut := float64(blockOutputTokens)
		snap.Metrics["block_output_tokens"] = core.Metric{
			Used:   &blockOut,
			Unit:   "tokens",
			Window: "rolling-5h",
		}

		blockMsgs := float64(blockMessages)
		snap.Metrics["block_messages"] = core.Metric{
			Used:   &blockMsgs,
			Unit:   "messages",
			Window: "rolling-5h",
		}

		// Block time remaining
		remaining := currentBlockEnd.Sub(now)
		if remaining > 0 {
			snap.Resets["billing_block"] = currentBlockEnd
			snap.Raw["block_time_remaining"] = fmt.Sprintf("%s", remaining.Round(time.Minute))

			// Block progress (0-100%)
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

		// Burn rate (USD/hour within current block)
		elapsed := now.Sub(currentBlockStart)
		if elapsed > time.Minute && blockCostUSD > 0 {
			burnRate := blockCostUSD / elapsed.Hours()
			snap.Metrics["burn_rate_usd_per_hour"] = core.Metric{
				Used:   floatPtr(burnRate),
				Unit:   "USD/h",
				Window: "rolling-5h",
			}
			snap.Raw["burn_rate"] = fmt.Sprintf("$%.2f/hour", burnRate)
		}
	}

	// All-time JSONL cost estimate
	if allTimeCostUSD > 0 {
		snap.Metrics["jsonl_total_cost_usd"] = core.Metric{
			Used:   floatPtr(allTimeCostUSD),
			Unit:   "USD",
			Window: "all-time",
		}
	}

	snap.Raw["jsonl_total_entries"] = fmt.Sprintf("%d", allTimeEntries)
	snap.Raw["jsonl_total_blocks"] = fmt.Sprintf("%d", len(blockStartCandidates))

	return nil
}

// collectJSONLFiles walks a projects directory and returns all .jsonl file paths.
func collectJSONLFiles(dir string) []string {
	var files []string
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return files
	}

	// Walk the directory structure: projects/<project-slug>/<session>.jsonl
	// Also includes subagent files: projects/<project-slug>/<session>/subagents/<agent>.jsonl
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

// parseJSONLFile reads a JSONL file and returns parsed entries.
// It's designed to be resilient — skips malformed lines.
func parseJSONLFile(path string) []jsonlEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []jsonlEntry
	scanner := bufio.NewScanner(f)
	// Increase buffer size for potentially large lines
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

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// sanitizeModelName converts a model ID to a safe metric key suffix.
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
