package claude_code

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

type Provider struct {
	providerbase.Base
	mu            sync.Mutex
	usageAPICache *usageResponse // last successful Usage API response

	jsonlCacheMu sync.Mutex
	jsonlCache   map[string]*jsonlCacheEntry // keyed by file path
}

// jsonlCacheEntry caches parsed conversation records for a single JSONL file.
// The cache is invalidated when the file's mtime or size changes.
type jsonlCacheEntry struct {
	modTime time.Time
	size    int64
	records []conversationRecord
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
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
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

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(true)
}

// HasChanged reports whether any of the local data sources have been modified since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")
	if override := acct.Hint("claude_dir", ""); override != "" {
		claudeDir = override
		home = filepath.Dir(claudeDir)
	}

	normalizeLegacyPaths(&acct)
	return shared.AnyPathModifiedAfter([]string{
		filepath.Join(claudeDir, "projects"),
		filepath.Join(home, ".config", "claude", "projects"),
		acct.Path("stats_cache", ""),
		acct.Path("account_config", filepath.Join(home, ".claude.json")),
		filepath.Join(claudeDir, "settings.json"),
	}, since), nil
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}
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
	if override := acct.Hint("claude_dir", ""); override != "" {
		claudeDir = override
		home = filepath.Dir(claudeDir) // derive "home" from the override
	}

	normalizeLegacyPaths(&acct)
	statsPath := acct.Path("stats_cache", "")
	accountPath := acct.Path("account_config", "")

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
		if err := p.readUsageAPI(ctx, orgUUID, &snap); err != nil {
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

func (p *Provider) readUsageAPI(ctx context.Context, orgUUID string, snap *core.UsageSnapshot) error {
	cookies, err := getClaudeSessionCookies()
	if err != nil {
		if cached := p.getCachedUsage(); cached != nil {
			applyUsageResponse(cached, snap, time.Now())
			snap.Raw["usage_api_cached"] = "true"
			return nil
		}
		return fmt.Errorf("cookie extraction: %w", err)
	}

	usage, err := fetchUsageAPI(ctx, orgUUID, cookies)
	if err != nil {
		if cached := p.getCachedUsage(); cached != nil {
			applyUsageResponse(cached, snap, time.Now())
			snap.Raw["usage_api_cached"] = "true"
			return nil
		}
		return fmt.Errorf("API fetch: %w", err)
	}

	p.setCachedUsage(usage)
	applyUsageResponse(usage, snap, time.Now())

	snap.Raw["usage_api_ok"] = "true"
	return nil
}

func (p *Provider) getCachedUsage() *usageResponse {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usageAPICache
}

func (p *Provider) setCachedUsage(u *usageResponse) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.usageAPICache = u
}
