package ollama

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.DefaultDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleAuto),
		providerbase.WithGaugePriority("usage_five_hour", "usage_weekly", "usage_one_day", "requests_today", "messages_today", "models_total", "loaded_models"),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Models",
				Keys:        []string{"models_total", "models_local", "models_cloud", "loaded_models"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Source",
				Keys:        []string{"source_local_requests", "source_cloud_requests", "source_unknown_requests"},
				MaxSegments: 3,
			},
			core.DashboardCompactRow{
				Label:       "Usage",
				Keys:        []string{"usage_five_hour", "usage_weekly", "usage_one_day", "requests_5h", "requests_1d"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"tokens_5h", "tokens_1d", "tokens_today", "7d_tokens"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Activity",
				Keys:        []string{"sessions_5h", "tool_calls_5h", "sessions_1d", "tool_calls_1d"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Realtime",
				Keys:        []string{"requests_today", "chat_requests_today", "generate_requests_today", "avg_latency_ms_today"},
				MaxSegments: 4,
			},
		),
		providerbase.WithHideMetricPrefixes("model_", "source_", "client_", "provider_", "tool_"),
		providerbase.WithSuppressZeroMetricKeys("http_4xx_today", "http_5xx_today", "tool_calls_today"),
		providerbase.WithMetricLabels(map[string]string{
			"models_total":            "All Models",
			"models_local":            "Local Models",
			"models_cloud":            "Cloud Models",
			"loaded_models":           "Loaded Models",
			"loaded_vram_bytes":       "Loaded VRAM",
			"model_storage_bytes":     "Local Storage",
			"usage_five_hour":         "Usage 5h",
			"usage_weekly":            "Usage Weekly",
			"usage_one_day":           "Usage 1d",
			"requests_5h":             "5h Requests",
			"messages_5h":             "5h Messages",
			"sessions_5h":             "5h Sessions",
			"tool_calls_5h":           "5h Tool Calls",
			"requests_1d":             "1d Requests",
			"messages_1d":             "1d Messages",
			"sessions_1d":             "1d Sessions",
			"tool_calls_1d":           "1d Tool Calls",
			"tokens_5h":               "5h Tokens (est)",
			"tokens_1d":               "1d Tokens (est)",
			"tokens_today":            "Today Tokens (est)",
			"7d_tokens":               "7d Tokens (est)",
			"requests_today":          "Today Requests",
			"recent_requests":         "Last 24h Requests",
			"source_local_requests":   "Local Requests",
			"source_cloud_requests":   "Cloud Requests",
			"source_unknown_requests": "Unknown Requests",
			"avg_latency_ms_today":    "Avg Latency (Today)",
			"avg_latency_ms_5h":       "Avg Latency (5h)",
			"avg_latency_ms_1d":       "Avg Latency (1d)",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"usage_five_hour":          "5h",
			"usage_weekly":             "week",
			"usage_one_day":            "1d",
			"models_total":             "all",
			"models_local":             "local",
			"models_cloud":             "cloud",
			"loaded_models":            "loaded",
			"source_local_requests":    "local",
			"source_cloud_requests":    "cloud",
			"source_unknown_requests":  "other",
			"requests_5h":              "5h req",
			"messages_5h":              "5h msg",
			"sessions_5h":              "5h sess",
			"tool_calls_5h":            "5h tools",
			"requests_1d":              "1d req",
			"messages_1d":              "1d msg",
			"sessions_1d":              "1d sess",
			"tool_calls_1d":            "1d tools",
			"tokens_5h":                "5h tok",
			"tokens_1d":                "1d tok",
			"tokens_today":             "today tok",
			"7d_tokens":                "7d tok",
			"requests_today":           "req",
			"chat_requests_today":      "chat",
			"generate_requests_today":  "gen",
			"messages_today":           "msgs",
			"sessions_today":           "sess",
			"tool_calls_today":         "tools",
			"avg_latency_ms_today":     "lat",
		}),
		providerbase.WithRawGroups(core.DashboardRawGroup{
			Label: "Ollama",
			Keys: []string{
				"account_email", "account_name", "plan_name",
				"selected_model", "cloud_disabled", "cloud_source", "cli_version",
				"models_usage_top", "model_tokens_estimated_top", "tool_usage", "token_estimation", "signin_url",
			},
		}),
	)

	cfg.ShowClientComposition = true
	cfg.ShowToolComposition = true

	return cfg
}
