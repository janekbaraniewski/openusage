package copilot

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleSapphire
	cfg.ShowClientComposition = true
	cfg.ShowToolComposition = true
	cfg.GaugePriority = []string{
		"chat_quota", "completions_quota", "premium_interactions_quota", "context_window",
		"gh_core_rpm", "gh_search_rpm", "gh_graphql_rpm",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Quota", Keys: []string{"chat_quota", "completions_quota", "premium_interactions_quota"}, MaxSegments: 4},
		{Label: "Rate", Keys: []string{"gh_core_rpm", "gh_search_rpm", "gh_graphql_rpm"}, MaxSegments: 3},
		{Label: "Activity", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "total_messages"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"context_window", "cli_input_tokens", "7d_tokens"}, MaxSegments: 3},
		{
			Label:       "Seats",
			Matcher:     core.DashboardMetricMatcher{Prefix: "org_", Suffix: "_seats"},
			MaxSegments: 3,
		},
	}
	cfg.RawGroups = append(cfg.RawGroups,
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
	)
	cfg.MetricLabelOverrides["premium_interactions_quota"] = "Premium Interactions"
	cfg.MetricLabelOverrides["gh_core_rpm"] = "GitHub Core RPM"
	cfg.MetricLabelOverrides["gh_search_rpm"] = "GitHub Search RPM"
	cfg.MetricLabelOverrides["gh_graphql_rpm"] = "GitHub GraphQL RPM"
	cfg.MetricLabelOverrides["cli_input_tokens"] = "CLI Input Tokens"
	cfg.MetricLabelOverrides["cli_total_tokens"] = "CLI Total Tokens"
	cfg.MetricLabelOverrides["7d_tokens"] = "7-Day Tokens"
	cfg.MetricLabelOverrides["tokens_today"] = "Today Tokens"
	cfg.CompactMetricLabelOverrides["gh_core_rpm"] = "core"
	cfg.CompactMetricLabelOverrides["gh_search_rpm"] = "search"
	cfg.CompactMetricLabelOverrides["gh_graphql_rpm"] = "graphql"
	cfg.CompactMetricLabelOverrides["premium_interactions_quota"] = "premium"
	cfg.CompactMetricLabelOverrides["cli_input_tokens"] = "cli in"
	cfg.CompactMetricLabelOverrides["7d_tokens"] = "7d tok"
	return cfg
}
