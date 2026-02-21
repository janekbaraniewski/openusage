package core

type DashboardDisplayStyle string

const (
	DashboardDisplayStyleDefault DashboardDisplayStyle = "default"
	// Detailed credits mode shows richer "remaining/today/week/models" messaging
	// when credit-like metrics are present.
	DashboardDisplayStyleDetailedCredits DashboardDisplayStyle = "detailed_credits"
)

type DashboardResetStyle string

const (
	DashboardResetStyleDefault DashboardResetStyle = "default"
	// Compact model resets mode groups many reset rows into model-oriented pills.
	DashboardResetStyleCompactModelResets DashboardResetStyle = "compact_model_resets"
)

type DashboardMetricMatcher struct {
	Prefix string
	Suffix string
}

type DashboardColorRole string

const (
	DashboardColorRoleAuto      DashboardColorRole = "auto"
	DashboardColorRoleGreen     DashboardColorRole = "green"
	DashboardColorRolePeach     DashboardColorRole = "peach"
	DashboardColorRoleLavender  DashboardColorRole = "lavender"
	DashboardColorRoleBlue      DashboardColorRole = "blue"
	DashboardColorRoleTeal      DashboardColorRole = "teal"
	DashboardColorRoleYellow    DashboardColorRole = "yellow"
	DashboardColorRoleSky       DashboardColorRole = "sky"
	DashboardColorRoleSapphire  DashboardColorRole = "sapphire"
	DashboardColorRoleMaroon    DashboardColorRole = "maroon"
	DashboardColorRoleFlamingo  DashboardColorRole = "flamingo"
	DashboardColorRoleRosewater DashboardColorRole = "rosewater"
)

type DashboardCompactRow struct {
	Label       string
	Keys        []string
	Matcher     DashboardMetricMatcher
	MaxSegments int
}

type DashboardRawGroup struct {
	Label string
	Keys  []string
}

type DashboardWidget struct {
	DisplayStyle DashboardDisplayStyle
	ResetStyle   DashboardResetStyle
	ColorRole    DashboardColorRole

	// API key provider metadata. APIKeyEnv marks a provider as configurable in API Keys tab.
	APIKeyEnv        string
	DefaultAccountID string

	// When ResetStyle is DashboardResetStyleCompactModelResets and the number of active
	// reset entries meets/exceeds this value, reset pills are grouped.
	ResetCompactThreshold int

	GaugePriority []string
	GaugeMaxLines int
	CompactRows   []DashboardCompactRow
	RawGroups     []DashboardRawGroup

	HideMetricKeys     []string
	HideMetricPrefixes []string
	// Hide key-level "credits" row when richer account-level balance metric is present.
	HideCreditsWhenBalancePresent bool

	// Hide noisy metrics that are often zero-value for this provider.
	SuppressZeroMetricKeys []string
	// Hide all zero-valued non-quota metrics.
	SuppressZeroNonQuotaMetrics bool
}

func DefaultDashboardWidget() DashboardWidget {
	return DashboardWidget{
		DisplayStyle: DashboardDisplayStyleDefault,
		ResetStyle:   DashboardResetStyleDefault,
		ColorRole:    DashboardColorRoleAuto,
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
		RawGroups: []DashboardRawGroup{
			{
				Label: "Account",
				Keys: []string{
					"account_email", "account_name", "plan_name", "plan_type", "plan_price",
					"membership_type", "team_membership", "organization_name",
				},
			},
			{
				Label: "Billing",
				Keys: []string{
					"billing_cycle_start", "billing_cycle_end", "billing_type",
					"subscription_status", "credits", "usage_based_billing",
					"spend_limit_type", "limit_policy_type",
				},
			},
			{
				Label: "Tool",
				Keys: []string{
					"cli_version", "oauth_status", "auth_type", "install_method",
					"binary", "project_id", "quota_api",
				},
			},
		},
	}
}
