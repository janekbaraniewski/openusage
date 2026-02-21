package cursor

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleLavender
	cfg.GaugePriority = []string{
		"spend_limit", "plan_spend", "plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used", "chat_quota", "completions_quota",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"plan_spend", "plan_included", "plan_bonus"}, MaxSegments: 4},
		{Label: "Usage", Keys: []string{"spend_limit", "individual_spend", "plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used"}, MaxSegments: 5},
	}
	cfg.MetricLabelOverrides["plan_auto_percent_used"] = "Auto Used"
	cfg.MetricLabelOverrides["plan_api_percent_used"] = "API Used"
	cfg.CompactMetricLabelOverrides["plan_auto_percent_used"] = "auto"
	cfg.CompactMetricLabelOverrides["plan_api_percent_used"] = "api"
	return cfg
}
