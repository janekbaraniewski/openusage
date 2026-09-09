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
		// Muse Code only has local session logs and quota — no client,
		// tool, MCP, language, or code-stats telemetry. Hide those
		// composition panels so the tile doesn't render a stack of
		// "No X data for this time range" placeholders. ModelBurn and
		// DailyUsage are also hidden: ModelBurn expects `model_*` metrics
		// (`ExtractModelBreakdown`) while Muse Code only populates
		// `ModelUsage`/`DailySeries`, so it would always be empty.
		providerbase.WithSectionOrder(
			core.DashboardSectionHeader,
			core.DashboardSectionTopUsageProgress,
			core.DashboardSectionOtherData,
		),
		func(cfg *core.DashboardWidget) {
			cfg.ShowClientComposition = false
			cfg.ShowLanguageComposition = false
			cfg.ShowCodeStatsComposition = false
			cfg.ShowActualToolUsage = false
			cfg.ShowMCPUsage = false
		},
	)
}

func detailWidget() core.DetailWidget {
	// Detail view: usage, model cost, and trends only. The full
	// CodingToolDetailWidget would add Clients/Projects/Tools/MCP/Language/
	// CodeStats sections that Muse Code never populates — they'd render as
	// empty and the user asked to hide them.
	return core.DetailWidget{
		Sections: []core.DetailSection{
			{Name: "Usage", Order: 1, Style: core.DetailSectionStyleUsage},
			{Name: "Models", Order: 2, Style: core.DetailSectionStyleModels},
			{Name: "Spending", Order: 3, Style: core.DetailSectionStyleSpending},
			{Name: "Trends", Order: 4, Style: core.DetailSectionStyleTrends},
			{Name: "Tokens", Order: 5, Style: core.DetailSectionStyleTokens},
			{Name: "Activity", Order: 6, Style: core.DetailSectionStyleActivity},
		},
	}
}
