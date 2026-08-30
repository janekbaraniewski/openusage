package command_code

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	defaultBaseURL   = "https://api.commandcode.ai"
	whoamiPath       = "/alpha/whoami"
	creditsPath      = "/alpha/billing/credits"
	subscriptionPath = "/alpha/billing/subscriptions"
	summaryPath      = "/alpha/usage/summary"
)

var planCredits = map[string]float64{
	"individual-goat":     70.0,
	"individual-go":       10.0,
	"individual-pro":      30.0,
	"individual-pro-v1":   80.0,
	"individual-provider": 15.0,
	"individual-max":      150.0,
	"individual-ultra":    300.0,
	"teams-pro":           40.0,
}

var planDisplayNames = map[string]string{
	"individual-goat":     "GOAT",
	"individual-go":       "Go",
	"individual-pro":      "Pro",
	"individual-pro-v1":   "Pro",
	"individual-provider": "Provider",
	"individual-max":      "Max",
	"individual-ultra":    "Ultra",
	"teams-pro":           "Teams Pro",
}

func getPlanCap(planID string) float64 {
	p := strings.ToLower(strings.ReplaceAll(planID, "_", "-"))
	if v, ok := planCredits[p]; ok {
		return v
	}
	type kv struct {
		k string
		v float64
	}
	keys := make([]kv, 0, len(planCredits))
	for k, v := range planCredits {
		keys = append(keys, kv{k, v})
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i].k) > len(keys[j].k)
	})
	for _, item := range keys {
		if strings.HasPrefix(p, item.k) {
			return item.v
		}
	}
	return 0
}

func getPlanDisplayName(planID string) string {
	p := strings.ToLower(strings.ReplaceAll(planID, "_", "-"))
	if v, ok := planDisplayNames[p]; ok {
		return v
	}
	type kv struct {
		k string
		v string
	}
	keys := make([]kv, 0, len(planDisplayNames))
	for k, v := range planDisplayNames {
		keys = append(keys, kv{k, v})
	}
	sort.Slice(keys, func(i, j int) bool {
		return len(keys[i].k) > len(keys[j].k)
	})
	for _, item := range keys {
		if strings.HasPrefix(p, item.k) {
			return item.v
		}
	}
	cleaned := strings.TrimPrefix(planID, "individual-")
	cleaned = strings.TrimPrefix(cleaned, "teams-")
	return strings.ToUpper(strings.ReplaceAll(cleaned, "-", " "))
}

func fetchCommandCodeJSON(ctx context.Context, url, apiKey string, out any, client *http.Client) (int, http.Header, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", "cli")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, resp.Header, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, resp.Header, fmt.Errorf("reading body: %w", err)
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, resp.Header, fmt.Errorf("parsing response: %w", err)
		}
	}
	return resp.StatusCode, resp.Header, nil
}

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "command_code",
			Info: core.ProviderInfo{
				Name:         "Command Code",
				Capabilities: []string{"usage_endpoint", "credits_endpoint", "subscription_endpoint"},
				DocURL:       "https://commandcode.ai/docs",
			},
			Auth: core.ProviderAuthSpec{
				Type: core.ProviderAuthTypeAPIKey,
			},
			Dashboard: providerbase.DefaultDashboard(
				providerbase.WithColorRole(core.DashboardColorRoleBlue),
				providerbase.WithGaugePriority("monthly_subscription", "weekly_usage", "five_hour_usage", "balance"),
				providerbase.WithGaugeMaxLines(3),
				providerbase.WithCompactRows(
					core.DashboardCompactRow{
						Label:       "Subscription",
						Keys:        []string{"monthly_subscription", "monthly_credits"},
						MaxSegments: 2,
					},
					core.DashboardCompactRow{
						Label:       "Quota",
						Keys:        []string{"five_hour_usage", "weekly_usage"},
						MaxSegments: 2,
					},
					core.DashboardCompactRow{
						Label:       "Credits",
						Keys:        []string{"balance", "total_cost", "total_tokens"},
						MaxSegments: 3,
					},
				),
				providerbase.WithMetricLabels(map[string]string{
					"monthly_subscription": "Monthly Subscription",
					"monthly_credits":      "Monthly Credits",
					"five_hour_usage":      "5h Limit",
					"weekly_usage":         "Weekly Limit",
					"balance":              "Balance",
					"total_cost":           "Period Spend",
					"total_tokens":         "Period Tokens",
				}),
				providerbase.WithCompactLabels(map[string]string{
					"monthly_subscription": "sub",
					"monthly_credits":      "credits",
					"five_hour_usage":      "5h",
					"weekly_usage":         "wk",
					"balance":              "bal",
					"total_cost":           "cost",
					"total_tokens":         "tok",
				}),
			),
		}),
	}
}

