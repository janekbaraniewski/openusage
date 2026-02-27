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

type DashboardMetricGroupOverride struct {
	Group string
	Label string
	Order int
}

type DashboardRawGroup struct {
	Label string
	Keys  []string
}

// StackedGaugeConfig describes how a metric renders as a stacked gauge bar.
// Each segment references another metric key whose Used value provides the
// segment's absolute amount.  Percentages are computed against the parent
// metric's Limit at render time.
type StackedGaugeConfig struct {
	SegmentMetricKeys []string // Metric keys — segment value = metric.Used
	SegmentLabels     []string // Display labels for each segment
	SegmentColors     []string // Theme color names: "teal", "peach", "green", etc.
}

// CodeStatsConfig maps metric keys to code statistics roles for graphical display.
type CodeStatsConfig struct {
	LinesAdded   string // metric key for lines added
	LinesRemoved string // metric key for lines removed
	FilesChanged string // metric key for files changed
	Commits      string // metric key for scored commits
	AIPercent    string // metric key for AI code %
	Prompts      string // metric key for total prompts
}

// WidgetDataSpec describes the expected metric payload for a dashboard widget.
// RequiredMetricKeys provide a strict contract; MetricPrefixes provide extensibility.
type WidgetDataSpec struct {
	RequiredMetricKeys []string
	OptionalMetricKeys []string
	MetricPrefixes     []string
}

// DashboardStandardSection identifies a normalized tile section.
type DashboardStandardSection string

const (
	DashboardSectionHeader            DashboardStandardSection = "header"
	DashboardSectionTopUsageProgress  DashboardStandardSection = "top_usage_progress"
	DashboardSectionModelBurn         DashboardStandardSection = "model_burn"
	DashboardSectionClientBurn        DashboardStandardSection = "client_burn"
	DashboardSectionToolUsage         DashboardStandardSection = "tool_usage"
	DashboardSectionActualToolUsage   DashboardStandardSection = "actual_tool_usage"
	DashboardSectionLanguageBurn      DashboardStandardSection = "language_burn"
	DashboardSectionCodeStats         DashboardStandardSection = "code_stats"
	DashboardSectionDailyUsage        DashboardStandardSection = "daily_usage"
	DashboardSectionProviderBurn      DashboardStandardSection = "provider_burn"
	DashboardSectionUpstreamProviders DashboardStandardSection = "upstream_providers"
	DashboardSectionOtherData         DashboardStandardSection = "other_data"
)

func defaultDashboardSectionOrder() []DashboardStandardSection {
	return []DashboardStandardSection{
		DashboardSectionHeader,
		DashboardSectionTopUsageProgress,
		DashboardSectionModelBurn,
		DashboardSectionClientBurn,
		DashboardSectionToolUsage,
		DashboardSectionActualToolUsage,
		DashboardSectionLanguageBurn,
		DashboardSectionCodeStats,
		DashboardSectionDailyUsage,
		DashboardSectionProviderBurn,
		DashboardSectionUpstreamProviders,
		DashboardSectionOtherData,
	}
}

func isKnownDashboardSection(section DashboardStandardSection) bool {
	switch section {
	case DashboardSectionHeader,
		DashboardSectionTopUsageProgress,
		DashboardSectionModelBurn,
		DashboardSectionClientBurn,
		DashboardSectionToolUsage,
		DashboardSectionActualToolUsage,
		DashboardSectionLanguageBurn,
		DashboardSectionCodeStats,
		DashboardSectionDailyUsage,
		DashboardSectionProviderBurn,
		DashboardSectionUpstreamProviders,
		DashboardSectionOtherData:
		return true
	default:
		return false
	}
}

type DashboardWidget struct {
	DisplayStyle DashboardDisplayStyle
	ResetStyle   DashboardResetStyle
	ColorRole    DashboardColorRole
	// Opt-in client composition panel (client share + trend) in tile view.
	ShowClientComposition bool
	// Override the default heading for the client composition section.
	ClientCompositionHeading string
	// When true, fold interface_ metrics into the client composition as separate entries.
	ClientCompositionIncludeInterfaces bool
	// Opt-in tool composition panel (tool share) in tile view.
	ShowToolComposition bool
	// Override the default "Tool Usage (calls)" heading for the tool composition section.
	ToolCompositionHeading string
	// Opt-in language composition panel (by-language request share) in tile view.
	ShowLanguageComposition bool
	// Opt-in graphical code statistics panel (lines added/removed, commits, AI %).
	ShowCodeStatsComposition bool
	// Metric keys for the code stats section (added, removed, files, commits, ai%).
	CodeStatsMetrics CodeStatsConfig
	// Opt-in actual tool usage panel (tool calls from agent bubbles).
	ShowActualToolUsage bool

	// API key provider metadata. APIKeyEnv marks a provider as configurable in API Keys tab.
	APIKeyEnv        string
	DefaultAccountID string

	// When ResetStyle is DashboardResetStyleCompactModelResets and the number of active
	// reset entries meets/exceeds this value, reset pills are grouped.
	ResetCompactThreshold int

	GaugePriority               []string
	StackedGaugeKeys            map[string]StackedGaugeConfig
	GaugeMaxLines               int
	CompactRows                 []DashboardCompactRow
	RawGroups                   []DashboardRawGroup
	MetricLabelOverrides        map[string]string
	MetricGroupOverrides        map[string]DashboardMetricGroupOverride
	CompactMetricLabelOverrides map[string]string

	HideMetricKeys     []string
	HideMetricPrefixes []string
	// Hide key-level "credits" row when richer account-level balance metric is present.
	HideCreditsWhenBalancePresent bool

	// Hide noisy metrics that are often zero-value for this provider.
	SuppressZeroMetricKeys []string
	// Hide all zero-valued non-quota metrics.
	SuppressZeroNonUsageMetrics bool

	// StandardSectionOrder controls normalized tile section ordering and visibility.
	// Unknown values are ignored; omitted sections are hidden.
	StandardSectionOrder []DashboardStandardSection

	DataSpec WidgetDataSpec
}

