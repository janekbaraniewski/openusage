package cursor

import (
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// scoredCommitsAggregate caches the aggregated scored_commits results so we
// can skip the full table scan when the count has not changed.
type scoredCommitsAggregate struct {
	SumAIPct      float64
	CountWithPct  int
	TotalTabAdd   int
	TotalTabDel   int
	TotalCompAdd  int
	TotalCompDel  int
	TotalHumanAdd int
	TotalHumanDel int
	TotalBlankAdd int
	TotalBlankDel int
	TotalLinesAdd int
	TotalLinesDel int
	TotalCommits  int
}

var cursorAPIBase = "https://api2.cursor.sh"

type Provider struct {
	providerbase.Base
	mu                    sync.RWMutex
	clock                 core.Clock
	modelAggregationCache map[string]cachedModelAggregation

	// Incremental read caches — tracking DB
	trackingCacheMu  sync.Mutex
	trackingMaxRowID int64
	trackingRecords  []cursorTrackingRecord

	// Incremental read caches — state DB
	stateCacheMu       sync.Mutex
	composerKeys       map[string]bool
	composerRecords    []cursorComposerSessionRecord
	bubbleKeys         map[string]bool
	bubbleRecords      []cursorBubbleRecord
	scoredCommitsCount int
	scoredCommitsAgg   *scoredCommitsAggregate
}

type cachedModelAggregation struct {
	BillingCycleStart string
	BillingCycleEnd   string
	Aggregations      []modelAggregation
	EffectiveLimitUSD float64                // cached plan/included limit for gauge fallback
	BillingMetrics    map[string]core.Metric // cached billing metrics for local-only fallback
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "cursor",
			Info: core.ProviderInfo{
				Name:         "Cursor IDE",
				Capabilities: []string{"dashboard_api", "billing", "spend_tracking", "model_aggregation", "local_tracking", "composer_sessions", "ai_code_scoring"},
				DocURL:       "https://www.cursor.com/",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeToken,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Authenticate in Cursor so usage/billing endpoints are available.",
					"Ensure Cursor local state is readable for fallback aggregation.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
		clock:                 core.SystemClock{},
		modelAggregationCache: make(map[string]cachedModelAggregation),
	}
}

type planUsage struct {
	TotalSpend       float64 `json:"totalSpend"`
	IncludedSpend    float64 `json:"includedSpend"`
	BonusSpend       float64 `json:"bonusSpend"`
	Limit            float64 `json:"limit"`
	AutoPercentUsed  float64 `json:"autoPercentUsed"`
	APIPercentUsed   float64 `json:"apiPercentUsed"`
	TotalPercentUsed float64 `json:"totalPercentUsed"`
}

type spendLimitUsage struct {
	TotalSpend      float64 `json:"totalSpend"`
	PooledLimit     float64 `json:"pooledLimit"`
	PooledUsed      float64 `json:"pooledUsed"`
	PooledRemaining float64 `json:"pooledRemaining"`
	IndividualUsed  float64 `json:"individualUsed"`
	LimitType       string  `json:"limitType"`
}

type currentPeriodUsageResp struct {
	BillingCycleStart string          `json:"billingCycleStart"`
	BillingCycleEnd   string          `json:"billingCycleEnd"`
	PlanUsage         planUsage       `json:"planUsage"`
	SpendLimitUsage   spendLimitUsage `json:"spendLimitUsage"`
	DisplayThreshold  float64         `json:"displayThreshold"`
	DisplayMessage    string          `json:"displayMessage"`
}

type planInfoResp struct {
	PlanInfo struct {
		PlanName            string  `json:"planName"`
		IncludedAmountCents float64 `json:"includedAmountCents"`
		Price               string  `json:"price"`
		BillingCycleEnd     string  `json:"billingCycleEnd"`
	} `json:"planInfo"`
}

type hardLimitResp struct {
	NoUsageBasedAllowed bool `json:"noUsageBasedAllowed"`
}

type modelAggregation struct {
	ModelIntent      string  `json:"modelIntent"`
	InputTokens      string  `json:"inputTokens"`
	OutputTokens     string  `json:"outputTokens"`
	CacheWriteTokens string  `json:"cacheWriteTokens"`
	CacheReadTokens  string  `json:"cacheReadTokens"`
	TotalCents       float64 `json:"totalCents"`
	Tier             int     `json:"tier"`
}

type aggregatedUsageResp struct {
	Aggregations          []modelAggregation `json:"aggregations"`
	TotalInputTokens      string             `json:"totalInputTokens"`
	TotalOutputTokens     string             `json:"totalOutputTokens"`
	TotalCacheWriteTokens string             `json:"totalCacheWriteTokens"`
	TotalCacheReadTokens  string             `json:"totalCacheReadTokens"`
	TotalCostCents        float64            `json:"totalCostCents"`
}

type stripeProfileResp struct {
	MembershipType           string  `json:"membershipType"`
	PaymentID                string  `json:"paymentId"`
	IsTeamMember             bool    `json:"isTeamMember"`
	TeamID                   float64 `json:"teamId"`
	TeamMembershipType       string  `json:"teamMembershipType"`
	IndividualMembershipType string  `json:"individualMembershipType"`
}

type usageLimitPolicyResp struct {
	CanConfigureSpendLimit bool   `json:"canConfigureSpendLimit"`
	LimitType              string `json:"limitType"`
}

type teamMembersResp struct {
	TeamMembers []teamMember `json:"teamMembers"`
	UserID      float64      `json:"userId"`
}

type teamMember struct {
	Name      string  `json:"name"`
	ID        float64 `json:"id"`
	Role      string  `json:"role"`
	Email     string  `json:"email"`
	IsRemoved bool    `json:"isRemoved"`
}

type dailyStats struct {
	Date                   string `json:"date"`
	TabSuggestedLines      int    `json:"tabSuggestedLines"`
	TabAcceptedLines       int    `json:"tabAcceptedLines"`
	ComposerSuggestedLines int    `json:"composerSuggestedLines"`
	ComposerAcceptedLines  int    `json:"composerAcceptedLines"`
}

type composerModelUsage struct {
	CostInCents float64 `json:"costInCents"`
	Amount      int     `json:"amount"`
}

// HasChanged reports whether either Cursor SQLite database has been modified since the given time.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	return shared.AnyPathModifiedAfter([]string{
		acct.Path("tracking_db", ""),
		acct.Path("state_db", ""),
	}, since), nil
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}

func (p *Provider) now() time.Time {
	if p != nil && p.clock != nil {
		return p.clock.Now()
	}
	return time.Now()
}
