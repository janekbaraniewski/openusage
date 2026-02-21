package ollama

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()

	// Existing palette roles are already assigned across providers. Keep Ollama neutral.
	cfg.ColorRole = core.DashboardColorRoleAuto
	cfg.GaugePriority = []string{"requests_today", "messages_today", "models_total", "loaded_models"}

	cfg.CompactRows = []core.DashboardCompactRow{
		{
			Label:       "Models",
			Keys:        []string{"models_total", "models_local", "models_cloud", "loaded_models"},
			MaxSegments: 4,
		},
		{
			Label:       "Usage",
			Keys:        []string{"requests_today", "chat_requests_today", "generate_requests_today", "messages_today"},
			MaxSegments: 4,
		},
		{
			Label:       "Activity",
			Keys:        []string{"sessions_today", "tool_calls_today", "recent_requests", "avg_latency_ms_today"},
			MaxSegments: 4,
		},
	}

	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_")
	cfg.SuppressZeroMetricKeys = []string{"http_4xx_today", "http_5xx_today", "tool_calls_today"}

	cfg.MetricLabelOverrides["models_total"] = "All Models"
	cfg.MetricLabelOverrides["models_local"] = "Local Models"
	cfg.MetricLabelOverrides["models_cloud"] = "Cloud Models"
	cfg.MetricLabelOverrides["loaded_models"] = "Loaded Models"
	cfg.MetricLabelOverrides["loaded_vram_bytes"] = "Loaded VRAM"
	cfg.MetricLabelOverrides["model_storage_bytes"] = "Local Storage"
	cfg.MetricLabelOverrides["requests_today"] = "Today Requests"
	cfg.MetricLabelOverrides["recent_requests"] = "Last 24h Requests"
	cfg.MetricLabelOverrides["avg_latency_ms_today"] = "Avg Latency (Today)"

	cfg.CompactMetricLabelOverrides["models_total"] = "all"
	cfg.CompactMetricLabelOverrides["models_local"] = "local"
	cfg.CompactMetricLabelOverrides["models_cloud"] = "cloud"
	cfg.CompactMetricLabelOverrides["loaded_models"] = "loaded"
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
			"selected_model", "cloud_disabled", "cli_version",
		},
	})

	return cfg
}
