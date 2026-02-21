package cursor

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleLavender
	cfg.ShowClientComposition = true
	cfg.GaugePriority = []string{
		"spend_limit", "plan_spend", "plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used", "chat_quota", "completions_quota",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"plan_spend", "plan_included", "plan_bonus"}, MaxSegments: 4},
		{Label: "Usage", Keys: []string{"spend_limit", "individual_spend", "plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used"}, MaxSegments: 5},
		{Label: "Activity", Keys: []string{"requests_today", "total_ai_requests", "client_ide_sessions", "client_cli_agents_sessions"}, MaxSegments: 4},
	}
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "source_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "client_")
	cfg.MetricLabelOverrides["plan_auto_percent_used"] = "Auto Used"
	cfg.MetricLabelOverrides["plan_api_percent_used"] = "API Used"
	cfg.MetricLabelOverrides["client_ide_sessions"] = "IDE Sessions"
	cfg.MetricLabelOverrides["client_cli_agents_sessions"] = "CLI Agent Sessions"
	cfg.CompactMetricLabelOverrides["plan_auto_percent_used"] = "auto"
	cfg.CompactMetricLabelOverrides["plan_api_percent_used"] = "api"
	cfg.CompactMetricLabelOverrides["client_ide_sessions"] = "ide"
	cfg.CompactMetricLabelOverrides["client_cli_agents_sessions"] = "cli"
	cfg.CompactMetricLabelOverrides["requests_today"] = "today"
	cfg.CompactMetricLabelOverrides["total_ai_requests"] = "all"
	return cfg
}
