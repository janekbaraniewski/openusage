package zai

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.DefaultDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleSapphire),
		providerbase.WithGaugePriority(
			"usage_five_hour",
			"tokens_five_hour",
			"mcp_monthly_usage",
			"credit_balance",
			"7d_api_cost",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{Label: "Usage", Keys: []string{"usage_five_hour", "tokens_five_hour", "mcp_monthly_usage", "7d_tokens"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Spend", Keys: []string{"credit_balance", "today_api_cost", "7d_api_cost"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Activity", Keys: []string{"today_requests", "today_input_tokens", "today_output_tokens", "tool_calls_today", "active_models"}, MaxSegments: 5},
		),
		providerbase.WithHideMetricPrefixes("model_", "tool_"),
		providerbase.WithRawGroups(
			core.DashboardRawGroup{
				Label: "Account",
				Keys: []string{
					"provider_region", "plan_type", "subscription_status", "models_count", "active_model", "auth_type",
				},
			},
			core.DashboardRawGroup{
				Label: "Usage APIs",
				Keys: []string{
					"quota_api", "model_usage_api", "tool_usage_api", "credits_api",
					"quota_limit_error", "model_usage_error", "tool_usage_error", "credits_error",
				},
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"usage_five_hour":   "5h Token Usage",
			"tokens_five_hour":  "5h Tokens",
			"mcp_monthly_usage": "MCP Monthly",
			"today_api_cost":    "Today Cost",
			"7d_api_cost":       "7-Day Cost",
			"today_requests":    "Today Requests",
			"tool_calls_today":  "Today Tool Calls",
			"active_models":     "Active Models",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"usage_five_hour":   "5h",
			"tokens_five_hour":  "5h tok",
			"mcp_monthly_usage": "mcp",
			"today_api_cost":    "today",
			"7d_api_cost":       "7d",
			"today_requests":    "req",
			"tool_calls_today":  "tools",
			"active_models":     "models",
		}),
	)
	cfg.DisplayStyle = core.DashboardDisplayStyleDetailedCredits
	cfg.HideCreditsWhenBalancePresent = true
	cfg.SuppressZeroNonUsageMetrics = true
	return cfg
}
