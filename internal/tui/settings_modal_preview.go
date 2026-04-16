package tui

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func settingsWidgetSectionsPreviewSnapshot() core.UsageSnapshot {
	usedMetric := func(used float64, unit, window string) core.Metric {
		return core.Metric{
			Used:   &used,
			Unit:   unit,
			Window: window,
		}
	}
	limitMetric := func(limit, used float64, unit, window string) core.Metric {
		remaining := limit - used
		return core.Metric{
			Limit:     &limit,
			Used:      &used,
			Remaining: &remaining,
			Unit:      unit,
			Window:    window,
		}
	}
	recentSeries := func(end time.Time, values ...float64) []core.TimePoint {
		if len(values) == 0 {
			return nil
		}
		points := make([]core.TimePoint, 0, len(values))
		start := end.UTC().AddDate(0, 0, -(len(values) - 1))
		for i, value := range values {
			day := start.AddDate(0, 0, i)
			points = append(points, core.TimePoint{
				Date:  day.Format("2006-01-02"),
				Value: value,
			})
		}
		return points
	}

	snap := core.NewUsageSnapshot(settingsWidgetPreviewProviderID, "claude-preview")
	snap.Status = core.StatusOK
	snap.Message = "Settings preview"
	seriesEnd := time.Now().UTC().Truncate(24 * time.Hour)
	snap.Attributes = map[string]string{
		"telemetry_view": "canonical",
	}
	snap.Metrics = map[string]core.Metric{
		"usage_five_hour":                       limitMetric(200, 62, "requests", "5h"),
		"usage_seven_day":                       limitMetric(5000, 1730, "requests", "7d"),
		"today_api_cost":                        usedMetric(5.20, "USD", "1d"),
		"7d_api_cost":                           usedMetric(28.40, "USD", "7d"),
		"all_time_api_cost":                     usedMetric(412.30, "USD", "all"),
		"messages_today":                        usedMetric(37, "requests", "1d"),
		"sessions_today":                        usedMetric(6, "sessions", "1d"),
		"tool_calls_today":                      usedMetric(52, "requests", "1d"),
		"7d_tool_calls":                         usedMetric(281, "requests", "7d"),
		"today_input_tokens":                    usedMetric(182000, "tokens", "1d"),
		"today_output_tokens":                   usedMetric(64000, "tokens", "1d"),
		"7d_input_tokens":                       usedMetric(1230000, "tokens", "7d"),
		"7d_output_tokens":                      usedMetric(421000, "tokens", "7d"),
		"model_claude_sonnet_4_5_input_tokens":  usedMetric(820000, "tokens", "7d"),
		"model_claude_sonnet_4_5_output_tokens": usedMetric(286000, "tokens", "7d"),
		"model_claude_sonnet_4_5_requests":      usedMetric(932, "requests", "7d"),
		"model_claude_sonnet_4_5_cost_usd":      usedMetric(22.30, "USD", "7d"),
		"model_claude_haiku_3_5_input_tokens":   usedMetric(210000, "tokens", "7d"),
		"model_claude_haiku_3_5_output_tokens":  usedMetric(83000, "tokens", "7d"),
		"model_claude_haiku_3_5_requests":       usedMetric(511, "requests", "7d"),
		"model_claude_haiku_3_5_cost_usd":       usedMetric(4.10, "USD", "7d"),
		"client_claude_code_total_tokens":       usedMetric(900000, "tokens", "7d"),
		"client_claude_code_requests":           usedMetric(1020, "requests", "7d"),
		"client_claude_code_sessions":           usedMetric(19, "sessions", "7d"),
		"client_ide_total_tokens":               usedMetric(330000, "tokens", "7d"),
		"client_ide_requests":                   usedMetric(423, "requests", "7d"),
		"client_ide_sessions":                   usedMetric(11, "sessions", "7d"),
		"tool_edit":                             usedMetric(32, "requests", "7d"),
		"tool_bash":                             usedMetric(18, "requests", "7d"),
		"tool_read":                             usedMetric(24, "requests", "7d"),
		"tool_success_rate":                     usedMetric(94, "percent", "7d"),
		"mcp_github_total":                      usedMetric(16, "requests", "7d"),
		"mcp_github_search_repositories":        usedMetric(9, "requests", "7d"),
		"mcp_github_get_pull_request":           usedMetric(7, "requests", "7d"),
		"lang_go":                               usedMetric(58, "requests", "7d"),
		"lang_typescript":                       usedMetric(35, "requests", "7d"),
		"lang_markdown":                         usedMetric(14, "requests", "7d"),
		"composer_lines_added":                  usedMetric(980, "lines", "7d"),
		"composer_lines_removed":                usedMetric(420, "lines", "7d"),
		"composer_files_changed":                usedMetric(37, "files", "7d"),
		"scored_commits":                        usedMetric(9, "commits", "7d"),
		"ai_code_percentage":                    usedMetric(63, "percent", "7d"),
		"total_prompts":                         usedMetric(241, "requests", "7d"),
		"interface_bash":                        usedMetric(31, "requests", "7d"),
		"interface_edit":                        usedMetric(44, "requests", "7d"),
		"provider_anthropic_input_tokens":       usedMetric(1100000, "tokens", "7d"),
		"provider_anthropic_output_tokens":      usedMetric(369000, "tokens", "7d"),
		"provider_anthropic_requests":           usedMetric(1450, "requests", "7d"),
		"provider_anthropic_cost_usd":           usedMetric(26.40, "USD", "7d"),
		"upstream_aws_bedrock_input_tokens":     usedMetric(510000, "tokens", "7d"),
		"upstream_aws_bedrock_output_tokens":    usedMetric(177000, "tokens", "7d"),
		"upstream_aws_bedrock_requests":         usedMetric(742, "requests", "7d"),
		"upstream_aws_bedrock_cost_usd":         usedMetric(12.40, "USD", "7d"),
		"upstream_anthropic_input_tokens":       usedMetric(590000, "tokens", "7d"),
		"upstream_anthropic_output_tokens":      usedMetric(192000, "tokens", "7d"),
		"upstream_anthropic_requests":           usedMetric(708, "requests", "7d"),
		"upstream_anthropic_cost_usd":           usedMetric(14.00, "USD", "7d"),
	}
	snap.DailySeries = map[string][]core.TimePoint{
		"analytics_cost":                recentSeries(seriesEnd, 2.8, 3.2, 4.1, 3.7, 5.2),
		"analytics_requests":            recentSeries(seriesEnd, 210, 238, 290, 256, 311),
		"usage_model_claude_sonnet_4_5": recentSeries(seriesEnd, 154, 183, 201, 176, 218),
		"usage_model_claude_haiku_3_5":  recentSeries(seriesEnd, 91, 88, 103, 97, 111),
		"usage_client_claude_code":      recentSeries(seriesEnd, 160, 182, 211, 189, 229),
		"usage_client_ide":              recentSeries(seriesEnd, 63, 71, 79, 67, 82),
		"usage_source_bedrock":          recentSeries(seriesEnd, 108, 114, 128, 121, 133),
		"usage_source_claude":           recentSeries(seriesEnd, 102, 124, 146, 135, 152),
	}
	return snap
}
