// Package cursor implements a QuotaProvider for the Cursor IDE.
//
// It uses TWO data sources, in priority order:
//
//  1. Cursor's internal gRPC/connect-rpc DashboardService API
//     (api2.cursor.sh) — provides real-time billing, spend, plan info,
//     per-model token aggregations, and individual usage events.
//     Authentication is via the access token stored in state.vscdb.
//
//  2. Local SQLite databases as fallback:
//     - ~/.cursor/ai-tracking/ai-code-tracking.db — per-request tracking
//     - ~/Library/Application Support/Cursor/User/globalStorage/state.vscdb
//     — daily stats (lines suggested/accepted for tab & composer)
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
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

const (
	cursorAPIBase = "https://api2.cursor.sh"

	// pricingSummary lists Cursor IDE subscription plans (as of 2025).
	pricingSummary = "Subscription plans · " +
		"Hobby: Free (2 000 completions, 50 slow premium requests/mo) · " +
		"Pro: $20/mo (unlimited completions, 500 fast premium requests/mo, unlimited slow premium) · " +
		"Pro (annual): $192/yr ($16/mo) · " +
		"Business: $40/user/mo (admin dashboard, SAML SSO, centralized billing, enforced privacy mode) · " +
		"Enterprise: Custom pricing · " +
		"Usage-based: pay-as-you-go after included budget; " +
		"models charged at provider rates (e.g. Claude Sonnet ~$3/$15 per 1M tok in/out, " +
		"GPT-4o ~$2.50/$10 per 1M tok in/out, o3-mini ~$1.10/$4.40 per 1M tok in/out) · " +
		"Fast premium requests consume 1 credit each; some models cost more (o1 = 10 credits)"
)

// Provider reads Cursor IDE usage via the DashboardService API + local DBs.
type Provider struct{}

// New returns a new Cursor provider instance.
func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "cursor" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "Cursor IDE",
		Capabilities: []string{"dashboard_api", "billing", "spend_tracking", "model_aggregation", "local_tracking"},
		DocURL:       "https://www.cursor.com/",
	}
}

// ── API response types ──────────────────────────────────────────────────────

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

// ── dailyStats for local DB fallback ────────────────────────────────────────

type dailyStats struct {
	Date                   string `json:"date"`
	TabSuggestedLines      int    `json:"tabSuggestedLines"`
	TabAcceptedLines       int    `json:"tabAcceptedLines"`
	ComposerSuggestedLines int    `json:"composerSuggestedLines"`
	ComposerAcceptedLines  int    `json:"composerAcceptedLines"`
}

// ── Fetch ───────────────────────────────────────────────────────────────────

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Status:     core.StatusOK,
		Metrics:    make(map[string]core.Metric),
		Raw:        make(map[string]string),
	}

	snap.Raw["pricing_summary"] = pricingSummary

	// Try the API first if we have a token
	if acct.Token != "" {
		apiErr := p.fetchFromAPI(ctx, acct.Token, &snap)
		if apiErr == nil {
			return snap, nil
		}
		log.Printf("[cursor] API fetch failed, falling back to local data: %v", apiErr)
		snap.Raw["api_error"] = apiErr.Error()
	}

	// Fallback to local databases
	trackingDBPath := ""
	stateDBPath := ""
	if acct.ExtraData != nil {
		trackingDBPath = acct.ExtraData["tracking_db"]
		stateDBPath = acct.ExtraData["state_db"]
	}
	// Also try the legacy field mapping
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

	snap.Message = "Local Cursor IDE usage tracking (API unavailable)"
	return snap, nil
}

// ── API fetching ────────────────────────────────────────────────────────────