func DefaultDashboardWidget() DashboardWidget {
	return DashboardWidget{
		DisplayStyle:        DashboardDisplayStyleDefault,
		ResetStyle:          DashboardResetStyleDefault,
		ColorRole:           DashboardColorRoleAuto,
		ShowToolComposition: true,
		GaugePriority: []string{
			"spend_limit", "plan_spend", "credits", "credit_balance",
		},
		StackedGaugeKeys: map[string]StackedGaugeConfig{},
		GaugeMaxLines:    2,
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
		MetricLabelOverrides: map[string]string{
			"plan_percent_used":    "Plan Used",
			"plan_total_spend_usd": "Total Plan Spend",
			"spend_limit":          "Spend Limit",
			"individual_spend":     "Individual Spend",
			"context_window":       "Context Window",
			"messages_today":       "Today Messages",
			"tool_calls_today":     "Today Tools",
			"sessions_today":       "Today Sessions",
			"total_messages":       "All-Time Msgs",
			"total_sessions":       "All-Time Sessions",
			"total_cost_usd":       "All-Time Cost",
			"burn_rate":            "Burn Rate",
		},
		MetricGroupOverrides: map[string]DashboardMetricGroupOverride{},
		CompactMetricLabelOverrides: map[string]string{
			"plan_spend":           "plan",
			"plan_included":        "incl",
			"plan_bonus":           "bonus",
			"spend_limit":          "cap",
			"individual_spend":     "mine",
			"plan_percent_used":    "used",
			"plan_total_spend_usd": "plan",
			"plan_limit_usd":       "limit",
			"credit_balance":       "balance",
			"credits":              "credits",
			"monthly_spend":        "month",
			"rpm":                  "rpm",
			"tpm":                  "tpm",
			"rpd":                  "rpd",
			"tpd":                  "tpd",
			"chat_quota":           "chat",
			"completions_quota":    "comp",
			"context_window":       "ctx",
			"messages_today":       "msgs",
			"sessions_today":       "sess",
			"tool_calls_today":     "tools",
			"requests_today":       "req",
			"total_conversations":  "conv",
			"recent_requests":      "recent",
		},
		StandardSectionOrder: defaultDashboardSectionOrder(),
		DataSpec: WidgetDataSpec{
			OptionalMetricKeys: []string{
				"rpm", "tpm", "rpd", "tpd",
				"spend_limit", "plan_spend", "credit_balance", "credits",
				"messages_today", "sessions_today", "tool_calls_today",
			},
			MetricPrefixes: []string{
				"rate_limit_", "model_", "client_",
				"today_", "7d_", "30d_", "analytics_",
			},
		},
	}
}

func (w DashboardWidget) EffectiveStandardSectionOrder() []DashboardStandardSection {
	if len(w.StandardSectionOrder) == 0 {
		return defaultDashboardSectionOrder()
	}

	seen := make(map[DashboardStandardSection]bool, len(w.StandardSectionOrder))
	out := make([]DashboardStandardSection, 0, len(w.StandardSectionOrder))
	for _, section := range w.StandardSectionOrder {
		if !isKnownDashboardSection(section) || seen[section] {
			continue
		}
		out = append(out, section)
		seen[section] = true
	}
	return out
}

func (w DashboardWidget) MissingMetrics(snap UsageSnapshot) []string {
	if len(w.DataSpec.RequiredMetricKeys) == 0 {
		return nil
	}
	missing := make([]string, 0, len(w.DataSpec.RequiredMetricKeys))
	for _, key := range w.DataSpec.RequiredMetricKeys {
		if key == "" {
			continue
		}
		if _, ok := snap.Metrics[key]; !ok {
			missing = append(missing, key)
		}
	}
	return missing
}
