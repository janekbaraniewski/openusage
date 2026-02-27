package copilot

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleLavender),
		providerbase.WithGaugePriority(
			"chat_quota", "completions_quota", "premium_interactions_quota", "context_window",
			"gh_core_rpm", "gh_search_rpm", "gh_graphql_rpm",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{Label: "Credits", Keys: []string{"chat_quota", "completions_quota", "premium_interactions_quota", "cli_cost", "cost_today", "7d_cost"}, MaxSegments: 6},
			core.DashboardCompactRow{Label: "Usage", Keys: []string{"context_window", "tokens_today", "7d_tokens", "7d_tool_calls"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Rate", Keys: []string{"gh_core_rpm", "gh_search_rpm", "gh_graphql_rpm"}, MaxSegments: 3},
			core.DashboardCompactRow{Label: "Activity", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "total_prompts"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Tokens", Keys: []string{"cli_input_tokens", "cli_output_tokens", "cli_cache_read_tokens", "cli_cache_write_tokens"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Lines", Keys: []string{"composer_lines_added", "composer_lines_removed", "composer_files_changed", "scored_commits", "total_prompts"}, MaxSegments: 5},
			core.DashboardCompactRow{
				Label:       "Seats",
				Matcher:     core.DashboardMetricMatcher{Prefix: "org_", Suffix: "_seats"},
				MaxSegments: 3,
			},
		),
		providerbase.WithHideMetricPrefixes(
			"org_", "provider_", "cli_messages_", "cli_tokens_", "tokens_client_",
		),
		providerbase.WithHideMetricKeys(
			"total_messages", "total_sessions", "total_turns", "total_tool_calls",
			"total_response_chars", "total_reasoning_chars", "total_conversations",
			"cli_messages", "cli_turns", "cli_sessions", "cli_tool_calls", "cli_response_chars", "cli_reasoning_chars",
		),
		providerbase.WithRawGroups(
			core.DashboardRawGroup{
				Label: "Usage Split",
				Keys: []string{
					"model_usage", "client_usage", "model_turns", "model_sessions", "model_tool_calls",
					"model_response_chars", "model_reasoning_chars",
				},
			},
			core.DashboardRawGroup{
				Label: "Session",
				Keys: []string{
					"last_session_model", "last_session_client", "last_session_tokens", "last_session_repo",
					"last_session_branch", "last_session_time",
				},
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"premium_interactions_quota": "Premium Interactions",
			"gh_core_rpm":                "GitHub Core RPM",
			"gh_search_rpm":              "GitHub Search RPM",
			"gh_graphql_rpm":             "GitHub GraphQL RPM",
			"cli_input_tokens":           "CLI Input Tokens",
			"cli_output_tokens":          "CLI Output Tokens",
			"cli_cache_read_tokens":      "CLI Cache Read",
			"cli_cache_write_tokens":     "CLI Cache Write",
			"cli_total_tokens":           "CLI Total Tokens",
			"cli_cost":                   "Total Cost",
			"cost_today":                 "Cost Today",
			"7d_cost":                    "7-Day Cost",
			"cli_premium_requests":       "Premium Requests",
			"7d_tokens":                  "7-Day Tokens",
			"tokens_today":               "Today Tokens",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"gh_core_rpm":                "core",
			"gh_search_rpm":              "search",
			"gh_graphql_rpm":             "graphql",
			"premium_interactions_quota": "premium",
			"cli_input_tokens":           "cli in",
			"cli_output_tokens":          "cli out",
			"cli_cache_read_tokens":      "cache r",
			"cli_cache_write_tokens":     "cache w",
			"cli_cost":                   "cost",
			"cost_today":                 "today",
			"7d_cost":                    "7d",
			"cli_premium_requests":       "premium",
			"7d_tokens":                  "7d tok",
		}),
	)
}
