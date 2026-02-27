package shared

import "github.com/janekbaraniewski/openusage/internal/core"

// DefaultCodeStatsConfig returns the standard CodeStatsConfig used by coding-tool providers.
func DefaultCodeStatsConfig() core.CodeStatsConfig {
	return core.CodeStatsConfig{
		LinesAdded:   "composer_lines_added",
		LinesRemoved: "composer_lines_removed",
		FilesChanged: "composer_files_changed",
		Commits:      "scored_commits",
		AIPercent:    "ai_code_percentage",
		Prompts:      "total_prompts",
	}
}

// CodeStatsMetricLabels are display labels shared across coding-tool providers.
var CodeStatsMetricLabels = map[string]string{
	"composer_lines_added":   "Lines Added",
	"composer_lines_removed": "Lines Removed",
	"composer_files_changed": "Files Changed",
	"scored_commits":         "Commits",
	"total_prompts":          "Prompts",
	"ai_code_percentage":     "AI Code",
}

// CodeStatsCompactLabels are compact (tile pill) labels for code stats metrics.
var CodeStatsCompactLabels = map[string]string{
	"composer_lines_added":   "added",
	"composer_lines_removed": "removed",
	"composer_files_changed": "files",
	"scored_commits":         "commits",
	"total_prompts":          "prompts",
	"ai_code_percentage":     "ai %",
}

// CodingToolHidePrefixes returns the set of metric prefixes hidden by most coding-tool providers.
func CodingToolHidePrefixes() []string {
	return []string{
		"model_", "source_", "client_", "mode_", "interface_",
		"subagent_", "lang_", "tool_",
	}
}

// CodingToolSectionOrder returns the standard section order used by coding-tool providers.
func CodingToolSectionOrder() []core.DashboardStandardSection {
	return []core.DashboardStandardSection{
		core.DashboardSectionHeader,
		core.DashboardSectionTopUsageProgress,
		core.DashboardSectionModelBurn,
		core.DashboardSectionClientBurn,
		core.DashboardSectionActualToolUsage,
		core.DashboardSectionLanguageBurn,
		core.DashboardSectionCodeStats,
		core.DashboardSectionOtherData,
	}
}
