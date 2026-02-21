package core

type DashboardDisplayStyle string

const (
	DashboardDisplayStyleDefault    DashboardDisplayStyle = "default"
	DashboardDisplayStyleOpenRouter DashboardDisplayStyle = "openrouter"
)

type DashboardResetStyle string

const (
	DashboardResetStyleDefault       DashboardResetStyle = "default"
	DashboardResetStyleGeminiCompact DashboardResetStyle = "gemini_compact"
)

type DashboardMetricMatcher struct {
	Prefix string
	Suffix string
}

type DashboardCompactRow struct {
	Label       string
	Keys        []string
	Matcher     DashboardMetricMatcher
	MaxSegments int
}

type DashboardWidget struct {
	DisplayStyle DashboardDisplayStyle
	ResetStyle   DashboardResetStyle

	// When ResetStyle is DashboardResetStyleGeminiCompact and the number of active
	// reset entries meets/exceeds this value, reset pills are grouped.
	ResetCompactThreshold int

	GaugePriority []string
	GaugeMaxLines int
	CompactRows   []DashboardCompactRow

	HideMetricKeys     []string
	HideMetricPrefixes []string

	// Hide noisy metrics that are often zero-value for this provider.
	SuppressZeroMetricKeys []string
	// Hide all zero-valued non-quota metrics.
	SuppressZeroNonQuotaMetrics bool
}

func DefaultDashboardWidget() DashboardWidget {
	return DashboardWidget{
		DisplayStyle: DashboardDisplayStyleDefault,
		ResetStyle:   DashboardResetStyleDefault,
		GaugePriority: []string{
			"spend_limit", "plan_spend", "credits", "credit_balance",
		},
		GaugeMaxLines: 2,
		CompactRows: []DashboardCompactRow{
			{
				Label:       "Credits",
				Keys:        []string{"credit_balance", "credits", "plan_spend", "plan_total_spend_usd", "total_cost_usd", "today_api_cost", "7d_api_cost", "all_time_api_cost", "monthly_spend"},
				MaxSegments: 4,
			},
			{
				Label:       "Usage",
				Keys:        []string{"spend_limit", "plan_percent_used", "usage_five_hour", "usage_seven_day", "rpm", "tpm", "rpd", "tpd"},
				MaxSegments: 4,
			},
			{
				Label:       "Activity",
				Keys:        []string{"messages_today", "sessions_today", "tool_calls_today", "requests_today", "total_conversations", "recent_requests"},
				MaxSegments: 4,
			},
		},
	}
}