type creditsResponse struct {
	Credits struct {
		MonthlyCredits   float64 `json:"monthlyCredits"`
		PurchasedCredits float64 `json:"purchasedCredits"`
		FreeCredits      float64 `json:"freeCredits"`
	} `json:"credits"`
	WindowLimits struct {
		Limited  bool   `json:"limited"`
		Exceeded string `json:"exceeded"`
		FiveHour struct {
			Used     float64 `json:"used"`
			Cap      float64 `json:"cap"`
			Exceeded bool    `json:"exceeded"`
			ResetAt  int64   `json:"resetAt"`
		} `json:"fiveHour"`
		Weekly struct {
			Used     float64 `json:"used"`
			Cap      float64 `json:"cap"`
			Exceeded bool    `json:"exceeded"`
			ResetAt  int64   `json:"resetAt"`
		} `json:"weekly"`
	} `json:"windowLimits"`
}

type subscriptionResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ID                 string `json:"id"`
		PlanID             string `json:"planId"`
		Status             string `json:"status"`
		CurrentPeriodStart string `json:"currentPeriodStart"`
		CurrentPeriodEnd   string `json:"currentPeriodEnd"`
	} `json:"data"`
}

type whoamiResponse struct {
	Success bool `json:"success"`
	User    struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		UserName string `json:"userName"`
		Email    string `json:"email"`
	} `json:"user"`
	Org *struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Login string `json:"login"`
	} `json:"org"`
}

