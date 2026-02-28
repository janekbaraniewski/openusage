package ollama

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleAuto),
		providerbase.WithGaugeMaxLines(3),
		providerbase.WithGaugePriority(
			"usage_five_hour", "usage_weekly", "usage_one_day",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Models",
				Keys:        []string{"models_total", "models_local", "models_cloud", "loaded_models"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Capabilities",
				Keys:        []string{"models_with_tools", "models_with_vision", "models_with_thinking", "max_context_length"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Usage",
				Keys:        []string{"requests_5h", "requests_1d", "requests_today", "requests_7d"},
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
				Keys:        []string{"chat_requests_today", "generate_requests_today", "avg_latency_ms_today", "thinking_requests"},
				MaxSegments: 4,
			},
		),
		providerbase.WithHideMetricPrefixes(
			"model_", "source_", "client_", "provider_", "tool_",
			"total_", "http_", "avg_latency_",
			"chat_requests_", "generate_requests_",
		),
		providerbase.WithHideMetricKeys(
			"total_parameters",
			"attachments_today", "cloud_model_stub_bytes",
			"configured_context_length", "last_24h_requests",
			"today_sessions", "messages_today", "messages_5h", "messages_1d",
			"requests_5h", "requests_1d", "requests_today",
			"tokens_5h", "tokens_1d", "tokens_today",
			"sessions_5h", "sessions_1d", "sessions_today",
			"tool_calls_5h", "tool_calls_1d",
			"loaded_model_bytes", "loaded_vram_bytes", "context_window",
			"recent_requests", "requests_7d", "7d_tokens",
			"usage_five_hour", "usage_weekly", "usage_one_day",
			"models_with_tools", "models_with_vision", "models_with_thinking",
			"max_context_length", "thinking_requests",
			"avg_thinking_seconds", "total_thinking_seconds",
		),
		providerbase.WithSuppressZeroMetricKeys("http_4xx_today", "http_5xx_today", "tool_calls_today"),
		providerbase.WithMetricLabels(map[string]string{
			"models_total":            "All Models",
			"models_local":            "Local Models",
			"models_cloud":            "Cloud Models",
			"loaded_models":           "Loaded Models",
			"loaded_vram_bytes":       "Loaded VRAM",
			"loaded_model_bytes":      "Loaded Size",
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
			"requests_7d":             "7d Requests",
			"avg_latency_ms_today":    "Avg Latency",
			"models_with_tools":       "Tool-capable",
			"models_with_vision":      "Vision-capable",
			"models_with_thinking":    "Think-capable",
			"max_context_length":      "Max Context",
			"thinking_requests":       "Think Requests",
			"avg_thinking_seconds":    "Avg Think Time",
			"total_thinking_seconds":  "Total Think Time",
			"context_window":          "Context Window",
			"chat_requests_today":     "Chat Requests",
			"generate_requests_today": "Generate Requests",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"usage_five_hour":         "5h",
			"usage_weekly":            "week",
			"usage_one_day":           "1d",
			"models_total":            "all",
			"models_local":            "local",
			"models_cloud":            "cloud",
			"loaded_models":           "loaded",
			"requests_5h":             "5h req",
			"sessions_5h":             "5h sess",
			"tool_calls_5h":           "5h tools",
			"requests_1d":             "1d req",
			"sessions_1d":             "1d sess",
			"tool_calls_1d":           "1d tools",
			"tokens_5h":               "5h tok",
			"tokens_1d":               "1d tok",
			"tokens_today":            "today tok",
			"7d_tokens":               "7d tok",
			"requests_today":          "req",
			"requests_7d":             "7d req",
			"chat_requests_today":     "chat",
			"generate_requests_today": "gen",
			"avg_latency_ms_today":    "lat",
			"models_with_tools":       "tools",
			"models_with_vision":      "vision",
			"models_with_thinking":    "think",
			"max_context_length":      "max ctx",
			"thinking_requests":       "think reqs",
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

	cfg.ClientCompositionIncludeInterfaces = true

	return cfg
}
