package kimi_cli

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleFlamingo),
		// No quota/rate-limit metrics: leave GaugePriority at the default
		// credit keys (absent here) so no gauge area or shimmer placeholder
		// renders for this local-stats-only provider.
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Sessions",
				Keys:        []string{"sessions_7d", "sessions_today", "total_sessions"},
				MaxSegments: 4,
			},
			// The daemon's windowed views drop the provider's all-time
			// total_* metrics and re-export the numbers as canonical
			// provider_kimi_cli_* metrics, so those are listed as fallbacks:
			// compact-label dedup keeps whichever variant exists first.
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"total_tokens", "total_input_tokens", "total_output_tokens", "provider_kimi_cli_input_tokens", "provider_kimi_cli_output_tokens", "total_cache_read", "total_cache_write"},
				MaxSegments: 4,
			},
		),
		// Canonical provider_kimi_cli_* metrics are consumed by the compact
		// rows and the hero summary; without hiding they also show up as raw
		// "Provider Kimi Cli Input Tokens" lines.
		providerbase.WithHideMetricPrefixes("provider_kimi_cli_"),
		// window_* duplicates the tile's "N reqs · M tok in <window>" activity
		// line; total_cache_write is noise while zero.
		providerbase.WithHideMetricKeys("window_requests", "window_tokens"),
		providerbase.WithSuppressZeroMetricKeys("total_cache_write"),
		providerbase.WithMetricLabels(map[string]string{
			"total_sessions":      "Sessions",
			"total_tokens":        "Total Tokens",
			"total_input_tokens":  "Input Tokens",
			"total_output_tokens": "Output Tokens",
			"total_cache_read":    "Cache Read",
			"total_cache_write":   "Cache Write",
			"sessions_today":      "Sessions Today",
			"sessions_7d":         "Sessions 7d",
			// The hero summary falls back to the alphabetically first
			// valued metric — in windowed views that is
			// provider_kimi_cli_input_tokens; give it a readable label.
			"provider_kimi_cli_input_tokens":  "Input",
			"provider_kimi_cli_output_tokens": "Output",
			"provider_kimi_cli_requests":      "Requests",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"total_sessions":                  "all",
			"sessions_today":                  "today",
			"sessions_7d":                     "7d",
			"total_tokens":                    "total",
			"total_input_tokens":              "in",
			"total_output_tokens":             "out",
			"total_cache_read":                "cache r",
			"total_cache_write":               "cache w",
			"provider_kimi_cli_input_tokens":  "in",
			"provider_kimi_cli_output_tokens": "out",
			"provider_kimi_cli_requests":      "reqs",
		}),
	)
}

func detailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}
