package copilot

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Quota", Keys: []string{"chat_quota", "completions_quota"}, MaxSegments: 3},
		{Label: "Rate", Keys: []string{"gh_core_rpm", "gh_search_rpm", "gh_graphql_rpm"}, MaxSegments: 3},
		{
			Label:       "Seats",
			Matcher:     core.DashboardMetricMatcher{Prefix: "org_", Suffix: "_seats"},
			MaxSegments: 3,
		},
	}
	return cfg
}
