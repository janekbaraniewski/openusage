package providerbase

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// Base centralizes provider metadata and widget/detail configuration.
// Provider-specific packages embed this and implement only Fetch().
type Base struct {
	spec core.ProviderSpec
}

func New(spec core.ProviderSpec) Base {
	normalized := spec
	if normalized.ID == "" {
		normalized.ID = "unknown"
	}
	if normalized.Info.Name == "" {
		normalized.Info.Name = normalized.ID
	}
	if normalized.Setup.DocsURL == "" {
		normalized.Setup.DocsURL = normalized.Info.DocURL
	}

	return Base{spec: normalized}
}

func (b Base) ID() string {
	return b.spec.ID
}

func (b Base) Describe() core.ProviderInfo {
	return b.spec.Info
}

func (b Base) Spec() core.ProviderSpec {
	return b.spec
}

func (b Base) DashboardWidget() core.DashboardWidget {
	cfg := b.spec.Dashboard
	if isZeroDashboardWidget(cfg) {
		cfg = core.DefaultDashboardWidget()
	}
	return cfg
}

func (b Base) DetailWidget() core.DetailWidget {
	if len(b.spec.Detail.Sections) == 0 {
		return core.DefaultDetailWidget()
	}
	return b.spec.Detail
}

func isZeroDashboardWidget(w core.DashboardWidget) bool {
	return w.DisplayStyle == "" &&
		w.ResetStyle == "" &&
		w.ColorRole == "" &&
		!w.ShowClientComposition &&
		!w.ShowToolComposition &&
		w.APIKeyEnv == "" &&
		w.DefaultAccountID == "" &&
		w.ResetCompactThreshold == 0 &&
		len(w.GaugePriority) == 0 &&
		w.GaugeMaxLines == 0 &&
		len(w.CompactRows) == 0 &&
		len(w.RawGroups) == 0 &&
		len(w.MetricLabelOverrides) == 0 &&
		len(w.MetricGroupOverrides) == 0 &&
		len(w.CompactMetricLabelOverrides) == 0 &&
		len(w.HideMetricKeys) == 0 &&
		len(w.HideMetricPrefixes) == 0 &&
		!w.HideCreditsWhenBalancePresent &&
		len(w.SuppressZeroMetricKeys) == 0 &&
		!w.SuppressZeroNonUsageMetrics &&
		len(w.StandardSectionOrder) == 0 &&
		len(w.DataSpec.RequiredMetricKeys) == 0 &&
		len(w.DataSpec.OptionalMetricKeys) == 0 &&
		len(w.DataSpec.MetricPrefixes) == 0
}

type DashboardOption func(*core.DashboardWidget)

func DefaultDashboard(options ...DashboardOption) core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	for _, opt := range options {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func WithColorRole(role core.DashboardColorRole) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.ColorRole = role
	}
}

func WithGaugePriority(keys ...string) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.GaugePriority = keys
	}
}

func WithGaugeMaxLines(n int) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.GaugeMaxLines = n
	}
}

func WithCompactRows(rows ...core.DashboardCompactRow) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.CompactRows = rows
	}
}

func WithHideMetricPrefixes(prefixes ...string) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, prefixes...)
	}
}

func WithHideMetricKeys(keys ...string) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.HideMetricKeys = append(cfg.HideMetricKeys, keys...)
	}
}

func WithSectionOrder(sections ...core.DashboardStandardSection) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.StandardSectionOrder = sections
	}
}

func WithMetricLabels(labels map[string]string) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		for k, v := range labels {
			cfg.MetricLabelOverrides[k] = v
		}
	}
}

func WithCompactLabels(labels map[string]string) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		for k, v := range labels {
			cfg.CompactMetricLabelOverrides[k] = v
		}
	}
}

func WithRawGroups(groups ...core.DashboardRawGroup) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.RawGroups = append(cfg.RawGroups, groups...)
	}
}

func WithSuppressZeroMetricKeys(keys ...string) DashboardOption {
	return func(cfg *core.DashboardWidget) {
		cfg.SuppressZeroMetricKeys = keys
	}
}

// CodingToolDashboard returns a DashboardWidget pre-configured for coding-tool
// providers (Cursor, Claude Code, Codex, Copilot, Gemini CLI). It enables client/
// language/code-stats composition panels, applies standard hidden prefixes and
// section ordering, and merges shared code-stats metric labels.
func CodingToolDashboard(options ...DashboardOption) core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()

	cfg.ShowClientComposition = true
	cfg.ClientCompositionHeading = "Clients"
	cfg.ShowToolComposition = false
	cfg.ShowActualToolUsage = true
	cfg.ShowLanguageComposition = true
	cfg.ShowCodeStatsComposition = true
	cfg.CodeStatsMetrics = shared.DefaultCodeStatsConfig()
	cfg.StandardSectionOrder = shared.CodingToolSectionOrder()
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, shared.CodingToolHidePrefixes()...)

	// Merge shared code-stats labels.
	for k, v := range shared.CodeStatsMetricLabels {
		cfg.MetricLabelOverrides[k] = v
	}
	for k, v := range shared.CodeStatsCompactLabels {
		cfg.CompactMetricLabelOverrides[k] = v
	}

	for _, opt := range options {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}
