package ollama

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()

	// Existing palette roles are already assigned across providers. Keep Ollama neutral.
	cfg.ColorRole = core.DashboardColorRoleAuto
	cfg.ShowClientComposition = true
	cfg.ShowToolComposition = true
	cfg.GaugePriority = []string{"usage_five_hour", "usage_weekly", "usage_one_day", "requests_today", "messages_today", "models_total", "loaded_models"}

	cfg.CompactRows = []core.DashboardCompactRow{
		{
			Label:       "Models",
			Keys:        []string{"models_total", "models_local", "models_cloud", "loaded_models"},
			MaxSegments: 4,
		},
		{
			Label:       "Source",
			Keys:        []string{"source_local_requests", "source_cloud_requests", "source_unknown_requests"},
			MaxSegments: 3,
		},
		{
			Label:       "Usage",
			Keys:        []string{"usage_five_hour", "usage_weekly", "usage_one_day", "requests_5h", "requests_1d"},
			MaxSegments: 4,
		},
		{
			Label:       "Tokens",
			Keys:        []string{"tokens_5h", "tokens_1d", "tokens_today", "7d_tokens"},
			MaxSegments: 4,
		},
		{
			Label:       "Activity",
			Keys:        []string{"sessions_5h", "tool_calls_5h", "sessions_1d", "tool_calls_1d"},
			MaxSegments: 4,
		},
		{
			Label:       "Realtime",
			Keys:        []string{"requests_today", "chat_requests_today", "generate_requests_today", "avg_latency_ms_today"},
			MaxSegments: 4,
		},
	}

	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_", "source_", "client_", "provider_", "tool_")
	cfg.SuppressZeroMetricKeys = []string{"http_4xx_today", "http_5xx_today", "tool_calls_today"}

	cfg.MetricLabelOverrides["models_total"] = "All Models"
	cfg.MetricLabelOverrides["models_local"] = "Local Models"
	cfg.MetricLabelOverrides["models_cloud"] = "Cloud Models"
	cfg.MetricLabelOverrides["loaded_models"] = "Loaded Models"
	cfg.MetricLabelOverrides["loaded_vram_bytes"] = "Loaded VRAM"
	cfg.MetricLabelOverrides["model_storage_bytes"] = "Local Storage"
	cfg.MetricLabelOverrides["usage_five_hour"] = "Usage 5h"
	cfg.MetricLabelOverrides["usage_weekly"] = "Usage Weekly"
	cfg.MetricLabelOverrides["usage_one_day"] = "Usage 1d"
	cfg.MetricLabelOverrides["requests_5h"] = "5h Requests"
	cfg.MetricLabelOverrides["messages_5h"] = "5h Messages"
	cfg.MetricLabelOverrides["sessions_5h"] = "5h Sessions"
	cfg.MetricLabelOverrides["tool_calls_5h"] = "5h Tool Calls"
	cfg.MetricLabelOverrides["requests_1d"] = "1d Requests"
	cfg.MetricLabelOverrides["messages_1d"] = "1d Messages"
	cfg.MetricLabelOverrides["sessions_1d"] = "1d Sessions"
	cfg.MetricLabelOverrides["tool_calls_1d"] = "1d Tool Calls"
	cfg.MetricLabelOverrides["tokens_5h"] = "5h Tokens (est)"
	cfg.MetricLabelOverrides["tokens_1d"] = "1d Tokens (est)"
	cfg.MetricLabelOverrides["tokens_today"] = "Today Tokens (est)"
	cfg.MetricLabelOverrides["7d_tokens"] = "7d Tokens (est)"
	cfg.MetricLabelOverrides["requests_today"] = "Today Requests"
	cfg.MetricLabelOverrides["recent_requests"] = "Last 24h Requests"
	cfg.MetricLabelOverrides["source_local_requests"] = "Local Requests"
	cfg.MetricLabelOverrides["source_cloud_requests"] = "Cloud Requests"
	cfg.MetricLabelOverrides["source_unknown_requests"] = "Unknown Requests"
	cfg.MetricLabelOverrides["avg_latency_ms_today"] = "Avg Latency (Today)"
	cfg.MetricLabelOverrides["avg_latency_ms_5h"] = "Avg Latency (5h)"
	cfg.MetricLabelOverrides["avg_latency_ms_1d"] = "Avg Latency (1d)"

	cfg.CompactMetricLabelOverrides["usage_five_hour"] = "5h"
	cfg.CompactMetricLabelOverrides["usage_weekly"] = "week"
	cfg.CompactMetricLabelOverrides["usage_one_day"] = "1d"
	cfg.CompactMetricLabelOverrides["models_total"] = "all"
	cfg.CompactMetricLabelOverrides["models_local"] = "local"
	cfg.CompactMetricLabelOverrides["models_cloud"] = "cloud"
	cfg.CompactMetricLabelOverrides["loaded_models"] = "loaded"
	cfg.CompactMetricLabelOverrides["source_local_requests"] = "local"
	cfg.CompactMetricLabelOverrides["source_cloud_requests"] = "cloud"
	cfg.CompactMetricLabelOverrides["source_unknown_requests"] = "other"
	cfg.CompactMetricLabelOverrides["requests_5h"] = "5h req"
	cfg.CompactMetricLabelOverrides["messages_5h"] = "5h msg"
	cfg.CompactMetricLabelOverrides["sessions_5h"] = "5h sess"
	cfg.CompactMetricLabelOverrides["tool_calls_5h"] = "5h tools"
	cfg.CompactMetricLabelOverrides["requests_1d"] = "1d req"
	cfg.CompactMetricLabelOverrides["messages_1d"] = "1d msg"
	cfg.CompactMetricLabelOverrides["sessions_1d"] = "1d sess"
	cfg.CompactMetricLabelOverrides["tool_calls_1d"] = "1d tools"
	cfg.CompactMetricLabelOverrides["tokens_5h"] = "5h tok"
	cfg.CompactMetricLabelOverrides["tokens_1d"] = "1d tok"
	cfg.CompactMetricLabelOverrides["tokens_today"] = "today tok"
	cfg.CompactMetricLabelOverrides["7d_tokens"] = "7d tok"
	cfg.CompactMetricLabelOverrides["requests_today"] = "req"
	cfg.CompactMetricLabelOverrides["chat_requests_today"] = "chat"
	cfg.CompactMetricLabelOverrides["generate_requests_today"] = "gen"
	cfg.CompactMetricLabelOverrides["messages_today"] = "msgs"
	cfg.CompactMetricLabelOverrides["sessions_today"] = "sess"
	cfg.CompactMetricLabelOverrides["tool_calls_today"] = "tools"
	cfg.CompactMetricLabelOverrides["avg_latency_ms_today"] = "lat"

	cfg.RawGroups = append(cfg.RawGroups, core.DashboardRawGroup{
		Label: "Ollama",
		Keys: []string{
			"account_email", "account_name", "plan_name",
			"selected_model", "cloud_disabled", "cloud_source", "cli_version",
			"models_usage_top", "model_tokens_estimated_top", "tool_usage", "token_estimation", "signin_url",
		},
	})

	return cfg
}
