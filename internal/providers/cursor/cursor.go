package cursor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/openusage/internal/core"
)

var cursorAPIBase = "https://api2.cursor.sh"

type Provider struct {
	mu                    sync.RWMutex
	modelAggregationCache map[string]cachedModelAggregation // account ID -> latest model aggregation
}

type cachedModelAggregation struct {
	BillingCycleStart string
	BillingCycleEnd   string
	Aggregations      []modelAggregation
}

func New() *Provider {
	return &Provider{
		modelAggregationCache: make(map[string]cachedModelAggregation),
	}
}

func (p *Provider) ID() string { return "cursor" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "Cursor IDE",
		Capabilities: []string{"dashboard_api", "billing", "spend_tracking", "model_aggregation", "local_tracking"},
		DocURL:       "https://www.cursor.com/",
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

type billingCycleResp struct {
	StartDateEpochMillis string `json:"startDateEpochMillis"`
	EndDateEpochMillis   string `json:"endDateEpochMillis"`
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
	Aggregations []modelAggregation `json:"aggregations"`
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

type dailyStats struct {
	Date                   string `json:"date"`
	TabSuggestedLines      int    `json:"tabSuggestedLines"`
	TabAcceptedLines       int    `json:"tabAcceptedLines"`
	ComposerSuggestedLines int    `json:"composerSuggestedLines"`
	ComposerAcceptedLines  int    `json:"composerAcceptedLines"`
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	snap := core.QuotaSnapshot{
		ProviderID:  p.ID(),
		AccountID:   acct.ID,
		Timestamp:   time.Now(),
		Status:      core.StatusOK,
		Metrics:     make(map[string]core.Metric),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	if acct.Token != "" {
		apiErr := p.fetchFromAPI(ctx, acct.Token, &snap)
		if apiErr == nil {
			return snap, nil
		}
		log.Printf("[cursor] API fetch failed, falling back to local data: %v", apiErr)
		snap.Raw["api_error"] = apiErr.Error()
	}

	trackingDBPath := ""
	stateDBPath := ""
	if acct.ExtraData != nil {
		trackingDBPath = acct.ExtraData["tracking_db"]
		stateDBPath = acct.ExtraData["state_db"]
	}
	if trackingDBPath == "" {
		trackingDBPath = acct.Binary
	}
	if stateDBPath == "" {
		stateDBPath = acct.BaseURL
	}

	var hasData bool

	if trackingDBPath != "" {
		if err := p.readTrackingDB(ctx, trackingDBPath, &snap); err != nil {
			log.Printf("[cursor] tracking DB error: %v", err)
			snap.Raw["tracking_db_error"] = err.Error()
		} else {
			hasData = true
		}
	}

	if stateDBPath != "" {
		if err := p.readStateDB(ctx, stateDBPath, &snap); err != nil {
			log.Printf("[cursor] state DB error: %v", err)
			snap.Raw["state_db_error"] = err.Error()
		} else {
			hasData = true
		}
	}

	if !hasData {
		snap.Status = core.StatusError
		snap.Message = "No Cursor tracking data accessible (no API token and no local DBs)"
		return snap, nil
	}

	p.applyCachedModelAggregations(acct.ID, "", "", &snap)

	snap.Message = "Local Cursor IDE usage tracking (API unavailable)"
	return snap, nil
}

func (p *Provider) fetchFromAPI(ctx context.Context, token string, snap *core.QuotaSnapshot) error {
	var periodUsage currentPeriodUsageResp
	if err := p.callDashboardAPI(ctx, token, "GetCurrentPeriodUsage", &periodUsage); err != nil {
		return fmt.Errorf("GetCurrentPeriodUsage: %w", err)
	}

	pu := periodUsage.PlanUsage
	totalSpendDollars := pu.TotalSpend / 100.0
	includedDollars := pu.IncludedSpend / 100.0
	limitDollars := pu.Limit / 100.0
	bonusDollars := pu.BonusSpend / 100.0

	snap.Metrics["plan_spend"] = core.Metric{
		Used:   &totalSpendDollars,
		Limit:  &limitDollars,
		Unit:   "USD",
		Window: "billing-cycle",
	}
	snap.Metrics["plan_included"] = core.Metric{
		Used:   &includedDollars,
		Unit:   "USD",
		Window: "billing-cycle",
	}
	snap.Metrics["plan_bonus"] = core.Metric{
		Used:   &bonusDollars,
		Unit:   "USD",
		Window: "billing-cycle",
	}

	totalPctUsed := pu.TotalPercentUsed
	totalPctRemaining := 100.0 - totalPctUsed
	hundredPct := 100.0
	snap.Metrics["plan_percent_used"] = core.Metric{
		Used:      &totalPctUsed,
		Remaining: &totalPctRemaining,
		Limit:     &hundredPct,
		Unit:      "%",
		Window:    "billing-cycle",
	}

	su := periodUsage.SpendLimitUsage
	if su.PooledLimit > 0 {
		pooledLimitDollars := su.PooledLimit / 100.0
		pooledUsedDollars := su.PooledUsed / 100.0
		pooledRemainingDollars := su.PooledRemaining / 100.0
		individualDollars := su.IndividualUsed / 100.0

		snap.Metrics["spend_limit"] = core.Metric{
			Limit:     &pooledLimitDollars,
			Used:      &pooledUsedDollars,
			Remaining: &pooledRemainingDollars,
			Unit:      "USD",
			Window:    "billing-cycle",
		}
		snap.Metrics["individual_spend"] = core.Metric{
			Used:   &individualDollars,
			Unit:   "USD",
			Window: "billing-cycle",
		}
		snap.Raw["spend_limit_type"] = su.LimitType
	}

	snap.Raw["display_message"] = periodUsage.DisplayMessage
	snap.Raw["billing_cycle_start"] = formatTimestamp(periodUsage.BillingCycleStart)
	snap.Raw["billing_cycle_end"] = formatTimestamp(periodUsage.BillingCycleEnd)

	if t := parseTimestamp(periodUsage.BillingCycleEnd); !t.IsZero() {
		if snap.Resets != nil {
			snap.Resets["billing_cycle_end"] = t
		}
	}

	if su.PooledLimit > 0 && su.PooledRemaining > 0 {
		spendPctUsed := (su.PooledUsed / su.PooledLimit) * 100
		if spendPctUsed >= 100 {
			snap.Status = core.StatusLimited
		} else if spendPctUsed >= 80 {
			snap.Status = core.StatusNearLimit
		} else {
			snap.Status = core.StatusOK
		}
	} else if pu.TotalPercentUsed >= 100 {
		snap.Status = core.StatusLimited
	} else if pu.TotalPercentUsed >= 80 {
		snap.Status = core.StatusNearLimit
	} else {
		snap.Status = core.StatusOK
	}

	snap.Metrics["plan_total_spend_usd"] = core.Metric{
		Used:   &totalSpendDollars,
		Limit:  &limitDollars,
		Unit:   "USD",
		Window: "billing-cycle",
	}
	if su.PooledLimit > 0 {
		pooledLimitDollars := su.PooledLimit / 100.0
		snap.Metrics["plan_limit_usd"] = core.Metric{
			Limit:  &pooledLimitDollars,
			Unit:   "USD",
			Window: "billing-cycle",
		}
	} else {
		snap.Metrics["plan_limit_usd"] = core.Metric{
			Limit:  &limitDollars,
			Unit:   "USD",
			Window: "billing-cycle",
		}
	}

	var planInfo planInfoResp
	if err := p.callDashboardAPI(ctx, token, "GetPlanInfo", &planInfo); err == nil {
		snap.Raw["plan_name"] = planInfo.PlanInfo.PlanName
		snap.Raw["plan_price"] = planInfo.PlanInfo.Price
	}

	var aggUsage aggregatedUsageResp
	aggErr := p.callDashboardAPI(ctx, token, "GetAggregatedUsageEvents", &aggUsage)
	aggApplied := false
	if aggErr == nil {
		aggApplied = applyModelAggregations(snap, aggUsage.Aggregations)
		if aggApplied {
			p.storeModelAggregationCache(snap.AccountID, snap.Raw["billing_cycle_start"], snap.Raw["billing_cycle_end"], aggUsage.Aggregations)
		}
	}
	if !aggApplied && p.applyCachedModelAggregations(snap.AccountID, snap.Raw["billing_cycle_start"], snap.Raw["billing_cycle_end"], snap) {
		if aggErr != nil {
			log.Printf("[cursor] using cached model aggregation after API error: %v", aggErr)
		} else {
			log.Printf("[cursor] using cached model aggregation after empty API aggregation response")
		}
	}

	var hardLimit hardLimitResp
	if err := p.callDashboardAPI(ctx, token, "GetHardLimit", &hardLimit); err == nil {
		if hardLimit.NoUsageBasedAllowed {
			snap.Raw["usage_based_billing"] = "disabled"
		} else {
			snap.Raw["usage_based_billing"] = "enabled"
		}
	}

	var profile stripeProfileResp
	if err := p.callRESTAPI(ctx, token, "/auth/full_stripe_profile", &profile); err == nil {
		snap.Raw["membership_type"] = profile.MembershipType
		snap.Raw["team_membership"] = profile.TeamMembershipType
		snap.Raw["individual_membership"] = profile.IndividualMembershipType
		if profile.IsTeamMember {
			snap.Raw["team_id"] = fmt.Sprintf("%.0f", profile.TeamID)
		}
	}

	var limitPolicy usageLimitPolicyResp
	if err := p.callDashboardAPI(ctx, token, "GetUsageLimitPolicyStatus", &limitPolicy); err == nil {
		snap.Raw["limit_policy_type"] = limitPolicy.LimitType
	}

	planName := snap.Raw["plan_name"]
	if su.PooledLimit > 0 {
		pooledLimitDollars := su.PooledLimit / 100.0
		pooledUsedDollars := su.PooledUsed / 100.0
		pooledRemainingDollars := su.PooledRemaining / 100.0
		snap.Message = fmt.Sprintf("%s — $%.0f / $%.0f team spend ($%.0f remaining)",
			planName, pooledUsedDollars, pooledLimitDollars, pooledRemainingDollars)
	} else if limitDollars > 0 {
		snap.Message = fmt.Sprintf("%s — $%.2f / $%.0f plan spend",
			planName, totalSpendDollars, limitDollars)
	} else {
		snap.Message = fmt.Sprintf("%s — %s", planName, periodUsage.DisplayMessage)
	}

	return nil
}

func (p *Provider) callDashboardAPI(ctx context.Context, token, method string, result interface{}) error {
	url := fmt.Sprintf("%s/aiserver.v1.DashboardService/%s", cursorAPIBase, method)
	return p.doPost(ctx, token, url, result)
}

func (p *Provider) callRESTAPI(ctx context.Context, token, path string, result interface{}) error {
	url := cursorAPIBase + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (p *Provider) doPost(ctx context.Context, token, url string, result interface{}) error {
	body := []byte("{}")
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func applyModelAggregations(snap *core.QuotaSnapshot, aggregations []modelAggregation) bool {
	if len(aggregations) == 0 {
		return false
	}
	if snap.Metrics == nil {
		snap.Metrics = make(map[string]core.Metric)
	}
	if snap.Raw == nil {
		snap.Raw = make(map[string]string)
	}

	var applied bool
	for _, agg := range aggregations {
		modelIntent := strings.TrimSpace(agg.ModelIntent)
		if modelIntent == "" {
			continue
		}

		inputTokens := strings.TrimSpace(agg.InputTokens)
		outputTokens := strings.TrimSpace(agg.OutputTokens)

		if agg.TotalCents > 0 {
			costDollars := agg.TotalCents / 100.0
			snap.Metrics[fmt.Sprintf("model_%s_cost", modelIntent)] = core.Metric{
				Used:   &costDollars,
				Unit:   "USD",
				Window: "billing-cycle",
			}
		}
		if inputTokens != "" {
			snap.Raw[fmt.Sprintf("model_%s_input_tokens", modelIntent)] = inputTokens
		}
		if outputTokens != "" {
			snap.Raw[fmt.Sprintf("model_%s_output_tokens", modelIntent)] = outputTokens
		}
		if agg.TotalCents > 0 || inputTokens != "" || outputTokens != "" {
			applied = true
		}
	}
	return applied
}

func (p *Provider) storeModelAggregationCache(accountID, billingCycleStart, billingCycleEnd string, aggregations []modelAggregation) {
	if accountID == "" || len(aggregations) == 0 {
		return
	}
	copied := make([]modelAggregation, len(aggregations))
	copy(copied, aggregations)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.modelAggregationCache == nil {
		p.modelAggregationCache = make(map[string]cachedModelAggregation)
	}
	p.modelAggregationCache[accountID] = cachedModelAggregation{
		BillingCycleStart: billingCycleStart,
		BillingCycleEnd:   billingCycleEnd,
		Aggregations:      copied,
	}
}

func (p *Provider) applyCachedModelAggregations(accountID, billingCycleStart, billingCycleEnd string, snap *core.QuotaSnapshot) bool {
	if accountID == "" {
		return false
	}

	p.mu.RLock()
	cached, ok := p.modelAggregationCache[accountID]
	p.mu.RUnlock()
	if !ok || len(cached.Aggregations) == 0 {
		return false
	}

	if billingCycleStart != "" && cached.BillingCycleStart != "" && billingCycleStart != cached.BillingCycleStart {
		return false
	}
	if billingCycleEnd != "" && cached.BillingCycleEnd != "" && billingCycleEnd != cached.BillingCycleEnd {
		return false
	}

	copied := make([]modelAggregation, len(cached.Aggregations))
	copy(copied, cached.Aggregations)
	return applyModelAggregations(snap, copied)
}

func (p *Provider) readTrackingDB(ctx context.Context, dbPath string, snap *core.QuotaSnapshot) error {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL", dbPath))
	if err != nil {
		return fmt.Errorf("opening tracking DB: %w", err)
	}
	defer db.Close()

	var totalRequests int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_code_hashes`).Scan(&totalRequests)
	if err != nil {
		return fmt.Errorf("querying total requests: %w", err)
	}

	if totalRequests > 0 {
		total := float64(totalRequests)
		snap.Metrics["total_ai_requests"] = core.Metric{
			Used:   &total,
			Unit:   "requests",
			Window: "all-time",
		}
	}

	todayStart := time.Now().Truncate(24 * time.Hour).UnixMilli()
	var todayCount int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ai_code_hashes WHERE createdAt >= ?`, todayStart).Scan(&todayCount)
	if err == nil && todayCount > 0 {
		tc := float64(todayCount)
		snap.Metrics["requests_today"] = core.Metric{
			Used:   &tc,
			Unit:   "requests",
			Window: "1d",
		}
	}

	return nil
}

func (p *Provider) readStateDB(ctx context.Context, dbPath string, snap *core.QuotaSnapshot) error {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL", dbPath))
	if err != nil {
		return fmt.Errorf("opening state DB: %w", err)
	}
	defer db.Close()

	today := time.Now().Format("2006-01-02")
	key := fmt.Sprintf("aiCodeTracking.dailyStats.v1.5.%s", today)

	var value string
	err = db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = ?`, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
			key = fmt.Sprintf("aiCodeTracking.dailyStats.v1.5.%s", yesterday)
			err = db.QueryRowContext(ctx, `SELECT value FROM ItemTable WHERE key = ?`, key).Scan(&value)
			if err != nil {
				return nil
			}
		} else {
			return fmt.Errorf("querying daily stats: %w", err)
		}
	}

	var stats dailyStats
	if err := json.Unmarshal([]byte(value), &stats); err != nil {
		return fmt.Errorf("parsing daily stats: %w", err)
	}

	if stats.TabSuggestedLines > 0 {
		suggested := float64(stats.TabSuggestedLines)
		accepted := float64(stats.TabAcceptedLines)
		snap.Metrics["tab_suggested_lines"] = core.Metric{Used: &suggested, Unit: "lines", Window: "1d"}
		snap.Metrics["tab_accepted_lines"] = core.Metric{Used: &accepted, Unit: "lines", Window: "1d"}
	}

	if stats.ComposerSuggestedLines > 0 {
		suggested := float64(stats.ComposerSuggestedLines)
		accepted := float64(stats.ComposerAcceptedLines)
		snap.Metrics["composer_suggested_lines"] = core.Metric{Used: &suggested, Unit: "lines", Window: "1d"}
		snap.Metrics["composer_accepted_lines"] = core.Metric{Used: &accepted, Unit: "lines", Window: "1d"}
	}

	p.readDailyStatsSeries(ctx, db, snap)

	var email string
	err = db.QueryRowContext(ctx,
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/cachedEmail'`).Scan(&email)
	if err == nil && email != "" {
		snap.Raw["account_email"] = email
	}

	return nil
}

func (p *Provider) readDailyStatsSeries(ctx context.Context, db *sql.DB, snap *core.QuotaSnapshot) {
	rows, err := db.QueryContext(ctx,
		`SELECT key, value FROM ItemTable WHERE key LIKE 'aiCodeTracking.dailyStats.v1.5.%' ORDER BY key ASC`)
	if err != nil {
		return
	}
	defer rows.Close()

	prefix := "aiCodeTracking.dailyStats.v1.5."
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		dateStr := strings.TrimPrefix(k, prefix)
		if len(dateStr) != 10 { // "2025-01-15"
			continue
		}

		var ds dailyStats
		if json.Unmarshal([]byte(v), &ds) != nil {
			continue
		}

		if ds.TabSuggestedLines > 0 || ds.TabAcceptedLines > 0 {
			snap.DailySeries["tab_suggested"] = append(snap.DailySeries["tab_suggested"],
				core.TimePoint{Date: dateStr, Value: float64(ds.TabSuggestedLines)})
			snap.DailySeries["tab_accepted"] = append(snap.DailySeries["tab_accepted"],
				core.TimePoint{Date: dateStr, Value: float64(ds.TabAcceptedLines)})
		}

		if ds.ComposerSuggestedLines > 0 || ds.ComposerAcceptedLines > 0 {
			snap.DailySeries["composer_suggested"] = append(snap.DailySeries["composer_suggested"],
				core.TimePoint{Date: dateStr, Value: float64(ds.ComposerSuggestedLines)})
			snap.DailySeries["composer_accepted"] = append(snap.DailySeries["composer_accepted"],
				core.TimePoint{Date: dateStr, Value: float64(ds.ComposerAcceptedLines)})
		}

		totalLines := float64(ds.TabSuggestedLines + ds.ComposerSuggestedLines)
		if totalLines > 0 {
			snap.DailySeries["total_lines"] = append(snap.DailySeries["total_lines"],
				core.TimePoint{Date: dateStr, Value: totalLines})
		}
	}
}

func parseTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		if ms > 1e12 { // epoch millis
			return time.Unix(ms/1000, (ms%1000)*1e6)
		}
		return time.Unix(ms, 0) // epoch secs
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func formatTimestamp(s string) string {
	t := parseTimestamp(s)
	if t.IsZero() {
		return s // return as-is if we can't parse
	}
	return t.Format("Jan 02, 2006 15:04 MST")
}
