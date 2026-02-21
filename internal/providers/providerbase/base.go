package providerbase

import "github.com/janekbaraniewski/openusage/internal/core"

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
	if b.spec.Auth.Type == core.ProviderAuthTypeAPIKey {
		if cfg.APIKeyEnv == "" {
			cfg.APIKeyEnv = b.spec.Auth.APIKeyEnv
		}
		if cfg.DefaultAccountID == "" {
			if b.spec.Auth.DefaultAccountID != "" {
				cfg.DefaultAccountID = b.spec.Auth.DefaultAccountID
			} else {
				cfg.DefaultAccountID = b.spec.ID
			}
		}
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