func (p *Provider) fetchFromAPI(ctx context.Context, token string, snap *core.QuotaSnapshot) error {
	// 1. GetCurrentPeriodUsage — THE MAIN DATA SOURCE
	var periodUsage currentPeriodUsageResp
	if err := p.callDashboardAPI(ctx, token, "GetCurrentPeriodUsage", &periodUsage); err != nil {
		return fmt.Errorf("GetCurrentPeriodUsage: %w", err)
	}

	// Plan usage metrics
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

	// Spend limit metrics (team or individual)
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

	// Store billing cycle as reset timers for nice display
	if t := parseTimestamp(periodUsage.BillingCycleEnd); !t.IsZero() {
		if snap.Resets != nil {
			snap.Resets["billing_cycle_end"] = t
		}
	}

	// Determine status based on EFFECTIVE budget, not just plan-included usage.
	// For team users, the spend limit is what matters — plan can be 100% used
	// but the user still has ample budget via the team spend limit.
	if su.PooledLimit > 0 && su.PooledRemaining > 0 {
		// Team/pooled spend limit exists — use THAT for status
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

	// Also store plan_total_spend_usd so the summary can show dollar amounts
	snap.Metrics["plan_total_spend_usd"] = core.Metric{
		Used:   &totalSpendDollars,
		Limit:  &limitDollars,
		Unit:   "USD",
		Window: "billing-cycle",
	}
	// Store the effective budget metric: whichever is the "real" ceiling
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

	// 2. GetPlanInfo
	var planInfo planInfoResp
	if err := p.callDashboardAPI(ctx, token, "GetPlanInfo", &planInfo); err == nil {
		snap.Raw["plan_name"] = planInfo.PlanInfo.PlanName
		snap.Raw["plan_price"] = planInfo.PlanInfo.Price
	}

	// 3. GetAggregatedUsageEvents — per-model breakdown
	var aggUsage aggregatedUsageResp
	if err := p.callDashboardAPI(ctx, token, "GetAggregatedUsageEvents", &aggUsage); err == nil {
		var totalCostCents float64
		for _, agg := range aggUsage.Aggregations {
			costDollars := agg.TotalCents / 100.0
			key := fmt.Sprintf("model_%s_cost", agg.ModelIntent)
			snap.Metrics[key] = core.Metric{
				Used:   &costDollars,
				Unit:   "USD",
				Window: "billing-cycle",
			}
			snap.Raw[fmt.Sprintf("model_%s_input_tokens", agg.ModelIntent)] = agg.InputTokens
			snap.Raw[fmt.Sprintf("model_%s_output_tokens", agg.ModelIntent)] = agg.OutputTokens
			totalCostCents += agg.TotalCents
		}
	}

	// 4. GetHardLimit
	var hardLimit hardLimitResp
	if err := p.callDashboardAPI(ctx, token, "GetHardLimit", &hardLimit); err == nil {
		if hardLimit.NoUsageBasedAllowed {
			snap.Raw["usage_based_billing"] = "disabled"
		} else {
			snap.Raw["usage_based_billing"] = "enabled"
		}
	}

	// 5. Stripe profile (REST endpoint)
	var profile stripeProfileResp
	if err := p.callRESTAPI(ctx, token, "/auth/full_stripe_profile", &profile); err == nil {
		snap.Raw["membership_type"] = profile.MembershipType
		snap.Raw["team_membership"] = profile.TeamMembershipType
		snap.Raw["individual_membership"] = profile.IndividualMembershipType
		if profile.IsTeamMember {
			snap.Raw["team_id"] = fmt.Sprintf("%.0f", profile.TeamID)
		}
	}

	// 6. GetUsageLimitPolicyStatus
	var limitPolicy usageLimitPolicyResp
	if err := p.callDashboardAPI(ctx, token, "GetUsageLimitPolicyStatus", &limitPolicy); err == nil {
		snap.Raw["limit_policy_type"] = limitPolicy.LimitType
	}

	// Build a more informative message
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

// callDashboardAPI makes a connect-rpc call to the Cursor DashboardService.
func (p *Provider) callDashboardAPI(ctx context.Context, token, method string, result interface{}) error {
	url := fmt.Sprintf("%s/aiserver.v1.DashboardService/%s", cursorAPIBase, method)
	return p.doPost(ctx, token, url, result)
}

// callRESTAPI makes a GET request to a Cursor REST endpoint.
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

// doPost sends a JSON POST request (connect-rpc style).
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

// ── Local DB fallback methods (unchanged) ───────────────────────────────────

func (p *Provider) readTrackingDB(ctx context.Context, dbPath string, snap *core.QuotaSnapshot) error {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL", dbPath))
	if err != nil {
		return fmt.Errorf("opening tracking DB: %w", err)
	}
	defer db.Close()

	// Get total request count
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

	// Get today's count
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

	// Read auth email for display
	var email string
	err = db.QueryRowContext(ctx,
		`SELECT value FROM ItemTable WHERE key = 'cursorAuth/cachedEmail'`).Scan(&email)
	if err == nil && email != "" {
		snap.Raw["account_email"] = email
	}

	return nil
}

// parseTimestamp tries to parse a string as epoch millis, epoch secs, or ISO-8601.
func parseTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// Try epoch millis (e.g. "1770733695000")
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		if ms > 1e12 { // epoch millis
			return time.Unix(ms/1000, (ms%1000)*1e6)
		}
		return time.Unix(ms, 0) // epoch secs
	}
	// Try ISO-8601
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

// formatTimestamp converts a raw timestamp string to a human-readable date.
func formatTimestamp(s string) string {
	t := parseTimestamp(s)
	if t.IsZero() {
		return s // return as-is if we can't parse
	}
	return t.Format("Jan 02, 2006 15:04 MST")
}