type summaryResponse struct {
	TotalCost   float64 `json:"totalCost"`
	TotalTokens int64   `json:"totalTokens"`
	TotalCount  int64   `json:"totalCount"`
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.SetAttribute("api_base_url", baseURL)

	// 1. Fetch Whoami (User / Org)
	var who whoamiResponse
	orgQuery := ""
	if _, _, err := fetchCommandCodeJSON(ctx, baseURL+whoamiPath, apiKey, &who, p.Client()); err == nil && who.Success {
		if who.User.Name != "" {
			snap.SetAttribute("user_name", who.User.Name)
		}
		if who.User.Email != "" {
			snap.SetAttribute("user_email", who.User.Email)
		}
		if who.User.UserName != "" {
			snap.SetAttribute("user_handle", who.User.UserName)
		}
		if who.Org != nil && who.Org.ID != "" {
			snap.SetAttribute("org_id", who.Org.ID)
			orgQuery = "?orgId=" + who.Org.ID
		}
	}

	// 2. Fetch Credits & Window Limits
	var creds creditsResponse
	statusCode, _, err := fetchCommandCodeJSON(ctx, baseURL+creditsPath+orgQuery, apiKey, &creds, p.Client())
	if err != nil {
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			snap.Status = core.StatusAuth
			snap.Message = fmt.Sprintf("HTTP %d – check COMMAND_CODE_API_KEY", statusCode)
			return snap, nil
		}
		return snap, fmt.Errorf("command code credits: %w", err)
	}

	// Balance = MonthlyCredits + PurchasedCredits + FreeCredits
	totalBalance := creds.Credits.MonthlyCredits + creds.Credits.PurchasedCredits + creds.Credits.FreeCredits
	snap.Metrics["balance"] = core.Metric{
		Remaining: &totalBalance,
		Unit:      "USD",
	}

	// 5-Hour Window Limit
	fiveHourCap := creds.WindowLimits.FiveHour.Cap
	fiveHourUsedDollars := creds.WindowLimits.FiveHour.Used
	if fiveHourCap > 0 {
		fiveHourUsedPct := (fiveHourUsedDollars / fiveHourCap) * 100
		fiveHourRemPct := 100 - fiveHourUsedPct
		if fiveHourRemPct < 0 {
			fiveHourRemPct = 0
		}
		snap.Metrics["five_hour_usage"] = core.Metric{
			Used:      &fiveHourUsedPct,
			Remaining: &fiveHourRemPct,
			Unit:      "percent",
			Window:    "5h",
		}
		snap.SetAttribute("five_hour_cap", fmt.Sprintf("$%.2f", fiveHourCap))
		snap.SetAttribute("five_hour_used", fmt.Sprintf("$%.2f", fiveHourUsedDollars))
		if creds.WindowLimits.FiveHour.ResetAt > 0 {
			snap.Resets["five_hour_usage"] = time.UnixMilli(creds.WindowLimits.FiveHour.ResetAt)
		}
	}

	// Weekly Window Limit
	weeklyCap := creds.WindowLimits.Weekly.Cap
	weeklyUsedDollars := creds.WindowLimits.Weekly.Used
	if weeklyCap > 0 {
		weeklyUsedPct := (weeklyUsedDollars / weeklyCap) * 100
		weeklyRemPct := 100 - weeklyUsedPct
		if weeklyRemPct < 0 {
			weeklyRemPct = 0
		}
		snap.Metrics["weekly_usage"] = core.Metric{
			Used:      &weeklyUsedPct,
			Remaining: &weeklyRemPct,
			Unit:      "percent",
			Window:    "7d",
		}
		snap.SetAttribute("weekly_cap", fmt.Sprintf("$%.2f", weeklyCap))
		snap.SetAttribute("weekly_used", fmt.Sprintf("$%.2f", weeklyUsedDollars))
		if creds.WindowLimits.Weekly.ResetAt > 0 {
			snap.Resets["weekly_usage"] = time.UnixMilli(creds.WindowLimits.Weekly.ResetAt)
		}
	}

	// 3. Fetch Subscription
	var sub subscriptionResponse
	if _, _, err := fetchCommandCodeJSON(ctx, baseURL+subscriptionPath+orgQuery, apiKey, &sub, p.Client()); err == nil && sub.Success {
		if sub.Data.PlanID != "" {
			snap.SetAttribute("plan_id", sub.Data.PlanID)
			snap.SetAttribute("plan_name", getPlanDisplayName(sub.Data.PlanID))
		}
		if sub.Data.Status != "" {
			snap.SetAttribute("subscription_status", sub.Data.Status)
		}
		if sub.Data.CurrentPeriodStart != "" {
			snap.SetAttribute("billing_cycle_start", sub.Data.CurrentPeriodStart)
		}
		if sub.Data.CurrentPeriodEnd != "" {
			snap.SetAttribute("billing_cycle_end", sub.Data.CurrentPeriodEnd)
			if periodEnd, parseErr := time.Parse(time.RFC3339, sub.Data.CurrentPeriodEnd); parseErr == nil {
				snap.Resets["billing_period"] = periodEnd
				snap.Resets["billing_cycle_end"] = periodEnd
				snap.Resets["monthly_subscription"] = periodEnd
				snap.Resets["monthly_credits"] = periodEnd
			}
		}
	}

	// 4. Fetch Usage Summary (Spend & Tokens)
	var sum summaryResponse
	summaryURL := baseURL + summaryPath + orgQuery
	if sub.Data.CurrentPeriodStart != "" {
		sep := "?"
		if strings.Contains(summaryURL, "?") {
			sep = "&"
		}
		summaryURL += sep + "since=" + sub.Data.CurrentPeriodStart
	}
	if _, _, err := fetchCommandCodeJSON(ctx, summaryURL, apiKey, &sum, p.Client()); err == nil {
		cost := sum.TotalCost
		snap.Metrics["total_cost"] = core.Metric{
			Used:   &cost,
			Unit:   "USD",
			Window: "billing-period",
		}
		toks := float64(sum.TotalTokens)
		snap.Metrics["total_tokens"] = core.Metric{
			Used:   &toks,
			Unit:   "tokens",
			Window: "billing-period",
		}
	}

	// 5. Compute Monthly Subscription Plan & Credits
	planCap := getPlanCap(sub.Data.PlanID)
	if planCap <= 0 && (creds.Credits.MonthlyCredits > 0 || sum.TotalCost > 0) {
		planCap = creds.Credits.MonthlyCredits + sum.TotalCost
	}
	if planCap > 0 {
		monthlyRemaining := creds.Credits.MonthlyCredits
		monthlyUsed := sum.TotalCost
		if monthlyUsed <= 0 && planCap >= monthlyRemaining {
			monthlyUsed = planCap - monthlyRemaining
		}
		usedPct := (monthlyUsed / planCap) * 100
		if usedPct < 0 {
			usedPct = 0
		}
		if usedPct > 100 {
			usedPct = 100
		}
		remPct := 100 - usedPct

		snap.Metrics["monthly_subscription"] = core.Metric{
			Limit:     core.Float64Ptr(100),
			Used:      core.Float64Ptr(usedPct),
			Remaining: core.Float64Ptr(remPct),
			Unit:      "percent",
			Window:    "month",
		}
		snap.Metrics["monthly_credits"] = core.Metric{
			Limit:     core.Float64Ptr(planCap),
			Used:      core.Float64Ptr(monthlyUsed),
			Remaining: core.Float64Ptr(monthlyRemaining),
			Unit:      "USD",
			Window:    "month",
		}
		snap.SetAttribute("monthly_cap", fmt.Sprintf("$%.2f", planCap))
		snap.SetAttribute("monthly_used", fmt.Sprintf("$%.2f", monthlyUsed))
		snap.SetAttribute("monthly_remaining", fmt.Sprintf("$%.2f", monthlyRemaining))
	}

	// Status calculation: only mark limited when a concrete window is exhausted.
	// windowLimits.limited alone is not reliable (API may set it while windows still have headroom).
	weeklyExceeded := creds.WindowLimits.Exceeded == "weekly" ||
		creds.WindowLimits.Weekly.Exceeded ||
		(weeklyCap > 0 && weeklyUsedDollars >= weeklyCap)
	fiveHourExceeded := creds.WindowLimits.Exceeded == "fiveHour" ||
		creds.WindowLimits.FiveHour.Exceeded ||
		(fiveHourCap > 0 && fiveHourUsedDollars >= fiveHourCap)
	if weeklyExceeded || fiveHourExceeded {
		snap.Status = core.StatusLimited
	} else {
		snap.Status = core.StatusOK
	}

	// Message formatting
	planLabel := "Command Code"
	if pn := snap.Attributes["plan_name"]; pn != "" {
		planLabel = fmt.Sprintf("Command Code (%s)", pn)
	} else if planID := snap.Attributes["plan_id"]; planID != "" {
		planLabel = fmt.Sprintf("Command Code (%s)", strings.ReplaceAll(planID, "-", " "))
	}
	if snap.Status == core.StatusLimited {
		if weeklyExceeded {
			snap.Message = fmt.Sprintf("%s · Weekly Limit Reached", planLabel)
		} else if fiveHourExceeded {
			snap.Message = fmt.Sprintf("%s · 5h Limit Reached", planLabel)
		}
	} else {
		if wu, ok := snap.Metrics["weekly_usage"]; ok && wu.Remaining != nil {
			snap.Message = fmt.Sprintf("%s · %.1f%% wk rem", planLabel, *wu.Remaining)
		} else {
			snap.Message = fmt.Sprintf("%s · $%.2f", planLabel, totalBalance)
		}
	}

	return snap, nil
}
