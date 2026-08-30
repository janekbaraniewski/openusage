package antigravity

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.DefaultDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleMauve),
		providerbase.WithGaugeMaxLines(4),
		providerbase.WithGaugePriority(
			"quota_gemini_5h",
			"quota_gemini_weekly",
			"quota_claude_5h",
			"quota_claude_weekly",
			"quota_3p_5h",
			"quota_3p_weekly",
			"quota_opus_sonnet_5h",
			"quota_opus_sonnet_weekly",
			"quota_gemini",
			"quota_claude",
			"quota_3p",
			"quota_opus_sonnet",
			"quota_gemini_flash",
			"quota_gemini_pro",
			"quota",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Gemini",
				Keys:        []string{"quota_gemini_5h", "quota_gemini_weekly", "quota_gemini_flash", "quota_gemini_pro", "quota_gemini"},
				MaxSegments: 3,
			},
			core.DashboardCompactRow{
				Label:       "Claude/Opus",
				Keys:        []string{"quota_claude_5h", "quota_claude_weekly", "quota_3p_5h", "quota_3p_weekly", "quota_opus_sonnet_5h", "quota_opus_sonnet_weekly", "quota_claude", "quota_3p", "quota_opus_sonnet"},
				MaxSegments: 3,
			},
		),
		providerbase.WithSectionOrder(
			core.DashboardSectionHeader,
			core.DashboardSectionTopUsageProgress,
			core.DashboardSectionModelBurn,
			core.DashboardSectionOtherData,
		),
		providerbase.WithMetricLabels(map[string]string{
			"quota_gemini_5h":          "Gemini (5h Limit)",
			"quota_gemini_weekly":      "Gemini (Weekly)",
			"quota_gemini":             "Gemini Quota",
			"quota_gemini_flash":       "Gemini Flash",
			"quota_gemini_pro":         "Gemini Pro",
			"quota_claude_5h":          "Claude/Opus (5h Limit)",
			"quota_claude_weekly":      "Claude/Opus (Weekly)",
			"quota_3p_5h":              "Claude/3P (5h Limit)",
			"quota_3p_weekly":          "Claude/3P (Weekly)",
			"quota_3p":                 "Claude/3P Quota",
			"quota_opus_sonnet_5h":     "Opus/Sonnet (5h Limit)",
			"quota_opus_sonnet_weekly": "Opus/Sonnet (Weekly)",
			"quota_claude":             "Claude Quota",
			"quota_opus_sonnet":        "Opus/Sonnet Quota",
			"quota":                    "Overall Quota",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"quota_gemini_5h":          "5h",
			"quota_gemini_weekly":      "wk",
			"quota_gemini":             "gemini",
			"quota_gemini_flash":       "flash",
			"quota_gemini_pro":         "pro",
			"quota_claude_5h":          "5h",
			"quota_claude_weekly":      "wk",
			"quota_3p_5h":              "5h",
			"quota_3p_weekly":          "wk",
			"quota_3p":                 "3p",
			"quota_opus_sonnet_5h":     "5h",
			"quota_opus_sonnet_weekly": "wk",
			"quota_claude":             "claude",
			"quota_opus_sonnet":        "opus",
			"quota":                    "all",
		}),
	)
}
