package command_code

import (
	"context"
	"fmt"
	"net/http"
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
				providerbase.WithGaugePriority("five_hour_usage", "weekly_usage", "balance"),
				providerbase.WithGaugeMaxLines(2),
				providerbase.WithCompactRows(
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
					"five_hour_usage": "5h Limit",
					"weekly_usage":    "Weekly Limit",
					"balance":         "Balance",
					"total_cost":      "Period Spend",
					"total_tokens":    "Period Tokens",
				}),
				providerbase.WithCompactLabels(map[string]string{
					"five_hour_usage": "5h",
					"weekly_usage":    "wk",
					"balance":         "bal",
					"total_cost":      "cost",
					"total_tokens":    "tok",
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
	Org struct {
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

	// 1. Fetch Credits & Window Limits
	var creds creditsResponse
	statusCode, _, err := shared.FetchJSON(ctx, baseURL+creditsPath, apiKey, &creds, p.Client())
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

	// 2. Fetch Subscription
	var sub subscriptionResponse
	if _, _, err := shared.FetchJSON(ctx, baseURL+subscriptionPath, apiKey, &sub, p.Client()); err == nil && sub.Success {
		if sub.Data.PlanID != "" {
			snap.SetAttribute("plan_id", sub.Data.PlanID)
		}
		if sub.Data.Status != "" {
			snap.SetAttribute("subscription_status", sub.Data.Status)
		}
		if periodEnd, parseErr := time.Parse(time.RFC3339, sub.Data.CurrentPeriodEnd); parseErr == nil {
			snap.Resets["billing_period"] = periodEnd
		}
	}

	// 3. Fetch Whoami (User / Org)
	var who whoamiResponse
	if _, _, err := shared.FetchJSON(ctx, baseURL+whoamiPath, apiKey, &who, p.Client()); err == nil && who.Success {
		if who.User.Name != "" {
			snap.SetAttribute("user_name", who.User.Name)
		}
		if who.User.Email != "" {
			snap.SetAttribute("user_email", who.User.Email)
		}
		if who.User.UserName != "" {
			snap.SetAttribute("user_handle", who.User.UserName)
		}
	}

	// 4. Fetch Usage Summary (Spend & Tokens)
	var sum summaryResponse
	if _, _, err := shared.FetchJSON(ctx, baseURL+summaryPath, apiKey, &sum, p.Client()); err == nil {
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

	// Status calculation
	if creds.WindowLimits.Limited || creds.WindowLimits.Exceeded != "" || (weeklyCap > 0 && weeklyUsedDollars >= weeklyCap) || (fiveHourCap > 0 && fiveHourUsedDollars >= fiveHourCap) {
		snap.Status = core.StatusLimited
	} else {
		snap.Status = core.StatusOK
	}

	// Message formatting
	planLabel := "Command Code"
	if planID := snap.Attributes["plan_id"]; planID != "" {
		planLabel = fmt.Sprintf("Command Code (%s)", strings.ReplaceAll(planID, "-", " "))
	}
	if snap.Status == core.StatusLimited {
		if creds.WindowLimits.Exceeded == "weekly" || (weeklyCap > 0 && weeklyUsedDollars >= weeklyCap) {
			snap.Message = fmt.Sprintf("%s · Weekly Limit Reached", planLabel)
		} else if creds.WindowLimits.Exceeded == "fiveHour" || (fiveHourCap > 0 && fiveHourUsedDollars >= fiveHourCap) {
			snap.Message = fmt.Sprintf("%s · 5h Limit Reached", planLabel)
		} else {
			snap.Message = fmt.Sprintf("%s · Rate Limited", planLabel)
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
