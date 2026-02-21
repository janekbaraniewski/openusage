package cursor

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.GaugePriority = []string{
		"spend_limit", "plan_spend", "plan_percent_used", "chat_quota", "completions_quota",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"plan_spend", "plan_included", "plan_bonus"}, MaxSegments: 4},
		{Label: "Usage", Keys: []string{"spend_limit", "individual_spend", "plan_percent_used"}, MaxSegments: 4},
	}
	return cfg
}
