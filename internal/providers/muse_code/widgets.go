package muse_code

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.CodingToolDashboard(
		// Meta brand blue. Shared with a few API providers (and gemini_cli);
		// bump if the maintainer prefers a unique role.
		providerbase.WithColorRole(core.DashboardColorRoleBlue),
		providerbase.WithGaugePriority(
			"muse.session", "muse.weekly",
			"total_sessions", "total_tokens", "total_cost_usd",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Sessions",
				Keys:        []string{"total_sessions", "sessions_today", "sessions_7d"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"total_tokens", "total_input_tokens", "total_output_tokens"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Cost",
				Keys:        []string{"total_cost_usd", "today_cost"},
				MaxSegments: 4,
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"muse.session":        "Session Usage",
			"muse.weekly":         "Weekly Usage",
			"total_sessions":      "Sessions",
			"sessions_today":      "Sessions Today",
			"sessions_7d":         "Sessions 7d",
			"total_tokens":        "Total Tokens",
			"total_input_tokens":  "Input Tokens",
			"total_output_tokens": "Output Tokens",
			"total_cache_read":    "Cache Read",
			"total_cache_write":   "Cache Write",
			"total_cost_usd":      "Cost",
			"today_cost":          "Cost Today",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"total_sessions":      "all",
			"sessions_today":      "today",
			"sessions_7d":         "7d",
			"total_tokens":        "total",
			"total_input_tokens":  "in",
			"total_output_tokens": "out",
			"total_cache_read":    "cache read",
			"total_cache_write":   "cache write",
			"total_cost_usd":      "USD",
			"today_cost":          "today",
		}),
		// Muse Code has local session logs, quota, and now tool calls
		// (1,920 tool_call in the 2026-09-07 session). Hide the still-empty
		// client/language/code-stats/MCP panels so the tile doesn't render a
		// stack of "No X data" placeholders, but show Tool Usage like Codex
		// does (Tool Usage 6.4k calls · exec 88% etc. in the Codex tile).
		providerbase.WithSectionOrder(
			core.DashboardSectionHeader,
			core.DashboardSectionTopUsageProgress,
			core.DashboardSectionToolUsage,
			core.DashboardSectionOtherData,
		),
		func(cfg *core.DashboardWidget) {
			cfg.ShowClientComposition = false
			cfg.ShowLanguageComposition = false
			cfg.ShowCodeStatsComposition = false
			cfg.ShowActualToolUsage = true
			cfg.ShowMCPUsage = false
		},
	)
}

func detailWidget() core.DetailWidget {
	// Detail view: usage, model cost, trends, tools, tokens, activity.
	// Was slimmed to 6 sections to hide empty Clients/Projects/MCP/Language/
	// CodeStats, but Tool Usage is now populated (1,920 tool_call in the
	// 2026-09-07 session, like Codex's 6.4k calls) so re-add Tools.
	return core.DetailWidget{
		Sections: []core.DetailSection{
			{Name: "Usage", Order: 1, Style: core.DetailSectionStyleUsage},
			{Name: "Models", Order: 2, Style: core.DetailSectionStyleModels},
			{Name: "Tools", Order: 3, Style: core.DetailSectionStyleList},
			{Name: "Spending", Order: 4, Style: core.DetailSectionStyleSpending},
			{Name: "Trends", Order: 5, Style: core.DetailSectionStyleTrends},
			{Name: "Tokens", Order: 6, Style: core.DetailSectionStyleTokens},
			{Name: "Activity", Order: 7, Style: core.DetailSectionStyleActivity},
		},
	}
}
